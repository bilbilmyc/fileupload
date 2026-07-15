package lifecycle

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// TrashJanitor permanently removes recycle-bin roots whose retention period expired.
// A non-positive retention disables cleanup, preserving all deleted data indefinitely.
type TrashJanitor struct {
	meta      domain.AdminStore
	upload    *domain.UploadService
	retention time.Duration
	interval  time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func NewTrashJanitor(meta domain.AdminStore, upload *domain.UploadService, retention, interval time.Duration) *TrashJanitor {
	if interval <= 0 {
		interval = time.Hour
	}
	return &TrashJanitor{meta: meta, upload: upload, retention: retention, interval: interval, stopCh: make(chan struct{})}
}
func (j *TrashJanitor) Start() {
	if j.retention <= 0 {
		log.Printf("[trash] 自动清理已禁用")
		return
	}
	j.wg.Add(1)
	go func() {
		defer j.wg.Done()
		ticker := time.NewTicker(j.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				j.reap(context.Background())
			case <-j.stopCh:
				return
			}
		}
	}()
	log.Printf("[trash] 自动清理已启动（保留期 %s，间隔 %s）", j.retention, j.interval)
}
func (j *TrashJanitor) Stop() { close(j.stopCh); j.wg.Wait() }
func (j *TrashJanitor) reap(ctx context.Context) {
	files, err := j.meta.ListAllFiles(ctx)
	if err != nil {
		log.Printf("[trash] 列举文件失败: %v", err)
		return
	}
	byID := make(map[string]*domain.FileMetadata, len(files))
	for _, f := range files {
		byID[f.FileID] = f
	}
	cutoff := time.Now().Add(-j.retention)
	for _, f := range files {
		if f.DeletedAt == nil || f.DeletedAt.After(cutoff) {
			continue
		}
		parent := byID[f.ParentID]
		if parent != nil && parent.DeletedAt != nil {
			continue
		}
		if err := j.upload.PurgeFromTrash(ctx, f.FileID, f.Namespace); err != nil {
			log.Printf("[trash] 清理 %s 失败: %v", f.FileID, err)
		} else {
			log.Printf("[trash] 已清理过期项目: %s", f.FileID)
		}
	}
}
