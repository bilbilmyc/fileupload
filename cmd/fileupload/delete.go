package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runDelete(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload rm <fileID|dirID> [--server <url>] [--namespace <ns>]\n  运行 fileupload help 查看详细帮助")
		os.Exit(1)
	}
	id := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	isDir, err := isDir(ctx, c, id)
	if err != nil {
		fmt.Printf("错误: 判断类型失败: %v\n", err)
		os.Exit(1)
	}
	if isDir {
		err = c.DeleteDir(ctx, id)
	} else {
		err = c.Delete(ctx, id)
	}
	if err != nil {
		fmt.Printf("错误: 删除失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已删除: %s\n", id)
}

func isDir(ctx context.Context, c *Client, id string) (bool, error) {
	// 快速判断：以 dir_ 开头的 ID 视为目录
	if len(id) > 4 && id[:4] == "dir_" {
		return true, nil
	}
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
