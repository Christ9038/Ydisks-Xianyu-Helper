package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestNamedSuccessResponseContracts 验证认证、订单和聊天主链路使用具名成功响应 DTO。
func TestNamedSuccessResponseContracts(t *testing.T) {
	// srv 是用于验证成功响应 DTO 的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := contractRecordingHandler(t, srv.Router())
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// verifyReq 是读取当前会话状态的请求。
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(sessionCookie)
	// verifyRecorder 是捕获会话状态响应的测试记录器。
	verifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(verifyRecorder, verifyReq)
	if verifyRecorder.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
	// verifyResponse 是会话校验具名响应 DTO。
	var verifyResponse sessionVerificationResponse
	// verifyDecodeErr 表示会话响应 JSON 反序列化失败的原因。
	if verifyDecodeErr := json.Unmarshal(verifyRecorder.Body.Bytes(), &verifyResponse); verifyDecodeErr != nil {
		t.Fatalf("decode verify response: %v", verifyDecodeErr)
	}
	if !verifyResponse.Authenticated || !verifyResponse.Initialized || verifyResponse.Username != "admin" {
		t.Fatalf("verify response=%+v", verifyResponse)
	}

	// orderReq 是读取当前用户订单列表的请求。
	orderReq := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	orderReq.AddCookie(sessionCookie)
	// orderRecorder 是捕获订单列表响应的测试记录器。
	orderRecorder := httptest.NewRecorder()
	handler.ServeHTTP(orderRecorder, orderReq)
	if orderRecorder.Code != http.StatusOK {
		t.Fatalf("order status=%d body=%s", orderRecorder.Code, orderRecorder.Body.String())
	}
	// orderResponse 是订单列表具名响应 DTO。
	var orderResponse orderListResponse
	// orderDecodeErr 表示订单列表响应 JSON 反序列化失败的原因。
	if orderDecodeErr := json.Unmarshal(orderRecorder.Body.Bytes(), &orderResponse); orderDecodeErr != nil {
		t.Fatalf("decode order response: %v", orderDecodeErr)
	}
	if !orderResponse.Success || orderResponse.Page != 1 || orderResponse.PageSize != 20 {
		t.Fatalf("order response=%+v", orderResponse)
	}

	// chatReq 是读取账号聊天会话列表的请求。
	chatReq := httptest.NewRequest(http.MethodGet, "/api/chat/sessions?account_id=acc1", nil)
	chatReq.AddCookie(sessionCookie)
	// chatRecorder 是捕获聊天会话响应的测试记录器。
	chatRecorder := httptest.NewRecorder()
	handler.ServeHTTP(chatRecorder, chatReq)
	if chatRecorder.Code != http.StatusOK {
		t.Fatalf("chat status=%d body=%s", chatRecorder.Code, chatRecorder.Body.String())
	}
	// chatResponse 是聊天会话分页具名响应 DTO。
	var chatResponse chatSessionPageResponse
	// chatDecodeErr 表示聊天会话响应 JSON 反序列化失败的原因。
	if chatDecodeErr := json.Unmarshal(chatRecorder.Body.Bytes(), &chatResponse); chatDecodeErr != nil {
		t.Fatalf("decode chat response: %v", chatDecodeErr)
	}
	if chatResponse.Sessions == nil || chatResponse.HasMore {
		t.Fatalf("chat response=%+v", chatResponse)
	}
}

// TestRemainingSuccessResponseContracts 验证账号、商品、自动化和订单详情响应使用具名 DTO。
func TestRemainingSuccessResponseContracts(t *testing.T) {
	// srv 是用于验证剩余成功响应 DTO 的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := contractRecordingHandler(t, srv.Router())
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// seedErr 是测试商品写入模板数据库失败的原因。
	if _, seedErr := store.DB.ExecContext(context.Background(), `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','contract-item','契约商品')`); seedErr != nil {
		t.Fatalf("seed item: %v", seedErr)
	}

	// accountReq 是读取单个账号非敏感详情的请求。
	accountReq := httptest.NewRequest(http.MethodGet, "/cookie/acc1/details", nil)
	accountReq.AddCookie(sessionCookie)
	// accountRecorder 是捕获账号详情响应的记录器。
	accountRecorder := httptest.NewRecorder()
	handler.ServeHTTP(accountRecorder, accountReq)
	if accountRecorder.Code != http.StatusOK {
		t.Fatalf("account status=%d body=%s", accountRecorder.Code, accountRecorder.Body.String())
	}
	// accountResponse 是账号详情具名响应 DTO。
	var accountResponse cookieDetailResponse
	// accountDecodeErr 是账号详情响应 JSON 反序列化失败的原因。
	if accountDecodeErr := json.Unmarshal(accountRecorder.Body.Bytes(), &accountResponse); accountDecodeErr != nil {
		t.Fatalf("decode account response: %v", accountDecodeErr)
	}
	if accountResponse.ID != "acc1" || !accountResponse.HasCookie {
		t.Fatalf("account response=%+v", accountResponse)
	}

	// itemReq 是读取本地商品列表的请求。
	itemReq := httptest.NewRequest(http.MethodGet, "/items", nil)
	itemReq.AddCookie(sessionCookie)
	// itemRecorder 是捕获商品列表响应的记录器。
	itemRecorder := httptest.NewRecorder()
	handler.ServeHTTP(itemRecorder, itemReq)
	if itemRecorder.Code != http.StatusOK {
		t.Fatalf("item status=%d body=%s", itemRecorder.Code, itemRecorder.Body.String())
	}
	// itemResponse 是本地商品列表具名响应 DTO 列表。
	var itemResponse []itemListResponse
	// itemDecodeErr 是商品列表响应 JSON 反序列化失败的原因。
	if itemDecodeErr := json.Unmarshal(itemRecorder.Body.Bytes(), &itemResponse); itemDecodeErr != nil {
		t.Fatalf("decode item response: %v", itemDecodeErr)
	}
	if len(itemResponse) != 1 || itemResponse[0].ItemID != "contract-item" {
		t.Fatalf("item response=%+v", itemResponse)
	}

	// ruleReq 是读取自动化规则分页响应的请求。
	ruleReq := httptest.NewRequest(http.MethodGet, "/automation-rules?page=1", nil)
	ruleReq.AddCookie(sessionCookie)
	// ruleRecorder 是捕获自动化规则响应的记录器。
	ruleRecorder := httptest.NewRecorder()
	handler.ServeHTTP(ruleRecorder, ruleReq)
	if ruleRecorder.Code != http.StatusOK {
		t.Fatalf("rule status=%d body=%s", ruleRecorder.Code, ruleRecorder.Body.String())
	}
	// ruleResponse 是自动化规则分页具名响应 DTO。
	var ruleResponse automationRulePageResponse
	// ruleDecodeErr 是自动化规则响应 JSON 反序列化失败的原因。
	if ruleDecodeErr := json.Unmarshal(ruleRecorder.Body.Bytes(), &ruleResponse); ruleDecodeErr != nil {
		t.Fatalf("decode rule response: %v", ruleDecodeErr)
	}
	if !ruleResponse.Success || ruleResponse.Page != 1 || ruleResponse.PageSize != 10 {
		t.Fatalf("rule response=%+v", ruleResponse)
	}

	// importReq 是创建一条测试订单的请求。
	importReq := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(`[{"order_id":"contract-order","item_id":"contract-item","status":"pending_ship","quantity":1,"amount":"1.00"}]`))
	importReq.Header.Set("Content-Type", "application/json")
	importReq.AddCookie(sessionCookie)
	// importRecorder 是捕获订单导入响应的记录器。
	importRecorder := httptest.NewRecorder()
	handler.ServeHTTP(importRecorder, importReq)
	if importRecorder.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", importRecorder.Code, importRecorder.Body.String())
	}
	// importResponse 是订单导入具名响应 DTO。
	var importResponse importOrdersResponse
	// importDecodeErr 是订单导入响应 JSON 反序列化失败的原因。
	if importDecodeErr := json.Unmarshal(importRecorder.Body.Bytes(), &importResponse); importDecodeErr != nil {
		t.Fatalf("decode import response: %v", importDecodeErr)
	}
	if importResponse.SuccessCount != 1 || importResponse.Total != 1 {
		t.Fatalf("import response=%+v", importResponse)
	}

	// orderReq 是读取刚导入订单详情的请求。
	orderReq := httptest.NewRequest(http.MethodGet, "/api/orders/contract-order", nil)
	orderReq.AddCookie(sessionCookie)
	// orderRecorder 是捕获订单详情响应的记录器。
	orderRecorder := httptest.NewRecorder()
	handler.ServeHTTP(orderRecorder, orderReq)
	if orderRecorder.Code != http.StatusOK {
		t.Fatalf("order status=%d body=%s", orderRecorder.Code, orderRecorder.Body.String())
	}
	// orderResponse 是订单详情具名响应 DTO。
	var orderResponse orderDetailResponse
	// orderDecodeErr 是订单详情响应 JSON 反序列化失败的原因。
	if orderDecodeErr := json.Unmarshal(orderRecorder.Body.Bytes(), &orderResponse); orderDecodeErr != nil {
		t.Fatalf("decode order response: %v", orderDecodeErr)
	}
	if !orderResponse.Success || orderResponse.Data.OrderID != "contract-order" || orderResponse.OrderID != "contract-order" {
		t.Fatalf("order response=%+v", orderResponse)
	}
}

