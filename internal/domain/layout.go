package domain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// HierarchicalLayout 集中管理文件系统的路径布局约定。
//
// 文件上传后存在两种物理路径格式：
//   - FlatPath:        "namespace/filename"             — Finalize 阶段写入（ADR-0001）
//   - HierarchicalPath: "namespace/dirName/entryPath"   — SubmitDir 阶段搬移后（ADR-0001）
//
// 此前两种路径以 fmt.Sprintf 散在 5 个调用点（upload.go:350、upload.go:470、
// download.go:248 等）。本类型把它们收敛到一处，方便 ADR-0001 的"层级搬移"
// 语义维护与未来重构。
type HierarchicalLayout struct{}

// NewHierarchicalLayout 构造（无状态，可共享）。
func NewHierarchicalLayout() *HierarchicalLayout { return &HierarchicalLayout{} }

// FlatPath 返回 Finalize 阶段写入的扁平路径。
// 格式："namespace/filename"
func (l *HierarchicalLayout) FlatPath(namespace, fileName string) string {
	return fmt.Sprintf("%s/%s", namespace, fileName)
}

// HierarchicalPath 返回 SubmitDir 阶段使用的层级路径。
// 格式："namespace/dirName/entryPath"
func (l *HierarchicalLayout) HierarchicalPath(namespace, dirName, entryPath string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, dirName, entryPath)
}

// Move 把文件从 srcPath 搬到 destPath（Open + Write + Delete 语义符合 ADR-0001）。
// 任一阶段失败立即返回错误，不静默吞错 — 与之前 upload.go:472-479 的 swallow 行为不同。
func (l *HierarchicalLayout) Move(ctx context.Context, storage Storage, srcPath, destPath string) error {
	reader, err := storage.Open(ctx, srcPath, 0, 0)
	if err != nil {
		return fmt.Errorf("打开源文件 %s: %w", srcPath, err)
	}
	if _, err := storage.Write(ctx, destPath, reader); err != nil {
		_ = reader.Close()
		return fmt.Errorf("写入目标文件 %s: %w", destPath, err)
	}
	if err := reader.Close(); err != nil {
		return fmt.Errorf("关闭源 reader: %w", err)
	}
	if err := storage.Delete(ctx, srcPath); err != nil {
		return fmt.Errorf("删除源文件 %s: %w", srcPath, err)
	}
	return nil
}

// ParentPath 解析 entryPath 的父目录路径（用于目录树构建）。
// "a/b/c.txt" → "a/b"，"" → ""
func ParentPath(entryPath string) string {
	if idx := strings.LastIndex(entryPath, "/"); idx >= 0 {
		return entryPath[:idx]
	}
	return ""
}

// Base 解析 entryPath 的最后一段作为 Name。
func Base(entryPath string) string {
	return filepath.Base(entryPath)
}