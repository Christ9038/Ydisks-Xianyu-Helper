package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
	"xianyu-go/internal/renewal"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// fakeNotifier 记录告警调用，供 OnAccountAlert 断言。
type fakeNotifier struct {
	alerts []struct{ cookieID, level, title, body string }
}

func (f *fakeNotifier) NotifyAccountAlert(cookieID, level, title, body string) {
	f.alerts = append(f.alerts, struct{ cookieID, level, title, body string }{cookieID, level, title, body})
}

// fakeBrowser 桩实现 browserManager 接口，记录调用并返回可控结果。
type fakeBrowser struct {
	fetchErr     error
	fetchDetail  *browser.OrderDetail
	renewErr     error
	renewCookies map[string]string
	renewCalls   int
	loginErr     error
	loginCookies map[string]string
	loginCalls   int
}

func (f *fakeBrowser) FetchOrderDetail(_ context.Context, _, _, _ string, _ ...bool) (*browser.OrderDetail, error) {
	return f.fetchDetail, f.fetchErr
}
func (f *fakeBrowser) CookieRenew(_ context.Context, _, _ string, _ bool) (map[string]string, error) {
	f.renewCalls++
	return f.renewCookies, f.renewErr
}
func (f *fakeBrowser) PasswordLogin(_ context.Context, _, _, _, _ string, _ bool) (map[string]string, error) {
	f.loginCalls++
	return f.loginCookies, f.loginErr
}

func newAdapterTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "adapt.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d, db.DialectSQLite)
	ctx := context.Background()
	s.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	s.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=tk; havana_lgc2_77=lgc;", admin.ID)
	renewal.GlobalCooldown.Reset("cid")
	return s, func() { d.Close() }
}

func verifiedRenewService(t *testing.T) (xrenew.Service, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do", "/silentHasLogin.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/setLoginSettings.do":
			http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "verified"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return xrenew.Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}, srv.Close
}

func unverifiedRenewService(t *testing.T) (xrenew.Service, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do", "/silentHasLogin.do", "/setLoginSettings.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return xrenew.Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}, srv.Close
}

// TestOnAccountAlert_ForwardedToNotifier 注入 notifier 后告警被转发；未注入时不 panic。
func TestOnAccountAlert_ForwardedToNotifier(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)

	// 未注入 notifier：不应 panic，仅记录日志。
	a.OnAccountAlert(context.Background(), "cid", "warn", "t", "b")

	n := &fakeNotifier{}
	a.SetNotifier(n)
	a.OnAccountAlert(context.Background(), "cid", "warn", "token 失效", "请重新登录")
	if len(n.alerts) != 1 || n.alerts[0].cookieID != "cid" || n.alerts[0].title != "token 失效" {
		t.Fatalf("告警未转发: %+v", n.alerts)
	}
}

// TestHandleSystemEvent_UninjectedSafe 未注入 automation 时安全返回 nil。
func TestHandleSystemEvent_UninjectedSafe(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	if err := a.HandleSystemEvent(context.Background(), automation.Task{AccountID: "cid"}); err != nil {
		t.Fatalf("未注入 automation 应返回 nil: %v", err)
	}
}

