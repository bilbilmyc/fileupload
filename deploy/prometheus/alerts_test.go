package prometheus

// alerts_test.go 验证 deploy/prometheus/alerts.yml 与 internal/metrics 一致。
//
// 防回归：改 metrics 名称但忘了同步 alerts.yml 时 CI 会失败。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type alertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

type alertGroup struct {
	Name     string      `yaml:"name"`
	Interval string      `yaml:"interval"`
	Rules    []alertRule `yaml:"rules"`
}

type alertsFile struct {
	Groups []alertGroup `yaml:"groups"`
}

// 真实指标名（与 internal/metrics/metrics.go 中的 Name 一致）
// 这里硬编码而非 import metrics 包是为了 deploy/ 不依赖 internal/ 包。
var knownMetrics = map[string]bool{
	"fileupload_uploads_total":               true,
	"fileupload_upload_bytes_total":          true,
	"fileupload_downloads_total":             true,
	"fileupload_batch_operations_total":      true,
	"fileupload_batch_operation_items_total": true,
	"fileupload_reaper_cleanups_total":       true,
	"fileupload_health_status":               true,
}

// 允许的非业务指标（Prometheus 标准 / 服务发现）
var allowedExternalMetrics = map[string]bool{
	"up": true,
}

func TestAlertsYAML_ValidSyntax(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alerts.yml"))
	if err != nil {
		t.Fatalf("read alerts.yml: %v", err)
	}
	var af alertsFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	if len(af.Groups) == 0 {
		t.Fatal("no groups defined")
	}
}

func TestAlertsYAML_Has6Rules(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alerts.yml"))
	if err != nil {
		t.Fatalf("read alerts.yml: %v", err)
	}
	var af alertsFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}

	totalRules := 0
	for _, g := range af.Groups {
		totalRules += len(g.Rules)
	}
	if totalRules != 6 {
		t.Errorf("expected 6 alert rules, got %d", totalRules)
	}
}

func TestAlertsYAML_RequiredAlertNames(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alerts.yml"))
	if err != nil {
		t.Fatalf("read alerts.yml: %v", err)
	}
	var af alertsFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}

	want := []string{
		"FileUploadDown",
		"FileUploadHealthDegraded",
		"FileUploadErrorRateHigh",
		"FileUploadStalled",
		"FileUploadBatchErrorRateHigh",
		"FileUploadReaperStalled",
	}

	var got []string
	for _, g := range af.Groups {
		for _, r := range g.Rules {
			got = append(got, r.Alert)
		}
	}

	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing required alert: %s", w)
		}
	}
}

// TestAlertsYAML_ExpressionsReferenceRealMetrics 验证每条规则的 expr 至少引用一个真实指标。
// 防回归：删/改 metrics 时忘了同步 alerts.yml。
func TestAlertsYAML_ExpressionsReferenceRealMetrics(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alerts.yml"))
	if err != nil {
		t.Fatalf("read alerts.yml: %v", err)
	}
	var af alertsFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}

	// 提取形如 fileupload_xxx 或 up 的指标名
	metricPattern := regexp.MustCompile(`\b(fileupload_[a-z_]+|up)\b`)

	for _, g := range af.Groups {
		for _, r := range g.Rules {
			matches := metricPattern.FindAllString(r.Expr, -1)
			if len(matches) == 0 {
				t.Errorf("rule %s expr 中未引用任何指标: %q", r.Alert, r.Expr)
				continue
			}
			for _, m := range matches {
				if knownMetrics[m] {
					continue
				}
				if allowedExternalMetrics[m] {
					continue
				}
				t.Errorf("rule %s 引用了未知指标 %q（不在 internal/metrics 中）", r.Alert, m)
			}
		}
	}
}

func TestAlertsYAML_EachRuleHasSeverity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alerts.yml"))
	if err != nil {
		t.Fatalf("read alerts.yml: %v", err)
	}
	var af alertsFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}

	for _, g := range af.Groups {
		for _, r := range g.Rules {
			sev, ok := r.Labels["severity"]
			if !ok {
				t.Errorf("rule %s 缺少 severity 标签", r.Alert)
				continue
			}
			if !strings.Contains("critical|warning|info", sev) {
				t.Errorf("rule %s severity=%q 不在 critical/warning/info 范围内", r.Alert, sev)
			}
		}
	}
}
