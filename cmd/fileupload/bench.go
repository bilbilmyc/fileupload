package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runBench(ctx context.Context, cfg config.Config, args []string) {
	flags := parseFlags(args)
	numFiles := parseInt(flags, "files", 10)
	fileSize := parseSize(flags, "size", 1*1024*1024) // 默认 1MB
	concurrency := parseInt(flags, "concurrency", 4)

	fmt.Printf("压测: %d 文件 × %d 字节, 并发 %d\n", numFiles, fileSize, concurrency)

	c := newClientFromFlags(flags, cfg)
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

			name := fmt.Sprintf("bench-%d.dat", idx)
			_, err := c.uploadBytes(ctx, name, data, "", "none", UploadOptions{
				ChunkSize:   fileSize, // 单分片
				Concurrency: 1,
				Compress:    "none",
			})
			if err != nil {
				fmt.Printf("[%d] 上传失败: %v\n", idx, err)
				return
			}

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
