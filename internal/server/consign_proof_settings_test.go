package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAccountSettingsConsignProofRoundTrip 验证确认发货凭证设置经真实 handler 保存后，列表与单账号详情都按 OpenAPI 返回，非法组合返回统一错误信封。
func TestAccountSettingsConsignProofRoundTrip(t *testing.T) {
	// srv 是带测试数据库的 HTTP 服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员会话。
	sessionCookie := loginHelper(t, handler)

	// send 发送带会话的请求并返回响应记录器。
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

	// defaultReq、defaultRec 验证未配置时详情返回默认的 delivery 模式。
	defaultReq, defaultRec := send(http.MethodGet, "/api/v1/accounts/acc1", "")
	assertOpenAPISuccessResponse(t, defaultReq, defaultRec)
	// defaultDetail 是默认详情响应。
	var defaultDetail cookieDetailResponse
	// decodeErr 保存默认详情响应的解码错误。
	if decodeErr := json.Unmarshal(defaultRec.Body.Bytes(), &defaultDetail); decodeErr != nil || defaultDetail.ConsignProofMode != "delivery" {
		t.Fatalf("默认凭证模式错误: %s err=%v", defaultRec.Body.String(), decodeErr)
	}

	// saveReq、saveRec 保存固定文字模式。
	saveReq, saveRec := send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"consign_proof_mode":"fixed","consign_proof_text":"已通过聊天发送"}`)
	assertOpenAPISuccessResponse(t, saveReq, saveRec)

	// detailReq、detailRec 读取单账号详情。
	detailReq, detailRec := send(http.MethodGet, "/api/v1/accounts/acc1", "")
	assertOpenAPISuccessResponse(t, detailReq, detailRec)
	// detail 是保存后的单账号详情。
	var detail cookieDetailResponse
	// decodeErr 保存保存后详情响应的解码错误。
	if decodeErr := json.Unmarshal(detailRec.Body.Bytes(), &detail); decodeErr != nil || detail.ConsignProofMode != "fixed" || detail.ConsignProofText != "已通过聊天发送" {
		t.Fatalf("详情凭证设置错误: %s err=%v", detailRec.Body.String(), decodeErr)
	}

	// listReq、listRec 读取账号详情列表。
	listReq, listRec := send(http.MethodGet, "/api/v1/accounts/details", "")
	assertOpenAPISuccessResponse(t, listReq, listRec)
	// list 是账号详情列表。
	var list []cookieSummaryResponse
	// decodeErr 保存账号列表响应的解码错误。
	if decodeErr := json.Unmarshal(listRec.Body.Bytes(), &list); decodeErr != nil || len(list) == 0 || list[0].ConsignProofMode != "fixed" || list[0].ConsignProofText != "已通过聊天发送" {
		t.Fatalf("列表凭证设置错误: %s err=%v", listRec.Body.String(), decodeErr)
	}

	// blankReq、blankRec 验证固定文字模式下把文字清空被拒绝。
	blankReq, blankRec := send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"consign_proof_text":""}`)
	assertOpenAPIResponse(t, blankReq, blankRec)
	if blankRec.Code != http.StatusBadRequest || !strings.Contains(blankRec.Body.String(), "固定文字不能为空") {
		t.Fatalf("清空固定文字应返回 400: status=%d body=%s", blankRec.Code, blankRec.Body.String())
	}

	// unknownReq、unknownRec 验证未知模式被拒绝（OpenAPI 枚举与应用层校验一致）。
	unknownReq, unknownRec := send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"consign_proof_mode":"all"}`)
	if unknownRec.Code != http.StatusBadRequest {
		t.Fatalf("未知模式应返回 400: status=%d body=%s req=%s", unknownRec.Code, unknownRec.Body.String(), unknownReq.URL.Path)
	}
}
