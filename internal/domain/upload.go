package domain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// UploadService 上传编排核心
// 处理上传会话生命周期、秒传预检、分片追加、Finalize 合并+解压+校验、
// content_blobs 去重写入、目录 manifest 提交、删除去重。
type UploadService struct {
	meta interface {
		SessionStore
		BlobStore
		FileStore
	}
	storage     Storage
	tempStorage Storage // 临时分片专用存储（根目录 = TempDir）
	compress    Compressor
	hasher      Hasher
	pool        WorkerPool
	cfg         UploadConfig
	layout      *HierarchicalLayout // 路径布局（ADR-0001 扁平/层级搬移）
}

// UploadConfig 上传服务配置
type UploadConfig struct {
	SessionTTL          time.Duration // 会话无活动超时
	DataDir             string        // 数据目录（data/<namespace>/<fileID>）
	DefaultChunkSize    int64         // 默认分片大小
	NamespaceQuotaBytes int64         // 命名空间逻辑容量上限；0 表示不限制
}

// NewUploadService 创建上传服务
func NewUploadService(meta interface {
	SessionStore
	BlobStore
	FileStore
}, storage Storage, tempStorage Storage, compress Compressor, hasher Hasher, pool WorkerPool, cfg UploadConfig) *UploadService {
	return &UploadService{
		meta:        meta,
		storage:     storage,
		tempStorage: tempStorage,
		compress:    compress,
		hasher:      hasher,
		pool:        pool,
		cfg:         cfg,
		layout:      NewHierarchicalLayout(),
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
		log.Printf("[upload] 秒传未命中 sha256=%s", shaPrefix(sha256))
		return nil, nil
	}

	fileID := NewID()
	reserved, err := s.reserveNamespace(ctx, fileID, namespace, blob.Size)
	if err != nil {
		return nil, err
	}
	if reserved {
		defer func() {
			if err := s.releaseNamespace(ctx, fileID); err != nil {
				log.Printf("[upload] 释放秒传配额预留失败 id=%s: %v", fileID, err)
			}
		}()
	}

	log.Printf("[upload] 秒传命中 sha256=%s (%s)", shaPrefix(sha256), name)
	// 命中：增加引用计数（带回滚）
	rollback, err := IncrWithRollback(ctx, s.meta, sha256)
	if err != nil {
		return nil, fmt.Errorf("增加引用计数: %w", err)
	}

	// 创建逻辑文件记录
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
		// 回滚引用计数（幂等）
		if rbErr := rollback(); rbErr != nil {
			log.Printf("[upload] 回滚引用计数失败 sha=%s: %v", sha256, rbErr)
		}
		return nil, fmt.Errorf("写入文件记录: %w", err)
	}
	return f, nil
}

