// Package testutil 提供领域层共享的 mock 类型，供 domain 和 transport 包的测试使用。
//
// 使用方式（domain 包）：
//
//	meta := testutil.NewMockMetadata()
//	storage := testutil.NewMockStorage()
//
// 所有 mock 均为线程安全。
package testutil

import "github.com/bilbilmyc/fileupload/internal/domain"

// compile-time interface checks
var (
	_ domain.Metadata        = (*MockMetadata)(nil)
	_ domain.Storage         = (*MockStorage)(nil)
	_ domain.Compressor      = (*MockCompressor)(nil)
	_ domain.Hasher          = (*MockHasher)(nil)
	_ domain.WorkerPool      = (*MockWorkerPool)(nil)
	_ domain.ArchiveWriter   = (*MockArchiveWriter)(nil)
	_ domain.HashAccumulator = (*MockHashAccumulator)(nil)
)

// ContainsIgnoreCase 检查 s 是否包含 substr（大小写不敏感）
func ContainsIgnoreCase(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc, tc := s[i+j], substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
