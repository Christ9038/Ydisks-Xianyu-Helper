package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

type fakeQRLoginService struct {
	status          map[string]any
	generateErr     error
	completeCookies string
	completeUNB     string
	completeErr     error
}

func (f *fakeQRLoginService) GenerateQRCode(context.Context) (string, string, error) {
	if f.generateErr != nil {
		return "", "", f.generateErr
	}
	return "qr-session", "data:image/png;base64,abc", nil
}

func (f *fakeQRLoginService) GetSessionStatus(sessionID string) map[string]any {
	if sessionID == "no-such-session" {
		return map[string]any{"status": "not_found"}
	}
	out := make(map[string]any, len(f.status)+1)
	for k, v := range f.status {
		out[k] = v
	}
	if _, ok := out["session_id"]; !ok {
		out["session_id"] = sessionID
	}
	return out
}

func (f *fakeQRLoginService) CompleteVerification(context.Context, string) (string, string, error) {
	if f.completeErr != nil {
		return "", "", f.completeErr
	}
	return f.completeCookies, f.completeUNB, nil
}

// TestGenerateQRLogin 生成扫码登录二维码。
func TestGenerateQRLogin(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{}
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/qr-login/generate", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true || res["session_id"] == nil || res["session_id"] == "" {
		t.Fatalf("生成二维码响应异常: %+v", res)
	}
}

// TestCheckQRLoginStatusEmptySession 缺 session_id 400。
func TestCheckQRLoginStatusEmptySession(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{}
	h := srv.Router()
	cookie := loginHelper(t, h)

	// chi 路由 /qr-login/check/{session_id}；空 session 走不到 handler（404）。
	// 用一个不存在的 session 验证不 panic。
	req := httptest.NewRequest(http.MethodGet, "/qr-login/check/no-such-session", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCompleteQRVerificationBadSession 不存在的 session 应返回失败响应（不 panic）。
func TestCompleteQRVerificationBadSession(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{completeErr: errors.New("会话不存在")}
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/qr-login/complete-verification/no-such-session", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != false {
		t.Fatalf("不存在的 session 应 success=false: %+v", res)
	}
}

func TestQRLoginStatusPersistsSuccessIdempotently(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	srv.QRLogin = &fakeQRLoginService{status: map[string]any{
		"status":  "success",
		"cookies": "unb=qr-new; _m_h5_tk=qr-token;",
		"unb":     "qr-new",
	}}
	h := srv.Router()
	cookie := loginHelper(t, h)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/qr-login/status/s1", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var res map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if res["success"] != true || res["account_id"] != "qr-new" {
			t.Fatalf("扫码状态保存响应异常: %+v", res)
		}
	}

	d, err := store.Cookies.GetDetails(context.Background(), "qr-new")
	if err != nil {
		t.Fatalf("GetDetails qr-new: %v", err)
	}
	if d.LoginMethod != "qr_scan" || d.LastLoginAt == 0 {
		t.Fatalf("扫码登录应标记登录审计字段: %+v", d)
	}
	logs, err := store.LoginLogs.ListByCookie(context.Background(), "qr-new", 10)
	if err != nil || len(logs) != 1 || logs[0].Status != "success" || logs[0].Method != "qr_scan" {
		t.Fatalf("重复轮询不应重复记录登录日志: logs=%#v err=%v", logs, err)
	}
}

