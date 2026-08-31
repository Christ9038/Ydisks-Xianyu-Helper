package logsafe

import (
	"errors"
	"strings"
	"testing"
)

// TestRedactionHelpers 封装TestRedactionHelpers业务协调。
func TestRedactionHelpers(t *testing.T) {
	if // emptyError 是 nil 错误的安全空文本。
	emptyError := Error(nil); emptyError != "" {
		t.Fatalf("nil 错误应返回空文本: %q", emptyError)
	}
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

// TestExternalErrorRedactsCredentialPaths 验证外部网络错误不会保留 Telegram Token、Webhook 路径、用户信息或查询参数。
func TestExternalErrorRedactsCredentialPaths(t *testing.T) {
	if externalURLOrigin("not-a-url") != "<redacted>" {
		t.Fatal("无效外部地址应完全脱敏")
	}
	if externalQuotedTarget("invalid") != "<redacted>" {
		t.Fatal("缺少动作分隔符的请求地址应完全脱敏")
	}
	// diagnosticErr 模拟 net/http 在连接失败时返回的完整请求地址和附加凭证键值。
	diagnosticErr := errors.New(`Post "https://user:password@api.telegram.org/bot123456:REVIEW_SECRET/sendMessage?access_token=query-secret": dial tcp: connection refused`)
	// sanitized 保存面向日志、数据库和内部包装的外部错误文本。
	sanitized := ExternalError(diagnosticErr)
	// secret 表示每个不得保留在安全诊断文本中的模拟秘密。
	for _, secret := range []string{"user", "password", "123456:REVIEW_SECRET", "sendMessage", "query-secret"} {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("外部错误仍包含秘密 %q: %s", secret, sanitized)
		}
	}
	if !strings.Contains(sanitized, "https://api.telegram.org/<redacted>") || !strings.Contains(sanitized, "connection refused") {
		t.Fatalf("外部错误未保留安全诊断上下文: %s", sanitized)
	}
	// malformedDiagnostic 模拟配置错误导致 URL 解析器直接回显不合法 Webhook 秘密的场景。
	malformedDiagnostic := ExternalError(errors.New(`parse "://MALFORMED_WEBHOOK_SECRET/path": missing protocol scheme`))
	if strings.Contains(malformedDiagnostic, "MALFORMED_WEBHOOK_SECRET") || !strings.Contains(malformedDiagnostic, `parse "<redacted>"`) {
		t.Fatalf("不合法外部地址未完全脱敏: %s", malformedDiagnostic)
	}
	if // emptyExternalError 验证 nil 外部错误保持安全空文本。
	emptyExternalError := ExternalError(nil); emptyExternalError != "" {
		t.Fatalf("nil 外部错误应返回空文本: %q", emptyExternalError)
	}
}
