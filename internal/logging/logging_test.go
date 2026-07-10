package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevelAndSetLevel(t *testing.T) {
	defer Level.Set(slog.LevelInfo)

	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseLevel(%q)=%v want %v", in, got, want)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Fatal("invalid level should fail")
	}
	if err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}
	if got := Level.Level(); got != slog.LevelDebug {
		t.Fatalf("global level=%v want debug", got)
	}
}

func TestNewLoggerHonorsDynamicLevel(t *testing.T) {
	defer Level.Set(slog.LevelInfo)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text")

	Level.Set(slog.LevelWarn)
	logger.Info("hidden")
	logger.Warn("visible")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("info log should be filtered: %s", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("warn log should be emitted: %s", out)
	}
}
