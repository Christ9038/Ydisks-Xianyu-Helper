package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestReplyImageConfigurationContracts 验证账号及商品图文的新旧接口回显、缺省保留、来源冲突及删除继承。
func TestReplyImageConfigurationContracts(t *testing.T) {
	// srv、cleanup 拥有隔离测试服务及资源释放责任。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler、sessionCookie 是真实路由树和认证会话。
	handler := srv.Router()
	// sessionCookie 是管理员登录后供每次请求使用的认证凭证。
	sessionCookie := loginHelper(t, handler)
	// request 执行当前 method/path/body 并检查 want 状态；成功响应使用同一 OpenAPI 契约验证。
	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		// req、recorder 是本次请求与其独立响应捕获器。
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(sessionCookie)
		// recorder 独立捕获当前真实路由响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, recorder.Code, want, recorder.Body.String())
		}
		if want == http.StatusOK && strings.HasPrefix(path, "/api/v1/") {
			assertOpenAPIRecordedSuccessResponse(t, req, recorder)
		}
		return recorder
	}
	// paths 将两种配置与各自旧兼容入口配对。
	for _, paths := range [][2]string{{"/api/v1/default-replies/acc1", "/api/default-reply/acc1"}, {"/api/v1/reply-rules/items/acc1/item-1", "/item-reply/acc1/item-1"}} {
		request(http.MethodPut, paths[0], `{"enabled":true,"reply_once":true,"reply_content":"图片说明","reply_image_url":"","reply_image_path":"商品/介绍.png"}`, http.StatusOK)
		request(http.MethodPut, paths[1], `{"enabled":true,"reply_once":true,"reply_content":"旧版更新","reply_image_url":""}`, http.StatusOK)
		// result、config 保存配置响应及解码后的非敏感字段。
		result := request(http.MethodGet, paths[0], "", http.StatusOK)
		// config 保存解码后的非敏感配置，用于验证本地图未丢失。
		var config map[string]any
		if err := json.Unmarshal(result.Body.Bytes(), &config); err != nil { // err 是测试响应解码错误。
			t.Fatal(err)
		}
		if config["reply_image_path"] != "商品/介绍.png" || config["reply_content"] != "旧版更新" {
			t.Fatalf("旧客户端丢失图片: %+v", config)
		}
		request(http.MethodPut, paths[0], `{"reply_content":"不应写入","reply_image_url":"https://example.test/image.png"}`, http.StatusBadRequest)
		request(http.MethodPut, paths[0], `{"reply_image_path":"../outside.png","reply_image_url":""}`, http.StatusBadRequest)
		request(http.MethodPut, paths[0], `{"reply_image_path":"C:\\outside.png","reply_image_url":""}`, http.StatusBadRequest)
		request(http.MethodPut, paths[0], `{"reply_image_path":"","reply_image_url":"https://example.test/image.png"}`, http.StatusOK)
		request(http.MethodGet, paths[0], "", http.StatusOK)
	}
	request(http.MethodGet, "/api/v1/reply-rules/items", "", http.StatusOK)
	request(http.MethodGet, "/api/v1/default-replies", "", http.StatusOK)
	request(http.MethodGet, "/api/v1/default-replies/list", "", http.StatusOK)
	request(http.MethodDelete, "/api/v1/reply-rules/items/acc1/item-1", "", http.StatusOK)
	// deleted 是删除后的兼容空正文响应，表示恢复账号兜底。
	deleted := request(http.MethodGet, "/api/v1/reply-rules/items/acc1/item-1", "", http.StatusOK)
	if strings.Contains(deleted.Body.String(), "example.test") || strings.Contains(deleted.Body.String(), "介绍.png") {
		t.Fatal("删除商品配置后仍返回旧图片")
	}
}
