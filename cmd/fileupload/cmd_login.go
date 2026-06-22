package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "login [server]",
		Short: "登录服务端（预留）",
		Long: `登录服务端并保存 token。

当前为预留命令，真实鉴权将在后续版本实现。可先用 --token 手动指定。`,
		Example: "  fileupload login http://localhost:8080",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("login 命令已预留。请使用 --server 和 --token 进行认证。")
			return nil
		},
	})
}
