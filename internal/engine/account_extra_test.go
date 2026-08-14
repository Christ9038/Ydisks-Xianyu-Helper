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
	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
)

type riskCountingMTop struct {
	fakeRunMtop
	calls int
}

func (m *riskCountingMTop) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	m.calls++
	if m.calls > 1 {
		return &mtop.RefreshResult{AccessToken: "standard-token", AccessTokenExpireAt: time.Now().Add(time.Hour).Unix(), UpdatedCookies: "unb=123; _m_h5_tk=recovered;"}, nil
	}
	return nil, &mtop.RiskVerificationError{Ret: []string{"FAIL_SYS_USER_VALIDATE"}, VerificationURL: "https://verify.example"}
}

type tokenRecoveredHandler struct{ recordingHandler }

func (h *tokenRecoveredHandler) OnTokenCaptchaVerification(context.Context, string, string, string, string) (*mtop.RefreshResult, bool) {
	return &mtop.RefreshResult{AccessToken: "recovered-token", UpdatedCookies: "unb=123; _m_h5_tk=recovered;"}, true
}

type rejectingTokenCaptchaHandler struct {
	recordingHandler
	calls int
}

func (h *rejectingTokenCaptchaHandler) OnTokenCaptchaVerification(context.Context, string, string, string, string) (*mtop.RefreshResult, bool) {
	h.calls++
	return nil, false
}

type capturingCaptchaHandler struct {
	recordingHandler
	cookieStr string
	deviceID  string
}

func (h *capturingCaptchaHandler) OnTokenCaptchaVerification(_ context.Context, _, cookieStr, _, deviceID string) (*mtop.RefreshResult, bool) {
	h.cookieStr = cookieStr
	h.deviceID = deviceID
	return &mtop.RefreshResult{UpdatedCookies: cookieStr + "; x5sec=fresh"}, true
}

type responseCookieRiskMTop struct{ calls int }

