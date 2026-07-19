package transport

import (
	"testing"

	"github.com/bilbilmyc/fileupload/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestUpdateHealthMetrics(t *testing.T) {
	metrics.HealthStatus.WithLabelValues("storage").Set(1)
	metrics.HealthStatus.WithLabelValues("metadata").Set(1)
	t.Cleanup(func() {
		metrics.HealthStatus.WithLabelValues("storage").Set(1)
		metrics.HealthStatus.WithLabelValues("metadata").Set(1)
	})

	updateHealthMetrics(map[string]any{
		"storage":  map[string]any{"status": "error"},
		"metadata": map[string]any{"status": "ok"},
		"ignored":  "not-a-health-map",
	})

	if got := testutil.ToFloat64(metrics.HealthStatus.WithLabelValues("storage")); got != 0 {
		t.Fatalf("storage health metric = %v, want 0", got)
	}
	if got := testutil.ToFloat64(metrics.HealthStatus.WithLabelValues("metadata")); got != 1 {
		t.Fatalf("metadata health metric = %v, want 1", got)
	}
}
