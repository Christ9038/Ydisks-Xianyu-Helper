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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// stubTransport 把所有请求转发到 httptest.Server，保留路径与查询，
// 从而让测试可以 mock 固定 host（passport.goofish.com 等）的接口。
// stubTransport 保存stubTransport，供当前处理流程使用
type stubTransport struct {
	server *httptest.Server
	calls  atomic.Int64
}

// RoundTrip 负责RoundTrip相关处理。
func (t *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	// 复制请求到测试服务器，保留方法/头部/正文。
	outReq := req.Clone(req.Context())
	outReq.URL.Scheme = "http"
	outReq.URL.Host = t.server.URL[len("http://"):]
	outReq.RequestURI = ""
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := t.server.Client().Do(outReq)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// newStubbedManager 构造一个使用 stubTransport 的 Manager，返回 manager 与 server。
func newStubbedManager(t *testing.T, handler http.Handler) (*Manager, *httptest.Server, *stubTransport) {
	t.Helper()
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(handler)
	// tr 保存tr，供当前处理流程使用
	tr := &stubTransport{server: srv}
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.httpc = &http.Client{Timeout: 10 * time.Second, Transport: tr}
	t.Cleanup(srv.Close)
	return m, srv, tr
}

// TestAbsorbSessionResponsePreservesCookieScope 负责TestAbsorb会话响应Preserves登录凭证Scope相关处理。
func TestAbsorbSessionResponsePreservesCookieScope(t *testing.T) {
	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{}, cookieSnapshot: []cookierefresh.BrowserCookie{}}
	// resp 保存resp，供当前处理流程使用
	resp := &http.Response{Header: http.Header{
		"Set-Cookie": {"_m_h5_tk=tok_1; Domain=.goofish.com; Path=/; Secure; HttpOnly"},
	}}
	absorbSessionResponse(sess, apiH5TK, resp)
	if // got 保存got，供当前处理流程使用
	got := sessionCookieHeader(sess, apiGenerateQR); !strings.Contains(got, "_m_h5_tk=tok_1") {
		t.Fatalf("跨子域 Cookie 作用域丢失: header=%q snapshot=%+v", got, sess.cookieSnapshot)
	}
}

// TestAbsorbSessionResponseDeletesExpiredFlatCookie 负责TestAbsorb会话响应DeletesExpiredFlat登录凭证相关处理。
func TestAbsorbSessionResponseDeletesExpiredFlatCookie(t *testing.T) {
	// sess 保存sess，供当前处理流程使用
	sess := &Session{
		cookies:        map[string]string{"unb": "stale", "keep": "yes"},
		cookieSnapshot: []cookierefresh.BrowserCookie{},
		unb:            "stale",
	}
	// resp 保存resp，供当前处理流程使用
	resp := &http.Response{Header: http.Header{
		"Set-Cookie": {"unb=; Domain=.goofish.com; Path=/; Max-Age=0; Expires=Thu, 01 Jan 1970 00:00:00 GMT"},
	}}
	absorbSessionResponse(sess, apiScanStatus, resp)
	if // exists 保存exists，供当前处理流程使用
	_, exists := sess.cookies["unb"]; exists || sess.unb != "" {
		t.Fatalf("服务端删除 Cookie 后扁平兼容状态仍残留 unb: cookies=%v unb=%q", sess.cookies, sess.unb)
	}
	if sess.cookies["keep"] != "yes" {
		t.Fatalf("删除 unb 时不得丢失无关 Cookie: %v", sess.cookies)
	}
}

