package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// fakeQRLoginService 保存fakeQR登录Service，供当前处理流程使用
type fakeQRLoginService struct {
	status           map[string]any
	generateErr      error
	generateDeadline time.Time
	completeCookies  string
	completeUNB      string
	completeErr      error
}

// GenerateQRCode 负责GenerateQRCode相关处理。
func (f *fakeQRLoginService) GenerateQRCode(ctx context.Context) (string, string, error) {
	f.generateDeadline, _ = ctx.Deadline()
	if f.generateErr != nil {
		return "", "", f.generateErr
	}
	return "qr-session", "data:image/png;base64,abc", nil
}

// GetSessionStatus 读取会话状态。
func (f *fakeQRLoginService) GetSessionStatus(sessionID string) map[string]any {
	if sessionID == "no-such-session" {
		return map[string]any{"status": "not_found"}
	}
	// out 保存out，供当前处理流程使用
	out := make(map[string]any, len(f.status)+1)
	// k、v 表示当前遍历过程中的k、v
	for k, v := range f.status {
		out[k] = v
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := out["session_id"]; !ok {
		out["session_id"] = sessionID
	}
	return out
}

// CompleteVerification 负责CompleteVerification相关处理。
func (f *fakeQRLoginService) CompleteVerification(context.Context, string) (string, string, error) {
	if f.completeErr != nil {
		return "", "", f.completeErr
	}
	return f.completeCookies, f.completeUNB, nil
}

// TestGenerateQRLogin 生成扫码登录二维码。
func TestGenerateQRLogin(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// qr 保存qr，供当前处理流程使用
	qr := &fakeQRLoginService{}
	srv.QRLogin = qr
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/qr-login/generate", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true || res["session_id"] == nil || res["session_id"] == "" {
		t.Fatalf("生成二维码响应异常: %+v", res)
	}
	// remaining 保存remaining，供当前处理流程使用
	remaining := time.Until(qr.generateDeadline)
	if remaining < qrLoginGenerateTimeout-time.Second || remaining > qrLoginGenerateTimeout {
		t.Fatalf("二维码生成超时窗口=%v want≈%v", remaining, qrLoginGenerateTimeout)
	}
}

// TestQRLoginSessionCannotBeReadByAnotherUser 负责TestQR登录会话CannotBeReadByAnother用户相关处理。
func TestQRLoginSessionCannotBeReadByAnotherUser(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	if // ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(context.Background(), "member", "member@example.com", "memberpw"); err != nil || !ok {
		t.Fatalf("create member: ok=%v err=%v", ok, err)
	}
	srv.QRLogin = &fakeQRLoginService{status: map[string]any{"status": "waiting"}}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// adminCookie 保存admin登录凭证，供当前处理流程使用
	adminCookie := loginHelper(t, h)
	// memberCookie 保存member登录凭证，供当前处理流程使用
	memberCookie := loginAsHelper(t, h, "member", "memberpw")

	// generateReq 保存generateReq，供当前处理流程使用
	generateReq := httptest.NewRequest(http.MethodPost, "/qr-login/generate", nil)
	generateReq.AddCookie(adminCookie)
	// generateRec 保存generateRec，供当前处理流程使用
	generateRec := httptest.NewRecorder()
	h.ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusOK {
		t.Fatalf("generate status=%d body=%s", generateRec.Code, generateRec.Body.String())
	}

	// memberReq 保存memberReq，供当前处理流程使用
	memberReq := httptest.NewRequest(http.MethodGet, "/qr-login/check/qr-session", nil)
	memberReq.AddCookie(memberCookie)
	// memberRec 保存memberRec，供当前处理流程使用
	memberRec := httptest.NewRecorder()
	h.ServeHTTP(memberRec, memberReq)
	if memberRec.Code != http.StatusNotFound {
		t.Fatalf("cross-user QR session read status=%d body=%s", memberRec.Code, memberRec.Body.String())
	}

	// adminReq 保存adminReq，供当前处理流程使用
	adminReq := httptest.NewRequest(http.MethodGet, "/qr-login/check/qr-session", nil)
	adminReq.AddCookie(adminCookie)
	// adminRec 保存adminRec，供当前处理流程使用
	adminRec := httptest.NewRecorder()
	h.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("owner QR session read status=%d body=%s", adminRec.Code, adminRec.Body.String())
	}
}

