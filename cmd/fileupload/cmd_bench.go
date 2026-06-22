package main

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	var (
		files       int
		size        string
		concurrency int
	)

	benchCmd := &cobra.Command{
		Use:   "bench",
		Short: "压测",
		Long:  `上传压测：生成临时文件并并发上传。`,
		Example: `  fileupload bench --files 50 --size 100m --concurrency 16`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			c := getClient()
			return runBench(ctx, c, files, size, concurrency)
		},
	}

	benchCmd.Flags().IntVar(&files, "files", 10, "文件数量")
	benchCmd.Flags().StringVar(&size, "size", "10m", "每个文件大小")
	benchCmd.Flags().IntVar(&concurrency, "concurrency", 4, "并发数")

	rootCmd.AddCommand(benchCmd)
}
