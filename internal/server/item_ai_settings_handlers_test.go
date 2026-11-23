package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// TestItemAISettingsHandlersEnforceOwnershipAndValidation 验证商品级 AI 配置接口的越权、商品不存在、请求格式和取值校验，
// 以及失败请求不会留下任何配置、重置为继承且无资料时恢复默认。
func TestItemAISettingsHandlersEnforceOwnershipAndValidation(t *testing.T) {
	// srv、store、cleanup 是带管理员账号 acc1 的 HTTP 测试服务及其存储。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是写入夹具时使用的独立上下文。
	ctx := context.Background()
	// 管理员账号 acc1 下有一件商品；另一用户拥有账号 other-acc。
	if _, execErr := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','item-1','相机')`); execErr != nil {
		t.Fatal(execErr)
	}
	// createErr 保存 Create 调用返回的错误，随后断言它是否符合该场景的预期。
	if _, createErr := store.Users.Create(ctx, "other-owner", "other@example.com", "password"); createErr != nil {
		t.Fatal(createErr)
	}
	// other、otherErr 是另一用户及其查询错误。
	other, otherErr := store.Users.GetByUsername(ctx, "other-owner")
	if otherErr != nil {
		t.Fatal(otherErr)
	}
	// saveErr 保存 Save 调用返回的错误，随后断言它是否符合该场景的预期。
	if saveErr := store.Cookies.Save(ctx, "other-acc", "unb=2", other.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// handler、sessionCookie 是完整路由树及管理员会话。
	handler := srv.Router()
	// sessionCookie 保存 loginHelper 调用的返回结果，供随后的断言使用。
	sessionCookie := loginHelper(t, handler)
	// call 发送带管理员会话的请求并返回响应记录；body 为空表示无请求体。
	call := func(method, path, body string) *httptest.ResponseRecorder {
		// request 保存按是否带正文构造出的测试请求。
		var request *http.Request
		if body == "" {
			request = httptest.NewRequest(method, path, nil)
		} else {
			request = httptest.NewRequest(method, path, strings.NewReader(body))
		}
		request.AddCookie(sessionCookie)
		// recorder 保存 NewRecorder 调用的返回结果，供随后的断言使用。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	// 未登录请求必须被认证层拒绝。
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-1/ai-settings", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("未登录读取应返回 401，got %d", anonymous.Code)
	}

	// cases 覆盖读取与保存的各类失败，每项都必须得到明确的状态码。
	cases := []struct {
		// name 是子用例说明。
		name string
		// method、path、body 是请求内容。
		method, path, body string
		// want 是期望状态码。
		want int
	}{
		{"读取他人账号", http.MethodGet, "/api/v1/items/other-acc/item-1/ai-settings", "", http.StatusForbidden},
		{"保存他人账号", http.MethodPut, "/api/v1/items/other-acc/item-1/ai-settings", `{"ai_override":"enabled","item_context":""}`, http.StatusForbidden},
		{"读取不存在账号", http.MethodGet, "/api/v1/items/ghost/item-1/ai-settings", "", http.StatusNotFound},
		{"读取不存在商品", http.MethodGet, "/api/v1/items/acc1/ghost-item/ai-settings", "", http.StatusNotFound},
		{"保存不存在商品", http.MethodPut, "/api/v1/items/acc1/ghost-item/ai-settings", `{"ai_override":"enabled","item_context":""}`, http.StatusNotFound},
		{"请求体不是 JSON", http.MethodPut, "/api/v1/items/acc1/item-1/ai-settings", `not-json`, http.StatusBadRequest},
		{"覆盖值非法", http.MethodPut, "/api/v1/items/acc1/item-1/ai-settings", `{"ai_override":"always","item_context":""}`, http.StatusBadRequest},
		{"资料超过上限", http.MethodPut, "/api/v1/items/acc1/item-1/ai-settings", fmt.Sprintf(`{"ai_override":"enabled","item_context":%q}`, strings.Repeat("中", itemapp.ItemAIContextMaxRunes+1)), http.StatusBadRequest},
	}
	// tc 是当前失败场景。
	for _, tc := range cases {
		// recorder 保存 call 调用的返回结果，供随后的断言使用。
		if recorder := call(tc.method, tc.path, tc.body); recorder.Code != tc.want {
			t.Errorf("%s: 状态码=%d want %d body=%s", tc.name, recorder.Code, tc.want, recorder.Body.String())
		}
	}
	// 所有失败请求之后，商品仍应是默认继承配置。
	var afterFailures itemAISettingsResponse
	// recorder 保存 call 调用的返回结果，供随后的断言使用。
	recorder := call(http.MethodGet, "/api/v1/items/acc1/item-1/ai-settings", "")
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &afterFailures) != nil || afterFailures.AIOverride != "inherit" || afterFailures.ItemContext != "" {
		t.Fatalf("失败请求不得留下配置: code=%d %+v", recorder.Code, afterFailures)
	}

	// 恰好上限字符数的资料可以保存（边界值），并通过商品列表体现 ai_override。
	maxContext := strings.Repeat("中", itemapp.ItemAIContextMaxRunes)
	// saved 保存 call 调用的返回结果，供随后的断言使用。
	if saved := call(http.MethodPut, "/api/v1/items/acc1/item-1/ai-settings", fmt.Sprintf(`{"ai_override":"disabled","item_context":%q}`, maxContext)); saved.Code != http.StatusOK {
		t.Fatalf("上限资料应允许保存: %d %s", saved.Code, saved.Body.String())
	}
	// list 保存商品列表接口响应解码后的商品集合。
	var list []itemListResponse
	// listRecorder 保存 call 调用的返回结果，供随后的断言使用。
	listRecorder := call(http.MethodGet, "/api/v1/items?cookie_id=acc1", "")
	if json.Unmarshal(listRecorder.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].AIOverride != "disabled" {
		t.Fatalf("商品列表必须反映保存后的 ai_override: %s", listRecorder.Body.String())
	}
	// 重置为继承且清空资料后，读取恢复默认并且列表回到 inherit。
	if reset := call(http.MethodPut, "/api/v1/items/acc1/item-1/ai-settings", `{"ai_override":"inherit","item_context":""}`); reset.Code != http.StatusOK {
		t.Fatalf("重置应成功: %d", reset.Code)
	}
	// resetValue 保存重置后读取到的商品 AI 配置响应。
	var resetValue itemAISettingsResponse
	// resetRecorder 保存 call 调用的返回结果，供随后的断言使用。
	resetRecorder := call(http.MethodGet, "/api/v1/items/acc1/item-1/ai-settings", "")
	if json.Unmarshal(resetRecorder.Body.Bytes(), &resetValue) != nil || resetValue.AIOverride != "inherit" || resetValue.ItemContext != "" {
		t.Fatalf("重置后必须是默认继承: %s", resetRecorder.Body.String())
	}
}

