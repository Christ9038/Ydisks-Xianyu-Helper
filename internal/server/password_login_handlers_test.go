package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordLoginAPIsArePermanentlyDisabled(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	authCookie := loginHelper(t, h)

	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/password-login", strings.NewReader(`{"account_id":"acc1","account":"u","password":"p"}`)),
		httptest.NewRequest(http.MethodGet, "/password-login/check/legacy", nil),
		httptest.NewRequest(http.MethodDelete, "/password-login/cancel/legacy", nil),
	}
	for _, req := range requests {
		req.AddCookie(authCookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", req.Method, req.URL.Path, rec.Code, rec.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["success"] != false || result["status"] != "disabled" {
			t.Fatalf("%s %s 应永久禁用: %+v", req.Method, req.URL.Path, result)
		}
	}
}
