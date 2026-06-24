package domain

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"
)

// ============================================================
// BatchService 依赖接口
// ============================================================

// FileDeleter 文件/目录删除接口 — 由 UploadService 实现
type FileDeleter interface {
	DeleteFile(ctx context.Context, fileID, namespace string) error
	DeleteDir(ctx context.Context, dirID string, recursive bool, namespace string) error
}

// FileMover 文件移动接口 — 由 UploadService 实现
type FileMover interface {
	MoveFile(ctx context.Context, fileID, targetDirID, namespace string) error
}

// DownloadPacker 批量下载打包接口 — 由 DownloadService 实现
type DownloadPacker interface {
	StreamBatch(ctx context.Context, ids []string, namespace string, format CompressionFormat) (io.ReadCloser, error)
}

// ============================================================
// BatchService
// ============================================================

// BatchService 批量操作编排
// 依赖接口而非具体服务类型，便于单元测试。
//
// meta 字段是 FileStore + BlobStore 的复合接口（ADR-0006）：
// BatchCopy 复制文件后需要 IncrBlobRef 增加引用计数，因此必须同时持有
// FileStore（GetFile/PutFile/ListChildren/SetFileTags）与 BlobStore（IncrBlobRef）。
// 不使用更宽的 domain.Metadata，因为 BatchService 不会用到 SessionStore/AdminStore。
type BatchService struct {
	deleter FileDeleter
	mover   FileMover
	packer  DownloadPacker
	meta    interface {
		FileStore
		BlobStore
	}
}

// NewBatchService 创建批量操作服务
func NewBatchService(deleter FileDeleter, mover FileMover, packer DownloadPacker, meta interface {
	FileStore
	BlobStore
}) *BatchService {
	return &BatchService{
		deleter: deleter,
		mover:   mover,
		packer:  packer,
		meta:    meta,
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
		// 先判断是文件还是目录
		file, err := s.meta.GetFile(ctx, id)
		if err != nil {
			result.Failed++
			continue
		}
		if file == nil {
			result.Failed++
			continue
		}
		if file.Namespace != namespace {
			result.Failed++
			continue
		}

		var delErr error
		if file.IsDir {
			delErr = s.deleter.DeleteDir(ctx, id, true, namespace)
		} else {
			delErr = s.deleter.DeleteFile(ctx, id, namespace)
		}
		if delErr != nil {
			log.Printf("[batch] 删除失败 id=%s: %v", id, delErr)
			result.Failed++
		} else {
			result.Success++
		}
	}
	return &result, nil
}

// BatchDownload 批量打包下载（流式）
// 返回 io.ReadCloser，读取它即获得打包数据流。
func (s *BatchService) BatchDownload(ctx context.Context, ids []string, namespace string, format CompressionFormat) (io.ReadCloser, error) {
	return s.packer.StreamBatch(ctx, ids, namespace, format)
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
		if err := s.mover.MoveFile(ctx, id, targetDirID, namespace); err != nil {
			log.Printf("[batch] 移动失败 id=%s: %v", id, err)
		}
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
				if err := s.meta.IncrBlobRef(ctx, file.SHA256); err != nil {
					// 引用计数失败不回滚 PutFile — 属于 refCounter 端口的范畴（ADR-0006）
					log.Printf("[batch] 复制后引用计数失败 sha=%s: %v", file.SHA256, err)
				}
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
				if err := s.meta.IncrBlobRef(ctx, child.SHA256); err != nil {
					// 引用计数失败不回滚 PutFile — 属于 refCounter 端口的范畴（ADR-0006）
					log.Printf("[batch] 复制后引用计数失败 sha=%s: %v", child.SHA256, err)
				}
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
