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

	"xianyu-go/internal/xianyu/cookierefresh"
)

// failingReader 用于本次流程后续判断的failingReader
type failingReader struct{}

// Read 读取当前值。
func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

// TestReadQRBodyRejectsOversizedResponse 封装TestReadQR请求体RejectsOversized响应业务协调。
func TestReadQRBodyRejectsOversizedResponse(t *testing.T) {
	if // err 用于本次流程后续判断的err
	_, err := readQRBody(strings.NewReader(strings.Repeat("x", maxQRResponseBytes+1))); err == nil {
		t.Fatal("oversized QR response should fail")
	}
}

// TestSessionStatusConcurrentSnapshot 封装Test会话状态ConcurrentSnapshot业务协调。
func TestSessionStatusConcurrentSnapshot(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := NewManager(nil)
	// sess 用于本次流程后续判断的sess
	sess := testVerificationSession()
	m.sessions["s1"] = sess
	// wg 用于本次流程后续判断的wg
	var wg sync.WaitGroup
	for // worker 用于本次流程后续判断的工作器
	worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for // i 用于本次流程后续判断的i
			i := 0; i < 500; i++ {
				if worker%2 == 0 {
					sess.mu.Lock()
					sess.verificationScreenshot = fmt.Sprintf("shot-%d-%d", worker, i)
					sess.faceQRURL = fmt.Sprintf("qr-%d-%d", worker, i)
					sess.Status = "verification_required"
					sess.mu.Unlock()
				} else {
					// status 用于本次流程后续判断的状态
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

// TestCompleteVerificationRequiresPureGoCredentialResult 封装TestCompleteVerificationRequiresPureGoCredential结果业务协调。
func TestCompleteVerificationRequiresPureGoCredentialResult(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := NewManager(nil)
	m.sessions["s1"] = testVerificationSession()
	// oldTarget 用于本次流程后续判断的oldTarget
	oldTarget := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = oldTarget }()

	// err 用于本次流程后续判断的err
	_, _, err := m.CompleteVerification(context.Background(), "s1")
	if err == nil || !strings.Contains(err.Error(), "纯 Go 登录凭证换取未获取到 unb") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestCompleteVerificationReturnsCompletedSessionWithoutAnotherRequest 封装TestCompleteVerificationReturnsCompleted会话WithoutAnother请求业务协调。
func TestCompleteVerificationReturnsCompletedSessionWithoutAnotherRequest(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := NewManager(nil)
	// sess 用于本次流程后续判断的sess
	sess := testVerificationSession()
	sess.Status = "success"
	sess.unb = "completed-account"
	sess.cookies["unb"] = sess.unb
	m.sessions["s1"] = sess

	// cookies、unb、err 用于本次流程后续判断的cookies、unb、err
	cookies, unb, err := m.CompleteVerification(context.Background(), "s1")
	if err != nil || unb != "completed-account" || !strings.Contains(cookies, "unb=completed-account") {
		t.Fatalf("completed session: cookies=%q unb=%q err=%v", cookies, unb, err)
	}
}

// TestCompleteVerificationMissingSession 封装TestCompleteVerificationMissing会话业务协调。
func TestCompleteVerificationMissingSession(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := NewManager(nil)
	// err 用于本次流程后续判断的err
	_, _, err := m.CompleteVerification(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "会话不存在") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestRandomUUIDRequiresEntropy 封装TestRandomUUIDRequiresEntropy业务协调。
func TestRandomUUIDRequiresEntropy(t *testing.T) {
	// original 用于本次流程后续判断的original
	original := randReader
	t.Cleanup(func() { randReader = original })
	randReader = failingReader{}
	if // err 用于本次流程后续判断的err
	_, err := randomUUID(); err == nil {
		t.Fatal("randomUUID should fail when entropy source fails")
	}
	randReader = io.LimitReader(strings.NewReader("0123456789abcdef"), 16)
	// id、err 用于本次流程后续判断的id、err
	id, err := randomUUID()
	if err != nil || len(id) != 36 || id[14] != '4' {
		t.Fatalf("randomUUID() = %q, %v", id, err)
	}
}

// TestFaceVerificationExtractors 封装TestFaceVerificationExtractors业务协调。
func TestFaceVerificationExtractors(t *testing.T) {
	// normal 用于本次流程后续判断的normal
	normal := `<script>window.location.href = "https://passport.goofish.com/iv/mini/verify_modes.htm?htoken=abc-123&_umidfg=";</script>`
	// htoken、err 用于本次流程后续判断的htoken、err
	htoken, err := extractFaceHToken(`https://passport.goofish.com/iv/mini/normal_validate.htm?htoken=abc-123`)
	if err != nil || htoken != "abc-123" {
		t.Fatalf("extractFaceHToken=%q err=%v", htoken, err)
	}
	// verifyURL、err 用于本次流程后续判断的verifyURL、err
	verifyURL, err := extractVerifyModesURL(normal)
	if err != nil {
		t.Fatalf("extractVerifyModesURL: %v", err)
	}
	if !strings.HasSuffix(verifyURL, "_umidfg=1") {
		t.Fatalf("verifyURL 未补齐 _umidfg: %q", verifyURL)
	}
	// qrContent、err 用于本次流程后续判断的qrContent、err
	qrContent, err := extractFaceQRCodeContent(`<script>new Qrcode({ text: "https:\/\/passport.goofish.com\/face?x=1&amp;y=2" });</script>`)
	if err != nil {
		t.Fatalf("extractFaceQRCodeContent: %v", err)
	}
	if qrContent != "https://passport.goofish.com/face?x=1&y=2" {
		t.Fatalf("qrContent=%q", qrContent)
	}
}

// TestCheckFaceVerificationDone 封装TestCheckFaceVerificationDone业务协调。
func TestCheckFaceVerificationDone(t *testing.T) {
	// hc 用于本次流程后续判断的hc
	hc := &handlerChain{}
	hc.handle("/iv/photoVerify/check.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("htoken") != "face-token" {
			t.Fatalf("htoken=%q", r.URL.Query().Get("htoken"))
		}
		_, _ = w.Write([]byte(`{"content":{"code":3,"url":"https://passport.goofish.com/ivCheckLogin.htm?ok=1"}}`))
	}))
	// m 用于本次流程后续判断的m
	m, _, _ := newStubbedManager(t, hc)
	// jar、err 用于本次流程后续判断的jar、err
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// client 用于本次流程后续判断的client
	client := *m.httpc
	client.Jar = jar
	// gotURL、done、err 用于本次流程后续判断的gotURL、done、err
	gotURL, done, err := m.checkFaceVerification(context.Background(), &client, "face-token")
	if err != nil || !done || !strings.Contains(gotURL, "ivCheckLogin") {
		t.Fatalf("checkFaceVerification url=%q done=%v err=%v", gotURL, done, err)
	}
}

// TestCollectJarCookies 封装TestCollectJarCookies业务协调。
func TestCollectJarCookies(t *testing.T) {
	// jar、err 用于本次流程后续判断的jar、err
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// u 用于本次流程后续判断的u
	u, _ := url.Parse("https://passport.goofish.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "unb", Value: "123"}, {Name: "cookie2", Value: "abc"}})
	// got 用于本次流程后续判断的got
	got := collectJarCookies(jar, u)
	if got["unb"] != "123" || got["cookie2"] != "abc" {
		t.Fatalf("collectJarCookies=%v", got)
	}
}

// TestFaceCookieJarExportsCrossDomainAttributes 封装TestFace登录凭证JarExportsCrossDomainAttributes业务协调。
func TestFaceCookieJarExportsCrossDomainAttributes(t *testing.T) {
	// jar 用于本次流程后续判断的jar
	jar := newFaceCookieJar(map[string]string{"tmp": "1"}, []cookierefresh.BrowserCookie{})
	// passport 用于本次流程后续判断的passport
	passport, _ := url.Parse("https://passport.goofish.com/ivCheckLogin.htm")
	// input 用于本次流程后续判断的input
	input := &http.Cookie{
		Name: "unb", Value: "777", Domain: ".goofish.com", Path: "/", Secure: true, HttpOnly: true,
	}
	jar.SetCookies(passport, []*http.Cookie{input})
	// www 用于本次流程后续判断的www
	www, _ := url.Parse("https://www.goofish.com/im")
	// got 用于本次流程后续判断的got
	got := collectJarCookies(jar, www)
	if got["unb"] != "777" {
		// snapshot 用于本次流程后续判断的snapshot
		snapshot, _ := jar.Snapshot()
		t.Fatalf("跨域 Cookie 未进入 /im: cookies=%v snapshot=%+v raw=%q", got, snapshot, input.String())
	}
	// snapshot、complete 用于本次流程后续判断的snapshot、complete
	snapshot, complete := jar.Snapshot()
	if !complete || len(snapshot) != 1 || snapshot[0].Domain != ".goofish.com" || !snapshot[0].HTTPOnly || !snapshot[0].Secure {
		t.Fatalf("完整 Cookie 属性未保留: complete=%v snapshot=%+v", complete, snapshot)
	}
}

// testVerificationSession 封装testVerification会话业务协调。
func testVerificationSession() *Session {
	return &Session{
		SessionID:   "s1",
		Status:      "verification_required",
		cookies:     map[string]string{"tmp": "1"},
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
}

// newEmptyCookieServer 封装newEmpty登录凭证Server业务协调。
func newEmptyCookieServer(t *testing.T) string {
	t.Helper()
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
