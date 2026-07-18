// Package storage 实现 domain.Storage 端口的本地文件系统适配器
package storage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// LocalFS 本地文件系统存储实现
type LocalFS struct {
	root string // 根目录（如 data/）
}

// NewLocalFS 创建本地文件系统存储
// root 是数据根目录，所有路径都相对于它。
// 根目录会在首次写入时自动创建。
func NewLocalFS(root string) (*LocalFS, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("本地存储根目录路径解析: %w", err)
	}
	return &LocalFS{root: absRoot}, nil
}

// absPath 将逻辑路径转为绝对路径，含路径穿越安全检查。
// 除了词法路径检查，还会校验已存在的路径组件，避免通过符号链接
//（以及平台能够解析的 reparse point/junction）逃出存储根目录。
func (s *LocalFS) absPath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || containsPathTraversal(path) {
		return "", domain.ErrPathTraversal
	}
	cleanPath := filepath.Clean(path)
	if cleanPath == "." || cleanPath == string(filepath.Separator) {
		return "", domain.ErrInvalidArgument
	}

	candidate := filepath.Join(s.root, cleanPath)
	rel, err := filepath.Rel(s.root, candidate)
	if err != nil {
		return "", fmt.Errorf("校验存储路径: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", domain.ErrPathTraversal
	}
	if err := s.validatePathComponents(candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func (s *LocalFS) validatePathComponents(candidate string) error {
	root := filepath.Clean(s.root)
	rootInfo, rootErr := os.Lstat(root)
	if rootErr == nil && rootInfo.Mode()&os.ModeSymlink != 0 {
		return domain.ErrPathTraversal
	}

	current := filepath.Clean(candidate)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return domain.ErrPathTraversal
			}
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return fmt.Errorf("解析存储路径: %w", err)
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return fmt.Errorf("解析存储路径: %w", err)
			}
			resolvedRoot, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("解析存储根目录: %w", err)
			}
			if rootInfo != nil {
				resolvedRoot, err = filepath.EvalSymlinks(root)
				if err != nil {
					return fmt.Errorf("解析存储根目录: %w", err)
				}
				resolvedRoot, err = filepath.Abs(resolvedRoot)
				if err != nil {
					return fmt.Errorf("解析存储根目录: %w", err)
				}
			}
			rel, err := filepath.Rel(resolvedRoot, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				return domain.ErrPathTraversal
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("检查存储路径: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current || current == root {
			return nil
		}
		current = parent
	}
}

// Write 从 reader 流式写入文件
func (s *LocalFS) Write(_ context.Context, path string, r io.Reader) (int64, error) {
	absPath, err := s.absPath(path)
	if err != nil {
		return 0, err
	}

	// 确保父目录存在
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return 0, fmt.Errorf("创建目录 %s: %w", filepath.Dir(absPath), err)
	}
	if err := s.validatePathComponents(absPath); err != nil {
		return 0, err
	}

	// 创建文件（原子写入：先写 tmp 再重命名）
	// 但流式场景不适合先写 tmp 再 rename，所以直接写目标文件
	f, err := os.Create(absPath)
	if err != nil {
		return 0, fmt.Errorf("创建文件 %s: %w", absPath, err)
	}
	defer f.Close()

	n, err := io.Copy(f, r)
	if err != nil {
		// 写入失败时尝试清理
		os.Remove(absPath)
		return n, fmt.Errorf("写入文件: %w", err)
	}

	return n, nil
}

// Open 打开文件读取，支持 Range
func (s *LocalFS) Open(_ context.Context, path string, offset, length int64) (io.ReadCloser, error) {
	absPath, err := s.absPath(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("打开文件 %s: %w", absPath, err)
	}

	// 如果指定了 offset，seek 到指定位置
	if offset > 0 {
		_, err := f.Seek(offset, io.SeekStart)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("seek 到 %d: %w", offset, err)
		}
	}

	// 如果指定了 length，用 LimitedReader 包装
	if length > 0 {
		return &struct {
			io.Reader
			io.Closer
		}{
			Reader: io.LimitReader(f, length),
			Closer: f,
		}, nil
	}

	return f, nil
}

// Delete 删除文件
func (s *LocalFS) Delete(_ context.Context, path string) error {
	absPath, err := s.absPath(path)
	if err != nil {
		return err
	}

	if err := os.Remove(absPath); err != nil {
		if os.IsNotExist(err) {
			return nil // 删除不存在的文件视为成功
		}
		return fmt.Errorf("删除文件 %s: %w", absPath, err)
	}
	return nil
}

// Stat 获取文件信息
func (s *LocalFS) Stat(_ context.Context, path string) (int64, bool, error) {
	absPath, err := s.absPath(path)
	if err != nil {
		return 0, false, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("stat %s: %w", absPath, err)
	}

	if info.IsDir() {
		return 0, false, fmt.Errorf("%s 是目录", path)
	}

	return info.Size(), true, nil
}

// PathExists 检查路径是否存在（内部使用）
func (s *LocalFS) PathExists(path string) (bool, error) {
	absPath, err := s.absPath(path)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Walk 遍历目录（一致性巡检用）
func (s *LocalFS) Walk(ctx context.Context, fn func(path string, info fs.FileInfo) error) error {
	return filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filepath.Clean(path) == filepath.Clean(s.root) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return domain.ErrPathTraversal
		}
		if err := s.validatePathComponents(path); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		return fn(relPath, info)
	})
}

// Root 返回根目录路径
func (s *LocalFS) Root() string {
	return s.root
}

// containsPathTraversal 检查路径是否含 .. 遍历
func containsPathTraversal(path string) bool {
	// 统一分隔符为 / 后再检查（Windows 上 filepath.Clean 会转 \）
	normalized := strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

// HealthCheck 检查本地存储根目录是否可访问。
func (s *LocalFS) HealthCheck(_ context.Context) error {
	info, err := os.Stat(s.root)
	if err != nil {
		return fmt.Errorf("本地存储根目录不可访问: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("本地存储根 %s 不是目录", s.root)
	}
	return nil
}