// TestSettingsCardsNotificationsBatchContracts 验证设置、卡券、通知和商品批量接口的具名成功响应。
func TestSettingsCardsNotificationsBatchContracts(t *testing.T) {
	// srv 是用于验证设置、卡券、通知和商品批量响应的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := contractRecordingHandler(t, srv.Router())
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// settingReq 是更新单个系统设置的请求。
	settingReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings/system/theme_color", strings.NewReader(`{"value":"contract-blue"}`))
	settingReq.AddCookie(sessionCookie)
	// settingRecorder 是捕获系统设置响应的记录器。
	settingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingRecorder, settingReq)
	if settingRecorder.Code != http.StatusOK {
		t.Fatalf("setting status=%d body=%s", settingRecorder.Code, settingRecorder.Body.String())
	}
	// settingResponse 是系统设置变更具名响应 DTO。
	var settingResponse operationResponse
	// settingDecodeErr 是系统设置响应 JSON 反序列化失败的原因。
	if settingDecodeErr := json.Unmarshal(settingRecorder.Body.Bytes(), &settingResponse); settingDecodeErr != nil {
		t.Fatalf("decode setting response: %v", settingDecodeErr)
	}
	if !settingResponse.Success {
		t.Fatalf("setting response=%+v", settingResponse)
	}

	// aiReq 是更新账号 AI 回复设置的请求。
	aiReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings/ai-reply/acc1", strings.NewReader(`{"ai_enabled":true,"auto_adjust_price_enabled":true,"max_discount_percent":12,"max_discount_amount":88,"max_bargain_rounds":4,"custom_prompts":"契约测试"}`))
	aiReq.AddCookie(sessionCookie)
	// aiRecorder 是捕获账号 AI 设置响应的记录器。
	aiRecorder := httptest.NewRecorder()
	handler.ServeHTTP(aiRecorder, aiReq)
	if aiRecorder.Code != http.StatusOK {
		t.Fatalf("ai update status=%d body=%s", aiRecorder.Code, aiRecorder.Body.String())
	}
	// aiResponse 是账号 AI 设置变更具名响应 DTO。
	var aiResponse operationResponse
	// aiDecodeErr 是账号 AI 设置响应 JSON 反序列化失败的原因。
	if aiDecodeErr := json.Unmarshal(aiRecorder.Body.Bytes(), &aiResponse); aiDecodeErr != nil {
		t.Fatalf("decode ai update response: %v", aiDecodeErr)
	}
	if !aiResponse.Success {
		t.Fatalf("ai update response=%+v", aiResponse)
	}

	// aiGetReq 是读取账号 AI 设置的请求。
	aiGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/ai-reply/acc1", nil)
	aiGetReq.AddCookie(sessionCookie)
	// aiGetRecorder 是捕获账号 AI 设置查询响应的记录器。
	aiGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(aiGetRecorder, aiGetReq)
	if aiGetRecorder.Code != http.StatusOK {
		t.Fatalf("ai get status=%d body=%s", aiGetRecorder.Code, aiGetRecorder.Body.String())
	}
	// aiGetResponse 是账号 AI 设置查询具名响应 DTO。
	var aiGetResponse aiReplySettingsResponse
	// aiGetDecodeErr 是账号 AI 设置查询响应 JSON 反序列化失败的原因。
	if aiGetDecodeErr := json.Unmarshal(aiGetRecorder.Body.Bytes(), &aiGetResponse); aiGetDecodeErr != nil {
		t.Fatalf("decode ai get response: %v", aiGetDecodeErr)
	}
	if !aiGetResponse.AIEnabled || !aiGetResponse.AutoAdjustPriceEnabled || aiGetResponse.MaxDiscountPercent != 12 {
		t.Fatalf("ai get response=%+v", aiGetResponse)
	}

	// cardReq 是创建文本卡券组的请求。
	cardReq := httptest.NewRequest(http.MethodPost, "/api/v1/cards", strings.NewReader(`{"name":"契约卡","type":"text","text_content":"CARD","enabled":true}`))
	cardReq.AddCookie(sessionCookie)
	// cardRecorder 是捕获卡券创建响应的记录器。
	cardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(cardRecorder, cardReq)
	if cardRecorder.Code != http.StatusOK {
		t.Fatalf("card status=%d body=%s", cardRecorder.Code, cardRecorder.Body.String())
	}
	// cardResponse 是卡券创建具名响应 DTO。
	var cardCreateResponse mutationIDResponse
	// cardDecodeErr 是卡券创建响应 JSON 反序列化失败的原因。
	if cardDecodeErr := json.Unmarshal(cardRecorder.Body.Bytes(), &cardCreateResponse); cardDecodeErr != nil {
		t.Fatalf("decode card response: %v", cardDecodeErr)
	}
	if !cardCreateResponse.Success || cardCreateResponse.ID == 0 {
		t.Fatalf("card response=%+v", cardCreateResponse)
	}

	// cardGetReq 是读取卡券详情的请求。
	cardGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/cards/"+strconv.FormatInt(cardCreateResponse.ID, 10), nil)
	cardGetReq.AddCookie(sessionCookie)
	// cardGetRecorder 是捕获卡券详情响应的记录器。
	cardGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(cardGetRecorder, cardGetReq)
	if cardGetRecorder.Code != http.StatusOK {
		t.Fatalf("card get status=%d body=%s", cardGetRecorder.Code, cardGetRecorder.Body.String())
	}
	// cardGetResponse 是卡券详情具名响应 DTO。
	var cardGetResponse cardResponse
	// cardGetDecodeErr 是卡券详情响应 JSON 反序列化失败的原因。
	if cardGetDecodeErr := json.Unmarshal(cardGetRecorder.Body.Bytes(), &cardGetResponse); cardGetDecodeErr != nil {
		t.Fatalf("decode card get response: %v", cardGetDecodeErr)
	}
	if cardGetResponse.ID != cardCreateResponse.ID || cardGetResponse.Name != "契约卡" {
		t.Fatalf("card get response=%+v", cardGetResponse)
	}

	// channelReq 是创建通知渠道的请求。
	channelReq := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/channels", strings.NewReader(`{"name":"契约通知","type":"bark","config":"{}","enabled":true}`))
	channelReq.AddCookie(sessionCookie)
	// channelRecorder 是捕获通知渠道创建响应的记录器。
	channelRecorder := httptest.NewRecorder()
	handler.ServeHTTP(channelRecorder, channelReq)
	if channelRecorder.Code != http.StatusOK {
		t.Fatalf("channel status=%d body=%s", channelRecorder.Code, channelRecorder.Body.String())
	}
	// channelResponse 是通知渠道创建具名响应 DTO。
	var channelResponse mutationIDResponse
	// channelDecodeErr 是通知渠道创建响应 JSON 反序列化失败的原因。
	if channelDecodeErr := json.Unmarshal(channelRecorder.Body.Bytes(), &channelResponse); channelDecodeErr != nil {
		t.Fatalf("decode channel response: %v", channelDecodeErr)
	}
	if !channelResponse.Success || channelResponse.ID == 0 {
		t.Fatalf("channel response=%+v", channelResponse)
	}

	// bindingReq 是将通知渠道绑定到测试账号的请求。
	bindingReq := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/accounts/acc1/bindings", strings.NewReader(`{"channel_ids":[`+strconv.FormatInt(channelResponse.ID, 10)+`]}`))
	bindingReq.AddCookie(sessionCookie)
	// bindingRecorder 是捕获通知绑定响应的记录器。
	bindingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bindingRecorder, bindingReq)
	if bindingRecorder.Code != http.StatusOK {
		t.Fatalf("binding status=%d body=%s", bindingRecorder.Code, bindingRecorder.Body.String())
	}
	// bindingResponse 是通知绑定变更具名响应 DTO。
	var bindingResponse operationResponse
	// bindingDecodeErr 是通知绑定响应 JSON 反序列化失败的原因。
	if bindingDecodeErr := json.Unmarshal(bindingRecorder.Body.Bytes(), &bindingResponse); bindingDecodeErr != nil {
		t.Fatalf("decode binding response: %v", bindingDecodeErr)
	}
	if !bindingResponse.Success {
		t.Fatalf("binding response=%+v", bindingResponse)
	}

	// bindingGetReq 是读取账号通知绑定的请求。
	bindingGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/accounts/acc1/bindings", nil)
	bindingGetReq.AddCookie(sessionCookie)
	// bindingGetRecorder 是捕获账号通知绑定响应的记录器。
	bindingGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bindingGetRecorder, bindingGetReq)
	if bindingGetRecorder.Code != http.StatusOK {
		t.Fatalf("binding get status=%d body=%s", bindingGetRecorder.Code, bindingGetRecorder.Body.String())
	}
	// bindingGetResponse 是账号通知绑定具名响应 DTO。
	var bindingGetResponse accountBindingsResponse
	// bindingGetDecodeErr 是账号通知绑定响应 JSON 反序列化失败的原因。
	if bindingGetDecodeErr := json.Unmarshal(bindingGetRecorder.Body.Bytes(), &bindingGetResponse); bindingGetDecodeErr != nil {
		t.Fatalf("decode binding get response: %v", bindingGetDecodeErr)
	}
	if bindingGetResponse.CookieID != "acc1" || len(bindingGetResponse.ChannelIDs) != 1 {
		t.Fatalf("binding get response=%+v", bindingGetResponse)
	}

	// batchReq 是读取商品批量任务列表的请求。
	batchReq := httptest.NewRequest(http.MethodGet, "/api/v1/items/publish-batches?limit=10", nil)
	batchReq.AddCookie(sessionCookie)
	// batchRecorder 是捕获商品批量任务列表响应的记录器。
	batchRecorder := httptest.NewRecorder()
	handler.ServeHTTP(batchRecorder, batchReq)
	if batchRecorder.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%s", batchRecorder.Code, batchRecorder.Body.String())
	}
	// batchResponse 是商品批量任务列表具名响应 DTO。
	var batchResponse itemPublishBatchListResponse
	// batchDecodeErr 是商品批量任务列表响应 JSON 反序列化失败的原因。
	if batchDecodeErr := json.Unmarshal(batchRecorder.Body.Bytes(), &batchResponse); batchDecodeErr != nil {
		t.Fatalf("decode batch response: %v", batchDecodeErr)
	}
	if batchResponse.Batches == nil {
		t.Fatalf("batch response=%+v", batchResponse)
	}
}

