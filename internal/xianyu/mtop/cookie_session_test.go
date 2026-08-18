package mtop

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

// cookieSessionRoundTripFunc 用于本次流程后续判断的登录凭证会话RoundTripFunc
type cookieSessionRoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 封装RoundTrip业务协调。
func (fn cookieSessionRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

// TestMTopRequestCookiesSeparatesDocumentAndRequestScopes 封装TestMTop请求CookiesSeparatesDocumentAnd请求Scopes业务协调。
func TestMTopRequestCookiesSeparatesDocumentAndRequestScopes(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "token_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "doc_only", Value: "doc", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "api_only", Value: "api", Domain: "h5api.m.goofish.com", Path: "/h5", Secure: true, HTTPOnly: true},
		{Name: "partitioned", Value: "part", Domain: ".goofish.com", Path: "/", Secure: true, PartitionKey: goofishTopSite},
		{Name: "foreign_partition", Value: "no", Domain: ".goofish.com", Path: "/", Secure: true, PartitionKey: "https://example.com"},
	}
	// ctx 用于本次流程后续判断的ctx
	ctx, _ := WithCookieSnapshot(context.Background(), snapshot)
	// signing、request 用于本次流程后续判断的signing、request
	signing, request := mtopRequestCookies(ctx, "fallback=1", mtopDocumentURL, TokenAPI)
	// want 表示当前遍历过程中的want
	for _, want := range []string{"_m_h5_tk=token_1", "doc_only=doc", "partitioned=part"} {
		if !strings.Contains(signing, want) {
			t.Fatalf("document cookies %q missing %q", signing, want)
		}
	}
	// unwanted 表示当前遍历过程中的unwanted
	for _, unwanted := range []string{"api_only=", "foreign_partition="} {
		if strings.Contains(signing, unwanted) {
			t.Fatalf("document cookies %q unexpectedly contain %q", signing, unwanted)
		}
	}
	// want 表示当前遍历过程中的want
	for _, want := range []string{"_m_h5_tk=token_1", "api_only=api", "partitioned=part"} {
		if !strings.Contains(request, want) {
			t.Fatalf("request cookies %q missing %q", request, want)
		}
	}
	// unwanted 表示当前遍历过程中的unwanted
	for _, unwanted := range []string{"doc_only=", "foreign_partition="} {
		if strings.Contains(request, unwanted) {
			t.Fatalf("request cookies %q unexpectedly contain %q", request, unwanted)
		}
	}
}

// TestCookieSessionAbsorbsScopedSetCookieAndDeletion 封装Test登录凭证会话AbsorbsScopedSet登录凭证AndDeletion业务协调。
func TestCookieSessionAbsorbsScopedSetCookieAndDeletion(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "same", Value: "root", Domain: ".goofish.com", Path: "/"},
		{Name: "same", Value: "api-old", Domain: "h5api.m.goofish.com", Path: "/h5"},
	}
	// session 用于本次流程后续判断的会话
	_, session := WithCookieSnapshot(context.Background(), snapshot)
	// requestURL、err 用于本次流程后续判断的请求URL、err
	requestURL, err := url.Parse(TokenAPI)
	if err != nil {
		t.Fatal(err)
	}
	// resp 用于本次流程后续判断的resp
	resp := &http.Response{
		Request: &http.Request{URL: requestURL},
		Header: http.Header{"Set-Cookie": {
			"same=api-new; Path=/h5; Secure",
			"partitioned=value; Path=/; Secure; SameSite=None; Partitioned",
		}},
	}
	session.absorb(requestURL.String(), resp.Header.Values("Set-Cookie"))
	// got、changed 用于本次流程后续判断的got、changed
	_, got, changed := session.State()
	if !changed {
		t.Fatal("Set-Cookie must mark session changed")
	}
	// values 用于本次流程后续判断的values
	values := make(map[string]string)
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range got {
		values[cookie.Name+"|"+cookie.Domain+"|"+cookie.Path+"|"+cookie.PartitionKey] = cookie.Value
	}
	if values["same|.goofish.com|/|"] != "root" {
		t.Fatalf("root same-name cookie changed: %+v", got)
	}
	if values["same|h5api.m.goofish.com|/h5|"] != "api-new" {
		t.Fatalf("host/path cookie not updated precisely: %+v", got)
	}
	if values["partitioned|h5api.m.goofish.com|/|"+goofishTopSite] != "value" {
		t.Fatalf("partitioned cookie missing top-level key: %+v", got)
	}

	session.absorb(requestURL.String(), []string{"same=; Max-Age=-1; Path=/h5"})
	_, got, _ = session.State()
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range got {
		if cookie.Name == "same" && cookie.Domain == "h5api.m.goofish.com" && cookie.Path == "/h5" {
			t.Fatalf("scoped deletion was not applied: %+v", got)
		}
	}
}

