package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	keywordsapp "xianyu-go/internal/application/keywords"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// keywordHandlerCoveragePort 为关键词 Handler 提供可编程的应用 Port 行为。
type keywordHandlerCoveragePort struct {
	// KeywordsPort 提供未被当前用例覆盖的方法默认实现。
	KeywordsPort
	// listRows 是关键词列表成功场景返回的应用模型。
	listRows []keywordsapp.Keyword
	// listErr 是关键词列表场景返回的错误。
	listErr error
	// itemRows 是商品回复列表成功场景返回的应用模型。
	itemRows []keywordsapp.ItemReply
	// itemListErr 是商品回复列表场景返回的错误。
	itemListErr error
	// itemReply 是指定商品查询成功场景返回的应用模型。
	itemReply keywordsapp.ItemReply
	// itemReplyErr 是指定商品查询场景返回的错误。
	itemReplyErr error
	// operationErr 是写入或删除操作统一返回的错误。
	operationErr error
	// addID 是新增关键词成功场景返回的持久化标识。
	addID int64
}

// List 返回测试预置的关键词列表或错误。
func (port *keywordHandlerCoveragePort) List(context.Context, int64, string) ([]keywordsapp.Keyword, error) {
	return port.listRows, port.listErr
}

// Add 返回测试预置的新增关键词标识或错误。
func (port *keywordHandlerCoveragePort) Add(context.Context, int64, string, keywordsapp.Draft) (int64, error) {
	return port.addID, port.operationErr
}

// Replace 返回测试预置的批量替换错误。
func (port *keywordHandlerCoveragePort) Replace(context.Context, int64, string, []keywordsapp.Draft) error {
	return port.operationErr
}

// Update 返回测试预置的关键词更新错误。
func (port *keywordHandlerCoveragePort) Update(context.Context, int64, string, int64, keywordsapp.Draft) error {
	return port.operationErr
}

// DeleteByID 返回测试预置的按 ID 删除错误。
func (port *keywordHandlerCoveragePort) DeleteByID(context.Context, int64, string, int64) error {
	return port.operationErr
}

// DeleteByIndex 返回测试预置的按索引删除错误。
func (port *keywordHandlerCoveragePort) DeleteByIndex(context.Context, int64, string, int) error {
	return port.operationErr
}

// ListItemReplies 返回测试预置的商品回复列表或错误。
func (port *keywordHandlerCoveragePort) ListItemReplies(context.Context, int64) ([]keywordsapp.ItemReply, error) {
	return port.itemRows, port.itemListErr
}

// GetItemReply 返回测试预置的单条商品回复或错误。
func (port *keywordHandlerCoveragePort) GetItemReply(context.Context, int64, string, string) (keywordsapp.ItemReply, error) {
	return port.itemReply, port.itemReplyErr
}

// SetItemReply 返回测试预置的商品回复写入错误。
func (port *keywordHandlerCoveragePort) SetItemReply(context.Context, int64, string, string, string) error {
	return port.operationErr
}

// DeleteItemReply 返回测试预置的商品回复删除错误。
func (port *keywordHandlerCoveragePort) DeleteItemReply(context.Context, int64, string, string) error {
	return port.operationErr
}

// requestWithKeywordParams 创建带认证会话和任意关键词路由参数的直接 Handler 请求。
func requestWithKeywordParams(method, path, body string, params map[string]string) *http.Request {
	// request 是供关键词 Handler 使用的本地 HTTP 请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	// requestContext 保存认证会话和 chi 路径参数上下文。
	requestContext := auth.WithSession(request.Context(), &db.Session{UserID: 1, Username: "admin", IsAdmin: true})
	// routeContext 保存直接调用 Handler 时需要的全部路径变量。
	routeContext := chi.NewRouteContext()
	// key、value 分别表示待写入路由上下文的参数名和值。
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	requestContext = context.WithValue(requestContext, chi.RouteCtxKey, routeContext)
	return request.WithContext(requestContext)
}

