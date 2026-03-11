package searchrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SearchItem 单条搜索结果
type SearchItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"` // 链接的简要描述（Google 结果页 snippet）
}

// SearchOptions 搜索选项
type SearchOptions struct {
	TimeRange      string        // 时间范围: day, week, month, year
	Language       string        // 语言代码: zh-CN, en, etc.
	RequestDelay   time.Duration // 请求间隔（限流）
	LogPagination bool // 是否打印翻页进度日志
}

const (
	MaxTitleLen        = 300
	OpenRetries        = 2
	EvalRetries        = 2
	RetryDelay         = 3 * time.Second
	PageLoadDelay      = 2 * time.Second
	MinRequestInterval = 500 * time.Millisecond // 默认最小请求间隔
	MaxPages           = 10                     // 最大翻页数，防止无限循环
	cdpHint            = "；若使用 CDP 模式，请检查 Chrome 调试端口是否连接成功"
)

// Logger 可选日志回调，nil 则不输出
type Logger func(format string, args ...interface{})

// RateLimiter 简单的限流器
type RateLimiter struct {
	interval time.Duration
	lastReq  time.Time
	mu       sync.Mutex
}

// NewRateLimiter 创建限流器
func NewRateLimiter(interval time.Duration) *RateLimiter {
	if interval <= 0 {
		interval = MinRequestInterval
	}
	return &RateLimiter{interval: interval}
}

// Wait 等待直到可以发送下一个请求（支持 context 取消）
func (r *RateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.lastReq)
	if elapsed < r.interval {
		sleepDuration := r.interval - elapsed
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			r.mu.Lock()
			return ctx.Err()
		case <-time.After(sleepDuration):
			r.mu.Lock()
		}
	}
	r.lastReq = time.Now()
	return nil
}

// trackingParams are query keys stripped to clean links for display/dedup
var trackingParams = map[string]bool{
	"utm_source": true, "utm_medium": true, "utm_campaign": true,
	"utm_content": true, "utm_term": true, "utm_referrer": true,
	"gclid": true, "gclsrc": true, "dclid": true,
	"fbclid": true, "msclkid": true,
	"ref": true, "ref_src": true, "ref_url": true,
	"_ga": true, "mc_cid": true, "mc_eid": true,
	"igshid": true, "si": true,
}

// cleanExtractedLink normalizes and cleans a URL from Google search results:
// unwraps Google redirects, strips tracking query params, removes empty fragments.
func cleanExtractedLink(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	// 1) Unwrap Google redirect
	u = cleanGoogleRedirect(u)
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	// Only clean http/https
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return u
	}
	// 2) Strip tracking query params
	q := parsed.Query()
	if len(q) > 0 {
		cleaned := make(url.Values)
		for k, v := range q {
			keyLower := strings.ToLower(k)
			if trackingParams[keyLower] {
				continue
			}
			if len(k) > 4 && (strings.HasPrefix(keyLower, "utm_") || keyLower == "gclid" || keyLower == "fbclid" || keyLower == "ref") {
				continue
			}
			cleaned[k] = v
		}
		parsed.RawQuery = cleaned.Encode()
	}
	// 3) Remove fragment if it's only tracking (e.g. #:~:text=...)
	if parsed.Fragment != "" && (strings.HasPrefix(parsed.Fragment, "~:") || strings.Contains(parsed.Fragment, ":~:")) {
		parsed.Fragment = ""
	}
	// 4) Normalize: trim trailing slash on path for consistent dedup
	if strings.HasSuffix(parsed.Path, "/") && len(parsed.Path) > 1 {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	return parsed.String()
}

func cleanGoogleRedirect(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if strings.HasPrefix(parsed.Host, "www.google.") && parsed.Path == "/url" {
		if dest := parsed.Query().Get("url"); dest != "" {
			if d, err := url.QueryUnescape(dest); err == nil {
				return d
			}
			return dest
		}
	}
	return u
}