// TestCookieSessionAuthoritativeEmptyJar 封装Test登录凭证会话AuthoritativeEmptyJar业务协调。
func TestCookieSessionAuthoritativeEmptyJar(t *testing.T) {
	// ctx、session 用于本次流程后续判断的ctx、session
	ctx, session := WithCookieSnapshot(context.Background(), nil)
	// signing、request 用于本次流程后续判断的signing、request
	signing, request := mtopRequestCookies(ctx, "fallback=must-not-leak", mtopDocumentURL, TokenAPI)
	if signing != "" || request != "" {
		t.Fatalf("authoritative empty jar leaked fallback: signing=%q request=%q", signing, request)
	}
	// canonical、snapshot、changed 用于本次流程后续判断的canonical、snapshot、changed
	canonical, snapshot, changed := session.State()
	if canonical != "" || snapshot == nil || len(snapshot) != 0 || changed {
		t.Fatalf("empty state canonical=%q snapshot=%#v changed=%v", canonical, snapshot, changed)
	}
}

// TestFlatCookieSessionCapturesUpdatesWithoutInventingSnapshot 封装TestFlat登录凭证会话CapturesUpdatesWithoutInventingSnapshot业务协调。
func TestFlatCookieSessionCapturesUpdatesWithoutInventingSnapshot(t *testing.T) {
	// ctx、session 用于本次流程后续判断的ctx、session
	ctx, session := WithFlatCookieSession(context.Background(), "sid=old; keep=1")
	// signing、request 用于本次流程后续判断的signing、request
	signing, request := mtopRequestCookies(ctx, "fallback=must-not-leak", mtopDocumentURL, TokenAPI)
	if signing != "sid=old; keep=1" || request != signing {
		t.Fatalf("legacy flat session signing=%q request=%q", signing, request)
	}
	session.absorb(TokenAPI, []string{
		"sid=new; Domain=.goofish.com; Path=/; Secure",
		"keep=; Max-Age=0; Domain=.goofish.com; Path=/; Secure",
	})
	// value、snapshot、changed 用于本次流程后续判断的value、snapshot、changed
	value, snapshot, changed := session.State()
	if !changed || snapshot != nil || value != "sid=new" {
		t.Fatalf("flat update value=%q snapshot=%#v changed=%v", value, snapshot, changed)
	}
}

// TestScopedCookieHeaderForRequestIncludesMatchingPartitionAndUnpartitioned 封装TestScoped登录凭证HeaderFor请求IncludesMatchingPartitionAndUnpartitioned业务协调。
func TestScopedCookieHeaderForRequestIncludesMatchingPartitionAndUnpartitioned(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "plain", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "part", Value: "2", Domain: ".goofish.com", Path: "/", Secure: true, PartitionKey: goofishTopSite},
		{Name: "other", Value: "3", Domain: ".goofish.com", Path: "/", Secure: true, PartitionKey: "https://example.com"},
	}
	// header、ok 用于本次流程后续判断的header、ok
	header, ok := cookierefresh.ScopedCookieHeaderForRequest(snapshot, TokenAPI, goofishTopSite, time.Now())
	if !ok || !strings.Contains(header, "plain=1") || !strings.Contains(header, "part=2") || strings.Contains(header, "other=3") {
		t.Fatalf("partition-scoped header=%q ok=%v", header, ok)
	}
}