// TestDynamicCompatibilitySuccessContracts 验证动态设置与二维码兼容响应保留明确边界。
func TestDynamicCompatibilitySuccessContracts(t *testing.T) {
	// srv 是用于验证动态成功响应的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := contractRecordingHandler(t, srv.Router())
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// publicSettingsReq 是读取公开系统设置的请求。
	publicSettingsReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/system/public", nil)
	// publicSettingsRecorder 是捕获公开设置响应的记录器。
	publicSettingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(publicSettingsRecorder, publicSettingsReq)
	if publicSettingsRecorder.Code != http.StatusOK {
		t.Fatalf("public settings status=%d body=%s", publicSettingsRecorder.Code, publicSettingsRecorder.Body.String())
	}
	// publicSettingsValue 是公开设置动态键值响应。
	var publicSettingsValue settingsResponse
	// publicSettingsDecodeErr 是公开设置响应反序列化失败的原因。
	if publicSettingsDecodeErr := json.Unmarshal(publicSettingsRecorder.Body.Bytes(), &publicSettingsValue); publicSettingsDecodeErr != nil {
		t.Fatalf("decode public settings response: %v", publicSettingsDecodeErr)
	}
	if publicSettingsValue.Entries == nil {
		t.Fatal("public settings response must be an object")
	}

	// allSettingsReq 是读取管理员系统设置的请求。
	allSettingsReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/system", nil)
	allSettingsReq.AddCookie(sessionCookie)
	// allSettingsRecorder 是捕获管理员设置响应的记录器。
	allSettingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(allSettingsRecorder, allSettingsReq)
	if allSettingsRecorder.Code != http.StatusOK {
		t.Fatalf("all settings status=%d body=%s", allSettingsRecorder.Code, allSettingsRecorder.Body.String())
	}
	// allSettingsValue 是管理员设置动态键值响应。
	var allSettingsValue settingsResponse
	// allSettingsDecodeErr 是管理员设置响应反序列化失败的原因。
	if allSettingsDecodeErr := json.Unmarshal(allSettingsRecorder.Body.Bytes(), &allSettingsValue); allSettingsDecodeErr != nil {
		t.Fatalf("decode all settings response: %v", allSettingsDecodeErr)
	}
	if allSettingsValue.Entries == nil {
		t.Fatal("all settings response must be an object")
	}

	// userSettingsReq 是读取当前用户设置的请求。
	userSettingsReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/user", nil)
	userSettingsReq.AddCookie(sessionCookie)
	// userSettingsRecorder 是捕获用户设置响应的记录器。
	userSettingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(userSettingsRecorder, userSettingsReq)
	if userSettingsRecorder.Code != http.StatusOK {
		t.Fatalf("user settings status=%d body=%s", userSettingsRecorder.Code, userSettingsRecorder.Body.String())
	}
	// userSettingsValue 是用户设置动态键值响应。
	var userSettingsValue settingsResponse
	// userSettingsDecodeErr 是用户设置响应反序列化失败的原因。
	if userSettingsDecodeErr := json.Unmarshal(userSettingsRecorder.Body.Bytes(), &userSettingsValue); userSettingsDecodeErr != nil {
		t.Fatalf("decode user settings response: %v", userSettingsDecodeErr)
	}
	if userSettingsValue.Entries == nil {
		t.Fatal("user settings response must be an object")
	}

	// 二维码状态与验证使用测试专用平台替身。
	setTestQRLogin(srv, &fakeQRLoginService{status: map[string]any{
		"status":                  "verification_required",
		"session_id":              "contract-qr",
		"verification_screenshot": "data:image/png;base64,screenshot",
		"face_qr_url":             "data:image/png;base64,face-qr",
		"custom_field":            "kept",
		"cookies":                 "must-not-leak",
	}})
	// statusSessionID 是二维码状态测试会话标识。
	statusSessionID := "contract-qr"
	ownQRSession(t, srv, store, statusSessionID)
	// qrStatusReq 是读取二维码状态的请求。
	qrStatusReq := httptest.NewRequest(http.MethodGet, "/api/v1/qr-login/check/"+statusSessionID, nil)
	qrStatusReq.AddCookie(sessionCookie)
	// qrStatusRecorder 是捕获二维码状态响应的记录器。
	qrStatusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(qrStatusRecorder, qrStatusReq)
	if qrStatusRecorder.Code != http.StatusOK {
		t.Fatalf("qr status=%d body=%s", qrStatusRecorder.Code, qrStatusRecorder.Body.String())
	}
	// qrStatusValue 是二维码状态兼容响应。
	var qrStatusValue qrLoginStatusResponse
	// qrStatusDecodeErr 是二维码状态响应反序列化失败的原因。
	if qrStatusDecodeErr := json.Unmarshal(qrStatusRecorder.Body.Bytes(), &qrStatusValue); qrStatusDecodeErr != nil {
		t.Fatalf("decode qr status response: %v", qrStatusDecodeErr)
	}
	if qrStatusValue.Status != "verification_required" || qrStatusValue.SessionID != "contract-qr" {
		t.Fatalf("qr status response=%+v", qrStatusValue)
	}
	if qrStatusValue.VerificationScreenshot != "data:image/png;base64,screenshot" ||
		qrStatusValue.FaceQRURL != "data:image/png;base64,face-qr" {
		t.Fatalf("qr verification fields=%+v", qrStatusValue)
	}
	// leaked 表示二维码状态响应是否意外包含敏感 Cookie 字段。
	var rawQR map[string]any
	// decodeErr 表示原始二维码响应反序列化失败的原因。
	if decodeErr := json.Unmarshal(qrStatusRecorder.Body.Bytes(), &rawQR); decodeErr != nil {
		t.Fatalf("decode raw qr status response: %v", decodeErr)
	}
	// leaked 表示原始响应是否包含不应暴露的 Cookie 字段。
	if _, leaked := rawQR["cookies"]; leaked {
		t.Fatalf("qr status response leaked cookies: %+v", rawQR)
	}

	// 二维码验证完成场景替换测试专用平台替身。
	setTestQRLogin(srv, &fakeQRLoginService{completeCookies: "unb=contract-unb; _m_h5_tk=token;", completeUNB: "contract-unb"})
	// completeSessionID 是二维码验证完成测试会话标识。
	completeSessionID := "contract-complete"
	ownQRSession(t, srv, store, completeSessionID)
	// completeReq 是提交二维码风控验证完成的请求。
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/complete-verification/"+completeSessionID, nil)
	completeReq.AddCookie(sessionCookie)
	// completeRecorder 是捕获验证完成响应的记录器。
	completeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(completeRecorder, completeReq)
	if completeRecorder.Code != http.StatusOK {
		t.Fatalf("complete verification status=%d body=%s", completeRecorder.Code, completeRecorder.Body.String())
	}
	// completeValue 是二维码验证完成具名响应。
	var completeValue qrLoginVerificationResponse
	// completeDecodeErr 是验证完成响应反序列化失败的原因。
	if completeDecodeErr := json.Unmarshal(completeRecorder.Body.Bytes(), &completeValue); completeDecodeErr != nil {
		t.Fatalf("decode complete verification response: %v", completeDecodeErr)
	}
	if !completeValue.Success || completeValue.UNB != "contract-unb" || completeValue.AccountID != "contract-unb" {
		t.Fatalf("complete verification response=%+v", completeValue)
	}
}

