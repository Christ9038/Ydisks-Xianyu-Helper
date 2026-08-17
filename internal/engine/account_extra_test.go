package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// riskCountingMTop 保存riskCountingMTop，供当前处理流程使用
type riskCountingMTop struct {
	fakeRunMtop
	calls int
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (m *riskCountingMTop) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	m.calls++
	if m.calls > 1 {
		return &mtop.RefreshResult{AccessToken: "standard-token", AccessTokenExpireAt: time.Now().Add(time.Hour).Unix(), UpdatedCookies: "unb=123; _m_h5_tk=recovered;"}, nil
	}
	return nil, &mtop.RiskVerificationError{Ret: []string{"FAIL_SYS_USER_VALIDATE"}, VerificationURL: "https://verify.example"}
}

// tokenRecoveredHandler 保存令牌RecoveredHandler，供当前处理流程使用
type tokenRecoveredHandler struct{ recordingHandler }

// OnTokenCaptchaVerification 负责On令牌CaptchaVerification相关处理。
func (h *tokenRecoveredHandler) OnTokenCaptchaVerification(context.Context, string, string, string, string) (*mtop.RefreshResult, bool) {
	return &mtop.RefreshResult{AccessToken: "recovered-token", UpdatedCookies: "unb=123; _m_h5_tk=recovered;"}, true
}

// rejectingTokenCaptchaHandler 保存rejecting令牌CaptchaHandler，供当前处理流程使用
type rejectingTokenCaptchaHandler struct {
	recordingHandler
	calls int
}

// OnTokenCaptchaVerification 负责On令牌CaptchaVerification相关处理。
func (h *rejectingTokenCaptchaHandler) OnTokenCaptchaVerification(context.Context, string, string, string, string) (*mtop.RefreshResult, bool) {
	h.calls++
	return nil, false
}

// capturingCaptchaHandler 保存capturingCaptchaHandler，供当前处理流程使用
type capturingCaptchaHandler struct {
	recordingHandler
	cookieStr string
	deviceID  string
}

// OnTokenCaptchaVerification 负责On令牌CaptchaVerification相关处理。
func (h *capturingCaptchaHandler) OnTokenCaptchaVerification(_ context.Context, _, cookieStr, _, deviceID string) (*mtop.RefreshResult, bool) {
	h.cookieStr = cookieStr
	h.deviceID = deviceID
	return &mtop.RefreshResult{UpdatedCookies: cookieStr + "; x5sec=fresh"}, true
}

// responseCookieRiskMTop 保存响应登录凭证RiskMTop，供当前处理流程使用
type responseCookieRiskMTop struct{ calls int }

// FetchUserProfile 负责Fetch用户Profile相关处理。
func (m *responseCookieRiskMTop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}

// ConsignContext 负责Consign上下文相关处理。
func (m *responseCookieRiskMTop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return true, nil, "", nil
}

// FetchItemsPage 负责Fetch商品列表页码相关处理。
func (m *responseCookieRiskMTop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// FetchAllItems 负责FetchAll商品列表相关处理。
func (m *responseCookieRiskMTop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// PublishItem 负责发布商品相关处理。
func (m *responseCookieRiskMTop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (m *responseCookieRiskMTop) RefreshTokenWithDeviceIDContext(_ context.Context, cookieStr, _ string) (*mtop.RefreshResult, error) {
	m.calls++
	if m.calls == 1 {
		// updated 保存updated，供当前处理流程使用
		updated := strings.Replace(cookieStr, "_m_h5_tk=tk_1", "_m_h5_tk=server_1", 1)
		return &mtop.RefreshResult{UpdatedCookies: updated}, &mtop.RiskVerificationError{
			Ret: []string{"FAIL_SYS_USER_VALIDATE"}, VerificationURL: "https://verify.example/punish",
		}
	}
	return &mtop.RefreshResult{AccessToken: "standard-after-captcha", AccessTokenExpireAt: time.Now().Add(time.Hour).Unix(), UpdatedCookies: cookieStr}, nil
}

// TestRefreshTokenRetriesStandardRequestAfterCaptchaRecovery 负责TestRefresh令牌RetriesStandard请求AfterCaptchaRecovery相关处理。
func TestRefreshTokenRetriesStandardRequestAfterCaptchaRecovery(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()
	// client 保存client，供当前处理流程使用
	client := &riskCountingMTop{}
	acc.mtop = client
	acc.handler = &tokenRecoveredHandler{}

	// token、cookies、err 保存token、cookies、err，供当前处理流程使用
	token, cookies, err := acc.refreshToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "standard-token" || !strings.Contains(cookies, "recovered") {
		t.Fatalf("token=%q cookies=%q", token, cookies)
	}
	if client.calls != 2 {
		t.Fatalf("refresh calls=%d want 2", client.calls)
	}
}

// TestRefreshTokenCaptchaFailureEntersCallerCooldown 负责TestRefresh令牌CaptchaFailureEntersCallerCooldown相关处理。
func TestRefreshTokenCaptchaFailureEntersCallerCooldown(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	// client 保存client，供当前处理流程使用
	client := &riskCountingMTop{}
	// handler 保存handler，供当前处理流程使用
	handler := &rejectingTokenCaptchaHandler{}
	acc.mtop = client
	acc.handler = handler

	if // err 保存err，供当前处理流程使用
	_, _, err := acc.refreshToken(context.Background()); !mtop.IsRiskVerificationErr(err) {
		t.Fatalf("first refresh error=%v want risk verification", err)
	}
	if // err 保存err，供当前处理流程使用
	_, _, err := acc.refreshToken(context.Background()); !errors.Is(err, errTokenCaptchaCooldown) {
		t.Fatalf("second refresh error=%v want cooldown", err)
	}
	if client.calls != 1 || handler.calls != 1 {
		t.Fatalf("cooldown must suppress repeated API/solver calls: api=%d solver=%d", client.calls, handler.calls)
	}
}

// TestRefreshTokenPersistsResponseCookiesBeforeCaptchaRecovery 负责TestRefresh令牌Persists响应CookiesBeforeCaptchaRecovery相关处理。
func TestRefreshTokenPersistsResponseCookiesBeforeCaptchaRecovery(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, &responseCookieRiskMTop{})
	defer cleanup()
	// handler 保存handler，供当前处理流程使用
	handler := &capturingCaptchaHandler{}
	acc.handler = handler

	// token、err 保存token、err，供当前处理流程使用
	token, _, err := acc.refreshToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "standard-after-captcha" {
		t.Fatalf("token=%q", token)
	}
	if !strings.Contains(handler.cookieStr, "_m_h5_tk=server_1") {
		t.Fatalf("captcha handler 未收到响应先下发的 Cookie: %q", handler.cookieStr)
	}
	if handler.deviceID != acc.deviceID {
		t.Fatalf("captcha deviceID=%q want %q", handler.deviceID, acc.deviceID)
	}
	// saved、err 保存saved、err，供当前处理流程使用
	saved, err := store.Cookies.GetValue(context.Background(), "cid")
	if err != nil || !strings.Contains(saved, "_m_h5_tk=server_1") || !strings.Contains(saved, "x5sec=fresh") {
		t.Fatalf("响应/验证 Cookie 未完整持久化: saved=%q err=%v", saved, err)
	}
}

// TestStop_IdempotentAndClearsTimers Stop 重复调用幂等；调用后防抖定时器被清空。
func TestStop_IdempotentAndClearsTimers(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	// 调度一个防抖定时器，验证 Stop 会清空。
	chat := ChatMessage{AccountID: "cid", ChatID: "c1", Text: "hi"}
	acc.scheduleDebouncedReply(chat)
	acc.debounceMu.Lock()
	if len(acc.debounceTimers) != 1 {
		t.Fatalf("应有 1 个定时器，got %d", len(acc.debounceTimers))
	}
	acc.debounceMu.Unlock()

	acc.Stop()
	// status 保存状态，供当前处理流程使用
	status := acc.RuntimeStatus()
	if status.State != RuntimeStopped {
		t.Errorf("state=%q want %q", status.State, RuntimeStopped)
	}
	acc.debounceMu.Lock()
	// timers 保存timers，供当前处理流程使用
	timers := len(acc.debounceTimers)
	acc.debounceMu.Unlock()
	if timers != 0 {
		t.Errorf("Stop 应清空防抖定时器，剩 %d", timers)
	}

	// 重复 Stop 幂等，不 panic。
	acc.Stop()
	acc.Stop()
	if acc.RuntimeStatus().State != RuntimeStopped {
		t.Error("重复 Stop 后状态应仍为 stopped")
	}
}

// TestStop_CancelFunc_WhenNil stopFn 为 nil 时不 panic（未启动 Run 的账号也能 Stop）。
func TestStop_CancelFunc_WhenNil(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	if acc.lifecycle.stopFn != nil {
		t.Fatal("未启动 Run 时 stopFn 应为 nil")
	}
	// 不应 panic。
	acc.Stop()
}

// TestRuntimeStatus_ConnectedRequiresOnlineAndConn conn 为 nil 或状态非 online 时 Connected=false。
func TestRuntimeStatus_ConnectedRequiresOnlineAndConn(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	// 初始：starting，无 conn，Connected=false。
	s := acc.RuntimeStatus()
	if s.Connected || s.State != RuntimeStarting {
		t.Fatalf("初始状态异常: %+v", s)
	}
	if s.Failures != 0 || s.Message == "" {
		t.Fatalf("Failures/Message 异常: %+v", s)
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt 应已设置")
	}

	// 设为 online 但无 conn：仍不算 Connected。
	acc.setRuntimeState(RuntimeOnline, "在线")
	if acc.RuntimeStatus().Connected {
		t.Error("无 conn 时 Connected 应为 false")
	}

	// 设为 reconnecting：Connected=false。
	acc.setRuntimeState(RuntimeReconnecting, "重连中")
	if acc.RuntimeStatus().Connected {
		t.Error("reconnecting 状态 Connected 应为 false")
	}
}

// alertCountingHandler 包装 recordingHandler 并原子统计告警次数。
type alertCountingHandler struct {
	recordingHandler
	alerts int32
}

// OnAccountAlert 负责On账号Alert相关处理。
func (a *alertCountingHandler) OnAccountAlert(_ context.Context, _, level, _, _ string) {
	if level == AlertLevelWarn {
		atomic.AddInt32(&a.alerts, 1)
	}
}

// stubCookieRenewer 保存stub登录凭证Renewer，供当前处理流程使用
type stubCookieRenewer struct {
	result *xrenew.Result
	err    error
	calls  int
	got    string
}

// RenewAPIFirst 负责RenewAPIFirst相关处理。
func (s *stubCookieRenewer) RenewAPIFirst(_ context.Context, cookiesStr string, _ ...[]cookierefresh.BrowserCookie) (*xrenew.Result, error) {
	s.calls++
	s.got = cookiesStr
	return s.result, s.err
}

// TestTryAPIRenewSuccessShortCircuitsRecovery 负责TestTryAPIRenewSuccessShortCircuitsRecovery相关处理。
func TestTryAPIRenewSuccessShortCircuitsRecovery(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Save(ctx, "cid", "did-old", "tok-old", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("save token: %v", err)
	}
	// renewer 保存renewer，供当前处理流程使用
	renewer := &stubCookieRenewer{result: &xrenew.Result{
		Success:            true,
		RenewMethod:        "api",
		NewCookies:         "unb=123; _m_h5_tk=tk_2;",
		UpdatedCookieNames: []string{"_m_h5_tk"},
	}}
	acc.renewer = renewer

	if !acc.tryAPIRenew(ctx) {
		t.Fatal("接口续期成功应短路后续恢复")
	}
	if renewer.calls != 1 || renewer.got != "unb=123; _m_h5_tk=tk_1;" {
		t.Fatalf("renewer 调用异常: calls=%d got=%q", renewer.calls, renewer.got)
	}
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=tk_2;" {
		t.Fatalf("内存 cookie 未更新: %q", got)
	}
	// saved 保存saved，供当前处理流程使用
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved != "unb=123; _m_h5_tk=tk_2;" {
		t.Fatalf("DB cookie 未更新: %q", saved)
	}
	if // tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(ctx, "cid"); err != nil || tk.AccessToken != "" {
		t.Fatalf("接口续期后应清 token；数据库中的旧 device ID 不再参与运行时身份: tk=%+v err=%v", tk, err)
	}
}

