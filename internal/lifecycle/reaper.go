// Package lifecycle 管理后台生命周期任务
// SessionReaper：定时清理过期上传会话
// ConsistencyScanner：定时/手动巡检存储与元数据一致性
package lifecycle

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mayc/casdao/fileupload/internal/domain"
)

// SessionReaper 上传会话超时清理器
// 定时扫描 Redis 中的过期/aborted 会话，清理临时分片和元数据。
type SessionReaper struct {
	meta    domain.Metadata
	storage domain.Storage
	tempDir string
	interval time.Duration
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewSessionReaper 创建会话清理器
func NewSessionReaper(meta domain.Metadata, storage domain.Storage, tempDir string, interval time.Duration) *SessionReaper {
	if interval <= 0 {
		interval = time.Minute
	}
	return &SessionReaper{
		meta:     meta,
		storage:  storage,
		tempDir:  tempDir,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start 启动后台定时扫描
func (r *SessionReaper) Start() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.reap(context.Background())
			case <-r.stopCh:
				return
			}
		}
	}()
	log.Printf("[reaper] 会话清理已启动（间隔 %s）", r.interval)
}

// Stop 停止清理器
func (r *SessionReaper) Stop() {
	close(r.stopCh)
	r.wg.Wait()
	log.Printf("[reaper] 会话清理已停止")
}

// reap 执行一次清理
func (r *SessionReaper) reap(ctx context.Context) {
	if r.meta == nil {
		return
	}
	expired, err := r.meta.ListExpiredSessions(ctx)
	if err != nil {
		log.Printf("[reaper] 扫描过期会话失败: %v", err)
		return
	}

	for _, sessionID := range expired {
		r.cleanupSession(ctx, sessionID)
	}

	// 额外：清理临时目录中的孤儿临时文件
	r.cleanupOrphanParts(ctx)
}

// cleanupSession 清理单个过期会话
func (r *SessionReaper) cleanupSession(ctx context.Context, sessionID string) {
	// 清理临时分片目录
	sessionDir := filepath.Join(r.tempDir, sessionID)
	entries, err := os.ReadDir(sessionDir)
	if err == nil {
		for _, entry := range entries {
			partPath := filepath.Join(sessionDir, entry.Name())
			os.Remove(partPath)
		}
		os.Remove(sessionDir)
	}

	// 删除 Redis 中的会话数据
	_ = r.meta.DeleteSession(ctx, sessionID)

	log.Printf("[reaper] 清理过期会话: %s", sessionID)
}

// cleanupOrphanParts 清理临时目录中无对应 Redis 会话的孤儿分片
func (r *SessionReaper) cleanupOrphanParts(ctx context.Context) {
	entries, err := os.ReadDir(r.tempDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()

		// 检查 Redis 中是否还有该会话
		session, err := r.meta.GetSession(ctx, sessionID)
		if err != nil || session != nil {
			continue
		}

		// 孤儿目录，清理
		sessionDir := filepath.Join(r.tempDir, sessionID)
		os.RemoveAll(sessionDir)
		log.Printf("[reaper] 清理孤儿临时目录: %s", sessionID)
	}
}

// ========== ConsistencyScanner ==========

// ScannerReport 巡检报告
type ScannerReport struct {
	OrphanParts    int      `json:"orphan_parts"`
	OrphanFiles    []string `json:"orphan_files"`
	MetadataOrphans int    `json:"metadata_orphans"`
	RefCountFixes  int      `json:"ref_count_fixes"`
	CorruptedFiles []string `json:"corrupted_files"`
}

// ConsistencyScanner 一致性巡检器
type ConsistencyScanner struct {
	meta    domain.Metadata
	storage domain.Storage
	dataDir string
	tempDir string
}

// NewConsistencyScanner 创建巡检器
func NewConsistencyScanner(meta domain.Metadata, storage domain.Storage, dataDir, tempDir string) *ConsistencyScanner {
	return &ConsistencyScanner{
		meta:    meta,
		storage: storage,
		dataDir: dataDir,
		tempDir: tempDir,
	}
}

// Scan 执行一次完整巡检
func (s *ConsistencyScanner) Scan(ctx context.Context) (any, error) {
	report := &ScannerReport{}

	// 1. 孤儿临时分片
	s.scanOrphanParts(ctx, report)

	// 2. 孤儿物理文件（data/ 下有文件但 DB 无记录）
	s.scanOrphanFiles(ctx, report)

	// 3. 元数据孤儿（DB 有记录但 Storage 文件丢失）
	s.scanMetadataOrphans(ctx, report)

	// 4. 引用计数漂移
	s.scanRefCount(ctx, report)

	return report, nil
}

func (s *ConsistencyScanner) scanOrphanParts(_ context.Context, report *ScannerReport) {
	entries, err := os.ReadDir(s.tempDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			report.OrphanParts++
		}
	}
}

func (s *ConsistencyScanner) scanOrphanFiles(ctx context.Context, report *ScannerReport) {
	if s.meta == nil {
		return
	}
	// 数据目录下的所有 namespace
	dataDir := s.dataDir
	nsEntries, err := os.ReadDir(dataDir)
	if err != nil {
		return
	}

	for _, nsEntry := range nsEntries {
		if !nsEntry.IsDir() {
			continue
		}
		nsDir := filepath.Join(dataDir, nsEntry.Name())
		fileEntries, err := os.ReadDir(nsDir)
		if err != nil {
			continue
		}

		for _, f := range fileEntries {
			if f.IsDir() {
				continue
			}
			// 检查 DB 是否有对应文件记录
			// 按文件名（不含扩展名？实际上 fileID = 文件名）
			fileID := f.Name()
			dbFile, err := s.meta.GetFile(ctx, fileID)
			if err != nil || dbFile == nil {
				report.OrphanFiles = append(report.OrphanFiles,
					fmt.Sprintf("data/%s/%s", nsEntry.Name(), fileID))
			}
		}
	}
}

func (s *ConsistencyScanner) scanMetadataOrphans(ctx context.Context, report *ScannerReport) {
	if s.meta == nil {
		return
	}
	blobs, err := s.meta.ListAllBlobs(ctx)
	if err != nil {
		return
	}

	for _, blob := range blobs {
		_, exists, err := s.storage.Stat(ctx, blob.StoragePath)
		if err != nil || !exists {
			report.MetadataOrphans++
			log.Printf("[scanner] 元数据孤儿: blob %s 对应文件 %s 不存在",
				blob.SHA256, blob.StoragePath)
		}
	}
}

func (s *ConsistencyScanner) scanRefCount(ctx context.Context, report *ScannerReport) {
	if s.meta == nil {
		return
	}
	blobs, err := s.meta.ListAllBlobs(ctx)
	if err != nil {
		return
	}

	for _, blob := range blobs {
		refFiles, err := s.meta.ListFilesByBlob(ctx, blob.SHA256)
		if err != nil {
			continue
		}
		actualCount := len(refFiles)
		if actualCount != blob.RefCount {
			log.Printf("[scanner] 引用计数漂移: blob %s DB=%d 实际=%d",
				blob.SHA256, blob.RefCount, actualCount)
			report.RefCountFixes++
		}
	}
}
