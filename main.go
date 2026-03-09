// 命令行工具：mcp | search | fetch，基于 Cobra，使用 agent-browser 做 Google 搜索与网页抓取
package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/ai/google-search-tool/cmd"
)

//go:embed extract.js
var extractJS string

func main() {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-V" {
			fmt.Println(cmd.Version())
			os.Exit(0)
		}
	}
	cmd.SetExtractJS(extractJS)
	cmd.Execute()
}
