package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
)

// chatMetadataHandlerCoveragePort 为聊天元数据 handler 注入可控的成功与错误结果。
type chatMetadataHandlerCoveragePort struct {
	// contractChatPort 提供聊天元数据测试之外的稳定默认能力。
	contractChatPort
	// listReplies 保存快捷回复列表查询要返回的结果。
	listReplies []chatapp.QuickReply
	// listErr 保存快捷回复列表查询要返回的错误。
	listErr error
	// createReply 保存快捷回复创建要返回的结果。
	createReply chatapp.QuickReply
	// createErr 保存快捷回复创建要返回的错误。
	createErr error
	// deleteErr 保存快捷回复删除要返回的错误。
	deleteErr error
	// buyerNote 保存买家备注读取与保存要返回的结果。
	buyerNote chatapp.BuyerNote
	// getNoteErr 保存买家备注读取要返回的错误。
	getNoteErr error
	// saveNoteErr 保存买家备注保存要返回的错误。
	saveNoteErr error
}

// ListQuickReplies 返回测试配置的快捷回复列表或错误。
func (port *chatMetadataHandlerCoveragePort) ListQuickReplies(context.Context, int64, string) ([]chatapp.QuickReply, error) {
	return port.listReplies, port.listErr
}

// CreateQuickReply 返回测试配置的快捷回复或错误。
func (port *chatMetadataHandlerCoveragePort) CreateQuickReply(context.Context, int64, string, string) (chatapp.QuickReply, error) {
	return port.createReply, port.createErr
}

// DeleteQuickReply 返回测试配置的快捷回复删除错误。
func (port *chatMetadataHandlerCoveragePort) DeleteQuickReply(context.Context, int64, string, int64) error {
	return port.deleteErr
}

// GetBuyerNote 返回测试配置的买家备注或错误。
func (port *chatMetadataHandlerCoveragePort) GetBuyerNote(context.Context, int64, string, string) (chatapp.BuyerNote, error) {
	return port.buyerNote, port.getNoteErr
}

// SaveBuyerNote 返回测试配置的买家备注或错误。
func (port *chatMetadataHandlerCoveragePort) SaveBuyerNote(context.Context, int64, string, string, string) (chatapp.BuyerNote, error) {
	return port.buyerNote, port.saveNoteErr
}

