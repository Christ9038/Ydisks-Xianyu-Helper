package logsafe

import (
	"errors"
	"strings"
	"testing"
)

// TestRedactionHelpers 封装TestRedactionHelpers业务协调。
func TestRedactionHelpers(t *testing.T) {
	if ID(" secret ") != ID("secret") || len(ID("secret")) != 12 {
		t.Fatal("ID should be trimmed, stable, and short")
	}
	if ID("") != "" {
		t.Fatal("empty ID should remain empty")
	}
	if // got 用于本次流程后续判断的got
	got := URL("https://example.com/path?q=token#secret"); got != "https://example.com/path" {
		t.Fatalf("URL leaked query or fragment: %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := URL("not-a-url"); got != "<redacted>" {
		t.Fatalf("invalid URL = %q", got)
	}
}

// TestErrorRedactsDiagnosticSecrets 验证错误日志不会保留 URL 查询和常见凭证键值。
func TestErrorRedactsDiagnosticSecrets(t *testing.T) {
	// err 保存包含模拟凭证和 webhook 查询参数的底层错误。
	err := errors.New(`Post "https://hooks.example.test/send?access_token=token-value": cookie=unb=account-secret password='password-value'`)
	// got 保存经过诊断脱敏的错误文本。
	got := Error(err)
	// secret 表示当前待确认未出现在诊断文本中的模拟秘密。
	for _, secret := range []string{"token-value", "account-secret", "password-value"} {
		if strings.Contains(got, secret) {
			t.Fatalf("脱敏错误仍包含秘密 %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "https://hooks.example.test/send") {
		t.Fatalf("应保留 URL 的安全路径: %s", got)
	}
}
