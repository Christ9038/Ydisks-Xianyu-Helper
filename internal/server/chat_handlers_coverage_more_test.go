package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	chatapp "xianyu-go/internal/application/chat"
)

// chatHandlerCoveragePort 为聊天主 handler 注入可控的本地结果和错误。
type chatHandlerCoveragePort struct {
	// contractChatPort 提供未被当前场景覆盖方法的稳定默认能力。
	contractChatPort
	// sendingAvailable 表示文字发送能力是否可用。
	sendingAvailable bool
	// imageUploadAvailable 表示图片上传能力是否可用。
	imageUploadAvailable bool
	// ownsAccountResult 与 ownsAccountErr 保存账号归属校验结果。
	ownsAccountResult bool
	ownsAccountErr    error
	// cleanupErr 保存清理空会话要返回的错误。
	cleanupErr error
	// refreshConversationsPage 与 refreshConversationsErr 保存联系人刷新结果。
	refreshConversationsPage chatapp.ConversationPage
	refreshConversationsErr  error
	// refreshHistoryPage 与 refreshHistoryErr 保存聊天历史刷新结果。
	refreshHistoryPage chatapp.HistoryPage
	refreshHistoryErr  error
	// listSessionsResult 与 listSessionsErr 保存会话列表结果。
	listSessionsResult []chatapp.Session
	listSessionsErr    error
	// refreshIdentitiesResult 与 refreshIdentitiesErr 保存会话身份补全结果。
	refreshIdentitiesResult []chatapp.Session
	refreshIdentitiesErr    error
	// findSessionResult 与 findSessionErr 保存当前会话查询结果。
	findSessionResult chatapp.Session
	findSessionErr    error
	// listStoredPage 与 listStoredErr 保存本地聊天消息查询结果。
	listStoredPage chatapp.Page
	listStoredErr  error
	// resolveIdentityResult 与 resolveIdentityErr 保存单个会话身份补全结果。
	resolveIdentityResult chatapp.Session
	resolveIdentityErr    error
	// sendTextResult 与 sendTextErr 保存文字发送结果。
	sendTextResult *chatapp.Message
	sendTextErr    error
	// sendImageResult 与 sendImageErr 保存图片发送结果。
	sendImageResult *chatapp.Message
	sendImageErr    error
	// markReadErr 与 reportReadErr 保存本地已读和平台上报错误。
	markReadErr   error
	reportReadErr error
	// resolvedReadID 保存旧版聊天消息标识转换后的平台标识。
	resolvedReadID string
}

// cookieSettingsCoveragePort 为账号 Cookie 设置 handler 注入可控的设置结果。
type cookieSettingsCoveragePort struct {
	// AccountSettingsPort 提供未被当前场景覆盖方法的默认能力。
	AccountSettingsPort
	// updateSettingsResult 与 updateSettingsErr 保存账号设置更新结果。
	updateSettingsResult accountapp.SettingsResult
	updateSettingsErr    error
	// loginInfoErr 保存登录资料更新错误。
	loginInfoErr error
	// statusResult 与 statusErr 保存账号启停结果。
	statusResult accountapp.StatusResult
	statusErr    error
	// autoConfirmResult 与 autoConfirmErr 保存自动确认设置结果。
	autoConfirmResult accountapp.SettingsResult
	autoConfirmErr    error
	// remarkResult 与 remarkErr 保存账号备注设置结果。
	remarkResult accountapp.SettingsResult
	remarkErr    error
	// pauseResult 与 pauseErr 保存暂停设置结果。
	pauseResult accountapp.SettingsResult
	pauseErr    error
	// pauseState 保存暂停状态查询结果。
	pauseState accountapp.PauseState
}

// cookieLoginCoveragePort 为 Cookie 新增和更新 handler 注入可控的登录持久化结果。
type cookieLoginCoveragePort struct {
	// AccountLoginPort 提供二维码等无关登录能力的默认实现。
	AccountLoginPort
	// createErr 保存账号创建要返回的错误。
	createErr error
	// updateErr 保存账号 Cookie 更新要返回的错误。
	updateErr error
}

// CreateCookie 返回测试配置的账号创建错误。
func (port *cookieLoginCoveragePort) CreateCookie(context.Context, string, string, int64, string) error {
	return port.createErr
}

// UpdateCookie 返回测试配置的账号 Cookie 更新错误。
func (port *cookieLoginCoveragePort) UpdateCookie(context.Context, string, string, int64, string, int64) error {
	return port.updateErr
}

// UpdateSettings 返回测试配置的账号设置更新结果。
func (port *cookieSettingsCoveragePort) UpdateSettings(context.Context, accountapp.SettingsUpdateInput) (accountapp.SettingsResult, error) {
	return port.updateSettingsResult, port.updateSettingsErr
}

// UpdateLoginInfo 返回测试配置的登录资料更新错误。
func (port *cookieSettingsCoveragePort) UpdateLoginInfo(context.Context, accountapp.LoginInfoUpdateInput) error {
	return port.loginInfoErr
}

// SetStatus 返回测试配置的账号启停结果。
func (port *cookieSettingsCoveragePort) SetStatus(context.Context, int64, string, bool) (accountapp.StatusResult, error) {
	return port.statusResult, port.statusErr
}

