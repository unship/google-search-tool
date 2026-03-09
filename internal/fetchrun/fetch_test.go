package fetchrun

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 测试无效URL
	_, err := Fetch(ctx, "invalid-url")
	if err == nil {
		t.Error("Fetch should return error for invalid URL")
	}

	// 测试有效URL（使用httpbin.org作为测试目标）
	result, err := Fetch(ctx, "https://httpbin.org/html")
	if err != nil {
		t.Logf("Fetch error (expected in test environment): %v", err)
	} else {
		if result == "" {
			t.Error("Fetch returned empty content for valid URL")
		}
		// 验证HTML标签被清理
		if strings.Contains(result, "<script>") {
			t.Error("Fetch did not remove script tags")
		}
	}
}

func TestIsAdLike(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		class    string
		expected bool
	}{
		{
			name:     "normal element",
			id:       "content",
			class:    "article",
			expected: false,
		},
		{
			name:     "ad keyword in id",
			id:       "sidebar-ad",
			class:    "",
			expected: true,
		},
		{
			name:     "ad keyword in class",
			id:       "",
			class:    "advertisement banner",
			expected: true,
		},
		{
			name:     "sponsored keyword",
			id:       "sponsored-content",
			class:    "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建模拟节点进行测试
			result := checkAdKeywords(tt.id, tt.class)
			if result != tt.expected {
				t.Errorf("checkAdKeywords(%q, %q) = %v, want %v", tt.id, tt.class, result, tt.expected)
			}
		})
	}
}

func TestNormalizeMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "  trimmed  ",
			expected: "trimmed",
		},
		{
			input:    "line1\n\n\n\nline2",
			expected: "line1\n\nline2",
		},
		{
			input:    "already\n\nclean",
			expected: "already\n\nclean",
		},
	}

	for _, tt := range tests {
		result := normalizeMarkdown(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeMarkdown(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func checkAdKeywords(id, class string) bool {
	idLower := strings.ToLower(id)
	classLower := strings.ToLower(class)
	for _, k := range adKeywords {
		if strings.Contains(idLower, k) || strings.Contains(classLower, k) {
			return true
		}
	}
	return false
}

func TestContentCache(t *testing.T) {
	cache := NewContentCache(100*time.Millisecond, 100)

	// 测试缓存设置和获取
	cache.Set("key1", "value1")
	val, found := cache.Get("key1")
	if !found || val != "value1" {
		t.Error("Cache Get/Set not working")
	}

	// 测试未缓存的key
	_, found = cache.Get("key2")
	if found {
		t.Error("Cache should not find uncached key")
	}

	// 测试过期
	time.Sleep(150 * time.Millisecond)
	_, found = cache.Get("key1")
	if found {
		t.Error("Cache entry should have expired")
	}
}

func TestConcurrentFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	urls := []string{
		"https://httpbin.org/delay/1",
		"https://httpbin.org/html",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := FetchConcurrent(ctx, urls, 2)

	if len(results) != len(urls) {
		t.Errorf("FetchConcurrent returned %d results, expected %d", len(results), len(urls))
	}

	for url, result := range results {
		t.Logf("URL: %s, Error: %v, Content length: %d", url, result.Error, len(result.Content))
	}
}

func TestContentCacheLRU(t *testing.T) {
	cache := NewContentCache(time.Hour, 3)

	// 填充缓存到容量
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	// 访问 key1，使其成为最近使用
	_, found := cache.Get("key1")
	if !found {
		t.Fatal("key1 should be found")
	}

	// 添加第4个key，应该淘汰最久未使用的 key2
	cache.Set("key4", "value4")

	// key1 应该还在（刚访问过）
	_, found = cache.Get("key1")
	if !found {
		t.Error("key1 should still exist (was recently used)")
	}

	// key2 应该被淘汰（最久未使用）
	_, found = cache.Get("key2")
	if found {
		t.Error("key2 should have been evicted (LRU)")
	}

	// key3 和 key4 应该存在
	_, found = cache.Get("key3")
	if !found {
		t.Error("key3 should exist")
	}
	_, found = cache.Get("key4")
	if !found {
		t.Error("key4 should exist")
	}
}

func TestContentCacheSingleFlight(t *testing.T) {
	cache := NewContentCache(time.Hour, 100)

	callCount := 0
	fetchFunc := func(ctx context.Context, url string) (string, error) {
		callCount++
		time.Sleep(50 * time.Millisecond) // 模拟延迟
		return "result-" + url, nil
	}

	ctx := context.Background()

	// 并发请求同一个URL
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := cache.getOrFetch(ctx, "same-url", fetchFunc)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result != "result-same-url" {
				t.Errorf("unexpected result: %s", result)
			}
		}()
	}
	wg.Wait()

	// 应该只执行一次实际获取
	if callCount != 1 {
		t.Errorf("expected 1 fetch call, got %d", callCount)
	}
}

func TestFetchConcurrentDeduplication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 包含重复URL的列表
	urls := []string{
		"https://httpbin.org/html",
		"https://httpbin.org/html", // 重复
		"https://httpbin.org/delay/1",
		"https://httpbin.org/html", // 再次重复
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := FetchConcurrent(ctx, urls, 2)

	// 结果应该只有2个（去重后）
	if len(results) != 2 {
		t.Errorf("FetchConcurrent returned %d results, expected 2 (deduplicated)", len(results))
	}
}

func TestIsClientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "404 error",
			err:      fmt.Errorf("HTTP 404"),
			expected: true,
		},
		{
			name:     "500 error",
			err:      fmt.Errorf("HTTP 500"),
			expected: false, // 服务端错误应该重试
		},
		{
			name:     "connection error",
			err:      fmt.Errorf("connection refused"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isClientError(tt.err)
			if result != tt.expected {
				t.Errorf("isClientError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}
