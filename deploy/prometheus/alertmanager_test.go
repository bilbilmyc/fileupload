package prometheus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type alertmanagerRoute struct {
	Matchers []struct {
		Name  string `yaml:"name"`
		Value string `yaml:"value"`
	} `yaml:"matchers"`
	Receiver       string `yaml:"receiver"`
	GroupWait      string `yaml:"group_wait"`
	GroupInterval  string `yaml:"group_interval"`
	RepeatInterval string `yaml:"repeat_interval"`
}

type alertmanagerConfig struct {
	Global         map[string]any         `yaml:"global"`
	Route          struct {
		Receiver string             `yaml:"receiver"`
		Routes   []alertmanagerRoute `yaml:"routes"`
	} `yaml:"route"`
	InhibitRules []struct {
		SourceMatchers []struct {
			Name  string `yaml:"name"`
			Value string `yaml:"value"`
		} `yaml:"source_matchers"`
		TargetMatchers []struct {
			Name  string `yaml:"name"`
			Value string `yaml:"value"`
		} `yaml:"target_matchers"`
		Equal []string `yaml:"equal"`
	} `yaml:"inhibit_rules"`
	Receivers []struct {
		Name string `yaml:"name"`
	} `yaml:"receivers"`
}

func TestAlertmanager_ValidYAML(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alertmanager.yml"))
	if err != nil {
		t.Fatalf("read alertmanager.yml: %v", err)
	}
	var cfg alertmanagerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	if cfg.Route.Receiver == "" {
		t.Error("route.receiver 为空")
	}
}

func TestAlertmanager_SeverityRoutes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alertmanager.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg alertmanagerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}

	wantSeverities := []string{"critical", "warning", "info"}
	for _, sev := range wantSeverities {
		found := false
		for _, r := range cfg.Route.Routes {
			for _, m := range r.Matchers {
				if m.Name == "severity" && m.Value == sev {
					found = true
					if r.Receiver == "" {
						t.Errorf("severity=%s 的路由缺少 receiver", sev)
					}
				}
			}
		}
		if !found {
			t.Errorf("missing severity=%s 路由", sev)
		}
	}
}

func TestAlertmanager_CriticalFasterThanWarning(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alertmanager.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg alertmanagerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}

	var criticalWait, warningWait string
	for _, r := range cfg.Route.Routes {
		for _, m := range r.Matchers {
			switch m.Value {
			case "critical":
				criticalWait = r.GroupWait
			case "warning":
				warningWait = r.GroupWait
			}
		}
	}
	if criticalWait == "" || warningWait == "" {
		t.Fatal("未找到 critical/warning 路由的 group_wait")
	}
	// 简单比较字符串形式的 duration（critical 应 < warning）
	if !isShorter(criticalWait, warningWait) {
		t.Errorf("critical group_wait (%s) 应短于 warning (%s)，保证 critical 立即通知",
			criticalWait, warningWait)
	}
}

// TestAlertmanager_HasInhibitRules 验证抑制规则存在，避免上游宕机时噪声告警
func TestAlertmanager_HasInhibitRules(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alertmanager.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg alertmanagerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.InhibitRules) == 0 {
		t.Error("未配置 inhibit_rules，建议添加上游宕机时的抑制")
	}
}

// TestAlertmanager_HasReceivers 验证所有 severity 都有对应 receiver
func TestAlertmanager_HasReceivers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "alertmanager.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg alertmanagerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}

	receiverNames := make(map[string]bool)
	for _, r := range cfg.Receivers {
		receiverNames[r.Name] = true
	}

	for _, r := range cfg.Route.Routes {
		if !receiverNames[r.Receiver] {
			t.Errorf("路由引用了未定义的 receiver: %s", r.Receiver)
		}
	}
}

// isShorter 比较两个 Prometheus duration 字符串（如 "10s" vs "1m"）
func isShorter(a, b string) bool {
	// 简单解析：取数字 + 单位比较
	va, ua := parseDur(a)
	vb, ub := parseDur(b)
	na := va * unitToMs(ua)
	nb := vb * unitToMs(ub)
	return na < nb
}

func parseDur(s string) (float64, string) {
	for i, c := range s {
		if (c >= '0' && c <= '9') || c == '.' {
			continue
		}
		if i == 0 {
			return 0, s
		}
		return parseFloatSimple(s[:i]), s[i:]
	}
	return parseFloatSimple(s), "s"
}

func unitToMs(u string) float64 {
	switch u {
	case "ms":
		return 1
	case "s":
		return 1000
	case "m":
		return 60 * 1000
	case "h":
		return 3600 * 1000
	}
	return 1000
}

// parseFloatSimple 简单 float 解析（避免 strconv 依赖）
func parseFloatSimple(s string) float64 {
	var x float64
	if _, err := fmtSscanF(s, &x); err != nil {
		return 0
	}
	return x
}

func fmtSscanF(s string, v *float64) (int, error) {
	var neg bool
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	var intPart, fracPart float64
	var fracDiv float64 = 1
	dotSeen := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			dotSeen = true
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		d := float64(c - '0')
		if dotSeen {
			fracDiv *= 10
			fracPart = fracPart*10 + d
		} else {
			intPart = intPart*10 + d
		}
	}
	*v = intPart + fracPart/fracDiv
	if neg {
		*v = -*v
	}
	return 1, nil
}

// 防止 import "strings" 未用报警
var _ = strings.Contains