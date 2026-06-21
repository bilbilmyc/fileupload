package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/config"
)

func runList(ctx context.Context, cfg config.Config, args []string) {
	// 从 args 中分离 flags 和位置参数
	var positional []string
	var flagArgs []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			flagArgs = append(flagArgs, args[i])
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}

	parentID := ""
	if len(positional) > 0 {
		parentID = positional[0]
	}
	flags := parseFlags(flagArgs)
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
