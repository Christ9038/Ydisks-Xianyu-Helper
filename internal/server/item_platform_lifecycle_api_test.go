package server

import (
	"context"
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
}
