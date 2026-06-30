package webui

import (
	"io/fs"
	"testing"
)

func TestStaticContainsIndex(t *testing.T) {
	static, err := Static()
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(static, "index.html")
	if err != nil || len(data) == 0 {
		t.Fatalf("embedded index missing: %v", err)
	}
}
