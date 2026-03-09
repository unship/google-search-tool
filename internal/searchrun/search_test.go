package searchrun

import (
	"context"
	"testing"
	"time"
)

func TestCleanGoogleRedirect(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "google redirect URL",
			input:    "https://www.google.com/url?url=https%3A%2F%2Fexample.com",
			expected: "https://example.com",
		},
		{
			name:     "normal URL",
			input:    "https://example.com/path",
			expected: "https://example.com/path",
		},
		{
			name:     "google redirect with other params",
			input:    "https://www.google.com/url?sa=t&url=https%3A%2F%2Fgithub.com%2Ftest",
			expected: "https://github.com/test",
		},
		{
			name:     "invalid URL",
			input:    "://invalid-url",
			expected: "://invalid-url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanGoogleRedirect(tt.input)
			if result != tt.expected {
				t.Errorf("cleanGoogleRedirect(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanExtractedLink(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "google redirect then strip utm",
			input:    "https://www.google.com/url?url=https%3A%2F%2Fexample.com%2Fpage%3Futm_source%3Dfoo",
			expected: "https://example.com/page",
		},
		{
			name:     "strip utm and gclid",
			input:    "https://example.com/path?utm_source=google&gclid=abc&keep=1",
			expected: "https://example.com/path?keep=1",
		},
		{
			name:     "remove text fragment",
			input:    "https://example.com/doc#:~:text=highlight",
			expected: "https://example.com/doc",
		},
		{
			name:     "trim trailing slash",
			input:    "https://example.com/foo/",
			expected: "https://example.com/foo",
		},
		{
			name:     "keep normal fragment",
			input:    "https://example.com/page#section",
			expected: "https://example.com/page#section",
		},
		{
			name:     "empty after trim",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanExtractedLink(tt.input)
			if result != tt.expected {
				t.Errorf("cleanExtractedLink(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsAdOrIrrelevant(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		title    string
		expected bool
	}{
		{
			name:     "normal result",
			link:     "https://example.com",
			title:    "Example Title",
			expected: false,
		},
		{
			name:     "ad title",
			link:     "https://example.com",
			title:    "Sponsored",
			expected: true,
		},
		{
			name:     "google sorry page",
			link:     "https://www.google.com/sorry",
			title:    "Sorry",
			expected: true,
		},
		{
			name:     "empty title",
			link:     "https://example.com",
			title:    "",
			expected: true,
		},
		{
			name:     "text fragment link",
			link:     "https://example.com#:~:text=something",
			title:    "Example",
			expected: true,
		},
		{
			name:     "doubleclick domain",
			link:     "https://doubleclick.net/ad",
			title:    "Ad",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAdOrIrrelevant(tt.link, tt.title)
			if result != tt.expected {
				t.Errorf("isAdOrIrrelevant(%q, %q) = %v, want %v", tt.link, tt.title, result, tt.expected)
			}
		})
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		maxLen  int
		wantLen int
		wantEnd string
	}{
		{
			name:    "short title",
			input:   "Short",
			maxLen:  300,
			wantLen: 5,
			wantEnd: "Short",
		},
		{
			name:    "long title",
			input:   string(make([]byte, 400)),
			maxLen:  300,
			wantLen: 303,
			wantEnd: "...",
		},
		{
			name:    "zero maxLen uses default",
			input:   string(make([]byte, 350)),
			maxLen:  0,
			wantLen: 303,
			wantEnd: "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateTitle(tt.input, tt.maxLen)
			if len(result) != tt.wantLen {
				t.Errorf("truncateTitle() length = %d, want %d", len(result), tt.wantLen)
			}
			if tt.wantEnd != "" && !endsWith(result, tt.wantEnd) {
				t.Errorf("truncateTitle() = %q, should end with %q", result, tt.wantEnd)
			}
		})
	}
}

func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

func TestFormatLLM(t *testing.T) {
	items := []SearchItem{
		{Title: "Test 1", URL: "https://example1.com"},
		{Title: "Test 2", URL: "https://example2.com"},
	}

	result := FormatLLM(items)
	if result == "" {
		t.Error("FormatLLM returned empty string")
	}
	if !contains(result, "Found 2 search results") {
		t.Error("FormatLLM missing header")
	}
	if !contains(result, "Test 1") || !contains(result, "Test 2") {
		t.Error("FormatLLM missing items")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Error("Rate limiter not working: requests too fast")
	}
}

func TestSearchResultTypes(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		wantErr  bool
		wantLen  int
	}{
		{
			name:     "valid string result",
			jsonData: `[{"title":"Test","url":"https://example.com"}]`,
			wantErr:  false,
			wantLen:  1,
		},
		{
			name:     "valid array result",
			jsonData: `[]`,
			wantErr:  false,
			wantLen:  0,
		},
		{
			name:     "invalid JSON",
			jsonData: `invalid`,
			wantErr:  true,
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := parseSearchResults([]byte(tt.jsonData))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSearchResults() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(items) != tt.wantLen {
				t.Errorf("parseSearchResults() got %d items, want %d", len(items), tt.wantLen)
			}
		})
	}
}
