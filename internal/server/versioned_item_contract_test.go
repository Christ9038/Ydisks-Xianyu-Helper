package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVersionedItemRoutesPreserveLegacyContracts 验证商品列表、详情、发布、更新和删除入口复用旧 handler。
func TestVersionedItemRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证版本化商品路由的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是写入商品夹具时使用的独立请求上下文。
	ctx := context.Background()
	// insertErr 是商品夹具写入失败的原因。
	_, insertErr := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail, is_multi_spec, multi_quantity_delivery) VALUES ('acc1','item-v1','版本化商品','商品描述','资料','12.00','{}',1,1), ('acc1','item-legacy','旧路径商品','','','10.00','{}',0,0)`)
	if insertErr != nil {
		t.Fatalf("insert item fixtures: %v", insertErr)
	}
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// listReq 是读取版本化商品列表的请求。
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/items?cookie_id=acc1", nil)
	listReq.AddCookie(sessionCookie)
	// listRecorder 是捕获版本化商品列表响应的记录器。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	assertOpenAPISuccessResponse(t, listReq, listRecorder)
	// listValue 是版本化商品列表响应 DTO 集合。
	var listValue []itemListResponse
	// listDecodeErr 是商品列表响应反序列化失败的原因。
	if listDecodeErr := json.Unmarshal(listRecorder.Body.Bytes(), &listValue); listDecodeErr != nil {
		t.Fatalf("decode versioned item list: %v", listDecodeErr)
	}
	if len(listValue) != 2 || listValue[0].CookieID != "acc1" {
		t.Fatalf("versioned item list=%+v", listValue)
	}
	if listValue[0].AIOverride != "inherit" || listValue[1].AIOverride != "inherit" {
		t.Fatalf("商品列表默认 AI 状态异常: %+v", listValue)
	}

	// aiGetReq 是读取未配置商品 AI 设置的请求。
	aiGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-v1/ai-settings", nil)
	aiGetReq.AddCookie(sessionCookie)
	// aiGetRecorder 是捕获默认商品 AI 设置的记录器。
	aiGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(aiGetRecorder, aiGetReq)
	assertOpenAPISuccessResponse(t, aiGetReq, aiGetRecorder)
	// aiDefault 是未配置商品返回的默认继承设置。
	var aiDefault itemAISettingsResponse
	if // decodeErr 是默认商品 AI 配置响应解码错误。
	decodeErr := json.Unmarshal(aiGetRecorder.Body.Bytes(), &aiDefault); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if aiDefault.AIOverride != "inherit" || aiDefault.ItemContext != "" {
		t.Fatalf("默认商品 AI 配置异常: %+v", aiDefault)
	}

	// aiPutReq 是保存商品 AI 配置的请求。
	aiPutReq := httptest.NewRequest(http.MethodPut, "/api/v1/items/acc1/item-v1/ai-settings", strings.NewReader(`{"ai_override":"enabled","item_context":"24 小时内发货"}`))
	aiPutReq.AddCookie(sessionCookie)
	// aiPutRecorder 是捕获商品 AI 保存响应的记录器。
	aiPutRecorder := httptest.NewRecorder()
	handler.ServeHTTP(aiPutRecorder, aiPutReq)
	assertOpenAPISuccessResponse(t, aiPutReq, aiPutRecorder)
	// aiSaved 是服务端返回的规范化商品 AI 配置。
	var aiSaved itemAISettingsResponse
	if // decodeErr 是已保存商品 AI 配置响应解码错误。
	decodeErr := json.Unmarshal(aiPutRecorder.Body.Bytes(), &aiSaved); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if aiSaved.AIOverride != "enabled" || aiSaved.ItemContext != "24 小时内发货" {
		t.Fatalf("保存商品 AI 配置异常: %+v", aiSaved)
	}

	// aiGetSavedReq 是再次读取已保存商品 AI 配置的请求。
	aiGetSavedReq := httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-v1/ai-settings", nil)
	aiGetSavedReq.AddCookie(sessionCookie)
	// aiGetSavedRecorder 是捕获已保存商品 AI 配置的记录器。
	aiGetSavedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(aiGetSavedRecorder, aiGetSavedReq)
	assertOpenAPISuccessResponse(t, aiGetSavedReq, aiGetSavedRecorder)

	// detailReq 是读取版本化商品详情的请求。
	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-v1", nil)
	detailReq.AddCookie(sessionCookie)
	// detailRecorder 是捕获版本化商品详情响应的记录器。
	detailRecorder := httptest.NewRecorder()
	handler.ServeHTTP(detailRecorder, detailReq)
	assertOpenAPISuccessResponse(t, detailReq, detailRecorder)
	// detailValue 是版本化商品详情响应 DTO。
	var detailValue itemDetailResponse
	// detailDecodeErr 是商品详情响应反序列化失败的原因。
	if detailDecodeErr := json.Unmarshal(detailRecorder.Body.Bytes(), &detailValue); detailDecodeErr != nil {
		t.Fatalf("decode versioned item detail: %v", detailDecodeErr)
	}
	if detailValue.CookieID != "acc1" || detailValue.ItemID != "item-v1" || detailValue.ItemTitle != "版本化商品" {
		t.Fatalf("versioned item detail=%+v", detailValue)
	}

	// updateReq 是通过版本化入口更新商品的请求。
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/items/acc1/item-v1", strings.NewReader(`{"item_title":"版本化新商品名"}`))
	updateReq.AddCookie(sessionCookie)
	// updateRecorder 是捕获版本化商品更新响应的记录器。
	updateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateRecorder, updateReq)
	assertOpenAPISuccessResponse(t, updateReq, updateRecorder)
	// updatedItem 是数据库中用于确认版本化更新结果的商品记录。
	updatedItem, updatedErr := store.Items.Get(ctx, "acc1", "item-v1")
	if updatedErr != nil || updatedItem == nil || updatedItem.ItemTitle != "版本化新商品名" {
		t.Fatalf("versioned item update not persisted: item=%+v err=%v", updatedItem, updatedErr)
	}

	// legacyUpdateReq 是验证旧商品更新入口仍可用的请求。
	legacyUpdateReq := httptest.NewRequest(http.MethodPut, "/items/acc1/item-legacy", strings.NewReader(`{"item_title":"旧路径新商品名"}`))
	legacyUpdateReq.AddCookie(sessionCookie)
	// legacyUpdateRecorder 是捕获旧商品更新响应的记录器。
	legacyUpdateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyUpdateRecorder, legacyUpdateReq)
	if legacyUpdateRecorder.Code != http.StatusOK {
		t.Fatalf("legacy item update status=%d body=%s", legacyUpdateRecorder.Code, legacyUpdateRecorder.Body.String())
	}

	// deleteReq 是通过版本化入口删除商品的请求。
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/items/acc1/item-v1", nil)
	deleteReq.AddCookie(sessionCookie)
	// deleteRecorder 是捕获版本化商品删除响应的记录器。
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	assertOpenAPISuccessResponse(t, deleteReq, deleteRecorder)

	// legacyDeleteReq 是验证旧商品删除入口仍可用的请求。
	legacyDeleteReq := httptest.NewRequest(http.MethodDelete, "/items/acc1/item-legacy", nil)
	legacyDeleteReq.AddCookie(sessionCookie)
	// legacyDeleteRecorder 是捕获旧商品删除响应的记录器。
	legacyDeleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyDeleteRecorder, legacyDeleteReq)
	if legacyDeleteRecorder.Code != http.StatusOK {
		t.Fatalf("legacy item delete status=%d body=%s", legacyDeleteRecorder.Code, legacyDeleteRecorder.Body.String())
	}

	// publishReq 是验证版本化商品发布入口已注册的无效请求。
	publishReq := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish", nil)
	publishReq.AddCookie(sessionCookie)
	// publishRecorder 是捕获版本化商品发布响应的记录器。
	publishRecorder := httptest.NewRecorder()
	handler.ServeHTTP(publishRecorder, publishReq)
	if publishRecorder.Code != http.StatusBadRequest {
		t.Fatalf("versioned item publish status=%d body=%s", publishRecorder.Code, publishRecorder.Body.String())
	}

	// legacyPublishReq 是验证旧商品发布入口仍可用的无效请求。
	legacyPublishReq := httptest.NewRequest(http.MethodPost, "/items/publish", nil)
	legacyPublishReq.AddCookie(sessionCookie)
	// legacyPublishRecorder 是捕获旧商品发布响应的记录器。
	legacyPublishRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyPublishRecorder, legacyPublishReq)
	if legacyPublishRecorder.Code != publishRecorder.Code {
		t.Fatalf("legacy item publish status=%d versioned=%d", legacyPublishRecorder.Code, publishRecorder.Code)
	}
}
