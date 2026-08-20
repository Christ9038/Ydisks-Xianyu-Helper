package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	automationapp "xianyu-go/internal/application/automation"
	chatapp "xianyu-go/internal/application/chat"
	itemapp "xianyu-go/internal/application/items"
	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// TestOpenAPIRemainingVersionedSuccessResponses 验证仍未被既有兼容测试触达的版本化本地成功响应。
func TestOpenAPIRemainingVersionedSuccessResponses(t *testing.T) {
	// t.Run 将相互独立的真实 Router 场景隔离，避免各自的数据库状态互相影响。
	t.Run("initialize", testOpenAPISessionInitializeSuccess)
	t.Run("long-login", testOpenAPILongLoginSuccess)
	t.Run("ai-models", testOpenAPIAIModelsSuccess)
	t.Run("cards-batch", testOpenAPICardsBatchSuccess)
	t.Run("chat-history-and-read", testOpenAPIChatHistoryAndReadSuccess)
	t.Run("local-item-and-manual-ship", testOpenAPILocalItemAndManualShipSuccess)
	t.Run("category-recommendation", testOpenAPIItemCategoryRecommendationSuccess)
	t.Run("publish-batch-preview", testOpenAPIPublishBatchPreviewSuccess)
	t.Run("uncertain-notifications", testOpenAPIUncertainNotificationSuccess)
	t.Run("chat-send", testOpenAPIChatSendSuccess)
	t.Run("item-sync", testOpenAPIItemSyncSuccess)
	t.Run("item-publish", testOpenAPIItemPublishSuccess)
	t.Run("batch-lifecycle", testOpenAPIBatchLifecycleSuccess)
	t.Run("automation-resolve", testOpenAPIAutomationResolveSuccess)
	t.Run("notification-test", testOpenAPINotificationChannelTestSuccess)
	t.Run("account-task-run", testOpenAPIAccountTaskRunSuccess)
}

// testOpenAPISessionInitializeSuccess 覆盖首次初始化的真实公开成功响应。
func testOpenAPISessionInitializeSuccess(t *testing.T) {
	// srv、_、cleanup 分别是未初始化服务、当前场景无需读取的存储和资源释放函数。
	srv, _, cleanup := newUninitializedTestServer(t)
	defer cleanup()
	// handler 是未经包装的真实 Router，初始化会在响应中写入会话 Cookie。
	handler := srv.Router()
	// request 是提交首次管理员密码的版本化初始化请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/initialize", strings.NewReader(`{"password":"initial-password"}`))
	request.Header.Set("Content-Type", "application/json")
	// recorder 保存真实 handler 返回的状态、响应头和响应体。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// testOpenAPILongLoginSuccess 覆盖长登录读取和开关更新的真实平台代理成功响应。
func testOpenAPILongLoginSuccess(t *testing.T) {
	// passport 是本地续期服务替身，仅提供真实 handler 所需的平台协议响应。
	passport := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		// responseWriter、request 分别承载本地平台替身的响应和请求。
		responseWriter.Header().Add("Set-Cookie", "havana_lgc_exp=4102444800000; Domain=.goofish.com; Path=/; Secure")
		if strings.Contains(request.URL.Path, "set") {
			_, _ = responseWriter.Write([]byte(`{"data":{"success":true}}`))
			return
		}
		_, _ = responseWriter.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	defer passport.Close()
	// srv、store、cleanup 分别是服务、用于准备浏览器 Cookie 快照的存储和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	setTestCookieRenew(srv, xrenew.Service{HTTPClient: passport.Client(), QueryLoginSettingsURL: passport.URL + "/query", SetLoginSettingsURL: passport.URL + "/set"})
	// detail、detailErr 保存当前账号的加密凭证详情及读取错误。
	detail, detailErr := store.Cookies.GetDetails(context.Background(), "acc1")
	if detailErr != nil {
		t.Fatalf("读取长登录账号失败: %v", detailErr)
	}
	// metadata 保存带域名快照的凭证元数据，使真实续期适配器可调用本地平台替身。
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, cookierefresh.SnapshotFromCookieString(detail.Value, ".goofish.com"))
	// updateErr 表示写入测试长登录 Cookie 快照失败。
	if updateErr := store.Cookies.UpdateRenewalCookie(context.Background(), "acc1", detail.Value, metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("准备长登录凭证快照失败: %v", updateErr)
	}
	// handler 是当前场景使用的真实版本化 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// requests 保存读取与写入长登录状态的两个版本化成功请求。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/accounts/acc1/long-login", nil),
		httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/long-login", strings.NewReader(`{"enabled":true}`)),
	}
	// request 是当前真实长登录请求。
	for _, request := range requests {
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(sessionCookie)
		// recorder 保存当前请求的完整真实响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
}