// TestTryAPIRenewPartialCookiesContinueRecovery 负责TestTryAPIRenewPartialCookiesContinueRecovery相关处理。
func TestTryAPIRenewPartialCookiesContinueRecovery(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Save(ctx, "cid", "did-old", "tok-old", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("save token: %v", err)
	}
	// renewer 保存renewer，供当前处理流程使用
	renewer := &stubCookieRenewer{result: &xrenew.Result{
		Success:            false,
		RenewMethod:        "none",
		NewCookies:         "unb=123; _m_h5_tk=partial;",
		UpdatedCookieNames: []string{"_m_h5_tk"},
		SetCookies:         []string{"_m_h5_tk=partial; Domain=.goofish.com; Path=/; Secure; HttpOnly"},
		Message:            "setLoginSettings 未返回 Set-Cookie",
	}}
	acc.renewer = renewer

	if acc.tryAPIRenew(ctx) {
		t.Fatal("仅有部分 Cookie 更新时不应短路后续浏览器/密码恢复")
	}
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=partial;" {
		t.Fatalf("部分 cookie 应先保存到内存: %q", got)
	}
	// saved 保存saved，供当前处理流程使用
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved != "unb=123; _m_h5_tk=partial;" {
		t.Fatalf("部分 cookie 应先保存到 DB: %q", saved)
	}
	if // tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(ctx, "cid"); err != nil || tk.AccessToken != "" {
		t.Fatalf("部分 cookie 更新后应清 token: tk=%+v err=%v", tk, err)
	}
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	if // complete 保存complete，供当前处理流程使用
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("接口扁平 Cookie 更新不得伪造成完整浏览器 Jar")
	}
}

// TestTryAPIRenewPersistsExplicitFlatCookieDeletionOnError 负责TestTryAPIRenewPersistsExplicitFlat登录凭证DeletionOn错误相关处理。
func TestTryAPIRenewPersistsExplicitFlatCookieDeletionOnError(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Save(ctx, "cid", "did-old", "tok-old", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("save token: %v", err)
	}
	acc.renewer = &stubCookieRenewer{
		result: &xrenew.Result{
			NewCookies: "",
			SetCookies: []string{
				"unb=; Domain=.goofish.com; Path=/; Max-Age=0",
				"_m_h5_tk=; Domain=.goofish.com; Path=/; Max-Age=0",
			},
		},
		err: errors.New("续期响应正文损坏"),
	}

	if acc.tryAPIRenew(ctx) {
		t.Fatal("失败响应即使带 Cookie 删除也不应视为续期成功")
	}
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != "" {
		t.Fatalf("运行时应采用服务端明确删除后的空 Cookie，got %q", got)
	}
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "" {
		t.Fatalf("数据库应保存明确删除后的空 Cookie，got %q", detail.Value)
	}
	if // complete 保存complete，供当前处理流程使用
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("扁平删除结果不得伪造成完整浏览器 Jar")
	}
	if // token、err 保存token、err，供当前处理流程使用
	token, err := store.Tokens.Get(ctx, "cid"); err != nil || token.AccessToken != "" {
		t.Fatalf("Cookie 删除后应清理旧 token: token=%+v err=%v", token, err)
	}
}

