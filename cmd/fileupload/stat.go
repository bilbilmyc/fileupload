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

func runStat(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload stat <fileID> [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}

	id := args[0]
	flags := parseFlags(args[1:])
	serverURL := getServerURL(cfg, flags)
	namespace := getFlag(flags, "namespace", "default")

	url := fmt.Sprintf("%s/v1/stat/%s?namespace=%s", serverURL, id, namespace)
	resp, err := http.DefaultClient.Get(url)
	if err != nil {
		fmt.Printf("错误: 查询失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: 查询失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	file := result["file"].(map[string]any)
	fmt.Printf("文件信息:\n")
	fmt.Printf("  FileID:    %s\n", file["file_id"])
	fmt.Printf("  名称:      %s\n", file["name"])
	fmt.Printf("  路径:      %s\n", file["path"])
	fmt.Printf("  大小:      %.0f 字节\n", file["size"].(float64))
	fmt.Printf("  Namespace: %s\n", file["namespace"])
	fmt.Printf("  目录:      %v\n", file["is_dir"])
	fmt.Printf("  SHA-256:   %s\n", file["sha256"])

	if blob, ok := result["blob"].(map[string]any); ok && blob != nil {
		fmt.Printf("  引用数:    %.0f\n", blob["ref_count"].(float64))
		fmt.Printf("  存储路径:  %s\n", blob["storage_path"])
	}
}
