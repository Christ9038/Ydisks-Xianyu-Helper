package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	defaultreplyapp "xianyu-go/internal/application/defaultreply"
)

// defaultReplyHandlerCoveragePort 为默认回复 Handler 提供可编程的应用 Port 结果。
type defaultReplyHandlerCoveragePort struct {
	// DefaultRepliesPort 提供当前场景未覆盖方法的默认能力。
	DefaultRepliesPort
	// reply、replyErr 保存单账号默认回复查询结果和错误。
	reply    defaultreplyapp.Reply
	replyErr error
	// rows、listErr 保存默认回复列表结果和错误。
	rows    []defaultreplyapp.Summary
	listErr error
	// operationErr 保存默认回复写入、删除和清理操作的错误。
	operationErr error
}

// Get 返回测试预置的默认回复或错误。
func (port *defaultReplyHandlerCoveragePort) Get(context.Context, int64, string) (defaultreplyapp.Reply, error) {
	return port.reply, port.replyErr
}

// Upsert 返回测试预置的默认回复写入错误。
func (port *defaultReplyHandlerCoveragePort) Upsert(context.Context, int64, string, defaultreplyapp.Reply) error {
	return port.operationErr
}

// List 返回测试预置的默认回复列表或错误。
func (port *defaultReplyHandlerCoveragePort) List(context.Context, int64) ([]defaultreplyapp.Summary, error) {
	return port.rows, port.listErr
}

// Delete 返回测试预置的默认回复删除错误。
func (port *defaultReplyHandlerCoveragePort) Delete(context.Context, int64, string) error {
	return port.operationErr
}

// ClearRecords 返回测试预置的默认回复记录清理错误。
func (port *defaultReplyHandlerCoveragePort) ClearRecords(context.Context, int64, string) error {
	return port.operationErr
}

