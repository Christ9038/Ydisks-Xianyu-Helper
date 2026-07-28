package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// fakeNotifier 记录告警调用，供 OnAccountAlert 断言。
type fakeNotifier struct {
	alerts []struct{ cookieID, level, title, body string }
	events []struct{ cookieID, eventType, level, title, body string }
}

func (f *fakeNotifier) NotifyAccountAlert(cookieID, level, title, body string) {
	f.alerts = append(f.alerts, struct{ cookieID, level, title, body string }{cookieID, level, title, body})
}

func (f *fakeNotifier) NotifyAccountEvent(cookieID, eventType, level, title, body string) {
	f.events = append(f.events, struct{ cookieID, eventType, level, title, body string }{cookieID, eventType, level, title, body})
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

	tokenCaptchaResult   string
	tokenCaptchaErr      error
	tokenCaptchaCalls    int
	tokenCaptchaURL      string
	tokenCaptchaHeadless bool
	providerURL          string
	providerTokenOK      bool
	providerUpdated      string
}

type fakeSnapshotBrowser struct {
	fakeBrowser
	snapshotCookies string
	snapshot        []cookierefresh.BrowserCookie
	snapshotErr     error
	snapshotCalls   int
}

func (f *fakeSnapshotBrowser) TokenCaptchaCookieSnapshot(context.Context, string, bool) (string, []cookierefresh.BrowserCookie, error) {
	f.snapshotCalls++
	return f.snapshotCookies, f.snapshot, f.snapshotErr
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
func (f *fakeBrowser) TokenCaptchaRecover(ctx context.Context, _ string, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (string, error) {
	f.tokenCaptchaCalls++
	f.tokenCaptchaURL = verificationURL
	f.tokenCaptchaHeadless = headless
	if provider != nil {
		f.providerURL, f.providerTokenOK, f.providerUpdated, _ = provider(ctx, cookieStr)
	}
	return f.tokenCaptchaResult, f.tokenCaptchaErr
}

type fakeCaptchaRequester struct {
	result      *mtop.FreshCaptchaResult
	err         error
	calls       int
	gotCookies  string
	gotDeviceID string
}

type fakeOrderDetailClient struct {
	detail *mtop.OrderDetailResult
	err    error
	calls  int
}

func (f *fakeOrderDetailClient) FetchOrderDetail(_ context.Context, _, _ string) (*mtop.OrderDetailResult, error) {
	f.calls++
	return f.detail, f.err
}

func (f *fakeCaptchaRequester) RequestFreshCaptchaURLContext(_ context.Context, cookiesStr, deviceID string) (*mtop.FreshCaptchaResult, error) {
	f.calls++
	f.gotCookies = cookiesStr
	f.gotDeviceID = deviceID
	return f.result, f.err
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
		case "/hasLogin.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "verified"})
			_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
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
	if len(n.events) != 1 || n.events[0].cookieID != "cid" || n.events[0].title != "token 失效" || n.events[0].eventType != engine.EventTokenRenewal {
		t.Fatalf("告警未转发为事件通知: events=%+v alerts=%+v", n.events, n.alerts)
	}
	a.OnAccountAlert(context.Background(), "cid", "warn", "闲鱼要求滑块验证", "请完成 captcha")
	if len(n.events) != 2 || n.events[1].eventType != engine.EventSecurityVerification {
		t.Fatalf("风控告警应分类为 security_verification: events=%+v alerts=%+v", n.events, n.alerts)
	}
}

func TestOnTokenCaptchaVerification_SavesCookiesAndRiskLog(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()

	fb := &fakeBrowser{tokenCaptchaResult: "unb=1; _m_h5_tk=fresh; x5sec=ok;"}
	req := &fakeCaptchaRequester{result: &mtop.FreshCaptchaResult{
		VerificationURL: "https://fresh.example/captcha",
		UpdatedCookies:  "unb=1; _m_h5_tk=fresh;",
		AccessToken:     "fresh-access-token",
		TokenOK:         true,
	}}
	notifier := &fakeNotifier{}
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	a.SetTokenCaptchaRequester(req)
	a.SetNotifier(notifier)

	const deviceID = "device-for-token-and-captcha"
	result, ok := a.OnTokenCaptchaVerification(ctx, "cid", "unb=1; _m_h5_tk=tk;", "https://old.example/captcha", deviceID)
	if !ok {
		t.Fatal("token captcha recovery should succeed")
	}
	if result == nil || !strings.Contains(result.UpdatedCookies, "x5sec=ok") {
		t.Fatalf("returned cookies should contain x5sec: %+v", result)
	}
	if result.AccessToken != "" {
		t.Fatalf("参考流程不得直接采用重取链接时返回的 token: %+v", result)
	}
	if fb.tokenCaptchaCalls != 1 || fb.tokenCaptchaURL != "https://old.example/captcha" {
		t.Fatalf("browser captcha call mismatch: calls=%d url=%q", fb.tokenCaptchaCalls, fb.tokenCaptchaURL)
	}
	if fb.providerURL != "https://fresh.example/captcha" || fb.providerUpdated == "" {
		t.Fatalf("provider result not passed through: url=%q updated=%q", fb.providerURL, fb.providerUpdated)
	}
	if req.calls != 1 || req.gotDeviceID != deviceID {
		t.Fatalf("fresh captcha requester not called correctly: calls=%d device=%q", req.calls, req.gotDeviceID)
	}
	saved, err := store.Cookies.GetValue(ctx, "cid")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if !strings.Contains(saved, "x5sec=ok") {
		t.Fatalf("saved cookies should contain x5sec: %q", saved)
	}
	var status, engineName string
	if err := store.DB.QueryRowContext(ctx,
		`SELECT processing_status, captcha_engine FROM risk_control_logs WHERE cookie_id='cid' ORDER BY id DESC LIMIT 1`).
		Scan(&status, &engineName); err != nil {
		t.Fatalf("query risk log: %v", err)
	}
	if status != "success" || engineName != "playwright" {
		t.Fatalf("risk log status=%q engine=%q", status, engineName)
	}
	if len(notifier.events) != 1 || notifier.events[0].eventType != engine.EventSecurityVerification || notifier.events[0].level != engine.AlertLevelInfo {
		t.Fatalf("security recovery notification missing: %+v", notifier.events)
	}
}

func TestOnTokenCaptchaVerificationPersistsExactBrowserJar(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	browserSnapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true, HTTPOnly: true},
		{Name: "x5sec", Value: "fresh", Domain: ".goofish.com", Path: "/", Secure: true, HTTPOnly: true, Expires: float64(time.Now().Add(time.Hour).Unix())},
	}
	fb := &fakeSnapshotBrowser{
		fakeBrowser:     fakeBrowser{tokenCaptchaResult: "unb=1; x5sec=fresh"},
		snapshotCookies: "unb=1; x5sec=fresh",
		snapshot:        browserSnapshot,
	}
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	a.SetTokenCaptchaRequester(&fakeCaptchaRequester{})
	result, ok := a.OnTokenCaptchaVerification(ctx, "cid", "unb=1", "https://passport.goofish.com/punish", "device")
	if !ok || result == nil || !result.CookieSnapshotComplete || fb.snapshotCalls != 1 {
		t.Fatalf("result=%+v ok=%v snapshot_calls=%d", result, ok, fb.snapshotCalls)
	}
	detail, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	saved, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !complete || len(saved) != 2 || detail.Value != "unb=1; x5sec=fresh" {
		t.Fatalf("滑块完整 Jar 未保存: value=%q complete=%v snapshot=%+v", detail.Value, complete, saved)
	}
	var exactX5 bool
	for _, cookie := range saved {
		if cookie.Name == "x5sec" && cookie.HTTPOnly && cookie.Secure && cookie.Expires > 0 {
			exactX5 = true
		}
	}
	if !exactX5 {
		t.Fatalf("x5sec 属性被扁平化: %+v", saved)
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

// TestFetchOrderDetail_MTopNilReturnsError 本地缺字段且 MTOP 客户端未配置时返回明确错误。
func TestFetchOrderDetail_MTopNilReturnsError(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	a.SetOrderDetailClient(nil)
	_, err := a.FetchOrderDetail(context.Background(), "cid", "o-missing", "item-1", "buyer-1", "cookie")
	if err == nil {
		t.Fatal("MTOP=nil 且本地缺失应返回错误")
	}
}

// TestFetchOrderDetail_GoMTop 本地缺失时调用 Go MTOP 并保存响应 Cookie。
func TestFetchOrderDetail_GoMTop(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	a := New(store, nil, nil)
	a.SetOrderDetailClient(&fakeOrderDetailClient{
		detail: &mtop.OrderDetailResult{
			Quantity: "2", SpecName: "套餐", SpecValue: "30天", Amount: "19.8",
			UpdatedCookies: "unb=1; _m_h5_tk=newtoken;",
		},
	})
	detail, err := a.FetchOrderDetail(ctx, "cid", "o-fallback", "item-1", "buyer-1", "old-cookie")
	if err != nil {
		t.Fatalf("Go MTOP 应成功: %v", err)
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

func TestFetchOrderDetail_PersistsFlatGoMTopCookie(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	a := New(store, nil, nil)
	a.SetOrderDetailClient(&fakeOrderDetailClient{detail: &mtop.OrderDetailResult{
		Quantity: "1", Amount: "9.9", UpdatedCookies: "unb=1; api_only=scoped",
	}})
	if _, err := a.FetchOrderDetail(ctx, "cid", "o-jar", "item-1", "buyer-1", "old-cookie"); err != nil {
		t.Fatal(err)
	}
	detail, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if complete || detail.Value != "unb=1; api_only=scoped" {
		t.Fatalf("Go MTOP flat cookie not persisted: value=%q complete=%v", detail.Value, complete)
	}
}

// TestOnPasswordLoginRefresh_BrowserNilStillUsesAPIRenew 浏览器未启用时仍先尝试接口轻量续期。
func TestOnPasswordLoginRefresh_BrowserNilStillUsesAPIRenew(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	if err := store.Cookies.UpdateValueExisting(context.Background(), "cid", "unb=1; _m_h5_tk=tk; havana_lgc2_77=lgc; havana_lgc_exp=9999999999999"); err != nil {
		t.Fatal(err)
	}
	renewSvc, closeRenew := verifiedRenewService(t)
	defer closeRenew()
	a.SetRenewService(renewSvc)
	if !a.OnPasswordLoginRefresh(context.Background(), "cid") {
		t.Fatal("browser=nil 但接口续期成功时应返回 true")
	}
	if !a.OnPasswordLoginRefresh(context.Background(), "cid") {
		t.Fatal("轻量续期成功不应启动密码登录冷却")
	}
	saved, _ := store.Cookies.GetValue(context.Background(), "cid")
	if !strings.Contains(saved, "havana_lgc2_77=verified") {
		t.Fatalf("接口续期 cookie 未保存: %q", saved)
	}
}

func TestOnPasswordLoginRefreshWaitsForPendingFinalResult(t *testing.T) {
	for _, tt := range []struct {
		name       string
		body       string
		want       bool
		wantCookie bool
	}{
		{name: "late success", body: `{"content":{"data":{"processFinished":true,"resultCode":100}}}`, want: true, wantCookie: true},
		{name: "late business failure", body: `{"content":{"data":{"processFinished":true,"resultCode":500}}}`, want: false, wantCookie: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, cleanup := newAdapterTestStore(t)
			defer cleanup()
			renewal.GlobalCooldown.Reset("cid")
			if err := store.Cookies.UpdateValueExisting(context.Background(), "cid", "unb=1; havana_lgc_exp=9999999999999"); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(40 * time.Millisecond)
				http.SetCookie(w, &http.Cookie{Name: "late_cookie", Value: "saved", Path: "/"})
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			a := New(store, nil, nil)
			a.SetRenewService(xrenew.Service{
				HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL,
				RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond,
			})
			if got := a.OnPasswordLoginRefresh(context.Background(), "cid"); got != tt.want {
				t.Fatalf("pending 最终恢复结果=%v want %v", got, tt.want)
			}
			saved, err := store.Cookies.GetValue(context.Background(), "cid")
			if err != nil || strings.Contains(saved, "late_cookie=saved") != tt.wantCookie {
				t.Fatalf("迟到 Cookie 保存异常: value=%q err=%v", saved, err)
			}
		})
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
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "qr_login_required" || logs[0].Method != "protocol" {
		t.Fatalf("协议续期失败应要求扫码: logs=%#v err=%v", logs, err)
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
	if !store.Cookies.GetStatus(ctx, "cid") {
		t.Fatal("Go 客户端不得因未配置密码停用账号")
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "qr_login_required" {
		t.Fatalf("应记录重新扫码日志: logs=%#v err=%v", logs, err)
	}
}

// TestOnPasswordLoginRefresh_DoesNotUseBrowserRenew 协议失败后不得调用浏览器续期。
func TestOnPasswordLoginRefresh_DoesNotUseBrowserRenew(t *testing.T) {
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
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("协议续期失败应返回 false")
	}
	if fb.renewCalls != 0 {
		t.Fatalf("不得调用浏览器 CookieRenew，got %d", fb.renewCalls)
	}
	if fb.loginCalls != 0 {
		t.Fatalf("快速续期成功后不应密码登录，got %d", fb.loginCalls)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "qr_login_required" {
		t.Fatalf("应记录重新扫码日志: logs=%#v err=%v", logs, err)
	}
}

// TestOnPasswordLoginRefresh_DoesNotUsePasswordLogin 配好密码也不得启动 Chromium 登录。
func TestOnPasswordLoginRefresh_DoesNotUsePasswordLogin(t *testing.T) {
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
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("协议续期失败应返回 false")
	}
	if fb.loginCalls != 0 {
		t.Fatalf("不得调用 Chromium PasswordLogin，got %d", fb.loginCalls)
	}
	d, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod == "password" {
		t.Fatalf("不得标记密码登录: %+v", d)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "qr_login_required" {
		t.Fatalf("应记录重新扫码日志: logs=%#v err=%v", logs, err)
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
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("验证失败后的账密错误冷却期应返回 false")
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 2 || logs[0].FailureReason != "qr_login_required" || fb.loginCalls != 0 {
		t.Fatalf("不得调用密码登录，应要求扫码: logs=%#v err=%v", logs, err)
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
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "qr_login_required" || fb.loginCalls != 0 {
		t.Fatalf("不得调用密码登录，应要求扫码: logs=%#v err=%v", logs, err)
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
	if !store.Cookies.GetStatus(ctx, "cid") || fb.loginCalls != 0 {
		t.Fatal("未执行密码登录时不得按浏览器页面错误停用账号")
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
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("协议续期失败应返回 false")
	}
	// 第二次在冷却期内，应被拒绝且不调用浏览器。
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("冷却期内应返回 false")
	}
	if fb.loginCalls != 0 {
		t.Fatalf("任何一次都不应调用浏览器，got calls=%d", fb.loginCalls)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil || len(logs) != 2 || logs[0].FailureReason != "qr_login_required" {
		t.Fatalf("协议失败应记录重新扫码日志: logs=%#v err=%v", logs, err)
	}
}
