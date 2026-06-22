package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:     "status <sessionID>",
		Short:   "查看上传会话进度",
		Example: "  fileupload status sess_abc",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			sessionID := args[0]
			c := getClient()

			chunks, total, err := c.GetStatus(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("查询失败: %w", err)
			}
			fmt.Printf("会话: %s\n", sessionID)
			fmt.Printf("已上传: %s (%d 分片)\n", humanBytes(total), len(chunks))
			if len(chunks) > 0 {
				fmt.Println("分片:")
				for _, ch := range chunks {
					fmt.Printf("  [%d] %s\n", ch.Index, humanBytes(ch.Size))
				}
			}
			return nil
		},
	})
}
