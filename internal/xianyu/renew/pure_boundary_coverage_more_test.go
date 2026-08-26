package renew

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestRenewPureBoundariesCoversPendingAndNumericCode 验证续期结果等待器和业务状态数字解析器的边界行为。
func TestRenewPureBoundariesCoversPendingAndNumericCode(t *testing.T) {
	// nilResult 验证空续期结果没有待处理请求时立即返回。
	var nilResult *Result
	// pendingResult、pendingErr 保存空续期结果的等待返回值。
	pendingResult, pendingErr := nilResult.AwaitPending(context.Background())
	if pendingResult != nil || pendingErr != nil {
		t.Fatalf("空续期结果=%+v err=%v", pendingResult, pendingErr)
	}
	// closedPending 保存已关闭的底层请求结果通道。
	closedPending := make(chan pendingRenewResult)
	close(closedPending)
	// closedResult 保存底层通道已关闭的续期结果。
	closedResult := &Result{pending: closedPending}
	// pendingResult、pendingErr 保存关闭通道的等待返回值。
	pendingResult, pendingErr = closedResult.AwaitPending(context.Background())
	if pendingResult != nil || pendingErr != nil {
		t.Fatalf("关闭通道结果=%+v err=%v", pendingResult, pendingErr)
	}
	// completedPending 保存已经返回业务结果的底层请求通道。
	completedPending := make(chan pendingRenewResult, 1)
	completedPending <- pendingRenewResult{result: &Result{Success: true}}
	// completedResult 保存可成功消费底层结果的续期结果。
	completedResult := &Result{pending: completedPending}
	// pendingResult、pendingErr 保存完成通道的等待返回值。
	pendingResult, pendingErr = completedResult.AwaitPending(context.Background())
	if pendingErr != nil || pendingResult == nil || !pendingResult.Success {
		t.Fatalf("完成通道结果=%+v err=%v", pendingResult, pendingErr)
	}
	// failedPending 保存底层请求失败的通道。
	failedPending := make(chan pendingRenewResult, 1)
	failedPending <- pendingRenewResult{err: errors.New("late renew failed")}
	// failedResult 保存需要传播底层错误的续期结果。
	failedResult := &Result{pending: failedPending}
	// pendingErr 保存失败通道需要传播的底层错误。
	_, pendingErr = failedResult.AwaitPending(context.Background())
	if pendingErr == nil {
		t.Fatal("底层续期错误不应被吞掉")
	}
	// canceledContext 保存等待期间已取消的上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// canceledResult 保存用于验证取消优先级的待处理结果。
	canceledResult := &Result{pending: make(chan pendingRenewResult)}
	// pendingErr 保存取消上下文优先返回的错误。
	_, pendingErr = canceledResult.AwaitPending(canceledContext)
	if !errors.Is(pendingErr, context.Canceled) {
		t.Fatalf("取消等待错误=%v", pendingErr)
	}
	// numericCases 保存支持的数值类型和不支持类型的期望转换结果。
	numericCases := []struct {
		input any
		want  int
	}{
		{input: float64(12), want: 12},
		{input: json.Number("13"), want: 13},
		{input: int(14), want: 14},
		{input: "15", want: 0},
		{input: nil, want: 0},
	}
	// numericCase 表示当前数字编码输入及期望值。
	for _, numericCase := range numericCases {
		// got 保存当前数字编码输入的转换值。
		got := numericResultCode(numericCase.input)
		if got != numericCase.want {
			t.Fatalf("numericResultCode(%#v)=%d want %d", numericCase.input, got, numericCase.want)
		}
	}
	// unchangedCookies 验证没有响应 Cookie 时保持原凭证状态且不报告变化。
	unchangedCookies, unchangedMetadata, changed := RebaseResponseCookies("cookie", "metadata", &Result{})
	if unchangedCookies != "cookie" || unchangedMetadata != "metadata" || changed {
		t.Fatalf("无响应 Cookie 结果=%q %q changed=%v", unchangedCookies, unchangedMetadata, changed)
	}
}

// TestLongLoginBusinessResultBoundaries 覆盖长登录保存接口的顶层、嵌套和非法响应判断。
func TestLongLoginBusinessResultBoundaries(t *testing.T) {
	// cases 保存平台响应文本及其业务成功判定。
	cases := []struct {
		// raw 表示平台返回的 JSON 响应。
		raw string
		// want 表示是否应判定为业务成功。
		want bool
	}{
		{`{"success":true}`, true},
		{`{"data":{"success":true}}`, true},
		{`{"data":{"success":false}}`, false},
		{`{"success":false}`, false},
		{"not-json", false},
	}
	// testCase 表示当前待验证的长登录业务响应样例。
	for _, testCase := range cases {
		// got 表示响应是否满足保存成功条件。
		if got := longLoginSetBusinessOK([]byte(testCase.raw)); got != testCase.want {
			t.Errorf("raw=%q got=%v want=%v", testCase.raw, got, testCase.want)
		}
	}
}

// TestRenewCookieAndSkipMessageHelpers 覆盖续期 Cookie 过滤、首个非空值和静默跳过文案边界。
func TestRenewCookieAndSkipMessageHelpers(t *testing.T) {
	// cookieCases 保存 Set-Cookie 文本及其过滤后的期望数量。
	cookieCases := []struct {
		// input 是续期响应中的原始 Set-Cookie 列表。
		input []string
		// wantCount 是过滤后保留的有效条目数量。
		wantCount int
	}{
		{input: nil, wantCount: 0},
		{input: []string{"", "  ", "unb=1", "sid=2; Path=/"}, wantCount: 2},
	}
	// cookieCase 表示当前待过滤的响应 Cookie 样本。
	for _, cookieCase := range cookieCases {
		// filtered 保存去除空白条目后的 Cookie 列表。
		filtered := filterValidSetCookies(cookieCase.input)
		if len(filtered) != cookieCase.wantCount {
			t.Fatalf("filterValidSetCookies(%v)=%v", cookieCase.input, filtered)
		}
	}
	// if value 表示当前候选的首个非空续期字段。
	if value := firstNonEmpty("", "  ", "token", "later"); value != "token" {
		t.Fatalf("firstNonEmpty=%q", value)
	}
	if firstNonEmpty("", " ") != "" {
		t.Fatal("all-empty firstNonEmpty should be empty")
	}
	// messageCases 保存静默续期原因及其稳定用户文案。
	messageCases := map[string]string{
		"fatigue":            "sdkSilent 疲劳窗口内，跳过静默续期",
		"long_login_expired": "长登录凭证已过期，静默续期不应发起请求",
		"other":              "无需静默续期",
	}
	// reason、want 表示当前跳过原因和稳定文案。
	for reason, want := range messageCases {
		// message 保存当前原因映射后的业务文案。
		if message := autoLoginSkipMessage(reason); message != want {
			t.Fatalf("autoLoginSkipMessage(%q)=%q want=%q", reason, message, want)
		}
	}
	// mergedCookie 保存平面 Cookie 响应回放后的结果。
	mergedCookie, _, merged := RebaseResponseCookies("unb=1", "metadata", &Result{SetCookies: []string{"unb=2; Path=/"}})
	if !merged || !strings.Contains(mergedCookie, "unb=2") {
		t.Fatalf("RebaseResponseCookies merged=%v cookie=%q", merged, mergedCookie)
	}
}
