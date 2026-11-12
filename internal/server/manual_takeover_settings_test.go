package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestAccountSettingsManualTakeoverRoundTrip 验证人工介入暂停设置经真实 handler 保存后，列表与详情按 OpenAPI 返回，越界与未知范围返回统一错误信封。
func TestAccountSettingsManualTakeoverRoundTrip(t *testing.T) {
	// srv 是带测试数据库的 HTTP 服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员会话。
	sessionCookie := loginHelper(t, handler)
	// send 发送带会话的请求并返回请求与响应记录器。
	send := func(method, path, body string) (*http.Request, *httptest.ResponseRecorder) {
		// request 是本次构造的 HTTP 请求；body 为空时不附带请求体。
		var request *http.Request
		if body == "" {
			request = httptest.NewRequest(method, path, nil)
		} else {
			request = httptest.NewRequest(method, path, strings.NewReader(body))
		}
		request.AddCookie(sessionCookie)
		// recorder 捕获响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return request, recorder
	}

	// defaultReq、defaultRec 验证未配置时详情返回关闭与只暂停 AI。
	defaultReq, defaultRec := send(http.MethodGet, "/api/v1/accounts/acc1", "")
	assertOpenAPISuccessResponse(t, defaultReq, defaultRec)
	// defaultDetail 是默认详情响应。
	var defaultDetail cookieDetailResponse
	// decodeErr 保存响应 JSON 解码错误，非空说明响应形状与 DTO 不符。
	if decodeErr := json.Unmarshal(defaultRec.Body.Bytes(), &defaultDetail); decodeErr != nil || defaultDetail.ManualTakeoverMinutes != 0 || defaultDetail.ManualTakeoverScope != "ai" {
		t.Fatalf("默认人工介入设置错误: %s err=%v", defaultRec.Body.String(), decodeErr)
	}

	// saveReq、saveRec 保存 15 分钟、暂停全部。
	saveReq, saveRec := send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"manual_takeover_minutes":15,"manual_takeover_scope":"all"}`)
	assertOpenAPISuccessResponse(t, saveReq, saveRec)

	// detailReq、detailRec 读取保存后的单账号详情。
	detailReq, detailRec := send(http.MethodGet, "/api/v1/accounts/acc1", "")
	assertOpenAPISuccessResponse(t, detailReq, detailRec)
	// detail 是保存后的详情。
	var detail cookieDetailResponse
	// decodeErr 保存响应 JSON 解码错误，非空说明响应形状与 DTO 不符。
	if decodeErr := json.Unmarshal(detailRec.Body.Bytes(), &detail); decodeErr != nil || detail.ManualTakeoverMinutes != 15 || detail.ManualTakeoverScope != "all" {
		t.Fatalf("详情人工介入设置错误: %s err=%v", detailRec.Body.String(), decodeErr)
	}
	// listReq、listRec 读取账号详情列表。
	listReq, listRec := send(http.MethodGet, "/api/v1/accounts/details", "")
	assertOpenAPISuccessResponse(t, listReq, listRec)
	// list 是账号详情列表。
	var list []cookieSummaryResponse
	// decodeErr 保存响应 JSON 解码错误，非空说明响应形状与 DTO 不符。
	if decodeErr := json.Unmarshal(listRec.Body.Bytes(), &list); decodeErr != nil || len(list) == 0 || list[0].ManualTakeoverMinutes != 15 || list[0].ManualTakeoverScope != "all" {
		t.Fatalf("列表人工介入设置错误: %s err=%v", listRec.Body.String(), decodeErr)
	}

	// overReq、overRec 验证超过 1440 分钟被拒绝。
	overReq, overRec := send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"manual_takeover_minutes":1441}`)
	if overRec.Code != http.StatusBadRequest || !strings.Contains(overRec.Body.String(), "暂停分钟数") {
		t.Fatalf("越界分钟数应返回 400: status=%d body=%s req=%s", overRec.Code, overRec.Body.String(), overReq.URL.Path)
	}
	// scopeReq、scopeRec 验证未知范围被拒绝。
	scopeReq, scopeRec := send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"manual_takeover_scope":"agent"}`)
	if scopeRec.Code != http.StatusBadRequest {
		t.Fatalf("未知范围应返回 400: status=%d body=%s req=%s", scopeRec.Code, scopeRec.Body.String(), scopeReq.URL.Path)
	}
}

// TestKeywordNotifyManualRoundTrip 验证关键词“通知人工”标记经真实 handler 创建、读取与更新，响应符合 OpenAPI。
func TestKeywordNotifyManualRoundTrip(t *testing.T) {
	// srv 是带测试数据库的 HTTP 服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员会话。
	sessionCookie := loginHelper(t, handler)
	// send 发送带会话的 JSON 请求。
	send := func(method, path, body string) (*http.Request, *httptest.ResponseRecorder) {
		// request 是本次构造的 HTTP 请求。
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.AddCookie(sessionCookie)
		// recorder 捕获响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return request, recorder
	}
	// createReq、createRec 创建一条开启通知的规则。
	createReq, createRec := send(http.MethodPost, "/api/v1/reply-rules/acc1/items", `{"keyword":"转人工","reply":"已为您转接人工","item_id":"","item_ids":[],"type":"text","image_url":"","notify_manual":true}`)
	assertOpenAPISuccessResponse(t, createReq, createRec)
	// readRules 读取类型化规则列表。
	readRules := func() []keywordTypedResponse {
		// listReq、listRec 读取类型化规则列表。
		listReq, listRec := send(http.MethodGet, "/api/v1/reply-rules/acc1/typed", "")
		assertOpenAPISuccessResponse(t, listReq, listRec)
		// rules 是解码后的规则列表。
		var rules []keywordTypedResponse
		// decodeErr 保存响应 JSON 解码错误，非空说明响应形状与 DTO 不符。
		if decodeErr := json.Unmarshal(listRec.Body.Bytes(), &rules); decodeErr != nil {
			t.Fatalf("解码规则列表: %v", decodeErr)
		}
		return rules
	}
	// rules 是创建后的规则列表。
	rules := readRules()
	if len(rules) != 1 || !rules[0].NotifyManual {
		t.Fatalf("创建后应开启通知人工: %#v", rules)
	}
	// updateReq、updateRec 关闭通知标记。
	updateReq, updateRec := send(http.MethodPut, "/api/v1/reply-rules/acc1/typed/"+strconv.FormatInt(rules[0].ID, 10), `{"keyword":"转人工","reply":"已为您转接人工","item_id":"","item_ids":[],"type":"text","image_url":""}`)
	assertOpenAPISuccessResponse(t, updateReq, updateRec)
	if rules = readRules(); len(rules) != 1 || rules[0].NotifyManual {
		t.Fatalf("省略 notify_manual 的更新应关闭通知: %#v", rules)
	}
}