// TestQRLoginStatusNeverExposesCookies 负责TestQR登录状态NeverExposesCookies相关处理。
func TestQRLoginStatusNeverExposesCookies(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	srv.QRLogin = &fakeQRLoginService{status: map[string]any{
		"status": "success", "cookies": "unb=acc1; secret=value", "unb": "acc1",
		"cookie_snapshot": []cookierefresh.BrowserCookie{{Name: "secret", Value: "value", Domain: ".goofish.com", Path: "/"}},
	}}
	ownQRSession(t, srv, store, "redacted")
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// path 表示当前遍历过程中的路径
	for _, path := range []string{"/qr-login/check/redacted", "/qr-login/status/redacted"} {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		// res 保存响应，供当前处理流程使用
		var res map[string]any
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if // exists 保存exists，供当前处理流程使用
		_, exists := res["cookies"]; exists {
			t.Fatalf("%s must redact cookies: %+v", path, res)
		}
		if // exists 保存exists，供当前处理流程使用
		_, exists := res["cookie_snapshot"]; exists {
			t.Fatalf("%s must redact cookie snapshot: %+v", path, res)
		}
	}
}

// TestQRLoginSessionExpiresWithoutAnotherGenerateRequest 负责TestQR登录会话ExpiresWithoutAnotherGenerate请求相关处理。
func TestQRLoginSessionExpiresWithoutAnotherGenerateRequest(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{status: map[string]any{"status": "waiting"}}
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	srv.qrOwners["expired-session"] = qrLoginOwner{UserID: admin.ID, CreatedAt: time.Now().UTC().Add(-31 * time.Minute)}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/qr-login/check/expired-session", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expired QR status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCheckQRLoginStatusEmptySession 缺 session_id 400。
func TestCheckQRLoginStatusEmptySession(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// chi 路由 /qr-login/check/{session_id}；空 session 走不到 handler（404）。
	// 用一个不存在的 session 验证不 panic。
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/qr-login/check/no-such-session", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCompleteQRVerificationBadSession 不存在的 session 应返回失败响应（不 panic）。
func TestCompleteQRVerificationBadSession(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{completeErr: errors.New("会话不存在")}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/qr-login/complete-verification/no-such-session", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ownQRSession 负责ownQR会话相关处理。
func ownQRSession(t *testing.T, srv *Server, store *db.Store, sessionID string) {
	t.Helper()
	// admin、err 保存admin、err，供当前处理流程使用
	admin, err := store.Users.GetByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetByUsername admin: %v", err)
	}
	srv.qrMu.Lock()
	srv.qrOwners[sessionID] = qrLoginOwner{UserID: admin.ID, CreatedAt: time.Now().UTC()}
	srv.qrMu.Unlock()
}

// TestQRLoginStatusPersistsSuccessIdempotently 负责TestQR登录状态PersistsSuccessIdempotently相关处理。
func TestQRLoginStatusPersistsSuccessIdempotently(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	srv.QRLogin = &fakeQRLoginService{status: map[string]any{
		"status":  "success",
		"cookies": "unb=qr-new; _m_h5_tk=qr-token;",
		"unb":     "qr-new",
		"cookie_snapshot": []cookierefresh.BrowserCookie{
			{Name: "unb", Value: "qr-new", Domain: ".goofish.com", Path: "/", Secure: true},
			{Name: "_m_h5_tk", Value: "qr-token", Domain: ".goofish.com", Path: "/", Secure: true, HTTPOnly: true},
		},
	}}
	if // snapshot、ok 保存snapshot、ok，供当前处理流程使用
	snapshot, ok := qrCookieSnapshot(srv.QRLogin.GetSessionStatus("s1")); !ok || len(snapshot) != 2 {
		t.Fatalf("测试扫码 Cookie 快照异常: ok=%v snapshot=%+v", ok, snapshot)
	}
	ownQRSession(t, srv, store, "s1")
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	for // i 保存i，供当前处理流程使用
	i := 0; i < 2; i++ {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodGet, "/qr-login/status/s1", nil)
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		// res 保存响应，供当前处理流程使用
		var res map[string]any
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if res["success"] != true || res["account_id"] != "qr-new" {
			t.Fatalf("扫码状态保存响应异常: %+v", res)
		}
	}

	// d、err 保存d、err，供当前处理流程使用
	d, err := store.Cookies.GetDetails(context.Background(), "qr-new")
	if err != nil {
		t.Fatalf("GetDetails qr-new: %v", err)
	}
	if d.LoginMethod != "qr_scan" || d.LastLoginAt == 0 {
		t.Fatalf("扫码登录应标记登录审计字段: %+v", d)
	}
	if // snapshot、ok 保存snapshot、ok，供当前处理流程使用
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(d.MetadataJSON); !ok || len(snapshot) != 2 {
		t.Fatalf("纯 Go 扫码完整 Cookie Jar 未持久化: ok=%v snapshot=%+v metadata=%s", ok, snapshot, d.MetadataJSON)
	}
	// logs、err 保存logs、err，供当前处理流程使用
	logs, err := store.LoginLogs.ListByCookie(context.Background(), "qr-new", 10)
	if err != nil || len(logs) != 1 || logs[0].Status != "success" || logs[0].Method != "qr_scan" {
		t.Fatalf("重复轮询不应重复记录登录日志: logs=%#v err=%v", logs, err)
	}
}

// TestCompleteQRVerificationPersistsAndReenablesAccount 负责TestCompleteQRVerificationPersistsAndReenables账号相关处理。
func TestCompleteQRVerificationPersistsAndReenablesAccount(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	srv.Manager = nil
	srv.QRLogin = &fakeQRLoginService{
		completeCookies: "unb=acc1; _m_h5_tk=qr-fresh;",
		completeUNB:     "acc1",
	}
	ownQRSession(t, srv, store, "s1")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.SetStatusWithReason(ctx, "acc1", false, "token 失效"); err != nil {
		t.Fatalf("SetStatusWithReason: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Save(ctx, "acc1", "did", "token", 9999999999); err != nil {
		t.Fatalf("Save token: %v", err)
	}
	seedStaleCookieSnapshot(t, store, "acc1")
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/qr-login/complete-verification/s1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res["success"] != true || res["account_id"] != "acc1" {
		t.Fatalf("完成验证响应异常: %+v", res)
	}
	if // exists 保存exists，供当前处理流程使用
	_, exists := res["cookies"]; exists {
		t.Fatalf("完成验证响应不得暴露 cookies: %+v", res)
	}
	if !store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("扫码验证成功后应重新启用账号")
	}
	requireCookieSnapshotCleared(t, store, "acc1")
	if // tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(ctx, "acc1"); err != nil || tk.AccessToken != "" || tk.DeviceID != "did" {
		t.Fatalf("扫码验证成功后应清 token 并保留 device ID: tk=%+v err=%v", tk, err)
	}
}

