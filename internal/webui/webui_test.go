package webui

import (
	"io/fs"
	"testing"
)

// TestStaticContainsIndex 封装TestStaticContainsIndex业务协调。
func TestStaticContainsIndex(t *testing.T) {
	// static、err 用于本次流程后续判断的static、err
	static, err := Static()
	if err != nil {
		t.Fatal(err)
	}
	// data、err 用于本次流程后续判断的data、err
	data, err := fs.ReadFile(static, "index.html")
	if err != nil || len(data) == 0 {
		t.Fatalf("embedded index missing: %v", err)
	}
}
