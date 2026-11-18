package renew

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// verificationBody 是 iframeRedirect 中间响应的人工样本；URL 为伪造值，不含真实令牌。
const verificationBody = `{"content":{"data":{"iframeRedirect":true,"iframeRedirectUrl":"https://passport.example.invalid/iv/identity_verify.htm?token=fixture-secret","resultCode":100}},"success":true}`

// TestRenewVerificationURL 验证 iframeRedirect 的严格识别：只认布尔 true，并支持嵌套 data.content 形态；t 管理断言。
func TestRenewVerificationURL(t *testing.T) {
	// cases 列出响应正文、是否需要验证与期望的验证地址。
	cases := []struct {
		// name 是子用例名称。
		name string
		// body 是被解析的响应正文。
		body string
		// wantRequired 表示是否应识别为需要验证。
		wantRequired bool
		// wantURL 是期望提取出的验证地址。
		wantURL string
	}{
		{"顶层 content", verificationBody, true, "https://passport.example.invalid/iv/identity_verify.htm?token=fixture-secret"},
		{"嵌套 data.content", `{"data":{"content":{"data":{"iframeRedirect":true,"iframeRedirectUrl":" https://x.invalid/a "}}}}`, true, "https://x.invalid/a"},
		{"缺少地址仍需验证", `{"content":{"data":{"iframeRedirect":true}}}`, true, ""},
		{"字符串 true 不触发", `{"content":{"data":{"iframeRedirect":"true","iframeRedirectUrl":"u"}}}`, false, ""},
		{"false 不触发", `{"content":{"data":{"iframeRedirect":false}}}`, false, ""},
		{"成功响应不触发", `{"content":{"data":{"processFinished":true,"resultCode":100}}}`, false, ""},
		{"空正文", ``, false, ""},
		{"非 JSON", `<html>`, false, ""},
		{"缺少 content.data", `{"content":{}}`, false, ""},
	}
	// sample 是当前被验证的人工样本。
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			// gotURL、gotRequired 保存识别结果。
			gotURL, gotRequired := renewVerificationURL([]byte(sample.body))
			if gotRequired != sample.wantRequired || gotURL != sample.wantURL {
				t.Fatalf("got (%q,%v) want (%q,%v)", gotURL, gotRequired, sample.wantURL, sample.wantRequired)
			}
		})
	}
}

// TestRenewBusinessOKUnchangedForVerification 验证带 iframeRedirect 的响应仍不被判为业务成功，保持官方插件语义；t 管理断言。
func TestRenewBusinessOKUnchangedForVerification(t *testing.T) {
	if renewBusinessOK([]byte(verificationBody)) {
		t.Fatal("iframeRedirect 中间响应不得判为续期成功")
	}
	if !renewBusinessOK([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100,"iframeRedirect":false}}}`)) {
		t.Fatal("processFinished=true 且 resultCode=100 必须仍判为成功")
	}
}

// TestRedactVerificationBody 验证脱敏会隐藏所有嵌套层级的验证地址且保持其余字段；t 管理断言。
func TestRedactVerificationBody(t *testing.T) {
	// nested 把验证地址同时放在对象与数组内，覆盖递归分支。
	nested := `{"a":[{"iframeRedirectUrl":"https://x.invalid/secret"}],"b":{"iframeRedirectUrl":"","resultCode":100}}`
	// redacted 为脱敏后的文本。
	redacted := string(redactVerificationBody([]byte(nested)))
	if strings.Contains(redacted, "secret") || !strings.Contains(redacted, "<redacted>") {
		t.Fatalf("验证地址未脱敏: %s", redacted)
	}
	if !strings.Contains(redacted, `"resultCode":100`) {
		t.Fatalf("其余字段不应丢失: %s", redacted)
	}
	// got 保存 string 的结果，供后续分支和断言使用。
	if got := string(redactVerificationBody([]byte("not json"))); strings.Contains(got, "not json") {
		t.Fatalf("无法解析时不得回显原文: %s", got)
	}
}

// TestRenewAPIFirstVerificationRequired 验证定时续期遇到 iframeRedirect 时：判失败、标记需验证、
// 仍接收 Set-Cookie、验证地址只能经 VerificationURL 读取且不出现在响应文本中；t 管理断言。
func TestRenewAPIFirstVerificationRequired(t *testing.T) {
	// srv 模拟平台返回 iframeRedirect 中间状态并下发新 Cookie。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "rotated_1"})
		_, _ = w.Write([]byte(verificationBody))
	}))
	defer srv.Close()
	// cookies 使用未进入疲劳窗口、长登录未过期的伪造 Cookie。
	cookies := "unb=1; havana_lgc_exp=" + strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	// service 把请求指向本地模拟服务并跳过官网等待。
	service := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// result、err 为一次续期的结果。
	result, err := service.RenewAPIFirst(context.Background(), cookies)
	if err != nil || result == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Success || !result.NeedPasswordLogin || !result.VerificationRequired {
		t.Fatalf("应判失败且标记需验证: %+v", result)
	}
	if !strings.Contains(result.NewCookies, "_m_h5_tk=rotated_1") {
		t.Fatalf("响应 Cookie 仍应被接收: %q", result.NewCookies)
	}
	// got 保存 result.VerificationURL 的结果，供后续分支和断言使用。
	if got := result.VerificationURL(); !strings.Contains(got, "/iv/identity_verify.htm") {
		t.Fatalf("验证地址应可经方法读取: %q", got)
	}
	if strings.Contains(result.ResponseText, "fixture-secret") || strings.Contains(result.Message, "fixture-secret") {
		t.Fatalf("验证地址令牌不得进入响应文本或消息: %q / %q", result.ResponseText, result.Message)
	}
	if len(result.StepDetails) != 1 || !result.StepDetails[0].VerificationRequired || result.StepDetails[0].BusinessOK {
		t.Fatalf("步骤应标记需验证: %+v", result.StepDetails)
	}
}

// TestResultVerificationURLNilSafe 验证空结果读取验证地址不会 panic；t 管理断言。
func TestResultVerificationURLNilSafe(t *testing.T) {
	// nilResult 是未初始化的续期结果。
	var nilResult *Result
	if nilResult.VerificationURL() != "" {
		t.Fatal("空结果应返回空串")
	}
}
