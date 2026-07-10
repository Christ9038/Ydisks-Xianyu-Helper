package qrlogin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestReadQRBodyRejectsOversizedResponse(t *testing.T) {
	if _, err := readQRBody(strings.NewReader(strings.Repeat("x", maxQRResponseBytes+1))); err == nil {
		t.Fatal("oversized QR response should fail")
	}
}

func TestSessionStatusConcurrentSnapshot(t *testing.T) {
	m := NewManager(nil)
	sess := testVerificationSession()
	m.sessions["s1"] = sess
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				if worker%2 == 0 {
					sess.mu.Lock()
					sess.verificationScreenshot = fmt.Sprintf("shot-%d-%d", worker, i)
					sess.faceQRURL = fmt.Sprintf("qr-%d-%d", worker, i)
					sess.Status = "verification_required"
					sess.mu.Unlock()
				} else {
					status := m.GetSessionStatus("s1")
					if status["status"] == "not_found" {
						t.Error("existing session reported not_found")
						return
					}
				}
			}
		}(worker)
	}
	wg.Wait()
}

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

func TestCompleteVerificationReturnsCompletedSessionWithoutAnotherRequest(t *testing.T) {
	m := NewManager(nil)
	sess := testVerificationSession()
	sess.Status = "success"
	sess.unb = "completed-account"
	sess.cookies["unb"] = sess.unb
	m.sessions["s1"] = sess

	cookies, unb, err := m.CompleteVerification(context.Background(), "s1")
	if err != nil || unb != "completed-account" || !strings.Contains(cookies, "unb=completed-account") {
		t.Fatalf("completed session: cookies=%q unb=%q err=%v", cookies, unb, err)
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

func TestFaceVerificationExtractors(t *testing.T) {
	normal := `<script>window.location.href = "https://passport.goofish.com/iv/mini/verify_modes.htm?htoken=abc-123&_umidfg=";</script>`
	htoken, err := extractFaceHToken(`https://passport.goofish.com/iv/mini/normal_validate.htm?htoken=abc-123`)
	if err != nil || htoken != "abc-123" {
		t.Fatalf("extractFaceHToken=%q err=%v", htoken, err)
	}
	verifyURL, err := extractVerifyModesURL(normal)
	if err != nil {
		t.Fatalf("extractVerifyModesURL: %v", err)
	}
	if !strings.HasSuffix(verifyURL, "_umidfg=1") {
		t.Fatalf("verifyURL 未补齐 _umidfg: %q", verifyURL)
	}
	qrContent, err := extractFaceQRCodeContent(`<script>new Qrcode({ text: "https:\/\/passport.goofish.com\/face?x=1&amp;y=2" });</script>`)
	if err != nil {
		t.Fatalf("extractFaceQRCodeContent: %v", err)
	}
	if qrContent != "https://passport.goofish.com/face?x=1&y=2" {
		t.Fatalf("qrContent=%q", qrContent)
	}
}

func TestCheckFaceVerificationDone(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/iv/photoVerify/check.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("htoken") != "face-token" {
			t.Fatalf("htoken=%q", r.URL.Query().Get("htoken"))
		}
		_, _ = w.Write([]byte(`{"content":{"code":3,"url":"https://passport.goofish.com/ivCheckLogin.htm?ok=1"}}`))
	}))
	m, _, _ := newStubbedManager(t, hc)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := *m.httpc
	client.Jar = jar
	gotURL, done, err := m.checkFaceVerification(context.Background(), &client, "face-token")
	if err != nil || !done || !strings.Contains(gotURL, "ivCheckLogin") {
		t.Fatalf("checkFaceVerification url=%q done=%v err=%v", gotURL, done, err)
	}
}

func TestCollectJarCookies(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://passport.goofish.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "unb", Value: "123"}, {Name: "cookie2", Value: "abc"}})
	got := collectJarCookies(jar, u)
	if got["unb"] != "123" || got["cookie2"] != "abc" {
		t.Fatalf("collectJarCookies=%v", got)
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
