package grafana

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

type panel struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Targets []struct {
		Expr       string `json:"expr"`
		RefID      string `json:"refId"`
		LegendFormat string `json:"legendFormat"`
	} `json:"targets"`
}

type dashboard struct {
	Title  string  `json:"title"`
	UID    string  `json:"uid"`
	Panels []panel `json:"panels"`
}

// 真实指标名（与 internal/metrics/metrics.go 一致 + Prometheus 标准指标）
var knownMetrics = map[string]bool{
	"fileupload_uploads_total":                 true,
	"fileupload_upload_bytes_total":             true,
	"fileupload_downloads_total":                true,
	"fileupload_batch_operations_total":         true,
	"fileupload_batch_operation_items_total":   true,
	"fileupload_reaper_cleanups_total":         true,
	"fileupload_health_status":                 true,
	"up":                                       true,
	// Go runtime metrics（由 promhttp 自动暴露）
	"go_memstats_heap_inuse_bytes":             true,
	"go_goroutines":                            true,
}

func TestDashboard_ValidJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "dashboard.json"))
	if err != nil {
		t.Fatalf("read dashboard.json: %v", err)
	}
	var d dashboard
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if d.Title == "" {
		t.Error("dashboard.title 为空")
	}
	if d.UID == "" {
		t.Error("dashboard.uid 为空")
	}
}

func TestDashboard_Has10Panels(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "dashboard.json"))
	if err != nil {
		t.Fatalf("read dashboard.json: %v", err)
	}
	var d dashboard
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if len(d.Panels) != 10 {
		t.Errorf("expected 10 panels, got %d", len(d.Panels))
	}
}

func TestDashboard_RequiredPanels(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "dashboard.json"))
	if err != nil {
		t.Fatalf("read dashboard.json: %v", err)
	}
	var d dashboard
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	wantTitles := []string{
		"服务可用性",
		"后端组件健康",
		"上传速率（每秒）",
		"上传字节吞吐量",
		"下载速率",
		"批量操作调用次数（5m rate）",
		"批量操作条目成功率",
		"Reaper 清理速率",
		"Go runtime — 内存",
		"Go runtime — Goroutines",
	}

	gotTitles := make([]string, 0, len(d.Panels))
	for _, p := range d.Panels {
		gotTitles = append(gotTitles, p.Title)
	}
	sort.Strings(gotTitles)

	for _, w := range wantTitles {
		found := false
		for _, g := range gotTitles {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing required panel: %q", w)
		}
	}
}

// TestDashboard_PanelsReferenceRealMetrics 验证每个面板的 expr 至少引用一个真实指标。
// 防回归：删/改 metrics 时忘了同步 dashboard。
func TestDashboard_PanelsReferenceRealMetrics(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "dashboard.json"))
	if err != nil {
		t.Fatalf("read dashboard.json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	metricPattern := regexp.MustCompile(`\b(fileupload_[a-z_]+|up|go_[a-z_]+)\b`)
	// 递归扫描所有 expr 字段
	exprs := extractExprs(raw)

	if len(exprs) == 0 {
		t.Fatal("未找到任何 expr 字段")
	}

	for _, expr := range exprs {
		matches := metricPattern.FindAllString(expr, -1)
		if len(matches) == 0 {
			t.Errorf("expr 引用了无法识别的指标: %q", expr)
			continue
		}
		for _, m := range matches {
			if !knownMetrics[m] {
				t.Errorf("expr %q 引用了未知指标 %q", expr, m)
			}
		}
	}
}

// extractExprs 递归提取 dashboard 中所有 "expr" 字段值。
func extractExprs(v any) []string {
	var out []string
	switch val := v.(type) {
	case map[string]any:
		for k, vv := range val {
			if k == "expr" {
				if s, ok := vv.(string); ok {
					out = append(out, s)
				}
			} else {
				out = append(out, extractExprs(vv)...)
			}
		}
	case []any:
		for _, item := range val {
			out = append(out, extractExprs(item)...)
		}
	}
	return out
}