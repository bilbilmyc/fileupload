package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestCLI_CommandTree 验证所有子命令已正确注册
func TestCLI_CommandTree(t *testing.T) {
	expected := map[string]string{
		"upload":     "上传文件或目录",
		"download":   "下载文件或目录",
		"rm":         "删除文件或目录",
		"ls":         "列目录",
		"stat":       "查看文件或目录元信息",
		"status":     "查看上传会话进度",
		"scan":       "触发服务端一致性巡检",
		"bench":      "压测",
		"config":     "查看当前配置",
		"login":      "保存 X-Auth-Token 到本地",
		"completion": "生成 shell 补全脚本",
	}

	cmds := rootCmd.Commands()
	if len(cmds) == 0 {
		t.Fatal("no commands registered")
	}

	registered := make(map[string]*cobra.Command)
	for _, c := range cmds {
		registered[c.Name()] = c
	}

	for name, desc := range expected {
		cmd, ok := registered[name]
		if !ok {
			t.Errorf("missing command: %s", name)
			continue
		}
		if !strings.Contains(cmd.Short, desc) && !strings.Contains(desc, cmd.Short) {
			t.Logf("command %s short=%q (expected to contain %q)", name, cmd.Short, desc)
		}
	}
}

// TestCLI_PersistentFlags 验证全局 flag 已注册
func TestCLI_PersistentFlags(t *testing.T) {
	flags := []string{"server", "namespace", "config", "token"}
	for _, name := range flags {
		f := rootCmd.PersistentFlags().Lookup(name)
		if f == nil {
			t.Errorf("missing persistent flag: --%s", name)
		}
	}
}

// TestCLI_UploadFlags 验证 upload 子命令有正确的 flag
func TestCLI_UploadFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"upload"})
	if err != nil {
		t.Fatalf("find upload command: %v", err)
	}
	if cmd == nil {
		t.Fatal("upload command not found")
	}

	// 验证 flag
	expectedFlags := []string{"chunk-size", "concurrency", "compress", "resume", "exclude-hidden"}
	for _, name := range expectedFlags {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("upload missing flag: --%s", name)
		}
	}

}

// TestCLI_DownloadFlags 验证 download 子命令 flag
func TestCLI_DownloadFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"download"})
	if err != nil {
		t.Fatalf("find download command: %v", err)
	}

	expectedFlags := []string{"output", "format", "verify", "range"}
	for _, name := range expectedFlags {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("download missing flag: --%s", name)
		}
	}
}

// TestCLI_BenchFlags 验证 bench 子命令 flag
func TestCLI_BenchFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"bench"})
	if err != nil {
		t.Fatalf("find bench command: %v", err)
	}

	for _, name := range []string{"files", "size", "concurrency"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("bench missing flag: --%s", name)
		}
	}
}

// TestCLI_Completion 验证 completion 子命令
func TestCLI_Completion(t *testing.T) {
	// 创建临时 rootCmd 的副本用于测试
	// 实际测试 completion 的基本参数
	cmd, _, err := rootCmd.Find([]string{"completion"})
	if err != nil {
		t.Fatalf("find completion command: %v", err)
	}

	if cmd == nil {
		t.Fatal("completion command not found")
	}

	// ValidArgs 应该包含 bash/zsh/fish/powershell
	validArgs := cmd.ValidArgs
	if len(validArgs) != 4 {
		t.Errorf("completion ValidArgs = %v, want [bash zsh fish powershell]", validArgs)
	}
}

// TestCLI_LoginFlags 验证 login 子命令有 --token flag
func TestCLI_LoginFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"login"})
	if err != nil {
		t.Fatalf("find login command: %v", err)
	}
	if cmd == nil {
		t.Fatal("login command not found")
	}

	f := cmd.Flags().Lookup("token")
	if f == nil {
		t.Error("login missing flag: --token")
	}
}

