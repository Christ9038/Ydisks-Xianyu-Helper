package mtop

import (
	"context"
	"errors"
	"testing"
)

// TestAccountTaskPureHelpersCoversBoundaries 覆盖账号任务响应摘要、端点回退和重复擦亮错误识别。
func TestAccountTaskPureHelpersCoversBoundaries(t *testing.T) {
	// got 表示空响应列表的兜底摘要。
	if got := firstRet(nil); got != "未知响应" {
		t.Fatalf("empty ret=%q", got)
	}
	// got 表示非空响应列表的首条摘要。
	if got := firstRet([]string{"FAIL_BIZ"}); got != "FAIL_BIZ" {
		t.Fatalf("first ret=%q", got)
	}
	// got 表示显式配置端点的保留结果。
	if got := firstNonEmptyURL("  https://configured ", "fallback"); got != "  https://configured " {
		t.Fatalf("configured URL=%q", got)
	}
	// got 表示空配置端点的回退结果。
	if got := firstNonEmptyURL("  ", "fallback"); got != "fallback" {
		t.Fatalf("fallback URL=%q", got)
	}
	// cases 保存重复擦亮错误文本及其是否应视为幂等成功。
	cases := []struct {
		// err 表示待识别的平台业务错误。
		err error
		// want 表示该错误是否属于重复擦亮。
		want bool
	}{
		{nil, false},
		{errors.New("IDLEITEM_POLISH_AGAIN"), true},
		{errors.New("宝贝已经擦亮过了"), true},
		{errors.New("POLISH_DUPLICATE"), true},
		{errors.New("一天只能擦亮一次"), true},
		{errors.New("其他失败"), false},
	}
	// testCase 表示当前待验证的重复擦亮识别样例。
	for _, testCase := range cases {
		// got 表示当前错误是否被识别为重复擦亮。
		if got := duplicatePolishError(testCase.err); got != testCase.want {
			t.Errorf("err=%v got=%v want=%v", testCase.err, got, testCase.want)
		}
	}
}

// TestAccountTaskRequestsRejectMissingPlatformToken 覆盖账号任务请求在缺少平台签名令牌时的确定性错误路径。
func TestAccountTaskRequestsRejectMissingPlatformToken(t *testing.T) {
	// client 是不包含外部网络调用的最小平台客户端。
	client := &ClientImpl{}
	// pendingErr 保存分页参数归一化后因缺少令牌返回的错误。
	if _, pendingErr := client.FetchPendingRateOrders(context.Background(), "unb=1", 0, 0); pendingErr == nil {
		t.Fatal("missing token should reject pending rate request")
	}
	// rateErr 保存空评价文本采用默认文案后因缺少令牌返回的错误。
	if _, rateErr := client.RateBuyer(context.Background(), "unb=1", "trade", " "); rateErr == nil {
		t.Fatal("missing token should reject rate request")
	}
	// polishErr 保存商品擦亮请求因缺少令牌返回的错误。
	if _, polishErr := client.PolishItem(context.Background(), "unb=1", "item"); polishErr == nil {
		t.Fatal("missing token should reject polish request")
	}
}
