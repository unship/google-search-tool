package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogger(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, DEBUG)

	log.Info("test message")
	output := buf.String()

	if !strings.Contains(output, "test message") {
		t.Errorf("Expected log to contain 'test message', got: %s", output)
	}

	if !strings.Contains(output, "INFO") {
		t.Errorf("Expected log to contain level INFO, got: %s", output)
	}
}

func TestLogLevel(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, WARN)

	// DEBUG消息应该被过滤
	log.Debug("debug message")
	if buf.Len() > 0 {
		t.Error("DEBUG message should be filtered when level is WARN")
	}

	// WARN消息应该被记录
	log.Warn("warn message")
	if !strings.Contains(buf.String(), "warn message") {
		t.Error("WARN message should be logged")
	}
}

func TestWithField(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, INFO)

	log.WithField("key", "value").Info("message with field")
	output := buf.String()

	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("Expected log to contain field key=value, got: %s", output)
	}
}

func TestWithFields(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, INFO)

	fields := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}
	log.WithFields(fields).Info("message with fields")
	output := buf.String()

	if !strings.Contains(output, `"key1":"value1"`) || !strings.Contains(output, `"key2":42`) {
		t.Errorf("Expected log to contain fields, got: %s", output)
	}
}

func TestLogf(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, INFO)

	log.Infof("formatted %s %d", "string", 42)
	output := buf.String()

	if !strings.Contains(output, "formatted string 42") {
		t.Errorf("Expected formatted message, got: %s", output)
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
		{FATAL, "FATAL"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.level.String(); got != tt.expected {
			t.Errorf("Level(%d).String() = %s, want %s", tt.level, got, tt.expected)
		}
	}
}
