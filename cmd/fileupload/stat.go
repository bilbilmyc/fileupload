package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runStat(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload stat <fileID> [--server <url>] [--namespace <ns>]\n  运行 fileupload help 查看详细帮助")
		os.Exit(1)
	}
	id := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	res, err := c.Stat(ctx, id)
	if err != nil {
		fmt.Printf("错误: 查询失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("文件信息:")
	for k, v := range res.File {
		fmt.Printf("  %s: %v\n", k, v)
	}
	if res.Blob != nil {
		fmt.Println("  (blob)")
		for k, v := range res.Blob {
			fmt.Printf("    %s: %v\n", k, v)
		}
	}
}
