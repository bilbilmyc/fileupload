package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Addr != ":8080" {
		t.Errorf("default addr = %s", cfg.Server.Addr)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("default redis addr = %s", cfg.Redis.Addr)
	}
	if cfg.Database.Type != "sqlite" {
		t.Errorf("default db type = %s", cfg.Database.Type)
	}
	if cfg.Upload.DefaultChunkSize != 10*1024*1024 {
		t.Errorf("default chunk size = %d", cfg.Upload.DefaultChunkSize)
	}
	if cfg.Upload.SessionTTLMinutes != 60 {
		t.Errorf("default session ttl = %d", cfg.Upload.SessionTTLMinutes)
	}
}

func TestLoad_NoConfigFile(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// 应返回默认配置
	if cfg.Server.Addr != ":8080" {
		t.Errorf("addr = %s", cfg.Server.Addr)
	}
}

func TestLoad_FromYAML(t *testing.T) {
	// 清理 CI 环境中可能干扰的环境变量
	for _, k := range []string{"FILEUPLOAD_SERVER_ADDR", "FILEUPLOAD_REDIS_ADDR", "FILEUPLOAD_CHUNK_SIZE", "FILEUPLOAD_SESSION_TTL", "FILEUPLOAD_DB_PATH", "FILEUPLOAD_STORAGE_DATA_DIR"} {
		if v := os.Getenv(k); v != "" {
			os.Unsetenv(k)
			t.Cleanup(func() { os.Setenv(k, v) })
		}
	}

	dir := t.TempDir()
	yamlContent := `
server:
  addr: ":9090"
  read_timeout: 60
upload:
  session_ttl_minutes: 120
  default_chunk_size: 20971520
redis:
  addr: "myredis:6379"
`
	yamlPath := filepath.Join(dir, "fileupload.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Addr != ":9090" {
		t.Errorf("addr = %s, want :9090", cfg.Server.Addr)
	}
	if cfg.Server.ReadTimeout != 60 {
		t.Errorf("read_timeout = %d, want 60", cfg.Server.ReadTimeout)
	}
	if cfg.Upload.SessionTTLMinutes != 120 {
		t.Errorf("session_ttl = %d, want 120", cfg.Upload.SessionTTLMinutes)
	}
	if cfg.Upload.DefaultChunkSize != 20971520 {
		t.Errorf("chunk_size = %d, want 20971520", cfg.Upload.DefaultChunkSize)
	}
	if cfg.Redis.Addr != "myredis:6379" {
		t.Errorf("redis addr = %s", cfg.Redis.Addr)
	}
}

func TestLoad_FromDirectory(t *testing.T) {
	dir := t.TempDir()
	yamlContent := "server:\n  addr: \":9999\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fileupload.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(dir) error = %v", err)
	}
	if cfg.Server.Addr != ":9999" {
		t.Errorf("addr = %s, want :9999", cfg.Server.Addr)
	}
}

