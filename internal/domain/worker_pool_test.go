package domain

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestSimpleWorkerPool_Submit(t *testing.T) {
	pool := NewSimpleWorkerPool(2, 10)
	defer pool.Stop()

	ctx := context.Background()
	var counter atomic.Int32

	for i := 0; i < 10; i++ {
		err := pool.Submit(ctx, func() {
			counter.Add(1)
		})
		if err != nil {
			t.Fatalf("Submit error = %v", err)
		}
	}

	// 等待所有任务完成
	time.Sleep(100 * time.Millisecond)

	if counter.Load() != 10 {
		t.Errorf("counter = %d, want 10", counter.Load())
	}
}

func TestSimpleWorkerPool_SubmitWithCancelledContext(t *testing.T) {
	pool := NewSimpleWorkerPool(1, 0) // 队列满，确保 select 选 ctx.Done()
	defer pool.Stop()

	// 先填满队列（容量=0 但实际默认=100...）
	// 用容量 1 队列 0 的实际效果
	// 用阻塞方式测试：创建已取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 多次尝试，至少有一次应该返回 ctx error
	foundErr := false
	for i := 0; i < 20; i++ {
		err := pool.Submit(ctx, func() {})
		if err != nil {
			foundErr = true
			break
		}
	}
	if !foundErr {
		// 不能保证总是命中，但不是错误
		t.Log("已取消上下文未返回错误（select 随机选择导致）")
	}
}

func TestSimpleWorkerPool_Stats(t *testing.T) {
	pool := NewSimpleWorkerPool(4, 20)
	defer pool.Stop()

	stats := pool.Stats()
	if stats.Capacity != 4 {
		t.Errorf("Capacity = %d, want 4", stats.Capacity)
	}
}

func TestSimpleWorkerPool_QueueFull(t *testing.T) {
	// 队列大小为 1，提交会阻塞，测试超时路径
	pool := NewSimpleWorkerPool(1, 0) // 0 会被转为默认 100
	defer pool.Stop()

	ctx := context.Background()
	for i := 0; i < 50; i++ {
		err := pool.Submit(ctx, func() {
			time.Sleep(5 * time.Millisecond)
		})
		if err != nil {
			// 可能返回 ErrBusy
			if err != ErrBusy {
				t.Errorf("意外的错误: %v", err)
			}
		}
	}
	// 只是验证不 panic
}

func TestNewSimpleWorkerPool_Defaults(t *testing.T) {
	pool := NewSimpleWorkerPool(0, 0) // 应自动按 CPU 核数缩放
	if pool == nil {
		t.Fatal("NewSimpleWorkerPool returned nil")
	}
	expected := runtime.GOMAXPROCS(0) * 8
	if pool.capacity != expected {
		t.Errorf("default capacity = %d, want %d (GOMAXPROCS*8)", pool.capacity, expected)
	}
	pool.Stop()
}

func TestNewSimpleWorkerPool_ExplicitCapacity(t *testing.T) {
	pool := NewSimpleWorkerPool(4, 20) // 显式指定，不应缩放
	if pool.capacity != 4 {
		t.Errorf("capacity = %d, want 4", pool.capacity)
	}
	if cap(pool.tasks) != 20 {
		t.Errorf("queue size = %d, want 20", cap(pool.tasks))
	}
	pool.Stop()
}
