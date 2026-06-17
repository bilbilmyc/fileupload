package domain

import (
	"context"
	"sync"
	"time"
)

// SimpleWorkerPool 简单的固定大小 worker 池实现
type SimpleWorkerPool struct {
	capacity int
	tasks    chan func()
	wg       sync.WaitGroup
	queued   int32
	running  int32
}

// NewSimpleWorkerPool 创建固定大小的 worker 池
func NewSimpleWorkerPool(capacity, queueSize int) *SimpleWorkerPool {
	if capacity <= 0 {
		capacity = 4
	}
	if queueSize <= 0 {
		queueSize = 100
	}
	p := &SimpleWorkerPool{
		capacity: capacity,
		tasks:    make(chan func(), queueSize),
	}
	p.start()
	return p
}

// start 启动 worker goroutines
func (p *SimpleWorkerPool) start() {
	for i := 0; i < p.capacity; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for task := range p.tasks {
				// atomic.AddInt32(&p.running, 1) // 可选跟踪
				task()
				// atomic.AddInt32(&p.running, -1)
			}
		}()
	}
}

// Submit 提交任务到池中，带超时排队
func (p *SimpleWorkerPool) Submit(ctx context.Context, task func()) error {
	select {
	case p.tasks <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// 队列满，尝试带超时的 select
		select {
		case p.tasks <- task:
			return nil
		case <-time.After(5 * time.Second):
			return ErrBusy
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Stats 返回当前池状态
func (p *SimpleWorkerPool) Stats() WorkerStats {
	return WorkerStats{
		Capacity:  p.capacity,
		Available: p.capacity, // 简化
	}
}

// Stop 停止所有 worker
func (p *SimpleWorkerPool) Stop() {
	close(p.tasks)
	p.wg.Wait()
}

// compile-time check
var _ WorkerPool = (*SimpleWorkerPool)(nil)
