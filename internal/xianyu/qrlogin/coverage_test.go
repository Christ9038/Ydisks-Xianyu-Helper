package qrlogin

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubTransport 把所有请求转发到 httptest.Server，保留路径与查询，
// 从而让测试可以 mock 固定 host（passport.goofish.com 等）的接口。
type stubTransport struct {
	server *httptest.Server
	calls  atomic.Int64
}

func (t *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	// 复制请求到测试服务器，保留方法/头部/正文。
	outReq := req.Clone(req.Context())
	outReq.URL.Scheme = "http"
	outReq.URL.Host = t.server.URL[len("http://"):]
	outReq.RequestURI = ""
	resp, err := t.server.Client().Do(outReq)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// newStubbedManager 构造一个使用 stubTransport 的 Manager，返回 manager 与 server。
func newStubbedManager(t *testing.T, handler http.Handler) (*Manager, *httptest.Server, *stubTransport) {
	t.Helper()
	srv := httptest.NewServer(handler)
	tr := &stubTransport{server: srv}
	m := NewManager(nil)
	m.httpc = &http.Client{Timeout: 10 * time.Second, Transport: tr}
	t.Cleanup(srv.Close)
	return m, srv, tr
}

// gzipBody 用 gzip 压缩输入字符串。
func gzipBody(t *testing.T, raw string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(raw)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// handlerChain 按请求路径分发到不同 handler。
type handlerChain struct {
	mu       sync.Mutex
	routes   map[string]http.Handler
	fallback http.Handler
}

func (h *handlerChain) handle(path string, fn http.Handler) *handlerChain {
	if h.routes == nil {
		h.routes = make(map[string]http.Handler)
	}
	h.routes[path] = fn
	return h
}

func (h *handlerChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// 用 path 前缀匹配：去掉查询串后比较 path。
	p := r.URL.Path
	for prefix, fn := range h.routes {
		if strings.HasPrefix(p, prefix) {
			fn.ServeHTTP(w, r)
			return
		}
	}
	if h.fallback != nil {
		h.fallback.ServeHTTP(w, r)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// ---- getMH5TK ----

func TestGetMH5TKMergesCookiesAndSucceeds(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 第一次 GET 下发 _m_h5_tk，第二次 POST 更新 cookie。
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "abc_token_1717000000000"})
		http.SetCookie(w, &http.Cookie{Name: "other", Value: "v1"})
		w.WriteHeader(http.StatusOK)
	}))
	m, _, _ := newStubbedManager(t, hc)

	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	if err := m.getMH5TK(context.Background(), sess); err != nil {
		t.Fatalf("getMH5TK: %v", err)
	}
	if sess.cookies["_m_h5_tk"] != "abc_token_1717000000000" {
		t.Fatalf("cookie 未合并: %v", sess.cookies)
	}
	if sess.cookies["other"] != "v1" {
		t.Fatalf("other cookie 未合并: %v", sess.cookies)
	}
}

func TestGetMH5TKErrorOnFirstRequest(t *testing.T) {
	hc := &handlerChain{
		fallback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	}
	m, _, _ := newStubbedManager(t, hc)
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	// 5xx 不会让 httpc.Do 报错，但 io.Copy 不报错；getMH5TK 仍会成功，
	// 只是拿不到 cookie。验证它不 panic 即可（且无 _m_h5_tk）。
	if err := m.getMH5TK(context.Background(), sess); err != nil {
		t.Fatalf("5xx 不应让 getMH5TK 报错: %v", err)
	}
	if _, ok := sess.cookies["_m_h5_tk"]; ok {
		t.Fatalf("不应有 _m_h5_tk cookie")
	}
}

func TestGetMH5TKTransportError(t *testing.T) {
	m := NewManager(nil)
	// 用一个会立即返回 transport 错误的 client。
	m.httpc = &http.Client{Timeout: 1 * time.Nanosecond, Transport: &errTransport{}}
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	if err := m.getMH5TK(context.Background(), sess); err == nil {
		t.Fatal("transport 错误应导致 getMH5TK 失败")
	}
}

type errTransport struct{}

