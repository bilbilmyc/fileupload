package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	var (
		chunkSize     string
		concurrency   int
		compress      string
		resume        bool
		excludeHidden bool
	)

	uploadCmd := &cobra.Command{
		Use:   "upload <path>",
		Short: "上传文件或目录",
		Long: `上传文件或目录。

支持单文件自动分片、zstd 压缩、秒传、断点续传。
目录上传会递归遍历并提交 manifest。`,
		Example: `  fileupload upload data.bin
  fileupload upload ./dir/ --concurrency 8 --compress zstd
  fileupload upload ./project/ --exclude-hidden`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			localPath := args[0]
			c := getClient()

			opts := UploadOptions{
				ChunkSize:     parseSizeFlag(chunkSize, 10*1024*1024),
				Concurrency:   concurrency,
				Compress:      compress,
				Resume:        resume,
				ExcludeHidden: excludeHidden,
			}

			info, err := os.Stat(localPath)
			if err != nil {
				return fmt.Errorf("无法访问 %s: %w", localPath, err)
			}

			if info.IsDir() {
				meta, err := c.UploadDir(ctx, localPath, opts)
				if err != nil {
					return fmt.Errorf("目录上传失败: %w", err)
				}
				fmt.Printf("目录上传完成! DirID: %s\n", meta.FileID)
				return nil
			}

			fmeta, err := c.UploadFile(ctx, localPath, opts)
			if err != nil {
				return fmt.Errorf("上传失败: %w", err)
			}
			fmt.Printf("上传完成! FileID: %s SHA-256: %s Size: %d 字节\n", fmeta.FileID, fmeta.SHA256, fmeta.Size)
			return nil
		},
	}

	uploadCmd.Flags().StringVar(&chunkSize, "chunk-size", "10m", "分片大小，支持 k/m/g 后缀")
	uploadCmd.Flags().IntVar(&concurrency, "concurrency", 4, "并发分片数")
	uploadCmd.Flags().StringVar(&compress, "compress", "zstd", "压缩格式 zstd|none")
	uploadCmd.Flags().BoolVar(&resume, "resume", true, "启用断点续传")
	uploadCmd.Flags().BoolVar(&excludeHidden, "exclude-hidden", false, "跳过隐藏文件")

	rootCmd.AddCommand(uploadCmd)
}

// parseSizeFlag parses size strings like "10m", "1g", "512k".
func parseSizeFlag(v string, defaultVal int64) int64 {
	if v == "" {
		return defaultVal
	}
	var n int64
	var unit string
	fmt.Sscanf(v, "%d%s", &n, &unit)
	if n <= 0 {
		return defaultVal
	}
	switch unit {
	case "k", "K":
		return n * 1024
	case "m", "M":
		return n * 1024 * 1024
	case "g", "G":
		return n * 1024 * 1024 * 1024
	default:
		return n
	}
}
