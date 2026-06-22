package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:     "stat <fileID|dirID>",
		Short:   "查看文件或目录元信息",
		Example: "  fileupload stat abc123",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			id := args[0]
			c := getClient()

			res, err := c.Stat(ctx, id)
			if err != nil {
				return fmt.Errorf("查询失败: %w", err)
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
			return nil
		},
	})
}