// testOpenAPIAIModelsSuccess 覆盖 AI 模型代理的本地成功响应。
func testOpenAPIAIModelsSuccess(t *testing.T) {
	// modelServer 模拟兼容 OpenAI 的模型列表端点。
	modelServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		// responseWriter、request 分别承载模型替身响应和请求；请求内容无需在本场景读取。
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"contract-model"}]}`))
	}))
	defer modelServer.Close()
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是访问管理员模型代理所需的认证会话。
	sessionCookie := loginHelper(t, handler)
	// request 是指向本地模型替身的版本化 AI 模型请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/ai-models", strings.NewReader(`{"base_url":"`+modelServer.URL+`","api_key":"test-key"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(sessionCookie)
	// recorder 保存真实代理成功响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// testOpenAPICardsBatchSuccess 覆盖卡券 CSV 上传的真实 multipart 成功响应。
func testOpenAPICardsBatchSuccess(t *testing.T) {
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是当前管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// body 保存上传 CSV 的 multipart 编码结果。
	var body bytes.Buffer
	// writer 负责组装符合真实 handler 预期的 multipart 请求。
	writer := multipart.NewWriter(&body)
	// file、fileErr 分别是 CSV 表单文件写入器及创建失败原因。
	file, fileErr := writer.CreateFormFile("file", "cards.csv")
	if fileErr != nil {
		t.Fatalf("创建卡券表单文件失败: %v", fileErr)
	}
	// writeErr 表示写入测试卡券 CSV 内容失败。
	if _, writeErr := io.WriteString(file, "name,type,content\n合同卡,text,serial-001\n"); writeErr != nil {
		t.Fatalf("写入卡券 CSV 失败: %v", writeErr)
	}
	// closeErr 表示完成卡券 multipart 编码失败。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("关闭卡券 multipart 失败: %v", closeErr)
	}
	// request 是真实卡券批量上传请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cards/batch", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(sessionCookie)
	// recorder 保存批量创建的真实成功响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// testOpenAPIChatHistoryAndReadSuccess 覆盖聊天消息查询与本地已读状态更新。
func testOpenAPIChatHistoryAndReadSuccess(t *testing.T) {
	// srv、store、cleanup 分别是启用聊天应用的服务、夹具存储和资源释放函数。
	srv, store, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// saveErr 表示写入可查询聊天消息失败。
	_, _, saveErr := store.Chats.SaveMessage(context.Background(), db.ChatSession{CookieID: "acc1", ChatID: "chat-contract", BuyerID: "buyer-contract", BuyerName: "契约买家", ItemImageURL: "https://img.example/item.jpg"}, db.ChatMessage{MessageKey: "message-contract", Direction: "incoming", SenderID: "buyer-contract", SenderName: "契约买家", MessageType: "audio", Content: "https://media.example/voice.amr", MediaDuration: 3, Status: "received", SentAt: 1}, true)
	if saveErr != nil {
		t.Fatalf("写入聊天夹具失败: %v", saveErr)
	}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是当前管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// requests 保存消息读取与已读更新的真实版本化请求。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/chat/messages?account_id=acc1&chat_id=chat-contract", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/chat/read", strings.NewReader(`{"account_id":"acc1","chat_id":"chat-contract"}`)),
	}
	// request 是当前待验证的聊天 HTTP 请求。
	for _, request := range requests {
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(sessionCookie)
		// recorder 保存真实 handler 的成功响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
	// channelResult、channelErr 保存删除场景所需通知渠道的插入结果。
	channelResult, channelErr := store.DB.ExecContext(context.Background(), `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('contract-delete','webhook','{}',1,1)`)
	if channelErr != nil {
		t.Fatalf("创建删除通知渠道失败: %v", channelErr)
	}
	// channelID、channelIDErr 保存删除场景通知渠道主键及读取错误。
	channelID, channelIDErr := channelResult.LastInsertId()
	if channelIDErr != nil {
		t.Fatalf("读取删除通知渠道主键失败: %v", channelIDErr)
	}
	// notificationResult、notificationErr 保存待删除消息通知夹具的主键及插入结果。
	notificationResult, notificationErr := store.DB.ExecContext(context.Background(), `INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES ('acc1',?,1)`, channelID)
	if notificationErr != nil {
		t.Fatalf("创建消息通知夹具失败: %v", notificationErr)
	}
	// notificationID、notificationIDErr 保存消息通知主键及读取错误。
	notificationID, notificationIDErr := notificationResult.LastInsertId()
	if notificationIDErr != nil {
		t.Fatalf("读取消息通知主键失败: %v", notificationIDErr)
	}
	// deleteRequest 是删除单条消息通知的版本化请求。
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/messages/"+strconv.FormatInt(notificationID, 10), nil)
	deleteRequest.AddCookie(sessionCookie)
	// deleteRecorder 保存删除响应。
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteRequest)
	assertOpenAPIRecordedSuccessResponse(t, deleteRequest, deleteRecorder)
}

