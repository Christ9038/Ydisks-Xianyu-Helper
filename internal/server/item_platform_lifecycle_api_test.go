package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/application/apikey"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// itemPlatformLifecyclePortStub 是商品平台下架 HTTP 契约测试使用的窄应用端口替身。
type itemPlatformLifecyclePortStub struct {
	// outcome 是测试预设的商品平台下架结果。
	outcome itemapp.DownShelfOutcome
	// err 是测试预设的应用层错误。
	err error
	// userID、cookieID、itemID 记录 HTTP Handler 实际传入的归属和商品标识。
	userID   int64
	cookieID string
	itemID   string
}

// DownShelf 记录归属和商品标识，并返回预设的平台生命周期结果。
func (port *itemPlatformLifecyclePortStub) DownShelf(_ context.Context, userID int64, cookieID, itemID string) (itemapp.DownShelfOutcome, error) {
	port.userID, port.cookieID, port.itemID = userID, cookieID, itemID
	return port.outcome, port.err
}

// TestItemPlatformUnpublishHandlerContract 验证真实路由调用平台下架用例，返回明确 off_sale 结果且不走本地 DELETE。
func TestItemPlatformUnpublishHandlerContract(t *testing.T) {
	// srv、store、cleanup 是使用隔离 SQLite 数据库构造的完整 HTTP Server、数据库和清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// owner 是本次外部令牌的归属用户。
	owner, err := store.Users.GetAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// token 是仅授予商品平台下架权限的测试 Bearer 凭证。
	token := apikey.TokenPrefix + "item-unpublish-api-test-token"
	// now 是本次测试令牌的签发时间。
	now := time.Now().Unix()
	// record 是只授予 items:unpublish 的令牌记录。
	record := db.APIAccessToken{ID: "item-unpublish-api-key", UserID: owner.ID, Name: "item unpublish API test", TokenHash: apikey.HashToken(token), Prefix: token[:len(apikey.TokenPrefix)+8], Scopes: []string{apikey.ScopeItemsUnpublish}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是将下架专用令牌摘要写入隔离数据库时的错误。
	if err := store.APIAccessTokens.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	// port 是返回平台已下架且 Cookie 会话已写回的窄应用端口替身。
	port := &itemPlatformLifecyclePortStub{outcome: itemapp.DownShelfOutcome{ItemID: "item-1", Status: "off_sale", Message: "SUCCESS::调用成功", PlatformSuccess: true, CookiePersisted: true}}
	srv.applications.itemPlatformLifecycle = port
	// handler 是包含真实认证和版本化商品路由的 Router。
	handler := srv.Router()
	// request 是仅携带 items:unpublish 权限的外部 Bearer 请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/acc1/item-1/unpublish", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	// recorder 捕获下架响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPISuccessResponse(t, request, recorder)
	if !strings.Contains(recorder.Body.String(), `"status":"off_sale"`) || !strings.Contains(recorder.Body.String(), `"cookie_persisted":true`) {
		t.Fatalf("platform unpublish response missing explicit completion state: %s", recorder.Body.String())
	}
	if port.userID <= 0 || port.cookieID != "acc1" || port.itemID != "item-1" {
		t.Fatalf("unpublish handler did not preserve current-user account ownership: %+v", port)
	}
	// 部分失败时平台已下架，统一错误 envelope 必须保留恢复信息供调用方避免重复请求。
	port.outcome = itemapp.DownShelfOutcome{ItemID: "item-1", Status: "off_sale", PlatformSuccess: true, CookiePersisted: false}
	port.err = itemapp.ErrDownShelfSessionPersistence
	// partialRequest 是再次通过版本化路由触发的平台已下架、本地写回失败场景。
	partialRequest := httptest.NewRequest(http.MethodPost, "/api/v1/items/acc1/item-1/unpublish", nil)
	partialRequest.Header.Set("Authorization", "Bearer "+token)
	// partialRecorder 保存带恢复信息的统一错误响应。
	partialRecorder := httptest.NewRecorder()
	handler.ServeHTTP(partialRecorder, partialRequest)
	if partialRecorder.Code != http.StatusBadGateway {
		t.Fatalf("部分失败状态码错误: status=%d body=%s", partialRecorder.Code, partialRecorder.Body.String())
	}
	assertOpenAPIResponse(t, partialRequest, partialRecorder)
	// envelope 按统一错误结构解码，并验证 API 客户端可读取的稳定 code 与 details。
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			ItemID          string `json:"item_id"`
			Status          string `json:"status"`
			PlatformSuccess bool   `json:"platform_success"`
			CookiePersisted bool   `json:"cookie_persisted"`
		} `json:"details"`
	}
	// err 是标准错误响应的 JSON 解码结果，失败时禁止继续依赖恢复字段。
	if err := json.Unmarshal(partialRecorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("无法解码部分失败响应: %v", err)
	}
	if envelope.Code != "platform_unpublished_cookie_persist_failed" || envelope.Message == "" ||
		envelope.Details.ItemID != "item-1" || envelope.Details.Status != "off_sale" ||
		!envelope.Details.PlatformSuccess || envelope.Details.CookiePersisted {
		t.Fatalf("部分失败没有保留可恢复状态: %+v", envelope)
	}

	// legacyRequest 证明新增的平台下架写接口没有暴露到历史 /items 路由。
	legacyRequest := httptest.NewRequest(http.MethodPost, "/items/acc1/item-1/unpublish", nil)
	legacyRequest.Header.Set("Authorization", "Bearer "+token)
	// legacyRecorder 保存旧路径拒绝结果，不允许旧路由触达应用端口。
	legacyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyRecorder, legacyRequest)
	if legacyRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("新增下架操作不应注册旧路径: status=%d", legacyRecorder.Code)
	}
}
