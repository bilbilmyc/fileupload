package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Progress 进度跟踪器。
type Progress struct {
	total    int64
	current  int64
	mu       sync.Mutex
	onUpdate func(current, total int64)
}

// NewProgress 创建新进度跟踪器。onUpdate 会在每次 Add 后回调（可能频繁，建议内部限频）。
func NewProgress(total int64, onUpdate func(current, total int64)) *Progress {
	return &Progress{total: total, onUpdate: onUpdate}
}

// Add 增加已完成的字节数。
func (p *Progress) Add(n int64) {
	atomic.AddInt64(&p.current, n)
	if p.onUpdate != nil {
		p.onUpdate(atomic.LoadInt64(&p.current), atomic.LoadInt64(&p.total))
	}
}

// Percent 返回当前完成百分比（0-100）。
func (p *Progress) Percent() int {
	if p.total == 0 {
		return 100
	}
	return int(atomic.LoadInt64(&p.current) * 100 / p.total)
}

// humanBytes 格式化字节数为人类可读字符串（如 "1.5 MB"）。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n >= div*unit && exp < 4 {
		div *= unit
		exp++
	}
	switch exp {
	case 0:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(div))
	case 1:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(div))
	case 2:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(div))
	default:
		return fmt.Sprintf("%.1f TB", float64(n)/float64(div))
	}
}