// TestTryAPIRenewPersistsLatePromiseCookieWithoutRestart 负责TestTryAPIRenewPersistsLatePromise登录凭证WithoutRestart相关处理。
func TestTryAPIRenewPersistsLatePromiseCookieWithoutRestart(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// oldFingerprint 保存oldFingerprint，供当前处理流程使用
	oldFingerprint := xianyu.CurrentBrowserFingerprint()
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: "Mozilla/5.0 (Macintosh) Chrome/999.0.0.0 Safari/537.36"})
	t.Cleanup(func() { xianyu.SetBrowserFingerprint(oldFingerprint) })
	// initial 保存initial，供当前处理流程使用
	initial := "unb=123; havana_lgc_exp=" + fmt.Sprint(time.Now().Add(time.Hour).UnixMilli())
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateValueExisting(ctx, "cid", initial); err != nil {
		t.Fatal(err)
	}
	acc.replaceCookieStr(initial)
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(60 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: fmt.Sprint(time.Now().Add(time.Hour).UnixMilli()), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	acc.renewer = xrenew.Service{
		HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1,
		PromiseTimeout: 10 * time.Millisecond,
	}
	if acc.tryAPIRenew(ctx) {
		t.Fatal("Promise 超时不得伪装成同步续期成功")
	}
	// deadline 保存deadline，供当前处理流程使用
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		// detail、err 保存detail、err，供当前处理流程使用
		detail, err := store.Cookies.GetDetails(ctx, "cid")
		if err == nil && detail != nil && strings.Contains(detail.Value, "sdkSilent=") {
			if !strings.Contains(acc.currentCookieStr(), "sdkSilent=") {
				t.Fatalf("迟到 Cookie 已入库但未更新运行时: %q", acc.currentCookieStr())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("迟到的 silentHasLogin Set-Cookie 未写回账号")
}

// TestTryAPIRenewPendingWatcherStopsWithAccountContext 负责TestTryAPIRenewPendingWatcherStopsWith账号上下文相关处理。
func TestTryAPIRenewPendingWatcherStopsWithAccountContext(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// initial 保存initial，供当前处理流程使用
	initial := "unb=123; havana_lgc_exp=" + fmt.Sprint(time.Now().Add(time.Hour).UnixMilli())
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateValueExisting(context.Background(), "cid", initial); err != nil {
		t.Fatal(err)
	}
	acc.replaceCookieStr(initial)
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: fmt.Sprint(time.Now().Add(time.Hour).UnixMilli()), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	acc.renewer = xrenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond}
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	acc.lifecycle.start(ctx, cancel)
	if acc.tryAPIRenew(ctx) {
		t.Fatal("Promise 超时不得伪装成同步续期成功")
	}
	cancel()
	// stopCtx 验证账号停止会等待迟到续期 worker，而不是只取消其父上下文。
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	// stopErr 表示停止账号并等待迟到续期 worker 的结果。
	if stopErr := acc.StopContext(stopCtx); stopErr != nil {
		t.Fatalf("StopContext 未等待迟到续期 worker: %v", stopErr)
	}
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(context.Background(), "cid")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail.Value, "sdkSilent=") || strings.Contains(acc.currentCookieStr(), "sdkSilent=") {
		t.Fatalf("账号关闭后不应接纳迟到 Cookie: db=%q runtime=%q", detail.Value, acc.currentCookieStr())
	}
}

// TestSetRuntimeError_AllBranches 覆盖验证/captcha、token 失效、默认重连三分支
// 及告警去重逻辑（仅从非验证态进入验证态时告警一次）。
// TestSetRuntimeError_AllBranches 负责TestSetRuntime错误AllBranches相关处理。
func TestSetRuntimeError_AllBranches(t *testing.T) {
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// 1) captcha/risk 关键词 → VerificationRequired，且从非验证态进入时告警一次。
	t.Run("captcha", func(t *testing.T) {
		// acc、cleanup 保存acc、cleanup，供当前处理流程使用
		acc, _, _, cleanup := newAccountForTest(t)
		defer cleanup()
		// h 保存h，供当前处理流程使用
		h := &alertCountingHandler{}
		acc.handler = h
		acc.setRuntimeError(ctx, fmt.Errorf("FAIL_SYS_USER_VALIDATE: captcha required"))
		if // s 保存s，供当前处理流程使用
		s := acc.RuntimeStatus(); s.State != RuntimeVerificationRequired {
			t.Fatalf("state=%q", s.State)
		}
		if // got 保存got，供当前处理流程使用
		got := atomic.LoadInt32(&h.alerts); got != 1 {
			t.Errorf("首次进入验证态应告警一次，got %d want 1", got)
		}
		// 再次以验证错误进入：不应重复告警（prev 已是 verification）。
		acc.setRuntimeError(ctx, fmt.Errorf("rgv587 风控"))
		if // got 保存got，供当前处理流程使用
		got := atomic.LoadInt32(&h.alerts); got != 1 {
			t.Errorf("验证态重复进入不应重复告警，got %d want 1", got)
		}
	})

	// 2) token expired 关键词 → AuthExpired（不告警）。
	t.Run("token_expired", func(t *testing.T) {
		// acc、cleanup 保存acc、cleanup，供当前处理流程使用
		acc, _, _, cleanup := newAccountForTest(t)
		defer cleanup()
		// h 保存h，供当前处理流程使用
		h := &alertCountingHandler{}
		acc.handler = h
		// msg 表示当前遍历过程中的msg
		for _, msg := range []string{
			"登录凭证已失效",
			"FAIL_SYS_TOKEN_EXOIRED",
			"FAIL_SYS_TOKEN_EXPIRED",
			"cookie 缺少 unb",
		} {
			acc.setRuntimeError(ctx, errors.New(msg))
			if // s 保存s，供当前处理流程使用
			s := acc.RuntimeStatus(); s.State != RuntimeAuthExpired {
				t.Fatalf("msg=%q state=%q want %q", msg, s.State, RuntimeAuthExpired)
			}
		}
		if // got 保存got，供当前处理流程使用
		got := atomic.LoadInt32(&h.alerts); got != 0 {
			t.Errorf("token 失效分支不应触发 warn 告警，got %d", got)
		}
	})

	// 3) 其他错误 → Reconnecting（不告警）。
	t.Run("other", func(t *testing.T) {
		// acc、cleanup 保存acc、cleanup，供当前处理流程使用
		acc, _, _, cleanup := newAccountForTest(t)
		defer cleanup()
		// h 保存h，供当前处理流程使用
		h := &alertCountingHandler{}
		acc.handler = h
		acc.setRuntimeError(ctx, errors.New("connection refused"))
		if // s 保存s，供当前处理流程使用
		s := acc.RuntimeStatus(); s.State != RuntimeReconnecting {
			t.Fatalf("state=%q want %q", s.State, RuntimeReconnecting)
		}
		if // got 保存got，供当前处理流程使用
		got := atomic.LoadInt32(&h.alerts); got != 0 {
			t.Errorf("默认分支不应告警，got %d", got)
		}
	})
}

// TestAlert_NilHandler handler 为 nil 时静默跳过，不 panic。
func TestAlert_NilHandler(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.handler = nil
	// 不应 panic。
	acc.alert(context.Background(), AlertLevelCritical, "title", "body")
}

// TestResetFailures 重置失败计数。
func TestResetFailures(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.connFailures = 7
	acc.resetFailures()
	if acc.connFailures != 0 {
		t.Errorf("resetFailures 后 connFailures=%d want 0", acc.connFailures)
	}
}