// gzipBody 用 gzip 压缩输入字符串。
func gzipBody(t *testing.T, raw string) []byte {
	t.Helper()
	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// gz 保存gz，供当前处理流程使用
	gz := gzip.NewWriter(&buf)
	if // err 保存err，供当前处理流程使用
	_, err := gz.Write([]byte(raw)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := gz.Close(); err != nil {
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

// handle 负责handle相关处理。
func (h *handlerChain) handle(path string, fn http.Handler) *handlerChain {
	if h.routes == nil {
		h.routes = make(map[string]http.Handler)
	}
	h.routes[path] = fn
	return h
}

// ServeHTTP 提供HTTP。
func (h *handlerChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// 用 path 前缀匹配：去掉查询串后比较 path。
	p := r.URL.Path
	// prefix、fn 表示当前遍历过程中的prefix、fn
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

// TestGetMH5TKCarriesInitialCookiesIntoSignedPost 负责TestGetMH5TKCarriesInitialCookiesIntoSignedPost相关处理。
func TestGetMH5TKCarriesInitialCookiesIntoSignedPost(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// postCookie 保存post登录凭证，供当前处理流程使用
	var postCookie string
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "abc_token_1717000000000"})
			http.SetCookie(w, &http.Cookie{Name: "other", Value: "v1"})
		} else {
			postCookie = r.Header.Get("Cookie")
			http.SetCookie(w, &http.Cookie{Name: "post_only", Value: "must-persist"})
		}
		w.WriteHeader(http.StatusOK)
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	if // err 保存err，供当前处理流程使用
	err := m.getMH5TK(context.Background(), sess); err != nil {
		t.Fatalf("getMH5TK: %v", err)
	}
	if sess.cookies["_m_h5_tk"] != "abc_token_1717000000000" {
		t.Fatalf("cookie 未合并: %v", sess.cookies)
	}
	if sess.cookies["other"] != "v1" {
		t.Fatalf("other cookie 未合并: %v", sess.cookies)
	}
	if !strings.Contains(postCookie, "_m_h5_tk=abc_token_1717000000000") || !strings.Contains(postCookie, "other=v1") {
		t.Fatalf("签名 POST 未携带首次请求 Cookie: %q", postCookie)
	}
	if sess.cookies["post_only"] != "must-persist" {
		t.Fatalf("签名 POST 响应 Cookie 必须写回扫码会话: %v", sess.cookies)
	}
}

// TestGetMH5TKErrorOnFirstRequest 负责TestGetMH5TK错误OnFirst请求相关处理。
func TestGetMH5TKErrorOnFirstRequest(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{
		fallback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	}
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	// 5xx 不会让 httpc.Do 报错，但 io.Copy 不报错；getMH5TK 仍会成功，
	// 只是拿不到 cookie。验证它不 panic 即可（且无 _m_h5_tk）。
	if // err 保存err，供当前处理流程使用
	err := m.getMH5TK(context.Background(), sess); err != nil {
		t.Fatalf("5xx 不应让 getMH5TK 报错: %v", err)
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := sess.cookies["_m_h5_tk"]; ok {
		t.Fatalf("不应有 _m_h5_tk cookie")
	}
}

// TestGetMH5TKTransportError 负责TestGetMH5TKTransport错误相关处理。
func TestGetMH5TKTransportError(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	// 用一个会立即返回 transport 错误的 client。
	m.httpc = &http.Client{Timeout: 1 * time.Nanosecond, Transport: &errTransport{}}
	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	if // err 保存err，供当前处理流程使用
	err := m.getMH5TK(context.Background(), sess); err == nil {
		t.Fatal("transport 错误应导致 getMH5TK 失败")
	}
}

// errTransport 保存errTransport，供当前处理流程使用
type errTransport struct{}

// RoundTrip 负责RoundTrip相关处理。
func (errTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

// ---- getLoginParams ----

// viewDataHTML 保存view数据HTML，供当前处理流程使用
const viewDataHTML = `<html><script>window.viewData = {"loginFormData":{"appName":"xianyu","appEntrance":"web","isMobile":false,"numField":123,"boolTrue":true}};</script></html>`

// TestGetLoginParamsParsesViewData 负责TestGet登录ParamsParsesView数据相关处理。
func TestGetLoginParamsParsesViewData(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// gotRnd 保存gotRnd，供当前处理流程使用
	var gotRnd string
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRnd = r.URL.Query().Get("rnd")
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// originalRandFloat 保存originalRandFloat，供当前处理流程使用
	originalRandFloat := randFloat
	randFloat = func() float64 { return 0.1234567890123456 }
	t.Cleanup(func() { randFloat = originalRandFloat })

	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{"_m_h5_tk": "tk_123"}, params: map[string]string{}}
	// params、err 保存params、err，供当前处理流程使用
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
	if gotRnd != "0.1234567890123456" {
		t.Fatalf("rnd 应保留参考实现的完整随机精度: %q", gotRnd)
	}
}

// TestGetLoginParamsMissingViewData 负责TestGet登录ParamsMissingView数据相关处理。
func TestGetLoginParamsMissingViewData(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>no viewData here</html>`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	// err 保存err，供当前处理流程使用
	_, err := m.getLoginParams(context.Background(), sess)
	if err == nil || !strings.Contains(err.Error(), "未找到 viewData") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestGetLoginParamsInvalidJSON 负责TestGet登录ParamsInvalidJSON相关处理。
func TestGetLoginParamsInvalidJSON(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script>window.viewData = {not valid json};</script>`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	// err 保存err，供当前处理流程使用
	_, err := m.getLoginParams(context.Background(), sess)
	if err == nil || !strings.Contains(err.Error(), "解析 viewData 失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestGetLoginParamsEmptyLoginFormData 负责TestGet登录ParamsEmpty登录表单数据相关处理。
func TestGetLoginParamsEmptyLoginFormData(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script>window.viewData = {"loginFormData":null};</script>`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sess 保存sess，供当前处理流程使用
	sess := &Session{cookies: map[string]string{}, params: map[string]string{}}
	// err 保存err，供当前处理流程使用
	_, err := m.getLoginParams(context.Background(), sess)
	if err == nil || !strings.Contains(err.Error(), "loginFormData 为空") {
		t.Fatalf("错误异常: %v", err)
	}
}

// ---- GenerateQRCode 端到端 ----

// TestGenerateQRCodeSuccess 负责TestGenerateQRCodeSuccess相关处理。
func TestGenerateQRCodeSuccess(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// generateCookie 保存generate登录凭证，供当前处理流程使用
	var generateCookie string
	// getMH5TK 路径
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "_m_h5_tk=tok_1717; Domain=.goofish.com; Path=/; Secure; HttpOnly")
		w.WriteHeader(http.StatusOK)
	}))
	// getLoginParams 路径
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	// generate.do 二维码接口
	hc.handle("/newlogin/qrcode/generate.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		generateCookie = r.Header.Get("Cookie")
		// resp 保存resp，供当前处理流程使用
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
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sessionID、qrCodeURL、err 保存会话ID、qrCodeURL、err，供当前处理流程使用
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
	if !strings.Contains(generateCookie, "_m_h5_tk=tok_1717") {
		t.Fatalf("生成二维码请求未继承当前 Cookie: %q snapshot=%+v cookies=%v", generateCookie, sess.cookieSnapshot, sess.cookies)
	}
}

// TestGenerateQRCodeFailureResponse 负责TestGenerateQRCodeFailure响应相关处理。
func TestGenerateQRCodeFailureResponse(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
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
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// err 保存err，供当前处理流程使用
	_, _, err := m.GenerateQRCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "获取登录二维码失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestGenerateQRCodeInvalidJSON 负责TestGenerateQRCodeInvalidJSON相关处理。
func TestGenerateQRCodeInvalidJSON(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
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
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// err 保存err，供当前处理流程使用
	_, _, err := m.GenerateQRCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "解析二维码响应失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestGenerateQRCodeTAsString 负责TestGenerateQRCodeTAsString相关处理。
func TestGenerateQRCodeTAsString(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tok"})
		w.WriteHeader(http.StatusOK)
	}))
	hc.handle("/mini_login.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(viewDataHTML))
	}))
	hc.handle("/newlogin/qrcode/generate.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// resp 保存resp，供当前处理流程使用
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
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sessionID、err 保存会话ID、err，供当前处理流程使用
	sessionID, _, err := m.GenerateQRCode(context.Background())
	if err != nil {
		t.Fatalf("GenerateQRCode: %v", err)
	}
	if m.sessions[sessionID].params["t"] != "1717000000001" {
		t.Fatalf("字符串 t 处理异常: %q", m.sessions[sessionID].params["t"])
	}
}

// ---- GetSessionStatus ----

// TestGetSessionStatusNotFound 负责TestGet会话状态NotFound相关处理。
func TestGetSessionStatusNotFound(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	// got 保存got，供当前处理流程使用
	got := m.GetSessionStatus("missing")
	if got["status"] != "not_found" {
		t.Fatalf("状态异常: %v", got)
	}
}

// TestGetSessionStatusExpired 负责TestGet会话状态Expired相关处理。
func TestGetSessionStatusExpired(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID:   "s",
		Status:      "waiting",
		createdTime: time.Now().Add(-10 * time.Minute),
		expireTime:  1 * time.Minute,
	}
	// got 保存got，供当前处理流程使用
	got := m.GetSessionStatus("s")
	if got["status"] != "expired" {
		t.Fatalf("应过期: %v", got)
	}
}

// TestGetSessionStatusSuccessWithCookies 负责TestGet会话状态SuccessWithCookies相关处理。
func TestGetSessionStatusSuccessWithCookies(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID: "s",
		Status:    "success",
		cookies:   map[string]string{"unb": "u1", "c": "v"},
		cookieSnapshot: []cookierefresh.BrowserCookie{
			{Name: "unb", Value: "u1", Domain: ".goofish.com", Path: "/", Secure: true, HTTPOnly: true},
			{Name: "scoped", Value: "v2", Domain: "www.goofish.com", Path: "/im", Secure: true},
		},
		unb:         "u1",
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
	// got 保存got，供当前处理流程使用
	got := m.GetSessionStatus("s")
	if got["status"] != "success" {
		t.Fatalf("状态异常: %v", got)
	}
	if got["unb"] != "u1" {
		t.Fatalf("unb 异常: %v", got)
	}
	// cookies 保存cookies，供当前处理流程使用
	cookies, _ := got["cookies"].(string)
	if !strings.Contains(cookies, "unb=u1") {
		t.Fatalf("cookie 字符串异常: %q", cookies)
	}
	// snapshot、ok 保存snapshot、ok，供当前处理流程使用
	snapshot, ok := got["cookie_snapshot"].([]cookierefresh.BrowserCookie)
	if !ok || len(snapshot) != 2 {
		t.Fatalf("成功状态必须返回内部权威 Cookie Jar: ok=%v snapshot=%+v", ok, snapshot)
	}
}

// TestGetSessionStatusUsesAuthoritativeScopedCookieHeader 负责TestGet会话状态UsesAuthoritativeScoped登录凭证Header相关处理。
func TestGetSessionStatusUsesAuthoritativeScopedCookieHeader(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID: "s", Status: "success", cookies: map[string]string{"unb": "u1", "same": "flat"}, unb: "u1",
		cookieSnapshot: []cookierefresh.BrowserCookie{
			{Name: "unb", Value: "u1", Domain: ".goofish.com", Path: "/", Secure: true},
			{Name: "same", Value: "im", Domain: "www.goofish.com", Path: "/im", Secure: true},
			{Name: "same", Value: "root", Domain: ".goofish.com", Path: "/", Secure: true},
		},
		createdTime: time.Now(), expireTime: 5 * time.Minute,
	}
	// got 保存got，供当前处理流程使用
	got := m.GetSessionStatus("s")
	// cookies 保存cookies，供当前处理流程使用
	cookies, _ := got["cookies"].(string)
	if strings.Count(cookies, "same=") != 2 || strings.Index(cookies, "same=im") > strings.Index(cookies, "same=root") {
		t.Fatalf("同名不同 Path Cookie 被扁平化: %q", cookies)
	}
}

// TestGetSessionStatusVerificationRequired 负责TestGet会话状态VerificationRequired相关处理。
func TestGetSessionStatusVerificationRequired(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID:       "s",
		Status:          "verification_required",
		verificationURL: "https://verify",
		createdTime:     time.Now(),
		expireTime:      5 * time.Minute,
	}
	// got 保存got，供当前处理流程使用
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

// TestGetSessionStatusVerificationWithScreenshot 负责TestGet会话状态VerificationWithScreenshot相关处理。
func TestGetSessionStatusVerificationWithScreenshot(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		SessionID:              "s",
		Status:                 "verification_required",
		verificationURL:        "https://verify",
		verificationScreenshot: "data:image/png;base64,abc",
		createdTime:            time.Now(),
		expireTime:             5 * time.Minute,
	}
	// got 保存got，供当前处理流程使用
	got := m.GetSessionStatus("s")
	if got["verification_screenshot"] != "data:image/png;base64,abc" {
		t.Fatalf("screenshot 异常: %v", got)
	}
}

// TestSessionIsExpired 负责Test会话IsExpired相关处理。
func TestSessionIsExpired(t *testing.T) {
	// s 保存s，供当前处理流程使用
	s := &Session{createdTime: time.Now().Add(-time.Minute), expireTime: time.Second}
	if !s.isExpired() {
		t.Fatal("应判定为过期")
	}
	// s2 保存s2，供当前处理流程使用
	s2 := &Session{createdTime: time.Now(), expireTime: time.Hour}
	if s2.isExpired() {
		t.Fatal("不应过期")
	}
}

// ---- monitorQRStatus 状态机 ----
//
// monitorQRStatus 在 goroutine 中跑（由 GenerateQRCode 启动），
// 这里直接调用并控制 ctx 取值/超时。为避免 5 分钟阻塞，用极短 ctx 超时。

// newMonitorSession 负责newMonitor会话相关处理。
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

// statusHandler 负责状态Handler相关处理。
func statusHandler(status string, cookies ...*http.Cookie) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c 表示当前遍历过程中的c
		for _, c := range cookies {
			http.SetCookie(w, c)
		}
		// resp 保存resp，供当前处理流程使用
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

// TestMonitorQRStatusConfirmed 负责TestMonitorQR状态Confirmed相关处理。
func TestMonitorQRStatusConfirmed(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("CONFIRMED",
		&http.Cookie{Name: "unb", Value: "u123"},
		&http.Cookie{Name: "cookie2", Value: "v2"},
	))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
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

// TestMonitorQRStatusConfirmedCollectsCookiesFromFinalIMNavigation 负责TestMonitorQR状态ConfirmedCollectsCookiesFromFinalIMNavigation相关处理。
func TestMonitorQRStatusConfirmedCollectsCookiesFromFinalIMNavigation(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("CONFIRMED",
		&http.Cookie{Name: "unb", Value: "u123", Domain: ".goofish.com", Path: "/", Secure: true},
		&http.Cookie{Name: "cookie2", Value: "v2", Domain: ".goofish.com", Path: "/", Secure: true},
	))
	hc.handle("/im", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "unb=u123") {
			t.Fatalf("最终登录跳转未携带扫码 Cookie: %q", r.Header.Get("Cookie"))
		}
		_, _ = w.Write([]byte("ok"))
	}))
	hc.handle("/ac/account/setLoginSettings.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if // err 保存err，供当前处理流程使用
		err := r.ParseForm(); err != nil || r.Form.Get("status") != "0" {
			t.Fatalf("保持登录参数异常: form=%v err=%v", r.Form, err)
		}
		http.SetCookie(w, &http.Cookie{
			Name: "havana_lgc_exp", Value: strconv.FormatInt(time.Now().Add(24*time.Hour).UnixMilli(), 10),
			Domain: ".goofish.com", Path: "/", Secure: true, HttpOnly: true,
		})
		_, _ = w.Write([]byte(`{"data":{"success":true}}`))
	}))
	hc.handle("/ac/account/queryLoginSettings.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "havana_lgc_exp=") {
			t.Fatalf("查询保持登录状态未携带新凭证: %q", r.Header.Get("Cookie"))
		}
		_, _ = w.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	sess.cookieSnapshot = []cookierefresh.BrowserCookie{}
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	// state 保存状态，供当前处理流程使用
	state := sess.snapshot()
	if state.status != "success" || state.cookies["havana_lgc_exp"] == "" {
		t.Fatalf("最终登录跳转 Cookie 未保存: status=%s cookies=%v", state.status, state.cookies)
	}
	// longLogin 保存long登录，供当前处理流程使用
	var longLogin *cookierefresh.BrowserCookie
	// i 表示当前遍历过程中的i
	for i := range state.cookieSnapshot {
		if state.cookieSnapshot[i].Name == "havana_lgc_exp" {
			longLogin = &state.cookieSnapshot[i]
			break
		}
	}
	if longLogin == nil || !longLogin.HTTPOnly || longLogin.Domain != ".goofish.com" {
		t.Fatalf("长登录 Cookie 属性未完整保留: %+v", longLogin)
	}
}

// TestMonitorQRStatusScanned 负责TestMonitorQR状态Scanned相关处理。
func TestMonitorQRStatusScanned(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// 第一次返回 SCANED，第二次返回 CONFIRMED。
	var n atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// s 保存s，供当前处理流程使用
		s := "SCANED"
		if n.Add(1) >= 2 {
			s = "CONFIRMED"
		}
		// resp 保存resp，供当前处理流程使用
		resp := map[string]any{
			"content": map[string]any{
				"data": map[string]any{
					"qrCodeStatus": s,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "success" {
		t.Fatalf("最终状态异常: %s", sess.Status)
	}
}

// TestMonitorQRStatusExpired 负责TestMonitorQR状态Expired相关处理。
func TestMonitorQRStatusExpired(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("EXPIRED"))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "expired" {
		t.Fatalf("状态异常: %s", sess.Status)
	}
}

// TestMonitorQRStatusExpiredIsTerminal 负责TestMonitorQR状态ExpiredIsTerminal相关处理。
func TestMonitorQRStatusExpiredIsTerminal(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("EXPIRED"))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// 参考状态机收到 EXPIRED 后无条件进入终态；真实人脸验证分支已停止本轮询。
	sess := newMonitorSession("success")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "expired" {
		t.Fatalf("EXPIRED 应进入过期终态: %s", sess.Status)
	}
}

// TestMonitorQRStatusExpiredDoesNotKeepPolling 负责TestMonitorQR状态ExpiredDoesNotKeepPolling相关处理。
func TestMonitorQRStatusExpiredDoesNotKeepPolling(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// n 保存n，供当前处理流程使用
	var n atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		// resp 保存resp，供当前处理流程使用
		resp := map[string]any{
			"content": map[string]any{
				"data": map[string]any{
					"qrCodeStatus": "EXPIRED",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("verification_required")
	sess.verificationURL = "https://verify"
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "expired" || n.Load() != 1 {
		t.Fatalf("EXPIRED 应立即停止轮询: status=%s calls=%d", sess.Status, n.Load())
	}
}

// TestMonitorQRStatusCancelled 负责TestMonitorQR状态Cancelled相关处理。
func TestMonitorQRStatusCancelled(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("CANCELED"))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "cancelled" {
		t.Fatalf("状态异常: %s", sess.Status)
	}
}

// TestMonitorQRStatusServerHasErrorStopsAfterFiveImmediateRetries 负责TestMonitorQR状态ServerHas错误StopsAfterFiveImmediateRetries相关处理。
func TestMonitorQRStatusServerHasErrorStopsAfterFiveImmediateRetries(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"hasError":true}`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "error" {
		t.Fatalf("连续业务错误应进入 error 状态: %s", sess.Status)
	}
	if // got 保存got，供当前处理流程使用
	got := calls.Load(); got != maxQRServerErrors {
		t.Fatalf("业务错误重试次数=%d, want %d", got, maxQRServerErrors)
	}
}

// TestMonitorQRStatusUnknownStatusKeepsPolling 负责TestMonitorQR状态Unknown状态KeepsPolling相关处理。
func TestMonitorQRStatusUnknownStatusKeepsPolling(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("FUTURE_STATUS"))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	m.monitorQRStatus(ctx, "s")
	if sess.Status != "waiting" {
		t.Fatalf("未知状态不应被推断成取消: %s", sess.Status)
	}
}

// TestMonitorQRStatusVerificationRequiredStopsPollingAndRunsFaceFlow 负责TestMonitorQR状态VerificationRequiredStopsPollingAnd运行记录FaceFlow相关处理。
func TestMonitorQRStatusVerificationRequiredStopsPollingAndRunsFaceFlow(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// queryCalls 保存查询Calls，供当前处理流程使用
	var queryCalls atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCalls.Add(1)
		// resp 保存resp，供当前处理流程使用
		resp := map[string]any{
			"content": map[string]any{
				"data": map[string]any{
					"qrCodeStatus":      "CONFIRMED",
					"iframeRedirect":    true,
					"iframeRedirectUrl": "https://passport.goofish.com/iv/mini/normal_validate.htm?htoken=face-token",
				},
			},
		}
		// 下发临时 cookie。
		http.SetCookie(w, &http.Cookie{Name: "tmp", Value: "1"})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	hc.handle("/iv/mini/normal_validate.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script>window.location.href = "https://passport.goofish.com/iv/mini/verify_modes.htm?htoken=face-token&_umidfg=";</script>`))
	}))
	hc.handle("/iv/mini/verify_modes.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script>new Qrcode({ text: "https:\/\/passport.goofish.com\/face?token=1" });</script>`))
	}))
	hc.handle("/iv/photoVerify/check.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":{"code":3,"url":"https://passport.goofish.com/ivCheckLogin.htm?ok=1"}}`))
	}))
	hc.handle("/ivCheckLogin.htm", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "unb", Value: "777", Domain: ".goofish.com", Path: "/", Secure: true, HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "cookie2", Value: "z", Domain: ".goofish.com", Path: "/", Secure: true})
		_, _ = w.Write([]byte(`ok`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	sess.cookieSnapshot = []cookierefresh.BrowserCookie{}
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	// 二维码轮询应立即退出，独立人脸验证任务继续完成登录。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// st 保存st，供当前处理流程使用
		st := sess.snapshot().status
		if st == "success" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if queryCalls.Load() != 1 {
		t.Fatalf("进入人脸验证后不应继续 query.do 轮询, got %d", queryCalls.Load())
	}
	// state 保存状态，供当前处理流程使用
	state := sess.snapshot()
	if state.status != "success" {
		t.Fatalf("状态异常: %s", state.status)
	}
	if state.unb != "777" {
		t.Fatalf("unb 异常: %q", state.unb)
	}
	if state.faceQRURL == "" {
		t.Fatal("人脸验证二维码未生成")
	}
	if state.cookieSnapshot == nil {
		t.Fatal("人脸验证跳转链必须保留权威 Cookie Jar")
	}
	// foundUNB 保存foundUNB，供当前处理流程使用
	var foundUNB bool
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range state.cookieSnapshot {
		if cookie.Name == "unb" && cookie.Value == "777" && cookie.Domain == ".goofish.com" && cookie.HTTPOnly && cookie.Secure {
			foundUNB = true
		}
	}
	if !foundUNB {
		t.Fatalf("人脸验证 Cookie 属性丢失: %+v", state.cookieSnapshot)
	}
}

// TestMonitorQRStatusMissingSession 负责TestMonitorQR状态Missing会话相关处理。
func TestMonitorQRStatusMissingSession(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// session 不存在，应立即返回不阻塞到 ctx 超时。
	start := time.Now()
	m.monitorQRStatus(ctx, "missing")
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("session 不存在应立即返回")
	}
}

// TestMonitorQRStatusInvalidJSONBody 负责TestMonitorQR状态InvalidJSON请求体相关处理。
func TestMonitorQRStatusInvalidJSONBody(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// requests 保存请求列表，供当前处理流程使用
	var requests atomic.Int64
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`not-json`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// 用短 ctx 让循环在解析失败后继续直到 ctx 取消。
	// monitorQRStatus 在 ctx 取消时直接返回（不进入 maxWait 超时分支），
	// 因此状态保持 waiting——此测试锁定“解析失败不会崩溃、不误改状态”。
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
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

// TestMonitorQRStatusCtxCancelled 负责TestMonitorQR状态CtxCancelled相关处理。
func TestMonitorQRStatusCtxCancelled(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("NEW"))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
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

// TestMonitorQRStatusSessionDeletedMidLoop 负责TestMonitorQR状态会话DeletedMidLoop相关处理。
func TestMonitorQRStatusSessionDeletedMidLoop(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", statusHandler("NEW"))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// 在循环开始前删除 session。
	delete(m.sessions, "s")
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// start 保存开始，供当前处理流程使用
	start := time.Now()
	m.monitorQRStatus(ctx, "s")
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("session 被删除后应尽快退出")
	}
}

// ---- pollQRCodeStatus ----

// TestPollQRCodeStatusSetsHeadersAndCookie 负责TestPollQRCode状态SetsHeadersAnd登录凭证相关处理。
func TestPollQRCodeStatusSetsHeadersAndCookie(t *testing.T) {
	// oldFingerprint 保存oldFingerprint，供当前处理流程使用
	oldFingerprint := xianyu.CurrentBrowserFingerprint()
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: "playwright-native-ua", Platform: "macOS"})
	t.Cleanup(func() { xianyu.SetBrowserFingerprint(oldFingerprint) })
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	// gotUA、gotCookie、gotCT 保存gotUA、gotCookie、gotCT，供当前处理流程使用
	var gotUA, gotCookie, gotCT string
	// gotForm 保存got表单，供当前处理流程使用
	var gotForm url.Values
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotCookie = r.Header.Get("Cookie")
		gotCT = r.Header.Get("Content-Type")
		if // err 保存err，供当前处理流程使用
		err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{}`))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)

	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	sess.cookies["k"] = "v"
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := m.pollQRCodeStatus(context.Background(), sess)
	if err != nil {
		t.Fatalf("pollQRCodeStatus: %v", err)
	}
	defer resp.Body.Close()
	if gotUA != "playwright-native-ua" {
		t.Fatalf("扫码请求 UA 未与参考实现一致: %q", gotUA)
	}
	if !strings.Contains(gotCookie, "k=v") {
		t.Fatalf("Cookie 未携带: %q", gotCookie)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type 异常: %q", gotCT)
	}
	// wantForm 保存want表单，供当前处理流程使用
	wantForm := map[string]string{
		"ua":              "",
		"navlanguage":     "zh-CN",
		"navUserAgent":    "playwright-native-ua",
		"navPlatform":     "MacIntel",
		"isIframe":        "true",
		"documentReferer": qrVerifyTargetURL,
		"defaultView":     "qrcode",
	}
	// key、want 表示当前遍历过程中的key、want
	for key, want := range wantForm {
		if // got 保存got，供当前处理流程使用
		got := gotForm.Get(key); got != want {
			t.Errorf("轮询字段 %s=%q, want %q", key, got, want)
		}
	}
}