// CreateSession 创建上传会话
func (s *UploadService) CreateSession(ctx context.Context, sha256 string, length int64, compression CompressionFormat, chunkSize int64, namespace, fileName string) (*UploadSession, error) {
	if length < 0 {
		return nil, ErrInvalidArgument
	}
	if chunkSize <= 0 {
		chunkSize = s.cfg.DefaultChunkSize
	}
	// Never persist a chunk size that the request path cannot safely accept.
	if chunkSize <= 0 || chunkSize > maxUploadChunkSize {
		chunkSize = maxUploadChunkSize
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

	reserved, err := s.reserveNamespace(ctx, sessionID, namespace, length)
	if err != nil {
		return nil, err
	}
	if err := s.meta.CreateSession(ctx, session); err != nil {
		if reserved {
			_ = s.releaseNamespace(ctx, sessionID)
		}
		return nil, fmt.Errorf("创建会话: %w", err)
	}
	log.Printf("[upload] 创建会话 %s: %s %d 字节 (ns=%s)", sessionID, fileName, length, namespace)
	return session, nil
}

const maxUploadChunkSize = 64 << 20 // 64 MiB hard ceiling, protects workers from unbounded requests.

// ensureNamespaceCapacity enforces the configured logical capacity before accepting a
// new upload or a deduplicated logical reference. A zero quota means unlimited.
func (s *UploadService) ensureNamespaceCapacity(ctx context.Context, namespace string, incomingBytes int64) error {
	if s.cfg.NamespaceQuotaBytes <= 0 || incomingBytes <= 0 {
		return nil
	}
	usage, err := s.meta.GetNamespaceUsage(ctx, namespace)
	if err != nil {
		return fmt.Errorf("读取命名空间用量: %w", err)
	}
	if usage != nil && incomingBytes > s.cfg.NamespaceQuotaBytes-usage.TotalSize {
		return ErrQuotaExceeded
	}
	return nil
}

func (s *UploadService) reserveNamespace(ctx context.Context, reservationID, namespace string, bytes int64) (bool, error) {
	if s.cfg.NamespaceQuotaBytes <= 0 || bytes <= 0 {
		return false, nil
	}
	if reservoir, ok := s.meta.(NamespaceQuotaReservoir); ok {
		if err := reservoir.ReserveNamespaceBytes(ctx, namespace, reservationID, bytes, s.cfg.NamespaceQuotaBytes); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, s.ensureNamespaceCapacity(ctx, namespace, bytes)
}

func (s *UploadService) releaseNamespace(ctx context.Context, reservationID string) error {
	if reservoir, ok := s.meta.(NamespaceQuotaReservoir); ok {
		return reservoir.ReleaseNamespaceReservation(ctx, reservationID)
	}
	return nil
}

// AppendChunk 追加一个分片（由 tus/REST handler 调用）。
// 请求体先被限制到会话协商的分片上限，再交由受限 worker 池写入临时存储，
// 这样不会因为伪造的 Content-Length 或超大请求耗尽进程内存。
func (s *UploadService) AppendChunk(ctx context.Context, sessionID string, index int, body io.Reader, declaredSha256 string) error {
	session, err := s.meta.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("获取会话: %w", err)
	}
	if session == nil {
		return ErrSessionNotFound
	}
	return s.appendChunkForSession(ctx, session, index, body, declaredSha256)
}

func (s *UploadService) appendChunkForSession(ctx context.Context, session *UploadSession, index int, body io.Reader, declaredSha256 string) error {
	if session.Status != SessionActive {
		return ErrSessionState
	}
	if index < 0 {
		return ErrInvalidArgument
	}

	// 续期。会话协商的 chunk_size 是主限制；同时保留 64 MiB 绝对上限。
	maxBytes := session.ChunkSize
	if maxBytes <= 0 || maxBytes > maxUploadChunkSize {
		maxBytes = maxUploadChunkSize
	}
	limited := io.LimitReader(body, maxBytes+1)
	chunkData, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("读取分片数据: %w", err)
	}
	if int64(len(chunkData)) > maxBytes {
		return ErrInvalidArgument
	}

	if err := s.meta.TouchSession(ctx, session.SessionID, s.cfg.SessionTTL); err != nil {
		return fmt.Errorf("续期会话: %w", err)
	}

	done := make(chan error, 1)
	task := func() {
		done <- s.processChunkBytes(context.Background(), session.SessionID, session.Namespace, index, chunkData, declaredSha256)
	}
	if err := s.pool.Submit(ctx, task); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processChunkBytes 处理已读入内存的分片数据（在 worker 池中执行）。
func (s *UploadService) processChunkBytes(ctx context.Context, sessionID string, _ string, index int, chunkData []byte, declaredSha256 string) error {
	relPath := sessionID + "/" + fmt.Sprintf("%d.part", index)

	teeReader, acc := s.hasher.TeeReader(bytes.NewReader(chunkData))

	written, err := s.tempStorage.Write(ctx, relPath, teeReader)
	if err != nil {
		return fmt.Errorf("写入分片 %d: %w", index, err)
	}

	actualSha := acc.SumHex()

	if declaredSha256 != "" && actualSha != declaredSha256 {
		_ = s.tempStorage.Delete(ctx, relPath)
		return ErrSliceChecksum
	}

	if err := s.meta.UpdateOffset(ctx, sessionID, index, actualSha, written); err != nil {
		return fmt.Errorf("更新偏移: %w", err)
	}
	log.Printf("[upload] 分片写入 %s/%d: %d 字节 (sha256=%s)", sessionID, index, written, shaPrefix(actualSha))
	return nil
}

// sessionForNamespace 校验上传会话归属，避免只持有 session_id 的请求跨 namespace
// 查询、续传、完成或取消别人的上传。
func (s *UploadService) sessionForNamespace(ctx context.Context, sessionID, namespace string) (*UploadSession, error) {
	session, err := s.meta.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("获取会话: %w", err)
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}
	if session.Namespace != namespace {
		return nil, ErrForbidden
	}
	return session, nil
}

