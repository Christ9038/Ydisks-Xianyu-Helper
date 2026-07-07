package engine

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/automation"
)

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

// TestRetryDelay_FailureClampsAtOne connFailures=0 时按 1 计算。
func TestRetryDelay_FailureClampsAtOne(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.connFailures = 0
	// close-frame：min(3*1,15)=3s。
	if d := acc.retryDelay("no close frame received or sent"); d != 3*time.Second {
		t.Errorf("failures=0 clamp to 1: got %v want 3s", d)
	}
	// timeout：min(10*1,60)=10s。
	if d := acc.retryDelay("timeout reading"); d != 10*time.Second {
		t.Errorf("timeout failures=0: got %v want 10s", d)
	}
	// default：min(5*1,30)=5s。
	if d := acc.retryDelay("random error"); d != 5*time.Second {
		t.Errorf("default failures=0: got %v want 5s", d)
	}
}

// TestRetryDelay_TimeoutVariant "timeout" 关键词分支。
func TestRetryDelay_TimeoutVariant(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	acc.connFailures = 3
	// min(10*3,60)=30s。
	if d := acc.retryDelay("dial timeout"); d != 30*time.Second {
		t.Errorf("timeout failures=3: got %v want 30s", d)
	}
	acc.connFailures = 10
	if d := acc.retryDelay("timeout"); d != 60*time.Second {
		t.Errorf("timeout failures=10 cap: got %v want 60s", d)
	}
}

// TestHandleMaxFailures_RecentMessageSkipPasswordLogin 最近仍收到消息时跳过密码登录刷新，
// 重置失败计数并返回 nil（睡眠时间可被 ctx 取消）。
func TestHandleMaxFailures_RecentMessageSkipPasswordLogin(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()

	// 设最近收到消息（在 MessageCooldown 内）。
	acc.mu.Lock()
	acc.lastMsgReceived = time.Now()
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	// 最近消息路径会 resetFailures 后进入 sleepCtx 等待 retryDelay。
	// 用一个短超时 ctx 让 sleepCtx 提前返回 ctx.Err()，证明走了 sleep 分支（而非密码刷新）。
	cctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	err := acc.handleMaxFailures(cctx)
	if err != cctx.Err() {
		t.Fatalf("最近收到消息应走 sleep 分支返回 ctx.Err()，got %v want %v", err, cctx.Err())
	}
	if h.refresh != 0 {
		t.Errorf("不应触发密码登录刷新，got %d", h.refresh)
	}
	if acc.connFailures != 0 {
		t.Errorf("应重置失败计数，got %d", acc.connFailures)
	}
}

// TestHandleMaxFailures_PasswordLoginCooldown 密码登录刷新冷却中跳过本次刷新。
func TestHandleMaxFailures_PasswordLoginCooldown(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	ctx := context.Background()

	// 无最近消息，但最近做过密码登录（在 PasswordLoginMinGap 内）。
	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{} // 零值，绕过 message cooldown
	acc.lastPasswordLogin = time.Now()
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	cctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	err := acc.handleMaxFailures(cctx)
	if err != cctx.Err() {
		t.Fatalf("冷却中应走 sleep 分支返回 ctx.Err()，got %v want %v", err, cctx.Err())
	}
	if h.refresh != 0 {
		t.Errorf("冷却中不应调用 OnPasswordLoginRefresh，got %d", h.refresh)
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
	acc.lastPasswordLogin = time.Time{}
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
}

func (f *failingRefreshHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }
func (f *failingRefreshHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}
func (f *failingRefreshHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }
func (f *failingRefreshHandler) OnAccountAlert(_ context.Context, _, level, _, _ string) {
	f.alerts = append(f.alerts, level)
}

// TestHandleMaxFailures_PasswordLoginFailure 密码登录刷新失败：不再硬退出，进入慢重试，
// 状态 AuthExpired，触发一次 critical 告警。
func TestHandleMaxFailures_PasswordLoginFailure(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.lastPasswordLogin = time.Time{}
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()

	// handler 返回 false → 刷新失败。
	h := &failingRefreshHandler{}
	acc.handler = h

	// 新行为：不再 return fatal error，而是 sleepCtx(AuthExpiredRetryInterval) 慢重试。
	// 用短超时 ctx 让 sleepCtx 提前返回 ctx.Err()，证明走了慢重试分支而非硬退出。
	cctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := acc.handleMaxFailures(cctx)
	if err != cctx.Err() {
		t.Fatalf("刷新失败应走慢重试 sleep 分支返回 ctx.Err()，got %v want %v", err, cctx.Err())
	}
	if s := acc.RuntimeStatus(); s.State != RuntimeAuthExpired {
		t.Errorf("状态应为 auth_expired，got %q", s.State)
	}
	if len(h.alerts) != 1 || h.alerts[0] != AlertLevelCritical {
		t.Errorf("应触发一次 critical 告警，got %+v", h.alerts)
	}
}

// TestReplaceCookieStr_UpdateUserIDAndDeviceID 更新 cookie 后 unb 变化时重置 deviceID。
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
	// 不同 unb 应重新生成 deviceID（基于 unb 的哈希）。
	if newDevice == oldDevice {
		t.Error("unb 变化后 deviceID 应重新生成")
	}
	if newCookie != "unb=456; _m_h5_tk=tk2;" {
		t.Errorf("CookieStr 未更新: got %q", newCookie)
	}
}

// TestReplaceCookieStr_EmptyDeviceIDFallback deviceID 为空且 cookie 含 unb 时兜底生成。
func TestReplaceCookieStr_EmptyDeviceIDFallback(t *testing.T) {
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
	if d == "" {
		t.Error("deviceID 应兜底生成")
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
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	orig := acc.currentCookieStr()

	// 空字符串与纯空白（TrimSpace 后为空）应被忽略。
	acc.UpdateCookie("")
	acc.UpdateCookie("   ")
	acc.UpdateCookie("\t\n")
	if got := acc.currentCookieStr(); got != orig {
		t.Errorf("空白 cookie 不应更新: got %q want %q", got, orig)
	}

	// 非空（即使含首尾空白）应原样存储——UpdateCookie 只做空值检查，不做 trim。
	acc.UpdateCookie("unb=123; _m_h5_tk=tk_new;")
	if got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=tk_new;" {
		t.Errorf("非空 cookie 应更新: got %q", got)
	}
}

// TestCurrentCookieStr 线程安全返回当前 CookieStr。
func TestCurrentCookieStr(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	if got := acc.currentCookieStr(); got != "unb=123; _m_h5_tk=tk_1;" {
		t.Errorf("currentCookieStr=%q", got)
	}
	acc.UpdateCookie("unb=1; x=2;")
	if got := acc.currentCookieStr(); got != "unb=1; x=2;" {
		t.Errorf("更新后 currentCookieStr=%q", got)
	}
}

// TestMinDuration 取较小值。
func TestMinDuration(t *testing.T) {
	cases := []struct {
		a, b, want time.Duration
	}{
		{1 * time.Second, 2 * time.Second, 1 * time.Second},
		{2 * time.Second, 1 * time.Second, 1 * time.Second},
		{5 * time.Second, 5 * time.Second, 5 * time.Second},
		{0, 3 * time.Second, 0},
	}
	for _, c := range cases {
		if got := minDuration(c.a, c.b); got != c.want {
			t.Errorf("minDuration(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
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