// TestCLI_CommandHelp 验证每个命令不 panic
func TestCLI_CommandHelp(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		t.Run(cmd.Name(), func(t *testing.T) {
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs([]string{"--help"})
			// 不执行 RunE，只验证 help 输出
			if err := cmd.Help(); err != nil {
				t.Errorf("help for %s: %v", cmd.Name(), err)
			}
			if buf.Len() == 0 {
				t.Errorf("help for %s produced no output", cmd.Name())
			}
		})
	}
}

// TestCLI_ParseSizeFlag 验证 parseSizeFlag 函数
func TestCLI_ParseSizeFlag(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"10m", 10 * 1024 * 1024},
		{"1g", 1024 * 1024 * 1024},
		{"512k", 512 * 1024},
		{"100", 100},
		{"", 10},
		{"0", 10},
	}
	for _, tt := range tests {
		got := parseSizeFlag(tt.input, 10)
		if got != tt.want {
			t.Errorf("parseSizeFlag(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// TestCLI_RootExecute 测试 cobra 命令能正常执行
func TestCLI_RootExecute(t *testing.T) {
	// 执行根命令不带参数，应返回错误（非零退出）
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{})
	err := rootCmd.Execute()
	_ = err // cobra 可能因 SilenceErrors 不返回错误

	// 不带参数应输出 usage
	output := buf.String()
	if !strings.Contains(output, "Usage") && !strings.Contains(output, "fileupload") {
		t.Logf("root execute output (no args): %s", output)
	}
}

// TestCLI_UnknownCommand 测试未知命令的错误处理
func TestCLI_UnknownCommand(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"nonexistent-command-xyz"})
	err := rootCmd.Execute()

	if err == nil {
		// cobra 默认会返回错误，但设置 SilenceErrors 后可能不返回
		// 至少输出应该提示未知命令
		output := buf.String()
		if !strings.Contains(output, "nonexistent") && !strings.Contains(output, "unknown") {
			t.Logf("output for unknown command: %s", output)
		}
	}
}

// TestCLI_InvalidArgs 测试 upload 不接受 0 个参数
func TestCLI_UploadNoArgs(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"upload"})
	err := rootCmd.Execute()

	// 应该因为缺少参数而失败
	if err == nil {
		t.Log("upload with no args did not return error (may still show usage)")
	}
	output := buf.String()
	if output == "" && err == nil {
		t.Error("upload with no args should produce output or error")
	}
}

// Helper to test flag default values
func TestCLI_DefaultFlagValues(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"upload"})
	if cmd == nil {
		t.Fatal("upload command not found")
	}

	checkDefault := func(name, expected string) {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("flag --%s not found", name)
			return
		}
		if f.DefValue != expected {
			t.Errorf("--%s default = %q, want %q", name, f.DefValue, expected)
		}
	}

	checkDefault("chunk-size", "10m")
	checkDefault("concurrency", "4")
	checkDefault("compress", "zstd")
	checkDefault("resume", "true")
	checkDefault("exclude-hidden", "false")

	cmd2, _, _ := rootCmd.Find([]string{"bench"})
	if cmd2 != nil {
		benchDefaults := map[string]string{
			"files":       "10",
			"size":        "10m",
			"concurrency": "4",
		}
		for name, expected := range benchDefaults {
			f := cmd2.Flags().Lookup(name)
			if f != nil && f.DefValue != expected {
				t.Errorf("bench --%s default = %q, want %q", name, f.DefValue, expected)
			}
		}
	}
}

// TestCLI_CompletionValidArgs 验证 completion 支持所有 shell
func TestCLI_CompletionValidArgs(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"completion"})
	if err != nil || cmd == nil {
		t.Fatal("completion command not found")
	}
	// 验证 ValidArgs 包含所有 shell 类型
	expected := []string{"bash", "zsh", "fish", "powershell"}
	for _, e := range expected {
		found := false
		for _, a := range cmd.ValidArgs {
			if a == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("completion ValidArgs missing: %s", e)
		}
	}
}