// TestDefaultReplyHandlersCoverSuccessAndErrorMappings 覆盖默认回复查询、写入、删除和清理 Handler 的兼容语义。
func TestDefaultReplyHandlersCoverSuccessAndErrorMappings(t *testing.T) {
	// server、cleanup 保存默认回复 Handler 使用的测试服务器及资源清理函数。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 保存当前注入的默认回复应用 Port。
	port := &defaultReplyHandlerCoveragePort{
		reply: defaultreplyapp.Reply{Enabled: true, ReplyContent: "hello", ReplyImageURL: "https://image", ReplyOnce: true},
		rows:  []defaultreplyapp.Summary{{CookieID: "cid", Reply: defaultreplyapp.Reply{Enabled: true, ReplyContent: "hello", ReplyOnce: true}}},
	}
	server.applications.defaultReplies = port
	// params 保存默认回复 Handler 使用的账号路径参数。
	params := map[string]string{"cid": "cid"}
	// successCases 保存默认回复成功场景。
	successCases := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "get", hand: server.getDefaultReply, req: requestWithServerSession(http.MethodGet, "/default-replies/cid", params)},
		{name: "set", hand: server.setDefaultReply, req: requestWithKeywordParams(http.MethodPut, "/default-replies/cid", `{"enabled":true,"reply_content":"hello"}`, params)},
		{name: "list", hand: server.listDefaultReplies, req: requestWithServerSession(http.MethodGet, "/default-replies", nil)},
		{name: "map", hand: server.listDefaultRepliesMap, req: requestWithServerSession(http.MethodGet, "/api/default-replies", nil)},
		{name: "delete", hand: server.deleteDefaultReply, req: requestWithServerSession(http.MethodDelete, "/default-replies/cid", params)},
		{name: "clear", hand: server.clearDefaultReplyRecords, req: requestWithServerSession(http.MethodPost, "/api/default-reply/cid/clear-records", params)},
	}
	// successCase 表示当前默认回复成功 Handler 场景。
	for _, successCase := range successCases {
		t.Run(successCase.name, func(t *testing.T) {
			// recorder 保存当前 Handler 的响应。
			recorder := httptest.NewRecorder()
			successCase.hand(recorder, successCase.req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	// port.replyErr 保存未配置默认回复的应用错误。
	port.replyErr = defaultreplyapp.ErrConfigNotFound
	// defaultRecorder 保存未配置默认回复的兼容默认响应。
	defaultRecorder := httptest.NewRecorder()
	server.getDefaultReply(defaultRecorder, requestWithServerSession(http.MethodGet, "/default-replies/cid", params))
	if defaultRecorder.Code != http.StatusOK {
		t.Fatalf("default reply status=%d", defaultRecorder.Code)
	}
	// replyErrors 保存默认回复查询的 HTTP 错误映射。
	replyErrors := []struct {
		name string
		err  error
		code int
	}{
		{name: "account", err: defaultreplyapp.ErrAccountNotFound, code: http.StatusNotFound},
		{name: "forbidden", err: defaultreplyapp.ErrForbidden, code: http.StatusForbidden},
		{name: "generic", err: errors.New("reply read failed"), code: http.StatusInternalServerError},
	}
	// replyError 表示当前默认回复查询错误场景。
	for _, replyError := range replyErrors {
		port.replyErr = replyError.err
		t.Run("get "+replyError.name, func(t *testing.T) {
			// recorder 保存默认回复查询错误响应。
			recorder := httptest.NewRecorder()
			server.getDefaultReply(recorder, requestWithServerSession(http.MethodGet, "/default-replies/cid", params))
			if recorder.Code != replyError.code {
				t.Fatalf("status=%d want=%d", recorder.Code, replyError.code)
			}
		})
	}
	// port.replyErr 清除查询错误，准备验证写入错误映射。
	port.replyErr = nil
	// port.operationErr 保存默认回复写入错误。
	port.operationErr = defaultreplyapp.ErrForbidden
	// forbiddenRecorder 保存默认回复写入无权限响应。
	for _, operation := range []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "set", hand: server.setDefaultReply, req: requestWithKeywordParams(http.MethodPut, "/default-replies/cid", `{}`, params)},
		{name: "delete", hand: server.deleteDefaultReply, req: requestWithServerSession(http.MethodDelete, "/default-replies/cid", params)},
		{name: "clear", hand: server.clearDefaultReplyRecords, req: requestWithServerSession(http.MethodPost, "/default-reply/cid/clear-records", params)},
	} {
		// recorder 保存默认回复操作错误响应。
		recorder := httptest.NewRecorder()
		operation.hand(recorder, operation.req)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d", operation.name, recorder.Code)
		}
	}
	// port.operationErr 保存普通默认回复操作错误。
	port.operationErr = errors.New("reply write failed")
	// genericRecorder 保存普通默认回复操作错误响应。
	genericRecorder := httptest.NewRecorder()
	server.setDefaultReply(genericRecorder, requestWithKeywordParams(http.MethodPut, "/default-replies/cid", `{}`, params))
	if genericRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("generic operation status=%d", genericRecorder.Code)
	}
	// port.listErr 保存默认回复列表查询错误。
	port.listErr = errors.New("reply list failed")
	// listErrorHandlers 保存两个默认回复列表 Handler。
	listErrorHandlers := []http.HandlerFunc{server.listDefaultReplies, server.listDefaultRepliesMap}
	// listErrorHandler 表示当前默认回复列表错误 Handler。
	for _, listErrorHandler := range listErrorHandlers {
		// recorder 保存默认回复列表错误响应。
		recorder := httptest.NewRecorder()
		listErrorHandler(recorder, requestWithServerSession(http.MethodGet, "/default-replies", nil))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("list error status=%d", recorder.Code)
		}
	}
}

// TestDefaultReplyUserIDCoversUnauthorizedRequest 覆盖默认回复 Handler 的缺失认证会话分支。
func TestDefaultReplyUserIDCoversUnauthorizedRequest(t *testing.T) {
	// recorder 保存缺失认证会话时的 HTTP 响应。
	recorder := httptest.NewRecorder()
	// request 是没有认证上下文的本地请求。
	request := httptest.NewRequest(http.MethodGet, "/default-replies/cid", nil)
	// ok 表示默认回复辅助函数是否读取到认证用户。
	if _, ok := defaultReplyUserID(recorder, request); ok || recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized default reply user id ok=%v status=%d", ok, recorder.Code)
	}
}
