package domain

import (
	"context"
	"fmt"
	"time"
)

// Trash 将文件或目录移动到回收站。软删除期间保留 blob 引用与物理内容，
// 从而保证恢复不需要重新上传，也不会使共享内容提前被清理。
func (s *UploadService) Trash(ctx context.Context, id, namespace string) error {
	file, err := s.meta.GetFile(ctx, id)
	if err != nil {
		return fmt.Errorf("获取文件: %w", err)
	}
	if file == nil {
		return ErrNotFound
	}
	if file.Namespace != namespace {
		return ErrForbidden
	}
	return s.trashTree(ctx, file, time.Now().UTC())
}

func (s *UploadService) trashTree(ctx context.Context, file *FileMetadata, deletedAt time.Time) error {
	if file.IsDir {
		children, err := s.meta.ListChildren(ctx, file.FileID, "")
		if err != nil {
			return fmt.Errorf("列举目录子节点: %w", err)
		}
		for _, child := range children {
			if err := s.trashTree(ctx, child, deletedAt); err != nil {
				return err
			}
		}
	}
	if err := s.meta.MoveFileToTrash(ctx, file.FileID, deletedAt); err != nil {
		return fmt.Errorf("移入回收站: %w", err)
	}
	return nil
}

// ListTrash 返回当前命名空间的回收站内容。目录与其子项均保留，前端可据 parent_id 渲染层级。
func (s *UploadService) ListTrash(ctx context.Context, namespace string) ([]*FileMetadata, error) {
	return s.meta.ListTrash(ctx, namespace)
}

// RestoreFromTrash 恢复目标及其已删除后代，保留原文件 ID、路径、标签和内容引用。
func (s *UploadService) RestoreFromTrash(ctx context.Context, id, namespace string) error {
	items, err := s.meta.ListTrash(ctx, namespace)
	if err != nil {
		return fmt.Errorf("列举回收站: %w", err)
	}
	root, tree := trashTreeByID(items, id)
	if root == nil {
		return ErrNotFound
	}
	for _, item := range tree {
		if err := s.meta.RestoreFile(ctx, item.FileID); err != nil {
			return fmt.Errorf("恢复文件 %s: %w", item.FileID, err)
		}
	}
	return nil
}

// PurgeFromTrash 彻底删除目标及其后代。该操作会递减 blob 引用并在无引用时删除物理内容，
// 因此仅应由用户在回收站内显式确认后调用。
func (s *UploadService) PurgeFromTrash(ctx context.Context, id, namespace string) error {
	items, err := s.meta.ListTrash(ctx, namespace)
	if err != nil {
		return fmt.Errorf("列举回收站: %w", err)
	}
	root, tree := trashTreeByID(items, id)
	if root == nil {
		return ErrNotFound
	}
	// 先完整恢复树，使既有、经充分验证的硬删除逻辑可以复用其引用计数处理。
	for _, item := range tree {
		if err := s.meta.RestoreFile(ctx, item.FileID); err != nil {
			return fmt.Errorf("准备彻底删除 %s: %w", item.FileID, err)
		}
	}
	if root.IsDir {
		return s.deleteDirDeferred(ctx, root, namespace)
	}
	var undo []undoOp
	var pending []string
	if err := s.deleteFileWithUndoDeferred(ctx, root, &undo, &pending); err != nil {
		s.rollbackDeleteUndo(ctx, undo)
		return err
	}
	for _, storagePath := range pending {
		if err := s.storage.Delete(ctx, storagePath); err != nil {
			// 元数据已经提交，删除失败只造成可重试的孤儿物理对象，不回滚引用。
			return fmt.Errorf("删除物理内容: %w", err)
		}
	}
	return nil
}

func trashTreeByID(items []*FileMetadata, id string) (*FileMetadata, []*FileMetadata) {
	byID := make(map[string]*FileMetadata, len(items))
	children := make(map[string][]*FileMetadata)
	for _, item := range items {
		byID[item.FileID] = item
		children[item.ParentID] = append(children[item.ParentID], item)
	}
	root := byID[id]
	if root == nil {
		return nil, nil
	}
	result := make([]*FileMetadata, 0, 1)
	queue := []*FileMetadata{root}
	seen := map[string]bool{root.FileID: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		for _, child := range children[current.FileID] {
			if !seen[child.FileID] {
				seen[child.FileID] = true
				queue = append(queue, child)
			}
		}
	}
	return root, result
}