func (m *responseCookieRiskMTop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}
func (m *responseCookieRiskMTop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return true, nil, "", nil
}
func (m *responseCookieRiskMTop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (m *responseCookieRiskMTop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (m *responseCookieRiskMTop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}
func (m *responseCookieRiskMTop) RefreshTokenWithDeviceIDContext(_ context.Context, cookieStr, _ string) (*mtop.RefreshResult, error) {
	m.calls++
	if m.calls == 1 {
		updated := strings.Replace(cookieStr, "_m_h5_tk=tk_1", "_m_h5_tk=server_1", 1)
		return &mtop.RefreshResult{UpdatedCookies: updated}, &mtop.RiskVerificationError{
			Ret: []string{"FAIL_SYS_USER_VALIDATE"}, VerificationURL: "https://verify.example/punish",
		}
	}
	return &mtop.RefreshResult{AccessToken: "standard-after-captcha", AccessTokenExpireAt: time.Now().Add(time.Hour).Unix(), UpdatedCookies: cookieStr}, nil
}

func TestRefreshTokenRetriesStandardRequestAfterCaptchaRecovery(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()
	client := &riskCountingMTop{}
	acc.mtop = client
	acc.handler = &tokenRecoveredHandler{}

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

func TestRefreshTokenCaptchaFailureEntersCallerCooldown(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	client := &riskCountingMTop{}
	handler := &rejectingTokenCaptchaHandler{}
	acc.mtop = client
	acc.handler = handler

	if _, _, err := acc.refreshToken(context.Background()); !mtop.IsRiskVerificationErr(err) {
		t.Fatalf("first refresh error=%v want risk verification", err)
	}
	if _, _, err := acc.refreshToken(context.Background()); !errors.Is(err, errTokenCaptchaCooldown) {
		t.Fatalf("second refresh error=%v want cooldown", err)
	}
	if client.calls != 1 || handler.calls != 1 {
		t.Fatalf("cooldown must suppress repeated API/solver calls: api=%d solver=%d", client.calls, handler.calls)
	}
}

func TestRefreshTokenPersistsResponseCookiesBeforeCaptchaRecovery(t *testing.T) {
	acc, _, store, cleanup := newRunAccount(t, &responseCookieRiskMTop{})
	defer cleanup()
	handler := &capturingCaptchaHandler{}
	acc.handler = handler

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
	saved, err := store.Cookies.GetValue(context.Background(), "cid")
	if err != nil || !strings.Contains(saved, "_m_h5_tk=server_1") || !strings.Contains(saved, "x5sec=fresh") {
		t.Fatalf("响应/验证 Cookie 未完整持久化: saved=%q err=%v", saved, err)
	}
}

// TestStop_IdempotentAndClearsTimers Stop 重复调用幂等；调用后防抖定时器被清空。
func TestStop_IdempotentAndClearsTimers(t *testing.T) {
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
	status := acc.RuntimeStatus()
	if status.State != RuntimeStopped {
		t.Errorf("state=%q want %q", status.State, RuntimeStopped)
	}
	acc.debounceMu.Lock()
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
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	if acc.stopFn != nil {
		t.Fatal("未启动 Run 时 stopFn 应为 nil")
	}
	// 不应 panic。
	acc.Stop()
}

// TestRuntimeStatus_ConnectedRequiresOnlineAndConn conn 为 nil 或状态非 online 时 Connected=false。
func TestRuntimeStatus_ConnectedRequiresOnlineAndConn(t *testing.T) {
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

func (a *alertCountingHandler) OnAccountAlert(_ context.Context, _, level, _, _ string) {
	if level == AlertLevelWarn {
		atomic.AddInt32(&a.alerts, 1)
	}
}

type stubCookieRenewer struct {
	result *xrenew.Result
	err    error
	calls  int
	got    string
}

func (s *stubCookieRenewer) RenewAPIFirst(_ context.Context, cookiesStr string, _ ...[]cookierefresh.BrowserCookie) (*xrenew.Result, error) {
	s.calls++
	s.got = cookiesStr
	return s.result, s.err
}

func TestTryAPIRenewSuccessShortCircuitsRecovery(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Tokens.Save(ctx, "cid", "did-old", "tok-old", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("save token: %v", err)
	}
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
	if got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=tk_2;" {
		t.Fatalf("内存 cookie 未更新: %q", got)
	}
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved != "unb=123; _m_h5_tk=tk_2;" {
		t.Fatalf("DB cookie 未更新: %q", saved)
	}
	if tk, err := store.Tokens.Get(ctx, "cid"); err != nil || tk.AccessToken != "" {
		t.Fatalf("接口续期后应清 token；数据库中的旧 device ID 不再参与运行时身份: tk=%+v err=%v", tk, err)
	}
}

func TestTryAPIRenewPartialCookiesContinueRecovery(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Tokens.Save(ctx, "cid", "did-old", "tok-old", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("save token: %v", err)
	}
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
	if got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=partial;" {
		t.Fatalf("部分 cookie 应先保存到内存: %q", got)
	}
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved != "unb=123; _m_h5_tk=partial;" {
		t.Fatalf("部分 cookie 应先保存到 DB: %q", saved)
	}
	if tk, err := store.Tokens.Get(ctx, "cid"); err != nil || tk.AccessToken != "" {
		t.Fatalf("部分 cookie 更新后应清 token: tk=%+v err=%v", tk, err)
	}
	detail, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	if _, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("接口扁平 Cookie 更新不得伪造成完整浏览器 Jar")
	}
}

func TestTryAPIRenewPersistsExplicitFlatCookieDeletionOnError(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Tokens.Save(ctx, "cid", "did-old", "tok-old", time.Now().Add(time.Hour).Unix()); err != nil {
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
	if got := acc.currentCookieStr(); got != "" {
		t.Fatalf("运行时应采用服务端明确删除后的空 Cookie，got %q", got)
	}
	detail, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "" {
		t.Fatalf("数据库应保存明确删除后的空 Cookie，got %q", detail.Value)
	}
	if _, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("扁平删除结果不得伪造成完整浏览器 Jar")
	}
	if token, err := store.Tokens.Get(ctx, "cid"); err != nil || token.AccessToken != "" {
		t.Fatalf("Cookie 删除后应清理旧 token: token=%+v err=%v", token, err)
	}
}

func TestTryAPIRenewPersistsLatePromiseCookieWithoutRestart(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
	oldFingerprint := xianyu.CurrentBrowserFingerprint()
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: "Mozilla/5.0 (Macintosh) Chrome/999.0.0.0 Safari/537.36"})
	t.Cleanup(func() { xianyu.SetBrowserFingerprint(oldFingerprint) })
	initial := "unb=123; havana_lgc_exp=" + fmt.Sprint(time.Now().Add(time.Hour).UnixMilli())
	if err := store.Cookies.UpdateValueExisting(ctx, "cid", initial); err != nil {
		t.Fatal(err)
	}
	acc.replaceCookieStr(initial)
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
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
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