// AppendChunkForNamespace 是 HTTP 层使用的受 namespace 隔离版本。
func (s *UploadService) AppendChunkForNamespace(ctx context.Context, sessionID, namespace string, index int, body io.Reader, declaredSHA256 string) error {
	session, err := s.sessionForNamespace(ctx, sessionID, namespace)
	if err != nil {
		return err
	}
	return s.appendChunkForSession(ctx, session, index, body, declaredSHA256)
}

// GetOffsetForNamespace 返回指定 namespace 中上传会话的偏移量。
func (s *UploadService) GetOffsetForNamespace(ctx context.Context, sessionID, namespace string) (int64, error) {
	if _, err := s.sessionForNamespace(ctx, sessionID, namespace); err != nil {
		return 0, err
	}
	return s.GetOffset(ctx, sessionID)
}

// GetStatusForNamespace 返回指定 namespace 中上传会话的状态。
func (s *UploadService) GetStatusForNamespace(ctx context.Context, sessionID, namespace string) ([]ChunkInfo, int64, error) {
	if _, err := s.sessionForNamespace(ctx, sessionID, namespace); err != nil {
		return nil, 0, err
	}
	return s.GetStatus(ctx, sessionID)
}

// FinalizeForNamespace 完成指定 namespace 中的上传会话。
func (s *UploadService) FinalizeForNamespace(ctx context.Context, sessionID, namespace string) (*FileMetadata, error) {
	if _, err := s.sessionForNamespace(ctx, sessionID, namespace); err != nil {
		return nil, err
	}
	return s.Finalize(ctx, sessionID)
}

// AbortForNamespace 取消指定 namespace 中的上传会话。
func (s *UploadService) AbortForNamespace(ctx context.Context, sessionID, namespace string) error {
	if _, err := s.sessionForNamespace(ctx, sessionID, namespace); err != nil {
		return err
	}
	return s.Abort(ctx, sessionID)
}

