package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAccountSettingsAutoRelistIsIndependent 验证“售出后自动上架”默认关闭、可单独保存，并且不影响自动免拼等其他开关；详情与列表都按 OpenAPI 返回。
func TestAccountSettingsAutoRelistIsIndependent(t *testing.T) {
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
	// detail 读取并解码单账号详情。
	detail := func() cookieDetailResponse {
		// recorder 是捕获 HTTP 响应的记录器；request 是本次构造的 HTTP 请求。
		request, recorder := send(http.MethodGet, "/api/v1/accounts/acc1", "")
		assertOpenAPISuccessResponse(t, request, recorder)
		// decoded 是解码后的详情响应。
		var decoded cookieDetailResponse
		// err 是本次调用返回的错误，用于断言错误映射或失败分类。
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("详情解码失败: %v", err)
		}
		return decoded
	}
	// initial 是未配置时读取到的账号详情。
	if initial := detail(); initial.AutoRelist {
		t.Fatalf("默认应关闭: %+v", initial)
	}
	// 先开启自动免拼，再单独开启自动上架，两者互不影响。
	saveReq, saveRec := send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"auto_bargain":true}`)
	assertOpenAPISuccessResponse(t, saveReq, saveRec)
	// current 是当前保存后读取到的账号详情。
	if current := detail(); !current.AutoBargain || current.AutoRelist {
		t.Fatalf("开启自动免拼不应开启自动上架: %+v", current)
	}
	saveReq, saveRec = send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"auto_relist":true}`)
	assertOpenAPISuccessResponse(t, saveReq, saveRec)
	// current 是当前保存后读取到的账号详情。
	if current := detail(); !current.AutoBargain || !current.AutoRelist {
		t.Fatalf("单独开启自动上架不应改变自动免拼: %+v", current)
	}
	saveReq, saveRec = send(http.MethodPut, "/api/v1/accounts/acc1/settings", `{"auto_bargain":false}`)
	assertOpenAPISuccessResponse(t, saveReq, saveRec)
	// current 是当前保存后读取到的账号详情。
	if current := detail(); current.AutoBargain || !current.AutoRelist {
		t.Fatalf("关闭自动免拼不应关闭自动上架: %+v", current)
	}
	// listReq、listRec 验证账号列表同样返回该字段。
	listReq, listRec := send(http.MethodGet, "/api/v1/accounts/details", "")
	assertOpenAPISuccessResponse(t, listReq, listRec)
	// list 是账号详情列表。
	var list []cookieSummaryResponse
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil || len(list) == 0 || !list[0].AutoRelist {
		t.Fatalf("列表应返回自动上架开关: %s err=%v", listRec.Body.String(), err)
	}
}
