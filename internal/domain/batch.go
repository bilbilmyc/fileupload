package domain

import (
	"context"
	"fmt"
	"io"
	"log"
	"sort"
	"time"
)

// BatchService 批量操作编排
type BatchService struct {
	uploadSvc   *UploadService
	downloadSvc *DownloadService
	meta        Metadata
	storage     Storage
	compress    Compressor
}

// NewBatchService 创建批量操作服务
func NewBatchService(uploadSvc *UploadService, downloadSvc *DownloadService, meta Metadata, storage Storage, compress Compressor) *BatchService {
	return &BatchService{
		uploadSvc:   uploadSvc,
		downloadSvc: downloadSvc,
		meta:        meta,
		storage:     storage,
		compress:    compress,
	}
}

// BatchDeleteRequest 批量删除请求
type BatchDeleteRequest struct {
	IDs []string `json:"ids"`
}

// BatchDeleteResult 批量删除结果
type BatchDeleteResult struct {
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

// BatchDelete 批量删除文件/目录
func (s *BatchService) BatchDelete(ctx context.Context, ids []string, namespace string) (*BatchDeleteResult, error) {
	var result BatchDeleteResult
	for _, id := range ids {
		if err := s.uploadSvc.Delete(ctx, id, namespace); err != nil {
			log.Printf("[batch] 删除失败 id=%s: %v", id, err)
			result.Failed++
		} else {
			result.Success++
		}
	}
	return &result, nil
}

// BatchDownloadRequest 批量下载请求
type BatchDownloadRequest struct {
	IDs    []string          `json:"ids"`
	Format CompressionFormat `json:"format"`
}

// BatchDownload 批量打包下载（流式）
// 返回 io.ReadCloser，读取它即获得打包数据流。
func (s *BatchService) BatchDownload(ctx context.Context, ids []string, namespace string, format CompressionFormat) (io.ReadCloser, error) {
	// 收集所有文件信息
	type fileEntry struct {
		name   string
		fullPath string
		size   int64
		sha256 string
		blob   *ContentBlob
	}

	var entries []fileEntry
	nameCount := make(map[string]int) // 重名计数

	for _, id := range ids {
		file, err := s.meta.GetFile(ctx, id)
		if err != nil {
			continue
		}
		if file == nil || file.IsDir {
			continue
		}
		if file.Namespace != namespace {
			continue
		}

		blob, err := s.meta.GetBlobBySha(ctx, file.SHA256)
		if err != nil || blob == nil {
			continue
		}

		// 处理重名
		name := file.Name
		nameCount[name]++
		if nameCount[name] > 1 {
			name = fmt.Sprintf("%d_%s", nameCount[name]-1, name)
		}

		entries = append(entries, fileEntry{
			name:     name,
			fullPath: blob.StoragePath,
			size:     file.Size,
			sha256:   file.SHA256,
			blob:     blob,
		})
	}

	if len(entries) == 0 {
		return nil, ErrNotFound
	}

	// 排序确保确定性
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	// 流式打包
	pr, pw := io.Pipe()
	go func() {
		archiveWriter, err := s.compress.NewArchiveWriter(ctx, pw, format)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("创建归档器: %w", err))
			return
		}

		for _, entry := range entries {
			reader, err := s.storage.Open(ctx, entry.blob.StoragePath, 0, 0)
			if err != nil {
				continue
			}

			if err := archiveWriter.AddFile(ctx, entry.name, entry.size, reader); err != nil {
				reader.Close()
				pw.CloseWithError(fmt.Errorf("写入归档条目 %s: %w", entry.name, err))
				return
			}
			reader.Close()
		}

		if err := archiveWriter.Close(); err != nil {
			pw.CloseWithError(fmt.Errorf("关闭归档器: %w", err))
			return
		}
		pw.Close()
	}()

	return pr, nil
}

// BatchMoveRequest 批量移动请求
type BatchMoveRequest struct {
	IDs         []string `json:"ids"`
	TargetDirID string   `json:"target_dir_id"`
}

// BatchMove 批量移动到目标目录
func (s *BatchService) BatchMove(ctx context.Context, ids []string, targetDirID string, namespace string) error {
	// 如果有目标目录，验证它存在且属于该 namespace
	if targetDirID != "" {
		targetDir, err := s.meta.GetFile(ctx, targetDirID)
		if err != nil {
			return fmt.Errorf("获取目标目录: %w", err)
		}
		if targetDir == nil {
			return ErrNotFound
		}
		if targetDir.Namespace != namespace {
			return ErrForbidden
		}
		if !targetDir.IsDir {
			return ErrInvalidArgument
		}
	}

	for _, id := range ids {
		file, err := s.meta.GetFile(ctx, id)
		if err != nil {
			continue
		}
		if file == nil {
			continue
		}
		if file.Namespace != namespace {
			continue
		}

		var parentID *string
		if targetDirID != "" {
			parentID = &targetDirID
		}
		_ = s.meta.UpdateFileParent(ctx, id, parentID)
	}

	log.Printf("[batch] 移动 %d 个文件到 %s", len(ids), targetDirID)
	return nil
}