// TestFetchOrderDetail_LocalHitShortCircuits 本地订单字段齐全时不启动浏览器。
func TestFetchOrderDetail_LocalHitShortCircuits(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, cookie_id, spec_name, spec_value, quantity, amount) VALUES ('o-local','cid','套餐','30天','1','9.9')`)

	// browser=nil，本地命中应短路；若误走浏览器分支会 panic。
	a := New(store, nil, nil)
	detail, err := a.FetchOrderDetail(ctx, "cid", "o-local", "item-1", "buyer-1", "cookie")
	if err != nil {
		t.Fatalf("本地命中不应报错: %v", err)
	}
	if detail.SpecName != "套餐" || detail.Amount != "9.9" {
		t.Fatalf("detail=%+v", detail)
	}
}

// TestFetchOrderDetail_BrowserNilReturnsError 本地缺字段且浏览器未启用时返回明确错误。
func TestFetchOrderDetail_BrowserNilReturnsError(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	_, err := a.FetchOrderDetail(context.Background(), "cid", "o-missing", "item-1", "buyer-1", "cookie")
	if err == nil {
		t.Fatal("browser=nil 且本地缺失应返回错误")
	}
}

// TestFetchOrderDetail_BrowserFallback 本地缺失但有浏览器时调用浏览器抓取并保存刷新的 cookie。
func TestFetchOrderDetail_BrowserFallback(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	a := New(store, nil, nil)
	a.SetBrowser(&fakeBrowser{
		fetchDetail: &browser.OrderDetail{
			Quantity: "2", SpecName: "套餐", SpecValue: "30天", Amount: "19.8",
			UpdatedCookies: "unb=1; _m_h5_tk=newtoken;",
		},
	})
	detail, err := a.FetchOrderDetail(ctx, "cid", "o-fallback", "item-1", "buyer-1", "old-cookie")
	if err != nil {
		t.Fatalf("浏览器兜底应成功: %v", err)
	}
	if detail.SpecValue != "30天" || detail.Amount != "19.8" {
		t.Fatalf("detail=%+v", detail)
	}
	// UpdatedCookies 与入参不同时应保存。
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved != "unb=1; _m_h5_tk=newtoken;" {
		t.Fatalf("刷新的 cookie 未保存: %q", saved)
	}
}

// TestOnPasswordLoginRefresh_BrowserNilStillUsesAPIRenew 浏览器未启用时仍先尝试接口轻量续期。
func TestOnPasswordLoginRefresh_BrowserNilStillUsesAPIRenew(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	renewSvc, closeRenew := verifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	if !a.OnPasswordLoginRefresh(context.Background(), "cid") {
		t.Fatal("browser=nil 但接口续期成功时应返回 true")
	}
	saved, _ := store.Cookies.GetValue(context.Background(), "cid")
	if !strings.Contains(saved, "havana_lgc2_77=verified") {
		t.Fatalf("接口续期 cookie 未保存: %q", saved)
	}
}

// TestOnPasswordLoginRefresh_BrowserNilReturnsFalseAfterAPIFailure 接口轻量续期也失败后才因浏览器不可用失败。
func TestOnPasswordLoginRefresh_BrowserNilReturnsFalseAfterAPIFailure(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	renewSvc, closeRenew := unverifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	if a.OnPasswordLoginRefresh(context.Background(), "cid") {
		t.Fatal("browser=nil 且接口续期失败时应返回 false")
	}
	logs, err := store.LoginLogs.ListByCookie(context.Background(), "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "browser_disabled" {
		t.Fatalf("浏览器不可用应记录 browser_disabled: logs=%#v err=%v", logs, err)
	}
}

// TestOnPasswordLoginRefresh_NoCredentialsReturnsFalse 有浏览器但账号未配密码时返回 false 并停用账号。
func TestOnPasswordLoginRefresh_NoCredentialsReturnsFalse(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	a := New(store, nil, nil)
	renewSvc, closeRenew := unverifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	a.SetBrowser(&fakeBrowser{renewErr: errors.New("quick enter unavailable")})
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("账号未配用户名/密码应返回 false")
	}
	if store.Cookies.GetStatus(ctx, "cid") {
		t.Fatal("未配置账号密码应自动停用账号，避免持续重试")
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].Status != "no_credentials" || logs[0].FailureReason != "no_credentials" {
		t.Fatalf("无凭据应记录 no_credentials 日志: logs=%#v err=%v", logs, err)
	}
}

// TestOnPasswordLoginRefresh_BrowserRenewSuccess 无账密时旧 Cookie 仍可快速续期，应保存新 Cookie 并跳过密码登录。
func TestOnPasswordLoginRefresh_BrowserRenewSuccess(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Tokens.Save(ctx, "cid", "did-old", "tok-old", 9999999999); err != nil {
		t.Fatalf("保存旧 token: %v", err)
	}

	fb := &fakeBrowser{renewCookies: map[string]string{"unb": "1", "_m_h5_tk": "renewed"}}
	a := New(store, nil, nil)
	renewSvc, closeRenew := verifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	a.SetBrowser(fb)
	if !a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("快速续期成功应返回 true")
	}
	if fb.renewCalls != 1 {
		t.Fatalf("CookieRenew 应调用一次，got %d", fb.renewCalls)
	}
	if fb.loginCalls != 0 {
		t.Fatalf("快速续期成功后不应密码登录，got %d", fb.loginCalls)
	}
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if !strings.Contains(saved, "_m_h5_tk=renewed") || !strings.Contains(saved, "havana_lgc2_77=verified") {
		t.Fatalf("快速续期 cookie 未保存: %q", saved)
	}
	if _, err := store.Tokens.Get(ctx, "cid"); err != db.ErrNotFound {
		t.Fatalf("Cookie 更新后应清除旧 token，got %v", err)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].Status != "success" {
		t.Fatalf("快速续期应记录登录日志: logs=%#v err=%v", logs, err)
	}
}

// TestOnPasswordLoginRefresh_Success 配好凭据后浏览器登录成功，cookie 被保存。
func TestOnPasswordLoginRefresh_Success(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	// 配置账号用户名/密码。
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{renewErr: errors.New("quick enter unavailable"), loginCookies: map[string]string{"unb": "1", "_m_h5_tk": "fresh"}}
	a := New(store, nil, nil)
	renewSvc, closeRenew := unverifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	a.SetBrowser(fb)
	if !a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("登录成功应返回 true")
	}
	if fb.loginCalls != 1 {
		t.Fatalf("PasswordLogin 应调用一次，got %d", fb.loginCalls)
	}
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved == "" {
		t.Fatal("刷新的 cookie 应已保存")
	}
	d, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "password" || d.LastLoginAt == 0 {
		t.Fatalf("密码登录成功后应标记登录审计字段: %+v", d)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].Status != "success" {
		t.Fatalf("密码登录成功应记录日志: logs=%#v err=%v", logs, err)
	}
}

// TestOnPasswordLoginRefresh_LoginError 浏览器登录返回错误时返回 false 且不保存 cookie。
func TestOnPasswordLoginRefresh_LoginError(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{renewErr: errors.New("quick enter unavailable"), loginErr: errors.New("captcha required")}
	a := New(store, nil, nil)
	renewSvc, closeRenew := unverifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	a.SetBrowser(fb)
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("登录失败应返回 false")
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].Status != "failed" || logs[0].FailureReason != "verification_required" {
		t.Fatalf("密码登录失败应记录日志: logs=%#v err=%v", logs, err)
	}
}

func TestOnPasswordLoginRefresh_BaxiaFailureReason(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{renewErr: errors.New("quick enter unavailable"), loginErr: errors.New("baxia-punish 风控图形验证")}
	a := New(store, nil, nil)
	renewSvc, closeRenew := unverifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	a.SetBrowser(fb)
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("风控失败应返回 false")
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "baxia_punish_captcha" {
		t.Fatalf("baxia 风控应记录专用 failure_reason: logs=%#v err=%v", logs, err)
	}
	if !store.Cookies.GetStatus(ctx, "cid") {
		t.Fatal("baxia 风控只应冷却，不应停用账号")
	}
}

func TestOnPasswordLoginRefresh_DisablesFrozenAccountError(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{renewErr: errors.New("quick enter unavailable"), loginErr: errors.New("账号已被冻结")}
	a := New(store, nil, nil)
	renewSvc, closeRenew := unverifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	a.SetBrowser(fb)
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("冻结账号登录失败应返回 false")
	}
	if store.Cookies.GetStatus(ctx, "cid") {
		t.Fatal("冻结账号登录错误应停用账号")
	}
}

func TestPasswordLoginProcessingLock(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	if !a.beginPasswordLogin("cid") {
		t.Fatal("首次获取 processing 锁应成功")
	}
	if a.beginPasswordLogin("cid") {
		t.Fatal("同账号重复获取 processing 锁应失败")
	}
	a.finishPasswordLogin("cid")
	if !a.beginPasswordLogin("cid") {
		t.Fatal("释放后应可再次获取 processing 锁")
	}
}

// TestOnPasswordLoginRefresh_Cooldown 同一账号短时间内第二次刷新被冷却拒绝。
func TestOnPasswordLoginRefresh_Cooldown(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{renewErr: errors.New("quick enter unavailable"), loginCookies: map[string]string{"unb": "1"}}
	a := New(store, nil, nil)
	renewSvc, closeRenew := unverifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	a.SetBrowser(fb)
	if !a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("首次应成功")
	}
	// 第二次在冷却期内，应被拒绝且不调用浏览器。
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("冷却期内应返回 false")
	}
	if fb.loginCalls != 1 {
		t.Fatalf("冷却期内不应调用浏览器，got calls=%d", fb.loginCalls)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 2 || logs[0].Status != "skipped_cooldown" || logs[0].FailureReason != "login_cooldown" {
		t.Fatalf("冷却拒绝应记录 skipped_cooldown 日志: logs=%#v err=%v", logs, err)
	}
}