// ---- gzip 解压（历史坑点）----
//
// 注意：当前生产代码 pollQRCodeStatus/monitorQRStatus 直接 json.Unmarshal(body)，
// 闲鱼真实接口会返回 gzip 压缩的响应体。Go http.Client 在 Transport 设置
// DisableCompression=false（默认）时会自动解压 Content-Encoding: gzip 的响应。
// 这里通过 stubTransport（使用 srv.Client()）验证：httptest server 写 gzip body，
// 默认 http.Client 会自动解压，从而 monitorQRStatus 能正确解析 JSON。

// TestMonitorQRStatusGzipBody 负责TestMonitorQR状态Gzip请求体相关处理。
func TestMonitorQRStatusGzipBody(t *testing.T) {
	// hc 保存hc，供当前处理流程使用
	hc := &handlerChain{}
	hc.handle("/newlogin/qrcode/query.do", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// payload 保存请求载荷，供当前处理流程使用
		payload := `{"content":{"data":{"qrCodeStatus":"CONFIRMED"}}}`
		// 闲鱼真实场景：服务端返回 Content-Encoding: gzip + gzip 压缩体。
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(gzipBody(t, payload))
	}))
	// m 保存m，供当前处理流程使用
	m, _, _ := newStubbedManager(t, hc)
	// sess 保存sess，供当前处理流程使用
	sess := newMonitorSession("waiting")
	m.sessions["s"] = sess

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.monitorQRStatus(ctx, "s")

	if sess.Status != "success" {
		t.Fatalf("gzip 响应未正确解压，状态异常: %s", sess.Status)
	}
}