// TestRefreshTokenSuccessResetsTokenFetchFailures 负责TestRefresh令牌SuccessResets令牌FetchFailures相关处理。
func TestRefreshTokenSuccessResetsTokenFetchFailures(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.mtop = &fakeRunMtop{token: "tok-reset"}
	acc.tokenFetchFailures = 19

	// token、err 保存token、err，供当前处理流程使用
	token, _, err := acc.refreshToken(context.Background())
	if err != nil {
		t.Fatalf("refreshToken: %v", err)
	}
	if token != "tok-reset" {
		t.Fatalf("token=%q want tok-reset", token)
	}
	if acc.tokenFetchFailures != 0 {
		t.Fatalf("tokenFetchFailures=%d want 0", acc.tokenFetchFailures)
	}
}

// TestRetryDelay_FailureClampsAtOne connFailures=0 时按 1 计算。
func TestRetryDelay_FailureClampsAtOne(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.connFailures = 0
	// close-frame：min(2^1,30)=2s。
	expectDelayRange(t, acc.retryDelay("no close frame received or sent"), 2*time.Second)
	// timeout：min(2*2^1,90)=4s。
	expectDelayRange(t, acc.retryDelay("timeout reading"), 4*time.Second)
	// default：min(2^1,45)=2s。
	expectDelayRange(t, acc.retryDelay("random error"), 2*time.Second)
}

// TestRetryDelay_TimeoutVariant "timeout" 关键词分支。
func TestRetryDelay_TimeoutVariant(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.connFailures = 3
	// min(2*2^3,90)=16s。
	expectDelayRange(t, acc.retryDelay("dial timeout"), 16*time.Second)
	acc.connFailures = 10
	expectDelayRange(t, acc.retryDelay("timeout"), 90*time.Second)
}

// TestNetworkRetryDelayMatchesReferenceBackoff 负责TestNetwork重试延迟MatchesReferenceBackoff相关处理。
func TestNetworkRetryDelayMatchesReferenceBackoff(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.networkFailures = 1
	expectDelayRange(t, acc.networkRetryDelay(), 4*time.Second)
	acc.networkFailures = 10
	expectDelayRange(t, acc.networkRetryDelay(), 60*time.Second)
}

// TestEstablishedNetworkErrorClassification 负责TestEstablishedNetwork错误Classification相关处理。
func TestEstablishedNetworkErrorClassification(t *testing.T) {
	// err 表示当前遍历过程中的err
	for _, err := range []error{
		errors.New("ConnectionClosedError"), errors.New("no close frame received or sent"),
		errors.New("WS read: connection reset by peer"), errors.New("received close frame"),
	} {
		if !isEstablishedNetworkError(err) {
			t.Fatalf("应识别为已建立连接后的网络错误: %v", err)
		}
	}
	if isEstablishedNetworkError(errors.New("device id or appkey is not equal")) {
		t.Fatal("注册认证错误不应归类为纯网络断线")
	}
}

// TestRecordShortDisconnectDisablesAtFiveWithinWindow 负责TestRecordShortDisconnectDisablesAtFiveWithinWindow相关处理。
func TestRecordShortDisconnectDisablesAtFiveWithinWindow(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	for // i 保存i，供当前处理流程使用
	i := 1; i < FrequentDisconnectLimit; i++ {
		if acc.recordShortDisconnect(time.Second) {
			t.Fatalf("第 %d 次短连接不应达到阈值", i)
		}
	}
	if !acc.recordShortDisconnect(time.Second) {
		t.Fatal("5 分钟内第 5 次短连接应达到禁用阈值")
	}
	if acc.recordShortDisconnect(ShortConnectionThreshold) {
		t.Fatal("长连接应清空短连接记录")
	}
	if len(acc.shortDisconnects) != 0 {
		t.Fatalf("长连接后短连接记录未清空: %d", len(acc.shortDisconnects))
	}
}

// TestHandleMaxFailures_RecentMessageStillRunsRecovery 消息冷却只约束 Token/Cookie
// 刷新，不约束达到认证失败阈值后的恢复链。
// TestHandleMaxFailures_RecentMessageStillRunsRecovery 负责TestHandleMaxFailuresRecent消息Still运行记录Recovery相关处理。
func TestHandleMaxFailures_RecentMessageStillRunsRecovery(t *testing.T) {
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// 设最近收到消息（在 MessageCooldown 内）。
	acc.mu.Lock()
	acc.lastMsgReceived = time.Now()
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	// cctx、cancel 保存cctx、cancel，供当前处理流程使用
	cctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	// err 保存err，供当前处理流程使用
	err := acc.handleMaxFailures(cctx)
	if err != cctx.Err() {
		t.Fatalf("成功恢复后的等待应响应 ctx 取消，got %v want %v", err, cctx.Err())
	}
	if h.refresh != 1 {
		t.Errorf("应触发一次恢复链，got %d", h.refresh)
	}
	if acc.connFailures != 0 {
		t.Errorf("应重置失败计数，got %d", acc.connFailures)
	}
}

// TestHandleMaxFailures_PasswordLoginSuccess 密码登录刷新成功：重置失败计数、状态置 connecting、cookie 更新。
func TestHandleMaxFailures_PasswordLoginSuccess(t *testing.T) {
	// acc、h、store、cleanup 保存acc、h、store、cleanup，供当前处理流程使用
	acc, h, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// 无最近消息、未在密码登录冷却中。
	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	// handler.OnPasswordLoginRefresh 返回 true → 成功路径。
	// store.Cookies.GetDetails 返回新 cookie，应触发 replaceCookieStr。
	// newCookie 保存new登录凭证，供当前处理流程使用
	newCookie := "unb=999; _m_h5_tk=tk_new;"
	// admin、err 保存admin、err，供当前处理流程使用
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "cid", newCookie, admin.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// 确认 GetDetails 能读到新值（排除 Save 静默失败）。
	d, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil || d.Value != newCookie {
		t.Fatalf("GetDetails 未返回新 cookie: d=%+v err=%v", d, err)
	}

	// 用一个延迟取消的 ctx：GetDetails 能成功完成，sleepCtx(2s) 会在取消时返回 ctx.Err()。
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err = acc.handleMaxFailures(cctx)
	// 成功刷新后进入 sleepCtx(2s)，超时 ctx 让其返回 ctx.Err()。
	if err != cctx.Err() {
		t.Fatalf("成功路径 sleepCtx 应返回 ctx.Err()，got %v want %v", err, cctx.Err())
	}
	if h.refresh != 1 {
		t.Errorf("应调用一次 OnPasswordLoginRefresh，got %d", h.refresh)
	}
	if acc.connFailures != 0 {
		t.Errorf("应重置失败计数，got %d", acc.connFailures)
	}
	if // s 保存s，供当前处理流程使用
	s := acc.RuntimeStatus(); s.State != RuntimeConnecting {
		t.Errorf("状态应为 connecting，got %q", s.State)
	}
	// replaceCookieStr 应已更新 cookie。
	if got := acc.currentCookieStr(); got != newCookie {
		t.Errorf("cookie 未更新: got %q want %q", got, newCookie)
	}
}

// failingRefreshHandler OnPasswordLoginRefresh 返回 false 的 handler，记录告警。
type failingRefreshHandler struct {
	alerts []string
	events []string
}

// lockAwareRefreshHandler 在外部恢复回调内重新获取账号凭证锁，用于验证锁边界。
type lockAwareRefreshHandler struct {
	// store 提供测试账号凭证锁。
	store *db.Store
	// acquired 表示回调是否成功获取并释放凭证锁。
	acquired bool
}

// HandleChatMessage 满足 Engine Handler 接口的聊天处理方法。
func (h *lockAwareRefreshHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }

// HandleSystemEvent 满足 Engine Handler 接口的系统事件处理方法。
func (h *lockAwareRefreshHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}

