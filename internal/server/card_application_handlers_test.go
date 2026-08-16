package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/db"
)

// TestCardApplicationEndpointsEnforceOwnership 验证详情、更新和删除均由应用服务执行用户隔离。
func TestCardApplicationEndpointsEnforceOwnership(t *testing.T) {
	// server、store、cleanup 保存测试 HTTP 服务、SQLite 存储和资源释放函数。
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是创建跨用户数据夹具时使用的非取消上下文。
	ctx := context.Background()
	// created、createUserErr 保存卡券所有者创建结果及数据库错误。
	created, createUserErr := store.Users.Create(ctx, "card-owner", "card-owner@example.com", "pw")
	if createUserErr != nil || !created {
		t.Fatalf("创建卡券所有者失败 created=%v err=%v", created, createUserErr)
	}
	// owner、ownerErr 保存新用户详情及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "card-owner")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// cardID、createCardErr 保存属于新用户的卡券标识及创建错误。
	cardID, createCardErr := store.Cards.Create(ctx, &db.CardFull{Name: "他人卡券", Type: "text", TextContent: "SECRET", Enabled: true, UserID: owner.ID})
	if createCardErr != nil {
		t.Fatal(createCardErr)
	}
	// handler 是包含认证中间件和卡券路由的测试处理器。
	handler := server.Router()
	// adminCookie 是不拥有测试卡券的管理员会话 Cookie。
	adminCookie := loginHelper(t, handler)
	// testCase 是当前待验证的跨用户 HTTP 操作。
	for _, testCase := range []struct {
		// name 是测试子场景名称。
		name string
		// method 是请求使用的 HTTP 方法。
		method string
		// body 是更新操作使用的请求体；其他操作为空。
		body string
	}{
		{name: "get", method: http.MethodGet},
		{name: "update", method: http.MethodPut, body: `{"name":"篡改","type":"text","text_content":"X","enabled":true}`},
		{name: "delete", method: http.MethodDelete},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// request 是当前跨用户访问请求。
			request := httptest.NewRequest(testCase.method, "/cards/"+strconv.FormatInt(cardID, 10), strings.NewReader(testCase.body))
			request.AddCookie(adminCookie)
			// response 记录当前请求的 HTTP 状态和统一错误响应。
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("跨用户操作应返回 403，status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	// remaining、remainingErr 保存跨用户删除尝试后的数据库卡券记录及错误。
	remaining, remainingErr := store.Cards.Get(ctx, cardID)
	if remainingErr != nil || remaining.UserID != owner.ID || remaining.Name != "他人卡券" {
		t.Fatalf("跨用户操作不得修改或删除卡券，card=%+v err=%v", remaining, remainingErr)
	}
}

// TestCardApplicationEndpointsTreatNonPositiveIDAsMissing 验证数字格式合法但非正数的标识保持 404 兼容语义。
func TestCardApplicationEndpointsTreatNonPositiveIDAsMissing(t *testing.T) {
	// server、cleanup 保存测试 HTTP 服务和资源释放函数。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含认证中间件和卡券路由的测试处理器。
	handler := server.Router()
	// sessionCookie 是管理员登录后的认证 Cookie。
	sessionCookie := loginHelper(t, handler)
	// testCase 是当前待验证的卡券操作和请求体。
	for _, testCase := range []struct {
		// name 是测试子场景名称。
		name string
		// method 是请求使用的 HTTP 方法。
		method string
		// body 是更新操作使用的合法请求体。
		body string
	}{
		{name: "get", method: http.MethodGet},
		{name: "update", method: http.MethodPut, body: `{"name":"不存在","type":"text","text_content":"X","enabled":true}`},
		{name: "delete", method: http.MethodDelete},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// request 是使用零值卡券标识的认证请求。
			request := httptest.NewRequest(testCase.method, "/cards/0", strings.NewReader(testCase.body))
			request.AddCookie(sessionCookie)
			// response 记录资源缺失兼容响应。
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("非正数卡券标识应返回 404，status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

// TestWriteCardReadErrorClassification 验证读取错误分别映射为 404、403 和 500。
func TestWriteCardReadErrorClassification(t *testing.T) {
	// testCase 是当前应用错误与期望 HTTP 状态的映射。
	for _, testCase := range []struct {
		// name 是错误分类子场景名称。
		name string
		// err 是应用服务返回给 HTTP 边界的错误。
		err error
		// wantStatus 是期望写入的 HTTP 状态码。
		wantStatus int
	}{
		{name: "not-found", err: cardsapp.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "invalid-numeric-id", err: cardsapp.ErrInvalidCardID, wantStatus: http.StatusNotFound},
		{name: "forbidden", err: cardsapp.ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "infrastructure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// response 记录错误映射函数写入的状态和统一响应体。
			response := httptest.NewRecorder()
			writeCardReadError(response, testCase.err)
			if response.Code != testCase.wantStatus {
				t.Fatalf("错误状态不匹配 got=%d want=%d body=%s", response.Code, testCase.wantStatus, response.Body.String())
			}
		})
	}
}
