package main

import (
	"context"
	"fmt"
	"os"

	"github.com/bilbilmyc/fileupload/internal/config"
)

func runList(ctx context.Context, cfg config.Config, args []string) {
	parentID := "/"
	if len(args) > 0 {
		parentID = args[0]
	}
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	res, err := c.List(ctx, parentID)
	if err != nil {
		fmt.Printf("错误: 列出目录失败: %v\n", err)
		os.Exit(1)
	}
	if len(res.Children) == 0 {
		fmt.Println("(空目录)")
		return
	}
	for _, child := range res.Children {
		name, _ := child["name"].(string)
		fileID, _ := child["file_id"].(string)
		isDir, _ := child["is_dir"].(bool)
		size := int64(0)
		if s, ok := child["size"].(float64); ok {
			size = int64(s)
		}
		prefix := "📄 "
		if isDir {
			prefix = "📁 "
		}
		fmt.Printf("%s%s  (%s, %s)\n", prefix, name, fileID, humanBytes(size))
	}
}
