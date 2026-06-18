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
		fmt.Println("用法: fileupload download <fileID|dirID> [-o <path>] [--format tar.gz] [--verify] [--range start-end] [--server <url>] [--namespace <ns>]")
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

	written, err := io.Copy(w, resp.Body)
	if err != nil {
		return err
	}

	serverSHA := resp.Header.Get("X-SHA256")
	if verify && serverSHA != "" && h != nil {
		got := hex.EncodeToString(h.Sum(nil))
		if got != serverSHA {
			return fmt.Errorf("SHA-256 mismatch: got %s want %s", got, serverSHA)
		}
	}
	fmt.Printf("下载完成: %s (%d 字节)\n", outputPath, written)
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

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	treeSHA := resp.Header.Get("X-Tree-SHA256")
	fmt.Printf("目录下载完成: %s (%d 字节)\n", outputPath, written)
	if treeSHA != "" {
		fmt.Printf("Tree SHA-256: %s\n", treeSHA)
	}
	return nil
}
