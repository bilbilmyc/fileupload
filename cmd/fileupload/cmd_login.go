package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	var loginToken string

	loginCmd := &cobra.Command{
		Use:   "login [server]",
		Short: "保存 X-Auth-Token 到本地",
		Long: `保存服务端认证令牌到 ~/.fileupload/token。

	首次使用：
	  fileupload login http://localhost:8080 --token my-secret-token

	之后所有命令自动携带该令牌（也可通过 --token flag 临时覆盖）。`,
		Example: `  fileupload login --token my-secret-token
	  fileupload login http://192.168.1.100:8080 --token my-secret-token`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				serverURL = args[0]
			}
			if loginToken == "" {
				fmt.Fprintln(os.Stderr, "请使用 --token 指定认证令牌")
				fmt.Fprintln(os.Stderr, "  fileupload login --token <your-token>")
				return nil
			}
			if err := saveToken(loginToken); err != nil {
				return fmt.Errorf("保存令牌失败: %w", err)
			}
			fmt.Printf("登录成功！令牌已保存到 %s\n", tokenFilePath())
			fmt.Println("后续命令自动携带认证令牌。使用 --token flag 可临时覆盖。")
			return nil
		},
	}

	loginCmd.Flags().StringVar(&loginToken, "token", "", "X-Auth-Token 认证令牌")
	rootCmd.AddCommand(loginCmd)
}