// BatchCopyRequest 批量复制请求
type BatchCopyRequest struct {
	IDs         []string `json:"ids"`
	TargetDirID string   `json:"target_dir_id"`
}

// BatchCopy 批量复制到目标目录
func (s *BatchService) BatchCopy(ctx context.Context, ids []string, targetDirID string, namespace string) error {
	// 验证目标目录
	if targetDirID != "" {
		targetDir, err := s.meta.GetFile(ctx, targetDirID)
		if err != nil {
			return fmt.Errorf("获取目标目录: %w", err)
		}
		if targetDir == nil {
			return ErrNotFound
		}
		if targetDir.Namespace != namespace {
			return ErrForbidden
		}
		if !targetDir.IsDir {
			return ErrInvalidArgument
		}
	}

	now := time.Now()
	var copied int

	for _, id := range ids {
		file, err := s.meta.GetFile(ctx, id)
		if err != nil || file == nil {
			continue
		}
		if file.Namespace != namespace {
			continue
		}

		if file.IsDir {
			// 目录复制：创建新目录节点
			newID := NewID()
			newDir := &FileMetadata{
				FileID:    newID,
				Name:      file.Name,
				Path:      file.Path,
				Namespace: namespace,
				IsDir:     true,
				ParentID:  targetDirID,
				CreatedAt: now,
			}
			if err := s.meta.PutFile(ctx, newDir); err != nil {
				continue
			}
			// 递归复制子节点
			if err := s.copyDirChildren(ctx, file.FileID, newID, namespace, now); err != nil {
				log.Printf("[batch] 递归复制目录失败 %s: %v", file.FileID, err)
			}
			copied++
		} else {
			// 文件复制：新 file_id，指向同一 blob
			newID := NewID()
			newFile := &FileMetadata{
				FileID:    newID,
				SHA256:    file.SHA256,
				Name:      file.Name,
				Path:      file.Path,
				Size:      file.Size,
				Namespace: namespace,
				IsDir:     false,
				ParentID:  targetDirID,
				CreatedAt: now,
			}
			if err := s.meta.PutFile(ctx, newFile); err != nil {
				continue
			}
			if file.SHA256 != "" {
				_ = s.meta.IncrBlobRef(ctx, file.SHA256)
			}
			copied++
		}
	}

	log.Printf("[batch] 复制 %d 个文件到 %s", copied, targetDirID)
	return nil
}

// copyDirChildren 递归复制目录子节点
func (s *BatchService) copyDirChildren(ctx context.Context, sourceDirID, targetDirID, namespace string, now time.Time) error {
	children, err := s.meta.ListChildren(ctx, sourceDirID, "")
	if err != nil {
		return err
	}

	for _, child := range children {
		if child.IsDir {
			newDirID := NewID()
			newDir := &FileMetadata{
				FileID:    newDirID,
				Name:      child.Name,
				Path:      child.Path,
				Namespace: namespace,
				IsDir:     true,
				ParentID:  targetDirID,
				CreatedAt: now,
			}
			if err := s.meta.PutFile(ctx, newDir); err != nil {
				continue
			}
			if err := s.copyDirChildren(ctx, child.FileID, newDirID, namespace, now); err != nil {
				return err
			}
		} else {
			newID := NewID()
			newFile := &FileMetadata{
				FileID:    newID,
				SHA256:    child.SHA256,
				Name:      child.Name,
				Path:      child.Path,
				Size:      child.Size,
				Namespace: namespace,
				IsDir:     false,
				ParentID:  targetDirID,
				CreatedAt: now,
			}
			if err := s.meta.PutFile(ctx, newFile); err != nil {
				continue
			}
			if child.SHA256 != "" {
				_ = s.meta.IncrBlobRef(ctx, child.SHA256)
			}
		}
	}
	return nil
}

// BatchTagRequest 批量标记请求
type BatchTagRequest struct {
	IDs  []string `json:"ids"`
	Tags []string `json:"tags"`
}

// BatchTag 批量设置标签
func (s *BatchService) BatchTag(ctx context.Context, ids []string, tags []string, namespace string) error {
	for _, id := range ids {
		file, err := s.meta.GetFile(ctx, id)
		if err != nil || file == nil {
			continue
		}
		if file.Namespace != namespace {
			continue
		}
		_ = s.meta.SetFileTags(ctx, id, tags)
	}
	log.Printf("[batch] 标记 %d 个文件: %v", len(ids), tags)
	return nil
}
