package logsafe

import "testing"

func TestRedactionHelpers(t *testing.T) {
	if ID(" secret ") != ID("secret") || len(ID("secret")) != 12 {
		t.Fatal("ID should be trimmed, stable, and short")
	}
	if ID("") != "" {
		t.Fatal("empty ID should remain empty")
	}
	if got := URL("https://example.com/path?q=token#secret"); got != "https://example.com/path" {
		t.Fatalf("URL leaked query or fragment: %q", got)
	}
	if got := URL("not-a-url"); got != "<redacted>" {
		t.Fatalf("invalid URL = %q", got)
	}
}
