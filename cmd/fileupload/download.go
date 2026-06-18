package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runDownload(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload download <fileID|dirID> [-o <path>] [--format tar.gz] [--verify] [--range start-end] [--server <url>] [--namespace <ns>]\n  运行 fileupload help 查看详细帮助")
		os.Exit(1)
	}
	id := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)
	outputPath := getFlag(flags, "o", id)
	format := getFlag(flags, "format", "tar.gz")
	verify := getFlag(flags, "verify", "false") == "true"
	rng := getFlag(flags, "range", "")

	isDir, err := isDir(ctx, c, id)
	if err != nil {
		// 如果 stat 失败，按文件处理
		fmt.Printf("警告: 无法判断类型 (%v)，按文件处理\n", err)
		isDir = false
	}

	if isDir {
		if err := downloadDirToFile(ctx, c, id, outputPath, format, verify); err != nil {
			fmt.Printf("错误: 下载目录失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := downloadFileToFile(ctx, c, id, outputPath, rng, verify); err != nil {
		fmt.Printf("错误: 下载失败: %v\n", err)
		os.Exit(1)
	}
}

func downloadFileToFile(ctx context.Context, c *Client, fileID, outputPath, rng string, verify bool) error {
	resp, err := c.DownloadFile(ctx, fileID, rng)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	var h hash.Hash
	var w io.Writer = out
	if verify {
		h = sha256.New()
		w = io.MultiWriter(out, h)
	}

	// 下载进度
	var total int64
	if rng == "" {
		total = resp.ContentLength
	} else {
		// Range 请求下 Content-Length 是实际分片大小
		total = resp.ContentLength
	}
	p := NewProgress(total, "Downloading")
	reader := NewProgressReader(resp.Body, p)

	_, err = io.Copy(w, reader)
	if err != nil {
		return err
	}

	p.Done()

	serverSHA := resp.Header.Get("X-SHA256")
	if verify && serverSHA != "" && h != nil {
		got := hex.EncodeToString(h.Sum(nil))
		if got != serverSHA {
			return fmt.Errorf("SHA-256 mismatch: got %s want %s", got, serverSHA)
		}
	}
	if serverSHA != "" {
		fmt.Printf("SHA-256: %s\n", serverSHA)
	}
	return nil
}

func downloadDirToFile(ctx context.Context, c *Client, dirID, outputPath, format string, verify bool) error {
	if !strings.HasSuffix(outputPath, ".tar.gz") && !strings.HasSuffix(outputPath, ".tar.zst") {
		outputPath += "." + format
	}

	resp, err := c.DownloadDir(ctx, dirID, format)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// 目录下载进度
	p := NewProgress(resp.ContentLength, "Downloading dir")
	reader := NewProgressReader(resp.Body, p)

	_, err = io.Copy(out, reader)
	if err != nil {
		return err
	}

	p.Done()
	_ = verify // 目录下载暂不支持 SHA-256 逐条目校验

	treeSHA := resp.Header.Get("X-Tree-SHA256")
	if treeSHA != "" {
		fmt.Printf("Tree SHA-256: %s\n", treeSHA)
	}
	return nil
}