func TestLoad_WithYmlExtension(t *testing.T) {
	dir := t.TempDir()
	yamlContent := "server:\n  addr: \":7777\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fileupload.yml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(yml) error = %v", err)
	}
	if cfg.Server.Addr != ":7777" {
		t.Errorf("addr = %s, want :7777", cfg.Server.Addr)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	os.Setenv("FILEUPLOAD_SERVER_ADDR", ":1234")
	os.Setenv("FILEUPLOAD_REDIS_ADDR", "envredis:9999")
	os.Setenv("FILEUPLOAD_CHUNK_SIZE", "4194304")
	defer func() {
		os.Unsetenv("FILEUPLOAD_SERVER_ADDR")
		os.Unsetenv("FILEUPLOAD_REDIS_ADDR")
		os.Unsetenv("FILEUPLOAD_CHUNK_SIZE")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":1234" {
		t.Errorf("env addr = %s, want :1234", cfg.Server.Addr)
	}
	if cfg.Redis.Addr != "envredis:9999" {
		t.Errorf("env redis = %s", cfg.Redis.Addr)
	}
	if cfg.Upload.DefaultChunkSize != 4194304 {
		t.Errorf("env chunk_size = %d, want 4194304", cfg.Upload.DefaultChunkSize)
	}
}

func TestLoad_EnvOverrideFile(t *testing.T) {
	// 环境变量应覆盖配置文件
	os.Setenv("FILEUPLOAD_SERVER_ADDR", ":4321")
	defer os.Unsetenv("FILEUPLOAD_SERVER_ADDR")

	dir := t.TempDir()
	yamlContent := "server:\n  addr: \":1111\"\n"
	yamlPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(yamlPath, []byte(yamlContent), 0644)

	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":4321" {
		t.Errorf("env should override file: got %s, want :4321", cfg.Server.Addr)
	}
}

func TestSessionTTL(t *testing.T) {
	u := UploadConfig{SessionTTLMinutes: 30}
	ttl := u.SessionTTL()
	if ttl.Minutes() != 30 {
		t.Errorf("SessionTTL = %v minutes, want 30", ttl.Minutes())
	}
}

func TestDumpYAML(t *testing.T) {
	cfg := DefaultConfig()
	yaml, err := cfg.DumpYAML()
	if err != nil {
		t.Fatalf("DumpYAML error = %v", err)
	}
	if len(yaml) == 0 {
		t.Error("DumpYAML 为空")
	}
}

func TestResolveConfigPath(t *testing.T) {
	dir := t.TempDir()

	// 不存在的路径
	path := resolveConfigPath(filepath.Join(dir, "nonexistent"))
	if path != "" {
		t.Errorf("不存在的路径应返回空: %s", path)
	}

	// 非 yaml 文件
	f := filepath.Join(dir, "config.json")
	os.WriteFile(f, []byte("{}"), 0644)
	path = resolveConfigPath(f)
	if path != "" {
		t.Errorf("非 yaml 文件应返回空: %s", path)
	}

	// yaml 文件
	f = filepath.Join(dir, "config.yaml")
	os.WriteFile(f, []byte(""), 0644)
	path = resolveConfigPath(f)
	if path != f {
		t.Errorf("yaml 文件应返回路径: got %s, want %s", path, f)
	}
}

func TestDefaultPGConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Database.PG.Host != "localhost" {
		t.Errorf("default pg host = %s", cfg.Database.PG.Host)
	}
	if cfg.Database.PG.Port != 5432 {
		t.Errorf("default pg port = %d", cfg.Database.PG.Port)
	}
	if cfg.Database.PG.SSLMode != "disable" {
		t.Errorf("default pg sslmode = %s", cfg.Database.PG.SSLMode)
	}
}

func TestPGConfig_BuildDSN(t *testing.T) {
	pg := PGConfig{
		Host:     "10.0.0.1",
		Port:     35432,
		User:     "admin",
		Password: "Pass@2026#secure",
		DBName:   "mydb",
		SSLMode:  "require",
	}
	dsn := pg.BuildDSN()
	// url.URL.String 会在路径前加 /
	expected := "postgres://admin:Pass%402026%23secure@10.0.0.1:35432/mydb?sslmode=require"
	if dsn != expected {
		t.Errorf("BuildDSN = %s, want %s", dsn, expected)
	}
}

func TestPGConfig_BuildDSN_DefaultPort(t *testing.T) {
	pg := PGConfig{
		Host:   "localhost",
		User:   "postgres",
		DBName: "fileupload",
	}
	dsn := pg.BuildDSN()
	if !strings.Contains(dsn, ":5432/") {
		t.Errorf("DSN should use default port 5432: %s", dsn)
	}
	if !strings.Contains(dsn, "sslmode=disable") {
		t.Errorf("DSN should default sslmode=disable: %s", dsn)
	}
}

func TestPGConfig_BuildDSN_EmptyPassword(t *testing.T) {
	pg := PGConfig{
		Host:   "localhost",
		User:   "postgres",
		DBName: "test",
	}
	dsn := pg.BuildDSN()
	if strings.Contains(dsn, "%") {
		t.Errorf("Empty password should not produce URL-encoded chars: %s", dsn)
	}
}

func TestPGConfig_BuildDSN_SpecialChars(t *testing.T) {
	// 密码含 @、#、?、&、空格 等特殊字符
	pg := PGConfig{
		Host:     "db.example.com",
		Port:     5432,
		User:     "user",
		Password: "@#?& =",
		DBName:   "app",
	}
	dsn := pg.BuildDSN()
	// 密码应被 URL 编码，不出现裸特殊字符
	if strings.Contains(dsn, "@#?") {
		t.Errorf("DSN should encode special chars in password: %s", dsn)
	}
	// 验证可以解析
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse error = %v", err)
	}
	pw, _ := parsed.User.Password()
	// url.PathEscape 将空格编为 %20
	expectedPW := "@#?& ="
	if pw != expectedPW {
		t.Errorf("password roundtrip = %s, want %s", pw, expectedPW)
	}
}
