package fetchrun

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
)

const (
	defaultTimeout       = 30 * time.Second
	maxContentLen        = 12000
	maxResponseSize      = 512 * 1024 // 512KB 响应限制
	defaultUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	cacheDefaultTTL      = 5 * time.Minute
	cacheCleanupInterval = 10 * time.Minute // 缓存清理间隔
	defaultMaxCacheSize  = 1000             // 默认最大缓存条目数
	maxRetries           = 3                // 最大重试次数
	retryDelay           = 500 * time.Millisecond
)

// HTTPClient 全局HTTP客户端（复用连接池）
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
	globalCache    *ContentCache
	cacheOnce      sync.Once
)

// getHTTPClient 获取复用的HTTP客户端
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		httpClient = &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	})
	return httpClient
}

// lruEntry LRU链表节点
type lruEntry struct {
	key       string
	value     cacheItem
	prev      *lruEntry
	next      *lruEntry
}

// ContentCache 内容缓存（带LRU淘汰）
type ContentCache struct {
	mu         sync.RWMutex
	items      map[string]*lruEntry
	ttl        time.Duration
	maxSize    int
	head, tail *lruEntry // LRU链表头尾

	// 单飞机制：防止相同URL并发请求
	inflight   map[string]*inflightCall
}

type inflightCall struct {
	wg    sync.WaitGroup
	val   string
	err   error
}

type cacheItem struct {
	content   string
	expiresAt time.Time
}

// NewContentCache 创建缓存
func NewContentCache(ttl time.Duration, maxSize int) *ContentCache {
	if ttl <= 0 {
		ttl = cacheDefaultTTL
	}
	if maxSize <= 0 {
		maxSize = defaultMaxCacheSize
	}
	c := &ContentCache{
		items:    make(map[string]*lruEntry),
		ttl:      ttl,
		maxSize:  maxSize,
		inflight: make(map[string]*inflightCall),
	}
	c.head = &lruEntry{}
	c.tail = &lruEntry{}
	c.head.next = c.tail
	c.tail.prev = c.head
	return c
}

// GetGlobalCache 获取全局缓存（自动启动后台清理）
func GetGlobalCache() *ContentCache {
	cacheOnce.Do(func() {
		globalCache = NewContentCache(cacheDefaultTTL, defaultMaxCacheSize)
		// 启动后台清理 goroutine
		go globalCache.startCleanup()
	})
	return globalCache
}

// startCleanup 定期清理过期缓存
func (c *ContentCache) startCleanup() {
	ticker := time.NewTicker(cacheCleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		c.Cleanup()
	}
}

// moveToFront 将节点移到LRU链表头部（最近使用）
func (c *ContentCache) moveToFront(e *lruEntry) {
	if e == nil || c.head.next == e {
		return // 已经在头部或无效
	}
	// 从当前位置移除
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	}
	// 插入头部
	e.next = c.head.next
	e.prev = c.head
	if c.head.next != nil {
		c.head.next.prev = e
	}
	c.head.next = e
}

// removeFromList 从LRU链表移除节点
func (c *ContentCache) removeFromList(e *lruEntry) {
	e.prev.next = e.next
	if e.next != nil {
		e.next.prev = e.prev
	}
}

// addToFront 添加节点到LRU链表头部
func (c *ContentCache) addToFront(e *lruEntry) {
	e.next = c.head.next
	e.prev = c.head
	c.head.next.prev = e
	c.head.next = e
}

// evictLRU 淘汰最久未使用的条目
func (c *ContentCache) evictLRU() {
	if len(c.items) < c.maxSize {
		return
	}
	// 从尾部淘汰（最久未使用）
	tail := c.tail.prev
	if tail != c.head {
		c.removeFromList(tail)
		delete(c.items, tail.key)
	}
}

// Get 获取缓存内容
func (c *ContentCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.items[key]
	if !found {
		return "", false
	}
	if time.Now().After(entry.value.expiresAt) {
		// 过期条目删除
		c.removeFromList(entry)
		delete(c.items, key)
		return "", false
	}

	// 更新LRU位置（移到头部）
	c.moveToFront(entry)
	return entry.value.content, true
}