// TestCompleteQRVerificationRejectsDifferentTarget 负责TestCompleteQRVerificationRejectsDifferentTarget相关处理。
func TestCompleteQRVerificationRejectsDifferentTarget(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	srv.QRLogin = &fakeQRLoginService{
		completeCookies: "unb=scanned-other; _m_h5_tk=qr-fresh;",
		completeUNB:     "scanned-other",
	}
	ownQRSession(t, srv, store, "s-mismatch")
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// request 保存请求，供当前处理流程使用
	request := func() map[string]any {
		// body 保存请求体，供当前处理流程使用
		body := `{"target_account_id":"acc1"}`
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPost, "/qr-login/complete-verification/s-mismatch", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		// result 保存结果，供当前处理流程使用
		var result map[string]any
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	// result 保存结果，供当前处理流程使用
	result := request()
	if result["code"] != "qr_account_mismatch" || result["details"].(map[string]any)["scanned_account_id"] != "scanned-other" {
		t.Fatalf("response=%+v", result)
	}
	// original 保存original，供当前处理流程使用
	original, _ := store.Cookies.GetValue(context.Background(), "acc1")
	if strings.Contains(original, "qr-fresh") {
		t.Fatal("mismatched account must never overwrite the target account")
	}
}

// TestKeywordsListAndDelete 关键字列表与删除（覆盖 listKeywords / listKeywordsWithItemID / listKeywordsWithType）。
func TestKeywordsListAndDelete(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 添加（普通 + 带 item_id）。
	post := func(body string) {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPost, "/keywords/acc1", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("add keyword status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	post(`{"keyword":"在吗","reply":"在的"}`)
	post(`{"keyword":"价格","reply":"50元","item_id":"item1"}`)

	// path 表示当前遍历过程中的路径
	for _, path := range []string{"/keywords/acc1", "/keywords-with-item-id/acc1", "/keywords-with-type/acc1"} {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("GET %s status=%d", path, rec.Code)
		}
		// arr 保存arr，供当前处理流程使用
		var arr []map[string]any
		json.Unmarshal(rec.Body.Bytes(), &arr)
		if len(arr) != 2 {
			t.Fatalf("%s 应2条，got %d", path, len(arr))
		}
	}
}

// TestAddKeywordMissingKeyword 缺 keyword 400。
func TestAddKeywordMissingKeyword(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/keywords/acc1", strings.NewReader(`{"reply":"x"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 keyword 应 400，got %d", rec.Code)
	}
}

