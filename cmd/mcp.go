package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ai/google-search-tool/internal/fetchrun"
	"github.com/ai/google-search-tool/internal/searchrun"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var mcpCDP int

// healthStatus MCP服务健康状态
type healthStatus struct {
	mu        sync.RWMutex
	isHealthy bool
	lastCheck time.Time
}

func (h *healthStatus) setHealthy(healthy bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isHealthy = healthy
	h.lastCheck = time.Now()
}

func (h *healthStatus) getHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isHealthy
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "启动 MCP 服务，供 agent 调用 search 与 fetch 工具",
	Long:  `通过 stdio 与 MCP 客户端通信，暴露 search（Google 搜索）和 fetch（抓取 URL 正文）两个 tool。`,
	RunE:  runMCP,
}

func init() {
	mcpCmd.Flags().IntVar(&mcpCDP, "cdp", 9222, "search 使用的 CDP 端口；0 表示自动起新浏览器")
}

func runMCP(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理以实现优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 健康状态管理
	health := &healthStatus{isHealthy: true, lastCheck: time.Now()}

	// 启动健康检查goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, checkCancel := context.WithTimeout(ctx, 10*time.Second)
				err := fetchrun.HealthCheck(checkCtx)
				checkCancel()

				if err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] Health check failed: %v\n", err)
					health.setHealthy(false)
				} else {
					health.setHealthy(true)
				}
			}
		}
	}()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "google-search-tool",
		Version: version,
	}, nil)

	// Tool: search
	type SearchIn struct {
		Query      string `json:"query" jsonschema:"the search query string"`
		MaxResults int    `json:"max_results" jsonschema:"maximum number of results to return (default: 10)"`
		TimeRange  string `json:"time_range,omitempty" jsonschema:"time range filter: day, week, month, or year"`
		Language   string `json:"language,omitempty" jsonschema:"language code (e.g., zh-CN, en)"`
	}
	type SearchOut struct {
		Text string `json:"text"`
	}

	searchHandler := func(ctx context.Context, req *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, SearchOut, error) {
		if in.Query == "" {
			return nil, SearchOut{}, fmt.Errorf("query 不能为空")
		}
		n := in.MaxResults
		if n <= 0 {
			n = 10
		}

		opts := &searchrun.SearchOptions{
			TimeRange: in.TimeRange,
			Language:  in.Language,
		}

		items, err := searchrun.RunWithOptions(ctx, extractJS, in.Query, n, mcpCDP, opts, nil, nil)
		if err != nil {
			return nil, SearchOut{}, err
		}
		return nil, SearchOut{Text: searchrun.FormatLLM(items)}, nil
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search Google and return titles and URLs (uses agent-browser). Supports time range and language filtering.",
	}, searchHandler)

	// Tool: fetch
	type FetchIn struct {
		URL      string `json:"url" jsonschema:"the URL of the webpage to fetch"`
		UseCache bool   `json:"use_cache,omitempty" jsonschema:"whether to use cache (default: false)"`
	}
	type FetchOut struct {
		Text string `json:"text"`
	}

	fetchHandler := func(ctx context.Context, req *mcp.CallToolRequest, in FetchIn) (*mcp.CallToolResult, FetchOut, error) {
		if in.URL == "" {
			return nil, FetchOut{}, fmt.Errorf("url 不能为空")
		}

		opts := &fetchrun.FetchOptions{
			UseCache: in.UseCache,
		}

		text, err := fetchrun.FetchWithOptions(ctx, in.URL, opts)
		if err != nil {
			return nil, FetchOut{}, err
		}
		return nil, FetchOut{Text: text}, nil
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "fetch",
		Description: "Fetch and parse main text content from a webpage URL. Supports caching.",
	}, fetchHandler)

	// Tool: health
	type HealthIn struct{}
	type HealthOut struct {
		Healthy   bool      `json:"healthy"`
		LastCheck time.Time `json:"last_check"`
		Version   string    `json:"version"`
	}

	healthHandler := func(ctx context.Context, req *mcp.CallToolRequest, in HealthIn) (*mcp.CallToolResult, HealthOut, error) {
		return nil, HealthOut{
			Healthy:   health.getHealthy(),
			LastCheck: health.lastCheck,
			Version:   version,
		}, nil
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "health",
		Description: "Check the health status of the MCP server.",
	}, healthHandler)

	// 在goroutine中运行服务器
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Run(ctx, &mcp.StdioTransport{})
	}()

	// 等待信号或服务器错误
	select {
	case sig := <-sigChan:
		fmt.Fprintf(os.Stderr, "[INFO] Received signal %v, shutting down gracefully...\n", sig)
		cancel()

		// 给服务器一些时间清理
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		select {
		case err := <-serverErr:
			return err
		case <-shutdownCtx.Done():
			fmt.Fprintf(os.Stderr, "[WARN] Shutdown timeout, forcing exit\n")
			return fmt.Errorf("shutdown timeout")
		}

	case err := <-serverErr:
		return err
	}

	return nil
}
