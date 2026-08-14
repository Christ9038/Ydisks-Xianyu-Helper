package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestParseLevelAndSetLevel 负责TestParseLevelAndSetLevel相关处理。
func TestParseLevelAndSetLevel(t *testing.T) {
	defer Level.Set(slog.LevelInfo)

	// cases 保存cases，供当前处理流程使用
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		// got、err 保存got、err，供当前处理流程使用
		got, err := ParseLevel(in)
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseLevel(%q)=%v want %v", in, got, want)
		}
	}
	if // err 保存err，供当前处理流程使用
	_, err := ParseLevel("verbose"); err == nil {
		t.Fatal("invalid level should fail")
	}
	if // err 保存err，供当前处理流程使用
	err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}
	if // got 保存got，供当前处理流程使用
	got := Level.Level(); got != slog.LevelDebug {
		t.Fatalf("global level=%v want debug", got)
	}
}

// TestNewLoggerHonorsDynamicLevel 负责TestNewLoggerHonorsDynamicLevel相关处理。
func TestNewLoggerHonorsDynamicLevel(t *testing.T) {
	defer Level.Set(slog.LevelInfo)
	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// logger 保存logger，供当前处理流程使用
	logger := NewLogger(&buf, "text")

	Level.Set(slog.LevelWarn)
	logger.Info("hidden")
	logger.Warn("visible")
	// out 保存out，供当前处理流程使用
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("info log should be filtered: %s", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("warn log should be emitted: %s", out)
	}
}
