package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Level is the process-wide dynamic slog level.
var Level slog.LevelVar

func init() {
	Level.Set(slog.LevelInfo)
}

// NewLogger creates a slog logger wired to the dynamic Level.
func NewLogger(w io.Writer, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: &Level}
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

// SetLevel updates the process-wide log level.
func SetLevel(raw string) error {
	lv, err := ParseLevel(raw)
	if err != nil {
		return err
	}
	Level.Set(lv)
	return nil
}

// ParseLevel parses debug/info/warn/error.
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