// TestReplyAndAccountTaskSuccessResponseContracts 验证回复规则、默认回复和账号任务成功响应的具名 DTO。
func TestReplyAndAccountTaskSuccessResponseContracts(t *testing.T) {
	// srv 是用于验证回复和账号任务响应的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := contractRecordingHandler(t, srv.Router())
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// keywordReq 是创建带商品范围关键词的请求。
	keywordReq := httptest.NewRequest(http.MethodPost, "/api/v1/reply-rules/acc1/items", strings.NewReader(`{"keyword":"契约关键词","reply":"契约回复","item_id":"contract-item"}`))
	keywordReq.Header.Set("Content-Type", "application/json")
	keywordReq.AddCookie(sessionCookie)
	// keywordRecorder 是捕获关键词创建响应的记录器。
	keywordRecorder := httptest.NewRecorder()
	handler.ServeHTTP(keywordRecorder, keywordReq)
	if keywordRecorder.Code != http.StatusOK {
		t.Fatalf("keyword status=%d body=%s", keywordRecorder.Code, keywordRecorder.Body.String())
	}
	// keywordResponse 是关键词创建具名响应 DTO。
	var keywordResponse mutationIDResponse
	// keywordDecodeErr 是关键词创建响应 JSON 反序列化失败的原因。
	if keywordDecodeErr := json.Unmarshal(keywordRecorder.Body.Bytes(), &keywordResponse); keywordDecodeErr != nil {
		t.Fatalf("decode keyword response: %v", keywordDecodeErr)
	}
	if !keywordResponse.Success || keywordResponse.ID == 0 {
		t.Fatalf("keyword response=%+v", keywordResponse)
	}

	// keywordListReq 是读取带类型关键词列表的请求。
	keywordListReq := httptest.NewRequest(http.MethodGet, "/api/v1/reply-rules/acc1/typed", nil)
	keywordListReq.AddCookie(sessionCookie)
	// keywordListRecorder 是捕获关键词列表响应的记录器。
	keywordListRecorder := httptest.NewRecorder()
	handler.ServeHTTP(keywordListRecorder, keywordListReq)
	if keywordListRecorder.Code != http.StatusOK {
		t.Fatalf("keyword list status=%d body=%s", keywordListRecorder.Code, keywordListRecorder.Body.String())
	}
	// keywordListResponse 是关键词列表具名响应 DTO 列表。
	var keywordListResponse []keywordTypedResponse
	// keywordListDecodeErr 是关键词列表响应 JSON 反序列化失败的原因。
	if keywordListDecodeErr := json.Unmarshal(keywordListRecorder.Body.Bytes(), &keywordListResponse); keywordListDecodeErr != nil {
		t.Fatalf("decode keyword list response: %v", keywordListDecodeErr)
	}
	if len(keywordListResponse) != 1 || keywordListResponse[0].ItemID != "contract-item" {
		t.Fatalf("keyword list response=%+v", keywordListResponse)
	}

	// itemReplyReq 是保存指定商品回复的请求。
	itemReplyReq := httptest.NewRequest(http.MethodPut, "/api/v1/reply-rules/items/acc1/contract-item", strings.NewReader(`{"reply_content":"商品契约回复"}`))
	itemReplyReq.Header.Set("Content-Type", "application/json")
	itemReplyReq.AddCookie(sessionCookie)
	// itemReplyRecorder 是捕获指定商品回复响应的记录器。
	itemReplyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(itemReplyRecorder, itemReplyReq)
	if itemReplyRecorder.Code != http.StatusOK {
		t.Fatalf("item reply status=%d body=%s", itemReplyRecorder.Code, itemReplyRecorder.Body.String())
	}
	// itemReplyResponse 是指定商品回复变更具名响应 DTO。
	var itemReplyResponse operationResponse
	// itemReplyDecodeErr 是指定商品回复响应 JSON 反序列化失败的原因。
	if itemReplyDecodeErr := json.Unmarshal(itemReplyRecorder.Body.Bytes(), &itemReplyResponse); itemReplyDecodeErr != nil {
		t.Fatalf("decode item reply response: %v", itemReplyDecodeErr)
	}
	if !itemReplyResponse.Success {
		t.Fatalf("item reply response=%+v", itemReplyResponse)
	}

	// defaultReplyReq 是保存默认回复的请求。
	defaultReplyReq := httptest.NewRequest(http.MethodPut, "/api/v1/default-replies/acc1", strings.NewReader(`{"enabled":true,"reply_content":"默认契约回复","reply_once":true,"reply_image_url":""}`))
	defaultReplyReq.Header.Set("Content-Type", "application/json")
	defaultReplyReq.AddCookie(sessionCookie)
	// defaultReplyRecorder 是捕获默认回复响应的记录器。
	defaultReplyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(defaultReplyRecorder, defaultReplyReq)
	if defaultReplyRecorder.Code != http.StatusOK {
		t.Fatalf("default reply status=%d body=%s", defaultReplyRecorder.Code, defaultReplyRecorder.Body.String())
	}
	// defaultReplyResponseValue 是默认回复变更具名响应 DTO。
	var defaultReplyResponseValue operationResponse
	// defaultReplyDecodeErr 是默认回复响应 JSON 反序列化失败的原因。
	if defaultReplyDecodeErr := json.Unmarshal(defaultReplyRecorder.Body.Bytes(), &defaultReplyResponseValue); defaultReplyDecodeErr != nil {
		t.Fatalf("decode default reply response: %v", defaultReplyDecodeErr)
	}
	if !defaultReplyResponseValue.Success {
		t.Fatalf("default reply response=%+v", defaultReplyResponseValue)
	}

	// defaultReplyGetReq 是读取默认回复的请求。
	defaultReplyGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/default-replies/acc1", nil)
	defaultReplyGetReq.AddCookie(sessionCookie)
	// defaultReplyGetRecorder 是捕获默认回复查询响应的记录器。
	defaultReplyGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(defaultReplyGetRecorder, defaultReplyGetReq)
	if defaultReplyGetRecorder.Code != http.StatusOK {
		t.Fatalf("default reply get status=%d body=%s", defaultReplyGetRecorder.Code, defaultReplyGetRecorder.Body.String())
	}
	// defaultReplyGetResponse 是默认回复查询具名响应 DTO。
	var defaultReplyGetResponse defaultReplyResponse
	// defaultReplyGetDecodeErr 是默认回复查询响应 JSON 反序列化失败的原因。
	if defaultReplyGetDecodeErr := json.Unmarshal(defaultReplyGetRecorder.Body.Bytes(), &defaultReplyGetResponse); defaultReplyGetDecodeErr != nil {
		t.Fatalf("decode default reply get response: %v", defaultReplyGetDecodeErr)
	}
	if !defaultReplyGetResponse.Enabled || defaultReplyGetResponse.ReplyContent != "默认契约回复" {
		t.Fatalf("default reply get response=%+v", defaultReplyGetResponse)
	}

	// taskSettingsReq 是读取账号任务设置的请求。
	taskSettingsReq := httptest.NewRequest(http.MethodGet, "/api/v1/account-tasks/acc1", nil)
	taskSettingsReq.AddCookie(sessionCookie)
	// taskSettingsRecorder 是捕获账号任务设置响应的记录器。
	taskSettingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(taskSettingsRecorder, taskSettingsReq)
	if taskSettingsRecorder.Code != http.StatusOK {
		t.Fatalf("task settings status=%d body=%s", taskSettingsRecorder.Code, taskSettingsRecorder.Body.String())
	}
	// taskSettingsResponse 是账号任务设置具名响应 DTO。
	var taskSettingsResponse accountTaskSettingsResponse
	// taskSettingsDecodeErr 是账号任务设置响应 JSON 反序列化失败的原因。
	if taskSettingsDecodeErr := json.Unmarshal(taskSettingsRecorder.Body.Bytes(), &taskSettingsResponse); taskSettingsDecodeErr != nil {
		t.Fatalf("decode task settings response: %v", taskSettingsDecodeErr)
	}
	if taskSettingsResponse.AccountID != "acc1" {
		t.Fatalf("task settings response=%+v", taskSettingsResponse)
	}

	// taskRunsReq 是读取账号任务执行记录的请求。
	taskRunsReq := httptest.NewRequest(http.MethodGet, "/api/v1/account-tasks/acc1/runs", nil)
	taskRunsReq.AddCookie(sessionCookie)
	// taskRunsRecorder 是捕获账号任务执行记录响应的记录器。
	taskRunsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(taskRunsRecorder, taskRunsReq)
	if taskRunsRecorder.Code != http.StatusOK {
		t.Fatalf("task runs status=%d body=%s", taskRunsRecorder.Code, taskRunsRecorder.Body.String())
	}
	// taskRunsResponse 是账号任务执行记录列表具名响应 DTO。
	var taskRunsResponse accountTaskRunsResponse
	// taskRunsDecodeErr 是账号任务执行记录响应 JSON 反序列化失败的原因。
	if taskRunsDecodeErr := json.Unmarshal(taskRunsRecorder.Body.Bytes(), &taskRunsResponse); taskRunsDecodeErr != nil {
		t.Fatalf("decode task runs response: %v", taskRunsDecodeErr)
	}
	if taskRunsResponse.Runs == nil {
		t.Fatalf("task runs response=%+v", taskRunsResponse)
	}
}

