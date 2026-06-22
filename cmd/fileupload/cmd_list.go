package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:     "ls [dirID|/]",
		Short:   "列目录",
		Long:    "列目录内容。默认列出根目录。",
		Example: "  fileupload ls /\n  fileupload ls dir_abc",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			parentID := "/"
			if len(args) > 0 {
				parentID = args[0]
			}
			c := getClient()

			res, err := c.List(ctx, parentID)
			if err != nil {
				return fmt.Errorf("列出目录失败: %w", err)
			}
			if len(res.Children) == 0 {
				fmt.Println("(空目录)")
				return nil
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
			return nil
		},
	})
}