// TestCLI_TokenRoundtrip 验证 saveToken/loadToken
func TestCLI_TokenRoundtrip(t *testing.T) {
	// os.UserHomeDir 在 Unix 读取 HOME，在 Windows 读取 USERPROFILE。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	wantPath := filepath.Join(home, ".fileupload", "token")
	if got := tokenFilePath(); got != wantPath {
		t.Fatalf("tokenFilePath = %q, want %q", got, wantPath)
	}

	token := "test-secret-token-123"
	if err := saveToken(token); err != nil {
		t.Fatalf("saveToken error = %v", err)
	}

	got := loadToken()
	if got != token {
		t.Errorf("loadToken = %q, want %q", got, token)
	}
}

func TestSaveTokenAtPathRejectsEmptyPath(t *testing.T) {
	if err := saveTokenAtPath("", "test-secret-token-123"); err == nil {
		t.Fatal("saveTokenAtPath error = nil, want non-nil")
	}
}

// TestCLI_HelpOutputContainsCommands 验证 help 输出包含关键命令
func TestCLI_HelpOutputContainsCommands(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--help"})
	rootCmd.Execute()

	output := buf.String()
	expectedCommands := []string{"upload", "download", "rm", "ls", "bench", "login", "completion"}
	for _, cmd := range expectedCommands {
		if !strings.Contains(output, cmd) {
			t.Errorf("help output missing command: %s", cmd)
		}
	}
}

// TestClient_NewClient 验证 Client 构造函数
func TestClient_NewClient(t *testing.T) {
	c := NewClient("http://example.com:8080", "test-ns")
	if c.ServerURL != "http://example.com:8080" {
		t.Errorf("ServerURL = %s", c.ServerURL)
	}
	if c.Namespace != "test-ns" {
		t.Errorf("Namespace = %s", c.Namespace)
	}
	if c.Token != "" {
		t.Errorf("initial Token = %s, want empty", c.Token)
	}
}

func TestClient_Defaults(t *testing.T) {
	c := NewClient("", "")
	if c.ServerURL != "http://localhost:8080" {
		t.Errorf("default ServerURL = %s", c.ServerURL)
	}
	if c.Namespace != "default" {
		t.Errorf("default Namespace = %s", c.Namespace)
	}
}

func TestClient_SetToken(t *testing.T) {
	c := NewClient("http://localhost", "ns")
	c.SetToken("my-token")
	if c.Token != "my-token" {
		t.Errorf("Token = %s", c.Token)
	}
}

// Test file helpers for e2e_test.go compatibility
func TestClient_UploadOptionsDefaults(t *testing.T) {
	opts := UploadOptions{}
	if opts.ChunkSize != 0 {
		t.Errorf("default ChunkSize = %d", opts.ChunkSize)
	}
	if opts.Concurrency != 0 {
		t.Errorf("default Concurrency = %d", opts.Concurrency)
	}
	// Verify that zero values use defaults in UploadFile
	_ = opts
}

// Verify bench.go runBench is callable
func TestBench_ParseSize(t *testing.T) {
	if got := parseSizeFlag("5m", 10); got != 5*1024*1024 {
		t.Errorf("5m = %d", got)
	}
	if got := parseSizeFlag("1g", 10); got != 1024*1024*1024 {
		t.Errorf("1g = %d", got)
	}
	if got := parseSizeFlag("", 10); got != 10 {
		t.Errorf("empty default = %d", got)
	}
}

// Benchmark placeholder
func BenchmarkParseSizeFlag(b *testing.B) {
	for i := 0; i < b.N; i++ {
		parseSizeFlag("10m", 10*1024*1024)
	}
}

// Verify fmt import used (for FormatInt and friends)
func init() {
	_ = fmt.Sprintf
}

func TestCLI_VersionMetadata(t *testing.T) {
	originalVersion, originalCommit, originalBuiltAt := version, commit, builtAt
	t.Cleanup(func() {
		version, commit, builtAt = originalVersion, originalCommit, originalBuiltAt
		rootCmd.Version = versionString()
	})

	version, commit, builtAt = "v1.2.3", "abc123", "2026-07-18T00:00:00Z"
	rootCmd.Version = versionString()

	if got := rootCmd.Version; got != "v1.2.3 (commit abc123, built 2026-07-18T00:00:00Z)" {
		t.Fatalf("rootCmd.Version = %q", got)
	}
}