// AppendChunkAtOffsetForNamespace 提供 tus 的 offset 语义。当前偏移必须精确匹配，
// 才会把请求体写入下一个分片，避免并发 PATCH 覆盖已有数据。
func (s *UploadService) AppendChunkAtOffsetForNamespace(ctx context.Context, sessionID, namespace string, offset int64, body io.Reader, declaredSHA256 string) (int64, error) {
	session, err := s.sessionForNamespace(ctx, sessionID, namespace)
	if err != nil {
		return 0, err
	}
	if offset < 0 || session.ChunkSize <= 0 || offset%session.ChunkSize != 0 {
		return 0, ErrOffsetConflict
	}
	current, err := s.GetOffset(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if current != offset {
		return current, ErrOffsetConflict
	}
	if err := s.appendChunkForSession(ctx, session, int(offset/session.ChunkSize), body, declaredSHA256); err != nil {
		return 0, err
	}
	return s.GetOffset(ctx, sessionID)
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

// validateUploadedChunks ensures finalize only commits a complete, contiguous byte stream.
// The metadata store can receive retries and out-of-order chunks, so only the expected
// indexes and the final chunk are accepted here.
func validateUploadedChunks(chunks []ChunkInfo, uploadLength, chunkSize int64, enforceLength bool) error {
	if uploadLength < 0 || chunkSize <= 0 {
		return ErrInvalidArgument
	}
	if uploadLength == 0 {
		if len(chunks) != 0 {
			return ErrUploadIncomplete
		}
		return nil
	}
	if len(chunks) == 0 {
		return ErrUploadIncomplete
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Index < chunks[j].Index })
	for index, chunk := range chunks {
		if chunk.Index != index || chunk.Size <= 0 || chunk.Size > chunkSize {
			return ErrUploadIncomplete
		}
	}
	if !enforceLength {
		return nil
	}
	expectedCount := int((uploadLength + chunkSize - 1) / chunkSize)
	if len(chunks) != expectedCount {
		return ErrUploadIncomplete
	}
	for index, chunk := range chunks {
		expectedSize := chunkSize
		if index == expectedCount-1 {
			expectedSize = uploadLength - int64(index)*chunkSize
		}
		if chunk.Size != expectedSize {
			return ErrUploadIncomplete
		}
	}
	return nil
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
//
// 使用三阶段流水线：mergeChunks → verifyStream → commitStream
// 各阶段可独立测试（见 finalize_test.go）。
func (s *UploadService) Finalize(ctx context.Context, sessionID string) (*FileMetadata, error) {
	var session *UploadSession
	if finalizer, ok := s.meta.(SessionFinalizer); ok {
		var claimErr error
		session, claimErr = finalizer.ClaimSessionFinalizing(ctx, sessionID)
		if claimErr != nil {
			return nil, claimErr
		}
	} else {
		var getErr error
		session, getErr = s.meta.GetSession(ctx, sessionID)
		if getErr != nil {
			return nil, fmt.Errorf("获取会话: %w", getErr)
		}
		if session == nil {
			return nil, ErrSessionNotFound
		}
		if session.Status != SessionActive {
			return nil, ErrSessionState
		}
		session.Status = SessionFinalizing
	}

	// 标记 finalizing 后延长 TTL，给合并和写入阶段留出时间。
	_ = s.meta.TouchSession(ctx, sessionID, s.cfg.SessionTTL)
	reservationActive := s.cfg.NamespaceQuotaBytes > 0
	if reservationActive {
		defer func() {
			if err := s.releaseNamespace(ctx, sessionID); err != nil {
				log.Printf("[upload] 释放 finalize 配额预留失败 id=%s: %v", sessionID, err)
			}
		}()
	}

	// 获取已落盘分片列表
	chunks, err := s.meta.ListChunks(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("列举分片: %w", err)
	}

	if err := validateUploadedChunks(chunks, session.UploadLength, session.ChunkSize, session.Compression == CompNone); err != nil {
		return nil, err
	}

	// 0 字节无声明 SHA → 使用空内容标准哈希
	if len(chunks) == 0 && session.SHA256 == "" {
		session.SHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}

	// Phase 1: 合并分片（流式）
	mergedReader, cleanup, err := mergeChunks(ctx, sessionID, chunks, s.tempStorage)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	defer mergedReader.Close()

	// Phase 2: 解压 + 哈希累积
	verifiedReader, hashRes, err := verifyStream(ctx, mergedReader, session.Compression, s.compress, s.hasher)
	if err != nil {
		return nil, err
	}

	// 确定存储路径
	fileName := session.FileName
	if fileName == "" {
		fileName = NewID()
	}
	// 物理路径必须与用户可见名称解耦：同名文件可安全并存，且不会把路径
	// 分隔符、保留设备名等带入底层存储。
	storagePath := s.layout.FlatPath(session.Namespace, sessionID+"-"+safeStorageName(fileName))

	// Phase 3: 写入存储 + 创建去重和文件记录。实际解压大小可能不同于
	// 会话声明长度，因此先把原子配额预留调整到最终大小。
	fileMeta, err := commitStreamWithGuard(ctx, verifiedReader, storagePath, session, s.storage, s.meta, s.meta, hashRes, func(size int64) error {
		_, err := s.reserveNamespace(ctx, sessionID, session.Namespace, size)
		return err
	})
	if err != nil {
		return nil, err
	}

	// 标记会话完成
	session.Status = SessionCompleted
	session.FileID = fileMeta.FileID
	_ = s.meta.DeleteSession(ctx, sessionID)

	log.Printf("[upload] Finalize %s → %s (%d bytes, sha256=%s, ns=%s)",
		sessionID, fileMeta.FileID, fileMeta.Size, shaPrefix(fileMeta.SHA256), session.Namespace)
	return fileMeta, nil
}

// cleanupTempChunks 异步清理临时分片文件
func (s *UploadService) cleanupTempChunks(ctx context.Context, sessionID string, chunks []ChunkInfo) {
	for _, chunk := range chunks {
		relPath := sessionID + "/" + fmt.Sprintf("%d.part", chunk.Index)
		_ = s.tempStorage.Delete(ctx, relPath)
	}
}

// validateDirManifest validates every entry before creating any directory record. It prevents
// malformed paths and cross-namespace file IDs from partially mutating the directory tree.
func (s *UploadService) validateDirManifest(ctx context.Context, manifest DirManifest, namespace string) error {
	const maxDirEntries = 10000
	if len(manifest.Entries) > maxDirEntries {
		return ErrInvalidArgument
	}
	seenPaths := make(map[string]struct{}, len(manifest.Entries))
	seenFiles := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.FileID == "" || !isSafeRelativePath(entry.Path) {
			return ErrInvalidArgument
		}
		if _, ok := seenPaths[entry.Path]; ok {
			return ErrInvalidArgument
		}
		if _, ok := seenFiles[entry.FileID]; ok {
			return ErrInvalidArgument
		}
		seenPaths[entry.Path] = struct{}{}
		seenFiles[entry.FileID] = struct{}{}

		file, err := s.meta.GetFile(ctx, entry.FileID)
		if err != nil {
			return fmt.Errorf("获取目录文件: %w", err)
		}
		if file == nil || file.IsDir {
			return ErrInvalidArgument
		}
		if file.Namespace != namespace {
			return ErrForbidden
		}
	}
	return nil
}

func isSafeRelativePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func safeStorageRelativePath(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = safeStorageName(part)
	}
	return strings.Join(parts, "/")
}

