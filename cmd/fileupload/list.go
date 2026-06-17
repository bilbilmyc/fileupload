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

func runList(ctx context.Context, cfg config.Config, args []string) {
	parentID := "/"
	if len(args) > 0 {
		parentID = args[0]
	}
	flags := parseFlags(args[1:])
	serverURL := getServerURL(cfg, flags)
	namespace := getFlag(flags, "namespace", "default")

	url := fmt.Sprintf("%s/v1/ls?parent=%s&namespace=%s", serverURL, parentID, namespace)
	resp, err := http.DefaultClient.Get(url)
	if err != nil {
		fmt.Printf("错误: 列出目录失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: 列出目录失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var result struct {
		Dir      any     `json:"dir"`
		Children []any   `json:"children"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Children) == 0 {
		fmt.Println("(空目录)")
		return
	}

	for _, child := range result.Children {
		c := child.(map[string]any)
		name := c["name"].(string)
		isDir := false
		if d, ok := c["is_dir"].(bool); ok {
			isDir = d
		}
		size := int64(0)
		if s, ok := c["size"].(float64); ok {
			size = int64(s)
		}
		fileID := c["file_id"].(string)

		prefix := "📄 "
		if isDir {
			prefix = "📁 "
		}
		fmt.Printf("%s%s  (%s, %d 字节)\n", prefix, name, fileID, size)
	}
}
