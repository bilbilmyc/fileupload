package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Progress 可视化进度条。
// 支持上传/下载的速率跟踪、百分比、视觉条渲染。
type Progress struct {
	total     int64
	current   int64
	label     string
	startTime time.Time
	stopCh    chan struct{}
	done      sync.Once
	mu        sync.Mutex
}

// NewProgress 创建新的进度跟踪器。
func NewProgress(total int64, label string) *Progress {
	return &Progress{
		total:     total,
		label:     label,
		startTime: time.Now(),
	}
}

// Add 增加已完成的字节数。
func (p *Progress) Add(n int64) {
	atomic.AddInt64(&p.current, n)
}

// Start 启动后台渲染协程（每 200ms 重绘一次进度条）。
// 用于上传等非 io.Reader 驱动的场景。
func (p *Progress) Start() {
	p.stopCh = make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.renderBar()
			case <-p.stopCh:
				return
			}
		}
	}()
}

// Stop 停止后台渲染协程并执行最终渲染。
func (p *Progress) Stop() {
	p.done.Do(func() {
		if p.stopCh != nil {
			close(p.stopCh)
		}
	})
}

// renderBar 渲染一条进度行（\r 开头覆盖当前行）。
func (p *Progress) renderBar() {
	current := atomic.LoadInt64(&p.current)
	total := atomic.LoadInt64(&p.total)
	elapsed := time.Since(p.startTime)

	if total <= 0 {
		fmt.Printf("\r  %s: %s", p.label, humanBytes(current))
		return
	}

	// 百分比
	pct := current * 100 / total
	if pct > 100 {
		pct = 100
	}

	// 速率
	var speedStr string
	if elapsed > 0 {
		speed := float64(current) / elapsed.Seconds()
		speedStr = humanBytes(int64(speed)) + "/s"
	}

	// ETA
	var etaStr string
	if current > 0 && current < total {
		remain := float64(total-current) / (float64(current) / elapsed.Seconds())
		etaStr = fmt.Sprintf(" ETA %s", formatDuration(time.Duration(remain)*time.Second))
	}

	// 进度条（20 格）
	done := int(pct * 20 / 100)
	if done > 20 {
		done = 20
	}
	bar := strings.Repeat("█", done) + strings.Repeat("░", 20-done)

	fmt.Printf("\r  %s: [%s]  %s / %s  (%d%%)  %s%s",
		p.label, bar,
		humanBytes(current), humanBytes(total), pct,
		speedStr, etaStr)
}

// Done 完成时调用，打印最终状态并换行。
func (p *Progress) Done() {
	current := atomic.LoadInt64(&p.current)
	total := atomic.LoadInt64(&p.total)
	elapsed := time.Since(p.startTime)

	if total <= 0 {
		fmt.Printf("\r  %s: %s  完成 (%s)\n", p.label, humanBytes(current), formatDuration(elapsed))
		return
	}

	pct := current * 100 / total
	if pct > 100 {
		pct = 100
	}
	bar := strings.Repeat("█", 20)

	var speedStr string
	if elapsed > 0 {
		speed := float64(current) / elapsed.Seconds()
		speedStr = humanBytes(int64(speed)) + "/s"
	}

	fmt.Printf("\r  %s: [%s]  %s / %s  (%d%%)  %s  (%s)\n",
		p.label, bar,
		humanBytes(current), humanBytes(total), pct,
		speedStr, formatDuration(elapsed))
}

// Percent 返回当前完成百分比（0-100）。
func (p *Progress) Percent() int {
	total := atomic.LoadInt64(&p.total)
	if total == 0 {
		return 100
	}
	return int(atomic.LoadInt64(&p.current) * 100 / total)
}

// ProgressReader 包装 io.Reader，读取时自动更新进度。
type ProgressReader struct {
	r        io.Reader
	progress *Progress
	tick     *time.Ticker
	done     chan struct{}
}

// NewProgressReader 包装 reader，每 100ms 渲染一次进度。
func NewProgressReader(r io.Reader, p *Progress) *ProgressReader {
	pr := &ProgressReader{
		r:        r,
		progress: p,
		tick:     time.NewTicker(100 * time.Millisecond),
		done:     make(chan struct{}),
	}
	go func() {
		for {
			select {
			case <-pr.tick.C:
				pr.progress.renderBar()
			case <-pr.done:
				return
			}
		}
	}()
	return pr
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.progress.Add(int64(n))
	}
	if err != nil {
		close(pr.done)
		pr.tick.Stop()
		// 最后渲染一次
		pr.progress.renderBar()
	}
	return n, err
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

// formatDuration 格式化时长到可读形式（截断精度）。
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