// SubmitDir 提交目录 manifest，建目录树
// 支持嵌套目录结构：entry.Path 中的 "/" 分隔符会被解析为子目录层级。
func (s *UploadService) SubmitDir(ctx context.Context, manifest DirManifest, namespace string) (*FileMetadata, error) {
	if err := s.validateDirManifest(ctx, manifest, namespace); err != nil {
		return nil, err
	}
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

	// 构建目录树映射：路径 -> 目录节点 FileID
	// "" 表示根目录
	dirMap := map[string]string{"": dirID}

	// 收集所有不重复的目录路径
	dirPaths := make(map[string]bool)
	for _, entry := range manifest.Entries {
		for i := 0; i < len(entry.Path); i++ {
			if entry.Path[i] == '/' {
				dirPaths[entry.Path[:i]] = true
			}
		}
	}

	// 排序确保父目录先于子目录创建
	sortedDirs := make([]string, 0, len(dirPaths))
	for d := range dirPaths {
		sortedDirs = append(sortedDirs, d)
	}
	sort.Strings(sortedDirs)

	// 创建子目录节点
	for _, dirPath := range sortedDirs {
		parentPath := ""
		if idx := strings.LastIndex(dirPath, "/"); idx >= 0 {
			parentPath = dirPath[:idx]
		}
		parentID, ok := dirMap[parentPath]
		if !ok {
			parentID = dirID
		}

		subDirID := NewID()
		subDir := &FileMetadata{
			FileID:    subDirID,
			Name:      filepath.Base(dirPath),
			Path:      dirPath,
			Namespace: namespace,
			IsDir:     true,
			ParentID:  parentID,
			CreatedAt: now,
		}
		if err := s.meta.PutFile(ctx, subDir); err != nil {
			return nil, fmt.Errorf("创建子目录 %s: %w", dirPath, err)
		}
		dirMap[dirPath] = subDirID
	}

	// 将已有的文件记录归入目录树（复用 Finalize 创建的记录，不重复创建）
	for _, entry := range manifest.Entries {
		parentFile, err := s.meta.GetFile(ctx, entry.FileID)
		if err != nil || parentFile == nil {
			continue
		}

		parentPath := ""
		if idx := strings.LastIndex(entry.Path, "/"); idx >= 0 {
			parentPath = entry.Path[:idx]
		}
		parentID, ok := dirMap[parentPath]
		if !ok {
			parentID = dirID
		}

		// 更新已有记录的父目录和路径
		_ = s.meta.ReparentFile(ctx, entry.FileID, &parentID, entry.Path)

		// 将物理文件从扁平路径搬到层级路径，方便运维直接拷贝
		if parentFile.SHA256 != "" {
			blob, err := s.meta.GetBlobBySha(ctx, parentFile.SHA256)
			if err == nil && blob != nil {
				hierPath := s.layout.HierarchicalPath(namespace, safeStorageName(dirName), safeStorageRelativePath(entry.Path))
				if blob.StoragePath != hierPath {
					// layout.Move 不再静默吞错（与之前 upload.go:472-479 不同）：
					// 失败会让调用方知晓，留下脏文件供 reaper 处理。
					if moveErr := s.layout.Move(ctx, s.storage, blob.StoragePath, hierPath); moveErr != nil {
						log.Printf("[upload] 层级搬移失败 sha=%s: %v", parentFile.SHA256, moveErr)
						continue
					}
					if err := s.meta.UpdateBlobStorage(ctx, parentFile.SHA256, hierPath); err != nil {
						log.Printf("[upload] 更新 blob storage_path 失败 sha=%s: %v", parentFile.SHA256, err)
					}
				}
			}
		}
	}

	log.Printf("[upload] 目录创建 %s (%s): %d 个子文件, %d 个子目录", root.FileID, dirName, len(manifest.Entries), len(dirPaths))
	return root, nil
}