// SetAutoConfirm 返回测试配置的自动确认设置结果。
func (port *cookieSettingsCoveragePort) SetAutoConfirm(context.Context, int64, string, bool) (accountapp.SettingsResult, error) {
	return port.autoConfirmResult, port.autoConfirmErr
}

// SetRemark 返回测试配置的账号备注设置结果。
func (port *cookieSettingsCoveragePort) SetRemark(context.Context, int64, string, string) (accountapp.SettingsResult, error) {
	return port.remarkResult, port.remarkErr
}

// SetPause 返回测试配置的账号暂停设置结果。
func (port *cookieSettingsCoveragePort) SetPause(context.Context, int64, string, int) (accountapp.SettingsResult, error) {
	return port.pauseResult, port.pauseErr
}

// GetPause 返回测试配置的账号暂停状态。
func (port *cookieSettingsCoveragePort) GetPause(context.Context, int64, string) (accountapp.PauseState, error) {
	return port.pauseState, nil
}

// SendingAvailable 返回测试配置的文字发送能力。
func (port *chatHandlerCoveragePort) SendingAvailable() bool { return port.sendingAvailable }

// ImageUploadAvailable 返回测试配置的图片上传能力。
func (port *chatHandlerCoveragePort) ImageUploadAvailable() bool { return port.imageUploadAvailable }

// OwnsAccount 返回测试配置的账号归属结果。
func (port *chatHandlerCoveragePort) OwnsAccount(context.Context, int64, string) (bool, error) {
	return port.ownsAccountResult, port.ownsAccountErr
}

// CleanupEmptySessions 返回测试配置的空会话清理错误。
func (port *chatHandlerCoveragePort) CleanupEmptySessions(context.Context, string) error {
	return port.cleanupErr
}

// RefreshConversations 返回测试配置的联系人刷新结果。
func (port *chatHandlerCoveragePort) RefreshConversations(context.Context, string, int64, int) (chatapp.ConversationPage, error) {
	return port.refreshConversationsPage, port.refreshConversationsErr
}

// RefreshHistory 返回测试配置的聊天历史刷新结果。
func (port *chatHandlerCoveragePort) RefreshHistory(context.Context, string, string, int64, int, chatapp.Session) (chatapp.HistoryPage, error) {
	return port.refreshHistoryPage, port.refreshHistoryErr
}

// ListSessions 返回测试配置的会话列表结果。
func (port *chatHandlerCoveragePort) ListSessions(context.Context, int64, string, int) ([]chatapp.Session, error) {
	return port.listSessionsResult, port.listSessionsErr
}

// ListSessionPage 返回测试配置的本地会话键集分页结果。
func (port *chatHandlerCoveragePort) ListSessionPage(context.Context, int64, string, *chatapp.SessionCursor, int) (chatapp.SessionPage, error) {
	return chatapp.SessionPage{Sessions: port.listSessionsResult}, port.listSessionsErr
}

// RefreshSessionIdentities 返回测试配置的会话身份补全结果。
func (port *chatHandlerCoveragePort) RefreshSessionIdentities(context.Context, string, []chatapp.Session) ([]chatapp.Session, error) {
	return port.refreshIdentitiesResult, port.refreshIdentitiesErr
}

// FindSession 返回测试配置的当前会话结果。
func (port *chatHandlerCoveragePort) FindSession(context.Context, int64, string, string) (chatapp.Session, error) {
	return port.findSessionResult, port.findSessionErr
}

// ListStoredMessages 返回测试配置的本地消息页。
func (port *chatHandlerCoveragePort) ListStoredMessages(context.Context, int64, string, string, int64, int) (chatapp.Page, error) {
	return port.listStoredPage, port.listStoredErr
}

// ResolveSessionIdentity 返回测试配置的单个会话身份补全结果。
func (port *chatHandlerCoveragePort) ResolveSessionIdentity(context.Context, chatapp.Session) (chatapp.Session, error) {
	return port.resolveIdentityResult, port.resolveIdentityErr
}

// SendText 返回测试配置的文字发送结果。
func (port *chatHandlerCoveragePort) SendText(context.Context, chatapp.OutgoingInput) (*chatapp.Message, error) {
	return port.sendTextResult, port.sendTextErr
}

// SendImage 返回测试配置的图片发送结果。
func (port *chatHandlerCoveragePort) SendImage(context.Context, chatapp.ImageInput) (*chatapp.Message, error) {
	return port.sendImageResult, port.sendImageErr
}

// MarkRead 返回测试配置的本地已读错误。
func (port *chatHandlerCoveragePort) MarkRead(context.Context, int64, string, string) error {
	return port.markReadErr
}

// ReportPlatformRead 返回测试配置的平台已读上报错误。
func (port *chatHandlerCoveragePort) ReportPlatformRead(context.Context, string, string, []map[string]any) error {
	return port.reportReadErr
}

// ResolveReadMessageID 返回测试配置的旧消息标识转换结果。
func (port *chatHandlerCoveragePort) ResolveReadMessageID(context.Context, string, string, string) string {
	return port.resolvedReadID
}

