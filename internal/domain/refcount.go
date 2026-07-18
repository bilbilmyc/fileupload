package domain

import "context"

// IncrWithRollback 是 BlobStore.IncrBlobRef + 可选 DecrBlobRef 回滚的便捷包装。
//
// 适用模式：先增加 ref_count，再做其他持久化操作；如其他操作失败则回滚。
// 6 个引用计数站点（CheckExists / commitStream / BatchCopy / 等）此前各自实现
// 这个模式，行为偶有偏差。本函数统一纪律：
//   - Incr 失败：返回 nil 闭包 + error，调用方应终止后续操作
//   - Incr 成功：调用方在后续失败时应调用 rollback() 撤销
//   - rollback 多次调用幂等（第二次起为 no-op）
//
// 不引入新端口：仍是 BlobStore 的标准方法组合，零 adapter 改动。
func IncrWithRollback(ctx context.Context, blobs BlobStore, sha256 string) (rollback func() error, err error) {
	if err := blobs.IncrBlobRef(ctx, sha256); err != nil {
		return nil, err
	}
	var done bool
	return func() error {
		if done {
			return nil
		}
		done = true
		_, derr := blobs.DecrBlobRef(ctx, sha256)
		return derr
	}, nil
}