// TestAnalyticsAdminAndPublicSuccessResponseContracts 验证统计、管理员和二维码公共成功响应的具名 DTO。
func TestAnalyticsAdminAndPublicSuccessResponseContracts(t *testing.T) {
	// srv 是用于验证统计、管理员和公共响应的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := contractRecordingHandler(t, srv.Router())
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// seedErr 是统计测试订单写入失败的原因。
	if _, seedErr := store.DB.ExecContext(context.Background(), `INSERT INTO orders (order_id,item_id,buyer_id,quantity,amount,order_status,cookie_id,created_at) VALUES ('contract-analytics','contract-item','buyer', '2','12.50','completed','acc1','2026-08-14 10:00:00')`); seedErr != nil {
		t.Fatalf("seed analytics order: %v", seedErr)
	}

	// adminStatsReq 是读取管理员全局统计的请求。
	adminStatsReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats", nil)
	adminStatsReq.AddCookie(sessionCookie)
	// adminStatsRecorder 是捕获管理员统计响应的记录器。
	adminStatsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminStatsRecorder, adminStatsReq)
	if adminStatsRecorder.Code != http.StatusOK {
		t.Fatalf("admin stats status=%d body=%s", adminStatsRecorder.Code, adminStatsRecorder.Body.String())
	}
	// adminStatsResponseValue 是管理员统计具名响应 DTO。
	var adminStatsResponseValue adminStatsResponse
	// adminStatsDecodeErr 是管理员统计响应 JSON 反序列化失败的原因。
	if adminStatsDecodeErr := json.Unmarshal(adminStatsRecorder.Body.Bytes(), &adminStatsResponseValue); adminStatsDecodeErr != nil {
		t.Fatalf("decode admin stats response: %v", adminStatsDecodeErr)
	}
	if adminStatsResponseValue.TotalUsers == 0 || adminStatsResponseValue.TotalOrders != 1 {
		t.Fatalf("admin stats response=%+v", adminStatsResponseValue)
	}

	// adminUsersReq 是读取管理员用户列表的请求。
	adminUsersReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	adminUsersReq.AddCookie(sessionCookie)
	// adminUsersRecorder 是捕获管理员用户列表响应的记录器。
	adminUsersRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminUsersRecorder, adminUsersReq)
	if adminUsersRecorder.Code != http.StatusOK {
		t.Fatalf("admin users status=%d body=%s", adminUsersRecorder.Code, adminUsersRecorder.Body.String())
	}
	// adminUsersResponse 是管理员用户列表具名响应 DTO 列表。
	var adminUsersResponse []adminUserResponse
	// adminUsersDecodeErr 是管理员用户列表响应 JSON 反序列化失败的原因。
	if adminUsersDecodeErr := json.Unmarshal(adminUsersRecorder.Body.Bytes(), &adminUsersResponse); adminUsersDecodeErr != nil {
		t.Fatalf("decode admin users response: %v", adminUsersDecodeErr)
	}
	if len(adminUsersResponse) != 1 || adminUsersResponse[0].Username != "admin" {
		t.Fatalf("admin users response=%+v", adminUsersResponse)
	}

	// adminCookiesReq 是读取管理员账号列表的请求。
	adminCookiesReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/cookies", nil)
	adminCookiesReq.AddCookie(sessionCookie)
	// adminCookiesRecorder 是捕获管理员账号列表响应的记录器。
	adminCookiesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminCookiesRecorder, adminCookiesReq)
	if adminCookiesRecorder.Code != http.StatusOK {
		t.Fatalf("admin cookies status=%d body=%s", adminCookiesRecorder.Code, adminCookiesRecorder.Body.String())
	}
	// adminCookiesResponse 是管理员账号列表具名响应 DTO 列表。
	var adminCookiesResponse []adminCookieResponse
	// adminCookiesDecodeErr 是管理员账号列表响应 JSON 反序列化失败的原因。
	if adminCookiesDecodeErr := json.Unmarshal(adminCookiesRecorder.Body.Bytes(), &adminCookiesResponse); adminCookiesDecodeErr != nil {
		t.Fatalf("decode admin cookies response: %v", adminCookiesDecodeErr)
	}
	if len(adminCookiesResponse) != 1 || adminCookiesResponse[0].ID != "acc1" {
		t.Fatalf("admin cookies response=%+v", adminCookiesResponse)
	}

	// dashboardReq 是读取当前用户概览统计的请求。
	dashboardReq := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/dashboard", nil)
	dashboardReq.AddCookie(sessionCookie)
	// dashboardRecorder 是捕获概览统计响应的记录器。
	dashboardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(dashboardRecorder, dashboardReq)
	if dashboardRecorder.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", dashboardRecorder.Code, dashboardRecorder.Body.String())
	}
	// dashboardResponse 是概览统计具名响应 DTO。
	var dashboardResponse dashboardStatsResponse
	// dashboardDecodeErr 是概览统计响应 JSON 反序列化失败的原因。
	if dashboardDecodeErr := json.Unmarshal(dashboardRecorder.Body.Bytes(), &dashboardResponse); dashboardDecodeErr != nil {
		t.Fatalf("decode dashboard response: %v", dashboardDecodeErr)
	}
	if dashboardResponse.TotalOrders != 1 {
		t.Fatalf("dashboard response=%+v", dashboardResponse)
	}

	// analyticsReq 是读取订单分析统计的请求。
	analyticsReq := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/orders?start_date=2026-08-14&end_date=2026-08-14", nil)
	analyticsReq.AddCookie(sessionCookie)
	// analyticsRecorder 是捕获订单分析响应的记录器。
	analyticsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(analyticsRecorder, analyticsReq)
	if analyticsRecorder.Code != http.StatusOK {
		t.Fatalf("analytics status=%d body=%s", analyticsRecorder.Code, analyticsRecorder.Body.String())
	}
	// analyticsResponse 是订单分析具名响应 DTO。
	var analyticsResponse orderAnalyticsResponse
	// analyticsDecodeErr 是订单分析响应 JSON 反序列化失败的原因。
	if analyticsDecodeErr := json.Unmarshal(analyticsRecorder.Body.Bytes(), &analyticsResponse); analyticsDecodeErr != nil {
		t.Fatalf("decode analytics response: %v", analyticsDecodeErr)
	}
	if analyticsResponse.RevenueStats.TotalOrders != 1 || len(analyticsResponse.DailyStats) != 1 {
		t.Fatalf("analytics response=%+v", analyticsResponse)
	}

	// validOrdersReq 是读取有效订单分页的请求。
	validOrdersReq := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/orders/valid?start_date=2026-08-14&end_date=2026-08-14", nil)
	validOrdersReq.AddCookie(sessionCookie)
	// validOrdersRecorder 是捕获有效订单响应的记录器。
	validOrdersRecorder := httptest.NewRecorder()
	handler.ServeHTTP(validOrdersRecorder, validOrdersReq)
	if validOrdersRecorder.Code != http.StatusOK {
		t.Fatalf("valid orders status=%d body=%s", validOrdersRecorder.Code, validOrdersRecorder.Body.String())
	}
	// validOrdersResponseValue 是有效订单分页具名响应 DTO。
	var validOrdersResponseValue validOrdersResponse
	// validOrdersDecodeErr 是有效订单响应 JSON 反序列化失败的原因。
	if validOrdersDecodeErr := json.Unmarshal(validOrdersRecorder.Body.Bytes(), &validOrdersResponseValue); validOrdersDecodeErr != nil {
		t.Fatalf("decode valid orders response: %v", validOrdersDecodeErr)
	}
	if validOrdersResponseValue.Total != 1 || len(validOrdersResponseValue.Orders) != 1 {
		t.Fatalf("valid orders response=%+v", validOrdersResponseValue)
	}

	// 二维码生成使用测试专用平台替身。
	setTestQRLogin(srv, &fakeQRLoginService{})
	// qrReq 是生成扫码登录二维码的请求。
	qrReq := httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/generate", nil)
	qrReq.AddCookie(sessionCookie)
	// qrRecorder 是捕获二维码生成响应的记录器。
	qrRecorder := httptest.NewRecorder()
	handler.ServeHTTP(qrRecorder, qrReq)
	if qrRecorder.Code != http.StatusOK {
		t.Fatalf("qr generate status=%d body=%s", qrRecorder.Code, qrRecorder.Body.String())
	}
	// qrResponse 是二维码生成具名响应 DTO。
	var qrResponse qrLoginGenerateResponse
	// qrDecodeErr 是二维码生成响应 JSON 反序列化失败的原因。
	if qrDecodeErr := json.Unmarshal(qrRecorder.Body.Bytes(), &qrResponse); qrDecodeErr != nil {
		t.Fatalf("decode qr response: %v", qrDecodeErr)
	}
	if !qrResponse.Success || qrResponse.SessionID == "" || qrResponse.QRCodeURL == "" {
		t.Fatalf("qr response=%+v", qrResponse)
	}
}