// serveChatCoverageRequest 发送带认证 Cookie 的聊天 HTTP 请求并返回响应记录器。
func serveChatCoverageRequest(handler http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	if strings.HasPrefix(path, "/api/v1/cookies/") {
		// path 兼容账号设置测试使用的旧版状态路由；其他版本化账号设置路由由独立挂载函数提供。
		path = strings.TrimPrefix(path, "/api/v1")
	}
	// request 是当前聊天覆盖场景的 HTTP 请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	// recorder 保存当前聊天请求的 HTTP 响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// TestChatSendTextHandlerCoversAvailabilityValidationAndErrors 覆盖文字发送 handler 的状态与错误分支。
func TestChatSendTextHandlerCoversAvailabilityValidationAndErrors(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// port 是当前测试注入的文字发送应用端口。
	port := &chatHandlerCoveragePort{sendingAvailable: true, imageUploadAvailable: true, ownsAccountResult: true,
		sendTextResult: &chatapp.Message{AccountID: "acc1", ChatID: "chat1", SenderID: "buyer1", Content: "已发送", Status: "sent"}}
	srv.applications.chat = port
	// handler 是注入可控聊天端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)
	// successRecorder 保存文字发送成功响应。
	successRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/messages", `{"account_id":"acc1","chat_id":"chat1","buyer_id":"buyer1","text":"你好"}`)
	if successRecorder.Code != http.StatusCreated {
		t.Fatalf("success status=%d body=%s", successRecorder.Code, successRecorder.Body.String())
	}
	// errorCases 保存文字发送应用错误及其 HTTP 状态。
	errorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"unavailable", chatapp.ErrUnavailable, http.StatusServiceUnavailable},
		{"offline", chatapp.ErrOffline, http.StatusConflict},
		{"send", chatapp.ErrSend, http.StatusBadGateway},
		{"status save", chatapp.ErrStatusSave, http.StatusInternalServerError},
		{"other", errors.New("send failed"), http.StatusInternalServerError},
	}
	// errorCase 表示当前文字发送应用错误场景。
	for _, errorCase := range errorCases {
		port.sendTextErr = errorCase.err
		// recorder 保存当前文字发送错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/messages", `{"account_id":"acc1","chat_id":"chat1","buyer_id":"buyer1","text":"你好"}`)
		if recorder.Code != errorCase.status {
			t.Errorf("%s status=%d want=%d body=%s", errorCase.name, recorder.Code, errorCase.status, recorder.Body.String())
		}
	}
	port.sendTextErr = nil

	// validationCases 保存文字发送能力和请求校验场景。
	validationCases := []struct {
		name   string
		body   string
		status int
	}{
		{"malformed json", "{", http.StatusBadRequest},
		{"missing fields", `{"account_id":"acc1","chat_id":"","buyer_id":"buyer1","text":"x"}`, http.StatusBadRequest},
		{"too long", `{"account_id":"acc1","chat_id":"chat1","buyer_id":"buyer1","text":"` + strings.Repeat("中", 2001) + `"}`, http.StatusBadRequest},
	}
	// validationCase 表示当前文字发送输入校验场景。
	for _, validationCase := range validationCases {
		// recorder 保存当前校验场景响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/messages", validationCase.body)
		if recorder.Code != validationCase.status {
			t.Errorf("%s status=%d want=%d body=%s", validationCase.name, recorder.Code, validationCase.status, recorder.Body.String())
		}
	}
	port.ownsAccountResult = false
	// forbiddenRecorder 保存账号归属失败响应。
	forbiddenRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/messages", `{"account_id":"acc1","chat_id":"chat1","buyer_id":"buyer1","text":"你好"}`)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("forbidden status=%d", forbiddenRecorder.Code)
	}
	port.sendingAvailable = false
	// unavailableRecorder 保存聊天发送能力关闭响应。
	unavailableRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/messages", `{}`)
	if unavailableRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", unavailableRecorder.Code)
	}
}

// newChatImageCoverageRequest 构造包含图片字段的 multipart 请求。
func newChatImageCoverageRequest(t *testing.T, cookie *http.Cookie, fileContentType, data string, includeFields bool) *http.Request {
	t.Helper()
	// body 保存 multipart 请求体。
	var body bytes.Buffer
	// writer 负责写入 multipart 字段和图片文件。
	writer := multipart.NewWriter(&body)
	if includeFields {
		// fields 保存图片发送所需的聊天标识字段。
		fields := map[string]string{"account_id": "acc1", "chat_id": "chat1", "buyer_id": "buyer1"}
		// fieldName、fieldValue 表示当前待写入的表单字段。
		for fieldName, fieldValue := range fields {
			// fieldErr 表示当前表单字段写入失败原因。
			if fieldErr := writer.WriteField(fieldName, fieldValue); fieldErr != nil {
				t.Fatalf("写入字段失败: %v", fieldErr)
			}
		}
	}
	// fileWriter 负责写入图片文件内容。
	var fileWriter io.Writer
	// fileErr 保存文件字段创建错误。
	var fileErr error
	if fileContentType == "" {
		// fileHeader 描述默认图片文件字段的 MIME 类型。
		fileHeader := make(textproto.MIMEHeader)
		fileHeader.Set("Content-Disposition", `form-data; name="image"; filename="test.png"`)
		fileHeader.Set("Content-Type", "image/png")
		fileWriter, fileErr = writer.CreatePart(fileHeader)
	} else {
		// fileHeader 描述需要测试的非图片 MIME 类型文件字段。
		fileHeader := make(textproto.MIMEHeader)
		fileHeader.Set("Content-Disposition", `form-data; name="image"; filename="test.bin"`)
		fileHeader.Set("Content-Type", fileContentType)
		fileWriter, fileErr = writer.CreatePart(fileHeader)
	}
	if fileErr != nil {
		t.Fatalf("创建图片字段失败: %v", fileErr)
	}
	if data != "" {
		// writeErr 表示图片内容写入失败原因。
		if _, writeErr := fileWriter.Write([]byte(data)); writeErr != nil {
			t.Fatalf("写入图片数据失败: %v", writeErr)
		}
	}
	// closeErr 表示 multipart 请求体收尾失败原因。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("关闭 multipart 请求失败: %v", closeErr)
	}
	// request 是当前图片上传测试请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat/images", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	return request
}

