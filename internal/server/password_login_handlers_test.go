package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
)

type fakePasswordLoginRunner struct {
	mu           sync.Mutex
	cookies      map[string]string
	err          error
	block        chan struct{}
	events       []browser.PasswordLoginEvent
	calls        int
	lastAccount  string
	lastHeadless bool
}

func (f *fakePasswordLoginRunner) PasswordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool) (map[string]string, error) {
	f.mu.Lock()
	f.calls++
	f.lastAccount = account
	f.lastHeadless = headless
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.cookies, f.err
}

func (f *fakePasswordLoginRunner) PasswordLoginWithEvents(ctx context.Context, account, password, cookieID, userDataDir string, headless bool, onEvent browser.PasswordLoginEventHandler) (map[string]string, error) {
	for _, event := range f.events {
		onEvent(event)
	}
	return f.PasswordLogin(ctx, account, password, cookieID, userDataDir, headless)
}

func (f *fakePasswordLoginRunner) snapshot() (int, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastAccount, f.lastHeadless
}

func loginForPasswordTest(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()[0]
}

func startPasswordLoginForTest(t *testing.T, h http.Handler, cookie *http.Cookie, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/password-login", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode start: %v body=%s", err, rec.Body.String())
	}
	return out
}

func checkPasswordLoginForTest(t *testing.T, h http.Handler, cookie *http.Cookie, sessionID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/password-login/check/"+sessionID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("check status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode check: %v body=%s", err, rec.Body.String())
	}
	return out
}

