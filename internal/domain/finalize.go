package domain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sort"
	"time"
)

// ============================================================
// Finalize Pipeline — 三阶段分解
//
// 1. mergeChunks:  合并分片 → 数据流
// 2. verifyStream: 解压(如有) + 哈希累积 → verified reader
// 3. commitStream: 写入存储 + 创建去重记录 + 文件记录
// ============================================================

// mergeChunks 合并所有分片为一个 io.ReadCloser（流式，不写中间文件）。
// chunks 按 Index 排序后按序拼接。
// 返回的 cleanup 函数清理临时分片文件，调用者最终必须调用它。
func mergeChunks(ctx context.Context, sessionID string, chunks []ChunkInfo, tempStorage Storage) (io.ReadCloser, func(), error) {
	// 空分片 → 空 reader
	if len(chunks) == 0 {
		noop := func() {}
		return io.NopCloser(bytes.NewReader(nil)), noop, nil
	}

	// 按 index 排序
	sorted := append([]ChunkInfo{}, chunks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Index < sorted[j].Index })

	pr, pw := io.Pipe()

	go func() {
		for _, chunk := range sorted {
			relPath := sessionID + "/" + fmt.Sprintf("%d.part", chunk.Index)
			r, err := tempStorage.Open(ctx, relPath, 0, 0)
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

	cleanup := func() {
		for _, chunk := range sorted {
			relPath := sessionID + "/" + fmt.Sprintf("%d.part", chunk.Index)
			_ = tempStorage.Delete(ctx, relPath)
		}
	}

	return pr, cleanup, nil
}

// verifyResult 用于在 reader 消耗完毕后获取哈希和大小。
// 在 verifyStream 返回后，调用者消耗 verified reader，
// 消耗完毕后调用 verifyResult() 获取实际 SHA-256 和字节数。

// verifyStream 设置解压（如有）和哈希累积。
// 返回 verified reader 和 hashResult 函数。
// 调用者消耗完 verified reader 后，通过 hashResult() 获取 (SHA256, size)。
func verifyStream(ctx context.Context, r io.Reader, compression CompressionFormat, compress Compressor, hasher Hasher) (io.Reader, func() (string, int64), error) {
	// 1. 解压（如果需要）
	var originalReader io.Reader = r
	if compression != CompNone && compression != "" {
		decompressed, err := compress.Decompress(ctx, r, compression)
		if err != nil {
			return nil, nil, fmt.Errorf("解压失败: %w", err)
		}
		originalReader = decompressed
	}

	// 2. 哈希累积 — 使用 hasher.TeeReader
	tee, acc := hasher.TeeReader(originalReader)

	hashRes := func() (string, int64) {
		return acc.SumHex(), acc.N()
	}

	return tee, hashRes, nil
}

// commitStream 写入最终存储、创建 content_blob 和 file 记录。
// hashRes 来自 verifyStream，在 reader 被消耗后（storage.Write 完成后）可获取实际哈希和大小。
// 验证写入后的实际哈希是否匹配声明的 SHA-256，不匹配则回滚。
func commitStream(ctx context.Context, r io.Reader, storagePath string, session *UploadSession, storage Storage, blobStore BlobStore, fileStore FileStore, hashRes func() (string, int64)) (*FileMetadata, error) {
	// 1. 写入最终存储（同时消耗 reader）
	written, err := storage.Write(ctx, storagePath, r)
	if err != nil {
		return nil, fmt.Errorf("写入存储: %w", err)
	}

	// 2. 获取实际哈希和大小
	actualSha, actualN := hashRes()

	// 3. 比对声明的 SHA-256
	if session.SHA256 != "" && actualSha != session.SHA256 {
		_ = storage.Delete(ctx, storagePath)
		return nil, ErrContentChecksum
	}

	// 取较大值：实际读取字节数和 storage.Write 返回值应一致
	size := written
	if actualN > size {
		size = actualN
	}

	fileID := NewID()
	now := time.Now()
	fileName := session.FileName
	if fileName == "" {
		fileName = fileID
	}

	// 4. 创建 content_blob
	blob := &ContentBlob{
		SHA256:      actualSha,
		StoragePath: storagePath,
		Size:        size,
		RefCount:    1,
		CreatedAt:   now,
	}
	if err := blobStore.PutBlob(ctx, blob); err != nil {
		_ = storage.Delete(ctx, storagePath)
		return nil, fmt.Errorf("写入去重记录: %w", err)
	}

	// 5. 创建逻辑文件记录
	fileMeta := &FileMetadata{
		FileID:    fileID,
		SHA256:    actualSha,
		Name:      filepath.Base(fileName),
		Path:      fileName,
		Size:      size,
		Namespace: session.Namespace,
		IsDir:     false,
		CreatedAt: now,
	}
	if err := fileStore.PutFile(ctx, fileMeta); err != nil {
		_, _ = blobStore.DecrBlobRef(ctx, actualSha)
		return nil, fmt.Errorf("写入文件记录: %w", err)
	}

	log.Printf("[upload] 提交 %s → %s (%d bytes, sha256=%s, ns=%s)",
		session.SessionID, fileID, size, shaPrefix(actualSha), session.Namespace)
	return fileMeta, nil
}
