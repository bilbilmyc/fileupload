package main

import (
	"context"
	"fmt"
	"os"

	"github.com/bilbilmyc/fileupload/internal/config"
)

func runScan(ctx context.Context, cfg config.Config) {
	flags := parseFlags(os.Args[2:])
	c := newClientFromFlags(flags, cfg)
	fmt.Printf("触发服务端一致性巡检: %s\n", c.ServerURL)

	res, err := c.Scan(ctx)
	if err != nil {
		fmt.Printf("错误: 巡检失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n巡检报告:")
	for k, v := range res {
		fmt.Printf("  %s: %v\n", k, v)
	}
}