// Set 设置缓存内容
func (c *ContentCache) Set(key string, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已存在，更新并移到头部
	if entry, exists := c.items[key]; exists {
		entry.value = cacheItem{
			content:   content,
			expiresAt: time.Now().Add(c.ttl),
		}
		c.moveToFront(entry)
		return
	}

	// 检查是否需要淘汰
	if len(c.items) >= c.maxSize {
		c.evictLRU()
	}

	// 添加新条目
	entry := &lruEntry{
		key: key,
		value: cacheItem{
			content:   content,
			expiresAt: time.Now().Add(c.ttl),
		},
	}
	c.items[key] = entry
	c.addToFront(entry)
}

// Cleanup 清理过期缓存
func (c *ContentCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.items {
		if now.After(entry.value.expiresAt) {
			c.removeFromList(entry)
			delete(c.items, key)
		}
	}
}

// getOrFetch 单飞获取：如果相同URL正在获取，等待其完成；否则执行获取
func (c *ContentCache) getOrFetch(ctx context.Context, url string, fetch func(context.Context, string) (string, error)) (string, error) {
	c.mu.Lock()

	// 先检查缓存
	if entry, found := c.items[url]; found {
		if time.Now().Before(entry.value.expiresAt) {
			c.moveToFront(entry)
			c.mu.Unlock()
			return entry.value.content, nil
		}
		// 过期条目删除
		c.removeFromList(entry)
		delete(c.items, url)
	}

	// 检查是否有正在进行的请求
	if call, ok := c.inflight[url]; ok {
		c.mu.Unlock()
		call.wg.Wait()
		return call.val, call.err
	}

	// 创建新的inflight请求
	call := &inflightCall{}
	call.wg.Add(1)
	c.inflight[url] = call
	c.mu.Unlock()

	// 执行获取（在锁外）
	call.val, call.err = fetch(ctx, url)

	// 如果成功，存入缓存
	if call.err == nil && call.val != "" {
		c.Set(url, call.val)
	}

	c.mu.Lock()
	delete(c.inflight, url)
	c.mu.Unlock()

	call.wg.Done()
	return call.val, call.err
}

// FetchOptions 抓取选项
type FetchOptions struct {
	UseCache  bool
	CacheTTL  time.Duration
	Timeout   time.Duration
	UserAgent string
	NoRetry   bool // 禁用重试
}

// FetchResult 抓取结果
type FetchResult struct {
	URL     string
	Content string
	Error   error
}

// 始终移除的标签（非正文、脚本、导航等）
var stripTags = map[string]bool{
	"script": true, "style": true, "nav": true, "header": true, "footer": true,
	"noscript": true, "iframe": true, "svg": true, "form": true,
}

// id/class 中含这些关键词的节点视为广告并移除（小写匹配）
var adKeywords = []string{
	"ad", "ads", "advertisement", "banner", "sponsor", "sponsored", "promo",
	"popup", "overlay", "cookie-consent", "consent-banner", "sidebar-ad",
	"ad-container", "ad-wrapper", "ad-slot", "ad-unit", "advertisement",
	"social-share", "related-posts", "comment-box", "newsletter-signup",
}

// Fetch 获取 URL 对应页面，清理广告与无关块后转为 Markdown 返回
func Fetch(ctx context.Context, pageURL string) (string, error) {
	return FetchWithOptions(ctx, pageURL, nil)
}

// FetchWithOptions 使用选项抓取URL
func FetchWithOptions(ctx context.Context, pageURL string, opts *FetchOptions) (string, error) {
	if opts == nil {
		opts = &FetchOptions{}
	}

	// 使用单飞机制避免重复请求
	if opts.UseCache {
		cache := GetGlobalCache()
		content, err := cache.getOrFetch(ctx, pageURL, func(ctx context.Context, url string) (string, error) {
			return fetchInternal(ctx, url, opts)
		})
		return content, err
	}

	return fetchInternal(ctx, pageURL, opts)
}

// fetchInternal 内部获取实现（带重试）
func fetchInternal(ctx context.Context, pageURL string, opts *FetchOptions) (string, error) {
	var lastErr error
	retries := maxRetries
	if opts.NoRetry {
		retries = 1
	}

	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(retryDelay * time.Duration(attempt)):
			}
		}

		content, err := doFetch(ctx, pageURL, opts)
		if err == nil {
			return content, nil
		}

		lastErr = err
		// 不重试客户端错误（4xx）
		if isClientError(err) {
			break
		}
	}

	return "", fmt.Errorf("获取失败（已重试%d次）: %w", retries-1, lastErr)
}

