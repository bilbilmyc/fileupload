package testutil

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// MockStorage implements domain.Storage (thread-safe)
type MockStorage struct {
	mu    sync.Mutex
	files map[string][]byte
}

// NewMockStorage 创建 MockStorage
func NewMockStorage() *MockStorage {
	return &MockStorage{files: make(map[string][]byte)}
}

func (m *MockStorage) Write(_ context.Context, path string, r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.files[path] = data
	m.mu.Unlock()
	return int64(len(data)), nil
}

func (m *MockStorage) Open(_ context.Context, path string, offset, length int64) (io.ReadCloser, error) {
	m.mu.Lock()
	data, ok := m.files[path]
	m.mu.Unlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	start := int(offset)
	end := len(data)
	if length > 0 && start+int(length) < end {
		end = start + int(length)
	}
	if start >= len(data) {
		return nil, domain.ErrInvalidArgument
	}
	return io.NopCloser(bytes.NewReader(data[start:end])), nil
}

func (m *MockStorage) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	delete(m.files, path)
	m.mu.Unlock()
	return nil
}

func (m *MockStorage) Stat(_ context.Context, path string) (int64, bool, error) {
	m.mu.Lock()
	data, ok := m.files[path]
	m.mu.Unlock()
	if !ok {
		return 0, false, nil
	}
	return int64(len(data)), true, nil
}

func (m *MockStorage) Walk(_ context.Context, fn func(path string, info fs.FileInfo) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for path, data := range m.files {
		if err := fn(path, mockFileInfo{name: path, size: int64(len(data))}); err != nil {
			return err
		}
	}
	return nil
}

// Has 检查路径是否存在
func (m *MockStorage) Has(path string) bool {
	m.mu.Lock()
	_, ok := m.files[path]
	m.mu.Unlock()
	return ok
}

type mockFileInfo struct {
	name string
	size int64
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() fs.FileMode  { return 0 }
func (m mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m mockFileInfo) IsDir() bool        { return false }
func (m mockFileInfo) Sys() any           { return nil }

// HealthCheck mock 始终返回 nil。
func (m *MockStorage) HealthCheck(_ context.Context) error { return nil }
