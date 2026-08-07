package renewal

import (
	"context"
	"errors"
	"io"
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

type schedulerRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f schedulerRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newSchedulerTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "renewal.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return db.NewStore(d, db.DialectSQLite), func() { d.Close() }
}

type schedulerFakeStarter struct {
	starts   atomic.Int32
	restarts atomic.Int32
}

func (f *schedulerFakeStarter) Start(context.Context, string, string) error {
	f.starts.Add(1)
	return nil
}

func (f *schedulerFakeStarter) Restart(context.Context, string) error {
	f.restarts.Add(1)
	return nil
}

type schedulerContextStarter struct {
	restarts atomic.Int32
	ctxAlive atomic.Bool
	err      error
}

func (f *schedulerContextStarter) Start(context.Context, string, string) error { return nil }

func (f *schedulerContextStarter) Restart(ctx context.Context, _ string) error {
	f.restarts.Add(1)
	f.ctxAlive.Store(ctx.Err() == nil)
	if f.err != nil {
		return f.err
	}
	return ctx.Err()
}

type schedulerFakePasswordRefresher struct {
	calls atomic.Int32
}

type schedulerFakeNotifier struct {
	calls   atomic.Int32
	title   string
	message string
}

func (f *schedulerFakeNotifier) NotifyAccountEvent(_ string, _ string, _ string, title, body string) {
	f.calls.Add(1)
	f.title = title
	f.message = body
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
	if apiCookieRenewInterval != 4*time.Hour {
		t.Fatalf("apiCookieRenewInterval=%s want 4h", apiCookieRenewInterval)
	}
}

