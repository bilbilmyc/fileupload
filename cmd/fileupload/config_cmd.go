package main

import (
	"fmt"

	"github.com/mayc/casdao/fileupload/internal/config"
)

func runConfig(cfg config.Config) {
	yaml, err := cfg.DumpYAML()
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	fmt.Println(yaml)
}