// testOpenAPILocalItemAndManualShipSuccess 覆盖无需平台请求的商品创建和状态型手动发货成功响应。
func testOpenAPILocalItemAndManualShipSuccess(t *testing.T) {
	// srv、store、cleanup 分别是服务、订单夹具存储和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// seedErr 表示写入待发货订单夹具失败。
	_, seedErr := store.DB.ExecContext(context.Background(), `INSERT INTO orders (order_id,item_id,buyer_id,order_status,cookie_id,chat_id) VALUES ('contract-manual-ship','contract-item','buyer-contract','2','acc1','chat-contract')`)
	if seedErr != nil {
		t.Fatalf("写入手动发货订单失败: %v", seedErr)
	}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是当前管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// requests 保存商品创建和状态型手动发货的版本化请求。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/items/acc1", strings.NewReader(`{"item_id":"contract-created","item_title":"契约商品","item_price":"10.00"}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/orders/manual-ship", strings.NewReader(`{"order_ids":["contract-manual-ship"],"ship_mode":"status_only"}`)),
	}
	// request 是当前待验证的本地资源请求。
	for _, request := range requests {
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(sessionCookie)
		// recorder 保存当前请求的完整成功响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
}

// testOpenAPIItemCategoryRecommendationSuccess 覆盖商品类目推荐的本地 MTOP 成功响应。
func testOpenAPIItemCategoryRecommendationSuccess(t *testing.T) {
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// client 是仅返回类目推荐结构的本地 MTOP 客户端。
	client := mtop.NewClient()
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		// request 是类目推荐的 MTOP 请求；本场景只需返回确定性成功数据。
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{"cardList":[{"cardData":{"propertyId":"-10000","propertyName":"分类","valuesList":[{"catId":"5001","catName":"虚拟商品","channelCatId":"6001","isClicked":"1"}]}}]}}`)), Request: request}, nil
	})}
	setTestMTop(srv, client)
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是当前管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// request 是商品类目推荐的版本化请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-categories/recommend", strings.NewReader(`{"cookie_id":"acc1","keyword":"资料"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(sessionCookie)
	// recorder 保存真实 handler 的成功响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// testOpenAPIPublishBatchPreviewSuccess 覆盖批量发布预检的真实 multipart 成功响应。
func testOpenAPIPublishBatchPreviewSuccess(t *testing.T) {
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是当前管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// body 保存预检上传的 multipart 编码结果。
	var body bytes.Buffer
	// writer 负责组装真实预检入口需要的默认账号、CSV 和图片压缩包字段。
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("default_cookie_id", "acc1")
	_ = writer.WriteField("fallback_category_id", "5001")
	_ = writer.WriteField("fallback_category_name", "虚拟商品")
	_ = writer.WriteField("fallback_channel_category_id", "6001")
	// csvFile、csvErr 分别是预检 CSV 文件写入器和创建错误。
	csvFile, csvErr := writer.CreateFormFile("file", "products.csv")
	if csvErr != nil {
		t.Fatalf("创建预检 CSV 失败: %v", csvErr)
	}
	// writeErr 表示写入测试预检 CSV 内容失败。
	if _, writeErr := io.WriteString(csvFile, "账号ID,标题,价格,库存,图片\nacc1,契约商品,12.50,1,\n"); writeErr != nil {
		t.Fatalf("写入预检 CSV 失败: %v", writeErr)
	}
	// closeErr 表示完成预检 multipart 编码失败。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("关闭预检 multipart 失败: %v", closeErr)
	}
	// request 是批量发布预检的版本化上传请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-batches/preview", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(sessionCookie)
	// recorder 保存真实预检成功响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// testOpenAPIUncertainNotificationSuccess 覆盖用户和管理员不确定通知摘要的真实成功响应。
func testOpenAPIUncertainNotificationSuccess(t *testing.T) {
	// srv、store、cleanup 分别是服务、通知夹具存储和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// result、insertErr 分别是渠道插入结果和数据库错误。
	result, insertErr := store.DB.ExecContext(context.Background(), `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('contract','webhook','{}',1,1)`)
	if insertErr != nil {
		t.Fatalf("创建通知渠道失败: %v", insertErr)
	}
	// channelID、idErr 分别是渠道标识和读取主键错误。
	channelID, idErr := result.LastInsertId()
	if idErr != nil {
		t.Fatalf("读取通知渠道标识失败: %v", idErr)
	}
	// enqueueErr 表示写入 outbox 夹具失败。
	enqueueErr := store.Notifications.EnqueueOutbox(context.Background(), []db.NotificationOutboxInput{{ChannelID: channelID, EventType: "contract"}})
	if enqueueErr != nil {
		t.Fatalf("写入通知 outbox 失败: %v", enqueueErr)
	}
	// claimed、claimErr 分别是被本地 worker 领取的 outbox 项和领取错误。
	claimed, claimErr := store.Notifications.ClaimOutbox(context.Background(), "contract-worker", time.Now(), 1)
	if claimErr != nil || len(claimed) != 1 {
		t.Fatalf("领取通知 outbox 失败: items=%+v err=%v", claimed, claimErr)
	}
	// marked、markErr 分别表示不确定状态是否写入及写入错误。
	if marked, markErr := store.Notifications.MarkOutboxUncertain(context.Background(), claimed[0].ID, "contract-worker", "contract-error"); markErr != nil || !marked {
		t.Fatalf("标记不确定通知失败: marked=%v err=%v", marked, markErr)
	}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// requests 保存用户与管理员不确定通知列表请求。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/notifications/outbox/uncertain?limit=10", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/notifications/outbox/uncertain?limit=10", nil),
	}
	// request 是当前待验证的不确定通知查询请求。
	for _, request := range requests {
		request.AddCookie(sessionCookie)
		// recorder 保存当前真实成功响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
}

// _ 使用 encoding/json 保持本文件与未来需解码的覆盖场景共用稳定依赖。
var _ = json.Valid

// testOpenAPIChatSendSuccess 使用最小聊天端口验证文字和图片成功响应的 HTTP 形状。
func testOpenAPIChatSendSuccess(t *testing.T) {
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// srv.applications.chat 注入只返回确定性消息的测试端口，避免调用真实账号运行时。
	srv.applications.chat = contractChatPort{}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// textRequest 是文字消息发送请求。
	textRequest := httptest.NewRequest(http.MethodPost, "/api/v1/chat/messages", strings.NewReader(`{"account_id":"acc1","chat_id":"chat","buyer_id":"buyer","text":"hello"}`))
	textRequest.Header.Set("Content-Type", "application/json")
	textRequest.AddCookie(sessionCookie)
	// textRecorder 保存文字发送响应。
	textRecorder := httptest.NewRecorder()
	handler.ServeHTTP(textRecorder, textRequest)
	assertOpenAPIRecordedSuccessResponse(t, textRequest, textRecorder)
	// imageBody 保存图片发送请求的 multipart 内容。
	var imageBody bytes.Buffer
	// imageWriter 负责写入图片发送字段。
	imageWriter := multipart.NewWriter(&imageBody)
	_ = imageWriter.WriteField("account_id", "acc1")
	_ = imageWriter.WriteField("chat_id", "chat")
	_ = imageWriter.WriteField("buyer_id", "buyer")
	// imageFile 是聊天图片表单文件。
	imageFile, imageErr := imageWriter.CreatePart(textproto.MIMEHeader{"Content-Disposition": []string{`form-data; name="image"; filename="chat.png"`}, "Content-Type": []string{"image/png"}})
	if imageErr != nil {
		t.Fatalf("创建聊天图片失败: %v", imageErr)
	}
	_, _ = imageFile.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	_ = imageWriter.Close()
	// imageRequest 是图片发送版本化请求。
	imageRequest := httptest.NewRequest(http.MethodPost, "/api/v1/chat/images", &imageBody)
	imageRequest.Header.Set("Content-Type", imageWriter.FormDataContentType())
	imageRequest.AddCookie(sessionCookie)
	// imageRecorder 保存图片发送响应。
	imageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(imageRecorder, imageRequest)
	assertOpenAPIRecordedSuccessResponse(t, imageRequest, imageRecorder)
}

// testOpenAPIItemSyncSuccess 使用本地 MTOP 返回商品列表，验证全量和分页同步响应。
func testOpenAPIItemSyncSuccess(t *testing.T) {
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// client 是返回最小有效商品卡片的本地 MTOP 客户端。
	client := mtop.NewClient()
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		// request 是同步请求；返回值只包含本地同步需要的非敏感商品字段。
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{"cardList":[{"cardData":{"id":"sync-contract","title":"同步商品","priceInfo":{"price":"10.00","preText":"¥"},"picInfo":{"picUrl":"https://img.example/s.png"},"detailParams":{"itemId":"sync-contract"}}}]}}`)), Request: request}, nil
	})}
	setTestMTop(srv, client)
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// requests 保存全量和分页同步请求。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/items/get-all-from-account", strings.NewReader(`{"cookie_id":"acc1","page_size":10}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/items/get-by-page", strings.NewReader(`{"cookie_id":"acc1","page_number":1,"page_size":10}`)),
	}
	// request 是当前同步请求。
	for _, request := range requests {
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(sessionCookie)
		// recorder 保存真实同步响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
}

