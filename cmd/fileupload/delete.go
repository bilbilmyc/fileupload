package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runDelete(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload rm <fileID|dirID> [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}

	id := args[0]
	flags := parseFlags(args[1:])
	serverURL := getServerURL(cfg, flags)
	namespace := getFlag(flags, "namespace", "default")

	// 检查是文件还是目录
	var url string
	if checkIsDir(ctx, serverURL, id, namespace) {
		url = fmt.Sprintf("%s/v1/dirs/%s?namespace=%s", serverURL, id, namespace)
	} else {
		url = fmt.Sprintf("%s/v1/files/%s?namespace=%s", serverURL, id, namespace)
	}

	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("错误: 删除失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("错误: 删除失败 (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	fmt.Printf("已删除: %s\n", id)
}