func (errTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

// ---- getLoginParams ----

const viewDataHTML = `<html><script>window.viewData = {"loginFormData":{"appName":"xianyu","appEntrance":"web","isMobile":false,"numField":123,"boolTrue":true}};</script></html>`

func TestGetLoginParamsParsesViewData(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	m, _, _ := newStubbedManager(t, hc)

	sess := &Session{cookies: map[string]string{"_m_h5_tk": "tk_123"}, params: map[string]string{}}
	params, err := m.getLoginParams(context.Background(), sess)
	if err != nil {
		t.Fatalf("getLoginParams: %v", err)
	}
	// 验证 bool/number 都被转成字符串（历史坑点）。
	if params["isMobile"] != "false" {
		t.Fatalf("bool→string 转换失败: isMobile=%q", params["isMobile"])
	}
	if params["boolTrue"] != "true" {
		t.Fatalf("bool→string 转换失败: boolTrue=%q", params["boolTrue"])
	}
	if params["numField"] != "123" {
		t.Fatalf("number→string 转换失败: numField=%q", params["numField"])
	}
	if params["umidTag"] != "SERVER" {
		t.Fatalf("umidTag 注入失败: %q", params["umidTag"])
	}
	if sess.params["appName"] != "xianyu" {
		t.Fatalf("sess.params 未更新: %v", sess.params)
	}
}

func TestGetLoginParamsMissingViewData(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>no viewData here</html>`))
	}))
	m, _, _ := newStubbedManager(t, hc)

	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	_, err := m.getLoginParams(context.Background(), sess)
	if err == nil || !strings.Contains(err.Error(), "未找到 viewData") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestGetLoginParamsInvalidJSON(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script>window.viewData = {not valid json};</script>`))
	}))
	m, _, _ := newStubbedManager(t, hc)

	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	_, err := m.getLoginParams(context.Background(), sess)
	if err == nil || !strings.Contains(err.Error(), "解析 viewData 失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestGetLoginParamsEmptyLoginFormData(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script>window.viewData = {"loginFormData":null};</script>`))
	}))
	m, _, _ := newStubbedManager(t, hc)

	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	_, err := m.getLoginParams(context.Background(), sess)
	if err == nil || !strings.Contains(err.Error(), "loginFormData 为空") {
		t.Fatalf("错误异常: %v", err)
	}
}

// ---- GenerateQRCode 端到端 ----

