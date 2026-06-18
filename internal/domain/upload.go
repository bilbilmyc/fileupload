package domain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
)
// UploadService 上传编排核心
// 处理上传会话生命周期、秒传预检、分片追加、Finalize 合并+解压+校验、
// content_blobs 去重写入、目录 manifest 提交、删除去重。
type UploadService struct {
	meta    Metadata
	storage Storage
	compress Compressor
	hasher  Hasher
	pool    WorkerPool
	cfg     UploadConfig
}

// UploadConfig 上传服务配置
type UploadConfig struct {
	SessionTTL      time.Duration // 会话无活动超时
	DataDir         string        // 数据目录（data/<namespace>/<fileID>）
	TempDir         string        // 临时分片目录
	DefaultChunkSize int64        // 默认分片大小
}

// NewUploadService 创建上传服务
func NewUploadService(meta Metadata, storage Storage, compress Compressor, hasher Hasher, pool WorkerPool, cfg UploadConfig) *UploadService {
	return &UploadService{
		meta:    meta,
		storage: storage,
		compress: compress,
		hasher:  hasher,
		pool:    pool,
		cfg:     cfg,
	}
}

// CheckExists 秒传预检：按原始内容 SHA-256 查重
// 命中 → ref_count+1，建逻辑文件，返回 fileID
// 未命中 → 返回 nil
func (s *UploadService) CheckExists(ctx context.Context, sha256, namespace, name string) (*FileMetadata, error) {
	if sha256 == "" {
		return nil, ErrInvalidArgument
	}

	blob, err := s.meta.GetBlobBySha(ctx, sha256)
	if err != nil {
		return nil, fmt.Errorf("查询秒传: %w", err)
	}
	if blob == nil {
		return nil, nil // 未命中
	}

	// 命中：增加引用计数
	if err := s.meta.IncrBlobRef(ctx, sha256); err != nil {
		return nil, fmt.Errorf("增加引用计数: %w", err)
	}

	// 创建逻辑文件记录
	fileID := NewID()
	now := time.Now()
	f := &FileMetadata{
		FileID:    fileID,
		SHA256:    sha256,
		Name:      name,
		Path:      name,
		Size:      blob.Size,
		Namespace: namespace,
		IsDir:     false,
		CreatedAt: now,
	}
	if err := s.meta.PutFile(ctx, f); err != nil {
		// 回滚引用计数
		_, _ = s.meta.DecrBlobRef(ctx, sha256)
		return nil, fmt.Errorf("写入文件记录: %w", err)
	}
	return f, nil
}

// CreateSession 创建上传会话
func (s *UploadService) CreateSession(ctx context.Context, sha256 string, length int64, compression CompressionFormat, chunkSize int64, namespace, fileName string) (*UploadSession, error) {
	if length <= 0 {
		return nil, ErrInvalidArgument
	}
	if chunkSize <= 0 {
		chunkSize = s.cfg.DefaultChunkSize
	}

	sessionID := NewID()
	now := time.Now()
	session := &UploadSession{
		SessionID:    sessionID,
		SHA256:       sha256,
		UploadLength: length,
		Compression:  compression,
		ChunkSize:    chunkSize,
		Namespace:    namespace,
		FileName:     fileName,
		CreatedAt:    now,
		ExpireAt:     now.Add(s.cfg.SessionTTL),
		Status:       SessionActive,
	}

	if err := s.meta.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("创建会话: %w", err)
	}
	return session, nil
}

