package transport

import (
	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/bilbilmyc/fileupload/internal/testutil"
)

// 薄包装层：将 testutil 的导出 mock 类型重命名为 transport 测试中原有的名字，
// 避免修改所有测试文件。

type mockMeta struct {
	*testutil.MockMetadata
}

func newMockMeta() *mockMeta {
	return &mockMeta{testutil.NewMockMetadata()}
}

type mockStore struct {
	*testutil.MockStorage
}

func newMockStore() *mockStore {
	return &mockStore{testutil.NewMockStorage()}
}

type mockCompr struct {
	*testutil.MockCompressor
}

func newMockCompr() *mockCompr {
	return &mockCompr{testutil.NewMockCompressor()}
}

type mockHashr struct {
	*testutil.MockHasher
}

func newMockHashr() *mockHashr {
	return &mockHashr{testutil.NewMockHasher()}
}

type mockWP struct {
	*testutil.MockWorkerPool
}

func newMockWP() *mockWP {
	return &mockWP{testutil.NewMockWorkerPool()}
}

// compile-time interface checks
var (
	_ domain.Metadata   = (*mockMeta)(nil)
	_ domain.Storage    = (*mockStore)(nil)
	_ domain.Compressor = (*mockCompr)(nil)
	_ domain.Hasher     = (*mockHashr)(nil)
	_ domain.WorkerPool = (*mockWP)(nil)
)
