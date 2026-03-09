package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ai/google-search-tool/internal/fetchrun"
	"github.com/spf13/cobra"
)

var (
	fetchUseCache bool
	fetchOutput   string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <url>",
	Short: "抓取 URL 正文并输出为 Markdown（清理广告与无关块）",
	Long: `请求 URL，移除广告/脚本/导航等后转为 Markdown，相对链接会变为绝对链接，过长则截断。
支持缓存机制，可以多次抓取相同URL时加快速度。`,
	Args: cobra.ExactArgs(1),
	RunE: runFetch,
}

func init() {
	fetchCmd.Flags().BoolVar(&fetchUseCache, "cache", false, "使用缓存（默认5分钟TTL）")
	fetchCmd.Flags().StringVarP(&fetchOutput, "output", "o", "", "输出到文件（默认输出到stdout）")
}

func runFetch(_ *cobra.Command, args []string) error {
	url := args[0]

	opts := &fetchrun.FetchOptions{
		UseCache: fetchUseCache,
	}

	text, err := fetchrun.FetchWithOptions(context.Background(), url, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		return err
	}

	// 输出到文件或stdout
	if fetchOutput != "" {
		err := os.WriteFile(fetchOutput, []byte(text), 0644)
		if err != nil {
			return fmt.Errorf("写入文件失败: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[INFO] 内容已保存到: %s\n", fetchOutput)
	} else {
		fmt.Println(text)
	}

	return nil
}
