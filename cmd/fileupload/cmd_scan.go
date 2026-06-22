package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:     "scan",
		Short:   "触发服务端一致性巡检",
		Example: "  fileupload scan",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			c := getClient()

			res, err := c.Scan(ctx)
			if err != nil {
				return fmt.Errorf("巡检失败: %w", err)
			}
			fmt.Println("巡检结果:")
			for k, v := range res {
				fmt.Printf("  %s: %v\n", k, v)
			}
			return nil
		},
	})
}
