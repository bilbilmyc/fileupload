package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "config",
		Short: "查看当前配置",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}
			yaml, err := cfg.DumpYAML()
			if err != nil {
				return fmt.Errorf("导出配置失败: %w", err)
			}
			fmt.Println(yaml)
			return nil
		},
	})
}
