package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runScan(ctx context.Context, cfg config.Config) {
	serverURL := os.Getenv("FILEUPLOAD_SERVER")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	fmt.Printf("触发服务端一致性巡检: %s\n", serverURL)

	url := fmt.Sprintf("%s/v1/admin/scan", serverURL)
	resp, err := http.DefaultClient.Post(url, "application/json", nil)
	if err != nil {
		fmt.Printf("错误: 巡检请求失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: 巡检失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var report map[string]any
	json.NewDecoder(resp.Body).Decode(&report)

	fmt.Println("\n巡检报告:")
	fmt.Printf("  孤儿临时文件: %.0f\n", report["orphan_parts"].(float64))
	fmt.Printf("  孤儿物理文件: %d\n", len(report["orphan_files"].([]any)))
	fmt.Printf("  元数据孤儿:   %.0f\n", report["metadata_orphans"].(float64))
	fmt.Printf("  引用计数修复: %.0f\n", report["ref_count_fixes"].(float64))
}
