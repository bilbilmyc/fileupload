package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runDownload(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload download <fileID|dirID> [-o <path>] [--format tar.gz] [--verify] [--range start-end] [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}

	id := args[0]
	flags := parseFlags(args[1:])
	serverURL := getServerURL(cfg, flags)
	outputPath := getFlag(flags, "o", "")
	format := getFlag(flags, "format", "tar.gz")
	verify := getFlag(flags, "verify", "false") == "true"
	rangeStr := getFlag(flags, "range", "")
	namespace := getFlag(flags, "namespace", "default")

	if outputPath == "" {
		outputPath = id
	}

	// 先尝试 stat 判断是文件还是目录
	isDir := checkIsDir(ctx, serverURL, id, namespace)

	if isDir {
		downloadDir(ctx, serverURL, id, outputPath, format, namespace)
	} else {
		downloadFile(ctx, serverURL, id, outputPath, rangeStr, verify, namespace)
	}
}

func downloadFile(ctx context.Context, serverURL, fileID, outputPath, rangeStr string, verify bool, namespace string) {
	url := fmt.Sprintf("%s/v1/files/%s?namespace=%s", serverURL, fileID, namespace)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)

	if rangeStr != "" {
		req.Header.Set("Range", "bytes="+rangeStr)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("错误: 下载失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: 下载失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	// 创建输出文件
	out, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("错误: 创建输出文件失败: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		fmt.Printf("错误: 写入文件失败: %v\n", err)
		os.Exit(1)
	}

	sha256 := resp.Header.Get("X-SHA256")
	fmt.Printf("下载完成: %s (%d 字节)\n", outputPath, written)
	if sha256 != "" {
		fmt.Printf("服务端 SHA-256: %s\n", sha256)
	}
}

func downloadDir(ctx context.Context, serverURL, dirID, outputPath, format, namespace string) {
	url := fmt.Sprintf("%s/v1/dirs/%s?format=%s&namespace=%s", serverURL, dirID, format, namespace)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("错误: 目录下载失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: 目录下载失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	// 确保输出路径有正确扩展名
	if !strings.HasSuffix(outputPath, ".tar.gz") && !strings.HasSuffix(outputPath, ".tar.zst") {
		outputPath += "." + format
	}

	out, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("错误: 创建输出文件失败: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		fmt.Printf("错误: 写入文件失败: %v\n", err)
		os.Exit(1)
	}

	treeSHA := resp.Header.Get("X-Tree-SHA256")
	fmt.Printf("目录下载完成: %s (%d 字节)\n", outputPath, written)
	if treeSHA != "" {
		fmt.Printf("目录 Tree SHA-256: %s\n", treeSHA)
	}
}

func checkIsDir(ctx context.Context, serverURL, id, namespace string) bool {
	url := fmt.Sprintf("%s/v1/stat/%s?namespace=%s", serverURL, id, namespace)
	resp, err := http.DefaultClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// 通过解析 response 判断 is_dir
	// 简化：如果 id 以 "dir_" 开头则视为目录
	return strings.HasPrefix(id, "dir_") || resp.StatusCode != http.StatusOK
}