// TestChatSendImageHandlerCoversValidationAndErrors 覆盖图片发送 handler 的上传校验和应用错误分支。
func TestChatSendImageHandlerCoversValidationAndErrors(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// port 是当前测试注入的图片发送应用端口。
	port := &chatHandlerCoveragePort{sendingAvailable: true, imageUploadAvailable: true, ownsAccountResult: true,
		sendImageResult: &chatapp.Message{AccountID: "acc1", ChatID: "chat1", SenderID: "buyer1", Content: "image", Status: "sent"}}
	srv.applications.chat = port
	// handler 是注入可控聊天端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)
	// successRequest 是合法图片上传请求。
	successRequest := newChatImageCoverageRequest(t, cookie, "", "image-bytes", true)
	// successRecorder 保存合法图片上传响应。
	successRecorder := httptest.NewRecorder()
	handler.ServeHTTP(successRecorder, successRequest)
	if successRecorder.Code != http.StatusCreated {
		t.Fatalf("success status=%d body=%s", successRecorder.Code, successRecorder.Body.String())
	}
	// errorCases 保存图片应用错误及其 HTTP 状态。
	errorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"unavailable", chatapp.ErrUnavailable, http.StatusServiceUnavailable},
		{"offline", chatapp.ErrOffline, http.StatusConflict},
		{"send", chatapp.ErrSend, http.StatusBadGateway},
		{"status save", chatapp.ErrStatusSave, http.StatusInternalServerError},
		{"other", errors.New("image failed"), http.StatusInternalServerError},
	}
	// errorCase 表示当前图片发送应用错误场景。
	for _, errorCase := range errorCases {
		port.sendImageErr = errorCase.err
		// request 是当前图片发送错误请求。
		request := newChatImageCoverageRequest(t, cookie, "", "image-bytes", true)
		// recorder 保存当前图片发送错误响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != errorCase.status {
			t.Errorf("%s status=%d want=%d body=%s", errorCase.name, recorder.Code, errorCase.status, recorder.Body.String())
		}
	}
	port.sendImageErr = nil

	// malformedRequest 是缺少 multipart Content-Type 的请求。
	malformedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/chat/images", strings.NewReader("bad"))
	malformedRequest.AddCookie(cookie)
	// malformedRecorder 保存 multipart 格式错误响应。
	malformedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(malformedRecorder, malformedRequest)
	if malformedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d", malformedRecorder.Code)
	}
	// missingBody 保存不含图片字段的 multipart 请求体。
	var missingBody bytes.Buffer
	// missingWriter 负责构造不含图片字段的 multipart 请求。
	missingWriter := multipart.NewWriter(&missingBody)
	// fieldErr 表示缺失图片场景的账号字段写入失败原因。
	if fieldErr := missingWriter.WriteField("account_id", "acc1"); fieldErr != nil {
		t.Fatal(fieldErr)
	}
	// closeErr 表示缺失图片场景的 multipart 收尾失败原因。
	if closeErr := missingWriter.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// missingRequest 是真正不含图片字段的上传请求。
	missingRequest := httptest.NewRequest(http.MethodPost, "/api/v1/chat/images", &missingBody)
	missingRequest.Header.Set("Content-Type", missingWriter.FormDataContentType())
	missingRequest.AddCookie(cookie)
	// missingRecorder 保存图片字段缺失响应。
	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusBadRequest {
		t.Fatalf("missing image status=%d", missingRecorder.Code)
	}

	// wrongTypeRequest 是文件 Content-Type 非图片的上传请求。
	wrongTypeRequest := newChatImageCoverageRequest(t, cookie, "application/octet-stream", "bytes", true)
	// wrongTypeRecorder 保存文件类型错误响应。
	wrongTypeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(wrongTypeRecorder, wrongTypeRequest)
	if wrongTypeRecorder.Code != http.StatusBadRequest {
		t.Fatalf("wrong content type status=%d", wrongTypeRecorder.Code)
	}
	port.ownsAccountResult = false
	// forbiddenRequest 是账号归属失败的合法图片请求。
	forbiddenRequest := newChatImageCoverageRequest(t, cookie, "", "bytes", true)
	// forbiddenRecorder 保存账号归属失败响应。
	forbiddenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRecorder, forbiddenRequest)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("forbidden status=%d", forbiddenRecorder.Code)
	}
	port.imageUploadAvailable = false
	// unavailableRequest 是图片服务关闭时的请求。
	unavailableRequest := httptest.NewRequest(http.MethodPost, "/api/v1/chat/images", nil)
	unavailableRequest.AddCookie(cookie)
	// unavailableRecorder 保存图片服务关闭响应。
	unavailableRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unavailableRecorder, unavailableRequest)
	if unavailableRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", unavailableRecorder.Code)
	}
}

