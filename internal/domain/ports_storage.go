package domain

import (
	"context"
	"io"
	"io/fs"
)

// Storage 存储后端抽象（本地 FS / S3）
type Storage interface {
	// Write 从 reader 流式写到 path，返回写入字节数
	Write(ctx context.Context, path string, r io.Reader) (n int64, err error)

	// Open 打开读取句柄，支持 Range（length=0 表示到文件尾）
	Open(ctx context.Context, path string, offset, length int64) (io.ReadCloser, error)

	// Delete 删除物理文件
	Delete(ctx context.Context, path string) error

	// Stat 获取文件信息（大小、是否存在）
	Stat(ctx context.Context, path string) (size int64, exists bool, err error)

	// Walk 遍历存储，fn 收到的是相对于 root 的路径。
	// 用于一致性巡检、批量列举等场景。
	Walk(ctx context.Context, fn func(path string, info fs.FileInfo) error) error

	// HealthCheck 健康检查：返回 nil 表示后端可用，否则返回错误信息。
	// 用于服务探活（/healthz 等），不应做写入操作。
	HealthCheck(ctx context.Context) error
}
