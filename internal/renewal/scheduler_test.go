package renewal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	apirenew "xianyu-go/internal/xianyu/renew"
)

func newSchedulerTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "renewal.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return db.NewStore(d, db.DialectSQLite), func() { d.Close() }
}

type schedulerFakeBrowser struct {
	quickCookies string
	quickErr     error
	quickCalls   int
	quickInputs  []string
}

type schedulerRefreshBrowser struct {
	refreshCalls int
}

func (f *schedulerRefreshBrowser) BrowserQuickRenew(context.Context, string, string, bool) (string, error) {
	return "", errors.New("not implemented")
}

func (f *schedulerRefreshBrowser) CookiesRefreshSnapshot(_ context.Context, _ string, cookieStr string, snapshot []cookierefresh.BrowserCookie, _ bool) (string, []cookierefresh.BrowserCookie, error) {
	f.refreshCalls++
	return cookieStr, snapshot, nil
}

func (f *schedulerFakeBrowser) BrowserQuickRenew(_ context.Context, _ string, cookieStr string, _ bool) (string, error) {
	f.quickCalls++
	f.quickInputs = append(f.quickInputs, cookieStr)
	return f.quickCookies, f.quickErr
}

func (f *schedulerFakeBrowser) CookiesRefreshSnapshot(_ context.Context, _, _ string, _ []cookierefresh.BrowserCookie, _ bool) (string, []cookierefresh.BrowserCookie, error) {
	return "", nil, errors.New("not implemented")
}

type schedulerFakePasswordRefresher struct {
	calls atomic.Int32
	ch    chan string
}

func (f *schedulerFakePasswordRefresher) OnPasswordLoginRefresh(_ context.Context, cookieID string) bool {
	f.calls.Add(1)
	if f.ch != nil {
		select {
		case f.ch <- cookieID:
		default:
		}
	}
	return true
}

func (f *schedulerFakePasswordRefresher) waitCookie(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-f.ch:
		if got != want {
			t.Fatalf("password refresher cookieID=%q want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("password refresher 未触发")
	}
}

type apiRenewLogSnapshot struct {
	status             string
	message            string
	errorMessage       string
	updatedCookieNames string
	responseContent    string
	stepDetails        string
	renewMethod        string
	durationMS         int64
	requestCount       int
}

func createSchedulerAccount(t *testing.T, store *db.Store, cookieID, cookieValue string) db.RenewalAccount {
	t.Helper()
	ctx := context.Background()
	username := "user_" + strings.ReplaceAll(cookieID, "-", "_")
	if ok, err := store.Users.Create(ctx, username, username+"@example.com", "pw"); err != nil || !ok {
		t.Fatalf("Create user: ok=%v err=%v", ok, err)
	}
	user, err := store.Users.GetByUsername(ctx, username)
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if err := store.Cookies.Save(ctx, cookieID, cookieValue, user.ID); err != nil {
		t.Fatalf("Save cookie: %v", err)
	}
	return db.RenewalAccount{ID: cookieID, Value: cookieValue, UserID: user.ID, Enabled: true}
}

func lastAPIRenewLog(t *testing.T, store *db.Store, cookieID string) apiRenewLogSnapshot {
	t.Helper()
	var log apiRenewLogSnapshot
	err := store.DB.QueryRowContext(context.Background(),
		`SELECT status, COALESCE(message,''), COALESCE(error_message,''), COALESCE(updated_cookie_names,''),
		        COALESCE(response_content,''), COALESCE(step_details,''), COALESCE(renew_method,''),
		        COALESCE(duration_ms,0), COALESCE(request_count,0)
		 FROM scheduled_api_cookie_renew_log
		 WHERE cookie_id=?
		 ORDER BY id DESC LIMIT 1`, cookieID).
		Scan(&log.status, &log.message, &log.errorMessage, &log.updatedCookieNames, &log.responseContent,
			&log.stepDetails, &log.renewMethod, &log.durationMS, &log.requestCount)
	if err != nil {
		t.Fatalf("query api renew log: %v", err)
	}
	return log
}

func schedulerRenewServiceFromServer(srv *httptest.Server) apirenew.Service {
	return apirenew.Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}
}

func TestSchedulerIntervalsAligned(t *testing.T) {
	if loginRenewInterval != 10*time.Minute {
		t.Fatalf("loginRenewInterval=%s want 10m", loginRenewInterval)
	}
	if cookiesRefreshInterval != 10*time.Minute {
		t.Fatalf("cookiesRefreshInterval=%s want 10m", cookiesRefreshInterval)
	}
	if apiCookieRenewInterval != time.Hour {
		t.Fatalf("apiCookieRenewInterval=%s want 1h", apiCookieRenewInterval)
	}
}