func cancelPasswordLoginForTest(t *testing.T, h http.Handler, cookie *http.Cookie, sessionID string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/password-login/cancel/"+sessionID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func waitPasswordLoginStatus(t *testing.T, h http.Handler, cookie *http.Cookie, sessionID, want string) map[string]any {
	t.Helper()
	var out map[string]any
	for i := 0; i < 50; i++ {
		out = checkPasswordLoginForTest(t, h, cookie, sessionID)
		if out["status"] == want {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status did not reach %q, last=%+v", want, out)
	return nil
}

func waitLoginLogCount(t *testing.T, store *db.Store, cookieID string, want int) []db.AccountLoginLog {
	t.Helper()
	var logs []db.AccountLoginLog
	var err error
	for i := 0; i < 50; i++ {
		logs, err = store.LoginLogs.ListByCookie(context.Background(), cookieID, 10)
		if err == nil && len(logs) >= want {
			return logs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("login logs did not reach %d: logs=%#v err=%v", want, logs, err)
	return nil
}

func TestPasswordLoginSessionSuccessSavesCookiesAndAudit(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	fake := &fakePasswordLoginRunner{cookies: map[string]string{"unb": "acc1", "_m_h5_tk": "fresh"}}
	srv.PasswordLogin = fake
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)
	if err := store.Tokens.Save(context.Background(), "acc1", "did", "token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("save token: %v", err)
	}

	start := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"login-user","password":"secret","show_browser":false}`)
	if start["success"] != true || start["status"] != "processing" {
		t.Fatalf("start response=%+v", start)
	}
	sessionID, _ := start["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("missing session_id: %+v", start)
	}
	done := waitPasswordLoginStatus(t, h, cookie, sessionID, "success")
	if done["account_id"] != "acc1" || done["is_new_account"] != false {
		t.Fatalf("success response=%+v", done)
	}
	calls, account, headless := fake.snapshot()
	if calls != 1 || account != "login-user" || !headless {
		t.Fatalf("PasswordLogin 调用异常: calls=%d account=%q headless=%v", calls, account, headless)
	}
	saved, _ := store.Cookies.GetValue(context.Background(), "acc1")
	if !strings.Contains(saved, "_m_h5_tk=fresh") {
		t.Fatalf("cookie 未保存: %q", saved)
	}
	d, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.Username != "login-user" || d.Password != "secret" || d.ShowBrowser || d.LoginMethod != "password" || d.LastLoginAt == 0 {
		t.Fatalf("登录信息/审计字段异常: %+v", d)
	}
	if _, err := store.Tokens.Get(context.Background(), "acc1"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("密码登录成功应清 token，got %v", err)
	}
	logs, err := store.LoginLogs.ListByCookie(context.Background(), "acc1", 10)
	if err != nil || len(logs) != 1 || logs[0].Status != "success" {
		t.Fatalf("登录日志异常: logs=%#v err=%v", logs, err)
	}
}

func TestPasswordLoginUnavailableWhenBrowserManagerNil(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	start := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	if start["success"] != false || start["message"] != "浏览器登录服务不可用" {
		t.Fatalf("nil 浏览器应返回不可用且不启动登录: %+v", start)
	}
}

func TestPasswordLoginSessionShowBrowserRunsVisibleAndSavesPreference(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	fake := &fakePasswordLoginRunner{cookies: map[string]string{"unb": "acc1", "_m_h5_tk": "fresh"}}
	srv.PasswordLogin = fake
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	start := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"login-user","password":"secret","show_browser":true}`)
	sessionID, _ := start["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("missing session_id: %+v", start)
	}
	waitPasswordLoginStatus(t, h, cookie, sessionID, "success")

	calls, account, headless := fake.snapshot()
	if calls != 1 || account != "login-user" || headless {
		t.Fatalf("show_browser=true 应使用可视浏览器: calls=%d account=%q headless=%v", calls, account, headless)
	}
	d, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if !d.ShowBrowser {
		t.Fatalf("show_browser=true 应按原请求落库: %+v", d)
	}
}

func TestPasswordLoginDuplicateStartReturnsProcessingSession(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	fake := &fakePasswordLoginRunner{block: make(chan struct{}), cookies: map[string]string{"unb": "acc1"}}
	srv.PasswordLogin = fake
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	first := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	second := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	if first["session_id"] == "" || second["session_id"] != first["session_id"] || second["status"] != "processing" {
		t.Fatalf("重复启动应返回同一 processing 会话: first=%+v second=%+v", first, second)
	}

	cancelPasswordLoginForTest(t, h, cookie, first["session_id"].(string))
	waitLoginLogCount(t, store, "acc1", 1)
}

func TestPasswordLoginVerificationRequiredKeepsProcessingLock(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	fake := &fakePasswordLoginRunner{
		block: make(chan struct{}),
		events: []browser.PasswordLoginEvent{{
			Status:         browser.PasswordLoginStatusVerificationRequired,
			Message:        "需要人脸验证，请查看验证截图",
			Error:          "需要人脸验证",
			ScreenshotPath: "/tmp/face.png",
		}},
		cookies: map[string]string{"unb": "acc1"},
	}
	srv.PasswordLogin = fake
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	first := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	sessionID := first["session_id"].(string)
	waitPasswordLoginStatus(t, h, cookie, sessionID, "verification_required")
	second := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	if second["session_id"] != sessionID {
		t.Fatalf("验证码等待期间应继续复用同一会话: first=%+v second=%+v", first, second)
	}
	if calls, _, _ := fake.snapshot(); calls != 1 {
		t.Fatalf("验证码等待期间不应启动第二个浏览器任务，calls=%d", calls)
	}

	cancelPasswordLoginForTest(t, h, cookie, sessionID)
	waitLoginLogCount(t, store, "acc1", 1)
}

func TestPasswordLoginProcessingTimeoutReleasesAccount(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	fake := &fakePasswordLoginRunner{block: make(chan struct{}), cookies: map[string]string{"unb": "acc1"}}
	srv.PasswordLogin = fake
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	first := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	sessionID := first["session_id"].(string)
	srv.passwordMu.Lock()
	srv.passwordSessions[sessionID].CreatedAt = time.Now().Add(-(passwordLoginProcessingTimeout + time.Second))
	srv.passwordMu.Unlock()

	second := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	if second["session_id"] == "" || second["session_id"] == sessionID {
		t.Fatalf("processing 超时后应创建新会话: first=%+v second=%+v", first, second)
	}
	oldStatus := checkPasswordLoginForTest(t, h, cookie, sessionID)
	if oldStatus["status"] != "failed" {
		t.Fatalf("超时旧会话应标记 failed: %+v", oldStatus)
	}
	cancelPasswordLoginForTest(t, h, cookie, second["session_id"].(string))
	waitLoginLogCount(t, store, "acc1", 2)
}

func TestPasswordLoginSessionExpiresAfterOneHour(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	fake := &fakePasswordLoginRunner{block: make(chan struct{}), cookies: map[string]string{"unb": "acc1"}}
	srv.PasswordLogin = fake
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	start := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	sessionID := start["session_id"].(string)
	srv.passwordMu.Lock()
	srv.passwordSessions[sessionID].CreatedAt = time.Now().Add(-(passwordLoginSessionMaxAge + time.Second))
	srv.passwordMu.Unlock()

	done := checkPasswordLoginForTest(t, h, cookie, sessionID)
	if done["status"] != "not_found" {
		t.Fatalf("1小时后会话应过期: %+v", done)
	}
	waitLoginLogCount(t, store, "acc1", 1)
}

func TestPasswordLoginVerificationRequired(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	srv.PasswordLogin = &fakePasswordLoginRunner{
		events: []browser.PasswordLoginEvent{{
			Status:         browser.PasswordLoginStatusVerificationRequired,
			Message:        "需要人脸验证，请查看验证截图",
			Error:          "需要人脸验证",
			ScreenshotPath: "/tmp/face.png",
		}},
		err: errors.New("需要人脸验证"),
	}
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	start := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	sessionID := start["session_id"].(string)
	done := waitPasswordLoginStatus(t, h, cookie, sessionID, "verification_required")
	if done["error"] == "" {
		t.Fatalf("verification_required 应带 error: %+v", done)
	}
	if done["screenshot_path"] != "/tmp/face.png" {
		t.Fatalf("verification_required 应带截图: %+v", done)
	}
	again := checkPasswordLoginForTest(t, h, cookie, sessionID)
	if again["status"] != "verification_required" {
		t.Fatalf("验证态会话应保留: %+v", again)
	}
	logs := waitLoginLogCount(t, store, "acc1", 1)
	if logs[0].FailureReason != "verification_required" {
		t.Fatalf("验证态应记录 failure_reason: %#v", logs)
	}
}

func TestPasswordLoginBaxiaReason(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	srv.PasswordLogin = &fakePasswordLoginRunner{err: errors.New("baxia-punish 风控图形验证")}
	h := srv.Router()
	cookie := loginForPasswordTest(t, h)

	start := startPasswordLoginForTest(t, h, cookie, `{"account_id":"acc1","account":"u","password":"p"}`)
	sessionID := start["session_id"].(string)
	done := waitPasswordLoginStatus(t, h, cookie, sessionID, "failed")
	if done["reason"] != "baxia_punish_captcha" || done["cooldown_hours"] != float64(5) {
		t.Fatalf("baxia 错误应带 reason/cooldown: %+v", done)
	}
	logs, err := store.LoginLogs.ListByCookie(context.Background(), "acc1", 10)
	if err != nil || len(logs) != 1 || logs[0].FailureReason != "baxia_punish_captcha" {
		t.Fatalf("baxia 错误应记录专用 failure_reason: logs=%#v err=%v", logs, err)
	}
}
