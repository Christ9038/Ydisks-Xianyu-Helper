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

// TestProtectedRouteGroupsRequireAuthentication 负责TestProtectedRouteGroupsRequireAuthentication相关处理。
func TestProtectedRouteGroupsRequireAuthentication(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// routes 保存routes，供当前处理流程使用
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
			// req 保存req，供当前处理流程使用
			req := httptest.NewRequest(route.method, route.path, nil)
			// rec 保存rec，供当前处理流程使用
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestCookiePreferenceEndpoints 负责Test登录凭证PreferenceEndpoints相关处理。
func TestCookiePreferenceEndpoints(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// requests 保存请求列表，供当前处理流程使用
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
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}

	// path 表示当前遍历过程中的路径
	for _, path := range []string{"/cookies/acc1/auto-confirm", "/cookies/acc1/pause-duration", "/cookie/acc1/details"} {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	// paused、pausedUntil、err 保存paused、pausedUntil、err，供当前处理流程使用
	paused, pausedUntil, err := store.Cookies.IsPaused(context.Background(), "acc1")
	if err != nil || !paused || pausedUntil <= time.Now().UTC().Unix() {
		t.Fatalf("pause deadline not persisted: paused=%v until=%d err=%v", paused, pausedUntil, err)
	}
	// pauseReq 保存pauseReq，供当前处理流程使用
	pauseReq := httptest.NewRequest(http.MethodGet, "/cookies/acc1/pause-duration", nil)
	pauseReq.AddCookie(cookie)
	// pauseRec 保存pauseRec，供当前处理流程使用
	pauseRec := httptest.NewRecorder()
	h.ServeHTTP(pauseRec, pauseReq)
	// pauseResponse 保存pause响应，供当前处理流程使用
	var pauseResponse map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(pauseRec.Body.Bytes(), &pauseResponse); err != nil || pauseResponse["paused"] != true {
		t.Fatalf("pause response=%+v err=%v", pauseResponse, err)
	}

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/pause-duration", strings.NewReader(`{"pause_duration":-1}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative pause status=%d body=%s", rec.Code, rec.Body.String())
	}
	// tooLongReq 保存tooLongReq，供当前处理流程使用
	tooLongReq := httptest.NewRequest(http.MethodPut, "/cookies/acc1/pause-duration", strings.NewReader(`{"pause_duration":1441}`))
	tooLongReq.AddCookie(cookie)
	// tooLongRec 保存tooLongRec，供当前处理流程使用
	tooLongRec := httptest.NewRecorder()
	h.ServeHTTP(tooLongRec, tooLongReq)
	if tooLongRec.Code != http.StatusBadRequest {
		t.Fatalf("too-long pause status=%d body=%s", tooLongRec.Code, tooLongRec.Body.String())
	}
}