func TestGenerateQRCodeSuccess(t *testing.T) {
	hc := &handlerChain{}
	// getMH5TK 路径
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tok_1717"})
		w.WriteHeader(http.StatusOK)
	}))
	// getLoginParams 路径
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	// generate.do 二维码接口
	hc.handle("/newlogin/qrcode/generate.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": map[string]any{
				"success": true,
				"data": map[string]any{
					"t":           1717000000000,
					"ck":          "ck_value",
					"codeContent": "https://login/qr?token=xyz",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	m, _, _ := newStubbedManager(t, hc)

	sessionID, qrCodeURL, err := m.GenerateQRCode(context.Background())
	if err != nil {
		t.Fatalf("GenerateQRCode: %v", err)
	}
	if sessionID == "" {
		t.Fatal("sessionID 为空")
	}
	if !strings.HasPrefix(qrCodeURL, "data:image/png;base64,") {
		t.Fatalf("二维码 data URL 异常: %q", qrCodeURL[:min(40, len(qrCodeURL))])
	}
	// session 已注册。
	sess, ok := m.sessions[sessionID]
	if !ok {
		t.Fatal("session 未注册")
	}
	if sess.params["t"] != "1717000000000" {
		t.Fatalf("t 未转成纯数字字符串: %q", sess.params["t"])
	}
	if sess.params["ck"] != "ck_value" {
		t.Fatalf("ck 未保存: %q", sess.params["ck"])
	}
	if sess.qrContent != "https://login/qr?token=xyz" {
		t.Fatalf("qrContent 异常: %q", sess.qrContent)
	}
}

func TestGenerateQRCodeFailureResponse(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tok"})
		w.WriteHeader(http.StatusOK)
	}))
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	hc.handle("/newlogin/qrcode/generate.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":{"success":false,"data":{}}}`))
	}))
	m, _, _ := newStubbedManager(t, hc)

	_, _, err := m.GenerateQRCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "获取登录二维码失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestGenerateQRCodeInvalidJSON(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tok"})
		w.WriteHeader(http.StatusOK)
	}))
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	hc.handle("/newlogin/qrcode/generate.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	m, _, _ := newStubbedManager(t, hc)

	_, _, err := m.GenerateQRCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "解析二维码响应失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestGenerateQRCodeTAsString(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tok"})
		w.WriteHeader(http.StatusOK)
	}))
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	hc.handle("/newlogin/qrcode/generate.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": map[string]any{
				"success": true,
				"data": map[string]any{
					"t":           "1717000000001", // 字符串类型
					"ck":          "ck",
					"codeContent": "content",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	m, _, _ := newStubbedManager(t, hc)

	sessionID, _, err := m.GenerateQRCode(context.Background())
	if err != nil {
		t.Fatalf("GenerateQRCode: %v", err)
	}
	if m.sessions[sessionID].params["t"] != "1717000000001" {
		t.Fatalf("字符串 t 处理异常: %q", m.sessions[sessionID].params["t"])
	}
}

// ---- GetSessionStatus ----

func TestGetSessionStatusNotFound(t *testing.T) {
	m := NewManager(nil)
	got := m.GetSessionStatus("missing")
	if got["status"] != "not_found" {
		t.Fatalf("状态异常: %v", got)
	}
}

func TestGetSessionStatusExpired(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID:   "s",
		Status:      "waiting",
		createdTime: time.Now().Add(-10 * time.Minute),
		expireTime:  1 * time.Minute,
	}
	got := m.GetSessionStatus("s")
	if got["status"] != "expired" {
		t.Fatalf("应过期: %v", got)
	}
}

func TestGetSessionStatusSuccessWithCookies(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID:   "s",
		Status:      "success",
		cookies:     map[string]string{"unb": "u1", "c": "v"},
		unb:         "u1",
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
	got := m.GetSessionStatus("s")
	if got["status"] != "success" {
		t.Fatalf("状态异常: %v", got)
	}
	if got["unb"] != "u1" {
		t.Fatalf("unb 异常: %v", got)
	}
	cookies, _ := got["cookies"].(string)
	if !strings.Contains(cookies, "unb=u1") {
		t.Fatalf("cookie 字符串异常: %q", cookies)
	}
}

func TestGetSessionStatusVerificationRequired(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID:       "s",
		Status:          "verification_required",
		verificationURL: "https://verify",
		createdTime:     time.Now(),
		expireTime:      5 * time.Minute,
	}
	got := m.GetSessionStatus("s")
	if got["status"] != "verification_required" {
		t.Fatalf("状态异常: %v", got)
	}
	if got["verification_url"] != "https://verify" {
		t.Fatalf("verification_url 异常: %v", got)
	}
	if got["message"] != "账号被风控，需要手机验证" {
		t.Fatalf("message 异常: %v", got)
	}
}

func TestGetSessionStatusVerificationWithScreenshot(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID:              "s",
		Status:                 "verification_required",
		verificationURL:        "https://verify",
		verificationScreenshot: "data:image/png;base64,abc",
		createdTime:            time.Now(),
		expireTime:             5 * time.Minute,
	}
	got := m.GetSessionStatus("s")
	if got["verification_screenshot"] != "data:image/png;base64,abc" {
		t.Fatalf("screenshot 异常: %v", got)
	}
}

func TestSessionIsExpired(t *testing.T) {
	s := &Session{createdTime: time.Now().Add(-time.Minute), expireTime: time.Second}
	if !s.isExpired() {
		t.Fatal("应判定为过期")
	}
	s2 := &Session{createdTime: time.Now(), expireTime: time.Hour}
	if s2.isExpired() {
		t.Fatal("不应过期")
	}
}

// ---- monitorQRStatus 状态机 ----
//
// monitorQRStatus 在 goroutine 中跑（由 GenerateQRCode 启动），
// 这里直接调用并控制 ctx 取值/超时。为避免 5 分钟阻塞，用极短 ctx 超时。

func newMonitorSession(status string) *Session {
	return &Session{
		SessionID:   "s",
		Status:      status,
		cookies:     map[string]string{},
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
		params:      map[string]string{"t": "1", "ck": "ck"},
	}
}

func statusHandler(status string, cookies ...*http.Cookie) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range cookies {
			http.SetCookie(w, c)
		}
		resp := map[string]any{
			"content": map[string]any{
				"data": map[string]any{
					"qrCodeStatus": status,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func TestMonitorQRStatusConfirmed(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("CONFIRMED",
		&http.Cookie{Name: "unb", Value: "u123"},
		&http.Cookie{Name: "cookie2", Value: "v2"},
	))
	m, _, _ := newStubbedManager(t, hc)

	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "success" {
		t.Fatalf("状态异常: %s", sess.Status)
	}
	if sess.unb != "u123" {
		t.Fatalf("unb 未提取: %q", sess.unb)
	}
	if sess.cookies["cookie2"] != "v2" {
		t.Fatalf("cookie 未合并: %v", sess.cookies)
	}
}

func TestMonitorQRStatusScanned(t *testing.T) {
	hc := &handlerChain{}
	// 第一次返回 SCANED，第二次返回 CONFIRMED。
	var n atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := "SCANED"
		if n.Add(1) >= 2 {
			s = "CONFIRMED"
		}
		resp := map[string]any{
			"content": map[string]any{
				"data": map[string]any{
					"qrCodeStatus": s,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "success" {
		t.Fatalf("最终状态异常: %s", sess.Status)
	}
}

func TestMonitorQRStatusExpired(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("EXPIRED"))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "expired" {
		t.Fatalf("状态异常: %s", sess.Status)
	}
}

func TestMonitorQRStatusExpiredAfterVerificationStaysSuccess(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("EXPIRED"))
	m, _, _ := newStubbedManager(t, hc)
	// 模拟浏览器验证已把状态置为 success。
	sess := newMonitorSession("success")
	m.sessions["s"] = sess

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "success" {
		t.Fatalf("不应把 success 覆盖回 expired: %s", sess.Status)
	}
}

func TestMonitorQRStatusExpiredDuringVerificationContinues(t *testing.T) {
	hc := &handlerChain{}
	var n atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := "EXPIRED"
		if n.Add(1) >= 3 {
			s = "CONFIRMED"
		}
		resp := map[string]any{
			"content": map[string]any{
				"data": map[string]any{
					"qrCodeStatus": s,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("verification_required")
	sess.verificationURL = "https://verify"
	m.sessions["s"] = sess

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "success" {
		t.Fatalf("验证中收到 EXPIRED 应继续，最终 success: %s", sess.Status)
	}
}

func TestMonitorQRStatusCancelled(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("REFUSED"))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "cancelled" {
		t.Fatalf("状态异常: %s", sess.Status)
	}
}

func TestMonitorQRStatusVerificationRequiredTriggersBrowser(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": map[string]any{
				"data": map[string]any{
					"qrCodeStatus":      "CONFIRMED",
					"iframeRedirect":    true,
					"iframeRedirectUrl": "https://verify.example.com",
				},
			},
		}
		// 下发临时 cookie。
		http.SetCookie(w, &http.Cookie{Name: "tmp", Value: "1"})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	m, _, _ := newStubbedManager(t, hc)

	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	var browserCalled atomic.Int64
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		browserCalled.Add(1)
		if !strings.Contains(tmpCookies, "tmp=1") {
			t.Errorf("浏览器刷新器应收到临时 cookie: %q", tmpCookies)
		}
		if verificationURL != "https://verify.example.com" {
			t.Errorf("verificationURL 异常: %q", verificationURL)
		}
		// 触发截图回调。
		onScreenshot("data:image/png;base64,shot")
		return "unb=777; cookie2=z", "777", nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	// 等待浏览器 goroutine 完成。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := sess.snapshot().status
		if st == "success" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if browserCalled.Load() != 1 {
		t.Fatalf("浏览器刷新器应只调用一次，got %d", browserCalled.Load())
	}
	state := sess.snapshot()
	if state.status != "success" {
		t.Fatalf("状态异常: %s", state.status)
	}
	if state.unb != "777" {
		t.Fatalf("unb 异常: %q", state.unb)
	}
	if state.verificationScreenshot != "data:image/png;base64,shot" {
		t.Fatalf("screenshot 未保存: %q", state.verificationScreenshot)
	}
}

func TestMonitorQRStatusMissingSession(t *testing.T) {
	hc := &handlerChain{}
	m, _, _ := newStubbedManager(t, hc)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// session 不存在，应立即返回不阻塞到 ctx 超时。
	start := time.Now()
	m.monitorQRStatus(ctx, "missing")
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("session 不存在应立即返回")
	}
}

func TestMonitorQRStatusInvalidJSONBody(t *testing.T) {
	hc := &handlerChain{}
	var requests atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`not-json`))
	}))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// 用短 ctx 让循环在解析失败后继续直到 ctx 取消。
	// monitorQRStatus 在 ctx 取消时直接返回（不进入 maxWait 超时分支），
	// 因此状态保持 waiting——此测试锁定“解析失败不会崩溃、不误改状态”。
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	m.monitorQRStatus(ctx, "s")
	if sess.Status != "waiting" {
		t.Fatalf("解析失败期间不应改状态: %s", sess.Status)
	}
	if requests.Load() > 2 {
		t.Fatalf("解析失败不应无间隔忙轮询，requests=%d", requests.Load())
	}
}

func TestMonitorQRStatusCtxCancelled(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("NEW"))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	m.monitorQRStatus(ctx, "s")
	// cancel 后状态保持 waiting（超时分支未走到）。
	if sess.Status != "waiting" {
		t.Fatalf("cancel 应保持 waiting: %s", sess.Status)
	}
}

func TestMonitorQRStatusSessionDeletedMidLoop(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("NEW"))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// 在循环开始前删除 session。
	delete(m.sessions, "s")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	m.monitorQRStatus(ctx, "s")
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("session 被删除后应尽快退出")
	}
}

// ---- pollQRCodeStatus ----

func TestPollQRCodeStatusSetsHeadersAndCookie(t *testing.T) {
	hc := &handlerChain{}
	var gotUA, gotCookie, gotCT string
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotCookie = r.Header.Get("Cookie")
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{}`))
	}))
	m, _, _ := newStubbedManager(t, hc)

	sess := newMonitorSession("waiting")
	sess.cookies["k"] = "v"
	resp, err := m.pollQRCodeStatus(context.Background(), sess)
	if err != nil {
		t.Fatalf("pollQRCodeStatus: %v", err)
	}
	defer resp.Body.Close()
	if gotUA == "" {
		t.Fatal("UA 未设置")
	}
	if !strings.Contains(gotCookie, "k=v") {
		t.Fatalf("Cookie 未携带: %q", gotCookie)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type 异常: %q", gotCT)
	}
}