// TestChatMetadataHandlersCoverSuccessValidationAndApplicationErrors 覆盖聊天元数据 handler 的成功、输入校验和错误映射。
func TestChatMetadataHandlersCoverSuccessValidationAndApplicationErrors(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// port 是本次测试注入的可控聊天应用端口。
	port := &chatMetadataHandlerCoveragePort{
		listReplies: []chatapp.QuickReply{{ID: 7, AccountID: "acc1", Content: "你好", CreatedAt: 9}},
		createReply: chatapp.QuickReply{ID: 8, AccountID: "acc1", Content: "已创建", CreatedAt: 10},
		buyerNote:   chatapp.BuyerNote{AccountID: "acc1", BuyerID: "buyer1", Content: "备注", UpdatedAt: 11},
	}
	srv.applications.chat = port
	// handler 是注入聊天应用端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// successCases 保存成功请求及其预期 HTTP 状态。
	successCases := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/api/v1/chat/quick-replies?account_id=acc1", "", http.StatusOK},
		{http.MethodPost, "/api/v1/chat/quick-replies", `{"account_id":"acc1","content":"新回复"}`, http.StatusCreated},
		{http.MethodDelete, "/api/v1/chat/quick-replies/8?account_id=acc1", "", http.StatusOK},
		{http.MethodGet, "/api/v1/chat/buyer-notes/buyer1?account_id=acc1", "", http.StatusOK},
		{http.MethodPut, "/api/v1/chat/buyer-notes/buyer1", `{"account_id":"acc1","content":"新备注"}`, http.StatusOK},
	}
	// successCase 表示当前待执行的成功请求。
	for _, successCase := range successCases {
		// request 是当前元数据成功请求。
		request := httptest.NewRequest(successCase.method, successCase.path, strings.NewReader(successCase.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		// recorder 保存当前请求的 HTTP 响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != successCase.status {
			t.Errorf("%s %s status=%d want=%d body=%s", successCase.method, successCase.path, recorder.Code, successCase.status, recorder.Body.String())
		}
	}

	// validationCases 保存请求体、路径标识和应用错误触发的 HTTP 结果。
	validationCases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"create malformed json", http.MethodPost, "/api/v1/chat/quick-replies", "{", http.StatusBadRequest},
		{"delete malformed id", http.MethodDelete, "/api/v1/chat/quick-replies/nope?account_id=acc1", "", http.StatusBadRequest},
		{"save malformed json", http.MethodPut, "/api/v1/chat/buyer-notes/buyer1", "{", http.StatusBadRequest},
	}
	// validationCase 表示当前待执行的请求校验场景。
	for _, validationCase := range validationCases {
		// request 是当前请求校验场景的 HTTP 请求。
		request := httptest.NewRequest(validationCase.method, validationCase.path, strings.NewReader(validationCase.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		// recorder 保存当前校验请求的响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != validationCase.status {
			t.Errorf("%s status=%d want=%d body=%s", validationCase.name, recorder.Code, validationCase.status, recorder.Body.String())
		}
	}

	// errorCases 保存每类聊天元数据应用错误及其 HTTP 映射。
	errorCases := []struct {
		name   string
		method string
		path   string
		body   string
		setErr func()
		status int
	}{
		{"list invalid", http.MethodGet, "/api/v1/chat/quick-replies?account_id=acc1", "", func() { port.listErr = chatapp.ErrInvalidInput }, http.StatusBadRequest},
		{"list forbidden", http.MethodGet, "/api/v1/chat/quick-replies?account_id=acc1", "", func() { port.listErr = chatapp.ErrMetadataForbidden }, http.StatusForbidden},
		{"list internal", http.MethodGet, "/api/v1/chat/quick-replies?account_id=acc1", "", func() { port.listErr = errors.New("list failed") }, http.StatusInternalServerError},
		{"create limit", http.MethodPost, "/api/v1/chat/quick-replies", `{"account_id":"acc1","content":"x"}`, func() { port.createErr = chatapp.ErrQuickReplyLimitReached }, http.StatusConflict},
		{"create internal", http.MethodPost, "/api/v1/chat/quick-replies", `{"account_id":"acc1","content":"x"}`, func() { port.createErr = errors.New("create failed") }, http.StatusInternalServerError},
		{"delete not found", http.MethodDelete, "/api/v1/chat/quick-replies/8?account_id=acc1", "", func() { port.deleteErr = chatapp.ErrQuickReplyNotFound }, http.StatusNotFound},
		{"delete forbidden", http.MethodDelete, "/api/v1/chat/quick-replies/8?account_id=acc1", "", func() { port.deleteErr = chatapp.ErrMetadataForbidden }, http.StatusForbidden},
		{"get note invalid", http.MethodGet, "/api/v1/chat/buyer-notes/buyer1?account_id=acc1", "", func() { port.getNoteErr = chatapp.ErrInvalidInput }, http.StatusBadRequest},
		{"get note internal", http.MethodGet, "/api/v1/chat/buyer-notes/buyer1?account_id=acc1", "", func() { port.getNoteErr = errors.New("get failed") }, http.StatusInternalServerError},
		{"save note forbidden", http.MethodPut, "/api/v1/chat/buyer-notes/buyer1", `{"account_id":"acc1","content":"x"}`, func() { port.saveNoteErr = chatapp.ErrMetadataForbidden }, http.StatusForbidden},
		{"save note internal", http.MethodPut, "/api/v1/chat/buyer-notes/buyer1", `{"account_id":"acc1","content":"x"}`, func() { port.saveNoteErr = errors.New("save failed") }, http.StatusInternalServerError},
	}
	// errorCase 表示当前待执行的应用错误映射场景。
	for _, errorCase := range errorCases {
		errorCase.setErr()
		// request 是当前应用错误映射场景的 HTTP 请求。
		request := httptest.NewRequest(errorCase.method, errorCase.path, strings.NewReader(errorCase.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		// recorder 保存当前应用错误响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != errorCase.status {
			t.Errorf("%s status=%d want=%d body=%s", errorCase.name, recorder.Code, errorCase.status, recorder.Body.String())
		}
		port.listErr, port.createErr, port.deleteErr = nil, nil, nil
		port.getNoteErr, port.saveNoteErr = nil, nil
	}
}
