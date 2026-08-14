package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Level is the process-wide dynamic slog level.
// Level 保存Level，供当前处理流程使用
var Level slog.LevelVar

// init 负责init相关处理。
func init() {
	Level.Set(slog.LevelInfo)
}

// NewLogger creates a slog logger wired to the dynamic Level.
// NewLogger 负责NewLogger相关处理。
func NewLogger(w io.Writer, format string) *slog.Logger {
	// opts 保存opts，供当前处理流程使用
	opts := &slog.HandlerOptions{Level: &Level}
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

// SetLevel updates the process-wide log level.
// SetLevel 设置Level。
func SetLevel(raw string) error {
	// lv、err 保存lv、err，供当前处理流程使用
	lv, err := ParseLevel(raw)
	if err != nil {
		return err
	}
	Level.Set(lv)
	return nil
}

// ParseLevel parses debug/info/warn/error.
// ParseLevel 解析Level。
func ParseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("无效日志等级: %s", raw)
	}
}