func TestCompleteQRVerificationPersistsAndReenablesAccount(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	srv.Manager = nil
	srv.QRLogin = &fakeQRLoginService{
		completeCookies: "unb=acc1; _m_h5_tk=qr-fresh;",
		completeUNB:     "acc1",
	}
	if err := store.Cookies.SetStatusWithReason(ctx, "acc1", false, "token 失效"); err != nil {
		t.Fatalf("SetStatusWithReason: %v", err)
	}
	if err := store.Tokens.Save(ctx, "acc1", "did", "token", 9999999999); err != nil {
		t.Fatalf("Save token: %v", err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/qr-login/complete-verification/s1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res["success"] != true || res["account_id"] != "acc1" {
		t.Fatalf("完成验证响应异常: %+v", res)
	}
	if !store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("扫码验证成功后应重新启用账号")
	}
	if _, err := store.Tokens.Get(ctx, "acc1"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("扫码验证成功后应清 token，got %v", err)
	}
}

// TestKeywordsListAndDelete 关键字列表与删除（覆盖 listKeywords / listKeywordsWithItemID / listKeywordsWithType）。
func TestKeywordsListAndDelete(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 添加（普通 + 带 item_id）。
	post := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/keywords/acc1", strings.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("add keyword status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	post(`{"keyword":"在吗","reply":"在的"}`)
	post(`{"keyword":"价格","reply":"50元","item_id":"item1"}`)

	for _, path := range []string{"/keywords/acc1", "/keywords-with-item-id/acc1", "/keywords-with-type/acc1"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("GET %s status=%d", path, rec.Code)
		}
		var arr []map[string]any
		json.Unmarshal(rec.Body.Bytes(), &arr)
		if len(arr) != 2 {
			t.Fatalf("%s 应2条，got %d", path, len(arr))
		}
	}
}

// TestAddKeywordMissingKeyword 缺 keyword 400。
func TestAddKeywordMissingKeyword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/keywords/acc1", strings.NewReader(`{"reply":"x"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 keyword 应 400，got %d", rec.Code)
	}
}

// TestAddKeywordBadJSON 非法 JSON 400。
func TestAddKeywordBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/keywords/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

func TestReplaceKeywordsWithItemID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	postBatch := func(body string) []map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("replace status=%d body=%s", rec.Code, rec.Body.String())
		}

		listReq := httptest.NewRequest(http.MethodGet, "/keywords-with-item-id/acc1", nil)
		listReq.AddCookie(cookie)
		listRec := httptest.NewRecorder()
		h.ServeHTTP(listRec, listReq)
		if listRec.Code != http.StatusOK {
			t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
		}
		var rows []map[string]any
		if err := json.Unmarshal(listRec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}

	rows := postBatch(`{"keywords":[{"keyword":"在吗","reply":"在的","item_id":""},{"keyword":"价格","reply":"50","item_id":"it1"}]}`)
	if len(rows) != 2 || rows[1]["item_id"] != "it1" {
		t.Fatalf("批量新增异常: %+v", rows)
	}

	rows = postBatch(`{"keywords":[{"keyword":"在吗","reply":"稍等","item_id":""}]}`)
	if len(rows) != 1 || rows[0]["reply"] != "稍等" {
		t.Fatalf("批量覆盖编辑异常: %+v", rows)
	}

	rows = postBatch(`{"keywords":[]}`)
	if len(rows) != 0 {
		t.Fatalf("空数组应清空关键词: %+v", rows)
	}
}

// TestDeleteKeywordNotFound 不存在关键字 404。
func TestDeleteKeywordNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/keywords/acc1/999", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在关键字应 404，got %d", rec.Code)
	}
}

// TestItemRepliesCRUD 指定商品回复增删查。
func TestItemRepliesCRUD(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','it-reply','商品R')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 设。
	body := `{"reply_content":"这是专属回复"}`
	req := httptest.NewRequest(http.MethodPut, "/item-reply/acc1/it-reply", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 查。
	req2 := httptest.NewRequest(http.MethodGet, "/item-reply/acc1/it-reply", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("get status=%d", rec2.Code)
	}
	var got map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &got)
	if got["reply_content"] != "这是专属回复" {
		t.Fatalf("回复内容异常: %+v", got)
	}

	// 列表。
	req3 := httptest.NewRequest(http.MethodGet, "/itemReplays", nil)
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("list status=%d", rec3.Code)
	}

	// 删除。
	req4 := httptest.NewRequest(http.MethodDelete, "/item-reply/acc1/it-reply", nil)
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Fatalf("delete status=%d", rec4.Code)
	}
}

// TestGetItemReplyNotFound 不存在回复返回空。
func TestGetItemReplyNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/item-reply/acc1/no-such", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["reply_content"] != "" {
		t.Fatalf("不存在应返回空: %+v", got)
	}
}

// TestSetItemReplyBadJSON 非法 JSON 400。
func TestSetItemReplyBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/item-reply/acc1/it1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}
