package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ai/google-search-tool/internal/searchrun"
	"github.com/spf13/cobra"
)

var (
	searchKeyword   string
	searchN         int
	searchCDP       int
	searchLLM       bool
	searchJSON      bool
	searchTime      string
	searchLang      string
	searchDelay     time.Duration
	searchOutput    string
	searchLogPagination bool
)

var searchCmd = &cobra.Command{
	Use:   "search [关键词]",
	Short: "使用 agent-browser 执行 Google 搜索并输出结果",
	Long: `打开 Google 搜索页，提取前 N 条结果的标题与链接；不足时自动翻页。
支持多种输出格式（默认、LLM友好、JSON），支持时间范围和语言过滤。`,
	Args: cobra.ArbitraryArgs,
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().StringVarP(&searchKeyword, "query", "q", "", "搜索关键词（必填，也可用位置参数）")
	searchCmd.Flags().IntVarP(&searchN, "num", "n", 100, "返回前 N 条结果")
	searchCmd.Flags().IntVar(&searchCDP, "cdp", 9222, "CDP 端口；0 表示自动起新浏览器")
	searchCmd.Flags().BoolVar(&searchLLM, "llm", false, "输出为 LLM 友好格式")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "输出为 JSON 格式")
	searchCmd.Flags().StringVar(&searchTime, "time", "", "时间范围: day(一天内), week(一周内), month(一月内), year(一年内)")
	searchCmd.Flags().StringVar(&searchLang, "lang", "", "语言代码: zh-CN(中文), en(英文) 等")
	searchCmd.Flags().DurationVar(&searchDelay, "delay", 500*time.Millisecond, "请求间隔（限流），默认500ms")
	searchCmd.Flags().StringVarP(&searchOutput, "output", "o", "", "输出到文件（默认输出到stdout）")
	searchCmd.Flags().BoolVar(&searchLogPagination, "log-pagination", false, "打印翻页进度日志（已获取 N 条、获取第 M 页等）")
}

func runSearch(cmd *cobra.Command, args []string) error {
	keyword := searchKeyword
	if keyword == "" && len(args) > 0 {
		keyword = strings.Join(args, " ")
	}
	if keyword == "" {
		return fmt.Errorf("请提供搜索关键词（-q 或位置参数）")
	}

	// 验证时间范围参数
	if searchTime != "" {
		validTimes := map[string]bool{"day": true, "week": true, "month": true, "year": true}
		if !validTimes[searchTime] {
			return fmt.Errorf("无效的时间范围: %s，可选值: day, week, month, year", searchTime)
		}
	}

	infoLog := func(format string, a ...interface{}) {
		fmt.Fprintf(os.Stderr, "[INFO] "+format+"\n", a...)
	}
	errLog := func(format string, a ...interface{}) {
		fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", a...)
	}

	// 构建搜索选项
	opts := &searchrun.SearchOptions{
		TimeRange:      searchTime,
		Language:       searchLang,
		RequestDelay:   searchDelay,
		LogPagination:  searchLogPagination,
	}

	items, err := searchrun.RunWithOptions(context.Background(), extractJS, keyword, searchN, searchCDP, opts, infoLog, errLog)
	if err != nil {
		return err
	}

	var output string

	// 根据格式选择输出方式
	switch {
	case searchJSON:
		output, err = searchrun.FormatJSON(items)
		if err != nil {
			return fmt.Errorf("格式化 JSON 失败: %w", err)
		}
	case searchLLM:
		output = searchrun.FormatLLM(items)
	default:
		var b strings.Builder
		for i, it := range items {
			b.WriteString(it.Title)
			b.WriteString("\n")
			b.WriteString(it.URL)
			if i < len(items)-1 {
				b.WriteString("\n---\n")
			}
		}
		output = b.String()
	}

	// 输出到文件或stdout
	if searchOutput != "" {
		err := os.WriteFile(searchOutput, []byte(output), 0644)
		if err != nil {
			return fmt.Errorf("写入文件失败: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[INFO] 结果已保存到: %s\n", searchOutput)
	} else {
		fmt.Println(output)
	}

	return nil
}