// TestLoginStatusUsesDocumentSigningAndURLScopedRequestCookies 封装Test登录状态UsesDocumentSigningAndURLScoped请求Cookies业务协调。
func TestLoginStatusUsesDocumentSigningAndURLScopedRequestCookies(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "im-token_1", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "_m_h5_tk", Value: "root-token_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "document_only", Value: "visible", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "api_http_only", Value: "secret", Domain: "h5api.m.goofish.com", Path: "/h5", Secure: true, HTTPOnly: true},
	}
	// ctx、session 用于本次流程后续判断的ctx、session
	ctx, session := WithCookieSnapshot(context.Background(), snapshot)
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		// requestCookies 用于本次流程后续判断的请求Cookies
		requestCookies := req.Header.Get("Cookie")
		// want 表示当前遍历过程中的want
		for _, want := range []string{"_m_h5_tk=root-token_1", "unb=123", "api_http_only=secret"} {
			if !strings.Contains(requestCookies, want) {
				t.Errorf("URL scoped Cookie %q missing %q", requestCookies, want)
			}
		}
		// unwanted 表示当前遍历过程中的unwanted
		for _, unwanted := range []string{"document_only=", "fallback_only="} {
			if strings.Contains(requestCookies, unwanted) {
				t.Errorf("URL scoped Cookie %q unexpectedly contains %q", requestCookies, unwanted)
			}
		}
		if // got 用于本次流程后续判断的got
		got := req.Header.Get("Referer"); got != mtopDocumentURL {
			t.Errorf("Referer=%q want %q", got, mtopDocumentURL)
		}
		// timestamp 用于本次流程后续判断的timestamp
		timestamp := req.URL.Query().Get("t")
		// wantSign 用于本次流程后续判断的wantSign
		wantSign := protocol.GenerateSign(timestamp, "im-token", `{}`)
		if // got 用于本次流程后续判断的got
		got := req.URL.Query().Get("sign"); got != wantSign {
			t.Errorf("sign=%q want %q", got, wantSign)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": {
				"_m_h5_tk=rotated-token_9; Domain=.goofish.com; Path=/; Secure",
			}},
			Body:    io.NopCloser(strings.NewReader("not-json")),
			Request: req,
		}, nil
	})}}

	// err 用于本次流程后续判断的err
	_, err := client.CheckLoginStatusContext(ctx, "_m_h5_tk=fallback_1; fallback_only=leak")
	if err == nil || !strings.Contains(err.Error(), "解析 loginuser.get 响应失败") {
		t.Fatalf("err=%v", err)
	}
	// canonical、gotSnapshot、changed 用于本次流程后续判断的canonical、gotSnapshot、changed
	canonical, gotSnapshot, changed := session.State()
	if !changed || !strings.Contains(canonical, "_m_h5_tk=rotated-token_9") {
		t.Fatalf("response Cookie was not absorbed before body parse: canonical=%q changed=%v snapshot=%+v", canonical, changed, gotSnapshot)
	}
}