// ---- gzip 解压（历史坑点）----
//
// 注意：当前生产代码 pollQRCodeStatus/monitorQRStatus 直接 json.Unmarshal(body)，
// 闲鱼真实接口会返回 gzip 压缩的响应体。Go http.Client 在 Transport 设置
// DisableCompression=false（默认）时会自动解压 Content-Encoding: gzip 的响应。
// 这里通过 stubTransport（使用 srv.Client()）验证：httptest server 写 gzip body，
// 默认 http.Client 会自动解压，从而 monitorQRStatus 能正确解析 JSON。

func TestMonitorQRStatusGzipBody(t *testing.T) {
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := `{"content":{"data":{"qrCodeStatus":"CONFIRMED"}}}`
		// 闲鱼真实场景：服务端返回 Content-Encoding: gzip + gzip 压缩体。
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(gzipBody(t, payload))
	}))
	m, _, _ := newStubbedManager(t, hc)
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "success" {
		t.Fatalf("gzip 响应未正确解压，状态异常: %s", sess.Status)
	}
}

// ---- CompleteVerification 额外分支 ----

func TestCompleteVerificationNoTmpCookies(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		Status:      "verification_required",
		cookies:     map[string]string{},
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
	_, _, err := m.CompleteVerification(context.Background(), "s")
	if err == nil || !strings.Contains(err.Error(), "无扫码临时 cookie") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestCompleteVerificationHTTPSuccessWithUNB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "unb", Value: "100"})
		http.SetCookie(w, &http.Cookie{Name: "extra", Value: "e"})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	old := qrVerifyTargetURL
	qrVerifyTargetURL = srv.URL
	defer func() { qrVerifyTargetURL = old }()

	cookies, unb, err := m.CompleteVerification(context.Background(), "s")
	if err != nil {
		t.Fatalf("CompleteVerification: %v", err)
	}
	if unb != "100" {
		t.Fatalf("unb 异常: %q", unb)
	}
	if !strings.Contains(cookies, "unb=100") {
		t.Fatalf("cookies 异常: %q", cookies)
	}
	if m.sessions["s"].Status != "success" {
		t.Fatalf("状态异常: %s", m.sessions["s"].Status)
	}
}

