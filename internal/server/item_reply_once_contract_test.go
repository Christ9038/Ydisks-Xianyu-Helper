package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestItemReplyOnceContracts 通过真实handler验证商品开关创建、读写、列表、旧客户端缺省保留与显式false。
func TestItemReplyOnceContracts(t *testing.T) {
	// srv、cleanup 拥有隔离应用与数据库，store不直接暴露到HTTP响应。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是生产注册的真实路由；session 是测试登录后仅用于鉴权的凭据，不写入断言输出。
	handler := srv.Router()
	// session 只在请求头内使用，不参与业务DTO。
	session := loginHelper(t, handler)
	// request 以 method/path/body 调用真实路由并验证预期状态与成功契约。
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		// req、result 是单次请求和独立响应捕获器。
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(session)
		// result 不保存请求头，失败只报告非敏感状态。
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != status {
			t.Fatalf("%s %s status=%d want=%d", method, path, result.Code, status)
		}
		if status == http.StatusOK && strings.HasPrefix(path, "/api/v1/") {
			assertOpenAPIRecordedSuccessResponse(t, req, result)
		}
		return result
	}
	// path 是商品新接口；旧路径共用同一应用合并和授权流程。
	path := "/api/v1/reply-rules/items/acc1/once-item"
	request(http.MethodPut, path, `{"reply_content":"初始"}`, http.StatusOK)
	// check 读取当前DTO并断言 enabled，不将账号控制字段混入商品契约。
	check := func(enabled bool) {
		// response、reply 是真实接口响应与具名非敏感DTO。
		response := request(http.MethodGet, path, "", http.StatusOK)
		// reply 保留商品开关，避免测试只检查字段存在而漏掉false往返。
		var reply itemReplyResponse
		if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil { // err 是测试响应解码失败。
			t.Fatal(err)
		}
		if reply.ReplyOnce != enabled {
			t.Fatalf("reply_once=%v want=%v", reply.ReplyOnce, enabled)
		}
	}
	check(false)
	request(http.MethodPut, path, `{"reply_content":"单次","reply_once":true}`, http.StatusOK)
	check(true)
	request(http.MethodPut, "/item-reply/acc1/once-item", `{"reply_content":"旧客户端更新"}`, http.StatusOK)
	check(true)
	// list、rows 验证列表adapter能重新加载已勾选状态。
	list := request(http.MethodGet, "/api/v1/reply-rules/items", "", http.StatusOK)
	// rows 是当前用户商品配置DTO，不涉及持久化模型或凭证。
	var rows []itemReplyResponse
	if err := json.Unmarshal(list.Body.Bytes(), &rows); err != nil { // err 是列表响应解码失败。
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].ReplyOnce {
		t.Fatalf("列表未保存独立开关：%+v", rows)
	}
	request(http.MethodPut, path, `{"reply_content":"非法","reply_once":"true"}`, http.StatusBadRequest)
	check(true)
	request(http.MethodPut, path, `{"reply_content":"重复","reply_once":false}`, http.StatusOK)
	check(false)
	request(http.MethodDelete, path, "", http.StatusOK)
	check(false)
}
