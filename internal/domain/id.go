package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
)

var (
	idCounter uint64
	prefix    string
)

func init() {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	prefix = hex.EncodeToString(b)
}

// NewID 生成唯一 ID（简短、无序、并发安全）
// 格式：8 字符随机前缀 + 自增计数器（hex）
func NewID() string {
	n := atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("%s%016x", prefix, n)
}