// OnPasswordLoginRefresh 在恢复回调中获取凭证锁，证明调用方未跨外部 I/O 持锁。
func (h *lockAwareRefreshHandler) OnPasswordLoginRefresh(context.Context, string) bool {
	// unlock 释放恢复回调取得的账号凭证锁。
	unlock := h.store.LockAccountCredentials("cid")
	unlock()
	h.acquired = true
	return false
}

// OnAccountAlert 满足 Engine Handler 接口的告警处理方法。
func (h *lockAwareRefreshHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// HandleChatMessage 处理聊天消息。
func (f *failingRefreshHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }

// HandleSystemEvent 处理系统Event。
func (f *failingRefreshHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}

// OnPasswordLoginRefresh 负责On密码登录Refresh相关处理。
func (f *failingRefreshHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 负责On账号Alert相关处理。
func (f *failingRefreshHandler) OnAccountAlert(_ context.Context, _, level, _, _ string) {
	f.alerts = append(f.alerts, level)
}

// OnAccountEvent 负责On账号Event相关处理。
func (f *failingRefreshHandler) OnAccountEvent(_ context.Context, _, eventType, level, _, _ string) {
	f.events = append(f.events, eventType)
	f.alerts = append(f.alerts, level)
}

// TestHandleMaxFailuresReleasesCredentialLockBeforeExternalRecovery 验证外部凭证恢复回调执行时账号锁已释放。
func TestHandleMaxFailuresReleasesCredentialLockBeforeExternalRecovery(t *testing.T) {
	// acc、store、cleanup 保存账号运行时、凭证存储及清理函数。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// handler 保存会尝试重新获取账号锁的恢复回调。
	handler := &lockAwareRefreshHandler{store: store}
	acc.handler = handler
	// ctx 保存本测试共用的上下文。
	ctx := context.Background()
	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()
	// err 保存连续失败恢复流程返回的错误。
	if err := acc.handleMaxFailures(ctx); err == nil || !strings.Contains(err.Error(), "自动恢复失败") {
		t.Fatalf("恢复失败应返回终止错误: %v", err)
	}
	if !handler.acquired {
		t.Fatal("外部恢复回调未成功获取账号凭证锁")
	}
}

// TestHandleMaxFailures_PasswordLoginFailure 密码登录刷新失败后终止账号主循环。
func TestHandleMaxFailures_PasswordLoginFailure(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	// handler 返回 false → 刷新失败。
	h := &failingRefreshHandler{}
	acc.handler = h

	// err 保存err，供当前处理流程使用
	err := acc.handleMaxFailures(context.Background())
	if err == nil || !strings.Contains(err.Error(), "自动恢复失败") {
		t.Fatalf("刷新失败应返回终止主循环的错误，got %v", err)
	}
	if // s 保存s，供当前处理流程使用
	s := acc.RuntimeStatus(); s.State != RuntimeAuthExpired {
		t.Errorf("状态应为 auth_expired，got %q", s.State)
	}
	if len(h.alerts) != 2 || h.alerts[0] != AlertLevelWarn || h.alerts[1] != AlertLevelCritical {
		t.Errorf("应先触发掉线 warn，再触发恢复失败 critical，got %+v", h.alerts)
	}
	if len(h.events) != 2 || h.events[0] != EventAccountOffline || h.events[1] != EventAccountOffline {
		t.Errorf("事件类型应为账号掉线通知，got %+v", h.events)
	}
}

// TestReplaceCookieStr_UpdateUserIDAndDeviceID 更新 cookie 不得改变永久 deviceID。
func TestReplaceCookieStr_UpdateUserIDAndDeviceID(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	// 初始：UserID=123，deviceID 已由 New 生成。
	acc.mu.Lock()
	// oldDevice 保存oldDevice，供当前处理流程使用
	oldDevice := acc.deviceID
	// oldUser 保存old用户，供当前处理流程使用
	oldUser := acc.UserID
	acc.mu.Unlock()
	if oldUser != "123" {
		t.Fatalf("初始 UserID=%q want 123", oldUser)
	}
	if oldDevice == "" {
		t.Fatal("初始 deviceID 不应为空")
	}

	// 更新为新 unb。
	acc.replaceCookieStr("unb=456; _m_h5_tk=tk2;")
	acc.mu.Lock()
	// newDevice 保存newDevice，供当前处理流程使用
	newDevice := acc.deviceID
	// newUser 保存new用户，供当前处理流程使用
	newUser := acc.UserID
	// newCookie 保存new登录凭证，供当前处理流程使用
	newCookie := acc.CookieStr
	acc.mu.Unlock()
	if newUser != "456" {
		t.Errorf("UserID 未更新: got %q want 456", newUser)
	}
	if newDevice == "" {
		t.Error("deviceID 不应为空")
	}
	if newDevice != oldDevice {
		t.Error("unb 变化后 deviceID 仍应保持不变")
	}
	if newCookie != "unb=456; _m_h5_tk=tk2;" {
		t.Errorf("CookieStr 未更新: got %q", newCookie)
	}
}

// TestReplaceCookieStrDoesNotGenerateDeviceID ensures cookie mutation cannot
// silently replace the identity established during account construction.
// TestReplaceCookieStrDoesNotGenerateDeviceID 负责TestReplace登录凭证StrDoesNotGenerateDeviceID相关处理。
func TestReplaceCookieStrDoesNotGenerateDeviceID(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	// 清空 deviceID 与 UserID，模拟异常状态。
	acc.mu.Lock()
	acc.deviceID = ""
	acc.UserID = ""
	acc.mu.Unlock()

	acc.replaceCookieStr("unb=789; _m_h5_tk=tk3;")
	acc.mu.Lock()
	// d 保存d，供当前处理流程使用
	d := acc.deviceID
	// u 保存u，供当前处理流程使用
	u := acc.UserID
	acc.mu.Unlock()
	if u != "789" {
		t.Errorf("UserID=%q want 789", u)
	}
	if d != "" {
		t.Error("Cookie 更新不应隐式生成 deviceID")
	}
}

// TestReplaceCookieStr_NoUnbNoChange cookie 无 unb 时只更新 CookieStr。
func TestReplaceCookieStr_NoUnbNoChange(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.mu.Lock()
	// oldDevice 保存oldDevice，供当前处理流程使用
	oldDevice := acc.deviceID
	// oldUser 保存old用户，供当前处理流程使用
	oldUser := acc.UserID
	acc.mu.Unlock()

	acc.replaceCookieStr("foo=bar; baz=qux;")
	acc.mu.Lock()
	// d 保存d，供当前处理流程使用
	d := acc.deviceID
	// u 保存u，供当前处理流程使用
	u := acc.UserID
	// c 保存c，供当前处理流程使用
	c := acc.CookieStr
	acc.mu.Unlock()
	if u != oldUser {
		t.Errorf("无 unb 时 UserID 不应变: got %q want %q", u, oldUser)
	}
	if d != oldDevice {
		t.Errorf("无 unb 时 deviceID 不应变: got %q want %q", d, oldDevice)
	}
	if c != "foo=bar; baz=qux;" {
		t.Errorf("CookieStr 未更新: got %q", c)
	}
}

// TestUpdateCookie_IgnoresEmpty 纯空白/空字符串被忽略，不覆盖现有 cookie。
func TestUpdateCookie_IgnoresEmpty(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// orig 保存orig，供当前处理流程使用
	orig := acc.currentCookieStr()

	// 空字符串与纯空白（TrimSpace 后为空）应被忽略。
	acc.UpdateCookie("")
	acc.UpdateCookie("   ")
	acc.UpdateCookie("\t\n")
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != orig {
		t.Errorf("空白 cookie 不应更新: got %q want %q", got, orig)
	}

	acc.mu.Lock()
	// originalDevice 保存originalDevice，供当前处理流程使用
	originalDevice := acc.deviceID
	// healthyConn 保存healthyConn，供当前处理流程使用
	healthyConn := &fakeWSConn{}
	acc.conn = healthyConn
	acc.mu.Unlock()

	// 非空（即使含首尾空白）应原样存储，但不能模拟页面 reload 去打断健康 WS。
	updated := "unb=123; _m_h5_tk=tk_new;"
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateValueExisting(context.Background(), acc.CookieID, updated); err != nil {
		t.Fatal(err)
	}
	acc.UpdateCookie(updated)
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != updated {
		t.Errorf("非空 cookie 应更新: got %q", got)
	}
	acc.mu.Lock()
	// currentDevice 保存currentDevice，供当前处理流程使用
	currentDevice := acc.deviceID
	acc.mu.Unlock()
	healthyConn.mu.Lock()
	// closed 保存closed，供当前处理流程使用
	closed := healthyConn.closed
	healthyConn.mu.Unlock()
	if currentDevice != originalDevice || closed {
		t.Fatalf("普通 Cookie 更新不应轮换 device ID 或关闭健康连接: device=%q/%q closed=%v", currentDevice, originalDevice, closed)
	}
}

// TestUpdateCookie_AcceptsAuthoritativeEmptySnapshot 负责TestUpdate登录凭证AcceptsAuthoritativeEmptySnapshot相关处理。
func TestUpdateCookie_AcceptsAuthoritativeEmptySnapshot(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// metadata 保存metadata，供当前处理流程使用
	metadata := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{})
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateRenewalCookie(context.Background(), acc.CookieID, "", metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	acc.UpdateCookie("")
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != "" {
		t.Fatalf("权威空 Jar 应清空运行时 Cookie，got %q", got)
	}
}

// TestReloadCookieFromDBDetectsMetadataOnlyCredentialRotation 负责TestReload登录凭证FromDBDetectsMetadataOnlyCredentialRotation相关处理。
func TestReloadCookieFromDBDetectsMetadataOnlyCredentialRotation(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// flat 保存flat，供当前处理流程使用
	flat := acc.currentCookieStr()
	// metadataA 保存metadataA，供当前处理流程使用
	metadataA := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-a", Domain: ".goofish.com", Path: "/im"},
	})
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataA, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if !acc.reloadCookieFromDB(ctx) {
		t.Fatal("首次权威 Jar 应同步到运行时")
	}
	acc.mu.Lock()
	// boundFP 保存boundFP，供当前处理流程使用
	boundFP := acc.credentialFP
	acc.currentToken = "old-token"
	acc.tokenCredentialFP = boundFP
	acc.mu.Unlock()
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.SaveBound(ctx, acc.CookieID, acc.deviceID, "old-token", time.Now().Add(time.Hour).Unix(), boundFP); err != nil {
		t.Fatal(err)
	}

	// metadataB 保存metadataB，供当前处理流程使用
	metadataB := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-b", Domain: ".goofish.com", Path: "/im"},
	})
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataB, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if !acc.reloadCookieFromDB(ctx) {
		t.Fatal("扁平 Cookie 未变但权威 Jar 已变化时必须重新加载")
	}
	acc.mu.Lock()
	// currentToken 保存current令牌，供当前处理流程使用
	currentToken := acc.currentToken
	// currentFP 保存currentFP，供当前处理流程使用
	currentFP := acc.credentialFP
	acc.mu.Unlock()
	if currentToken != "" {
		t.Fatalf("Jar 变化后应清内存 token，got %q", currentToken)
	}
	if currentFP != credentialStateFingerprint(flat, metadataB) {
		t.Fatal("运行时未绑定到最新完整凭证状态")
	}
	if // cached、err 保存cached、err，供当前处理流程使用
	cached, err := store.Tokens.Get(ctx, acc.CookieID); err != nil || cached.AccessToken != "" {
		t.Fatalf("Jar 变化后应清数据库 token: cached=%+v err=%v", cached, err)
	}
}

