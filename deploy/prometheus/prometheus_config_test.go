package prometheus

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type prometheusConfig struct {
	Global struct {
		ScrapeInterval     string `yaml:"scrape_interval"`
		ScrapeTimeout      string `yaml:"scrape_timeout"`
		EvaluationInterval string `yaml:"evaluation_interval"`
	} `yaml:"global"`
	RuleFiles []string `yaml:"rule_files"`
	Alerting  struct {
		Alertmanagers []struct {
			StaticConfigs []struct {
				Targets []string `yaml:"targets"`
			} `yaml:"static_configs"`
		} `yaml:"alertmanagers"`
	} `yaml:"alerting"`
	ScrapeConfigs []struct {
		JobName       string `yaml:"job_name"`
		MetricsPath   string `yaml:"metrics_path"`
		Scheme        string `yaml:"scheme"`
		StaticConfigs []struct {
			Targets []string `yaml:"targets"`
		} `yaml:"static_configs"`
	} `yaml:"scrape_configs"`
}

func TestPrometheusYAML_ValidP0Config(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}

	var cfg prometheusConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	if cfg.Global.ScrapeInterval == "" || cfg.Global.EvaluationInterval == "" {
		t.Fatal("global scrape/evaluation interval must be configured")
	}
	if len(cfg.RuleFiles) != 1 || filepath.Base(cfg.RuleFiles[0]) != "alerts.yml" {
		t.Fatalf("rule_files = %#v, want alerts.yml", cfg.RuleFiles)
	}
	if len(cfg.Alerting.Alertmanagers) != 1 || len(cfg.Alerting.Alertmanagers[0].StaticConfigs) != 1 {
		t.Fatal("alertmanager target is not configured")
	}
	if len(cfg.Alerting.Alertmanagers[0].StaticConfigs[0].Targets) != 1 || cfg.Alerting.Alertmanagers[0].StaticConfigs[0].Targets[0] != "alertmanager:9093" {
		t.Fatalf("alertmanager targets = %#v, want alertmanager:9093", cfg.Alerting.Alertmanagers[0].StaticConfigs[0].Targets)
	}
	if len(cfg.ScrapeConfigs) != 1 {
		t.Fatalf("scrape_configs = %d, want one fileupload job", len(cfg.ScrapeConfigs))
	}
	job := cfg.ScrapeConfigs[0]
	if job.JobName != "fileupload" {
		t.Fatalf("job_name = %q, want fileupload", job.JobName)
	}
	if job.MetricsPath != "/metrics" || job.Scheme != "http" {
		t.Fatalf("metrics endpoint = %s://%s, want http:///metrics", job.Scheme, job.MetricsPath)
	}
	if len(job.StaticConfigs) != 1 || len(job.StaticConfigs[0].Targets) != 1 || job.StaticConfigs[0].Targets[0] != "server:8080" {
		t.Fatalf("fileupload targets = %#v, want server:8080", job.StaticConfigs)
	}
}