// TestOrderDetailUsesOrderDocumentForSigningAndReferer 封装Test订单DetailUses订单DocumentForSigningAndReferer业务协调。
func TestOrderDetailUsesOrderDocumentForSigningAndReferer(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "order-token_1", Domain: "www.goofish.com", Path: "/order-detail", Secure: true},
		{Name: "_m_h5_tk", Value: "root-token_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
	}
	// ctx 用于本次流程后续判断的ctx
	ctx, _ := WithCookieSnapshot(context.Background(), snapshot)
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if // got 用于本次流程后续判断的got
		got := req.Header.Get("Referer"); got != "https://www.goofish.com/order-detail?orderId=order-1&role=seller" {
			t.Errorf("Referer=%q", got)
		}
		if // got 用于本次流程后续判断的got
		got := req.Header.Get("Cookie"); !strings.Contains(got, "_m_h5_tk=root-token_1") || strings.Contains(got, "order-token_1") {
			t.Errorf("request Cookie=%q", got)
		}
		// timestamp 用于本次流程后续判断的timestamp
		timestamp := req.URL.Query().Get("t")
		// wantSign 用于本次流程后续判断的wantSign
		wantSign := protocol.GenerateSign(timestamp, "order-token", `{"tid":"order-1"}`)
		if // got 用于本次流程后续判断的got
		got := req.URL.Query().Get("sign"); got != wantSign {
			t.Errorf("sign=%q want %q", got, wantSign)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{"components":[]}}`)),
			Request:    req,
		}, nil
	})}}
	if // err 用于本次流程后续判断的err
	_, err := client.FetchOrderDetail(ctx, "fallback=must-not-leak", "order-1"); err != nil {
		t.Fatal(err)
	}
}

// TestPublishWorkflowUsesCookieSessionForEveryRequest 封装Test发布WorkflowUses登录凭证会话ForEvery请求业务协调。
func TestPublishWorkflowUsesCookieSessionForEveryRequest(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "token0_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "document_only", Value: "visible", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "api_http_only", Value: "api", Domain: "h5api.m.goofish.com", Path: "/h5", Secure: true, HTTPOnly: true},
		{Name: "upload_http_only", Value: "upload", Domain: "stream-upload.goofish.com", Path: "/api", Secure: true, HTTPOnly: true},
	}
	// ctx、session 用于本次流程后续判断的ctx、session
	ctx, session := WithCookieSnapshot(context.Background(), snapshot)
	// requestCount 用于本次流程后续判断的请求数量
	requestCount := 0
	// assertCookies 用于本次流程后续判断的assertCookies
	assertCookies := func(req *http.Request, token, scoped string) {
		t.Helper()
		requestCount++
		// cookieHeader 用于本次流程后续判断的登录凭证Header
		cookieHeader := req.Header.Get("Cookie")
		// want 表示当前遍历过程中的want
		for _, want := range []string{"unb=123", "_m_h5_tk=" + token + "_1", scoped} {
			if !strings.Contains(cookieHeader, want) {
				t.Errorf("%s Cookie %q missing %q", req.URL.Host, cookieHeader, want)
			}
		}
		// unwanted 表示当前遍历过程中的unwanted
		for _, unwanted := range []string{"document_only=", "fallback_only="} {
			if strings.Contains(cookieHeader, unwanted) {
				t.Errorf("%s Cookie %q unexpectedly contains %q", req.URL.Host, cookieHeader, unwanted)
			}
		}
	}
	// assertSign 用于本次流程后续判断的assertSign
	assertSign := func(req *http.Request, token string) {
		t.Helper()
		// body、err 用于本次流程后续判断的body、err
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		// form、err 用于本次流程后续判断的form、err
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("parse request body: %v", err)
			return
		}
		// timestamp 用于本次流程后续判断的timestamp
		timestamp := req.URL.Query().Get("t")
		// want 用于本次流程后续判断的want
		want := protocol.GenerateSign(timestamp, token, form.Get("data"))
		if // got 用于本次流程后续判断的got
		got := req.URL.Query().Get("sign"); got != want {
			t.Errorf("%s sign=%q want %q", req.URL.Query().Get("api"), got, want)
		}
	}
	// rotate 用于本次流程后续判断的rotate
	rotate := func(w http.ResponseWriter, value string) {
		w.Header().Add("Set-Cookie", "_m_h5_tk="+value+"_1; Domain=.goofish.com; Path=/; Secure")
	}

	// dt 用于本次流程后续判断的dt
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, req *http.Request) {
			assertCookies(req, "token0", "upload_http_only=upload")
			if strings.Contains(req.Header.Get("Cookie"), "api_http_only=") {
				t.Errorf("upload request leaked h5api cookie: %q", req.Header.Get("Cookie"))
			}
			rotate(w, "token1")
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"1x1"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, req *http.Request) {
			assertCookies(req, "token1", "api_http_only=api")
			assertSign(req, "token1")
			rotate(w, "token2")
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"c1","catName":"类目"}}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, req *http.Request) {
			assertCookies(req, "token2", "api_http_only=api")
			assertSign(req, "token2")
			rotate(w, "token4")
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"new-item"}}`)
		},
	}}
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	// result、err 用于本次流程后续判断的result、err
	result, err := client.PublishItem(ctx, "_m_h5_tk=fallback_1; fallback_only=leak", PublishItemRequest{
		Title:      "T",
		PriceCents: 100,
		Quantity:   1,
		Location:   &PublishLocation{Area: "X", City: "Y", DivisionID: "1", Longitude: 118.7, Latitude: 31.9, POIID: "p1", POIName: "P", Province: "Z"},
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: tinyPNG(t)}},
	})
	if err != nil {
		t.Fatalf("PublishItem: %v", err)
	}
	if requestCount != 3 || result.ItemID != "new-item" || !strings.Contains(result.UpdatedCookies, "_m_h5_tk=token4_1") {
		t.Fatalf("requests=%d result=%+v", requestCount, result)
	}
	// canonical、changed 用于本次流程后续判断的canonical、changed
	canonical, _, changed := session.State()
	if !changed || !strings.Contains(canonical, "_m_h5_tk=token4_1") {
		t.Fatalf("final session canonical=%q changed=%v", canonical, changed)
	}
}