// testOpenAPIItemPublishSuccess 注入商品发布端口并验证 multipart 发布成功响应。
func testOpenAPIItemPublishSuccess(t *testing.T) {
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.applications.itemSinglePublish = contractItemPublishPort{}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// body 保存发布接口要求的 multipart 字段。
	var body bytes.Buffer
	// writer 负责组装发布表单。
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("cookie_id", "acc1")
	_ = writer.WriteField("title", "契约发布")
	_ = writer.WriteField("price", "10.00")
	_ = writer.WriteField("original_price", "10.00")
	_ = writer.WriteField("quantity", "1")
	// image 是发布图片表单写入器。
	image, imageErr := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": []string{`form-data; name="images"; filename="contract.png"`}, "Content-Type": []string{"image/png"}})
	if imageErr != nil {
		t.Fatalf("创建发布图片失败: %v", imageErr)
	}
	_, _ = image.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	_ = writer.Close()
	// request 是真实版本化商品发布请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(sessionCookie)
	// recorder 保存真实发布响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// testOpenAPIBatchLifecycleSuccess 通过预检批次覆盖批次查询、取消和删除成功响应。
func testOpenAPIBatchLifecycleSuccess(t *testing.T) {
	// srv、store、cleanup 分别是服务、批次状态夹具存储和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// batchID 是真实预检批次标识。
	batchID := previewPublishBatch(t, handler, sessionCookie)
	// requests 保存批次详情、取消和删除请求；取消后批次可安全删除。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/items/publish-batches/"+batchID, nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-batches/"+batchID+"/cancel", nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/items/publish-batches/"+batchID, nil),
	}
	// request 是当前批次生命周期请求。
	for _, request := range requests {
		request.AddCookie(sessionCookie)
		// recorder 保存当前批次成功响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
	// startBatchID 是另一条真实预检批次标识，专门覆盖启动 operation。
	startBatchID := previewPublishBatch(t, handler, sessionCookie)
	// startRequest 是启动批量发布的版本化请求。
	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-batches", strings.NewReader(`{"preview_id":"`+startBatchID+`"}`))
	startRequest.Header.Set("Content-Type", "application/json")
	startRequest.AddCookie(sessionCookie)
	// startRecorder 保存启动响应。
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startRequest)
	assertOpenAPIRecordedSuccessResponse(t, startRequest, startRecorder)
	// retryBatchID 是预置为失败状态的批次标识，确保重试 operation 真实进入成功分支。
	retryBatchID := previewPublishBatch(t, handler, sessionCookie)
	// updateErr 表示写入批次失败终态失败。
	if _, updateErr := store.DB.ExecContext(context.Background(), `UPDATE item_publish_batches SET status='failed',failed_count=1 WHERE id=?`, retryBatchID); updateErr != nil {
		t.Fatalf("设置重试批次状态失败: %v", updateErr)
	}
	// updateErr 表示写入失败批次明细失败。
	if _, updateErr := store.DB.ExecContext(context.Background(), `UPDATE item_publish_batch_rows SET status='failed',error_message='contract failure' WHERE batch_id=?`, retryBatchID); updateErr != nil {
		t.Fatalf("设置重试批次明细失败: %v", updateErr)
	}
	// retryRequest 是重试失败明细的版本化请求。
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-batches/"+retryBatchID+"/retry-failed", nil)
	retryRequest.AddCookie(sessionCookie)
	// retryRecorder 保存重试响应。
	retryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(retryRecorder, retryRequest)
	assertOpenAPIRecordedSuccessResponse(t, retryRequest, retryRecorder)
}

