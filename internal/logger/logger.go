package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level 日志级别
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Logger 结构化日志记录器
type Logger struct {
	writer io.Writer
	level  Level
	fields map[string]interface{}
	mu     sync.RWMutex
}

// New 创建新的日志记录器
func New(w io.Writer, level Level) *Logger {
	if w == nil {
		w = os.Stderr
	}
	return &Logger{
		writer: w,
		level:  level,
		fields: make(map[string]interface{}),
	}
}

// Default 返回默认日志记录器
func Default() *Logger {
	return New(os.Stderr, INFO)
}

// WithField 添加字段
func (l *Logger) WithField(key string, value interface{}) *Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value
	return &Logger{
		writer: l.writer,
		level:  l.level,
		fields: newFields,
	}
}

// WithFields 添加多个字段
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	return &Logger{
		writer: l.writer,
		level:  l.level,
		fields: newFields,
	}
}

// log 内部日志方法
func (l *Logger) log(level Level, msg string) {
	l.mu.RLock()
	currentLevel := l.level
	l.mu.RUnlock()
	if level < currentLevel {
		return
	}

	entry := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"level":     level.String(),
		"message":   msg,
	}

	for k, v := range l.fields {
		entry[k] = v
	}

	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(l.writer, "{\"error\":\"failed to marshal log entry\"}\n")
		return
	}

	fmt.Fprintln(l.writer, string(data))
}

// logf 内部格式化日志方法
func (l *Logger) logf(level Level, format string, args ...interface{}) {
	l.log(level, fmt.Sprintf(format, args...))
}

// Debug 调试日志
func (l *Logger) Debug(msg string) {
	l.log(DEBUG, msg)
}

// Debugf 格式化调试日志
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logf(DEBUG, format, args...)
}

// Info 信息日志
func (l *Logger) Info(msg string) {
	l.log(INFO, msg)
}

// Infof 格式化信息日志
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logf(INFO, format, args...)
}

// Warn 警告日志
func (l *Logger) Warn(msg string) {
	l.log(WARN, msg)
}

// Warnf 格式化警告日志
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logf(WARN, format, args...)
}

// Error 错误日志
func (l *Logger) Error(msg string) {
	l.log(ERROR, msg)
}

// Errorf 格式化错误日志
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logf(ERROR, format, args...)
}

// Fatal 致命错误日志
func (l *Logger) Fatal(msg string) {
	l.log(FATAL, msg)
	os.Exit(1)
}

// Fatalf 格式化致命错误日志
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.logf(FATAL, format, args...)
	os.Exit(1)
}

// SetLevel 设置日志级别（线程安全）
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel 获取日志级别（线程安全）
func (l *Logger) GetLevel() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}
