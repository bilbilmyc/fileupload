package main

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	var (
		files         int
		size          string
		concurrency   int
		seed          int64
		cleanup       bool
		jsonOutput    bool
		maxErrorRate  float64
		minThroughput float64
	)

	benchCmd := &cobra.Command{
		Use:     "bench",
		Short:   "运行可重复上传压测",
		Long:    `生成确定性测试数据，并发上传，输出吞吐、错误率和 p50/p95/p99 延迟。默认在测试后永久清理上传文件。`,
		Example: `  fileupload bench --files 100 --size 10m --concurrency 8 --seed 20260719 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			c := getClient()
			_, err := runBench(ctx, c, benchOptions{
				Files:         files,
				FileSize:      parseSizeFlag(size, 10*1024*1024),
				Concurrency:   concurrency,
				Seed:          seed,
				Cleanup:       cleanup,
				JSON:          jsonOutput,
				MaxErrorRate:  maxErrorRate,
				MinThroughput: minThroughput,
			})
			return err
		},
	}

	benchCmd.Flags().IntVar(&files, "files", 10, "文件数量")
	benchCmd.Flags().StringVar(&size, "size", "10m", "每个文件大小")
	benchCmd.Flags().IntVar(&concurrency, "concurrency", 4, "并发数")
	benchCmd.Flags().Int64Var(&seed, "seed", 1, "确定性测试数据种子")
	benchCmd.Flags().BoolVar(&cleanup, "cleanup", true, "测试后删除并永久清理上传文件")
	benchCmd.Flags().BoolVar(&jsonOutput, "json", false, "以 JSON 输出结果")
	benchCmd.Flags().Float64Var(&maxErrorRate, "max-error-rate", 0, "允许的最大错误率（0-1）")
	benchCmd.Flags().Float64Var(&minThroughput, "min-throughput", 0, "最低吞吐门槛（MiB/s，0 表示不检查）")

	rootCmd.AddCommand(benchCmd)
}