func TestTryAPIRenewPendingWatcherStopsWithAccountContext(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	initial := "unb=123; havana_lgc_exp=" + fmt.Sprint(time.Now().Add(time.Hour).UnixMilli())
	if err := store.Cookies.UpdateValueExisting(context.Background(), "cid", initial); err != nil {
		t.Fatal(err)
	}
	acc.replaceCookieStr(initial)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: fmt.Sprint(time.Now().Add(time.Hour).UnixMilli()), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	acc.renewer = xrenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	if acc.tryAPIRenew(ctx) {
		t.Fatal("Promise 超时不得伪装成同步续期成功")
	}
	cancel()
	acc.pendingRenewWG.Wait()
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
func TestSetRuntimeError_AllBranches(t *testing.T) {
	ctx := context.Background()

	// 1) captcha/risk 关键词 → VerificationRequired，且从非验证态进入时告警一次。
	t.Run("captcha", func(t *testing.T) {
		acc, _, _, cleanup := newAccountForTest(t)
		defer cleanup()
		h := &alertCountingHandler{}
		acc.handler = h
		acc.setRuntimeError(ctx, fmt.Errorf("FAIL_SYS_USER_VALIDATE: captcha required"))
		if s := acc.RuntimeStatus(); s.State != RuntimeVerificationRequired {
			t.Fatalf("state=%q", s.State)
		}
		if got := atomic.LoadInt32(&h.alerts); got != 1 {
			t.Errorf("首次进入验证态应告警一次，got %d want 1", got)
		}
		// 再次以验证错误进入：不应重复告警（prev 已是 verification）。
		acc.setRuntimeError(ctx, fmt.Errorf("rgv587 风控"))
		if got := atomic.LoadInt32(&h.alerts); got != 1 {
			t.Errorf("验证态重复进入不应重复告警，got %d want 1", got)
		}
	})

	// 2) token expired 关键词 → AuthExpired（不告警）。
	t.Run("token_expired", func(t *testing.T) {
		acc, _, _, cleanup := newAccountForTest(t)
		defer cleanup()
		h := &alertCountingHandler{}
		acc.handler = h
		for _, msg := range []string{
			"登录凭证已失效",
			"FAIL_SYS_TOKEN_EXOIRED",
			"FAIL_SYS_TOKEN_EXPIRED",
			"cookie 缺少 unb",
		} {
			acc.setRuntimeError(ctx, errors.New(msg))
			if s := acc.RuntimeStatus(); s.State != RuntimeAuthExpired {
				t.Fatalf("msg=%q state=%q want %q", msg, s.State, RuntimeAuthExpired)
			}
		}
		if got := atomic.LoadInt32(&h.alerts); got != 0 {
			t.Errorf("token 失效分支不应触发 warn 告警，got %d", got)
		}
	})

	// 3) 其他错误 → Reconnecting（不告警）。
	t.Run("other", func(t *testing.T) {
		acc, _, _, cleanup := newAccountForTest(t)
		defer cleanup()
		h := &alertCountingHandler{}
		acc.handler = h
		acc.setRuntimeError(ctx, errors.New("connection refused"))
		if s := acc.RuntimeStatus(); s.State != RuntimeReconnecting {
			t.Fatalf("state=%q want %q", s.State, RuntimeReconnecting)
		}
		if got := atomic.LoadInt32(&h.alerts); got != 0 {
			t.Errorf("默认分支不应告警，got %d", got)
		}
	})
}

// TestAlert_NilHandler handler 为 nil 时静默跳过，不 panic。
func TestAlert_NilHandler(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.handler = nil
	// 不应 panic。
	acc.alert(context.Background(), AlertLevelCritical, "title", "body")
}

// TestResetFailures 重置失败计数。
func TestResetFailures(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.connFailures = 7
	acc.resetFailures()
	if acc.connFailures != 0 {
		t.Errorf("resetFailures 后 connFailures=%d want 0", acc.connFailures)
	}
}

func TestRefreshTokenSuccessResetsTokenFetchFailures(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.mtop = &fakeRunMtop{token: "tok-reset"}
	acc.tokenFetchFailures = 19

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
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.connFailures = 3
	// min(2*2^3,90)=16s。
	expectDelayRange(t, acc.retryDelay("dial timeout"), 16*time.Second)
	acc.connFailures = 10
	expectDelayRange(t, acc.retryDelay("timeout"), 90*time.Second)
}

func TestNetworkRetryDelayMatchesReferenceBackoff(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.networkFailures = 1
	expectDelayRange(t, acc.networkRetryDelay(), 4*time.Second)
	acc.networkFailures = 10
	expectDelayRange(t, acc.networkRetryDelay(), 60*time.Second)
}

func TestEstablishedNetworkErrorClassification(t *testing.T) {
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

func TestRecordShortDisconnectDisablesAtFiveWithinWindow(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	for i := 1; i < FrequentDisconnectLimit; i++ {
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
func TestHandleMaxFailures_RecentMessageStillRunsRecovery(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()

	// 设最近收到消息（在 MessageCooldown 内）。
	acc.mu.Lock()
	acc.lastMsgReceived = time.Now()
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	cctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
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
	acc, h, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()

	// 无最近消息、未在密码登录冷却中。
	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	// handler.OnPasswordLoginRefresh 返回 true → 成功路径。
	// store.Cookies.GetDetails 返回新 cookie，应触发 replaceCookieStr。
	newCookie := "unb=999; _m_h5_tk=tk_new;"
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if err := store.Cookies.Save(ctx, "cid", newCookie, admin.ID); err != nil {
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
	if s := acc.RuntimeStatus(); s.State != RuntimeConnecting {
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

func (f *failingRefreshHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }
func (f *failingRefreshHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}
func (f *failingRefreshHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }
func (f *failingRefreshHandler) OnAccountAlert(_ context.Context, _, level, _, _ string) {
	f.alerts = append(f.alerts, level)
}
func (f *failingRefreshHandler) OnAccountEvent(_ context.Context, _, eventType, level, _, _ string) {
	f.events = append(f.events, eventType)
	f.alerts = append(f.alerts, level)
}

// TestHandleMaxFailures_PasswordLoginFailure 密码登录刷新失败后终止账号主循环。
func TestHandleMaxFailures_PasswordLoginFailure(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	// handler 返回 false → 刷新失败。
	h := &failingRefreshHandler{}
	acc.handler = h

	err := acc.handleMaxFailures(context.Background())
	if err == nil || !strings.Contains(err.Error(), "自动恢复失败") {
		t.Fatalf("刷新失败应返回终止主循环的错误，got %v", err)
	}
	if s := acc.RuntimeStatus(); s.State != RuntimeAuthExpired {
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
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	// 初始：UserID=123，deviceID 已由 New 生成。
	acc.mu.Lock()
	oldDevice := acc.deviceID
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
	newDevice := acc.deviceID
	newUser := acc.UserID
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
func TestReplaceCookieStrDoesNotGenerateDeviceID(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	// 清空 deviceID 与 UserID，模拟异常状态。
	acc.mu.Lock()
	acc.deviceID = ""
	acc.UserID = ""
	acc.mu.Unlock()

	acc.replaceCookieStr("unb=789; _m_h5_tk=tk3;")
	acc.mu.Lock()
	d := acc.deviceID
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
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.mu.Lock()
	oldDevice := acc.deviceID
	oldUser := acc.UserID
	acc.mu.Unlock()

	acc.replaceCookieStr("foo=bar; baz=qux;")
	acc.mu.Lock()
	d := acc.deviceID
	u := acc.UserID
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
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	orig := acc.currentCookieStr()

	// 空字符串与纯空白（TrimSpace 后为空）应被忽略。
	acc.UpdateCookie("")
	acc.UpdateCookie("   ")
	acc.UpdateCookie("\t\n")
	if got := acc.currentCookieStr(); got != orig {
		t.Errorf("空白 cookie 不应更新: got %q want %q", got, orig)
	}

	acc.mu.Lock()
	originalDevice := acc.deviceID
	healthyConn := &fakeWSConn{}
	acc.conn = healthyConn
	acc.mu.Unlock()

	// 非空（即使含首尾空白）应原样存储，但不能模拟页面 reload 去打断健康 WS。
	updated := "unb=123; _m_h5_tk=tk_new;"
	if err := store.Cookies.UpdateValueExisting(context.Background(), acc.CookieID, updated); err != nil {
		t.Fatal(err)
	}
	acc.UpdateCookie(updated)
	if got := acc.currentCookieStr(); got != updated {
		t.Errorf("非空 cookie 应更新: got %q", got)
	}
	acc.mu.Lock()
	currentDevice := acc.deviceID
	acc.mu.Unlock()
	healthyConn.mu.Lock()
	closed := healthyConn.closed
	healthyConn.mu.Unlock()
	if currentDevice != originalDevice || closed {
		t.Fatalf("普通 Cookie 更新不应轮换 device ID 或关闭健康连接: device=%q/%q closed=%v", currentDevice, originalDevice, closed)
	}
}

func TestUpdateCookie_AcceptsAuthoritativeEmptySnapshot(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	metadata := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{})
	if err := store.Cookies.UpdateRenewalCookie(context.Background(), acc.CookieID, "", metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	acc.UpdateCookie("")
	if got := acc.currentCookieStr(); got != "" {
		t.Fatalf("权威空 Jar 应清空运行时 Cookie，got %q", got)
	}
}

func TestReloadCookieFromDBDetectsMetadataOnlyCredentialRotation(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
	flat := acc.currentCookieStr()
	metadataA := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-a", Domain: ".goofish.com", Path: "/im"},
	})
	if err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataA, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if !acc.reloadCookieFromDB(ctx) {
		t.Fatal("首次权威 Jar 应同步到运行时")
	}
	acc.mu.Lock()
	boundFP := acc.credentialFP
	acc.currentToken = "old-token"
	acc.tokenCredentialFP = boundFP
	acc.mu.Unlock()
	if err := store.Tokens.SaveBound(ctx, acc.CookieID, acc.deviceID, "old-token", time.Now().Add(time.Hour).Unix(), boundFP); err != nil {
		t.Fatal(err)
	}

	metadataB := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-b", Domain: ".goofish.com", Path: "/im"},
	})
	if err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataB, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if !acc.reloadCookieFromDB(ctx) {
		t.Fatal("扁平 Cookie 未变但权威 Jar 已变化时必须重新加载")
	}
	acc.mu.Lock()
	currentToken := acc.currentToken
	currentFP := acc.credentialFP
	acc.mu.Unlock()
	if currentToken != "" {
		t.Fatalf("Jar 变化后应清内存 token，got %q", currentToken)
	}
	if currentFP != credentialStateFingerprint(flat, metadataB) {
		t.Fatal("运行时未绑定到最新完整凭证状态")
	}
	if cached, err := store.Tokens.Get(ctx, acc.CookieID); err != nil || cached.AccessToken != "" {
		t.Fatalf("Jar 变化后应清数据库 token: cached=%+v err=%v", cached, err)
	}
}

func TestCookieSnapshotMatchesDBUsesCompleteCredentialState(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
	flat := acc.currentCookieStr()
	metadataA := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "sgcookie", Value: "a", Domain: ".goofish.com", Path: "/"},
	})
	if err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataA, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	expected := credentialStateFingerprint(flat, metadataA)
	if !acc.cookieSnapshotMatchesDB(ctx, expected) {
		t.Fatal("相同扁平 Cookie 与权威 Jar 应允许 /reg")
	}
	metadataB := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "sgcookie", Value: "b", Domain: ".goofish.com", Path: "/"},
	})
	if err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, flat, metadataB, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if acc.cookieSnapshotMatchesDB(ctx, expected) {
		t.Fatal("token 获取后 Jar 变化时必须拒绝 /reg")
	}
	emptyMetadata := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{})
	if err := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, "", emptyMetadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if !acc.cookieSnapshotMatchesDB(ctx, credentialStateFingerprint("", emptyMetadata)) {
		t.Fatal("完整空 Jar 是权威状态，不应因扁平值为空而被拒绝")
	}
}