func TestSchedulerDefaultsMatchUpstreamConfig(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	s := NewScheduler(store, nil, nil, nil, nil)

	if s.settingEnabled(ctx, loginRenewEnabledSetting, false) {
		t.Fatal("login_renew 未配置时应默认关闭")
	}
	if s.settingEnabled(ctx, cookiesRefreshEnabledSetting, false) {
		t.Fatal("cookies_refresh 未配置时应默认关闭")
	}
	if !s.settingEnabled(ctx, apiCookieRenewEnabledSetting, true) {
		t.Fatal("api_cookie_renew 未配置时应默认开启")
	}
}

func TestLoginRenewPreservesValidTokenCache(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-login-renew", "unb=1; _m_h5_tk=old_1")
	if err := store.Tokens.Save(ctx, account.ID, "did-stable", "cached-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "new_1"})
		_, _ = w.Write([]byte(`{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`))
	}))
	defer srv.Close()

	s := NewScheduler(store, nil, nil, nil, nil)
	s.mtop = &mtop.ClientImpl{HTTPClient: srv.Client(), LoginUserURL: srv.URL}
	s.loginRenewOne(ctx, "batch-login-renew", account)

	token, err := store.Tokens.Get(ctx, account.ID)
	if err != nil || token.AccessToken != "cached-token" {
		t.Fatalf("login_renew 不得删除有效 token 缓存: token=%+v err=%v", token, err)
	}
	updated, err := store.Cookies.GetValue(ctx, account.ID)
	if err != nil || !strings.Contains(updated, "_m_h5_tk=new_1") {
		t.Fatalf("login_renew Cookie 未保存: %q err=%v", updated, err)
	}
}

func TestBrowserCookieRefreshSkipsManuallyDisabledAccount(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-manual-disabled", "unb=1; cookie2=c2")
	if err := store.Cookies.SetStatusWithReason(ctx, account.ID, false, db.DisableReasonManual); err != nil {
		t.Fatal(err)
	}
	accounts, err := store.Cookies.AllRenewalAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	if accounts[0].DisableReason != db.DisableReasonManual {
		t.Fatalf("disable reason=%q", accounts[0].DisableReason)
	}
	browser := &schedulerRefreshBrowser{}
	s := NewScheduler(store, nil, browser, nil, nil)
	s.executeBrowserCookieRefresh(ctx)
	if browser.refreshCalls != 0 {
		t.Fatalf("manual disabled account refreshed %d times", browser.refreshCalls)
	}
	if store.Cookies.GetStatus(ctx, account.ID) {
		t.Fatal("manual disabled account must remain disabled")
	}
}