// TestChatSessionAndMessageHandlersCoverRefreshFallbacks 覆盖会话和消息查询的刷新、回退及错误路径。
func TestChatSessionAndMessageHandlersCoverRefreshFallbacks(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// port 是当前测试注入的会话与消息应用端口。
	port := &chatHandlerCoveragePort{
		ownsAccountResult:        true,
		listSessionsResult:       []chatapp.Session{{AccountID: "acc1", ChatID: "chat1", BuyerID: "buyer1", BuyerName: "买家"}},
		refreshIdentitiesResult:  []chatapp.Session{{AccountID: "acc1", ChatID: "chat1", BuyerID: "buyer1", BuyerName: "补全买家"}},
		refreshConversationsPage: chatapp.ConversationPage{HasMore: true, NextCursor: 9},
		refreshHistoryPage:       chatapp.HistoryPage{Session: chatapp.Session{AccountID: "acc1", ChatID: "chat1", BuyerID: "buyer1"}, HasMore: true, NextCursor: 10},
		listStoredPage:           chatapp.Page{Session: chatapp.Session{AccountID: "acc1", ChatID: "chat1", BuyerID: "buyer1"}},
		resolveIdentityResult:    chatapp.Session{AccountID: "acc1", ChatID: "chat1", BuyerID: "buyer1", BuyerName: "本地补全"},
	}
	srv.applications.chat = port
	// handler 是注入可控聊天端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// refreshSessionsRecorder 保存联系人刷新成功响应。
	refreshSessionsRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/sessions?account_id=acc1&refresh=1&cursor=3&limit=20", "")
	if refreshSessionsRecorder.Code != http.StatusOK {
		t.Fatalf("refresh sessions status=%d body=%s", refreshSessionsRecorder.Code, refreshSessionsRecorder.Body.String())
	}
	port.refreshConversationsErr = chatapp.ErrRefreshUnavailable
	// unavailableRefreshRecorder 保存平台刷新不可用时的本地会话响应。
	unavailableRefreshRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/sessions?account_id=acc1&refresh=1", "")
	if unavailableRefreshRecorder.Code != http.StatusOK {
		t.Fatalf("unavailable refresh status=%d", unavailableRefreshRecorder.Code)
	}
	port.refreshConversationsErr = chatapp.ErrOffline
	// offlineRefreshRecorder 保存账号离线时的本地会话响应。
	offlineRefreshRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/sessions?account_id=acc1&refresh=1", "")
	if offlineRefreshRecorder.Code != http.StatusOK {
		t.Fatalf("offline refresh status=%d", offlineRefreshRecorder.Code)
	}
	port.refreshConversationsErr = chatapp.ErrRefreshPersist
	// persistRefreshRecorder 保存联系人刷新持久化失败响应。
	persistRefreshRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/sessions?account_id=acc1&refresh=1", "")
	if persistRefreshRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("persist refresh status=%d", persistRefreshRecorder.Code)
	}
	port.refreshConversationsErr = nil
	port.cleanupErr = errors.New("cleanup failed")
	// cleanupRecorder 保存无效会话清理失败响应。
	cleanupRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/sessions?account_id=acc1", "")
	if cleanupRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("cleanup status=%d", cleanupRecorder.Code)
	}
	port.cleanupErr = nil
	port.listSessionsErr = errors.New("list sessions failed")
	// listSessionsRecorder 保存会话列表查询失败响应。
	listSessionsRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/sessions?account_id=acc1", "")
	if listSessionsRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("list sessions status=%d", listSessionsRecorder.Code)
	}
	port.listSessionsErr = nil
	port.ownsAccountResult = false
	// forbiddenSessionsRecorder 保存会话账号归属失败响应。
	forbiddenSessionsRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/sessions?account_id=acc1", "")
	if forbiddenSessionsRecorder.Code != http.StatusForbidden {
		t.Fatalf("forbidden sessions status=%d", forbiddenSessionsRecorder.Code)
	}
	port.ownsAccountResult = true

	// messageSuccessRecorder 保存聊天历史刷新成功响应。
	messageSuccessRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/messages?account_id=acc1&chat_id=chat1&cursor=2&limit=20", "")
	if messageSuccessRecorder.Code != http.StatusOK {
		t.Fatalf("message refresh status=%d body=%s", messageSuccessRecorder.Code, messageSuccessRecorder.Body.String())
	}
	port.resolveIdentityErr = errors.New("identity expired")
	// messageIdentityErrorRecorder 保存历史刷新后身份补全失败但仍返回消息的响应。
	messageIdentityErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/messages?account_id=acc1&chat_id=chat1", "")
	if messageIdentityErrorRecorder.Code != http.StatusOK {
		t.Fatalf("message identity error status=%d", messageIdentityErrorRecorder.Code)
	}
	port.resolveIdentityErr = nil
	port.refreshHistoryErr = chatapp.ErrRefreshPersist
	// messagePersistRecorder 保存聊天历史持久化失败响应。
	messagePersistRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/messages?account_id=acc1&chat_id=chat1", "")
	if messagePersistRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("message persist status=%d", messagePersistRecorder.Code)
	}
	port.refreshHistoryErr = chatapp.ErrRefreshUnavailable
	port.listStoredErr = errors.New("stored messages failed")
	// messageStoredErrorRecorder 保存平台不可用且本地消息读取失败响应。
	messageStoredErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/messages?account_id=acc1&chat_id=chat1", "")
	if messageStoredErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("message stored error status=%d", messageStoredErrorRecorder.Code)
	}
	port.listStoredErr = nil
	// messageFallbackRecorder 保存平台不可用时回退本地消息成功响应。
	messageFallbackRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/messages?account_id=acc1&chat_id=chat1&before_id=4", "")
	if messageFallbackRecorder.Code != http.StatusOK {
		t.Fatalf("message fallback status=%d body=%s", messageFallbackRecorder.Code, messageFallbackRecorder.Body.String())
	}
	port.refreshHistoryErr = chatapp.ErrOffline
	// messageOfflineRecorder 保存账号离线时回退本地消息成功响应。
	messageOfflineRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/messages?account_id=acc1&chat_id=chat1", "")
	if messageOfflineRecorder.Code != http.StatusOK {
		t.Fatalf("message offline status=%d", messageOfflineRecorder.Code)
	}
	// missingChatRecorder 保存缺少会话标识的请求校验响应。
	missingChatRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/chat/messages?account_id=acc1", "")
	if missingChatRecorder.Code != http.StatusBadRequest {
		t.Fatalf("missing chat status=%d", missingChatRecorder.Code)
	}
}