func isAdOrIrrelevant(link, title string) bool {
	link = strings.ToLower(link)
	title = strings.TrimSpace(title)
	if title == "" || len(title) < 2 {
		return true
	}
	adLike := []string{"ad", "sponsored", "广告", "赞助"}
	t := strings.ToLower(title)
	for _, a := range adLike {
		if t == a || strings.HasPrefix(t, a+" ") {
			return true
		}
	}
	skip := []string{
		"google.com/sorry", "accounts.google.com", "support.google.com",
		"doubleclick.net", "googleadservices.com", "googlesyndication.com",
		"google.com/intl/", "policies.google.com", "consent.google",
	}
	for _, s := range skip {
		if strings.Contains(link, s) {
			return true
		}
	}
	if strings.Contains(link, "#:~:text=") {
		return true
	}
	return false
}

func truncateTitle(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 {
		maxLen = MaxTitleLen
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// AgentBrowserResponse agent-browser的标准响应格式
type AgentBrowserResponse struct {
	Success bool `json:"success"`
	Data    *struct {
		Result json.RawMessage `json:"result"` // 使用RawMessage避免类型断言
	} `json:"data"`
	Error *string `json:"error"`
}

// ExtractResult 新的extract.js返回格式
type ExtractResult struct {
	Success      bool         `json:"success"`
	Count        int          `json:"count"`
	Selector     string       `json:"selector"`
	LinkSelector string       `json:"linkSelector"`
	Results      []SearchItem `json:"results"`
	Error        string       `json:"error"`
}

// parseSearchResults 解析搜索结果，支持多种格式
func parseSearchResults(data []byte) ([]SearchItem, error) {
	var items []SearchItem

	// 尝试解析为新的extract.js格式
	var extractResult ExtractResult
	if err := json.Unmarshal(data, &extractResult); err == nil && extractResult.Success {
		return extractResult.Results, nil
	}

	// 尝试直接解析为数组（旧格式兼容）
	if err := json.Unmarshal(data, &items); err == nil {
		return items, nil
	}

	// 尝试解析为字符串（JSON字符串）
	var strResult string
	if err := json.Unmarshal(data, &strResult); err == nil {
		if err := json.Unmarshal([]byte(strResult), &items); err != nil {
			// 再次尝试新格式
			if err := json.Unmarshal([]byte(strResult), &extractResult); err == nil && extractResult.Success {
				return extractResult.Results, nil
			}
			return nil, fmt.Errorf("解析字符串结果失败: %w", err)
		}
		return items, nil
	}

	// 尝试解析为接口数组
	var rawItems []map[string]interface{}
	if err := json.Unmarshal(data, &rawItems); err == nil {
		for _, m := range rawItems {
			title, _ := m["title"].(string)
			u, _ := m["url"].(string)
			snippet, _ := m["snippet"].(string)
			if title != "" || u != "" {
				items = append(items, SearchItem{Title: title, URL: u, Snippet: snippet})
			}
		}
		return items, nil
	}

	return nil, fmt.Errorf("无法解析搜索结果格式")
}

// buildSearchURL 构建搜索URL，支持时间范围和语言
func buildSearchURL(baseURL, keyword string, opts *SearchOptions) string {
	query := strings.ReplaceAll(keyword, " ", "+")
	url := baseURL + query

	if opts != nil {
		// 时间范围参数
		switch opts.TimeRange {
		case "day":
			url += "&tbs=qdr:d"
		case "week":
			url += "&tbs=qdr:w"
		case "month":
			url += "&tbs=qdr:m"
		case "year":
			url += "&tbs=qdr:y"
		}

		// 语言参数
		if opts.Language != "" {
			url += "&hl=" + opts.Language
		}
	}

	return url
}

// RunWithOptions 执行 Google 搜索，支持更多选项
func RunWithOptions(ctx context.Context, extractJS, keyword string, n, cdp int, opts *SearchOptions, infoLog, errLog Logger) ([]SearchItem, error) {
	js := strings.TrimSpace(extractJS)
	if js == "" {
		return nil, fmt.Errorf("extract.js 为空")
	}

	// 设置默认选项
	if opts == nil {
		opts = &SearchOptions{}
	}

	// 创建限流器
	limiter := NewRateLimiter(opts.RequestDelay)

	// 每次调用使用独立 session，避免多进程/多并发共用一个 tab 互相覆盖（CDP 与自启浏览器均一 session 一 tab）
	var session string
	var browserArgs []string
	session = fmt.Sprintf("google-search-%d-%d", os.Getpid(), time.Now().UnixNano())
	browserArgs = []string{"--session", session}
	if cdp != 0 {
		browserArgs = append(browserArgs, "--cdp", strconv.Itoa(cdp))
		if infoLog != nil {
			infoLog("搜索: %s (取前 %d 条，CDP %d)", keyword, n, cdp)
		}
	} else {
		if infoLog != nil {
			infoLog("搜索: %s (取前 %d 条)", keyword, n)
		}
	}
	defer func() {
		if session != "" {
			closeArgs := []string{"--session", session}
			if cdp != 0 {
				closeArgs = append(closeArgs, "--cdp", strconv.Itoa(cdp))
			}
			closeArgs = append(closeArgs, "close")
			_ = exec.Command("agent-browser", closeArgs...).Run()
		}
	}()

	baseURL := "https://www.google.com/search?q="
	var allItems []SearchItem
	seenURL := make(map[string]bool)
	start := 0
	page := 1

	for {
		// 限流检查
		if err := limiter.Wait(ctx); err != nil {
			if len(allItems) > 0 {
				if infoLog != nil {
					infoLog("限流等待被取消，返回已收集的 %d 条结果", len(allItems))
				}
				break
			}
			return nil, fmt.Errorf("限流等待被取消: %w", err)
		}

		if page > MaxPages {
			if infoLog != nil {
				infoLog("已达到最大翻页数 %d，停止搜索", MaxPages)
			}
			break
		}

		pageURL := buildSearchURL(baseURL, keyword, opts) + "&start=" + strconv.Itoa(start)

		// 带重试机制的页面打开
		var openErr error
		for attempt := 0; attempt <= OpenRetries; attempt++ {
			if attempt > 0 {
				if infoLog != nil {
					infoLog("打开页面重试 %d/%d（可能为限流或超时）", attempt, OpenRetries)
				}
				time.Sleep(RetryDelay)
			}

			cmdOpen := exec.CommandContext(ctx, "agent-browser", append(browserArgs, "open", pageURL)...)
			openErr = cmdOpen.Run()
			if openErr == nil {
				break
			}
			if errLog != nil {
				errLog("打开页面失败: %v", openErr)
			}
		}
		if openErr != nil {
			// 如果已有结果，返回已有结果
			if len(allItems) > 0 {
				if infoLog != nil {
					infoLog("打开页面失败，返回已收集的 %d 条结果", len(allItems))
				}
				break
			}
			return nil, fmt.Errorf("打开页面失败: %w%s", openErr, cdpHint)
		}

		time.Sleep(PageLoadDelay)

		// 带重试机制的内容提取
		var out []byte
		var evalErr error
		for attempt := 0; attempt <= EvalRetries; attempt++ {
			if attempt > 0 {
				if infoLog != nil {
					infoLog("执行提取重试 %d/%d", attempt, EvalRetries)
				}
				time.Sleep(RetryDelay)
			}

			cmdEval := exec.CommandContext(ctx, "agent-browser", append(browserArgs, "eval", js, "--json")...)
			out, evalErr = cmdEval.Output()
			if evalErr == nil {
				break
			}
			if errLog != nil {
				errLog("执行提取失败: %v", evalErr)
			}
		}
		if evalErr != nil {
			// 如果已有结果，返回已有结果
			if len(allItems) > 0 {
				if infoLog != nil {
					infoLog("执行提取失败，返回已收集的 %d 条结果", len(allItems))
				}
				break
			}
			return nil, fmt.Errorf("执行提取失败: %w%s", evalErr, cdpHint)
		}

		// 解析响应
		var wrap AgentBrowserResponse
		if err := json.Unmarshal(out, &wrap); err != nil {
			if len(allItems) > 0 {
				if infoLog != nil {
					infoLog("解析响应失败，返回已收集的 %d 条结果", len(allItems))
				}
				break
			}
			return nil, fmt.Errorf("解析 agent-browser 输出: %w%s", err, cdpHint)
		}

		if !wrap.Success {
			msg := "未返回成功结果"
			if wrap.Error != nil && *wrap.Error != "" {
				msg = *wrap.Error
			}
			if len(allItems) > 0 {
				if infoLog != nil {
					infoLog("agent-browser错误: %s，返回已收集的 %d 条结果", msg, len(allItems))
				}
				break
			}
			return nil, fmt.Errorf("agent-browser: %s%s", msg, cdpHint)
		}

		if wrap.Data == nil || wrap.Data.Result == nil {
			if len(allItems) > 0 {
				if infoLog != nil {
					infoLog("未获取到数据，返回已收集的 %d 条结果", len(allItems))
				}
				break
			}
			return nil, fmt.Errorf("未获取到搜索结果（可能遇到验证页或页面结构变化）%s", cdpHint)
		}

		// 使用改进的解析函数
		pageItems, err := parseSearchResults(wrap.Data.Result)
		if err != nil {
			if len(allItems) > 0 && infoLog != nil {
				infoLog("解析结果失败: %v，跳过本页继续", err)
			} else if len(allItems) == 0 {
				return nil, fmt.Errorf("%w%s", err, cdpHint)
			}
			pageItems = nil
		}

		// 处理结果
		newItems := 0
		for _, it := range pageItems {
			u := cleanExtractedLink(it.URL)
			if u == "" {
				continue
			}
			if isAdOrIrrelevant(u, it.Title) {
				continue
			}
			if seenURL[u] {
				continue
			}
			seenURL[u] = true
			snippet := truncateTitle(it.Snippet, 500)
			allItems = append(allItems, SearchItem{
				Title:   truncateTitle(it.Title, MaxTitleLen),
				URL:     u,
				Snippet: snippet,
			})
			newItems++
		}

		if len(allItems) >= n {
			break
		}
		if len(pageItems) == 0 || newItems == 0 {
			if infoLog != nil {
				infoLog("本页无新结果，停止搜索")
			}
			break
		}

		start += 10
		page++
		if opts.LogPagination && infoLog != nil {
			infoLog("已获取 %d 条，不足 %d 条，获取第 %d 页", len(allItems), n, page)
		}
	}

	if len(allItems) == 0 {
		return nil, fmt.Errorf("未解析到任何搜索结果%s", cdpHint)
	}

	limit := n
	if limit > len(allItems) {
		limit = len(allItems)
	}
	return allItems[:limit], nil
}

// Run 执行 Google 搜索（向后兼容）
func Run(ctx context.Context, extractJS, keyword string, n, cdp int, infoLog, errLog Logger) ([]SearchItem, error) {
	return RunWithOptions(ctx, extractJS, keyword, n, cdp, nil, infoLog, errLog)
}

// FormatLLM 格式化为 LLM 友好字符串（含每条链接的简要描述）
func FormatLLM(items []SearchItem) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d search results:\n\n", len(items)))
	for i, it := range items {
		b.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n", i+1, it.Title, it.URL))
		if it.Snippet != "" {
			b.WriteString(fmt.Sprintf("   Snippet: %s\n", it.Snippet))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// FormatJSON 格式化为 JSON 字符串
func FormatJSON(items []SearchItem) (string, error) {
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