// AppendChunk 追加一个分片（由 tus/REST handler 调用）
// body 在当前 goroutine 中完全读进内存后，提交到 worker 池异步处理。
// worker 池使用 background context，不受 HTTP handler 取消影响。
// 调用方通过 done channel 等待处理完成。
func (s *UploadService) AppendChunk(ctx context.Context, sessionID string, index int, body io.Reader, declaredSha256 string) error {
	// 校验会话存在且状态正确
	session, err := s.meta.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("获取会话: %w", err)
	}
	if session == nil {
		return ErrSessionNotFound
	}
	if session.Status != SessionActive {
		return ErrSessionState
	}

	// 续期
	_ = s.meta.TouchSession(ctx, sessionID, s.cfg.SessionTTL)

	// 在当前 goroutine 中先把 body 完全读进内存
	chunkData, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("读取分片数据: %w", err)
	}

	// 提交到 worker 池异步处理（使用 background context 避免 HTTP handler 取消的影响）
	done := make(chan error, 1)
	task := func() {
		// 使用 background context，防止 HTTP handler 返回后 ctx 取消导致操作失败
		bgCtx := context.Background()
		done <- s.processChunkBytes(bgCtx, sessionID, session.Namespace, index, chunkData, declaredSha256)
	}

	if err := s.pool.Submit(ctx, task); err != nil {
		return err
	}

	// 等待异步处理完成
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processChunkBytes 处理已读入内存的分片数据（在 worker 池中执行）。
func (s *UploadService) processChunkBytes(ctx context.Context, sessionID string, _ string, index int, chunkData []byte, declaredSha256 string) error {
	partPath := filepath.Join(s.cfg.TempDir, sessionID, fmt.Sprintf("%d.part", index))

	teeReader, acc := s.hasher.TeeReader(bytes.NewReader(chunkData))

	written, err := s.storage.Write(ctx, partPath, teeReader)
	if err != nil {
		return fmt.Errorf("写入分片 %d: %w", index, err)
	}

	actualSha := acc.SumHex()

	if declaredSha256 != "" && actualSha != declaredSha256 {
		_ = s.storage.Delete(ctx, partPath)
		return ErrSliceChecksum
	}

	if err := s.meta.UpdateOffset(ctx, sessionID, index, actualSha, written); err != nil {
		return fmt.Errorf("更新偏移: %w", err)
	}
	return nil
}

