package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runUpload(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload upload <path> [--chunk-size 10m] [--concurrency 4] [--compress zstd|none] [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}

	localPath := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Printf("错误: 无法访问 %s: %v\n", localPath, err)
		os.Exit(1)
	}

	opts := UploadOptions{
		ChunkSize:   parseSize(flags, "chunk-size", 10*1024*1024),
		Concurrency: parseInt(flags, "concurrency", 4),
		Compress:    getFlag(flags, "compress", "zstd"),
		Resume:      getFlag(flags, "resume", "true") != "false",
	}

	if info.IsDir() {
		meta, err := c.UploadDir(ctx, localPath, opts)
		if err != nil {
			fmt.Printf("错误: 目录上传失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("目录上传完成! DirID: %s\n", meta.FileID)
		return
	}

	fmeta, err := c.UploadFile(ctx, localPath, opts)
	if err != nil {
		fmt.Printf("错误: 上传失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("上传完成! FileID: %s SHA-256: %s Size: %d 字节\n", fmeta.FileID, fmeta.SHA256, fmeta.Size)
}

// newClientFromFlags 从解析后的 flag 映射和配置创建 Client。
func newClientFromFlags(flags map[string]string, cfg config.Config) *Client {
	serverURL := getServerURL(cfg, flags)
	namespace := getFlag(flags, "namespace", "default")
	return NewClient(serverURL, namespace)
}

// ---- 辅助函数 ----

// parseFlags 解析命令行参数为 map。
func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if args[i][0] == '-' {
			key := args[i]
			if key[1] == '-' {
				key = key[2:]
			} else {
				key = key[1:]
			}
			if i+1 < len(args) && args[i+1][0] != '-' {
				flags[key] = args[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		}
	}
	return flags
}

func getFlag(flags map[string]string, key, defaultVal string) string {
	if v, ok := flags[key]; ok && v != "" {
		return v
	}
	return defaultVal
}

func parseInt(flags map[string]string, key string, defaultVal int) int {
	if v, ok := flags[key]; ok && v != "" {
		n := 0
		fmt.Sscanf(v, "%d", &n)
		if n > 0 {
			return n
		}
	}
	return defaultVal
}

func parseSize(flags map[string]string, key string, defaultVal int64) int64 {
	if v, ok := flags[key]; ok && v != "" {
		var n int64
		var unit string
		fmt.Sscanf(v, "%d%s", &n, &unit)
		if n > 0 {
			switch unit {
			case "k", "K":
				return n * 1024
			case "m", "M":
				return n * 1024 * 1024
			case "g", "G":
				return n * 1024 * 1024 * 1024
			default:
				return n
			}
		}
	}
	return defaultVal
}
