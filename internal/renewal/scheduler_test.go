package renewal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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
}

func (f *schedulerFakePasswordRefresher) OnPasswordLoginRefresh(_ context.Context, _ string) bool {
	f.calls.Add(1)
	return true
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
	var typedNilBrowser *schedulerFakeBrowser
	if got := browserRenewerOrNil(typedNilBrowser); got != nil {
		t.Fatal("typed nil BrowserRenewer 应被规范化为 nil")
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

func TestAPICookieRenewOneSkipsExpiredLongLoginWithoutEscalation(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-expired", "unb=1; cookie2=c2")
	browser := &schedulerFakeBrowser{}
	refresher := &schedulerFakePasswordRefresher{}
	s := NewScheduler(store, nil, browser, refresher, nil)
	s.apiCookieRenewOne(ctx, "batch-expired", account)

	if browser.quickCalls != 0 || refresher.calls.Load() != 0 {
		t.Fatalf("proactive renewal escalated: browser=%d password=%d", browser.quickCalls, refresher.calls.Load())
	}
	log := lastAPIRenewLog(t, store, account.ID)
	if log.status != "skipped" || !strings.Contains(log.stepDetails, "long_login_expired") {
		t.Fatalf("expired long login log=%+v", log)
	}
}

func TestAPICookieRenewOneUsesSingleSilentRequestAndSavesCookies(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	expire := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	account := createSchedulerAccount(t, store, "cid-silent", "unb=1; cookie2=c2; havana_lgc_exp="+expire)
	browser := &schedulerFakeBrowser{}
	refresher := &schedulerFakePasswordRefresher{}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)})
		_, _ = w.Write([]byte(`{"content":{"success":true},"marker":"single-silent"}`))
	}))
	defer srv.Close()

	s := NewScheduler(store, nil, browser, refresher, nil)
	s.api = schedulerRenewServiceFromServer(srv)
	s.apiCookieRenewOne(ctx, "batch-silent", account)

	if requests.Load() != 1 || browser.quickCalls != 0 || refresher.calls.Load() != 0 {
		t.Fatalf("requests=%d browser=%d password=%d", requests.Load(), browser.quickCalls, refresher.calls.Load())
	}
	got, err := store.Cookies.GetValue(ctx, account.ID)
	if err != nil || !strings.Contains(got, "sdkSilent=") {
		t.Fatalf("silent Cookie not saved: value=%q err=%v", got, err)
	}
	log := lastAPIRenewLog(t, store, account.ID)
	if log.status != "cookie_updated" || log.requestCount != 1 || !strings.Contains(log.responseContent, "single-silent") {
		t.Fatalf("silent renewal log=%+v", log)
	}
}