func TestAdoptIncompleteTokenCookiesDoesNotInventCompleteSnapshot(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
	updated := "unb=123; _m_h5_tk=flat-only; cookie2=next"
	got, err := acc.adoptTokenResponseCookies(ctx, acc.currentCookieStr(), &mtop.RefreshResult{
		UpdatedCookies: updated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != updated {
		t.Fatalf("adopted Cookie=%q want %q", got, updated)
	}
	detail, err := store.Cookies.GetDetails(ctx, acc.CookieID)
	if err != nil {
		t.Fatal(err)
	}
	if _, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("仅有扁平 token 响应时不得伪造成完整浏览器 Jar")
	}
}

func TestAdoptTokenResponseCookiesPersistsExplicitDeletionToEmpty(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()
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
	detail, err := store.Cookies.GetDetails(ctx, acc.CookieID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "" {
		t.Fatalf("数据库 Cookie=%q want empty", detail.Value)
	}
	if _, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("扁平 token 删除不得伪造成完整浏览器 Jar")
	}
}

// TestCurrentCookieStr 线程安全返回当前 CookieStr。
func TestCurrentCookieStr(t *testing.T) {
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	if got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=tk_1;" {
		t.Errorf("currentCookieStr=%q", got)
	}
	updated := "unb=1; x=2;"
	if err := store.Cookies.UpdateValueExisting(context.Background(), acc.CookieID, updated); err != nil {
		t.Fatal(err)
	}
	acc.UpdateCookie(updated)
	if got := acc.currentCookieStr(); got != updated {
		t.Errorf("更新后 currentCookieStr=%q", got)
	}
}

