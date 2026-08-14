package adapter

import (
	"context"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
)

// TestOnPasswordLoginRefreshUsesPlatformRuntimeData 验证协议续期不解密登录密码。
func TestOnPasswordLoginRefreshUsesPlatformRuntimeData(t *testing.T) {
	// store 是本测试使用的账号数据库；cleanup 负责关闭数据库连接。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是协议续期测试共用的上下文。
	ctx := context.Background()
	// updateErr 表示写入协议续期所需 Cookie 标记失败的原因。
	if updateErr := store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; _m_h5_tk=tk; havana_lgc_exp=9999999999999"); updateErr != nil {
		t.Fatalf("prepare renewal cookie: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"protocol-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// renewSvc 是返回成功结果的协议续期服务桩。
	renewSvc, closeRenew := verifiedRenewService(t)
	defer closeRenew()
	// adapter 是待验证平台凭证边界的装配器。
	adapter := New(store, nil, nil)
	adapter.SetRenewService(renewSvc)
	if !adapter.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("损坏登录密码不应阻断协议 Cookie 续期")
	}
}

// TestFetchOrderDetailUsesPlatformRuntimeData 验证订单详情 MTOP 请求不解密登录密码。
func TestFetchOrderDetailUsesPlatformRuntimeData(t *testing.T) {
	// store 是本测试使用的账号数据库；cleanup 负责关闭数据库连接。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是订单详情测试共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"order-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// adapter 是待验证平台凭证边界的装配器。
	adapter := New(store, nil, nil)
	adapter.SetOrderDetailClient(&fakeOrderDetailClient{detail: &mtop.OrderDetailResult{
		Quantity: "1", SpecName: "套餐", SpecValue: "30天", Amount: "9.9",
	}})
	// detail 和 fetchErr 分别是 MTOP 返回的订单详情及调用错误。
	detail, fetchErr := adapter.FetchOrderDetail(ctx, "cid", "platform-order", "item", "buyer", "")
	if fetchErr != nil || detail == nil || detail.SpecValue != "30天" {
		t.Fatalf("detail=%+v err=%v", detail, fetchErr)
	}
}

// TestOnTokenCaptchaVerificationUsesPlatformRuntimeData 验证 token 风控读取设置时不解密登录密码。
func TestOnTokenCaptchaVerificationUsesPlatformRuntimeData(t *testing.T) {
	// store 是本测试使用的账号数据库；cleanup 负责关闭数据库连接。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是 token 风控测试共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET show_browser=1, username=?,password=? WHERE id=?`,
		"captcha-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// browser 是记录风控恢复调用参数的浏览器桩。
	browser := &fakeBrowser{tokenCaptchaResult: "unb=1; x5sec=recovered;"}
	// requester 是返回新风控验证链接的请求桩。
	requester := &fakeCaptchaRequester{result: &mtop.FreshCaptchaResult{
		VerificationURL: "https://fresh.example/captcha", UpdatedCookies: "unb=1; x5sec=recovered;", TokenOK: true,
	}}
	// adapter 是待验证平台凭证边界的装配器。
	adapter := New(store, nil, nil)
	adapter.SetBrowser(browser)
	adapter.SetTokenCaptchaRequester(requester)
	t.Setenv("BROWSER_HEADLESS", "")
	// result 和 ok 分别是风控恢复结果及成功标志。
	result, ok := adapter.OnTokenCaptchaVerification(ctx, "cid", "unb=1; _m_h5_tk=old;", "https://old.example/captcha", "device")
	if !ok || result == nil || browser.tokenCaptchaHeadless {
		t.Fatalf("token captcha result=%+v ok=%v headless=%v", result, ok, browser.tokenCaptchaHeadless)
	}
}
