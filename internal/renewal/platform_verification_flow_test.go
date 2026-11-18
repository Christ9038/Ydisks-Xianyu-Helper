package renewal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
	apirenew "xianyu-go/internal/xianyu/renew"
)

// 本文件是 GitHub #116 的平台模拟端到端测试：
// 真实调度器 + 真实续期服务（打到本地 passport 模拟）+ 真实待验证登记表 + MTOP 平台模拟器，
// 复现“silentHasLogin 返回 iframeRedirect → 平台随后吊销 Session”的时间线，
// 并断言本次改动不会新增任何 MTOP 请求（防风控约束）。

// platformVerificationSellerID 是模拟平台上的卖家账号标识。
const platformVerificationSellerID = "2200000000116"

// TestPlatformVerificationTimelineIssue116 按 issue 描述的时间线驱动完整链路；t 管理断言。
func TestPlatformVerificationTimelineIssue116(t *testing.T) {
	// ctx 是全部调用共用的上下文。
	ctx := context.Background()
	// gateway 是 MTOP 平台模拟器；账号 Cookie 由它签发。
	gateway := mtopsim.New()
	// simCookie 是模拟平台为卖家账号签发的初始 Cookie。
	simCookie := gateway.AddAccount(platformVerificationSellerID, "issue116")
	// store、cleanup 是隔离的 SQLite 存储与释放函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// stored 是写入账号表的 Cookie：模拟器签发的凭证加上未到期的长登录时间。
	stored := simCookie + "; havana_lgc_exp=" + strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	// account 是写入账号表并参与续期的运行时账号。
	account := createSchedulerAccount(t, store, "cid-issue116", stored)

	// passportHits 统计 passport 模拟收到的 silentHasLogin 次数。
	var passportHits atomic.Int32
	// passport 复刻 issue 中的响应：iframeRedirect 中间状态，同时下发新 _m_h5_tk 与 sdkSilent（疲劳窗口）。
	passport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		passportHits.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "rotated_1", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: strconv.FormatInt(time.Now().Add(30*time.Minute).UnixMilli(), 10), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"iframeRedirect":true,"iframeRedirectUrl":"https://passport.example.invalid/iv/identity_verify.htm?token=fixture-secret","resultCode":100}},"success":true}`))
	}))
	defer passport.Close()

	// notifier 记录通知；registry 是与 HTTP 验证用例共用的真实登记表实现。
	notifier := &recordingNotifier{}
	// registry 是真实待验证登记表，与 HTTP 验证用例共用同一实现。
	registry := accountapp.NewPendingVerificationRegistry()
	// s 是被测的真实续期调度器。
	s := NewScheduler(store, nil, nil, nil, notifier)
	s.SetVerificationRegistry(registry)
	s.api = apirenew.Service{HTTPClient: passport.Client(), SilentHasLoginURL: passport.URL, RetryDelay: -1}

	// 第 1 次定时续期：遇到 iframeRedirect。
	s.apiCookieRenewOne(ctx, "batch-1", account)
	if passportHits.Load() != 1 {
		t.Fatalf("首次续期应只请求一次 passport: %d", passportHits.Load())
	}
	// first 是首次续期日志：failed 且带验证标记，不含令牌。
	first := lastAPIRenewLog(t, store, account.ID)
	if first.status != "failed" || !strings.Contains(first.stepDetails, "verification_required=true") || strings.Contains(first.responseContent, "fixture-secret") {
		t.Fatalf("首次日志不符合预期: %+v", first)
	}
	if !registry.Pending(account.ID) {
		t.Fatal("首次遇到验证要求就应登记待验证")
	}
	// notices 是首次失败后的通知：立即发出一条 security_verification，且不含验证地址。
	notices := notifier.snapshot()
	if len(notices) != 1 || notices[0].eventType != "security_verification" || strings.Contains(notices[0].body, "fixture-secret") {
		t.Fatalf("首次失败应立即收到一条不含令牌的 security_verification: %+v", notices)
	}

	// 后续三次定时续期：sdkSilent 疲劳窗口使其 skipped，不再请求 passport，也不重复通知。
	for round := 2; round <= 4; round++ {
		s.apiCookieRenewOne(ctx, "batch-"+strconv.Itoa(round), account)
	}
	if passportHits.Load() != 1 {
		t.Fatalf("疲劳窗口内不应再请求 passport: %d", passportHits.Load())
	}
	// got 是疲劳窗口内最新的续期日志，应记为 skipped。
	if got := lastAPIRenewLog(t, store, account.ID); got.status != "skipped" {
		t.Fatalf("疲劳窗口内应记 skipped: %+v", got)
	}
	// 修复前的缺陷：最近 3 条含 skipped 时连续失败通知永远不成立；现在忽略 skipped，仅 1 次终态失败，不应触发 token_renewal。
	for _, notice := range notifier.snapshot() {
		if notice.eventType == "token_renewal" {
			t.Fatalf("仅 1 次终态失败不应触发 token_renewal: %+v", notices)
		}
	}
	if len(notifier.snapshot()) != 1 {
		t.Fatalf("同一验证要求不应重复通知: %+v", notifier.snapshot())
	}

	// 防风控约束：整个续期链路没有向 MTOP 发出任何请求。
	if gateway.CallCount() != 0 {
		t.Fatalf("续期与验证登记不得发起 MTOP 请求: %v", gateway.CallSequence())
	}

	// 约 15 分钟后平台吊销 Session：业务接口返回 SESSION_EXPIRED，只需一次探测。
	gateway.ExpireSession(platformVerificationSellerID)
	// client 是连接模拟器的真实 MTOP 客户端；detail 是账号当前保存的凭证。
	client := gateway.Client()
	// detail、err 是账号当前保存的凭证及读取错误，用于探测 Session。
	detail, err := store.Cookies.GetDetails(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	// status 是登录态探测结果。
	status, err := client.CheckLoginStatusContext(ctx, detail.Value)
	if err != nil && status == nil {
		t.Fatalf("探测失败: %v", err)
	}
	if status == nil || status.Status != mtop.LoginStatusSessionExpired {
		t.Fatalf("Session 被吊销后应判定为 session_expired: %+v err=%v", status, err)
	}
	if gateway.CallCount() != 1 {
		t.Fatalf("Session 探测应只产生一次 MTOP 请求: %v", gateway.CallSequence())
	}

	// 用户完成人工验证并重新授权：平台签发新 Session，新 Cookie 下登录态应恢复。
	fresh := gateway.Relogin(platformVerificationSellerID)
	// recovered 是重新授权后的登录态探测结果。
	recovered, recoverErr := client.CheckLoginStatusContext(ctx, fresh)
	if recoverErr != nil || recovered == nil || (recovered.Status != mtop.LoginStatusSuccess && recovered.Status != mtop.LoginStatusTokenRefreshed) {
		t.Fatalf("重新授权后登录态应恢复: %+v err=%v", recovered, recoverErr)
	}
}

// TestPlatformVerificationRiskControlBlocksNothingExtra 验证账号处于 MTOP 风控期时，续期验证登记与通知链路仍不发出任何 MTOP 请求；t 管理断言。
func TestPlatformVerificationRiskControlBlocksNothingExtra(t *testing.T) {
	// gateway 是处于风控状态的 MTOP 平台模拟器。
	gateway := mtopsim.New()
	// simCookie 是模拟平台为处于风控的账号签发的 Cookie。
	simCookie := gateway.AddAccount(platformVerificationSellerID, "risk")
	gateway.SetRiskControl(platformVerificationSellerID, true)
	// store、cleanup 是隔离的 SQLite 存储与释放函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// account 是写入账号表的风控期账号。
	account := createSchedulerAccount(t, store, "cid-issue116-risk", simCookie+"; havana_lgc_exp="+strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10))
	// srv 是返回 iframeRedirect 的 passport 模拟服务。
	srv := iframeRedirectServer(0)
	defer srv.Close()
	// notifier 记录续期过程发出的通知。
	notifier := &recordingNotifier{}
	// s 是被测的真实续期调度器。
	s := NewScheduler(store, nil, nil, nil, notifier)
	s.SetVerificationRegistry(accountapp.NewPendingVerificationRegistry())
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	s.apiCookieRenewOne(context.Background(), "batch-risk", account)
	if gateway.CallCount() != 0 {
		t.Fatalf("风控期内续期验证链路不得请求 MTOP: %v", gateway.CallSequence())
	}
	if len(notifier.snapshot()) != 1 {
		t.Fatalf("仍应通知用户完成安全验证: %+v", notifier.snapshot())
	}
}
