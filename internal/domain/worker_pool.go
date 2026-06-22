package domain

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// SimpleWorkerPool 简单的固定大小 worker 池实现
type SimpleWorkerPool struct {
	capacity int
	tasks    chan func()
	wg       sync.WaitGroup
	running  atomic.Int32
	queued   atomic.Int32
}

// NewSimpleWorkerPool 创建 worker 池。
// capacity <= 0 时自动按 CPU 核数缩放：GOMAXPROCS × 8。
// queueSize <= 0 时按 capacity × 5 设置。
func NewSimpleWorkerPool(capacity, queueSize int) *SimpleWorkerPool {
	if capacity <= 0 {
		capacity = runtime.GOMAXPROCS(0) * 8
	}
	if queueSize <= 0 {
		queueSize = capacity * 5
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
				p.running.Add(1)
				task()
				p.running.Add(-1)
			}
		}()
	}
}

// Submit 提交任务到池中，带超时排队
func (p *SimpleWorkerPool) Submit(ctx context.Context, task func()) error {
	p.queued.Add(1)
	defer p.queued.Add(-1)

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
	running := int(p.running.Load())
	available := p.capacity - running
	if available < 0 {
		available = 0
	}
	return WorkerStats{
		Capacity:  p.capacity,
		Available: available,
	}
}

// Stop 停止所有 worker
func (p *SimpleWorkerPool) Stop() {
	close(p.tasks)
	p.wg.Wait()
}

// compile-time check
var _ WorkerPool = (*SimpleWorkerPool)(nil)
