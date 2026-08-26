package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// settingsHandlerCoveragePort 为单项系统设置 handler 注入可控敏感键判断和写入结果。
type settingsHandlerCoveragePort struct {
	// SettingsPort 提供当前场景未覆盖设置查询能力的默认实现。
	SettingsPort
	// sensitiveKeys 保存测试环境中的敏感设置名称。
	sensitiveKeys map[string]bool
	// setErr 保存单项系统设置写入错误。
	setErr error
}

// IsSensitiveSettingKey 返回测试配置的敏感设置判断结果。
func (port *settingsHandlerCoveragePort) IsSensitiveSettingKey(key string) bool {
	return port.sensitiveKeys[key]
}

// SetSystem 返回测试配置的单项系统设置写入错误。
func (port *settingsHandlerCoveragePort) SetSystem(context.Context, int64, string, string, string) error {
	return port.setErr
}

// TestSetSettingHandlerCoversSensitiveAndValidationBranches 覆盖单项系统设置的普通、敏感和校验分支。
func TestSetSettingHandlerCoversSensitiveAndValidationBranches(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是当前测试注入的系统设置应用端口。
	port := &settingsHandlerCoveragePort{sensitiveKeys: map[string]bool{"ai_api_key": true}}
	srv.applications.settings = port
	// handler 是注入可控系统设置端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// successCases 保存普通和敏感设置的成功请求。
	successCases := []struct {
		path string
		body string
	}{
		{"/system-settings/theme_color", `{"value":"blue"}`},
		{"/system-settings/ai_api_key", `{"action":"replace","value":"key"}`},
		{"/system-settings/ai_api_key", `{"action":"clear"}`},
		{"/system-settings/log_level", `{"value":"debug"}`},
		{"/system-settings/outbound_http_public_only", `{"value":"true"}`},
	}
	// successCase 表示当前系统设置成功场景。
	for _, successCase := range successCases {
		// recorder 保存当前系统设置响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, successCase.path, successCase.body)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s status=%d body=%s", successCase.path, recorder.Code, recorder.Body.String())
		}
	}

	// validationCases 保存系统设置请求格式和字段校验场景。
	validationCases := []struct {
		path string
		body string
	}{
		{"/system-settings/theme_color", "{"},
		{"/system-settings/ai_api_key", `{"action":"invalid","value":"key"}`},
		{"/system-settings/log_level", `{"value":"verbose"}`},
		{"/system-settings/outbound_http_public_only", `{"value":"maybe"}`},
	}
	// validationCase 表示当前系统设置校验场景。
	for _, validationCase := range validationCases {
		// recorder 保存当前系统设置校验响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, validationCase.path, validationCase.body)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", validationCase.path, recorder.Code, recorder.Body.String())
		}
	}
	port.setErr = errors.New("setting write failed")
	// normalErrorRecorder 保存普通设置写入错误响应。
	normalErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/system-settings/theme_color", `{"value":"red"}`)
	if normalErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("normal error status=%d", normalErrorRecorder.Code)
	}
	// sensitiveErrorRecorder 保存敏感设置写入错误响应。
	sensitiveErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/system-settings/ai_api_key", `{"action":"clear"}`)
	if sensitiveErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("sensitive error status=%d", sensitiveErrorRecorder.Code)
	}
}
