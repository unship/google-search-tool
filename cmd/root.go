package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

// Version 返回当前版本号（供 main 打印 --version）
func Version() string {
	return version
}

// extractJS 由 main 注入（embed extract.js）
var extractJS string

// SetExtractJS 设置页面提取脚本内容
func SetExtractJS(s string) {
	extractJS = s
}

var rootCmd = &cobra.Command{
	Use:   "google-search-tool",
	Short: "Google 搜索与网页抓取工具（agent-browser + MCP）",
	Long: `通过 agent-browser 执行 Google 搜索、抓取网页正文，并可启动 MCP 服务供 agent 调用。
子命令: mcp（启动 MCP 服务）、search（搜索）、fetch（抓取 URL 正文）。`,
}

func init() {
	rootCmd.PersistentFlags().BoolP("version", "V", false, "显示版本")
}

// Execute 运行根命令
func Execute() {
	rootCmd.AddCommand(mcpCmd, searchCmd, fetchCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
