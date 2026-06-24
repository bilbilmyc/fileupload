package domain

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkDeleteDir_Atomic 验证 deleteDir 在原子回滚路径下的性能。
// 100 文件 / 10 子目录嵌套，每个文件关联一个 blob，blob.RefCount=1 → 删除后 ref_count=0。
// 目的：防止 undoOp 栈机制无意中引入线性回归。
func BenchmarkDeleteDir_Atomic(b *testing.B) {
	sizes := []int{10, 50, 200}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("files=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				svc, meta, _ := newBenchUploadService(b)
				ctx := context.Background()
				rootID := "bench-root"
				meta.PutFile(ctx, &FileMetadata{
					FileID: rootID, Name: "root", Namespace: "demo", IsDir: true, CreatedAt: time.Now(),
				})

				for j := 0; j < n; j++ {
					fid := fmt.Sprintf("f-%d", j)
					sha := fmt.Sprintf("sha-%d", j)
					meta.PutFile(ctx, &FileMetadata{
						FileID: fid, Name: fid, Namespace: "demo", ParentID: rootID, SHA256: sha, CreatedAt: time.Now(),
					})
					meta.PutBlob(ctx, &ContentBlob{SHA256: sha, StoragePath: sha, RefCount: 1})
				}

				_ = svc.DeleteDir(ctx, rootID, true, "demo")
			}
		})
	}
}

// BenchmarkDeleteDir_Nested 验证嵌套目录场景。
// 3 层嵌套 × 每层 5 个文件 = 15 个 leaf 文件 + 3 层目录节点。
func BenchmarkDeleteDir_Nested(b *testing.B) {
	for i := 0; i < b.N; i++ {
		svc, meta, _ := newBenchUploadService(b)
		ctx := context.Background()

		rootID := "root"
		l1ID := "level-1"
		l2ID := "level-2"

		meta.PutFile(ctx, &FileMetadata{FileID: rootID, Name: "root", Namespace: "demo", IsDir: true, CreatedAt: time.Now()})
		meta.PutFile(ctx, &FileMetadata{FileID: l1ID, Name: "l1", Namespace: "demo", ParentID: rootID, IsDir: true, CreatedAt: time.Now()})
		meta.PutFile(ctx, &FileMetadata{FileID: l2ID, Name: "l2", Namespace: "demo", ParentID: l1ID, IsDir: true, CreatedAt: time.Now()})

		for j := 0; j < 5; j++ {
			fid := fmt.Sprintf("f-%d", j)
			sha := fmt.Sprintf("sha-%d", j)
			meta.PutFile(ctx, &FileMetadata{FileID: fid, Name: fid, Namespace: "demo", ParentID: l2ID, SHA256: sha, CreatedAt: time.Now()})
			meta.PutBlob(ctx, &ContentBlob{SHA256: sha, StoragePath: sha, RefCount: 1})
		}

		_ = svc.DeleteDir(ctx, rootID, true, "demo")
	}
}