// testOpenAPIAutomationResolveSuccess 写入真实死信延期任务并验证人工处理成功响应。
func testOpenAPIAutomationResolveSuccess(t *testing.T) {
	// srv、store、cleanup 分别是服务、自动化夹具存储和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ruleResult、ruleErr 保存异常运行归属规则的插入结果。
	ruleResult, ruleErr := store.DB.ExecContext(context.Background(), `INSERT INTO automation_rules (user_id,cookie_id,name,trigger_type,enabled,config_json) VALUES (1,'acc1','contract-rule','order_paid',1,'{}')`)
	if ruleErr != nil {
		t.Fatalf("写入自动化规则失败: %v", ruleErr)
	}
	// ruleID、ruleIDErr 保存异常运行归属规则主键及读取错误。
	ruleID, ruleIDErr := ruleResult.LastInsertId()
	if ruleIDErr != nil {
		t.Fatalf("读取自动化规则主键失败: %v", ruleIDErr)
	}
	// runResult、runErr 保存待处理异常运行的插入结果。
	runResult, runErr := store.DB.ExecContext(context.Background(), `INSERT INTO automation_runs (rule_id,cookie_id,trigger_type,trigger_key,status,raw_event_json,error_message) VALUES (?,?,?,'contract-run','needs_review','{"AccountID":"acc1"}','contract failure')`, ruleID, "acc1", "order_paid")
	if runErr != nil {
		t.Fatalf("写入自动化异常运行失败: %v", runErr)
	}
	// runID、runIDErr 保存待处理异常运行主键及读取错误。
	runID, runIDErr := runResult.LastInsertId()
	if runIDErr != nil {
		t.Fatalf("读取自动化异常运行主键失败: %v", runIDErr)
	}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// runRequest 是取消异常运行的版本化请求。
	runRequest := httptest.NewRequest(http.MethodPost, "/api/v1/automation-runs/"+strconv.FormatInt(runID, 10)+"/resolve", strings.NewReader(`{"resolution":"cancel"}`))
	runRequest.Header.Set("Content-Type", "application/json")
	runRequest.AddCookie(sessionCookie)
	// runRecorder 保存异常运行处理响应。
	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, runRequest)
	assertOpenAPIRecordedSuccessResponse(t, runRequest, runRecorder)
	// insertErr 表示写入死信延期任务失败。
	_, insertErr := store.DB.ExecContext(context.Background(), `INSERT INTO automation_pending_tasks (task_key,cookie_id,trigger_type,task_json,status,attempt_count,error_message) VALUES ('contract-task','acc1','contract','{}','dead_letter',3,'failed')`)
	if insertErr != nil {
		t.Fatalf("写入自动化死信任务失败: %v", insertErr)
	}
	// taskID 保存刚刚写入的延期任务主键。
	var taskID int64
	// scanErr 表示读取延期任务主键失败。
	if scanErr := store.DB.QueryRowContext(context.Background(), `SELECT id FROM automation_pending_tasks WHERE task_key='contract-task'`).Scan(&taskID); scanErr != nil {
		t.Fatalf("读取自动化任务标识失败: %v", scanErr)
	}
	// request 是驳回延期任务的版本化请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/automation-pending-tasks/"+strconv.FormatInt(taskID, 10)+"/resolve", strings.NewReader(`{"resolution":"dismiss"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(sessionCookie)
	// recorder 保存真实人工处理响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// testOpenAPINotificationChannelTestSuccess 验证通知渠道测试的真实网络成功响应。