// TestOpenAPILocalResourceMutationResponses 验证会话、账号和管理员本地资源变更均有真实版本化成功响应。
func TestOpenAPILocalResourceMutationResponses(t *testing.T) {
	// srv、store、cleanup 分别提供独立 HTTP 服务、测试数据库和资源释放责任。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是全量 chi Router，所有请求均通过正式版本化路由执行。
	handler := srv.Router()
	// healthRecorder 保存无需认证的健康检查成功响应。
	healthRecorder := serveOpenAPISuccess(t, handler, nil, http.MethodGet, "/health", "")
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", healthRecorder.Code, healthRecorder.Body.String())
	}
	// sessionCookie 是初始管理员会话，用于验证当前密码并执行账号管理操作。
	sessionCookie := loginHelper(t, handler)
	// passwordRecorder 保存当前用户修改密码后的成功响应；该操作会废弃旧会话。
	passwordRecorder := serveOpenAPISuccess(t, handler, sessionCookie, http.MethodPost, "/api/v1/session/password", `{"current_password":"pw","new_password":"new-password"}`)
	if passwordRecorder.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", passwordRecorder.Code, passwordRecorder.Body.String())
	}
	// refreshedCookie 是使用新密码重新登录后的会话，后续操作不依赖已撤销的旧会话。
	refreshedCookie := loginAsHelper(t, handler, "admin", "new-password")
	// credentialsRecorder 保存管理员用户名更新成功响应；当前密码保持新密码以验证敏感输入仅在服务端处理。
	credentialsRecorder := serveOpenAPISuccess(t, handler, refreshedCookie, http.MethodPut, "/api/v1/session/credentials", `{"current_password":"new-password","new_username":"admin-contract"}`)
	if credentialsRecorder.Code != http.StatusOK {
		t.Fatalf("update credentials status=%d body=%s", credentialsRecorder.Code, credentialsRecorder.Body.String())
	}
	// renamedCookie 是用户名变更后重新登录得到的会话，确保后续管理员操作使用当前身份。
	renamedCookie := loginAsHelper(t, handler, "admin-contract", "new-password")
	// adminPasswordRecorder 保存管理员密码变更成功响应，覆盖管理员专属密码 operation。
	adminPasswordRecorder := serveOpenAPISuccess(t, handler, renamedCookie, http.MethodPost, "/api/v1/admin/password", `{"current_password":"new-password","new_password":"final-password"}`)
	if adminPasswordRecorder.Code != http.StatusOK {
		t.Fatalf("change admin password status=%d body=%s", adminPasswordRecorder.Code, adminPasswordRecorder.Body.String())
	}
	// finalCookie 是最终密码对应的管理员会话，供账号和用户删除生命周期使用。
	finalCookie := loginAsHelper(t, handler, "admin-contract", "final-password")
	// accountRecorder 保存新建可删除账号的成功响应；请求内 Cookie 只用于测试输入，响应不得返回该明文。
	accountRecorder := serveOpenAPISuccess(t, handler, finalCookie, http.MethodPost, "/api/v1/accounts", `{"id":"delete-contract","value":"unb=delete-contract","login_method":"manual"}`)
	if accountRecorder.Code != http.StatusOK || strings.Contains(accountRecorder.Body.String(), "unb=delete-contract") {
		t.Fatalf("create account response invalid: status=%d body=%s", accountRecorder.Code, accountRecorder.Body.String())
	}
	// deleteAccountRecorder 保存同一账号的删除成功响应，形成真实账号资源生命周期闭环。
	deleteAccountRecorder := serveOpenAPISuccess(t, handler, finalCookie, http.MethodDelete, "/api/v1/accounts/delete-contract", "")
	if deleteAccountRecorder.Code != http.StatusOK {
		t.Fatalf("delete account status=%d body=%s", deleteAccountRecorder.Code, deleteAccountRecorder.Body.String())
	}
	// created、createUserErr 分别表示普通用户是否已写入和创建失败原因。
	created, createUserErr := store.Users.Create(context.Background(), "delete-user", "delete-user@example.com", "pw")
	if createUserErr != nil {
		t.Fatalf("create deletable user: %v", createUserErr)
	}
	if !created {
		t.Fatal("create deletable user returned false")
	}
	// deletableUser、getUserErr 分别保存待删除普通用户的稳定数据库 ID 和重新读取失败原因。
	deletableUser, getUserErr := store.Users.GetByUsername(context.Background(), "delete-user")
	if getUserErr != nil || deletableUser == nil {
		t.Fatalf("load deletable user: user=%+v err=%v", deletableUser, getUserErr)
	}
	// deleteUserRecorder 保存管理员删除普通用户的成功响应，避免把不存在用户的 4xx 当作成功证据。
	deleteUserRecorder := serveOpenAPISuccess(t, handler, finalCookie, http.MethodDelete, "/api/v1/admin/users/"+strconv.FormatInt(deletableUser.ID, 10), "")
	if deleteUserRecorder.Code != http.StatusOK {
		t.Fatalf("delete user status=%d body=%s", deleteUserRecorder.Code, deleteUserRecorder.Body.String())
	}
	// automationCardID、automationCreateRecorder 分别保存自动化规则所需卡券 ID 和创建响应。
	automationCardID, cardErr := store.Cards.Create(context.Background(), &db.CardFull{Name: "契约自动化卡", Type: "text", TextContent: "CARD", Enabled: true, UserID: 1})
	if cardErr != nil {
		t.Fatalf("create automation card: %v", cardErr)
	}
	// itemErr 表示自动化规则关联商品夹具写入失败。
	if _, itemErr := store.DB.ExecContext(context.Background(), `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','contract-automation-item','自动化商品')`); itemErr != nil {
		t.Fatalf("create automation item: %v", itemErr)
	}
	// automationCreateRecorder 保存自动化规则创建成功响应。
	automationCreateRecorder := serveOpenAPISuccess(t, handler, finalCookie, http.MethodPost, "/api/v1/automation-rules", `{"cookie_id":"acc1","item_id":"contract-automation-item","trigger_type":"order_paid","enabled":true,"actions":[{"action_type":"send_card","card_id":`+strconv.FormatInt(automationCardID, 10)+`,"delivery_count":1}]}`)
	// automationCreateResponse 保存自动化规则创建返回的数值主键。
	var automationCreateResponse mutationIDResponse
	// decodeErr 表示解析自动化规则创建响应失败。
	if decodeErr := json.Unmarshal(automationCreateRecorder.Body.Bytes(), &automationCreateResponse); decodeErr != nil || automationCreateResponse.ID == 0 {
		t.Fatalf("decode automation create: id=%d err=%v body=%s", automationCreateResponse.ID, decodeErr, automationCreateRecorder.Body.String())
	}
	// automationRuleID 是后续更新和删除操作使用的规则路径标识。
	automationRuleID := strconv.FormatInt(automationCreateResponse.ID, 10)
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPut, "/api/v1/automation-rules/"+automationRuleID, `{"cookie_id":"acc1","item_id":"contract-automation-item","trigger_type":"order_paid","enabled":false,"actions":[{"action_type":"send_card","card_id":`+strconv.FormatInt(automationCardID, 10)+`,"delivery_count":1}]}`)
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodDelete, "/api/v1/automation-rules/"+automationRuleID, "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPut, "/api/v1/default-replies/acc1", `{"enabled":true,"reply_content":"契约默认回复","reply_once":true}`)
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodGet, "/api/v1/default-replies/acc1", "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodGet, "/api/v1/default-replies", "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodGet, "/api/v1/default-replies/list", "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPost, "/api/v1/default-replies/acc1/clear-records", "{}")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodDelete, "/api/v1/default-replies/acc1", "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodGet, "/api/v1/items/cookie/acc1", "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodGet, "/api/v1/items/publish-batches", "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodGet, "/api/v1/cards/1/details", "")
	// orderID 是用于删除订单成功场景的本地订单标识。
	orderID := "contract-delete-order"
	// orderErr 表示订单删除成功场景夹具写入失败。
	if _, orderErr := store.DB.ExecContext(context.Background(), `INSERT INTO orders (order_id,item_id,buyer_id,order_status,cookie_id) VALUES ('contract-delete-order','contract-automation-item','buyer','pending_ship','acc1')`); orderErr != nil {
		t.Fatalf("create delete order: %v", orderErr)
	}
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodDelete, "/api/v1/orders/"+orderID, "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPut, "/api/v1/items/acc1/contract-automation-item/multi-spec", `{"enabled":true}`)
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPut, "/api/v1/items/acc1/contract-automation-item/multi-quantity-delivery", `{"enabled":true}`)
	// keywordRecorder 保存普通关键词规则创建成功响应，后续通过带类型列表获取稳定 ID。
	keywordRecorder := serveOpenAPISuccess(t, handler, finalCookie, http.MethodPost, "/api/v1/reply-rules/acc1", `{"keyword":"契约关键字","reply":"契约回复"}`)
	if keywordRecorder.Code != http.StatusOK {
		t.Fatalf("create keyword status=%d body=%s", keywordRecorder.Code, keywordRecorder.Body.String())
	}
	// keywordListRecorder 保存带类型关键词列表成功响应。
	keywordListRecorder := serveOpenAPISuccess(t, handler, finalCookie, http.MethodGet, "/api/v1/reply-rules/acc1/typed", "")
	// keywordRows 保存带稳定数值主键的关键词规则集合。
	var keywordRows []keywordTypedResponse
	// keywordListErr 表示解析带类型关键词列表失败。
	if keywordListErr := json.Unmarshal(keywordListRecorder.Body.Bytes(), &keywordRows); keywordListErr != nil || len(keywordRows) == 0 {
		t.Fatalf("load typed keywords: rows=%+v err=%v", keywordRows, keywordListErr)
	}
	// keywordID 是当前创建规则的稳定主键，用于更新、按 ID 删除与按索引删除的成功场景。
	keywordID := strconv.FormatInt(keywordRows[0].ID, 10)
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPut, "/api/v1/reply-rules/acc1/typed/"+keywordID, `{"keyword":"契约关键字","reply":"已更新回复","type":"text"}`)
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodDelete, "/api/v1/reply-rules/acc1/typed/"+keywordID, "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPost, "/api/v1/reply-rules/acc1", `{"keyword":"索引关键字","reply":"索引回复"}`)
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodDelete, "/api/v1/reply-rules/acc1/index/0", "")
	serveOpenAPISuccess(t, handler, finalCookie, http.MethodPut, "/api/v1/account-tasks/acc1", `{"auto_rate_enabled":true,"rate_content":"服务很好","auto_polish_enabled":false,"polish_time":"03:00"}`)
}
