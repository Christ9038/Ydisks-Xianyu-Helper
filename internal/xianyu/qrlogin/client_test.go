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

// TestManagerCloseContextCancelsSessionTask 验证进程关闭会取消 Manager 拥有的会话后台任务并等待其退出。
func TestManagerCloseContextCancelsSessionTask(t *testing.T) {
	// manager 保存待关闭的二维码会话管理器。
	manager := NewManager(nil)
	// session 保存不依赖平台网络的最小会话，startSessionTask 会为它补齐生命周期 Context。
	session := &Session{SessionID: "close-session", Status: "waiting", cookies: map[string]string{}, params: map[string]string{}}
	manager.sessions[session.SessionID] = session
	// started 通知测试任务已进入等待。
	started := make(chan struct{})
	// stopped 通知测试任务已收到关闭取消。
	stopped := make(chan struct{})
	// startedTask 表示后台任务是否被 Manager 成功登记。
	startedTask := manager.startSessionTask(session.SessionID, time.Minute, func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	})
	if !startedTask {
		t.Fatal("二维码后台任务未启动")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("二维码后台任务未进入等待")
	}
	// closeErr 保存关闭等待结果；CloseContext 必须在任务退出后才返回。
	closeErr := manager.CloseContext(context.Background())
	if closeErr != nil {
		t.Fatalf("关闭二维码管理器失败: %v", closeErr)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("关闭未取消二维码后台任务")
	}
}

// TestManagerDeleteSessionCancelsSessionTask 验证删除会话会停止该会话的后台任务而不影响 Manager 的后续关闭。
func TestManagerDeleteSessionCancelsSessionTask(t *testing.T) {
	// manager 保存待验证删除行为的二维码会话管理器。
	manager := NewManager(nil)
	// session 保存被删除的最小会话。
	session := &Session{SessionID: "delete-session", Status: "waiting", cookies: map[string]string{}, params: map[string]string{}}
	manager.sessions[session.SessionID] = session
	// started 协调测试任务进入等待时机。
	started := make(chan struct{})
	// stopped 协调测试任务收到删除取消后的退出时机。
	stopped := make(chan struct{})
	if !manager.startSessionTask(session.SessionID, time.Minute, func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	}) {
		t.Fatal("二维码后台任务未启动")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("二维码后台任务未进入等待")
	}
	manager.DeleteSession(session.SessionID)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("删除会话未取消后台任务")
	}
	// closeErr 验证删除后的 Manager 仍可幂等关闭并回收 WaitGroup。
	closeErr := manager.CloseContext(context.Background())
	if closeErr != nil {
		t.Fatalf("删除后关闭二维码管理器失败: %v", closeErr)
	}
}

// TestManagerCloseRejectsNewQRCode 验证关闭后的 Manager 在访问平台前拒绝创建新二维码会话。
func TestManagerCloseRejectsNewQRCode(t *testing.T) {
	// manager 保存已经关闭的二维码会话管理器。
	manager := NewManager(nil)
	// closeErr 保存关闭后的返回错误。
	if closeErr := manager.CloseContext(context.Background()); closeErr != nil {
		t.Fatalf("关闭二维码管理器失败: %v", closeErr)
	}
	// _, _, generateErr 保存关闭后创建二维码返回的结果和错误。
	_, _, generateErr := manager.GenerateQRCode(context.Background())
	if generateErr == nil || !strings.Contains(generateErr.Error(), "已关闭") {
		t.Fatalf("关闭后仍接受新二维码会话: %v", generateErr)
	}
}

// TestManagerCloseContextCancelsFaceVerification 验证普通扫码切换到人脸验证后，Manager 关闭仍会取消并等待人脸 HTTP 任务。
func TestManagerCloseContextCancelsFaceVerification(t *testing.T) {
	// requestStarted 通知人脸验证的第一跳已经进入测试服务器。
	requestStarted := make(chan struct{})
	// requestCanceled 通知测试服务器收到请求 Context 取消。
	requestCanceled := make(chan struct{})
	// manager 是带可阻塞人脸验证 HTTP 传输的二维码管理器。
	manager, _, _ := newStubbedManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
	}))
	// session 保存已确认但尚未完成风控的人脸会话。
	session := &Session{SessionID: "face-close-session", Status: "waiting", cookies: map[string]string{"tmp": "cookie"}, params: map[string]string{}, createdTime: time.Now(), expireTime: time.Minute}
	manager.sessions[session.SessionID] = session
	manager.handleConfirmedQRStatus(context.Background(), session, session.SessionID, true, "https://passport.goofish.com/iv/mini/normal_validate.htm")
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("人脸验证任务未进入第一跳请求")
	}
	// closeErr 保存 Manager 取消并等待人脸验证任务退出的结果。
	if closeErr := manager.CloseContext(context.Background()); closeErr != nil {
		t.Fatalf("关闭二维码管理器失败: %v", closeErr)
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("关闭未取消人脸验证 HTTP 请求")
	}
	// repeatErr 验证关闭操作幂等。
	if repeatErr := manager.CloseContext(context.Background()); repeatErr != nil {
		t.Fatalf("重复关闭二维码管理器失败: %v", repeatErr)
	}
}

// TestDeleteSessionCancelsFaceVerification 验证删除已进入人脸验证的会话会立即停止其后台 HTTP 任务。
func TestDeleteSessionCancelsFaceVerification(t *testing.T) {
	// requestStarted 通知人脸验证已开始网络请求。
	requestStarted := make(chan struct{})
	// requestCanceled 通知删除会话已经取消网络请求。
	requestCanceled := make(chan struct{})
	// manager 是带可观察人脸 HTTP 取消信号的二维码管理器。
	manager, _, _ := newStubbedManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
	}))
	// session 保存等待人脸验证的会话。
	session := &Session{SessionID: "face-delete-session", Status: "waiting", cookies: map[string]string{"tmp": "cookie"}, params: map[string]string{}, createdTime: time.Now(), expireTime: time.Minute}
	manager.sessions[session.SessionID] = session
	manager.handleConfirmedQRStatus(context.Background(), session, session.SessionID, true, "https://passport.goofish.com/iv/mini/normal_validate.htm")
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("人脸验证任务未进入第一跳请求")
	}
	manager.DeleteSession(session.SessionID)
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("删除会话未取消人脸验证 HTTP 请求")
	}
	// closeErr 保存删除人脸会话后等待所有后台任务退出的关闭结果。
	if closeErr := manager.CloseContext(context.Background()); closeErr != nil {
		t.Fatalf("删除人脸会话后关闭失败: %v", closeErr)
	}
}

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
