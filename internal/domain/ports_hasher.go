package domain

import (
	"context"
	"io"
)

// Hasher SHA-256 哈希抽象
type Hasher interface {
	// Sum 边读边算，返回 hex 哈希 + 读取字节数；完成后 reader 已耗尽
	Sum(ctx context.Context, r io.Reader) (sha256hex string, n int64, err error)

	// TeeReader 返回一个 reader，读它的同时累计哈希，最后通过 hash 方法取值
	TeeReader(r io.Reader) (io.Reader, HashAccumulator)
}

// HashAccumulator 累计哈希值接口，配合 TeeReader 使用
type HashAccumulator interface {
	// SumHex 返回当前已读取数据的哈希 hex
	SumHex() string
	// N 返回已读取字节数
	N() int64
}
