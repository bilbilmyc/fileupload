package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/config"
)

func runStatus(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 || args[0] == "" {
		fmt.Println("用法: fileupload status <sessionID> [--server <url>] [--namespace <ns>]\n  运行 fileupload help 查看详细帮助")
		os.Exit(1)
	}

	// 分离 flags 和位置参数
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

	if len(positional) == 0 {
		fmt.Println("用法: fileupload status <sessionID> [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}

	flags := parseFlags(flagArgs)
	c := newClientFromFlags(flags, cfg)
	sessionID := positional[0]

	chunks, total, err := c.GetStatus(ctx, sessionID)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("会话: %s\n", sessionID)
	fmt.Printf("已上传: %s (%d 分片)\n", humanBytes(total), len(chunks))
	if len(chunks) > 0 {
		fmt.Println("分片:")
		for _, ch := range chunks {
			fmt.Printf("  [%d] %s offset=%d\n", ch.Index, humanBytes(ch.Size), ch.Offset)
		}
	}
}
