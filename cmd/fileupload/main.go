// Command fileupload 是文件上传下载服务的 CLI 客户端。
//
// 用法:
//
//	fileupload upload <path>                   上传文件或目录
//	fileupload download <fileID|dirID> [-o输出] 下载文件或目录
//	fileupload rm <fileID|dirID>              删除
//	fileupload ls <dirID|/>                   列目录
//	fileupload stat <fileID>                  文件信息
//	fileupload scan                           触发一致性巡检
//	fileupload bench                          压测
//	fileupload config                         查看配置
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// 加载配置
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	ctx := context.Background()
	cmd := os.Args[1]

	switch cmd {
	case "upload":
		runUpload(ctx, cfg, os.Args[2:])
	case "download":
		runDownload(ctx, cfg, os.Args[2:])
	case "rm":
		runDelete(ctx, cfg, os.Args[2:])
	case "ls":
		runList(ctx, cfg, os.Args[2:])
	case "stat":
		runStat(ctx, cfg, os.Args[2:])
	case "scan":
		runScan(ctx, cfg)
	case "bench":
		runBench(ctx, cfg, os.Args[2:])
	case "config":
		runConfig(cfg)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("未知命令: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`fileupload — 文件上传下载 CLI

用法:
  fileupload <command> [参数...]

全局参数（放在命令之前或之后均可）:
  --server <url>        服务端地址（默认 http://localhost:8080）
  --namespace <ns>      Namespace（默认 default）

命令:

  upload <path>         上传文件或目录
    --chunk-size 10m      分片大小，支持 k/m/g 后缀（默认 10MB）
    --concurrency 4       并发分片数（默认 4）
    --compress zstd       压缩格式 zstd|none（默认 zstd）
    --resume              启用断点续传（默认开启）
    --exclude-hidden      跳过以 . 开头的隐藏文件（默认不跳）
    示例:
      fileupload upload data.bin
      fileupload upload ./dir/ --concurrency 8
      fileupload upload ./git-project/ --exclude-hidden

  download <id>         下载文件或目录
    -o <path>             输出路径（默认使用 fileID/dirID）
    --format tar.gz       目录打包格式 tar.gz|tar.zst（默认 tar.gz）
    --verify              下载后校验 SHA-256
    --range start-end     分段下载，如 0-1048575
    示例:
      fileupload download abc123 -o out.bin
      fileupload download dir_xxx -o project.tar.gz --format tar.gz

  rm <id>               删除文件或目录
    示例:
      fileupload rm abc123

  ls <id>               列目录（可传 / 或 dirID）
    示例:
      fileupload ls /
      fileupload ls dir_xxx

  stat <id>             查看文件或目录元信息

  status <sessionID>    查看上传会话进度

  scan                  触发服务端一致性巡检

  bench                 压测
    --files 10            文件数量（默认 10）
    --size 10m            每个文件大小，支持 k/m/g（默认 10MB）
    --concurrency 4       并发数（默认 4）
    示例:
      fileupload bench --files 50 --size 100m --concurrency 16

  config                查看当前配置

  help                  显示本帮助
`)
}

// getServerURL 获取服务端地址
func getServerURL(cfg config.Config, args map[string]string) string {
	if u, ok := args["server"]; ok && u != "" {
		return u
	}
	// 环境变量或默认
	if u := os.Getenv("FILEUPLOAD_SERVER"); u != "" {
		return u
	}
	return "http://localhost:8080"
}