// GetOffset 获取当前已接收字节偏移（断点续传用）
func (s *UploadService) GetOffset(ctx context.Context, sessionID string) (int64, error) {
	session, err := s.meta.GetSession(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("获取会话: %w", err)
	}
	if session == nil {
		return 0, ErrSessionNotFound
	}

	chunks, err := s.meta.ListChunks(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, c := range chunks {
		total += c.Size
	}
	return total, nil
}

// GetStatus 获取上传进度
func (s *UploadService) GetStatus(ctx context.Context, sessionID string) ([]ChunkInfo, int64, error) {
	session, err := s.meta.GetSession(ctx, sessionID)
	if err != nil {
		return nil, 0, fmt.Errorf("获取会话: %w", err)
	}
	if session == nil {
		return nil, 0, ErrSessionNotFound
	}
	chunks, err := s.meta.ListChunks(ctx, sessionID)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	for _, c := range chunks {
		total += c.Size
	}
	return chunks, total, nil
}


// safeStorageName 将用户提供的文件名转为安全的存储路径名。
// 保留原始文件名不变，仅处理会导致文件系统问题的边缘情况。
func safeStorageName(name string) string {
	// 1. 去路径（防穿越）
	base := filepath.Base(name)

	// 2. 去 null 字节
	base = strings.ReplaceAll(base, "\x00", "")

	// 3. 处理空/点号名
	if base == "" || base == "." || base == ".." {
		return "file"
	}

	// 4. 去除尾部的点和空格（Windows 会静默去掉，导致路径不匹配）
	base = strings.TrimRight(base, ". ")
	if base == "" {
		return "file"
	}

	// 5. Windows 保留字符替换为下划线
	windowsReserved := []string{"\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, ch := range windowsReserved {
		base = strings.ReplaceAll(base, ch, "_")
	}

	// 6. Windows 保留设备名（不区分大小写）
	upper := strings.ToUpper(base)
	reservedNames := []string{"CON", "PRN", "AUX", "NUL", "CLOCK$",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
	for _, rn := range reservedNames {
		if upper == rn {
			base = "_" + base
			break
		}
	}

	// 7. 截断过长文件名（ext4/ NTFS 限制 255 字节）ext4 单组件上限 255 字节（非字符）
	if len([]byte(base)) > 200 {
		ext := ""
		if idx := strings.LastIndex(base, "."); idx > 0 {
			ext = base[idx:]
			if len([]byte(ext)) > 20 {
				ext = ext[:20]
			}
		}
		maxBaseLen := 200 - len([]byte(ext))
		if maxBaseLen < 10 {
			maxBaseLen = 10
		}
		base = base[:maxBaseLen] + ext
	}

	return base
}
// Finalize 完成上传：合并分片 → 解压 → 整体 SHA-256 校验 → 写入 Storage
func (s *UploadService) Finalize(ctx context.Context, sessionID string) (*FileMetadata, error) {
	session, err := s.meta.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("获取会话: %w", err)
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}
	if session.Status != SessionActive {
		return nil, ErrSessionState
	}

	// 标记 finalizing
	session.Status = SessionFinalizing
	_ = s.meta.TouchSession(ctx, sessionID, s.cfg.SessionTTL)

	// 获取已落盘分片列表
	chunks, err := s.meta.ListChunks(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("列举分片: %w", err)
	}
	if len(chunks) == 0 {
		return nil, ErrInvalidArgument
	}

	// 按 index 排序
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Index < chunks[j].Index })

	// 流式合并所有分片到一个 pipe
	pr, pw := io.Pipe()
	go func() {
		for _, chunk := range chunks {
			partPath := filepath.Join(s.cfg.TempDir, sessionID, fmt.Sprintf("%d.part", chunk.Index))
			r, err := s.storage.Open(ctx, partPath, 0, 0)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("打开分片 %d: %w", chunk.Index, err))
				return
			}
			_, err = io.Copy(pw, r)
			r.Close()
			if err != nil {
				pw.CloseWithError(fmt.Errorf("合并分片 %d: %w", chunk.Index, err))
				return
			}
		}
		pw.Close()
	}()

	// 是否需要解压
	var originalReader io.Reader = pr
	if session.Compression != CompNone && session.Compression != "" {
		decompressed, err := s.compress.Decompress(ctx, pr, session.Compression)
		if err != nil {
			return nil, fmt.Errorf("解压失败: %w", err)
		}
		defer decompressed.Close()
		originalReader = decompressed
	}

	// 边读边算整体 SHA-256 + 写入 Storage
	fileID := NewID()
	fileName := session.FileName
	if fileName == "" {
		fileName = fileID
	}
	// 存储路径用 namespace/original_path，保留原始文件名和目录结构
	// 方便直接从存储目录拷贝文件。LocalFS 自带路径穿越防护。
	storagePath := fmt.Sprintf("%s/%s", session.Namespace, fileName)
	teeReader, acc := s.hasher.TeeReader(originalReader)

	written, err := s.storage.Write(ctx, storagePath, teeReader)
	if err != nil {
		return nil, fmt.Errorf("写入存储: %w", err)
	}

	actualSha := acc.SumHex()

	// 比对整体 SHA-256
	if session.SHA256 != "" && actualSha != session.SHA256 {
		// 校验失败：删除已写数据
		_ = s.storage.Delete(ctx, storagePath)
		return nil, ErrContentChecksum
	}

	// 创建 content_blob
	blob := &ContentBlob{
		SHA256:      actualSha,
		StoragePath: storagePath,
		Size:        written,
		RefCount:    1,
		CreatedAt:   time.Now(),
	}
	if err := s.meta.PutBlob(ctx, blob); err != nil {
		_ = s.storage.Delete(ctx, storagePath)
		return nil, fmt.Errorf("写入去重记录: %w", err)
	}

		// 创建逻辑文件记录
		// Name=基本名（展示用），Path=原始路径（目录上传时含相对路径）
		fileMeta := &FileMetadata{
			FileID:    fileID,
			SHA256:    actualSha,
			Name:      filepath.Base(fileName),
			Path:      fileName,
			Size:      written,
			Namespace: session.Namespace,
			IsDir:     false,
			CreatedAt: time.Now(),
	}
	if err := s.meta.PutFile(ctx, fileMeta); err != nil {
		_, _ = s.meta.DecrBlobRef(ctx, actualSha)
		return nil, fmt.Errorf("写入文件记录: %w", err)
	}

	// 清理临时分片
	go s.cleanupTempChunks(ctx, sessionID, chunks)

	// 标记会话完成
	session.Status = SessionCompleted
	session.FileID = fileID
	_ = s.meta.DeleteSession(ctx, sessionID)

	return fileMeta, nil
}