// DeleteFile 删除一个逻辑文件（不处理目录）
// 实现 FileDeleter 接口。由 Delete 和 BatchService 调用。
func (s *UploadService) DeleteFile(ctx context.Context, fileID, namespace string) error {
	file, err := s.meta.GetFile(ctx, fileID)
	if err != nil {
		return fmt.Errorf("获取文件: %w", err)
	}
	if file == nil {
		return ErrNotFound
	}
	if file.Namespace != namespace {
		return ErrForbidden
	}
	return s.deleteFile(ctx, file)
}

// DeleteDir 删除一个目录及其子节点（递归）
// recursive=true 时递归删除所有子节点。
// 实现 FileDeleter 接口。
func (s *UploadService) DeleteDir(ctx context.Context, dirID string, recursive bool, namespace string) error {
	dir, err := s.meta.GetFile(ctx, dirID)
	if err != nil {
		return fmt.Errorf("获取目录: %w", err)
	}
	if dir == nil {
		return ErrNotFound
	}
	if dir.Namespace != namespace {
		return ErrForbidden
	}
	if !dir.IsDir {
		return ErrInvalidArgument
	}
	return s.deleteDir(ctx, dir, namespace)
}

// MoveFile 将单个文件移动到目标目录
// 实现 FileMover 接口。
// Rename 重命名文件/目录
// 更新 name 和 path，保持 namespace 不变
func (s *UploadService) Rename(ctx context.Context, fileID, newName, newPath, namespace string) error {
	file, err := s.meta.GetFile(ctx, fileID)
	if err != nil {
		return fmt.Errorf("获取文件: %w", err)
	}
	if file == nil {
		return ErrNotFound
	}
	if file.Namespace != namespace {
		return ErrForbidden
	}
	return s.meta.RenameFile(ctx, fileID, newName, newPath)
}