func TestNewSchedulerTreatsTypedNilBrowserAsDisabled(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-typed-nil-browser", "unb=1; cookie2=c2")
	refresher := &schedulerFakePasswordRefresher{ch: make(chan string, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do", "/silentHasLogin.do", "/setLoginSettings.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var typedNilBrowser *schedulerFakeBrowser
	s := NewScheduler(store, nil, typedNilBrowser, refresher, nil)
	if s.browser != nil {
		t.Fatal("typed nil BrowserRenewer 应被规范化为 nil")
	}
	s.api = schedulerRenewServiceFromServer(srv)
	s.cooldown = NewCooldownManager()

	s.apiCookieRenewOne(ctx, "batch-typed-nil-browser", account)
	refresher.waitCookie(t, "cid-typed-nil-browser")
	log := lastAPIRenewLog(t, store, "cid-typed-nil-browser")
	if log.status != "need_password_login" || !strings.Contains(log.errorMessage, "浏览器自动化未启用") {
		t.Fatalf("typed nil browser 应按浏览器禁用处理: %+v", log)
	}
}

func TestSchedulerSettingEnabledOverrides(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	s := NewScheduler(store, nil, nil, nil, nil)

	if err := store.Settings.Set(ctx, loginRenewEnabledSetting, "enabled"); err != nil {
		t.Fatalf("Set enabled: %v", err)
	}
	if !s.settingEnabled(ctx, loginRenewEnabledSetting, false) {
		t.Fatal("enabled 应开启任务")
	}
	if err := store.Settings.Set(ctx, loginRenewEnabledSetting, "off"); err != nil {
		t.Fatalf("Set off: %v", err)
	}
	if s.settingEnabled(ctx, loginRenewEnabledSetting, true) {
		t.Fatal("off 应关闭任务")
	}
}

func TestSchedulerSettingIntervalOverrides(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	s := NewScheduler(store, nil, nil, nil, nil)

	if got := s.settingInterval(ctx, loginRenewIntervalSetting, loginRenewInterval); got != loginRenewInterval {
		t.Fatalf("未配置间隔应返回默认值: %s", got)
	}
	if err := store.Settings.Set(ctx, loginRenewIntervalSetting, "30"); err != nil {
		t.Fatalf("Set seconds: %v", err)
	}
	if got := s.settingInterval(ctx, loginRenewIntervalSetting, loginRenewInterval); got != 30*time.Second {
		t.Fatalf("秒数间隔=%s want 30s", got)
	}
	if err := store.Settings.Set(ctx, loginRenewIntervalSetting, "2m"); err != nil {
		t.Fatalf("Set duration: %v", err)
	}
	if got := s.settingInterval(ctx, loginRenewIntervalSetting, loginRenewInterval); got != 2*time.Minute {
		t.Fatalf("duration 间隔=%s want 2m", got)
	}
	if err := store.Settings.Set(ctx, loginRenewIntervalSetting, "-1"); err != nil {
		t.Fatalf("Set invalid: %v", err)
	}
	if got := s.settingInterval(ctx, loginRenewIntervalSetting, loginRenewInterval); got != loginRenewInterval {
		t.Fatalf("非法间隔应回退默认值: %s", got)
	}
}

func TestSchedulerSettingIntOverrides(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	s := NewScheduler(store, nil, nil, nil, nil)

	if got := s.settingInt(ctx, "missing_int", 10); got != 10 {
		t.Fatalf("missing setting=%d want 10", got)
	}
	if err := store.Settings.Set(ctx, "renewal_log_retention_days", "20"); err != nil {
		t.Fatalf("Set int: %v", err)
	}
	if got := s.settingInt(ctx, "renewal_log_retention_days", 10); got != 20 {
		t.Fatalf("setting int=%d want 20", got)
	}
	if err := store.Settings.Set(ctx, "renewal_log_retention_days", "-1"); err != nil {
		t.Fatalf("Set invalid: %v", err)
	}
	if got := s.settingInt(ctx, "renewal_log_retention_days", 10); got != 10 {
		t.Fatalf("invalid setting int=%d want 10", got)
	}
}

func TestRenewalCleanupLogsUsesRetentionDays(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	cookie := createSchedulerAccount(t, store, "cid-cleanup", "unb=1")
	_ = cookie

	for _, table := range []string{
		"scheduled_cookies_refresh_log",
		"scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log",
	} {
		if _, err := store.DB.ExecContext(ctx,
			`INSERT INTO `+table+` (batch_id, cookie_id, status, created_at) VALUES ('old', 'cid-cleanup', 'failed', datetime('now','-20 days'))`); err != nil {
			t.Fatalf("insert old %s: %v", table, err)
		}
		if _, err := store.DB.ExecContext(ctx,
			`INSERT INTO `+table+` (batch_id, cookie_id, status, created_at) VALUES ('new', 'cid-cleanup', 'success', CURRENT_TIMESTAMP)`); err != nil {
			t.Fatalf("insert new %s: %v", table, err)
		}
	}
	if err := store.Settings.Set(ctx, "renewal_log_retention_days", "10"); err != nil {
		t.Fatalf("set retention: %v", err)
	}
	s := NewScheduler(store, nil, nil, nil, nil)
	s.cleanupExpiredLogs(ctx)
	for _, table := range []string{
		"scheduled_cookies_refresh_log",
		"scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log",
	} {
		var oldRows, newRows int
		if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE batch_id='old'`).Scan(&oldRows); err != nil {
			t.Fatalf("count old %s: %v", table, err)
		}
		if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE batch_id='new'`).Scan(&newRows); err != nil {
			t.Fatalf("count new %s: %v", table, err)
		}
		if oldRows != 0 || newRows != 1 {
			t.Fatalf("%s cleanup old=%d new=%d, want old=0 new=1", table, oldRows, newRows)
		}
	}
}

func TestAPICookieRenewOneSavesPartialCookiesBeforePasswordLogin(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-partial", "unb=1; cookie2=c2")
	refresher := &schedulerFakePasswordRefresher{ch: make(chan string, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "sgcookie", Value: "s1"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tk_1"})
			_, _ = w.Write([]byte(`{"content":{"success":true},"marker":"silent-partial"}`))
		case "/setLoginSettings.do":
			_, _ = w.Write([]byte(`{"content":{"success":false}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := NewScheduler(store, nil, nil, refresher, nil)
	s.api = schedulerRenewServiceFromServer(srv)
	s.cooldown = NewCooldownManager()
	s.apiCookieRenewOne(ctx, "batch-partial", account)
	refresher.waitCookie(t, "cid-partial")

	got, err := store.Cookies.GetValue(ctx, "cid-partial")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	for _, want := range []string{"sgcookie=s1", "_m_h5_tk=tk_1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("部分 Cookie 未保存，缺少 %s: %q", want, got)
		}
	}
	log := lastAPIRenewLog(t, store, "cid-partial")
	if log.status != "need_password_login" {
		t.Fatalf("status=%q want need_password_login, log=%+v", log.status, log)
	}
	if !strings.Contains(log.updatedCookieNames, "sgcookie") || !strings.Contains(log.updatedCookieNames, "_m_h5_tk") {
		t.Fatalf("updated_cookie_names 未记录部分更新: %+v", log)
	}
	if !strings.Contains(log.responseContent, "silent-partial") {
		t.Fatalf("response_content 应记录 silentHasLogin 响应: %+v", log)
	}
}

func TestAPICookieRenewOneSavesBrowserCookiesWhenVerifyFails(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-browser-fail", "unb=1; cookie2=c2")
	browser := &schedulerFakeBrowser{quickCookies: "unb=1; cookie2=c2; sgcookie=s1; _m_h5_tk=tk_1; browser=b1"}
	refresher := &schedulerFakePasswordRefresher{ch: make(chan string, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "sgcookie", Value: "s1"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tk_1"})
			_, _ = w.Write([]byte(`{"content":{"success":true},"marker":"silent-browser-fail"}`))
		case "/setLoginSettings.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := NewScheduler(store, nil, browser, refresher, nil)
	s.api = schedulerRenewServiceFromServer(srv)
	s.cooldown = NewCooldownManager()
	s.apiCookieRenewOne(ctx, "batch-browser-fail", account)
	refresher.waitCookie(t, "cid-browser-fail")

	if browser.quickCalls != 1 {
		t.Fatalf("BrowserQuickRenew calls=%d want 1", browser.quickCalls)
	}
	if len(browser.quickInputs) != 1 || !strings.Contains(browser.quickInputs[0], "sgcookie=s1") || !strings.Contains(browser.quickInputs[0], "_m_h5_tk=tk_1") {
		t.Fatalf("浏览器续期输入未使用接口部分更新: %#v", browser.quickInputs)
	}
	got, err := store.Cookies.GetValue(ctx, "cid-browser-fail")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	for _, want := range []string{"sgcookie=s1", "_m_h5_tk=tk_1", "browser=b1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("浏览器 Cookie 未保存，缺少 %s: %q", want, got)
		}
	}
	log := lastAPIRenewLog(t, store, "cid-browser-fail")
	if log.status != "need_password_login" {
		t.Fatalf("status=%q want need_password_login, log=%+v", log.status, log)
	}
	if !strings.Contains(log.updatedCookieNames, "browser") {
		t.Fatalf("updated_cookie_names 未记录浏览器更新: %+v", log)
	}
}

func TestAPICookieRenewOneBrowserRenewedAfterVerifySuccess(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-browser-ok", "unb=1; cookie2=c2")
	if err := store.Tokens.Save(ctx, account.ID, "did-stable", "cached-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("Save token: %v", err)
	}
	browser := &schedulerFakeBrowser{quickCookies: "unb=1; cookie2=c2; browser=b1"}
	var setLoginCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			_, _ = w.Write([]byte(`{"content":{"success":true},"marker":"silent-browser-ok"}`))
		case "/setLoginSettings.do":
			if setLoginCalls.Add(1) == 2 {
				http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "lgc"})
			}
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := NewScheduler(store, nil, browser, nil, nil)
	s.api = schedulerRenewServiceFromServer(srv)
	s.cooldown = NewCooldownManager()
	s.apiCookieRenewOne(ctx, "batch-browser-ok", account)
	if token, err := store.Tokens.Get(ctx, account.ID); err != nil || token.AccessToken != "cached-token" {
		t.Fatalf("定时续期不得删除有效 token 缓存: token=%+v err=%v", token, err)
	}

	if browser.quickCalls != 1 || setLoginCalls.Load() != 2 {
		t.Fatalf("browser calls=%d setLoginCalls=%d", browser.quickCalls, setLoginCalls.Load())
	}
	got, err := store.Cookies.GetValue(ctx, "cid-browser-ok")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	for _, want := range []string{"browser=b1", "havana_lgc2_77=lgc"} {
		if !strings.Contains(got, want) {
			t.Fatalf("最终 Cookie 缺少 %s: %q", want, got)
		}
	}
	log := lastAPIRenewLog(t, store, "cid-browser-ok")
	if log.status != "browser_renewed" {
		t.Fatalf("status=%q want browser_renewed, log=%+v", log.status, log)
	}
	if !strings.Contains(log.responseContent, "silent-browser-ok") {
		t.Fatalf("response_content 应记录验证阶段 silentHasLogin 响应: %+v", log)
	}
	if log.renewMethod != "browser+api" || log.requestCount != 6 {
		t.Fatalf("浏览器兜底日志应记录方法和请求数: %+v", log)
	}
	if !strings.Contains(log.stepDetails, "browser: quick_enter") || !strings.Contains(log.stepDetails, "api_verify") {
		t.Fatalf("step_details 未记录浏览器兜底步骤: %+v", log)
	}
	if log.durationMS < 0 {
		t.Fatalf("duration_ms 不应为负数: %+v", log)
	}
}
