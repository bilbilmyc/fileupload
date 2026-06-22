package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/config"
	"github.com/spf13/cobra"
)

var (
	serverURL string
	namespace string
	cfgFile   string
	token     string
)

var rootCmd = &cobra.Command{
	Use:   "fileupload",
	Short: "fileupload — 文件上传下载 CLI",
	Long: `fileupload — 文件上传下载 CLI

支持单文件/目录上传、下载、管理、秒传、断点续传、压测。`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "服务端地址（默认 http://localhost:8080）")
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace（默认 default）")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "X-Auth-Token 认证令牌")

	rootCmd.CompletionOptions.HiddenDefaultCmd = true
}

// getClient 根据全局 flag 创建 Client（复用现有配置加载）。
func getClient() *Client {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}
	url := getServerURL(cfg, map[string]string{"server": serverURL})
	ns := namespace
	if ns == "" {
		ns = "default"
	}
	c := NewClient(url, ns)

	// 认证令牌：优先 --token flag，其次 token 文件
	if token != "" {
		c.SetToken(token)
	} else if t := loadToken(); t != "" {
		c.SetToken(t)
	}
	return c
}

// tokenFilePath 返回令牌文件路径
func tokenFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".fileupload", "token")
}

// saveToken 保存令牌到 ~/.fileupload/token
func saveToken(tok string) error {
	dir := filepath.Dir(tokenFilePath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(tokenFilePath(), []byte(tok), 0600)
}

// loadToken 从 ~/.fileupload/token 读取令牌
func loadToken() string {
	p := tokenFilePath()
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func loadConfig() (config.Config, error) {
	return config.Load(cfgFile)
}

// getServerURL 获取服务端地址
func getServerURL(cfg config.Config, args map[string]string) string {
	if u, ok := args["server"]; ok && u != "" {
		return u
	}
	if u := os.Getenv("FILEUPLOAD_SERVER"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// getServerURLFromConfig 兼容性保留，当前未使用 config 字段。
func getServerURLFromConfig(cfg config.Config) string {
	_ = cfg
	if u := os.Getenv("FILEUPLOAD_SERVER"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// trimServerURL 去掉末尾斜杠，确保与旧 Client 一致。
func trimServerURL(u string) string {
	return strings.TrimRight(u, "/")
}