func testOpenAPINotificationChannelTestSuccess(t *testing.T) {
	// endpoint 是接收通知测试请求的本地 Webhook。
	endpoint := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// srv.applications.notificationChannels 注入成功替身，避免测试依赖进程级通知器。
	srv.applications.notificationChannels = contractNotificationChannelsPort{}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// createRequest 创建本地 Webhook 渠道。
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/channels", strings.NewReader(`{"name":"契约Webhook","type":"webhook","config":"{\"url\":\"`+endpoint.URL+`\"}","enabled":true}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.AddCookie(sessionCookie)
	// createRecorder 保存渠道创建响应。
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)
	assertOpenAPIRecordedSuccessResponse(t, createRequest, createRecorder)
	// channelID 保存创建响应中的渠道主键。
	var created struct {
		ID int64 `json:"id"`
	}
	// decodeErr 表示解析通知渠道创建响应失败。
	if decodeErr := json.Unmarshal(createRecorder.Body.Bytes(), &created); decodeErr != nil || created.ID <= 0 {
		t.Fatalf("渠道创建响应无效: %s", createRecorder.Body.String())
	}
	// testRequest 是调用渠道测试的版本化请求。
	testRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/channels/"+strconv.FormatInt(created.ID, 10)+"/test", nil)
	testRequest.AddCookie(sessionCookie)
	// testRecorder 保存渠道测试成功响应。
	testRecorder := httptest.NewRecorder()
	handler.ServeHTTP(testRecorder, testRequest)
	assertOpenAPIRecordedSuccessResponse(t, testRequest, testRecorder)
}

