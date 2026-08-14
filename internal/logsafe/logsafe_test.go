package logsafe

import "testing"

// TestRedactionHelpers 负责TestRedactionHelpers相关处理。
func TestRedactionHelpers(t *testing.T) {
	if ID(" secret ") != ID("secret") || len(ID("secret")) != 12 {
		t.Fatal("ID should be trimmed, stable, and short")
	}
	if ID("") != "" {
		t.Fatal("empty ID should remain empty")
	}
	if // got 保存got，供当前处理流程使用
	got := URL("https://example.com/path?q=token#secret"); got != "https://example.com/path" {
		t.Fatalf("URL leaked query or fragment: %q", got)
	}
	if // got 保存got，供当前处理流程使用
	got := URL("not-a-url"); got != "<redacted>" {
		t.Fatalf("invalid URL = %q", got)
	}
}