// TestMarkChatReadHandlerCoversValidationAndErrorBranches 覆盖聊天已读 handler 的本地提交与平台上报分支。
func TestMarkChatReadHandlerCoversValidationAndErrorBranches(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// port 是当前测试注入的已读应用端口。
	port := &chatHandlerCoveragePort{
		ownsAccountResult: true,
		listStoredPage: chatapp.Page{Messages: []chatapp.Message{
			{MessageKey: "incoming-1", Direction: "incoming", MessageType: "text"},
			{MessageKey: "system-1", Direction: "incoming", MessageType: "system"},
			{MessageKey: "outgoing-1", Direction: "outgoing", MessageType: "text"},
		}},
		resolvedReadID: "incoming-1.PNM",
	}
	srv.applications.chat = port
	// handler 是注入可控聊天端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// successRecorder 保存未显式提供 message_ids 时自动补全并提交成功的响应。
	successRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/read", `{"account_id":"acc1","chat_id":"chat1"}`)
	if successRecorder.Code != http.StatusOK {
		t.Fatalf("success status=%d body=%s", successRecorder.Code, successRecorder.Body.String())
	}
	port.reportReadErr = errors.New("platform report failed")
	// reportErrorRecorder 保存平台上报失败但本地已读成功的响应。
	reportErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/read", `{"account_id":"acc1","chat_id":"chat1","message_ids":[{"messageId":"explicit.PNM"}]}`)
	if reportErrorRecorder.Code != http.StatusOK {
		t.Fatalf("report error status=%d", reportErrorRecorder.Code)
	}
	port.reportReadErr = nil
	port.markReadErr = errors.New("mark failed")
	// markErrorRecorder 保存本地已读提交失败响应。
	markErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/read", `{"account_id":"acc1","chat_id":"chat1","message_ids":[{"messageId":"explicit.PNM"}]}`)
	if markErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("mark error status=%d", markErrorRecorder.Code)
	}
	port.markReadErr = nil
	port.ownsAccountResult = false
	// forbiddenRecorder 保存已读账号归属失败响应。
	forbiddenRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/read", `{"account_id":"acc1","chat_id":"chat1"}`)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("forbidden status=%d", forbiddenRecorder.Code)
	}
	port.ownsAccountResult = true

	// validationCases 保存已读请求的格式和必要字段校验场景。
	validationCases := []struct {
		name string
		body string
	}{
		{"malformed", "{"},
		{"missing chat", `{"account_id":"acc1"}`},
	}
	// validationCase 表示当前已读请求校验场景。
	for _, validationCase := range validationCases {
		// recorder 保存当前校验场景响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/read", validationCase.body)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d", validationCase.name, recorder.Code)
		}
	}
}