func (s *UploadService) MoveFile(ctx context.Context, fileID, targetDirID, namespace string) error {
	file, err := s.meta.GetFile(ctx, fileID)
	if err != nil {
		return err
	}
	if file == nil {
		return ErrNotFound
	}
	if file.Namespace != namespace {
		return ErrForbidden
	}

	var parentID *string
	if targetDirID != "" {
		if targetDirID == fileID {
			return ErrInvalidArgument
		}
		target, err := s.meta.GetFile(ctx, targetDirID)
		if err != nil {
			return err
		}
		if target == nil {
			return ErrNotFound
		}
		if target.Namespace != namespace {
			return ErrForbidden
		}
		if !target.IsDir || target.DeletedAt != nil {
			return ErrInvalidArgument
		}

		// 目录不能移动到自己的后代，否则会形成不可遍历的环。
		seen := map[string]bool{}
		current := target
		for current != nil && current.FileID != "" && !seen[current.FileID] {
			seen[current.FileID] = true
			if current.FileID == fileID {
				return ErrInvalidArgument
			}
			if current.ParentID == "" {
				break
			}
			current, err = s.meta.GetFile(ctx, current.ParentID)
			if err != nil {
				return err
			}
		}
		parentID = &targetDirID
	}
	return s.meta.UpdateFileParent(ctx, fileID, parentID)
}

// Delete 删除逻辑文件/目录（目录递归）
// 便捷入口，由 HTTP handler 调用。
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
	log.Printf("[upload] 删除文件 %s (%s)", file.FileID, file.Name)
	return nil
}

func (s *UploadService) deleteDir(ctx context.Context, dir *FileMetadata, namespace string) error {
	children, err := s.meta.ListChildren(ctx, dir.FileID, "")
	if err != nil {
		return fmt.Errorf("列举子节点: %w", err)
	}

	// undo 操作栈：失败时回滚
	// op.kind=deleteFile 时，op.file 保存原始记录用于 PutFile 恢复
	// op.kind=decrRef 时，op.sha 用于 IncrBlobRef 撤销引用计数递减
	var undo []undoOp
	doUndo := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			op := undo[i]
			switch op.kind {
			case undoDeleteFile:
				if op.file != nil {
					if rbErr := s.meta.PutFile(ctx, op.file); rbErr != nil {
						log.Printf("[upload] 回滚 PutFile 失败 id=%s: %v", op.file.FileID, rbErr)
					}
				}
			case undoDecrRef:
				if rbErr := s.meta.IncrBlobRef(ctx, op.sha); rbErr != nil {
					log.Printf("[upload] 回滚 IncrBlobRef 失败 sha=%s: %v", op.sha, rbErr)
				}
			}
		}
	}

	for _, child := range children {
		var childErr error
		if child.IsDir {
			childErr = s.deleteDir(ctx, child, namespace)
		} else {
			childErr = s.deleteFileWithUndo(ctx, child, &undo)
		}
		if childErr != nil {
			doUndo()
			return childErr
		}
	}

	// 删除目录自身
	if err := s.meta.DeleteFile(ctx, dir.FileID); err != nil {
		doUndo()
		return err
	}
	return nil
}

type undoKind int

const (
	undoDeleteFile undoKind = iota
	undoDecrRef
)

type undoOp struct {
	kind undoKind
	file *FileMetadata // for undoDeleteFile
	sha  string        // for undoDecrRef
}

// deleteFileWithUndo 与 deleteFile 行为一致，但把每一步登记到 undo 栈
// 用于 deleteDir 的事务式回滚。
func (s *UploadService) deleteFileWithUndo(ctx context.Context, file *FileMetadata, undo *[]undoOp) error {
	// 删文件记录
	if err := s.meta.DeleteFile(ctx, file.FileID); err != nil {
		return fmt.Errorf("删除文件记录: %w", err)
	}
	*undo = append(*undo, undoOp{kind: undoDeleteFile, file: file.Clone()})

	// 减少引用计数
	if file.SHA256 != "" {
		newCount, err := s.meta.DecrBlobRef(ctx, file.SHA256)
		if err != nil {
			return fmt.Errorf("减少引用: %w", err)
		}
		*undo = append(*undo, undoOp{kind: undoDecrRef, sha: file.SHA256})

		if newCount <= 0 {
			blob, err := s.meta.GetBlobBySha(ctx, file.SHA256)
			if err == nil && blob != nil {
				_ = s.storage.Delete(ctx, blob.StoragePath)
				// 物理文件删除不回滚 — ref_count 恢复后，下次 DecrBlobRef <=0 时会再次触发
				// 这是已知的弱保证：物理文件可能被"提前"删除，rollback 只能恢复 DB 状态
			}
		}
	}
	log.Printf("[upload] 删除文件 %s (%s)", file.FileID, file.Name)
	return nil
}