// TestCookieSnapshotMatchesDBUsesCompleteCredentialState 负责Test登录凭证SnapshotMatchesDBUsesCompleteCredential状态相关处理。
func TestCookieSnapshotMatchesDBUsesCompleteCredentialState(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// flat 保存flat，供当前处理流程使用
	flat := acc.currentCookieStr()
	// metadataA 保存metadataA，供当前处理流程使用
	metadataA := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "sgcookie", Value: "a", Domain: ".goofish.com", Path: "/"},
	})
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataA, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	// expected 保存expected，供当前处理流程使用
	expected := credentialStateFingerprint(flat, metadataA)
	if !acc.cookieSnapshotMatchesDB(ctx, expected) {
		t.Fatal("相同扁平 Cookie 与权威 Jar 应允许 /reg")
	}
	// metadataB 保存metadataB，供当前处理流程使用
	metadataB := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "sgcookie", Value: "b", Domain: ".goofish.com", Path: "/"},
	})
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataB, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if acc.cookieSnapshotMatchesDB(ctx, expected) {
		t.Fatal("token 获取后 Jar 变化时必须拒绝 /reg")
	}
	// emptyMetadata 保存emptyMetadata，供当前处理流程使用
	emptyMetadata := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{})
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, "", emptyMetadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if !acc.cookieSnapshotMatchesDB(ctx, credentialStateFingerprint("", emptyMetadata)) {
		t.Fatal("完整空 Jar 是权威状态，不应因扁平值为空而被拒绝")
	}
}

// TestAdoptIncompleteTokenCookiesDoesNotInventCompleteSnapshot 负责TestAdoptIncomplete令牌CookiesDoesNotInventCompleteSnapshot相关处理。
func TestAdoptIncompleteTokenCookiesDoesNotInventCompleteSnapshot(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// updated 保存updated，供当前处理流程使用
	updated := "unb=123; _m_h5_tk=flat-only; cookie2=next"
	// got、err 保存got、err，供当前处理流程使用
	got, err := acc.adoptTokenResponseCookies(ctx, acc.currentCookieStr(), &mtop.RefreshResult{
		UpdatedCookies: updated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != updated {
		t.Fatalf("adopted Cookie=%q want %q", got, updated)
	}
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(ctx, acc.CookieID)
	if err != nil {
		t.Fatal(err)
	}
	if // complete 保存complete，供当前处理流程使用
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("仅有扁平 token 响应时不得伪造成完整浏览器 Jar")
	}
}

// TestAdoptTokenResponseCookiesPersistsExplicitDeletionToEmpty 负责TestAdopt令牌响应CookiesPersistsExplicitDeletionToEmpty相关处理。
func TestAdoptTokenResponseCookiesPersistsExplicitDeletionToEmpty(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// got、err 保存got、err，供当前处理流程使用
	got, err := acc.adoptTokenResponseCookies(ctx, acc.currentCookieStr(), &mtop.RefreshResult{
		UpdatedCookies:     "",
		CookieStateChanged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" || acc.currentCookieStr() != "" {
		t.Fatalf("明确删除后的 Cookie 未同步: got=%q runtime=%q", got, acc.currentCookieStr())
	}
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(ctx, acc.CookieID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "" {
		t.Fatalf("数据库 Cookie=%q want empty", detail.Value)
	}
	if // complete 保存complete，供当前处理流程使用
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("扁平 token 删除不得伪造成完整浏览器 Jar")
	}
}

// TestCurrentCookieStr 线程安全返回当前 CookieStr。
func TestCurrentCookieStr(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=tk_1;" {
		t.Errorf("currentCookieStr=%q", got)
	}
	// updated 保存updated，供当前处理流程使用
	updated := "unb=1; x=2;"
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateValueExisting(context.Background(), acc.CookieID, updated); err != nil {
		t.Fatal(err)
	}
	acc.UpdateCookie(updated)
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != updated {
		t.Errorf("更新后 currentCookieStr=%q", got)
	}
}

// TestSleepCtx 正常睡眠返回 nil；ctx 取消返回 ctx.Err()；d<=0 立即返回。
func TestSleepCtx(t *testing.T) {
	// d<=0 立即返回 nil。
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Errorf("d=0 应返回 nil，got %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := sleepCtx(context.Background(), -time.Second); err != nil {
		t.Errorf("d<0 应返回 nil，got %v", err)
	}

	// 正常短睡眠。
	start := time.Now()
	if // err 保存err，供当前处理流程使用
	err := sleepCtx(context.Background(), 50*time.Millisecond); err != nil {
		t.Errorf("正常睡眠应返回 nil，got %v", err)
	}
	if // elapsed 保存elapsed，供当前处理流程使用
	elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("睡眠时间过短: %v", elapsed)
	}

	// ctx 取消：应立即返回 ctx.Err()。
	cctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start = time.Now()
	// err 保存err，供当前处理流程使用
	err := sleepCtx(cctx, 5*time.Second)
	if err != cctx.Err() {
		t.Errorf("sleepCtx 取消应返回 ctx.Err(): got %v want %v", err, cctx.Err())
	}
	if // elapsed 保存elapsed，供当前处理流程使用
	elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("ctx 取消应立即返回，耗时 %v", elapsed)
	}
}