// TestCookieSettingsHandlersCoverStatusUpdateAndValidation 覆盖账号设置、启停、备注和暂停 handler 的分支。
func TestCookieSettingsHandlersCoverStatusUpdateAndValidation(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是当前测试注入的账号设置应用端口。
	port := &cookieSettingsCoveragePort{
		updateSettingsResult: accountapp.SettingsResult{PausedUntil: 1},
		pauseResult:          accountapp.SettingsResult{PausedUntil: 2},
		pauseState:           accountapp.PauseState{Duration: 30, PausedUntil: 3, Paused: true},
	}
	srv.applications.accountSettings = port
	// handler 是注入可控账号设置端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// successCases 保存账号设置类成功请求。
	successCases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPut, "/api/v1/cookies/acc1/status", `{"enabled":true}`},
		{http.MethodPut, "/api/v1/cookies/acc1/auto-confirm", `{"auto_confirm":true}`},
		{http.MethodPut, "/api/v1/cookies/acc1/remark", `{"remark":"备注"}`},
		{http.MethodPut, "/api/v1/cookies/acc1/pause-duration", `{"pause_duration":30}`},
		{http.MethodGet, "/api/v1/cookies/acc1/pause-duration", ""},
		{http.MethodPut, "/api/v1/cookies/acc1/settings", `{"remark":"新备注","clear_password":true}`},
		{http.MethodPut, "/api/v1/cookies/acc1/login-info", `{"username":"user","login_password":"pass","show_browser":true}`},
	}
	// successCase 表示当前账号设置成功请求。
	for _, successCase := range successCases {
		// recorder 保存当前账号设置响应。
		recorder := serveChatCoverageRequest(handler, cookie, successCase.method, successCase.path, successCase.body)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s %s status=%d body=%s", successCase.method, successCase.path, recorder.Code, recorder.Body.String())
		}
	}

	// statusErrorCases 保存账号启停应用错误及其状态码。
	statusErrorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", accountapp.ErrForbidden, http.StatusForbidden},
		{"not found", accountapp.ErrNotFound, http.StatusNotFound},
		{"internal", errors.New("status failed"), http.StatusInternalServerError},
	}
	// statusErrorCase 表示当前账号启停应用错误场景。
	for _, statusErrorCase := range statusErrorCases {
		port.statusErr = statusErrorCase.err
		// recorder 保存当前启停应用错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/status", `{"enabled":true}`)
		if recorder.Code != statusErrorCase.status {
			t.Errorf("%s status=%d want=%d", statusErrorCase.name, recorder.Code, statusErrorCase.status)
		}
	}
	port.statusErr = nil
	port.statusResult.RuntimeError = accountapp.ErrRuntimeStopConflict
	// stopConflictRecorder 保存运行实例停止冲突响应。
	stopConflictRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/status", `{"enabled":false}`)
	if stopConflictRecorder.Code != http.StatusConflict {
		t.Fatalf("stop conflict status=%d", stopConflictRecorder.Code)
	}
	port.statusResult.RuntimeError = errors.New("runtime start failed")
	// runtimeErrorRecorder 保存运行实例启动失败响应。
	runtimeErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/status", `{"enabled":true}`)
	if runtimeErrorRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("runtime error status=%d", runtimeErrorRecorder.Code)
	}
	port.statusResult.RuntimeError = nil

	// updateErrorCases 保存账号设置更新错误及其状态码。
	updateErrorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", accountapp.ErrForbidden, http.StatusForbidden},
		{"not found", accountapp.ErrNotFound, http.StatusNotFound},
		{"other", errors.New("settings failed"), http.StatusBadRequest},
	}
	// updateErrorCase 表示当前账号设置更新错误场景。
	for _, updateErrorCase := range updateErrorCases {
		port.updateSettingsErr = updateErrorCase.err
		// recorder 保存当前账号设置错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/settings", `{"remark":"x"}`)
		if recorder.Code != updateErrorCase.status {
			t.Errorf("%s status=%d want=%d", updateErrorCase.name, recorder.Code, updateErrorCase.status)
		}
	}
	port.updateSettingsErr = nil
	port.updateSettingsResult.RuntimeError = errors.New("restart failed")
	// settingsRuntimeErrorRecorder 保存设置写入成功但运行实例重启失败响应。
	settingsRuntimeErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/settings", `{"remark":"x"}`)
	if settingsRuntimeErrorRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("settings runtime error status=%d", settingsRuntimeErrorRecorder.Code)
	}
	port.updateSettingsResult.RuntimeError = nil

	// validationCases 保存账号设置请求格式和字段长度校验场景。
	validationCases := []struct {
		path string
		body string
	}{
		{"/api/v1/cookies/acc1/status", "{"},
		{"/api/v1/cookies/acc1/login-info", "{"},
		{"/api/v1/cookies/acc1/settings", `{"cookie":""}`},
		{"/api/v1/cookies/acc1/settings", `{"remark":"` + strings.Repeat("中", 501) + `"}`},
		{"/api/v1/cookies/acc1/settings", `{"username":"` + strings.Repeat("u", 257) + `"}`},
		{"/api/v1/cookies/acc1/settings", `{"login_password":"` + strings.Repeat("p", 1025) + `"}`},
		{"/api/v1/cookies/acc1/pause-duration", `{"pause_duration":-1}`},
		{"/api/v1/cookies/acc1/pause-duration", `{"pause_duration":1441}`},
	}
	// validationCase 表示当前账号设置校验场景。
	for _, validationCase := range validationCases {
		// recorder 保存当前校验响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, validationCase.path, validationCase.body)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", validationCase.path, recorder.Code, recorder.Body.String())
		}
	}

	port.autoConfirmErr = accountapp.ErrForbidden
	// autoConfirmErrorRecorder 保存自动确认无权错误响应。
	autoConfirmErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/auto-confirm", `{"auto_confirm":true}`)
	if autoConfirmErrorRecorder.Code != http.StatusForbidden {
		t.Fatalf("auto confirm status=%d", autoConfirmErrorRecorder.Code)
	}
	port.autoConfirmErr = errors.New("auto confirm failed")
	// autoConfirmInternalRecorder 保存自动确认内部错误响应。
	autoConfirmInternalRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/auto-confirm", `{"auto_confirm":true}`)
	if autoConfirmInternalRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("auto confirm internal status=%d", autoConfirmInternalRecorder.Code)
	}
	port.autoConfirmErr = nil
	port.remarkErr = accountapp.ErrNotFound
	// remarkErrorRecorder 保存备注账号不存在响应。
	remarkErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/remark", `{"remark":"x"}`)
	if remarkErrorRecorder.Code != http.StatusForbidden {
		t.Fatalf("remark status=%d", remarkErrorRecorder.Code)
	}
	port.remarkErr = errors.New("remark failed")
	// remarkInternalRecorder 保存备注内部错误响应。
	remarkInternalRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/remark", `{"remark":"x"}`)
	if remarkInternalRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("remark internal status=%d", remarkInternalRecorder.Code)
	}
	port.remarkErr = nil
	port.pauseErr = accountapp.ErrForbidden
	// pauseErrorRecorder 保存暂停设置无权错误响应。
	pauseErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/pause-duration", `{"pause_duration":10}`)
	if pauseErrorRecorder.Code != http.StatusForbidden {
		t.Fatalf("pause status=%d", pauseErrorRecorder.Code)
	}
	port.pauseErr = errors.New("pause failed")
	// pauseInternalRecorder 保存暂停设置内部错误响应。
	pauseInternalRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/cookies/acc1/pause-duration", `{"pause_duration":10}`)
	if pauseInternalRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("pause internal status=%d", pauseInternalRecorder.Code)
	}
}

