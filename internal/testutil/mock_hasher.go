package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// MockHasher implements domain.Hasher
type MockHasher struct{}

// NewMockHasher 创建 MockHasher
func NewMockHasher() *MockHasher { return &MockHasher{} }

func (m *MockHasher) Sum(_ context.Context, r io.Reader) (string, int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), int64(len(data)), nil
}

func (m *MockHasher) TeeReader(r io.Reader) (io.Reader, domain.HashAccumulator) {
	acc := NewMockHashAccumulator()
	tee := io.TeeReader(r, acc)
	return tee, acc
}

// MockHashAccumulator implements domain.HashAccumulator
type MockHashAccumulator struct {
	data []byte
}

// NewMockHashAccumulator 创建 MockHashAccumulator
func NewMockHashAccumulator() *MockHashAccumulator {
	return &MockHashAccumulator{}
}

func (m *MockHashAccumulator) Write(p []byte) (int, error) {
	m.data = append(m.data, p...)
	return len(p), nil
}

func (m *MockHashAccumulator) SumHex() string {
	h := sha256.Sum256(m.data)
	return hex.EncodeToString(h[:])
}

func (m *MockHashAccumulator) N() int64 {
	return int64(len(m.data))
}