// TestKeywordHandlersCoverCompatibilityAndErrorMappings 覆盖关键词兼容接口的转换、校验和错误映射。
func TestKeywordHandlersCoverCompatibilityAndErrorMappings(t *testing.T) {
	// server、cleanup 保存关键词 Handler 使用的测试服务器及资源清理函数。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 保存当前用例注入的关键词应用 Port。
	port := &keywordHandlerCoveragePort{
		listRows:  []keywordsapp.Keyword{{ID: 7, Keyword: "hello", Reply: "world", ItemID: "item-1", Type: "image", ImageURL: "https://image"}},
		itemRows:  []keywordsapp.ItemReply{{ItemID: "item-1", CookieID: "cid", ReplyContent: "reply"}},
		itemReply: keywordsapp.ItemReply{ItemID: "item-1", CookieID: "cid", ReplyContent: "reply"},
		addID:     9,
	}
	server.applications.keywords = port
	// params 保存兼容关键词路由使用的账号、规则和商品标识。
	params := map[string]string{"cid": "cid", "id": "7", "index": "0", "cookie_id": "cid", "item_id": "item-1"}
	// successCases 保存多个成功响应 Handler，验证应用模型到兼容 DTO 的映射。
	successCases := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "list", hand: server.listKeywords, req: requestWithKeywordParams(http.MethodGet, "/keywords/cid", "", params)},
		{name: "list item", hand: server.listKeywordsWithItemID, req: requestWithKeywordParams(http.MethodGet, "/keywords-with-item/cid", "", params)},
		{name: "list type", hand: server.listKeywordsWithType, req: requestWithKeywordParams(http.MethodGet, "/keywords-with-type/cid", "", params)},
		{name: "add", hand: server.addKeyword, req: requestWithKeywordParams(http.MethodPost, "/keywords/cid", `{"keyword":"hello","reply":"world"}`, params)},
		{name: "add item", hand: server.addKeywordWithItemID, req: requestWithKeywordParams(http.MethodPost, "/keywords-with-item/cid", `{"keyword":"hello","reply":"world","item_id":"item-1","type":"text"}`, params)},
		{name: "add item batch", hand: server.addKeywordWithItemID, req: requestWithKeywordParams(http.MethodPost, "/keywords-with-item/cid", `{"keywords":[{"keyword":"hello","reply":"world","type":"text"}]}`, params)},
		{name: "update", hand: server.updateKeywordByID, req: requestWithKeywordParams(http.MethodPut, "/keywords-with-type/cid/7", `{"keyword":"hello","reply":"changed","type":"text"}`, params)},
		{name: "delete id", hand: server.deleteKeywordByID, req: requestWithKeywordParams(http.MethodDelete, "/keywords-with-type/cid/7", "", params)},
		{name: "delete index", hand: server.deleteKeyword, req: requestWithKeywordParams(http.MethodDelete, "/keywords/cid/0", "", params)},
		{name: "set item", hand: server.setItemReply, req: requestWithKeywordParams(http.MethodPut, "/item-reply/cid/item-1", `{"reply_content":"reply"}`, params)},
		{name: "delete item", hand: server.deleteItemReply, req: requestWithKeywordParams(http.MethodDelete, "/item-reply/cid/item-1", "", params)},
	}
	// successCase 表示当前关键词成功 Handler 场景。
	for _, successCase := range successCases {
		t.Run(successCase.name, func(t *testing.T) {
			// recorder 保存当前 Handler 的 HTTP 响应。
			recorder := httptest.NewRecorder()
			successCase.hand(recorder, successCase.req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	// getRecorder 保存单条商品回复成功响应。
	getRecorder := httptest.NewRecorder()
	server.getItemReply(getRecorder, requestWithKeywordParams(http.MethodGet, "/item-reply/cid/item-1", "", params))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get item status=%d", getRecorder.Code)
	}
	// invalidRequests 保存应在进入应用 Port 前被拒绝的请求。
	// badIDParams 保存故意非法的关键词 ID 路由参数。
	badIDParams := map[string]string{"cid": "cid", "id": "no", "index": "0", "cookie_id": "cid", "item_id": "item-1"}
	// invalidRequests 保存各类 JSON 和路径参数错误请求。
	invalidRequests := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "add malformed", hand: server.addKeyword, req: requestWithKeywordParams(http.MethodPost, "/keywords/cid", "{", params)},
		{name: "add item malformed", hand: server.addKeywordWithItemID, req: requestWithKeywordParams(http.MethodPost, "/keywords-with-item/cid", "{", params)},
		{name: "update id", hand: server.updateKeywordByID, req: requestWithKeywordParams(http.MethodPut, "/keywords-with-type/cid/no", `{}`, badIDParams)},
		{name: "update body", hand: server.updateKeywordByID, req: requestWithKeywordParams(http.MethodPut, "/keywords-with-type/cid/7", "{", params)},
		{name: "delete id", hand: server.deleteKeywordByID, req: requestWithKeywordParams(http.MethodDelete, "/keywords-with-type/cid/no", "", badIDParams)},
	}
	// invalidRequest 表示当前关键词输入校验场景。
	for _, invalidRequest := range invalidRequests {
		t.Run(invalidRequest.name, func(t *testing.T) {
			// recorder 保存输入校验响应。
			recorder := httptest.NewRecorder()
			invalidRequest.hand(recorder, invalidRequest.req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	// port.operationErr 保存应用 Port 错误，用于覆盖各写入接口的统一映射。
	port.operationErr = keywordsapp.ErrNotFound
	// errorCases 保存返回关键词不存在的 Handler 场景。
	errorCases := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "add", hand: server.addKeyword, req: requestWithKeywordParams(http.MethodPost, "/keywords/cid", `{"keyword":"hello","reply":"world"}`, params)},
		{name: "replace", hand: server.addKeywordWithItemID, req: requestWithKeywordParams(http.MethodPost, "/keywords-with-item/cid", `{"keywords":[]}`, params)},
		{name: "update", hand: server.updateKeywordByID, req: requestWithKeywordParams(http.MethodPut, "/keywords-with-type/cid/7", `{}`, params)},
		{name: "delete id", hand: server.deleteKeywordByID, req: requestWithKeywordParams(http.MethodDelete, "/keywords-with-type/cid/7", "", params)},
		{name: "delete index", hand: server.deleteKeyword, req: requestWithKeywordParams(http.MethodDelete, "/keywords/cid/0", "", params)},
		{name: "set item", hand: server.setItemReply, req: requestWithKeywordParams(http.MethodPut, "/item-reply/cid/item-1", `{}`, params)},
		{name: "delete item", hand: server.deleteItemReply, req: requestWithKeywordParams(http.MethodDelete, "/item-reply/cid/item-1", "", params)},
	}
	// errorCase 表示当前应用层错误映射场景。
	for _, errorCase := range errorCases {
		t.Run(errorCase.name+" error", func(t *testing.T) {
			// recorder 保存应用错误响应。
			recorder := httptest.NewRecorder()
			errorCase.hand(recorder, errorCase.req)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	// port.itemReplyErr 保存单条商品回复查询错误，用于覆盖 ErrNotFound 之前的普通错误映射。
	port.itemReplyErr = keywordsapp.ErrNotFound
	// getItemErrorRecorder 保存缺失商品回复的兼容空正文响应。
	getItemErrorRecorder := httptest.NewRecorder()
	server.getItemReply(getItemErrorRecorder, requestWithKeywordParams(http.MethodGet, "/item-reply/cid/item-1", "", params))
	if getItemErrorRecorder.Code != http.StatusOK {
		t.Fatalf("missing item status=%d", getItemErrorRecorder.Code)
	}
	// port.listErr 保存关键词列表错误，用于覆盖列表读取失败分支。
	port.listErr = errors.New("list failed")
	// listErrorRecorder 保存关键词列表错误响应。
	listErrorRecorder := httptest.NewRecorder()
	server.listKeywords(listErrorRecorder, requestWithKeywordParams(http.MethodGet, "/keywords/cid", "", params))
	if listErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("list error status=%d", listErrorRecorder.Code)
	}
	// listErrorHandlers 保存带商品范围和类型列表的错误 Handler。
	listErrorHandlers := []http.HandlerFunc{server.listKeywordsWithItemID, server.listKeywordsWithType}
	// listErrorHandler 表示当前待验证的关键词列表错误 Handler。
	for _, listErrorHandler := range listErrorHandlers {
		// recorder 保存带扩展字段列表的错误响应。
		recorder := httptest.NewRecorder()
		listErrorHandler(recorder, requestWithKeywordParams(http.MethodGet, "/keywords/cid", "", params))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("extended list error status=%d", recorder.Code)
		}
	}
	// port.itemListErr 保存商品回复列表错误。
	port.itemListErr = errors.New("item list failed")
	// itemListErrorRecorder 保存商品回复列表兼容空列表响应。
	itemListErrorRecorder := httptest.NewRecorder()
	server.listItemReplies(itemListErrorRecorder, requestWithKeywordParams(http.MethodGet, "/itemReplays", "", params))
	if itemListErrorRecorder.Code != http.StatusOK {
		t.Fatalf("item list error status=%d", itemListErrorRecorder.Code)
	}
	// port.itemReplyErr 保存普通单条商品回复查询错误。
	port.itemReplyErr = errors.New("item query failed")
	// itemReplyErrorRecorder 保存普通单条商品回复查询错误响应。
	itemReplyErrorRecorder := httptest.NewRecorder()
	server.getItemReply(itemReplyErrorRecorder, requestWithKeywordParams(http.MethodGet, "/item-reply/cid/item-1", "", params))
	if itemReplyErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("item query error status=%d", itemReplyErrorRecorder.Code)
	}
	// port.operationErr 保存兼容索引删除失败时的应用错误。
	port.operationErr = keywordsapp.ErrNotFound
	// invalidIndexRecorder 保存无法解析零基索引时的兼容错误响应。
	invalidIndexRecorder := httptest.NewRecorder()
	// invalidIndexParams 保存故意无法解析的零基索引路由参数。
	invalidIndexParams := map[string]string{"cid": "cid", "index": "no", "cookie_id": "cid", "item_id": "item-1"}
	server.deleteKeyword(invalidIndexRecorder, requestWithKeywordParams(http.MethodDelete, "/keywords/cid/no", "", invalidIndexParams))
	if invalidIndexRecorder.Code != http.StatusNotFound {
		t.Fatalf("invalid index status=%d", invalidIndexRecorder.Code)
	}
}

// TestKeywordHelpersCoverUnauthorizedAndErrorMapping 覆盖关键词无认证和各类错误映射辅助分支。
func TestKeywordHelpersCoverUnauthorizedAndErrorMapping(t *testing.T) {
	// recorder 保存无认证用户辅助函数的响应。
	recorder := httptest.NewRecorder()
	// request 是不包含认证会话的本地请求。
	request := httptest.NewRequest(http.MethodGet, "/keywords/cid", nil)
	// ok 表示关键词辅助函数是否读取到认证用户。
	if _, ok := keywordUserID(recorder, request); ok || recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized user id ok=%v status=%d", ok, recorder.Code)
	}
	// mappingCases 保存关键词应用错误到 HTTP 状态的映射。
	mappingCases := []struct {
		name string
		err  error
		code int
	}{
		{name: "nil", err: nil, code: http.StatusOK},
		{name: "validation", err: &keywordsapp.ValidationError{Message: "bad input"}, code: http.StatusBadRequest},
		{name: "forbidden", err: keywordsapp.ErrForbidden, code: http.StatusForbidden},
		{name: "not found", err: keywordsapp.ErrNotFound, code: http.StatusNotFound},
		{name: "invalid", err: keywordsapp.ErrInvalidInput, code: http.StatusBadRequest},
		{name: "generic", err: errors.New("generic"), code: http.StatusInternalServerError},
	}
	// mappingCase 表示当前关键词错误映射场景。
	for _, mappingCase := range mappingCases {
		t.Run(mappingCase.name, func(t *testing.T) {
			// mappingRecorder 保存当前错误映射响应。
			mappingRecorder := httptest.NewRecorder()
			writeKeywordError(mappingRecorder, mappingCase.err, "fallback")
			if mappingRecorder.Code != mappingCase.code {
				t.Fatalf("status=%d want=%d", mappingRecorder.Code, mappingCase.code)
			}
		})
	}
}
