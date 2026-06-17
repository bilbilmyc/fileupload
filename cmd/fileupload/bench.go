package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runBench(ctx context.Context, cfg config.Config, args []string) {
	flags := parseFlags(args)
	serverURL := getServerURL(cfg, flags)
	numFiles := parseInt(flags, "files", 10)
	fileSize := parseSize(flags, "size", 1*1024*1024) // 默认 1MB
	concurrency := parseInt(flags, "concurrency", 4)

	fmt.Printf("压测: %d 文件 × %d 字节, 并发 %d, 服务端 %s\n",
		numFiles, fileSize, concurrency, serverURL)

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	start := time.Now()
	var totalBytes int64
	var mu sync.Mutex

	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer func() {
				<-sem
				wg.Done()
			}()

			// 生成随机数据
			data := make([]byte, fileSize)
			rand.Read(data)

			// 上传
			url := fmt.Sprintf("%s/uploads", serverURL)
			req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
			req.Header.Set("Upload-Length", fmt.Sprintf("%d", fileSize))
			req.Header.Set("X-Compression", "none")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Printf("[%d] 创建会话失败: %v\n", idx, err)
				return
			}
			location := resp.Header.Get("Location")
			resp.Body.Close()

			if location == "" {
				return
			}
			sessionID := location[9:] // 去掉 /uploads/

			// 单分片上传
			chunkURL := fmt.Sprintf("%s/uploads/%s", serverURL, sessionID)
			chunkReq, _ := http.NewRequestWithContext(ctx, "PATCH", chunkURL, bytes.NewReader(data))
			chunkReq.Header.Set("Upload-Offset", "0")
			chunkReq.Header.Set("X-Slice-Index", "0")

			chunkResp, err := http.DefaultClient.Do(chunkReq)
			if err != nil {
				fmt.Printf("[%d] 分片上传失败: %v\n", idx, err)
				return
			}
			chunkResp.Body.Close()

			// finalize
			finalURL := fmt.Sprintf("%s/v1/uploads/%s/finalize", serverURL, sessionID)
			finalReq, _ := http.NewRequestWithContext(ctx, "POST", finalURL, nil)
			finalResp, err := http.DefaultClient.Do(finalReq)
			if err != nil {
				return
			}
			io.Copy(io.Discard, finalResp.Body)
			finalResp.Body.Close()

			mu.Lock()
			totalBytes += fileSize
			mu.Unlock()

			if (idx+1)%10 == 0 {
				fmt.Printf("  %d/%d 完成\n", idx+1, numFiles)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)
	throughput := float64(totalBytes) / elapsed.Seconds() / 1024 / 1024

	fmt.Printf("\n压测完成!\n")
	fmt.Printf("  总传输: %d MB\n", totalBytes/1024/1024)
	fmt.Printf("  耗时: %.2f 秒\n", elapsed.Seconds())
	fmt.Printf("  吞吐: %.2f MB/s\n", throughput)
	fmt.Printf("  平均延迟: %.0f ms\n", float64(elapsed.Milliseconds())/float64(numFiles))
}
