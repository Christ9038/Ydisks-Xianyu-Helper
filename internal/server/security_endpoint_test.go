package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestProtectedRouteGroupsRequireAuthentication 封装TestProtectedRouteGroupsRequireAuthentication业务协调。
func TestProtectedRouteGroupsRequireAuthentication(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// routes 用于本次流程后续判断的routes
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/cookies"},
		{http.MethodGet, "/api/orders"},
		{http.MethodGet, "/analytics/orders"},
		{http.MethodGet, "/cards"},
		{http.MethodGet, "/items"},
		{http.MethodGet, "/keywords/acc1"},
		{http.MethodGet, "/default-replies/acc1"},
		{http.MethodGet, "/notification-channels"},
		{http.MethodGet, "/system-settings"},
		{http.MethodGet, "/ai-reply-settings"},
		{http.MethodGet, "/user-settings"},
		{http.MethodGet, "/admin/stats"},
	}
	// route 表示当前遍历过程中的route
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// req 用于本次流程后续判断的req
			req := httptest.NewRequest(route.method, route.path, nil)
			// rec 用于本次流程后续判断的rec
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestCookiePreferenceEndpoints 封装Test登录凭证PreferenceEndpoints业务协调。
func TestCookiePreferenceEndpoints(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// requests 用于本次流程后续判断的请求列表
	requests := []struct {
		path string
		body string
	}{
		{"/cookies/acc1/auto-confirm", `{"auto_confirm":true}`},
		{"/cookies/acc1/remark", `{"remark":"primary"}`},
		{"/cookies/acc1/pause-duration", `{"pause_duration":30}`},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range requests {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}

	// path 表示当前遍历过程中的路径
	for _, path := range []string{"/cookies/acc1/auto-confirm", "/cookies/acc1/pause-duration", "/cookie/acc1/details"} {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	// paused、pausedUntil、err 用于本次流程后续判断的paused、pausedUntil、err
	paused, pausedUntil, err := store.Cookies.IsPaused(context.Background(), "acc1")
	if err != nil || !paused || pausedUntil <= time.Now().UTC().Unix() {
		t.Fatalf("pause deadline not persisted: paused=%v until=%d err=%v", paused, pausedUntil, err)
	}
	// pauseReq 用于本次流程后续判断的pauseReq
	pauseReq := httptest.NewRequest(http.MethodGet, "/cookies/acc1/pause-duration", nil)
	pauseReq.AddCookie(cookie)
	// pauseRec 用于本次流程后续判断的pauseRec
	pauseRec := httptest.NewRecorder()
	h.ServeHTTP(pauseRec, pauseReq)
	// pauseResponse 用于本次流程后续判断的pause响应
	var pauseResponse map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(pauseRec.Body.Bytes(), &pauseResponse); err != nil || pauseResponse["paused"] != true {
		t.Fatalf("pause response=%+v err=%v", pauseResponse, err)
	}

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/pause-duration", strings.NewReader(`{"pause_duration":-1}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative pause status=%d body=%s", rec.Code, rec.Body.String())
	}
	// tooLongReq 用于本次流程后续判断的tooLongReq
	tooLongReq := httptest.NewRequest(http.MethodPut, "/cookies/acc1/pause-duration", strings.NewReader(`{"pause_duration":1441}`))
	tooLongReq.AddCookie(cookie)
	// tooLongRec 用于本次流程后续判断的tooLongRec
	tooLongRec := httptest.NewRecorder()
	h.ServeHTTP(tooLongRec, tooLongReq)
	if tooLongRec.Code != http.StatusBadRequest {
		t.Fatalf("too-long pause status=%d body=%s", tooLongRec.Code, tooLongRec.Body.String())
	}
}
