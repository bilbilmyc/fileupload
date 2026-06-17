package domain

import "context"

// WorkerPool 分片处理池
// 所有上传会话共享，限制并发磁盘 IO。
type WorkerPool interface {
	// Submit 提交一个任务到池中；带超时排队，池满返回 ErrBusy
	Submit(ctx context.Context, task func()) error

	// Stats 返回当前池状态（并发数、排队数、容量）
	Stats() WorkerStats
}

// WorkerStats 池状态快照
type WorkerStats struct {
	Running   int `json:"running"`
	Queued    int `json:"queued"`
	Capacity  int `json:"capacity"`
	Available int `json:"available"`
}
