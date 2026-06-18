package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
)

// DownloadService 下载编排核心
// 单文件 Range 读取 + 校验和返回；目录流式打包（io.Pipe）。
type DownloadService struct {
	meta     Metadata
	storage  Storage
	compress Compressor
	hasher   Hasher
	cfg      DownloadConfig
}

// DownloadConfig 下载服务配置
type DownloadConfig struct {
	DataDir string // 数据目录
}

// NewDownloadService 创建下载服务
func NewDownloadService(meta Metadata, storage Storage, compress Compressor, hasher Hasher, cfg DownloadConfig) *DownloadService {
	return &DownloadService{
		meta:     meta,
		storage:  storage,
		compress: compress,
		hasher:   hasher,
		cfg:      cfg,
	}
}

// FileReader 单文件下载结果
type FileReader struct {
	File     *FileMetadata
	Blob     *ContentBlob
	Reader   io.ReadCloser
	FileSize int64 // HTTP Range 需要用到的文件总大小
}

// GetFile 单文件下载，支持 Range
// offset=0, length=0 表示整个文件
func (s *DownloadService) GetFile(ctx context.Context, fileID, namespace string, rng DownloadRange) (*FileReader, error) {
	file, err := s.meta.GetFile(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("获取文件: %w", err)
	}
	if file == nil {
		return nil, ErrNotFound
	}
	if file.Namespace != namespace {
		return nil, ErrForbidden
	}

	// 获取 blob 信息
	var blob *ContentBlob
	if file.SHA256 != "" {
		blob, err = s.meta.GetBlobBySha(ctx, file.SHA256)
		if err != nil {
			return nil, fmt.Errorf("获取 blob: %w", err)
		}
	}
	if blob == nil {
		return nil, ErrCorrupted
	}

	// 限制 range 不超出文件
	offset := rng.Offset
	length := rng.Length
	if offset < 0 {
		offset = 0
	}
	if offset >= blob.Size {
		return nil, ErrInvalidArgument
	}
	if length == 0 || offset+length > blob.Size {
		length = blob.Size - offset
	}

	reader, err := s.storage.Open(ctx, blob.StoragePath, offset, length)
	if err != nil {
		return nil, fmt.Errorf("打开存储文件: %w", err)
	}

	log.Printf("[download] 文件 %s (%s): %+v offset=%d length=%d", fileID, file.Name, blob, rng.Offset, rng.Length)
	return &FileReader{
		File:     file,
		Blob:     blob,
		Reader:   reader,
		FileSize: blob.Size,
	}, nil
}

// DirWalker 目录遍历结果，含 manifest 哈希
type DirWalker struct {
	DirID       string
	Entries     []DirEntryInfo
	TreeSHA256  string // X-Tree-SHA256
	TotalSize   int64
}

// DirEntryInfo 目录遍历中的一个文件条目
type DirEntryInfo struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	FileID string `json:"file_id,omitempty"` // 用于直接查询
}

// GetDirManifest 获取目录 manifest 并计算 X-Tree-SHA256
// 在流式打包开始前调用，用于在响应头中返回校验和。
func (s *DownloadService) GetDirManifest(ctx context.Context, dirID, namespace string) (*DirWalker, error) {
	dir, err := s.meta.GetFile(ctx, dirID)
	if err != nil {
		return nil, fmt.Errorf("获取目录: %w", err)
	}
	if dir == nil {
		return nil, ErrNotFound
	}
	if !dir.IsDir {
		return nil, ErrInvalidArgument
	}
	if dir.Namespace != namespace {
		return nil, ErrForbidden
	}

	// 递归遍历子节点
	var entries []DirEntryInfo
	if err := s.walkDir(ctx, dirID, "", &entries); err != nil {
		return nil, fmt.Errorf("遍历目录: %w", err)
	}

	// 排序确保确定性
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	// 计算 X-Tree-SHA256：对 manifest 的规范化 JSON 序列化取哈希
	treeHash := computeTreeSHA256(entries)

	var totalSize int64
	for _, e := range entries {
		totalSize += e.Size
	}

	return &DirWalker{
		DirID:      dirID,
		Entries:    entries,
		TreeSHA256: treeHash,
		TotalSize:  totalSize,
	}, nil
}