// isClientError 检查是否为客户端错误
func isClientError(err error) bool {
	if err == nil {
		return false
	}
	// 简单的错误字符串检查
	errStr := err.Error()
	for i := 400; i < 500; i++ {
		if strings.Contains(errStr, fmt.Sprintf("HTTP %d", i)) {
			return true
		}
	}
	return false
}

// doFetch 执行单次获取
func doFetch(ctx context.Context, pageURL string, opts *FetchOptions) (string, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return "", fmt.Errorf("读取响应: %w", err)
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("解析 HTML: %w", err)
	}

	cleanAdsAndNoise(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return "", fmt.Errorf("渲染 HTML: %w", err)
	}
	cleanedHTML := buf.String()

	md, err := htmltomarkdown.ConvertString(
		cleanedHTML,
		converter.WithDomain(pageURL),
	)
	if err != nil {
		return "", fmt.Errorf("转为 Markdown: %w", err)
	}

	md = normalizeMarkdown(md)
	if len(md) > maxContentLen {
		md = md[:maxContentLen] + "\n\n... [content truncated]"
	}

	return md, nil
}

// FetchConcurrent 并发抓取多个URL（自动去重）
func FetchConcurrent(ctx context.Context, urls []string, maxConcurrency int) map[string]*FetchResult {
	if maxConcurrency <= 0 {
		maxConcurrency = 3
	}

	// 去重：保留原始顺序，但只保留第一次出现的URL
	seen := make(map[string]bool, len(urls))
	uniqueURLs := make([]string, 0, len(urls))
	for _, url := range urls {
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		uniqueURLs = append(uniqueURLs, url)
	}

	results := make(map[string]*FetchResult, len(uniqueURLs))
	var mu sync.Mutex

	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, url := range uniqueURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
				content, err := Fetch(ctx, u)
				<-semaphore

				mu.Lock()
				results[u] = &FetchResult{
					URL:     u,
					Content: content,
					Error:   err,
				}
				mu.Unlock()
			case <-ctx.Done():
				mu.Lock()
				results[u] = &FetchResult{
					URL:   u,
					Error: ctx.Err(),
				}
				mu.Unlock()
			}
		}(url)
	}

	wg.Wait()
	return results
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func isAdLike(n *html.Node) bool {
	id := strings.ToLower(getAttr(n, "id"))
	class := strings.ToLower(getAttr(n, "class"))
	for _, k := range adKeywords {
		if strings.Contains(id, k) || strings.Contains(class, k) {
			return true
		}
	}
	return false
}

func shouldRemoveElement(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	tag := strings.ToLower(n.Data)
	// 保留关键结构标签
	if tag == "body" || tag == "html" || tag == "head" || tag == "main" || tag == "article" {
		return false
	}
	if stripTags[tag] {
		return true
	}
	return isAdLike(n)
}

// cleanAdsAndNoise 原地移除广告与无关节点（先收集再按深度倒序移除）
func cleanAdsAndNoise(doc *html.Node) {
	var toRemove []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if shouldRemoveElement(n) {
			toRemove = append(toRemove, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(toRemove) == 0 {
		return
	}

	// 按深度从大到小移除，先移子节点再移父节点
	// 使用预分配的深度数组避免重复计算
	type nodeDepth struct {
		node  *html.Node
		depth int
	}
	withDepth := make([]nodeDepth, len(toRemove))
	for i, n := range toRemove {
		d := 0
		for p := n.Parent; p != nil; p = p.Parent {
			d++
		}
		withDepth[i] = nodeDepth{node: n, depth: d}
	}

	sort.Slice(withDepth, func(i, j int) bool {
		return withDepth[i].depth > withDepth[j].depth
	})
	for _, nd := range withDepth {
		if nd.node.Parent != nil {
			nd.node.Parent.RemoveChild(nd.node)
		}
	}
}

// normalizeMarkdown 合并多余空行（使用strings.Builder优化）
func normalizeMarkdown(s string) string {
	s = strings.TrimSpace(s)
	// 合并多余空行：3+个换行符 -> 2个换行符
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// HealthCheck 健康检查
func HealthCheck(ctx context.Context) error {
	client := getHTTPClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://www.google.com", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