// testOpenAPIAccountTaskRunSuccess 注入账号任务端口并验证手动运行响应。
func testOpenAPIAccountTaskRunSuccess(t *testing.T) {
	// srv、_、cleanup 分别是服务、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.applications.accountTasks = contractAccountTasksPort{}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// request 是手动自动评价运行请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account-tasks/acc1/run", strings.NewReader(`{"task_type":"auto_rate"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(sessionCookie)
	// recorder 保存任务运行响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// contractChatPort 是只返回确定性消息的聊天 HTTP 应用端口。
type contractChatPort struct{}

// SendingAvailable 报告测试端口支持文字发送。
func (contractChatPort) SendingAvailable() bool { return true }

// ImageUploadAvailable 报告测试端口支持图片上传。
func (contractChatPort) ImageUploadAvailable() bool { return true }

// Subscribe 返回空事件流，当前场景不验证 WebSocket。
func (contractChatPort) Subscribe(context.Context, int64) (<-chan chatapp.Event, func(), error) {
	return nil, func() {}, nil
}

// RefreshConversations 返回空联系人页。
func (contractChatPort) RefreshConversations(context.Context, string, int64, int) (chatapp.ConversationPage, error) {
	return chatapp.ConversationPage{}, nil
}

// RefreshHistory 返回不可用错误，使查询回退本地存储。
func (contractChatPort) RefreshHistory(context.Context, string, string, int64, int, chatapp.Session) (chatapp.HistoryPage, error) {
	return chatapp.HistoryPage{}, chatapp.ErrRefreshUnavailable
}

// SendText 返回确定性外发消息。
func (contractChatPort) SendText(context.Context, chatapp.OutgoingInput) (*chatapp.Message, error) {
	return &chatapp.Message{AccountID: "acc1", ChatID: "chat", MessageKey: "contract-message", Direction: "outgoing", MessageType: "text", Content: "hello", Status: "sent"}, nil
}

// SendImage 返回确定性外发图片消息。
func (contractChatPort) SendImage(context.Context, chatapp.ImageInput) (*chatapp.Message, error) {
	return &chatapp.Message{AccountID: "acc1", ChatID: "chat", MessageKey: "contract-image", Direction: "outgoing", MessageType: "image", Content: "https://img.example/i.png", Status: "sent"}, nil
}

// ListStoredMessages 返回空消息页。
func (contractChatPort) ListStoredMessages(context.Context, int64, string, string, int64, int) (chatapp.Page, error) {
	return chatapp.Page{}, nil
}

// ListSessions 返回空会话集合。
func (contractChatPort) ListSessions(context.Context, int64, string, int) ([]chatapp.Session, error) {
	return nil, nil
}

// FindSession 返回空会话。
func (contractChatPort) FindSession(context.Context, int64, string, string) (chatapp.Session, error) {
	return chatapp.Session{}, nil
}

// ResolveReadMessageID 返回有效测试平台消息标识。
func (contractChatPort) ResolveReadMessageID(context.Context, string, string, string) string {
	return "contract.PNM"
}

// CleanupEmptySessions 在测试端口中无副作用。
func (contractChatPort) CleanupEmptySessions(context.Context, string) error { return nil }

// OwnsAccount 始终确认测试账号归属。
func (contractChatPort) OwnsAccount(context.Context, int64, string) (bool, error) { return true, nil }

// MarkRead 确认本地已读更新成功。
func (contractChatPort) MarkRead(context.Context, int64, string, string) error { return nil }

// ReportPlatformRead 确认平台已读上报成功。
func (contractChatPort) ReportPlatformRead(context.Context, string, string, []map[string]any) error {
	return nil
}

// ResolveSessionIdentity 保持测试会话为空。
func (contractChatPort) ResolveSessionIdentity(context.Context, chatapp.Session) (chatapp.Session, error) {
	return chatapp.Session{}, nil
}

// RefreshSessionIdentities 返回空会话集合。
func (contractChatPort) RefreshSessionIdentities(context.Context, string, []chatapp.Session) ([]chatapp.Session, error) {
	return nil, nil
}

// contractItemPublishPort 是只返回确定性商品结果的单商品发布端口。
type contractItemPublishPort struct{}

// PublishSingle 返回确定性远端发布结果。
func (contractItemPublishPort) PublishSingle(context.Context, itemapp.PublishInput) (itemapp.PublishOutcome, error) {
	return itemapp.PublishOutcome{Result: &itemapp.PublishResult{ItemID: "contract-published", ItemURL: "https://item.example/contract", Title: "契约发布", PriceText: "10.00", Quantity: 1}}, nil
}

// contractAccountTasksPort 是只返回确定性统计结果的账号任务端口。
type contractAccountTasksPort struct{}

// contractNotificationChannelsPort 是只覆盖渠道测试动作的通知渠道端口。
type contractNotificationChannelsPort struct{ NotificationChannelsPort }

// CreateChannel 返回固定渠道标识，使后续测试请求仍通过真实 HTTP handler。
func (contractNotificationChannelsPort) CreateChannel(context.Context, int64, notificationsapp.ChannelInput) (int64, error) {
	return 1, nil
}

// TestChannel 将渠道测试视为已由本地替身成功发送。
func (contractNotificationChannelsPort) TestChannel(context.Context, int64, int64, time.Time) error {
	return nil
}

// GetSettings 返回测试账号默认任务设置。
func (contractAccountTasksPort) GetSettings(context.Context, string) (automationapp.AccountTaskSettings, error) {
	return automationapp.AccountTaskSettings{CookieID: "acc1"}, nil
}

// UpdateSettings 返回规范化后的测试设置。
func (contractAccountTasksPort) UpdateSettings(context.Context, automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error) {
	return automationapp.AccountTaskSettings{CookieID: "acc1"}, nil
}

// ListRuns 返回空运行记录。
func (contractAccountTasksPort) ListRuns(context.Context, string, int) ([]automationapp.AccountTaskRun, error) {
	return nil, nil
}

// Run 返回包含一个成功动作的任务摘要。
func (contractAccountTasksPort) Run(context.Context, string, string) (automationapp.TaskSummary, error) {
	return automationapp.TaskSummary{TaskType: automationapp.TaskAutoRate, Success: 1}, nil
}