// TestErrString errString 处理 nil 与非 nil。
func TestErrString(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := errString(nil); got != "" {
		t.Errorf("errString(nil)=%q want empty", got)
	}
	// e 保存e，供当前处理流程使用
	e := errors.New("boom")
	if // got 保存got，供当前处理流程使用
	got := errString(e); got != "boom" {
		t.Errorf("errString=%q want boom", got)
	}
}

// TestTruncID 长串截断、短串原样。
func TestTruncID(t *testing.T) {
	// short 保存short，供当前处理流程使用
	short := "abc123"
	if // got 保存got，供当前处理流程使用
	got := truncID(short); got != short {
		t.Errorf("短串应原样返回: got %q", got)
	}
	// long 保存long，供当前处理流程使用
	long := ""
	for // i 保存i，供当前处理流程使用
	i := 0; i < 80; i++ {
		long += "x"
	}
	// got 保存got，供当前处理流程使用
	got := truncID(long)
	if len(got) != 53 || got[50:] != "..." {
		t.Errorf("长串应截断到 53 字符并加 ...: got %q (len=%d)", got, len(got))
	}
}

// TestTryAPIRenewUsingExcludesLoginSecrets 验证接口续期读取窄模型时不解密登录密码。
func TestTryAPIRenewUsingExcludesLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-api-renew-query-key")
	// acc 是使用接口续期窄查询路径的测试账号；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试接口续期共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-api-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// callbackCalled 表示接口续期回调是否收到窄查询得到的 Cookie。
	callbackCalled := false
	// result 是不产生 Cookie 更新、仅表示接口续期成功的模拟响应。
	result := &xrenew.Result{Success: true, RenewMethod: "api"}
	// renewed 和 renewErr 是接口续期结果及其错误。
	renewed, renewErr := acc.tryAPIRenewUsing(ctx, func(_ context.Context, cookieStr string, _ []cookierefresh.BrowserCookie) (*xrenew.Result, error) {
		callbackCalled = true
		if cookieStr != "unb=123; _m_h5_tk=tk_1;" {
			t.Fatalf("接口续期收到错误 Cookie: %q", cookieStr)
		}
		return result, nil
	})
	if renewErr != nil || !renewed || !callbackCalled {
		t.Fatalf("接口续期应在登录密码损坏时成功: renewed=%v callback=%v err=%v", renewed, callbackCalled, renewErr)
	}
}

// TestPersistRenewFlatCookieExcludesLoginSecrets 验证扁平 Cookie 写回只读取 metadata，不解密旧 Cookie 或登录密码。
func TestPersistRenewFlatCookieExcludesLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-flat-renew-query-key")
	// acc 是执行扁平 Cookie 写回的测试账号；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试扁平 Cookie 写回共用的上下文。
	ctx := context.Background()
	// metadata 是包含旧浏览器快照和其他配置的合法运行 metadata。
	metadata := cookierefresh.MetadataWithSnapshot(`{"other":true}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "old", Domain: ".goofish.com", Path: "/"}})
	// updateErr 表示预置完整 metadata 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", "sid=old", metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("preset metadata: %v", updateErr)
	}
	// corruptErr 表示将旧 Cookie 和登录密码密文损坏，用于验证写回只读取 metadata。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET value=?,password=? WHERE id=?`,
		"not-a-cookie-ciphertext", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt secrets: %v", corruptErr)
	}
	// persistErr 表示使用新响应 Cookie 写回数据库的结果。
	if persistErr := acc.persistRenewFlatCookie(ctx, "sid=fresh"); persistErr != nil {
		t.Fatalf("persist flat cookie: %v", persistErr)
	}
	// runtimeData 是写回后读取的 Cookie 与 metadata 窄模型。
	runtimeData, runtimeErr := store.Cookies.GetCookieRuntimeData(ctx, "cid")
	if runtimeErr != nil || runtimeData.Value != "sid=fresh" {
		t.Fatalf("runtime data=%+v err=%v", runtimeData, runtimeErr)
	}
	// complete 表示写回后的 metadata 是否仍包含完整浏览器 Cookie 快照。
	if _, complete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); complete {
		t.Fatal("扁平 Cookie 写回不得保留完整浏览器快照")
	}
	if !strings.Contains(runtimeData.MetadataJSON, `"other":true`) {
		t.Fatalf("扁平 Cookie 写回应保留其他 metadata: %s", runtimeData.MetadataJSON)
	}
}

// TestHandleMaxFailuresUsesValueWithoutLoginSecrets 验证恢复回调只读取 Cookie 明文，不解密登录密码。
func TestHandleMaxFailuresUsesValueWithoutLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-max-failures-query-key")
	// acc、handler 和 store 是本测试的账号、恢复回调记录器及数据库。
	acc, handler, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试数据库和恢复流程共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-max-failures-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// 将账号置于最大连接失败状态，确保进入密码登录成功后的 Cookie 写回分支。
	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()
	// cctx 让成功恢复后的固定等待快速结束，避免测试实际等待两秒。
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	// handleErr 是最大失败恢复流程的结果。
	handleErr := acc.handleMaxFailures(cctx)
	if handleErr != cctx.Err() {
		t.Fatalf("恢复成功后的等待应返回 ctx.Err(): got %v want %v", handleErr, cctx.Err())
	}
	if handler.refresh != 1 || acc.currentCookieStr() != "unb=123; _m_h5_tk=tk_1;" {
		t.Fatalf("恢复回调或 Cookie 异常: refresh=%d cookie=%q", handler.refresh, acc.currentCookieStr())
	}
}

// TestPersistPendingRenewCookiesUsesRuntimeData 验证迟到续期合并不解密损坏的登录密码。
func TestPersistPendingRenewCookiesUsesRuntimeData(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-pending-renew-query-key")
	// acc 和 store 是本测试的账号及数据库；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试迟到续期合并共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-pending-renew-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// lateResult 是后台静默续期迟到的 Set-Cookie 响应。
	lateResult := &xrenew.Result{SetCookies: []string{"sdkSilent=9999999999999; Domain=goofish.com; Path=/; Secure; HttpOnly"}}
	// persistErr 表示迟到 Cookie 合并和持久化的结果。
	if persistErr := acc.persistPendingRenewCookies(ctx, lateResult); persistErr != nil {
		t.Fatalf("persist pending renew cookies: %v", persistErr)
	}
	// runtimeData 是合并后读取的运行时 Cookie 与 metadata 窄模型。
	runtimeData, runtimeErr := store.Cookies.GetCookieRuntimeData(ctx, "cid")
	if runtimeErr != nil || !strings.Contains(runtimeData.Value, "sdkSilent=9999999999999") {
		t.Fatalf("迟到 Cookie 未写入: data=%+v err=%v", runtimeData, runtimeErr)
	}
	// snapshotComplete 表示迟到扁平 Cookie 是否被错误标记为完整浏览器快照。
	if _, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); snapshotComplete {
		t.Fatal("扁平迟到 Cookie 不应伪造完整浏览器快照")
	}
}

