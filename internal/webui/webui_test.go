package webui

import (
	"io/fs"
	"testing"
)

// TestStaticContainsIndex 负责TestStaticContainsIndex相关处理。
func TestStaticContainsIndex(t *testing.T) {
	// static、err 保存static、err，供当前处理流程使用
	static, err := Static()
	if err != nil {
		t.Fatal(err)
	}
	// data、err 保存data、err，供当前处理流程使用
	data, err := fs.ReadFile(static, "index.html")
	if err != nil || len(data) == 0 {
		t.Fatalf("embedded index missing: %v", err)
	}
}