// TestCookieCreateAndUpdateHandlersCoverValidationAndErrors 覆盖 Cookie 新增和更新的成功、校验及应用错误路径。
func TestCookieCreateAndUpdateHandlersCoverValidationAndErrors(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是当前测试注入的账号登录应用端口。
	port := &cookieLoginCoveragePort{}
	srv.applications.accountLogin = port
	// handler 是注入可控登录端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// createRecorder 保存 Cookie 新增成功响应。
	createRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/cookies", `{"id":"new-account","value":"cookie-value","login_method":"manual"}`)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	// createErrorCases 保存 Cookie 新增应用错误及状态码。
	createErrorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", accountapp.ErrForbidden, http.StatusForbidden},
		{"already exists", accountapp.ErrAlreadyExists, http.StatusConflict},
		{"other", errors.New("create failed"), http.StatusBadRequest},
	}
	// createErrorCase 表示当前 Cookie 新增错误场景。
	for _, createErrorCase := range createErrorCases {
		port.createErr = createErrorCase.err
		// recorder 保存当前 Cookie 新增错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/cookies", `{"id":"new-account","value":"cookie-value"}`)
		if recorder.Code != createErrorCase.status {
			t.Errorf("%s status=%d want=%d", createErrorCase.name, recorder.Code, createErrorCase.status)
		}
	}
	port.createErr = nil

	// createValidationCases 保存 Cookie 新增请求校验场景。
	createValidationCases := []string{"{", `{"id":"","value":"cookie-value"}`, `{"id":"new-account","value":""}`}
	// createValidationBody 表示当前 Cookie 新增非法请求体。
	for _, createValidationBody := range createValidationCases {
		// recorder 保存当前 Cookie 新增校验响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/cookies", createValidationBody)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("create validation status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}

	// updateRecorder 保存 Cookie 更新成功响应。
	updateRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/cookies/acc1", `{"value":"cookie-value-2","login_method":"manual"}`)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	// updateErrorCases 保存 Cookie 更新应用错误及状态码。
	updateErrorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"conflict", accountapp.ErrCredentialConflict, http.StatusConflict},
		{"forbidden", accountapp.ErrForbidden, http.StatusForbidden},
		{"other", errors.New("update failed"), http.StatusBadRequest},
	}
	// updateErrorCase 表示当前 Cookie 更新错误场景。
	for _, updateErrorCase := range updateErrorCases {
		port.updateErr = updateErrorCase.err
		// recorder 保存当前 Cookie 更新错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/cookies/acc1", `{"value":"cookie-value-2"}`)
		if recorder.Code != updateErrorCase.status {
			t.Errorf("%s status=%d want=%d", updateErrorCase.name, recorder.Code, updateErrorCase.status)
		}
	}
	port.updateErr = nil
	// updateValidationRecorder 保存 Cookie 更新 JSON 格式错误响应。
	updateValidationRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/cookies/acc1", "{")
	if updateValidationRecorder.Code != http.StatusBadRequest {
		t.Fatalf("update validation status=%d", updateValidationRecorder.Code)
	}
}
