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

func TestProtectedRouteGroupsRequireAuthentication(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
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
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCookiePreferenceEndpoints(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	requests := []struct {
		path string
		body string
	}{
		{"/cookies/acc1/auto-confirm", `{"auto_confirm":true}`},
		{"/cookies/acc1/remark", `{"remark":"primary"}`},
		{"/cookies/acc1/pause-duration", `{"pause_duration":30}`},
	}
	for _, tc := range requests {
		req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}

	for _, path := range []string{"/cookies/acc1/auto-confirm", "/cookies/acc1/pause-duration", "/cookie/acc1/details"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	paused, pausedUntil, err := store.Cookies.IsPaused(context.Background(), "acc1")
	if err != nil || !paused || pausedUntil <= time.Now().UTC().Unix() {
		t.Fatalf("pause deadline not persisted: paused=%v until=%d err=%v", paused, pausedUntil, err)
	}
	pauseReq := httptest.NewRequest(http.MethodGet, "/cookies/acc1/pause-duration", nil)
	pauseReq.AddCookie(cookie)
	pauseRec := httptest.NewRecorder()
	h.ServeHTTP(pauseRec, pauseReq)
	var pauseResponse map[string]any
	if err := json.Unmarshal(pauseRec.Body.Bytes(), &pauseResponse); err != nil || pauseResponse["paused"] != true {
		t.Fatalf("pause response=%+v err=%v", pauseResponse, err)
	}

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/pause-duration", strings.NewReader(`{"pause_duration":-1}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative pause status=%d body=%s", rec.Code, rec.Body.String())
	}
	tooLongReq := httptest.NewRequest(http.MethodPut, "/cookies/acc1/pause-duration", strings.NewReader(`{"pause_duration":1441}`))
	tooLongReq.AddCookie(cookie)
	tooLongRec := httptest.NewRecorder()
	h.ServeHTTP(tooLongRec, tooLongReq)
	if tooLongRec.Code != http.StatusBadRequest {
		t.Fatalf("too-long pause status=%d body=%s", tooLongRec.Code, tooLongRec.Body.String())
	}
}
