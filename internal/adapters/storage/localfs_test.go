package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

func newTestLocalFS(t *testing.T) (*LocalFS, string) {
	t.Helper()
	dir := t.TempDir()
	fs, err := NewLocalFS(dir)
	if err != nil {
		t.Fatalf("NewLocalFS error = %v", err)
	}
	return fs, dir
}

func TestLocalFS_WriteRead(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	data := []byte("hello localfs storage")
	path := "test/hello.txt"

	n, err := fs.Write(ctx, path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Write error = %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("Write n = %d, want %d", n, len(data))
	}

	// 读取验证
	reader, err := fs.Open(ctx, path, 0, 0)
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("读取数据不匹配: got %s, want %s", got, data)
	}
}

func TestLocalFS_Write_CreatesParentDir(t *testing.T) {
	fs, root := newTestLocalFS(t)
	ctx := context.Background()

	path := "a/b/c/d/file.txt"
	n, err := fs.Write(ctx, path, bytes.NewReader([]byte("data")))
	if err != nil {
		t.Fatalf("Write error = %v", err)
	}
	if n != 4 {
		t.Errorf("Write n = %d, want 4", n)
	}

	// 验证文件存在
	absPath := filepath.Join(root, path)
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Errorf("文件不存在: %s", absPath)
	}
}

func TestLocalFS_Open_Range(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	data := []byte("0123456789abcdef")
	path := "range_test.bin"

	_, err := fs.Write(ctx, path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Write error = %v", err)
	}

	tests := []struct {
		name   string
		offset int64
		length int64
		want   string
	}{
		{"全部", 0, 0, "0123456789abcdef"},
		{"前 5 字节", 0, 5, "01234"},
		{"中间 4 字节", 4, 4, "4567"},
		{"末尾 4 字节", 12, 4, "cdef"},
		{"offset 仅", 8, 0, "89abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := fs.Open(ctx, path, tt.offset, tt.length)
			if err != nil {
				t.Fatalf("Open error = %v", err)
			}
			defer reader.Close()

			got, _ := io.ReadAll(reader)
			if string(got) != tt.want {
				t.Errorf("Open(%d,%d) = %s, want %s", tt.offset, tt.length, got, tt.want)
			}
		})
	}
}

func TestLocalFS_Delete(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	path := "delete_me.txt"
	fs.Write(ctx, path, bytes.NewReader([]byte("data")))

	// 删除
	if err := fs.Delete(ctx, path); err != nil {
		t.Fatalf("Delete error = %v", err)
	}

	// 验证已删除
	_, exists, err := fs.Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat error = %v", err)
	}
	if exists {
		t.Error("文件删除后 Stat 仍返回 exists=true")
	}
}

func TestLocalFS_Delete_NotExist(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	// 删除不存在的文件不应报错
	if err := fs.Delete(ctx, "nonexistent.txt"); err != nil {
		t.Errorf("Delete(不存在) error = %v", err)
	}
}

func TestLocalFS_Stat(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	path := "stat_test.bin"
	data := []byte("stat test data")
	fs.Write(ctx, path, bytes.NewReader(data))

	size, exists, err := fs.Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat error = %v", err)
	}
	if !exists {
		t.Error("Stat exists = false, want true")
	}
	if size != int64(len(data)) {
		t.Errorf("Stat size = %d, want %d", size, len(data))
	}
}

func TestLocalFS_Stat_NotExist(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	_, exists, err := fs.Stat(ctx, "no_such_file")
	if err != nil {
		t.Fatalf("Stat error = %v", err)
	}
	if exists {
		t.Error("不存在的文件 Stat exists = true")
	}
}

func TestLocalFS_Open_NotExist(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	_, err := fs.Open(ctx, "no_such_file", 0, 0)
	if err != domain.ErrNotFound {
		t.Errorf("Open(不存在) error = %v, want ErrNotFound", err)
	}
}

func TestLocalFS_PathTraversal_Rejected(t *testing.T) {
	fs, _ := newTestLocalFS(t)
	ctx := context.Background()

	traversalPaths := []string{
		"../../etc/passwd",
		"data/../../../etc/shadow",
		"a/../../b/../../c",
		"..",
	}

	for _, path := range traversalPaths {
		t.Run(path, func(t *testing.T) {
			_, err := fs.Write(ctx, path, bytes.NewReader([]byte("x")))
			if err != domain.ErrPathTraversal {
				t.Errorf("路径穿越未拒绝: %s, err = %v", path, err)
			}
		})
	}
}

func TestLocalFS_PathExists(t *testing.T) {
	fs, _ := newTestLocalFS(t)

	exists, err := fs.PathExists("nonexistent")
	if err != nil {
		t.Fatalf("PathExists error = %v", err)
	}
	if exists {
		t.Error("不存在的路径 PathExists = true")
	}

	fs.Write(context.Background(), "exists.txt", bytes.NewReader([]byte("x")))
	exists, err = fs.PathExists("exists.txt")
	if err != nil {
		t.Fatalf("PathExists error = %v", err)
	}
	if !exists {
		t.Error("存在的路径 PathExists = false")
	}
}

func TestLocalFS_Root(t *testing.T) {
	fs, root := newTestLocalFS(t)
	if fs.Root() != root {
		t.Errorf("Root() = %s, want %s", fs.Root(), root)
	}
}

func TestContainsPathTraversal(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"normal/file.txt", false},
		{"a/b/c", false},
		{"../escape", true},
		{"a/../../b", true},
		{"...", false},
		{"..", true},
		{"foo/..", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := containsPathTraversal(tt.path); got != tt.want {
				t.Errorf("containsPathTraversal(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