func TestPendingAPIRenewLogsPendingAndRestartsAfterLateCookie(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	account := createSchedulerAccount(t, store, "cid-pending", "unb=1; havana_lgc_exp="+strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(60 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	starter := &schedulerFakeStarter{}
	s := NewScheduler(store, starter, nil, nil)
	s.api = apirenew.Service{
		HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 10 * time.Millisecond,
	}
	s.apiCookieRenewOne(context.Background(), "batch-pending", account)
	if got := lastAPIRenewLog(t, store, account.ID).status; got != "pending" {
		t.Fatalf("Promise 未完成时 status=%q want pending", got)
	}
	deadline := time.Now().Add(time.Second)
	for starter.restarts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := starter.restarts.Load(); got != 1 {
		t.Fatalf("迟到 Cookie 保存后 restarts=%d want 1", got)
	}
	detail, err := store.Cookies.GetDetails(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Value, "sdkSilent=") {
		t.Fatalf("迟到 Cookie 未保存: %q", detail.Value)
	}
	if got := lastAPIRenewLog(t, store, account.ID).status; got != "cookie_updated" {
		t.Fatalf("迟到响应最终状态=%q want cookie_updated", got)
	}
}

func TestAPICookieRenewSuccessWithoutCredentialChangeDoesNotRestart(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	expire := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	account := createSchedulerAccount(t, store, "cid-no-change", "unb=1; havana_lgc_exp="+expire)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	starter := &schedulerFakeStarter{}
	s := NewScheduler(store, starter, nil, nil)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	s.apiCookieRenewOne(context.Background(), "batch-no-change", account)
	if got := starter.restarts.Load(); got != 0 {
		t.Fatalf("Cookie 未变化时不应重启账号，restarts=%d", got)
	}
	if got := lastAPIRenewLog(t, store, account.ID).status; got != "success" {
		t.Fatalf("无变化成功状态=%q want success", got)
	}
}

func TestPendingAPIRenewUsesFreshContextForRestart(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	expire := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	account := createSchedulerAccount(t, store, "cid-fresh-context", "unb=1; havana_lgc_exp="+expire)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureSchedulerMillis(), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	starter := &schedulerContextStarter{}
	s := NewScheduler(store, starter, nil, nil)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond}
	s.apiCookieRenewOne(context.Background(), "batch-context", account)
	deadline := time.Now().Add(time.Second)
	for starter.restarts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if starter.restarts.Load() != 1 || !starter.ctxAlive.Load() {
		t.Fatalf("迟到响应重启必须使用独立有效上下文: restarts=%d alive=%v", starter.restarts.Load(), starter.ctxAlive.Load())
	}
}

func TestPendingAPIRenewStopsWithSchedulerContext(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	account := createSchedulerAccount(t, store, "cid-canceled-pending", "unb=1; havana_lgc_exp="+futureSchedulerMillis())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureSchedulerMillis(), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	starter := &schedulerFakeStarter{}
	s := NewScheduler(store, starter, nil, nil)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	s.apiCookieRenewOne(ctx, "batch-canceled-pending", account)
	cancel()
	s.watchers.Wait()
	if got := starter.restarts.Load(); got != 0 {
		t.Fatalf("调度器关闭后迟到响应不得重启账号，restarts=%d", got)
	}
	detail, err := store.Cookies.GetDetails(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail.Value, "sdkSilent=") {
		t.Fatalf("调度器关闭后不应再写入迟到 Cookie: %q", detail.Value)
	}
}

func TestPendingAPIRenewRestartFailureIsFinalFailure(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	account := createSchedulerAccount(t, store, "cid-restart-failure", "unb=1; havana_lgc_exp="+futureSchedulerMillis())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureSchedulerMillis(), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	starter := &schedulerContextStarter{err: errors.New("restart failed")}
	s := NewScheduler(store, starter, nil, nil)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond}
	s.apiCookieRenewOne(context.Background(), "batch-restart-failure", account)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		log := lastAPIRenewLog(t, store, account.ID)
		if log.status != "pending" {
			if log.status != "failed" || !strings.Contains(log.errorMessage, "restart failed") {
				t.Fatalf("重启失败终态异常: %+v", log)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("迟到续期没有写入重启失败终态")
}

func futureSchedulerMillis() string {
	return strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
}

func TestAPIRenewCompatibilitySettingsUseSingleRunner(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	expire := futureSchedulerMillis()
	createSchedulerAccount(t, store, "cid-single-runner", "unb=1; havana_lgc_exp="+expire)
	for _, key := range []string{apiCookieRenewEnabledSetting, cookiesRefreshEnabledSetting} {
		if err := store.Settings.Set(ctx, key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Settings.Set(ctx, apiCookieRenewIntervalSetting, "10s"); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	s := NewScheduler(store, nil, nil, nil)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	s.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	if got := requests.Load(); got != 1 {
		t.Fatalf("新旧配置同时开启只能启动一个续期任务，请求数=%d", got)
	}
}

func TestSchedulerDefaultsMatchUpstreamConfig(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	s := NewScheduler(store, nil, nil, nil)

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

func TestAPICookieRenewFailureNotifiesOnlyAtThirdConsecutiveFailure(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	notifier := &schedulerFakeNotifier{}
	s := NewScheduler(store, nil, nil, nil, notifier)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		s.addAPILog(ctx, db.RenewalLog{BatchID: "batch", CookieID: "cid-failure", Status: "failed", ErrorMessage: "接口超时"})
	}
	if notifier.calls.Load() != 1 {
		t.Fatalf("连续三次失败应通知 1 次，实际 %d", notifier.calls.Load())
	}
	if notifier.title == "" || !strings.Contains(notifier.message, "连续失败 3 次") {
		t.Fatalf("通知内容异常: title=%q body=%q", notifier.title, notifier.message)
	}
	s.addAPILog(ctx, db.RenewalLog{BatchID: "batch", CookieID: "cid-failure", Status: "failed", ErrorMessage: "接口超时"})
	if notifier.calls.Load() != 1 {
		t.Fatalf("连续第四次失败不应重复通知，实际 %d", notifier.calls.Load())
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

	s := NewScheduler(store, nil, nil, nil)
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

func TestLoginRenewPersistsAuthoritativeSessionBeforeParseError(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	account := createSchedulerAccount(t, store, "cid-login-session-error",
		"flat_leak=must-not-send; unb=1; _m_h5_tk=flat_old_1")
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "snapshot_old_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "document_only", Value: "doc", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "api_only", Value: "api", Domain: "h5api.m.goofish.com", Path: "/h5", Secure: true, HTTPOnly: true},
	}
	metadata := cookierefresh.MetadataWithSnapshot(`{"preserved":"yes"}`, snapshot)
	if err := store.Cookies.UpdateRenewalCookie(ctx, account.ID, account.Value, metadata, 1); err != nil {
		t.Fatal(err)
	}

	var requestCookie string
	client := &http.Client{Transport: schedulerRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requestCookie = req.Header.Get("Cookie")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": []string{
				"_m_h5_tk=snapshot_new_2; Domain=.goofish.com; Path=/; Secure",
				"api_rotated=new; Path=/h5; Secure; HttpOnly",
			}},
			Body:    io.NopCloser(strings.NewReader(`{"ret":`)),
			Request: req,
		}, nil
	})}
	s := NewScheduler(store, nil, nil, nil)
	s.mtop = &mtop.ClientImpl{HTTPClient: client, LoginUserURL: mtop.LoginUserAPI}
	s.loginRenewOne(ctx, "batch-login-session-error", account)

	for _, want := range []string{"unb=1", "_m_h5_tk=snapshot_old_1", "api_only=api"} {
		if !strings.Contains(requestCookie, want) {
			t.Fatalf("请求 Cookie %q 未使用加锁后重读的权威 Jar，缺少 %q", requestCookie, want)
		}
	}
	for _, unwanted := range []string{"flat_leak=", "document_only="} {
		if strings.Contains(requestCookie, unwanted) {
			t.Fatalf("请求 Cookie %q 泄漏了错误作用域 %q", requestCookie, unwanted)
		}
	}

	detail, err := store.Cookies.GetDetails(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Value, "_m_h5_tk=snapshot_new_2") || strings.Contains(detail.Value, "flat_leak=") {
		t.Fatalf("正文解析失败后未优先持久化响应 Cookie Jar: %q", detail.Value)
	}
	if !strings.Contains(detail.MetadataJSON, `"preserved":"yes"`) {
		t.Fatalf("持久化 Jar 时丢失原 metadata: %s", detail.MetadataJSON)
	}
	gotSnapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !ok {
		t.Fatalf("响应后权威 snapshot 丢失: %s", detail.MetadataJSON)
	}
	values := make(map[string]string, len(gotSnapshot))
	for _, cookie := range gotSnapshot {
		values[cookie.Name+"|"+cookie.Domain+"|"+cookie.Path] = cookie.Value
	}
	if values["_m_h5_tk|.goofish.com|/"] != "snapshot_new_2" ||
		values["api_rotated|h5api.m.goofish.com|/h5"] != "new" ||
		values["document_only|www.goofish.com|/im"] != "doc" {
		t.Fatalf("响应后 snapshot 作用域不完整: %+v", gotSnapshot)
	}
}