// TestSleepCtx 正常睡眠返回 nil；ctx 取消返回 ctx.Err()；d<=0 立即返回。
func TestSleepCtx(t *testing.T) {
	// d<=0 立即返回 nil。
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Errorf("d=0 应返回 nil，got %v", err)
	}
	if err := sleepCtx(context.Background(), -time.Second); err != nil {
		t.Errorf("d<0 应返回 nil，got %v", err)
	}

	// 正常短睡眠。
	start := time.Now()
	if err := sleepCtx(context.Background(), 50*time.Millisecond); err != nil {
		t.Errorf("正常睡眠应返回 nil，got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("睡眠时间过短: %v", elapsed)
	}

	// ctx 取消：应立即返回 ctx.Err()。
	cctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start = time.Now()
	err := sleepCtx(cctx, 5*time.Second)
	if err != cctx.Err() {
		t.Errorf("sleepCtx 取消应返回 ctx.Err(): got %v want %v", err, cctx.Err())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("ctx 取消应立即返回，耗时 %v", elapsed)
	}
}

// TestErrString errString 处理 nil 与非 nil。
func TestErrString(t *testing.T) {
	if got := errString(nil); got != "" {
		t.Errorf("errString(nil)=%q want empty", got)
	}
	e := errors.New("boom")
	if got := errString(e); got != "boom" {
		t.Errorf("errString=%q want boom", got)
	}
}

// TestTruncID 长串截断、短串原样。
func TestTruncID(t *testing.T) {
	short := "abc123"
	if got := truncID(short); got != short {
		t.Errorf("短串应原样返回: got %q", got)
	}
	long := ""
	for i := 0; i < 80; i++ {
		long += "x"
	}
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
