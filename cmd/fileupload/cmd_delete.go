package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:     "rm <fileID|dirID>",
		Short:   "删除文件或目录",
		Example: "  fileupload rm abc123",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			id := args[0]
			c := getClient()

			isDir, err := isDir(ctx, c, id)
			if err != nil {
				return fmt.Errorf("判断类型失败: %w", err)
			}
			if isDir {
				err = c.DeleteDir(ctx, id)
			} else {
				err = c.Delete(ctx, id)
			}
			if err != nil {
				return fmt.Errorf("删除失败: %w", err)
			}
			fmt.Printf("已删除: %s\n", id)
			return nil
		},
	})
}
