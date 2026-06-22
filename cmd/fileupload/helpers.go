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
)

// isDir 通过 stat 判断 ID 是否为目录。
func isDir(ctx context.Context, c *Client, id string) (bool, error) {
	res, err := c.Stat(ctx, id)
	if err != nil {
		return false, err
	}
	if res == nil || res.File == nil {
		return false, nil
	}
	if d, ok := res.File["is_dir"].(bool); ok {
		return d, nil
	}
	return false, nil
}

// downloadFileToFile 下载单个文件到本地路径。
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

	total := resp.ContentLength
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

// downloadDirToFile 下载目录打包文件到本地路径。
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

	p := NewProgress(resp.ContentLength, "Downloading dir")
	reader := NewProgressReader(resp.Body, p)

	_, err = io.Copy(out, reader)
	if err != nil {
		return err
	}

	p.Done()
	_ = verify

	treeSHA := resp.Header.Get("X-Tree-SHA256")
	if treeSHA != "" {
		fmt.Printf("Tree SHA-256: %s\n", treeSHA)
	}
	return nil
}