func TestCompleteVerificationHTTPFailsWithBrowser(t *testing.T) {
	// 用一个会立即失败的 target URL（端口不存在）。
	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		return "unb=200; c=1", "200", nil
	})
	old := qrVerifyTargetURL
	qrVerifyTargetURL = "http://127.0.0.1:0/im"
	defer func() { qrVerifyTargetURL = old }()

	cookies, unb, err := m.CompleteVerification(context.Background(), "s")
	if err != nil {
		t.Fatalf("CompleteVerification: %v", err)
	}
	if unb != "200" {
		t.Fatalf("unb 异常: %q", unb)
	}
	if !strings.Contains(cookies, "unb=200") {
		t.Fatalf("cookies 异常: %q", cookies)
	}
}

func TestCompleteVerificationBrowserReturnsEmptyCookies(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		return "noequalsign", "", nil // parseCookieStr 解析后为空 map
	})
	old := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = old }()

	_, _, err := m.CompleteVerification(context.Background(), "s")
	if err == nil || !strings.Contains(err.Error(), "未返回有效 cookie") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestCompleteVerificationBrowserFails(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		return "", "", errors.New("browser boom")
	})
	old := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = old }()

	_, _, err := m.CompleteVerification(context.Background(), "s")
	if err == nil || !strings.Contains(err.Error(), "浏览器提取 cookie 失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

func TestCompleteVerificationBrowserNoUNBAnywhere(t *testing.T) {
	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		return "cookie2=abc; cookie3=def", "", nil
	})
	old := qrVerifyTargetURL
	qrVerifyTargetURL = newEmptyCookieServer(t)
	defer func() { qrVerifyTargetURL = old }()

	_, _, err := m.CompleteVerification(context.Background(), "s")
	if err == nil || !strings.Contains(err.Error(), "仍未获取到 unb") {
		t.Fatalf("错误异常: %v", err)
	}
}

