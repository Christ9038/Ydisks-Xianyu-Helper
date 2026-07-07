package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListDefaultReplies 列表（数组形式）。
func TestListDefaultReplies(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 设置。
	body := `{"enabled":true,"reply_content":"你好","reply_once":false}`
	req := httptest.NewRequest(http.MethodPut, "/default-replies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d", rec.Code)
	}

	// 列表（数组）。
	req2 := httptest.NewRequest(http.MethodGet, "/default-replies", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["reply_content"] != "你好" {
		t.Fatalf("列表异常: %+v", arr)
	}

	// 列表（map 形式，兼容路径）。
	req3 := httptest.NewRequest(http.MethodGet, "/api/default-replies", nil)
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("map status=%d", rec3.Code)
	}
	var m map[string]map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &m)
	if m["acc1"]["reply_content"] != "你好" {
		t.Fatalf("map 列表异常: %+v", m)
	}
}

// TestSetDefaultReplyBadJSON 非法 JSON 400。
func TestSetDefaultReplyBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/default-replies/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestGetDefaultReplyNotFound 不存在返回默认。
func TestGetDefaultReplyNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/default-replies/no-such", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["enabled"] != false || got["reply_content"] != "" {
		t.Fatalf("默认值异常: %+v", got)
	}
}

// TestDeleteDefaultReply 删除。
func TestDeleteDefaultReply(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 设置。
	body := `{"enabled":true,"reply_content":"你好"}`
	req := httptest.NewRequest(http.MethodPut, "/default-replies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// 删除。
	req2 := httptest.NewRequest(http.MethodDelete, "/default-replies/acc1", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("delete status=%d", rec2.Code)
	}
}

// TestBtoi btoi 表驱动。
func TestBtoi(t *testing.T) {
	if btoi(true) != 1 {
		t.Fatal("btoi(true) should be 1")
	}
	if btoi(false) != 0 {
		t.Fatal("btoi(false) should be 0")
	}
}

// TestNullIfEmpty nullIfEmpty 表驱动。
func TestNullIfEmpty(t *testing.T) {
	if v := nullIfEmpty(""); v != nil {
		t.Fatalf("空串应为 nil，got %v", v)
	}
	if v := nullIfEmpty("x"); v != "x" {
		t.Fatalf("非空应原样返回，got %v", v)
	}
}