// scopedTokenRefreshRecorder 记录带 Cookie 快照的 token 请求参数。
type scopedTokenRefreshRecorder struct {
	fakeRunMtop
	// snapshot 保存 token 请求收到的 Cookie 快照副本。
	snapshot []cookierefresh.BrowserCookie
}

// RefreshTokenWithCredentialContext 返回成功 token，并记录请求使用的 Cookie 快照。
func (r *scopedTokenRefreshRecorder) RefreshTokenWithCredentialContext(_ context.Context, _ string, _ string, snapshot []cookierefresh.BrowserCookie) (*mtop.RefreshResult, error) {
	r.snapshot = append([]cookierefresh.BrowserCookie(nil), snapshot...)
	return &mtop.RefreshResult{AccessToken: "scoped-token", AccessTokenExpireAt: time.Now().Add(time.Hour).Unix()}, nil
}

// TestRefreshTokenWithMinGapUsesMetadataWithoutLoginSecrets 验证 token 刷新读取快照时不解密登录密码。
func TestRefreshTokenWithMinGapUsesMetadataWithoutLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-token-metadata-query-key")
	// acc 和 store 是本测试的账号及数据库；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试 token 刷新共用的上下文。
	ctx := context.Background()
	// snapshot 是应传给带凭证上下文 token 请求的权威 Cookie 快照。
	snapshot := []cookierefresh.BrowserCookie{{Name: "sid", Value: "snapshot", Domain: ".goofish.com", Path: "/", Secure: true}}
	// metadata 是包含快照的合法运行 metadata。
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"token"}`, snapshot)
	// updateErr 表示预置 token 刷新 metadata 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", "unb=123; _m_h5_tk=tk_1;", metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("preset metadata: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-token-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// recorder 记录 token 请求并提供不触网的成功响应。
	recorder := &scopedTokenRefreshRecorder{}
	acc.mtop = recorder
	// token、updatedCookies 和 refreshErr 是 token 刷新结果。
	token, updatedCookies, refreshErr := acc.refreshTokenWithMinGap(ctx, false)
	if refreshErr != nil || token != "scoped-token" || updatedCookies != "unb=123; _m_h5_tk=tk_1;" {
		t.Fatalf("token refresh=%q cookies=%q err=%v", token, updatedCookies, refreshErr)
	}
	if len(recorder.snapshot) != 1 || recorder.snapshot[0].Name != "sid" || recorder.snapshot[0].Value != "snapshot" {
		t.Fatalf("token 请求未收到 metadata 快照: %+v", recorder.snapshot)
	}
}

// TestAdoptTokenResponseCookiesUsesMetadataWithoutLoginSecrets 验证 token 响应合并不解密旧 Cookie 或登录密码。
func TestAdoptTokenResponseCookiesUsesMetadataWithoutLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-adopt-token-query-key")
	// acc 和 store 是本测试的账号及数据库；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试 token 响应合并共用的上下文。
	ctx := context.Background()
	// snapshot 是数据库中已有的权威 Cookie 快照。
	snapshot := []cookierefresh.BrowserCookie{{Name: "sid", Value: "old", Domain: ".goofish.com", Path: "/", Secure: true}}
	// metadata 是包含权威快照的合法运行 metadata。
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"adopt"}`, snapshot)
	// updateErr 表示预置 token 响应合并输入失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", "sid=old", metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("preset metadata: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-adopt-token-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// updatedCookies 是 token 响应带来的扁平 Cookie 更新。
	updatedCookies := "sid=fresh; token=next"
	// adoptedCookies 和 adoptErr 是 token 响应合并后的 Cookie 与错误。
	adoptedCookies, adoptErr := acc.adoptTokenResponseCookies(ctx, "sid=old", &mtop.RefreshResult{UpdatedCookies: updatedCookies})
	if adoptErr != nil || adoptedCookies != updatedCookies {
		t.Fatalf("adopt cookies=%q err=%v", adoptedCookies, adoptErr)
	}
	// runtimeData 是持久化后读取的 Cookie 与 metadata 窄模型。
	runtimeData, runtimeErr := store.Cookies.GetCookieRuntimeData(ctx, "cid")
	if runtimeErr != nil || runtimeData.Value != updatedCookies || !strings.Contains(runtimeData.MetadataJSON, `"note":"adopt"`) {
		t.Fatalf("adopted runtime data=%+v err=%v", runtimeData, runtimeErr)
	}
	// snapshotComplete 表示 token 响应合并后是否仍保留权威快照。
	if _, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); !snapshotComplete {
		t.Fatal("已有权威 Cookie 快照不应在 token 响应合并后丢失")
	}
}

// TestDatabaseCredentialFingerprintUsesRuntimeData 验证 token 凭证指纹不解密登录密码。
func TestDatabaseCredentialFingerprintUsesRuntimeData(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-credential-fingerprint-query-key")
	// acc 和 store 是本测试的账号及数据库；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试凭证指纹共用的上下文。
	ctx := context.Background()
	// cookieValue 是 token 请求期间数据库中的权威 Cookie 明文。
	cookieValue := "unb=123; _m_h5_tk=fingerprint"
	// metadata 是用于计算完整凭证状态指纹的运行 metadata。
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"fingerprint"}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "fp", Domain: ".goofish.com", Path: "/"}})
	// updateErr 表示预置凭证指纹输入失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", cookieValue, metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("preset credential: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-fingerprint-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// fingerprint 和 fingerprintErr 是窄查询生成的凭证状态指纹及错误。
	fingerprint, fingerprintErr := acc.databaseCredentialFingerprint(ctx, cookieValue)
	if fingerprintErr != nil || fingerprint != credentialStateFingerprint(cookieValue, metadata) {
		t.Fatalf("credential fingerprint=%q err=%v", fingerprint, fingerprintErr)
	}
}

// TestReloadCookieFromDBUsesRuntimeData 验证外部 Cookie 更新检测不解密登录密码。
func TestReloadCookieFromDBUsesRuntimeData(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-reload-cookie-query-key")
	// acc 和 store 是本测试的账号及数据库；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是测试外部 Cookie 更新检测共用的上下文。
	ctx := context.Background()
	// cookieValue 是数据库中待同步到运行时的 Cookie 明文。
	cookieValue := "unb=123; _m_h5_tk=reload-runtime"
	// metadata 是数据库中待同步的权威 Cookie 快照 metadata。
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"reload"}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "reload", Domain: ".goofish.com", Path: "/"}})
	// updateErr 表示预置外部 Cookie 更新失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", cookieValue, metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("preset reload credential: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-reload-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// reloaded 表示运行时是否采纳数据库中的新凭证状态。
	reloaded := acc.reloadCookieFromDB(ctx)
	if !reloaded || acc.currentCookieStr() != cookieValue {
		t.Fatalf("reload result=%v runtime cookie=%q", reloaded, acc.currentCookieStr())
	}
	// acc.mu 保护 credentialFP，读取后用于验证 Cookie 与 metadata 均已同步。
	acc.mu.Lock()
	// currentFP 是运行时记录的 Cookie 与 metadata 组合指纹。
	currentFP := acc.credentialFP
	acc.mu.Unlock()
	if currentFP != credentialStateFingerprint(cookieValue, metadata) {
		t.Fatalf("runtime credential fingerprint=%q", currentFP)
	}
}