// ---- CompleteVerification 额外分支 ----

// TestCompleteVerificationNoTmpCookies 负责TestCompleteVerificationNoTmpCookies相关处理。
func TestCompleteVerificationNoTmpCookies(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = &Session{
		Status:      "verification_required",
		cookies:     map[string]string{},
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
	}
	// err 保存err，供当前处理流程使用
	_, _, err := m.CompleteVerification(context.Background(), "s")
	if err == nil || !strings.Contains(err.Error(), "无扫码临时 cookie") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestCompleteVerificationHTTPSuccessWithUNB 负责TestCompleteVerificationHTTPSuccessWithUNB相关处理。
func TestCompleteVerificationHTTPSuccessWithUNB(t *testing.T) {
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "unb", Value: "100"})
		http.SetCookie(w, &http.Cookie{Name: "extra", Value: "e"})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	// old 保存old，供当前处理流程使用
	old := qrVerifyTargetURL
	qrVerifyTargetURL = srv.URL
	defer func() { qrVerifyTargetURL = old }()

	// cookies、unb、err 保存cookies、unb、err，供当前处理流程使用
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

// TestCompleteVerificationHTTPFailureDoesNotUseBrowser 负责TestCompleteVerificationHTTPFailureDoesNotUse浏览器相关处理。
func TestCompleteVerificationHTTPFailureDoesNotUseBrowser(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	// old 保存old，供当前处理流程使用
	old := qrVerifyTargetURL
	qrVerifyTargetURL = "http://127.0.0.1:0/im"
	defer func() { qrVerifyTargetURL = old }()

	// err 保存err，供当前处理流程使用
	_, _, err := m.CompleteVerification(context.Background(), "s")
	if err == nil || !strings.Contains(err.Error(), "换取登录凭证失败") {
		t.Fatalf("错误异常: %v", err)
	}
}

