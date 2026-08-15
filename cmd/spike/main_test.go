//go:build debug || tools

package main

import (
	"reflect"
	"testing"
)

// TestDiagnosticFieldNamesOnlyReturnsSortedKeys 验证 spike 诊断摘要只暴露字段结构且顺序稳定。
func TestDiagnosticFieldNamesOnlyReturnsSortedKeys(t *testing.T) {
	// message 保存含有模拟敏感正文的解密消息；正文不应进入摘要结果。
	message := map[string]any{
		"z":       "cookie=account-secret",
		"a":       map[string]any{"password": "password-secret"},
		"token":   "token-secret",
		"message": "买家私信正文",
	}
	// got 保存安全诊断摘要中的排序字段名。
	got := diagnosticFieldNames(message)
	// want 保存预期的稳定字段顺序。
	want := []string{"a", "message", "token", "z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnosticFieldNames()=%v, want %v", got, want)
	}
}
