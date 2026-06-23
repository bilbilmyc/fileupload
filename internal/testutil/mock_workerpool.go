package testutil

import (
	"context"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// MockWorkerPool implements domain.WorkerPool
type MockWorkerPool struct {
	RunImmediate bool
}

// NewMockWorkerPool 创建 MockWorkerPool
func NewMockWorkerPool() *MockWorkerPool {
	return &MockWorkerPool{RunImmediate: true}
}

func (m *MockWorkerPool) Submit(_ context.Context, fn func()) error {
	if m.RunImmediate {
		fn()
	}
	return nil
}

func (m *MockWorkerPool) Stats() domain.WorkerStats {
	return domain.WorkerStats{Capacity: 4, Available: 4}
}