// TestWriteItemAISettingsErrorMapsApplicationErrors 验证应用层稳定错误到 HTTP 状态码的完整映射，
// 且未识别的基础设施错误只返回通用 500 文案，不泄露底层错误细节。
func TestWriteItemAISettingsErrorMapsApplicationErrors(t *testing.T) {
	// srv 仅用于调用错误映射方法，不处理请求。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// cases 列出每种应用层错误及其期望状态码。
	cases := []struct {
		// name 是子用例说明。
		name string
		// err 是传入映射函数的错误。
		err error
		// want 是期望状态码。
		want int
	}{
		{"越权", itemapp.ErrItemAISettingsForbidden, http.StatusForbidden},
		{"包装的越权", fmt.Errorf("外层: %w", itemapp.ErrItemAISettingsForbidden), http.StatusForbidden},
		{"商品不存在", itemapp.ErrItemAISettingsItemNotFound, http.StatusNotFound},
		{"覆盖值非法", itemapp.ErrItemAISettingsInvalidOverride, http.StatusBadRequest},
		{"资料过长", itemapp.ErrItemAISettingsContextTooLong, http.StatusBadRequest},
		{"资料编码非法", itemapp.ErrItemAISettingsInvalidContext, http.StatusBadRequest},
		{"基础设施错误", errors.New("dial tcp 10.0.0.1:3306: secret-host refused"), http.StatusInternalServerError},
	}
	// tc 是当前错误映射场景。
	for _, tc := range cases {
		// recorder 保存 NewRecorder 调用的返回结果，供随后的断言使用。
		recorder := httptest.NewRecorder()
		srv.writeItemAISettingsError(recorder, httptest.NewRequest(http.MethodGet, "/x", nil), tc.err)
		if recorder.Code != tc.want {
			t.Errorf("%s: 状态码=%d want %d", tc.name, recorder.Code, tc.want)
		}
		if tc.want == http.StatusInternalServerError && strings.Contains(recorder.Body.String(), "secret-host") {
			t.Errorf("500 响应不得泄露底层错误: %s", recorder.Body.String())
		}
	}
}
