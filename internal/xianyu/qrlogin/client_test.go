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

// failingReader 保存failingReader，供当前处理流程使用
type failingReader struct{}

// Read 读取当前值。
func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

// TestReadQRBodyRejectsOversizedResponse 负责TestReadQR请求体RejectsOversized响应相关处理。
func TestReadQRBodyRejectsOversizedResponse(t *testing.T) {
	if // err 保存err，供当前处理流程使用
	_, err := readQRBody(strings.NewReader(strings.Repeat("x", maxQRResponseBytes+1))); err == nil {
		t.Fatal("oversized QR response should fail")
	}
}

// TestSessionStatusConcurrentSnapshot 负责Test会话状态ConcurrentSnapshot相关处理。
func TestSessionStatusConcurrentSnapshot(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	// sess 保存sess，供当前处理流程使用
	sess := testVerificationSession()
	m.sessions["s1"] = sess
	// wg 保存wg，供当前处理流程使用
	var wg sync.WaitGroup
	for // worker 保存工作器，供当前处理流程使用
	worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for // i 保存i，供当前处理流程使用
			i := 0; i < 500; i++ {
				if worker%2 == 0 {
					sess.mu.Lock()
					sess.verificationScreenshot = fmt.Sprintf("shot-%d-%d", worker, i)
					sess.faceQRURL = fmt.Sprintf("qr-%d-%d", worker, i)
					sess.Status = "verification_required"
					sess.mu.Unlock()
				} else {
					// status 保存状态，供当前处理流程使用
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

// TestCompleteVerificationRequiresPureGoCredentialResult 负责TestCompleteVerificationRequiresPureGoCredential结果相关处理。
func TestCompleteVerificationRequiresPureGoCredentialResult(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s1"] = testVerificationSession()
	// oldTarget 保存oldTarget，供当前处理流程使用
	oldTarget := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = oldTarget }()

	// err 保存err，供当前处理流程使用
	_, _, err := m.CompleteVerification(context.Background(), "s1")
	if err == nil || !strings.Contains(err.Error(), "纯 Go 登录凭证换取未获取到 unb") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestCompleteVerificationReturnsCompletedSessionWithoutAnotherRequest 负责TestCompleteVerificationReturnsCompleted会话WithoutAnother请求相关处理。
func TestCompleteVerificationReturnsCompletedSessionWithoutAnotherRequest(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	// sess 保存sess，供当前处理流程使用
	sess := testVerificationSession()
	sess.Status = "success"
	sess.unb = "completed-account"
	sess.cookies["unb"] = sess.unb
	m.sessions["s1"] = sess

	// cookies、unb、err 保存cookies、unb、err，供当前处理流程使用
	cookies, unb, err := m.CompleteVerification(context.Background(), "s1")
	if err != nil || unb != "completed-account" || !strings.Contains(cookies, "unb=completed-account") {
		t.Fatalf("completed session: cookies=%q unb=%q err=%v", cookies, unb, err)
	}
}

// TestCompleteVerificationMissingSession 负责TestCompleteVerificationMissing会话相关处理。
func TestCompleteVerificationMissingSession(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	// err 保存err，供当前处理流程使用
	_, _, err := m.CompleteVerification(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "会话不存在") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestRandomUUIDRequiresEntropy 负责TestRandomUUIDRequiresEntropy相关处理。
func TestRandomUUIDRequiresEntropy(t *testing.T) {
	// original 保存original，供当前处理流程使用
	original := randReader
	t.Cleanup(func() { randReader = original })
	randReader = failingReader{}
	if // err 保存err，供当前处理流程使用
	_, err := randomUUID(); err == nil {
		t.Fatal("randomUUID should fail when entropy source fails")
	}
	randReader = io.LimitReader(strings.NewReader("0123456789abcdef"), 16)
	// id、err 保存id、err，供当前处理流程使用
	id, err := randomUUID()
	if err != nil || len(id) != 36 || id[14] != '4' {
		t.Fatalf("randomUUID() = %q, %v", id, err)
	}
}

// TestFaceVerificationExtractors 负责TestFaceVerificationExtractors相关处理。
func TestFaceVerificationExtractors(t *testing.T) {
	// normal 保存normal，供当前处理流程使用
	normal := `<script>window.location.href = "https://passport.goofish.com/iv/mini/verify_modes.htm?htoken=abc-123&_umidfg=";</script>`
	// htoken、err 保存htoken、err，供当前处理流程使用
	htoken, err := extractFaceHToken(`https://passport.goofish.com/iv/mini/normal_validate.htm?htoken=abc-123`)
	if err != nil || htoken != "abc-123" {
		t.Fatalf("extractFaceHToken=%q err=%v", htoken, err)
	}
	// verifyURL、err 保存verifyURL、err，供当前处理流程使用
	verifyURL, err := extractVerifyModesURL(normal)
	if err != nil {
		t.Fatalf("extractVerifyModesURL: %v", err)
	}
	if !strings.HasSuffix(verifyURL, "_umidfg=1") {
		t.Fatalf("verifyURL 未补齐 _umidfg: %q", verifyURL)
	}
	// qrContent、err 保存qrContent、err，供当前处理流程使用
	qrContent, err := extractFaceQRCodeContent(`<script>new Qrcode({ text: "https:\/\/passport.goofish.com\/face?x=1&amp;y=2" });</script>`)
	if err != nil {
		t.Fatalf("extractFaceQRCodeContent: %v", err)
	}
	if qrContent != "https://passport.goofish.com/face?x=1&y=2" {
		t.Fatalf("qrContent=%q", qrContent)
	}
}

// TestCheckFaceVerificationDone 负责TestCheckFaceVerificationDone相关处理。
func TestCheckFaceVerificationDone(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/iv/photoVerify/check.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("htoken") != "face-token" {
			t.Fatalf("htoken=%q", r.URL.Query().Get("htoken"))
		}
		_, _ = w.Write([]byte(`{"content":{"code":3,"url":"https://passport.goofish.com/ivCheckLogin.htm?ok=1"}}`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// jar、err 保存jar、err，供当前处理流程使用
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// client 保存client，供当前处理流程使用
	client := *m.httpc
	client.Jar = jar
	// gotURL、done、err 保存gotURL、done、err，供当前处理流程使用
	gotURL, done, err := m.checkFaceVerification(context.Background(), &client, "face-token")
	if err != nil || !done || !strings.Contains(gotURL, "ivCheckLogin") {
		t.Fatalf("checkFaceVerification url=%q done=%v err=%v", gotURL, done, err)
	}
}

// TestCollectJarCookies 负责TestCollectJarCookies相关处理。
func TestCollectJarCookies(t *testing.T) {
	// jar、err 保存jar、err，供当前处理流程使用
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// u 保存u，供当前处理流程使用
	u, _ := url.Parse("https://passport.goofish.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "unb", Value: "123"}, {Name: "cookie2", Value: "abc"}})
	// got 保存got，供当前处理流程使用
	got := collectJarCookies(jar, u)
	if got["unb"] != "123" || got["cookie2"] != "abc" {
		t.Fatalf("collectJarCookies=%v", got)
	}
}

// TestFaceCookieJarExportsCrossDomainAttributes 负责TestFace登录凭证JarExportsCrossDomainAttributes相关处理。
func TestFaceCookieJarExportsCrossDomainAttributes(t *testing.T) {
	// jar 保存jar，供当前处理流程使用
	jar := newFaceCookieJar(map[string]string{"tmp": "1"}, []cookierefresh.BrowserCookie{})
	// passport 保存passport，供当前处理流程使用
	passport, _ := url.Parse("https://passport.goofish.com/ivCheckLogin.htm")
	// input 保存input，供当前处理流程使用
	input := &http.Cookie{
		Name: "unb", Value: "777", Domain: ".goofish.com", Path: "/", Secure: true, HttpOnly: true,
	}
	jar.SetCookies(passport, []*http.Cookie{input})
	// www 保存www，供当前处理流程使用
	www, _ := url.Parse("https://www.goofish.com/im")
	// got 保存got，供当前处理流程使用
	got := collectJarCookies(jar, www)
	if got["unb"] != "777" {
		// snapshot 保存snapshot，供当前处理流程使用
		snapshot, _ := jar.Snapshot()
		t.Fatalf("跨域 Cookie 未进入 /im: cookies=%v snapshot=%+v raw=%q", got, snapshot, input.String())
	}
	// snapshot、complete 保存snapshot、complete，供当前处理流程使用
	snapshot, complete := jar.Snapshot()
	if !complete || len(snapshot) != 1 || snapshot[0].Domain != ".goofish.com" || !snapshot[0].HTTPOnly || !snapshot[0].Secure {
		t.Fatalf("完整 Cookie 属性未保留: complete=%v snapshot=%+v", complete, snapshot)
	}
}

// testVerificationSession 负责testVerification会话相关处理。
func testVerificationSession() *Session {
	return &Session{
		SessionID:   "s1",
		Status:      "verification_required",
		cookies:     map[string]string{"tmp": "1"},
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
}

// newEmptyCookieServer 负责newEmpty登录凭证Server相关处理。
func newEmptyCookieServer(t *testing.T) string {
	t.Helper()
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