// cleanupTempChunks 异步清理临时分片文件
func (s *UploadService) cleanupTempChunks(ctx context.Context, sessionID string, chunks []ChunkInfo) {
	for _, chunk := range chunks {
		partPath := filepath.Join(s.cfg.TempDir, sessionID, fmt.Sprintf("%d.part", chunk.Index))
		_ = s.storage.Delete(ctx, partPath)
	}
}

// SubmitDir 提交目录 manifest，建目录树
func (s *UploadService) SubmitDir(ctx context.Context, manifest DirManifest, namespace string) (*FileMetadata, error) {
	dirID := NewID()
	now := time.Now()

	// 创建根目录节点，保留上传时的原始目录名
	dirName := manifest.Name
	if dirName == "" {
		dirName = fmt.Sprintf("dir_%s", dirID[:8])
	}
	root := &FileMetadata{
		FileID:    dirID,
		Name:      dirName,
		Path:      "/",
		Namespace: namespace,
		IsDir:     true,
		CreatedAt: now,
	}
	if err := s.meta.PutFile(ctx, root); err != nil {
		return nil, fmt.Errorf("创建目录节点: %w", err)
	}

	// 创建子节点
	for _, entry := range manifest.Entries {
		childID := NewID()
		// 查原文件信息
		parentFile, err := s.meta.GetFile(ctx, entry.FileID)
		if err != nil || parentFile == nil {
			continue
		}
		child := &FileMetadata{
			FileID:    childID,
			SHA256:    parentFile.SHA256,
			Name:      filepath.Base(entry.Path),
			Path:      entry.Path,
			Size:      parentFile.Size,
			Namespace: namespace,
			IsDir:     false,
			ParentID:  dirID,
			CreatedAt: now,
		}
		_ = s.meta.PutFile(ctx, child)
	}

	return root, nil
}

// Delete 删除逻辑文件/目录（目录递归）
func (s *UploadService) Delete(ctx context.Context, id string, namespace string) error {
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

	if file.IsDir {
		return s.deleteDir(ctx, file, namespace)
	}
	return s.deleteFile(ctx, file)
}

func (s *UploadService) deleteFile(ctx context.Context, file *FileMetadata) error {
	// 删除文件记录
	if err := s.meta.DeleteFile(ctx, file.FileID); err != nil {
		return fmt.Errorf("删除文件记录: %w", err)
	}

	// 减少引用计数
	if file.SHA256 != "" {
		newCount, err := s.meta.DecrBlobRef(ctx, file.SHA256)
		if err != nil {
			return fmt.Errorf("减少引用: %w", err)
		}
		if newCount <= 0 {
			// 无引用，删除物理文件
			blob, err := s.meta.GetBlobBySha(ctx, file.SHA256)
			if err == nil && blob != nil {
				_ = s.storage.Delete(ctx, blob.StoragePath)
			}
		}
	}
	return nil
}

func (s *UploadService) deleteDir(ctx context.Context, dir *FileMetadata, namespace string) error {
	children, err := s.meta.ListChildren(ctx, dir.FileID)
	if err != nil {
		return fmt.Errorf("列举子节点: %w", err)
	}
	for _, child := range children {
		if child.IsDir {
			if err := s.deleteDir(ctx, child, namespace); err != nil {
				return err
			}
		} else {
			if err := s.deleteFile(ctx, child); err != nil {
				return err
			}
		}
	}
	// 删除目录自身
	return s.meta.DeleteFile(ctx, dir.FileID)
}

// Abort 取消上传会话
func (s *UploadService) Abort(ctx context.Context, sessionID string) error {
	session, err := s.meta.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("获取会话: %w", err)
	}
	if session == nil {
		return ErrSessionNotFound
	}

	// 清理临时分片
	chunks, _ := s.meta.ListChunks(ctx, sessionID)
	s.cleanupTempChunks(ctx, sessionID, chunks)

	// 删除会话
	return s.meta.DeleteSession(ctx, sessionID)
}
