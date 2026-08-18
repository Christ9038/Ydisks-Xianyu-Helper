package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDecodeJSONLimitsAndRejectsTrailingValues 封装TestDecodeJSONLimitsAndRejectsTrailingValues业务协调。
func TestDecodeJSONLimitsAndRejectsTrailingValues(t *testing.T) {
	// out 用于本次流程后续判断的out
	var out map[string]any
	if // err 用于本次流程后续判断的err
	err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":true}`)), &out); err != nil {
		t.Fatalf("valid JSON: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{} {}`)), &out); err == nil {
		t.Fatal("trailing JSON value should fail")
	}
	// oversized 用于本次流程后续判断的oversized
	oversized := `{"value":"` + strings.Repeat("x", maxJSONRequestBytes) + `"}`
	if // err 用于本次流程后续判断的err
	err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(oversized)), &out); err == nil {
		t.Fatal("oversized JSON should fail")
	}
}