// deleteDirDeferred 删除元数据和引用后再统一清理物理对象。数据库阶段失败时，
// 只回滚数据库，不会留下“引用已恢复但物理内容已被提前删除”的数据损坏。
func (s *UploadService) deleteDirDeferred(ctx context.Context, dir *FileMetadata, namespace string) error {
	var undo []undoOp
	var pending []string
	if err := s.deleteDirDeferredWithUndo(ctx, dir, namespace, &undo, &pending); err != nil {
		s.rollbackDeleteUndo(ctx, undo)
		return err
	}
	for _, storagePath := range pending {
		if err := s.storage.Delete(ctx, storagePath); err != nil {
			log.Printf("[upload] 延迟删除物理内容失败 path=%s: %v", storagePath, err)
		}
	}
	return nil
}

func (s *UploadService) deleteDirDeferredWithUndo(ctx context.Context, dir *FileMetadata, namespace string, undo *[]undoOp, pending *[]string) error {
	if dir.Namespace != namespace {
		return ErrForbidden
	}
	children, err := s.meta.ListChildren(ctx, dir.FileID, "")
	if err != nil {
		return fmt.Errorf("列举子节点: %w", err)
	}
	for _, child := range children {
		if child.IsDir {
			if err := s.deleteDirDeferredWithUndo(ctx, child, namespace, undo, pending); err != nil {
				return err
			}
		} else if err := s.deleteFileWithUndoDeferred(ctx, child, undo, pending); err != nil {
			return err
		}
	}
	if err := s.meta.DeleteFile(ctx, dir.FileID); err != nil {
		return err
	}
	*undo = append(*undo, undoOp{kind: undoDeleteFile, file: dir.Clone()})
	return nil
}

func (s *UploadService) deleteFileWithUndoDeferred(ctx context.Context, file *FileMetadata, undo *[]undoOp, pending *[]string) error {
	if err := s.meta.DeleteFile(ctx, file.FileID); err != nil {
		return fmt.Errorf("删除文件记录: %w", err)
	}
	*undo = append(*undo, undoOp{kind: undoDeleteFile, file: file.Clone()})
	if file.SHA256 == "" {
		return nil
	}
	newCount, err := s.meta.DecrBlobRef(ctx, file.SHA256)
	if err != nil {
		return fmt.Errorf("减少引用: %w", err)
	}
	*undo = append(*undo, undoOp{kind: undoDecrRef, sha: file.SHA256})
	if newCount <= 0 {
		blob, err := s.meta.GetBlobBySha(ctx, file.SHA256)
		if err == nil && blob != nil && blob.StoragePath != "" {
			*pending = append(*pending, blob.StoragePath)
		}
	}
	return nil
}

func (s *UploadService) rollbackDeleteUndo(ctx context.Context, undo []undoOp) {
	for i := len(undo) - 1; i >= 0; i-- {
		op := undo[i]
		switch op.kind {
		case undoDeleteFile:
			if op.file != nil {
				if err := s.meta.PutFile(ctx, op.file); err != nil {
					log.Printf("[upload] 回滚 PutFile 失败 id=%s: %v", op.file.FileID, err)
				}
			}
		case undoDecrRef:
			if err := s.meta.IncrBlobRef(ctx, op.sha); err != nil {
				log.Printf("[upload] 回滚 IncrBlobRef 失败 sha=%s: %v", op.sha, err)
			}
		}
	}
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
	log.Printf("[upload] 取消上传 %s (%s)", sessionID, session.FileName)
	if err := s.meta.DeleteSession(ctx, sessionID); err != nil {
		return err
	}
	if err := s.releaseNamespace(ctx, sessionID); err != nil {
		return fmt.Errorf("释放配额预留: %w", err)
	}
	return nil
}

// shaPrefix 安全截取 SHA-256 前 12 位用于日志，不足时显示全部。
func shaPrefix(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