func TestSchedulerSettingEnabledOverrides(t *testing.T) {
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	ctx := context.Background()
	s := NewScheduler(store, nil, nil, nil)

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
	s := NewScheduler(store, nil, nil, nil)

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
	s := NewScheduler(store, nil, nil, nil)

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
	s := NewScheduler(store, nil, nil, nil)
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
	refresher := &schedulerFakePasswordRefresher{}
	s := NewScheduler(store, nil, refresher, nil)
	s.apiCookieRenewOne(ctx, "batch-expired", account)

	if refresher.calls.Load() != 0 {
		t.Fatalf("proactive renewal escalated to account recovery: %d", refresher.calls.Load())
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
	refresher := &schedulerFakePasswordRefresher{}
	starter := &schedulerFakeStarter{}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}},"marker":"single-silent"}`))
	}))
	defer srv.Close()

	s := NewScheduler(store, starter, refresher, nil)
	s.api = schedulerRenewServiceFromServer(srv)
	s.apiCookieRenewOne(ctx, "batch-silent", account)

	if requests.Load() != 1 || refresher.calls.Load() != 0 {
		t.Fatalf("requests=%d recovery=%d", requests.Load(), refresher.calls.Load())
	}
	if starter.restarts.Load() != 1 || starter.starts.Load() != 0 {
		t.Fatalf("官网静默续期成功应模拟 reload 重建运行时: starts=%d restarts=%d", starter.starts.Load(), starter.restarts.Load())
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