// TestAddKeywordBadJSON 非法 JSON 400。
func TestAddKeywordBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/keywords/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestReplaceKeywordsWithItemID 负责TestReplaceKeywordsWith商品ID相关处理。
func TestReplaceKeywordsWithItemID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// postBatch 保存post批次，供当前处理流程使用
	postBatch := func(body string) []map[string]any {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("replace status=%d body=%s", rec.Code, rec.Body.String())
		}

		// listReq 保存listReq，供当前处理流程使用
		listReq := httptest.NewRequest(http.MethodGet, "/keywords-with-item-id/acc1", nil)
		listReq.AddCookie(cookie)
		// listRec 保存listRec，供当前处理流程使用
		listRec := httptest.NewRecorder()
		h.ServeHTTP(listRec, listReq)
		if listRec.Code != http.StatusOK {
			t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
		}
		// rows 保存rows，供当前处理流程使用
		var rows []map[string]any
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal(listRec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}

	// rows 保存rows，供当前处理流程使用
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

// TestReplaceKeywordsValidatesReplyTypeContent 负责TestReplaceKeywordsValidates回复类型内容相关处理。
func TestReplaceKeywordsValidatesReplyTypeContent(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// body 表示当前遍历过程中的请求体
	for _, body := range []string{
		`{"keywords":[{"keyword":"文字","type":"text","reply":""}]}`,
		`{"keywords":[{"keyword":"图片","type":"image","image_url":""}]}`,
		`{"keywords":[{"keyword":"未知","type":"api","reply":"x"}]}`,
	} {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d body=%s", body, rec.Code, rec.Body.String())
		}
	}

	// valid 保存有效，供当前处理流程使用
	valid := `{"keywords":[{"keyword":"图片","type":"image","reply":"stale","image_url":"https://example.com/a.png"}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(valid))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid image status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDeleteKeywordNotFound 不存在关键字 404。
func TestDeleteKeywordNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/keywords/acc1/999", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在关键字应 404，got %d", rec.Code)
	}
}

// TestItemRepliesCRUD 指定商品回复增删查。
func TestItemRepliesCRUD(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','it-reply','商品R')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 设。
	body := `{"reply_content":"这是专属回复"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/item-reply/acc1/it-reply", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 查。
	req2 := httptest.NewRequest(http.MethodGet, "/item-reply/acc1/it-reply", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("get status=%d", rec2.Code)
	}
	// got 保存got，供当前处理流程使用
	var got map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &got)
	if got["reply_content"] != "这是专属回复" {
		t.Fatalf("回复内容异常: %+v", got)
	}

	// 列表。
	req3 := httptest.NewRequest(http.MethodGet, "/itemReplays", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("list status=%d", rec3.Code)
	}

	// 删除。
	req4 := httptest.NewRequest(http.MethodDelete, "/item-reply/acc1/it-reply", nil)
	req4.AddCookie(cookie)
	// rec4 保存rec4，供当前处理流程使用
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Fatalf("delete status=%d", rec4.Code)
	}
}

// TestGetItemReplyNotFound 不存在回复返回空。
func TestGetItemReplyNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/item-reply/acc1/no-such", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	// got 保存got，供当前处理流程使用
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["reply_content"] != "" {
		t.Fatalf("不存在应返回空: %+v", got)
	}
}

// TestSetItemReplyBadJSON 非法 JSON 400。
func TestSetItemReplyBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/item-reply/acc1/it1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}
