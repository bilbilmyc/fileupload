package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	var (
		outputPath string
		format     string
		verify     bool
		rng        string
	)

	downloadCmd := &cobra.Command{
		Use:   "download <fileID|dirID>",
		Short: "下载文件或目录",
		Long: `下载文件或目录。

单文件支持 Range 分段下载和 SHA-256 校验；目录会流式打包成 tar.gz / tar.zst。`,
		Example: `  fileupload download abc123 -o output.bin
  fileupload download dir_abc -o project.tar.gz --format tar.gz`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			id := args[0]
			c := getClient()

			isDir, err := isDir(ctx, c, id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "警告: 无法判断类型 (%v)，按文件处理\n", err)
				isDir = false
			}

			if isDir {
				if err := downloadDirToFile(ctx, c, id, outputPath, format, verify); err != nil {
					return fmt.Errorf("下载目录失败: %w", err)
				}
				return nil
			}
			if err := downloadFileToFile(ctx, c, id, outputPath, rng, verify); err != nil {
				return fmt.Errorf("下载失败: %w", err)
			}
			return nil
		},
	}

	downloadCmd.Flags().StringVarP(&outputPath, "output", "o", "", "输出路径（默认使用 fileID/dirID）")
	downloadCmd.Flags().StringVar(&format, "format", "tar.gz", "目录打包格式 tar.gz|tar.zst")
	downloadCmd.Flags().BoolVar(&verify, "verify", false, "下载后校验 SHA-256")
	downloadCmd.Flags().StringVar(&rng, "range", "", "分段下载，如 0-1048575")

	rootCmd.AddCommand(downloadCmd)
}
