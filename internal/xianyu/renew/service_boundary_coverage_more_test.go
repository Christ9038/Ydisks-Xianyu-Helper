package renew

import (
	"context"
	"net/url"
	"testing"
	"time"
)

// TestRenewServicePureBoundariesCoversModeQueryAndBusinessShapes 验证续期模式、查询参数和业务响应解析的边界。
func TestRenewServicePureBoundariesCoversModeQueryAndBusinessShapes(t *testing.T) {
	// now 保存所有 Cookie 有效期判断共用的当前时间。
	now := time.Now()
	// havanaMode、havanaReason 保存有效长登录凭证的模式判断结果。
	havanaMode, havanaReason := autoLoginModeWithoutFatigue(map[string]string{"havana_lgc_exp": futureMillis(time.Hour)}, now)
	if havanaMode != autoLoginModeHavana || havanaReason != "" {
		t.Fatalf("Havana 模式=%q 原因=%q", havanaMode, havanaReason)
	}
	// cookie3Mode、cookie3Reason 保存备用长登录凭证的模式判断结果。
	cookie3Mode, cookie3Reason := autoLoginModeWithoutFatigue(map[string]string{"cookie3_bak_exp": futureMillis(time.Hour)}, now)
	if cookie3Mode != autoLoginModeCookie3 || cookie3Reason != "" {
		t.Fatalf("Cookie3 模式=%q 原因=%q", cookie3Mode, cookie3Reason)
	}
	// expiredMode、expiredReason 保存没有有效长登录凭证的模式判断结果。
	expiredMode, expiredReason := autoLoginModeWithoutFatigue(map[string]string{}, now)
	if expiredMode != "" || expiredReason != "long_login_expired" {
		t.Fatalf("过期模式=%q 原因=%q", expiredMode, expiredReason)
	}

	// unknownCall、unknownModeErr 保存未知静默续期模式的调用结果。
	unknownCall, unknownModeErr := (Service{}).callAutoLogin(context.Background(), "unb=1", "unknown")
	_ = unknownCall
	if unknownModeErr == nil {
		t.Fatal("未知静默续期模式应返回错误")
	}
	// target 保存已有查询参数的目标 URL。
	target, parseErr := url.Parse("https://example.test/path?existing=value")
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	appendOrderedQuery(target, [][2]string{{"space key", "space value"}})
	if target.RawQuery != "existing=value&space+key=space+value" {
		t.Fatalf("有序查询参数=%q", target.RawQuery)
	}

	// businessCases 保存续期业务响应及其成功判定。
	businessCases := []struct {
		name string
		raw  []byte
		want bool
	}{
		{name: "empty", raw: nil, want: false},
		{name: "nested-content", raw: []byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`), want: true},
		{name: "unfinished", raw: []byte(`{"content":{"data":{"processFinished":false,"resultCode":100}}}`), want: false},
		{name: "string-code", raw: []byte(`{"content":{"data":{"processFinished":true,"resultCode":"100"}}}`), want: false},
		{name: "missing-content", raw: []byte(`{"message":"ok"}`), want: false},
	}
	// businessCase 表示当前待判断的续期业务响应样例。
	for _, businessCase := range businessCases {
		// businessOK 表示当前响应是否满足续期完成条件。
		businessOK := renewBusinessOK(businessCase.raw)
		if businessOK != businessCase.want {
			t.Errorf("%s: renewBusinessOK=%v want=%v", businessCase.name, businessOK, businessCase.want)
		}
	}
}
