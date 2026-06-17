package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runUpload(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload upload <path> [--chunk-size 10m] [--concurrency 4] [--compress zstd] [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}

	localPath := args[0]
	flags := parseFlags(args[1:])

	serverURL := getServerURL(cfg, flags)
	chunkSize := parseSize(flags, "chunk-size", 10*1024*1024)
	concurrency := parseInt(flags, "concurrency", 4)
	compress := getFlag(flags, "compress", "zstd")
	namespace := getFlag(flags, "namespace", "default")

	// 检查本地路径
	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Printf("错误: 无法访问 %s: %v\n", localPath, err)
		os.Exit(1)
	}

	if info.IsDir() {
		uploadDir(ctx, serverURL, localPath, namespace, compress)
	} else {
		uploadFile(ctx, serverURL, localPath, namespace, compress, chunkSize, concurrency)
	}
}

// uploadFile 上传单个文件
func uploadFile(ctx context.Context, serverURL, localPath, namespace, compress string, chunkSize int64, concurrency int) {
	file, err := os.Open(localPath)
	if err != nil {
		fmt.Printf("错误: 打开文件失败: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	info, _ := file.Stat()
	fileSize := info.Size()
	fileName := info.Name()

	fmt.Printf("上传: %s (%d 字节)\n", fileName, fileSize)

	// 计算文件 SHA-256（秒传预检用）
	originalSHA := computeFileSHA256(localPath)
	fmt.Printf("原始 SHA-256: %s\n", originalSHA)

	// 秒传预检
	exists := checkExist(ctx, serverURL, originalSHA, namespace, fileName)
	if exists {
		fmt.Println("秒传命中！文件已存在，无需上传。")
		return
	}

	// 创建上传会话
	sessionID := createUploadSession(ctx, serverURL, fileSize, originalSHA, compress, chunkSize, namespace, fileName)
	fmt.Printf("会话 ID: %s\n", sessionID)

	// 分片上传
	uploadChunks(ctx, serverURL, sessionID, file, fileSize, chunkSize, concurrency, namespace)

	// Finalize
	finalizeUpload(ctx, serverURL, sessionID)
}

// uploadDir 上传目录
func uploadDir(ctx context.Context, serverURL, dirPath, namespace, compress string) {
	fmt.Printf("上传目录: %s\n", dirPath)

	var entries []struct {
		Path   string `json:"path"`
		FileID string `json:"file_id"`
	}

	// 递归遍历目录
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(dirPath, path)

		// 上传每个文件
		uploadFile(ctx, serverURL, path, namespace, compress, 10*1024*1024, 4)
		// 这里简化处理，实际需要获取上传后的 fileID

		entries = append(entries, struct {
			Path   string `json:"path"`
			FileID string `json:"file_id"`
		}{
			Path:   relPath,
			FileID: "", // 需要从上传结果获取
		})
		return nil
	})

	fmt.Printf("目录上传完成，%d 个文件\n", len(entries))
}

// computeFileSHA256 计算文件的 SHA-256
func computeFileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// checkExist 秒传预检
func checkExist(ctx context.Context, serverURL, sha256, namespace, name string) bool {
	req, _ := http.NewRequestWithContext(ctx, "HEAD", serverURL+"/v1/files?sha256="+sha256+"&namespace="+namespace+"&name="+name, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// createUploadSession 创建上传会话
func createUploadSession(ctx context.Context, serverURL string, fileSize int64, sha256, compress string, chunkSize int64, namespace, fileName string) string {
	req, _ := http.NewRequestWithContext(ctx, "POST", serverURL+"/uploads", nil)
	req.Header.Set("Upload-Length", fmt.Sprintf("%d", fileSize))
	req.Header.Set("X-SHA256", sha256)
	req.Header.Set("X-Compression", compress)
	req.Header.Set("X-Chunk-Size", fmt.Sprintf("%d", chunkSize))
	req.Header.Set("X-File-Name", fileName)
	req.Header.Set("X-Namespace", namespace)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("错误: 创建会话失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: 创建会话失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	location := resp.Header.Get("Location")
	// 提取 sessionID
	sessionID := location
	if len(location) > 9 && location[:9] == "/uploads/" {
		sessionID = location[9:]
	}
	return sessionID
}

// uploadChunks 分片上传文件
func uploadChunks(ctx context.Context, serverURL, sessionID string, file *os.File, fileSize, chunkSize int64, concurrency int, namespace string) {
	// 计算总分片数
	totalChunks := int(fileSize / chunkSize)
	if fileSize%chunkSize != 0 {
		totalChunks++
	}

	fmt.Printf("分片上传: %d 个分片 (并发 %d)\n", totalChunks, concurrency)

	// 控制并发数
	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, totalChunks)

	for i := 0; i < totalChunks; i++ {
		sem <- struct{}{}
		go func(index int) {
			defer func() { <-sem }()
			offset := int64(index) * chunkSize
			size := chunkSize
			if offset+size > fileSize {
				size = fileSize - offset
			}

			// 读取分片数据
			buf := make([]byte, size)
			file.ReadAt(buf, offset)

			// 计算分片 SHA-256
			h := sha256.Sum256(buf)
			sliceSHA := hex.EncodeToString(h[:])

			// 发送分片
			url := fmt.Sprintf("%s/uploads/%s", serverURL, sessionID)
			req, _ := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(buf))
			req.Header.Set("Upload-Offset", fmt.Sprintf("%d", offset))
			req.Header.Set("X-Slice-SHA256", sliceSHA)
			req.Header.Set("X-Slice-Index", fmt.Sprintf("%d", index))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errCh <- fmt.Errorf("分片 %d 上传失败: %w", index, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				body, _ := io.ReadAll(resp.Body)
				errCh <- fmt.Errorf("分片 %d 上传失败 (%d): %s", index, resp.StatusCode, string(body))
				return
			}

			fmt.Printf("✓ 分片 %d/%d 完成\n", index+1, totalChunks)
		}(i)
	}

	// 等待所有分片完成
	for i := 0; i < totalChunks; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				os.Exit(1)
			}
		default:
		}
	}
}

// finalizeUpload 触发服务端合并
func finalizeUpload(ctx context.Context, serverURL, sessionID string) {
	url := fmt.Sprintf("%s/v1/uploads/%s/finalize", serverURL, sessionID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("错误: Finalize 失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: Finalize 失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("上传完成!\n")
	fmt.Printf("  FileID: %s\n", result["file_id"])
	fmt.Printf("  SHA-256: %s\n", result["sha256"])
	fmt.Printf("  Size: %d 字节\n", int64(result["size"].(float64)))
}

// ---- 辅助函数 ----

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