// ---- 工具函数 ----

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("短串应原样返回: %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc..." {
		t.Fatalf("长串应截断: %q", got)
	}
}

func TestParseCookieStrEdgeCases(t *testing.T) {
	m := parseCookieStr("a=1; b=2; c=hello world; malformed")
	if m["a"] != "1" || m["b"] != "2" || m["c"] != "hello world" {
		t.Fatalf("解析异常: %v", m)
	}
	if _, ok := m["malformed"]; ok {
		t.Fatal("malformed 不应作为 key")
	}
	empty := parseCookieStr("")
	if len(empty) != 0 {
		t.Fatalf("空串应返回空 map: %v", empty)
	}
}

func TestCookieMarshalRoundTrip(t *testing.T) {
	cookies := map[string]string{"unb": "1", "k": "v"}
	str := cookieMarshal(cookies)
	parsed := parseCookieStr(str)
	if parsed["unb"] != "1" || parsed["k"] != "v" {
		t.Fatalf("往返异常: %v", parsed)
	}
}

func TestMd5hex(t *testing.T) {
	// 已知 MD5: md5("abc") = 900150983cd24fb0d6963f7d28e17f72
	if got := md5hex("abc"); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("md5hex 异常: %q", got)
	}
}

func TestSetBrowserRefresher(t *testing.T) {
	m := NewManager(nil)
	if m.browser != nil {
		t.Fatal("初始 browser 应为 nil")
	}
	called := false
	m.SetBrowserRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
		called = true
		return "", "", nil
	})
	if m.browser == nil {
		t.Fatal("browser 未注入")
	}
	_, _, _ = m.browser(context.Background(), "", "", nil)
	if !called {
		t.Fatal("browser 未调用")
	}
}

func TestNewManagerDefaultsLogger(t *testing.T) {
	m := NewManager(nil)
	if m.logger == nil {
		t.Fatal("logger 不应为 nil")
	}
	if m.httpc == nil {
		t.Fatal("httpc 不应为 nil")
	}
	if m.sessions == nil {
		t.Fatal("sessions map 不应为 nil")
	}
}

// ---- Manager 会话生命周期 ----

func TestManagerSessionLifecycle(t *testing.T) {
	m := NewManager(nil)
	// 直接构造一个 success 会话。
	m.sessions["life"] = &Session{
		SessionID:   "life",
		Status:      "success",
		cookies:     map[string]string{"unb": "1"},
		unb:         "1",
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
	got := m.GetSessionStatus("life")
	if got["status"] != "success" {
		t.Fatalf("GetSessionStatus 异常: %v", got)
	}
	// 删除后查不到。
	delete(m.sessions, "life")
	if m.GetSessionStatus("life")["status"] != "not_found" {
		t.Fatal("删除后应 not_found")
	}
}

// min helper（兼容老 Go 版本）。
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 占位：避免 fmt import 未使用警告（如某些分支调整）。
var _ = fmt.Sprintf
