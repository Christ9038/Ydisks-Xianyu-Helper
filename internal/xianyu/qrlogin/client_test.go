package qrlogin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestCompleteVerificationRequiresBrowserWhenHTTPMissingUNB(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s1"] = testVerificationSession()
	oldTarget := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = oldTarget }()

	_, _, err := m.CompleteVerification(context.Background(), "s1")
	if err == nil || !strings.Contains(err.Error(), "需要浏览器支持") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestCompleteVerificationBrowserSuccess(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s1"] = testVerificationSession()
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		if !strings.Contains(tmpCookies, "tmp=1") {
			t.Fatalf("浏览器刷新器收到临时 cookie 异常: %q", tmpCookies)
		}
		return "unb=999; cookie2=abc", "999", nil
	})
	oldTarget := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = oldTarget }()

	cookies, unb, err := m.CompleteVerification(context.Background(), "s1")
	if err != nil {
		t.Fatalf("CompleteVerification: %v", err)
	}
	if unb != "999" || !strings.Contains(cookies, "unb=999") {
		t.Fatalf("返回异常: cookies=%q unb=%q", cookies, unb)
	}
	if m.sessions["s1"].Status != "success" {
		t.Fatalf("状态异常: %s", m.sessions["s1"].Status)
	}
}

func TestCompleteVerificationBrowserUNBFromCookieMap(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s1"] = testVerificationSession()
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		return "unb=888; cookie2=abc", "", nil
	})
	oldTarget := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = oldTarget }()

	_, unb, err := m.CompleteVerification(context.Background(), "s1")
	if err != nil {
		t.Fatalf("CompleteVerification: %v", err)
	}
	if unb != "888" {
		t.Fatalf("应从 cookie map 提取 unb，got %q", unb)
	}
}

func TestCompleteVerificationMissingSession(t *testing.T) {
	m := NewManager(nil)
	_, _, err := m.CompleteVerification(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "会话不存在") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestRandomUUIDRequiresEntropy(t *testing.T) {
	original := randReader
	t.Cleanup(func() { randReader = original })
	randReader = failingReader{}
	if _, err := randomUUID(); err == nil {
		t.Fatal("randomUUID should fail when entropy source fails")
	}
	randReader = io.LimitReader(strings.NewReader("0123456789abcdef"), 16)
	id, err := randomUUID()
	if err != nil || len(id) != 36 || id[14] != '4' {
		t.Fatalf("randomUUID() = %q, %v", id, err)
	}
}

func testVerificationSession() *Session {
	return &Session{
		SessionID:   "s1",
		Status:      "verification_required",
		cookies:     map[string]string{"tmp": "1"},
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
}

func newEmptyCookieServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