// StreamDir 流式打包目录下载
// 返回 io.ReadCloser，读取它即获得打包数据流。
func (s *DownloadService) StreamDir(ctx context.Context, dw *DirWalker, format CompressionFormat) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	go func() {
		archiveWriter, err := s.compress.NewArchiveWriter(ctx, pw, format)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("创建归档器: %w", err))
			return
		}

		for _, entry := range dw.Entries {
			// 优先用 FileID 查文件
			file, err := s.meta.GetFile(ctx, entry.FileID)
			if err != nil || file == nil {
				continue
			}

			blob, err := s.meta.GetBlobBySha(ctx, file.SHA256)
			if err != nil || blob == nil {
				continue
			}

			reader, err := s.storage.Open(ctx, blob.StoragePath, 0, 0)
			if err != nil {
				continue
			}

			if err := archiveWriter.AddFile(ctx, entry.Path, entry.Size, reader); err != nil {
				reader.Close()
				pw.CloseWithError(fmt.Errorf("写入归档条目 %s: %w", entry.Path, err))
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

// walkDir 递归遍历目录树，收集所有子节点路径
func (s *DownloadService) walkDir(ctx context.Context, parentID, prefix string, entries *[]DirEntryInfo) error {
	children, err := s.meta.ListChildren(ctx, parentID)
	if err != nil {
		return err
	}
	for _, child := range children {
		relPath := child.Name
		if prefix != "" {
			relPath = prefix + "/" + child.Name
		}
		if child.IsDir {
			if err := s.walkDir(ctx, child.FileID, relPath, entries); err != nil {
				return err
			}
		} else {
			*entries = append(*entries, DirEntryInfo{
				Path:   relPath,
				Size:   child.Size,
				SHA256: child.SHA256,
				FileID: child.FileID,
			})
		}
	}
	return nil
}

// computeTreeSHA256 计算目录 manifest 的 SHA-256
// 序列化格式：path|size|sha256\n 排序后取哈希
func computeTreeSHA256(entries []DirEntryInfo) string {
	h := sha256.New()
	for _, e := range entries {
		line := fmt.Sprintf("%s|%d|%s\n", e.Path, e.Size, e.SHA256)
		h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ListDir 列目录
func (s *DownloadService) ListDir(ctx context.Context, parentID, namespace string) (*FileMetadata, []*FileMetadata, error) {
	if parentID == "" || parentID == "/" || parentID == "root" {
		// 根目录
		children, err := s.meta.ListRoot(ctx, namespace)
		if err != nil {
			return nil, nil, fmt.Errorf("列根目录: %w", err)
		}
		return nil, children, nil
	}

	dir, err := s.meta.GetFile(ctx, parentID)
	if err != nil {
		return nil, nil, fmt.Errorf("获取目录: %w", err)
	}
	if dir == nil {
		return nil, nil, ErrNotFound
	}
	if dir.Namespace != namespace {
		return nil, nil, ErrForbidden
	}

	children, err := s.meta.ListChildren(ctx, parentID)
	if err != nil {
		return nil, nil, fmt.Errorf("列子节点: %w", err)
	}
	return dir, children, nil
}

// Stat 获取文件/目录元信息
func (s *DownloadService) Stat(ctx context.Context, id, namespace string) (*FileMetadata, *ContentBlob, error) {
	file, err := s.meta.GetFile(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("获取文件: %w", err)
	}
	if file == nil {
		return nil, nil, ErrNotFound
	}
	if file.Namespace != namespace {
		// 不暴露文件存在信息，返回未找到
		return nil, nil, ErrNotFound
	}
	if file.SHA256 == "" || file.IsDir {
		return file, nil, nil
	}

	blob, err := s.meta.GetBlobBySha(ctx, file.SHA256)
	if err != nil {
		return file, nil, nil
	}
	return file, blob, nil
}

// Must be implemented in model.go or utils.go
func init() {
	_ = strings.Compare // prevent unused import
}
