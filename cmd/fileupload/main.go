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
	fmt.Print(`文件上传下载服务 CLI

用法:
  fileupload upload <path>           上传文件或目录
    --chunk-size 10m                   分片大小（默认 10MB）
    --concurrency 4                    并发分片数
    --compress zstd                    压缩格式（zstd / none，默认 zstd）
    --server http://localhost:8080     服务端地址
    --namespace <ns>                   namespace
    --resume                           断点续传（默认开启）

  fileupload download <fileID|dirID>  下载文件或目录
    -o <path>                          输出路径
    --format tar.gz                    目录打包格式
    --verify                           下载后校验 SHA-256
    --range start-end                  分段下载
    --server <url>
    --namespace <ns>

  fileupload rm <fileID|dirID>        删除文件或目录
    --server <url>
    --namespace <ns>

  fileupload ls <dirID|/>             列目录
    --server <url>
    --namespace <ns>

  fileupload stat <fileID>            文件信息
    --server <url>
    --namespace <ns>

  fileupload scan                     触发服务端一致性巡检
    --server <url>

  fileupload bench                    压测
    --files 10
    --size 10m
    --concurrency 4

  fileupload config                   查看当前配置

  fileupload help                     显示帮助
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
