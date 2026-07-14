package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONLimitsAndRejectsTrailingValues(t *testing.T) {
	var out map[string]any
	if err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":true}`)), &out); err != nil {
		t.Fatalf("valid JSON: %v", err)
	}
	if err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{} {}`)), &out); err == nil {
		t.Fatal("trailing JSON value should fail")
	}
	oversized := `{"value":"` + strings.Repeat("x", maxJSONRequestBytes) + `"}`
	if err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(oversized)), &out); err == nil {
		t.Fatal("oversized JSON should fail")
	}
}
