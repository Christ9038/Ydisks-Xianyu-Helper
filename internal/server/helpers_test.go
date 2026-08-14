package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDecodeJSONLimitsAndRejectsTrailingValues 负责TestDecodeJSONLimitsAndRejectsTrailingValues相关处理。
func TestDecodeJSONLimitsAndRejectsTrailingValues(t *testing.T) {
	// out 保存out，供当前处理流程使用
	var out map[string]any
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":true}`)), &out); err != nil {
		t.Fatalf("valid JSON: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{} {}`)), &out); err == nil {
		t.Fatal("trailing JSON value should fail")
	}
	// oversized 保存oversized，供当前处理流程使用
	oversized := `{"value":"` + strings.Repeat("x", maxJSONRequestBytes) + `"}`
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(oversized)), &out); err == nil {
		t.Fatal("oversized JSON should fail")
	}
}
