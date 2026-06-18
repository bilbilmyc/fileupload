// Package hasher 实现 domain.Hasher 端口的 SHA-256 适配器
package hasher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// SHA256Hasher SHA-256 哈希实现
type SHA256Hasher struct{}

// NewSHA256Hasher 创建 SHA-256 哈希器
func NewSHA256Hasher() *SHA256Hasher {
	return &SHA256Hasher{}
}

// Sum 边读边算 SHA-256，返回 hex 哈希 + 读取字节数
func (h *SHA256Hasher) Sum(_ context.Context, r io.Reader) (string, int64, error) {
	hasher := sha256.New()
	n, err := io.Copy(hasher, r)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), n, nil
}

// TeeReader 返回一个 Tee Reader，读它的同时累计哈希
func (h *SHA256Hasher) TeeReader(r io.Reader) (io.Reader, domain.HashAccumulator) {
	acc := NewAccumulator()
	tee := io.TeeReader(r, acc)
	return tee, acc
}

// Accumulator 完整的 HashAccumulator 实现
type Accumulator struct {
	h hash.Hash
	n int64
}

// NewAccumulator 创建累计器
func NewAccumulator() *Accumulator {
	return &Accumulator{
		h: sha256.New(),
	}
}

// Write 实现 io.Writer，累计写入数据和字节数
func (a *Accumulator) Write(p []byte) (int, error) {
	n, err := a.h.Write(p)
	a.n += int64(n)
	return n, err
}

// SumHex 返回当前已写入数据的 SHA-256 hex
func (a *Accumulator) SumHex() string {
	return hex.EncodeToString(a.h.Sum(nil))
}

// N 返回已写入字节数
func (a *Accumulator) N() int64 {
	return a.n
}