// TestCompleteVerificationCarriesCookiesAcrossRedirects 负责TestCompleteVerificationCarriesCookiesAcrossRedirects相关处理。
func TestCompleteVerificationCarriesCookiesAcrossRedirects(t *testing.T) {
	// srv 保存srv，供当前处理流程使用
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.SetCookie(w, &http.Cookie{Name: "redirect_anchor", Value: "1", Path: "/"})
			http.Redirect(w, r, srv.URL+"/done", http.StatusFound)
		case "/done":
			// cookie、err 保存cookie、err，供当前处理流程使用
			cookie, err := r.Cookie("redirect_anchor")
			if err != nil || cookie.Value != "1" {
				t.Fatalf("重定向未携带中间 Cookie: cookie=%v err=%v", cookie, err)
			}
			http.SetCookie(w, &http.Cookie{Name: "unb", Value: "redirect-account", Path: "/"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	// m 保存m，供当前处理流程使用
	m := NewManager(nil)
	m.sessions["s"] = testVerificationSession()
	// old 保存old，供当前处理流程使用
	old := qrVerifyTargetURL
	qrVerifyTargetURL = srv.URL + "/start"
	defer func() { qrVerifyTargetURL = old }()

	// cookies、unb、err 保存cookies、unb、err，供当前处理流程使用
	cookies, unb, err := m.CompleteVerification(context.Background(), "s")
	if err != nil || unb != "redirect-account" || !strings.Contains(cookies, "redirect_anchor=1") {
		t.Fatalf("纯 Go 重定向 Cookie Jar 异常: cookies=%q unb=%q err=%v", cookies, unb, err)
	}
}

// ---- 工具函数 ----

// TestTruncate 负责TestTruncate相关处理。
func TestTruncate(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := truncate("abc", 5); got != "abc" {
		t.Fatalf("短串应原样返回: %q", got)
	}
	if // got 保存got，供当前处理流程使用
	got := truncate("abcdef", 3); got != "abc..." {
		t.Fatalf("长串应截断: %q", got)
	}
}

// TestParseCookieStrEdgeCases 负责TestParse登录凭证StrEdgeCases相关处理。
func TestParseCookieStrEdgeCases(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := parseCookieStr("a=1; b=2; c=hello world; malformed")
	if m["a"] != "1" || m["b"] != "2" || m["c"] != "hello world" {
		t.Fatalf("解析异常: %v", m)
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := m["malformed"]; ok {
		t.Fatal("malformed 不应作为 key")
	}
	// empty 保存empty，供当前处理流程使用
	empty := parseCookieStr("")
	if len(empty) != 0 {
		t.Fatalf("空串应返回空 map: %v", empty)
	}
}

// TestCookieMarshalRoundTrip 负责Test登录凭证MarshalRoundTrip相关处理。
func TestCookieMarshalRoundTrip(t *testing.T) {
	// cookies 保存cookies，供当前处理流程使用
	cookies := map[string]string{"unb": "1", "k": "v"}
	// str 保存str，供当前处理流程使用
	str := cookieMarshal(cookies)
	// parsed 保存解析结果，供当前处理流程使用
	parsed := parseCookieStr(str)
	if parsed["unb"] != "1" || parsed["k"] != "v" {
		t.Fatalf("往返异常: %v", parsed)
	}
}

// TestMd5hex 负责TestMd5hex相关处理。
func TestMd5hex(t *testing.T) {
	// 已知 MD5: md5("abc") = 900150983cd24fb0d6963f7d28e17f72
	if got := md5hex("abc"); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("md5hex 异常: %q", got)
	}
}

// TestNewManagerDefaultsLogger 负责TestNewManagerDefaultsLogger相关处理。
func TestNewManagerDefaultsLogger(t *testing.T) {
	// m 保存m，供当前处理流程使用
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

// TestManagerSessionLifecycle 负责TestManager会话Lifecycle相关处理。
func TestManagerSessionLifecycle(t *testing.T) {
	// m 保存m，供当前处理流程使用
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
	// got 保存got，供当前处理流程使用
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
