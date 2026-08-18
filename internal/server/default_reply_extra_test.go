package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListDefaultReplies 列表（数组形式）。
func TestListDefaultReplies(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 设置。
	body := `{"enabled":true,"reply_content":"你好","reply_once":false}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/default-replies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d", rec.Code)
	}

	// 列表（数组）。
	req2 := httptest.NewRequest(http.MethodGet, "/default-replies", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	// arr 用于本次流程后续判断的arr
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["reply_content"] != "你好" {
		t.Fatalf("列表异常: %+v", arr)
	}

	// 列表（map 形式，兼容路径）。
	req3 := httptest.NewRequest(http.MethodGet, "/api/default-replies", nil)
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("map status=%d", rec3.Code)
	}
	// m 用于本次流程后续判断的m
	var m map[string]map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &m)
	if m["acc1"]["reply_content"] != "你好" {
		t.Fatalf("map 列表异常: %+v", m)
	}
}

// TestSetDefaultReplyBadJSON 非法 JSON 400。
func TestSetDefaultReplyBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/default-replies/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestGetDefaultReplyMissingAccountIsNotFound 封装TestGetDefault回复Missing账号IsNotFound业务协调。
func TestGetDefaultReplyMissingAccountIsNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/default-replies/no-such", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在账号应 404，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetDefaultReplyExistingAccountWithoutConfigReturnsDefault 封装TestGetDefault回复Existing账号Without配置ReturnsDefault业务协调。
func TestGetDefaultReplyExistingAccountWithoutConfigReturnsDefault(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/default-replies/acc1", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// got 用于本次流程后续判断的got
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["enabled"] != false || got["reply_content"] != "" {
		t.Fatalf("默认值异常: %+v", got)
	}
}

// TestGetDefaultReplyRejectsCrossUserAccount 封装TestGetDefault回复RejectsCross用户账号业务协调。
func TestGetDefaultReplyRejectsCrossUserAccount(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "default-other", "default-other@example.com", "pw"); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	// other、err 用于本次流程后续判断的other、err
	other, err := store.Users.GetByUsername(ctx, "default-other")
	if err != nil {
		t.Fatalf("get other user: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(ctx, "other-acc", "unb=456; _m_h5_tk=tk2_1;", other.ID); err != nil {
		t.Fatalf("save other cookie: %v", err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/default-replies/other-acc", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("跨用户账号应 403，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDeleteDefaultReply 删除。
func TestDeleteDefaultReply(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 设置。
	body := `{"enabled":true,"reply_content":"你好"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/default-replies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// 删除。
	req2 := httptest.NewRequest(http.MethodDelete, "/default-replies/acc1", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
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
	if // v 用于本次流程后续判断的v
	v := nullIfEmpty(""); v != nil {
		t.Fatalf("空串应为 nil，got %v", v)
	}
	if // v 用于本次流程后续判断的v
	v := nullIfEmpty("x"); v != "x" {
		t.Fatalf("非空应原样返回，got %v", v)
	}
}
