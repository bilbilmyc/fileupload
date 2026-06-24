package metrics

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// TestMetricsRegistered 验证关键指标在默认 Registerer 中已注册
func TestMetricsRegistered(t *testing.T) {
	// 预触发所有指标（Vec 类型至少要有一个 label 才会出现在 /metrics 输出）
	UploadsTotal.WithLabelValues(ResultSuccess).Inc()
	UploadBytesTotal.Add(1024)
	DownloadsTotal.WithLabelValues("file", ResultSuccess).Inc()
	BatchOpsTotal.WithLabelValues("copy", ResultSuccess).Inc()
	BatchOpItems.WithLabelValues("copy", ResultSuccess).Inc()
	ReaperCleanupsTotal.WithLabelValues("expired_session").Inc()
	HealthStatus.WithLabelValues("storage").Set(1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	promhttp.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("metrics endpoint status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	wantSubstrings := []string{
		`fileupload_uploads_total{result="success"}`,
		`fileupload_upload_bytes_total`,
		`fileupload_downloads_total{kind="file",result="success"}`,
		`fileupload_batch_operations_total{operation="copy",result="success"}`,
		`fileupload_batch_operation_items_total{item_result="success",operation="copy"}`,
		`fileupload_reaper_cleanups_total{kind="expired_session"}`,
		`fileupload_health_status{component="storage"}`,
		`# HELP fileupload_uploads_total`,
		`# TYPE fileupload_uploads_total counter`,
	}
	for _, sub := range wantSubstrings {
		if !strings.Contains(body, sub) {
			t.Errorf("missing metric in /metrics output: %s", sub)
		}
	}
}

// TestMetricsCounterIncrement 验证 Counter 累积语义
func TestMetricsCounterIncrement(t *testing.T) {
	before := readCounter(t, `fileupload_batch_operations_total{operation="delete",result="success"}`)

	for i := 0; i < 3; i++ {
		BatchOpsTotal.WithLabelValues("delete", ResultSuccess).Inc()
	}

	after := readCounter(t, `fileupload_batch_operations_total{operation="delete",result="success"}`)
	if after-before < 3 {
		t.Errorf("expected counter to increase by ≥3, got before=%v after=%v", before, after)
	}
}

// readCounter 解析 prometheus 文本格式，提取指定行当前值
func readCounter(t *testing.T, target string) float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	promhttp.Handler().ServeHTTP(rec, req)

	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, target+" ") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			v, err := strconv.ParseFloat(parts[len(parts)-1], 64)
			if err == nil {
				return v
			}
		}
	}
	return 0
}