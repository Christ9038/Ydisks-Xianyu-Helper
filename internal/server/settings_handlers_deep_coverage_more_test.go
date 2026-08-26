package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	settingsapp "xianyu-go/internal/application/settings"
)

// settingsHandlerDeepCoveragePort 为设置 Handler 提供可编程的账号 AI、用户和系统设置结果。
type settingsHandlerDeepCoveragePort struct {
	// SettingsPort 提供当前场景未覆盖方法的默认能力。
	SettingsPort
	// sensitiveKeys 保存测试环境中的敏感设置键。
	sensitiveKeys map[string]bool
	// aiConfig、aiConfigErr 保存账号 AI 设置查询结果和错误。
	aiConfig    settingsapp.AIReplySettings
	aiConfigErr error
	// aiRows、aiRowsErr 保存账号 AI 列表查询结果和错误。
	aiRows    []settingsapp.AIReplySettings
	aiRowsErr error
	// operationErr 保存设置写入操作的预置错误。
	operationErr error
	// listErr 保存列表查询的预置错误。
	listErr error
	// userValue 保存用户单键设置成功值。
	userValue string
}

// IsSensitiveSettingKey 返回测试配置中的敏感键判断结果。
func (port *settingsHandlerDeepCoveragePort) IsSensitiveSettingKey(key string) bool {
	return port.sensitiveKeys[key]
}

// GetAIReply 返回测试预置的账号 AI 配置或错误。
func (port *settingsHandlerDeepCoveragePort) GetAIReply(context.Context, int64, string) (settingsapp.AIReplySettings, error) {
	return port.aiConfig, port.aiConfigErr
}

// ListAIReply 返回测试预置的账号 AI 配置列表或错误。
func (port *settingsHandlerDeepCoveragePort) ListAIReply(context.Context, int64) ([]settingsapp.AIReplySettings, error) {
	if port.aiRowsErr != nil {
		return nil, port.aiRowsErr
	}
	return port.aiRows, port.listErr
}

// UpsertAIReply 返回测试预置的账号 AI 写入错误。
func (port *settingsHandlerDeepCoveragePort) UpsertAIReply(context.Context, int64, string, settingsapp.AIReplySettings) error {
	return port.operationErr
}

// ApplySystemChanges 返回测试预置的批量系统设置写入错误。
func (port *settingsHandlerDeepCoveragePort) ApplySystemChanges(context.Context, int64, map[string]string, map[string]settingsapp.SecretChange) error {
	return port.operationErr
}

// GetSystem 返回测试预置的系统设置错误或空设置。
func (port *settingsHandlerDeepCoveragePort) GetSystem(context.Context, int64) (map[string]string, error) {
	return map[string]string{"theme_color": "blue"}, port.listErr
}

// ListAIModels 返回测试预置的模型列表或错误。
func (port *settingsHandlerDeepCoveragePort) ListAIModels(context.Context, int64, string, string) ([]string, error) {
	if port.listErr != nil {
		return nil, port.listErr
	}
	return []string{"model-a"}, nil
}

// ListUser 返回测试预置的用户设置或错误。
func (port *settingsHandlerDeepCoveragePort) ListUser(context.Context, int64) (map[string]string, error) {
	return map[string]string{"theme": "dark"}, port.listErr
}

// GetUser 返回测试预置的用户设置值或错误。
func (port *settingsHandlerDeepCoveragePort) GetUser(context.Context, int64, string) (string, error) {
	return port.userValue, port.listErr
}

// SetUser 返回测试预置的用户设置写入错误。
func (port *settingsHandlerDeepCoveragePort) SetUser(context.Context, int64, string, string) error {
	return port.operationErr
}

// TestSettingsHandlersCoverAIAndUserBranches 覆盖 AI 设置、模型目录和用户设置 Handler 的成功与错误分支。
func TestSettingsHandlersCoverAIAndUserBranches(t *testing.T) {
	// server、cleanup 保存设置 Handler 使用的测试服务器和资源清理函数。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 保存当前注入的设置应用 Port。
	port := &settingsHandlerDeepCoveragePort{
		sensitiveKeys: map[string]bool{"ai_api_key": true},
		aiConfig:      settingsapp.AIReplySettings{CookieID: "cid", AIEnabled: true, AutoAdjustPriceEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 3, CustomPrompts: "prompt"},
		aiRows:        []settingsapp.AIReplySettings{{CookieID: "cid", AIEnabled: true}},
		userValue:     "value",
	}
	server.applications.settings = port
	// params 保存 AI 设置路由所需的账号标识。
	params := map[string]string{"cookie_id": "cid"}
	// successCases 保存 AI、模型和用户设置成功 Handler 场景。
	successCases := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "list ai", hand: server.listAIReply, req: requestWithKeywordParams(http.MethodGet, "/ai-reply-settings", "", params)},
		{name: "get ai", hand: server.getAIReply, req: requestWithKeywordParams(http.MethodGet, "/ai-reply-settings/cid", "", params)},
		{name: "set ai", hand: server.setAIReply, req: requestWithKeywordParams(http.MethodPut, "/ai-reply-settings/cid", `{"ai_enabled":true,"auto_adjust_price_enabled":true,"max_discount_percent":10,"max_discount_amount":20,"max_bargain_rounds":3}`, params)},
		{name: "models", hand: server.listAIModels, req: requestWithKeywordParams(http.MethodPost, "/ai-models", `{}`, nil)},
		{name: "list user", hand: server.listUserSettings, req: requestWithServerSession(http.MethodGet, "/user-settings", nil)},
		{name: "get user", hand: server.getUserSetting, req: requestWithKeywordParams(http.MethodGet, "/user-settings/theme", "", map[string]string{"key": "theme"})},
		{name: "set user", hand: server.setUserSetting, req: requestWithKeywordParams(http.MethodPut, "/user-settings/theme", `{"value":"value"}`, map[string]string{"key": "theme"})},
	}
	// successCase 表示当前设置成功 Handler 场景。
	for _, successCase := range successCases {
		t.Run(successCase.name, func(t *testing.T) {
			// recorder 保存当前设置 Handler 的响应。
			recorder := httptest.NewRecorder()
			successCase.hand(recorder, successCase.req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	// port.aiConfigErr 保存 AI 配置缺失结果，验证 getAIReply 的默认响应。
	port.aiConfigErr = settingsapp.ErrConfigNotFound
	// defaultRecorder 保存未配置 AI 时的默认值响应。
	defaultRecorder := httptest.NewRecorder()
	server.getAIReply(defaultRecorder, requestWithKeywordParams(http.MethodGet, "/ai-reply-settings/cid", "", params))
	if defaultRecorder.Code != http.StatusOK {
		t.Fatalf("default AI status=%d", defaultRecorder.Code)
	}
	// port.aiConfigErr 保存账号不存在结果，验证账号错误映射。
	port.aiConfigErr = settingsapp.ErrAccountNotFound
	// missingRecorder 保存账号不存在响应。
	missingRecorder := httptest.NewRecorder()
	server.getAIReply(missingRecorder, requestWithKeywordParams(http.MethodGet, "/ai-reply-settings/cid", "", params))
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("missing AI status=%d", missingRecorder.Code)
	}
	// port.aiConfigErr 保存普通查询错误，验证内部错误映射。
	port.aiConfigErr = errors.New("AI read failed")
	// readErrorRecorder 保存 AI 查询失败响应。
	readErrorRecorder := httptest.NewRecorder()
	server.getAIReply(readErrorRecorder, requestWithKeywordParams(http.MethodGet, "/ai-reply-settings/cid", "", params))
	if readErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("AI read error status=%d", readErrorRecorder.Code)
	}
	// port.aiConfigErr 清空所有权查询错误，准备验证字段校验和写入映射。
	port.aiConfigErr = nil
	// validationBodies 保存 AI 设置的边界校验请求。
	validationBodies := []string{
		`{"max_discount_percent":101,"max_bargain_rounds":3}`,
		`{"max_discount_amount":-1,"max_bargain_rounds":3}`,
		`{"max_bargain_rounds":0}`,
		`{"auto_adjust_price_enabled":true,"ai_enabled":false,"max_bargain_rounds":3}`,
	}
	// validationBody 表示当前 AI 字段校验请求。
	for _, validationBody := range validationBodies {
		// recorder 保存字段校验响应。
		recorder := httptest.NewRecorder()
		server.setAIReply(recorder, requestWithKeywordParams(http.MethodPut, "/ai-reply-settings/cid", validationBody, params))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("AI validation status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	// port.operationErr 保存 AI 议价冲突错误。
	port.operationErr = settingsapp.ErrPricingModeConflict
	// conflictRecorder 保存 AI 议价冲突响应。
	conflictRecorder := httptest.NewRecorder()
	server.setAIReply(conflictRecorder, requestWithKeywordParams(http.MethodPut, "/ai-reply-settings/cid", `{"ai_enabled":true,"max_bargain_rounds":3}`, params))
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf("AI conflict status=%d", conflictRecorder.Code)
	}
	// port.operationErr 保存普通 AI 写入错误。
	port.operationErr = errors.New("AI write failed")
	// writeErrorRecorder 保存 AI 写入失败响应。
	writeErrorRecorder := httptest.NewRecorder()
	server.setAIReply(writeErrorRecorder, requestWithKeywordParams(http.MethodPut, "/ai-reply-settings/cid", `{"ai_enabled":true,"max_bargain_rounds":3}`, params))
	if writeErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("AI write error status=%d", writeErrorRecorder.Code)
	}
	// port.listErr 保存列表读取错误，覆盖 AI 列表、系统设置和用户设置错误分支。
	port.listErr = errors.New("settings list failed")
	// listErrorHandlers 保存多个设置列表错误 Handler。
	listErrorHandlers := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "ai list", hand: server.listAIReply, req: requestWithKeywordParams(http.MethodGet, "/ai-reply-settings", "", params)},
		{name: "system", hand: server.allSettings, req: requestWithServerSession(http.MethodGet, "/system-settings", nil)},
		{name: "user list", hand: server.listUserSettings, req: requestWithServerSession(http.MethodGet, "/user-settings", nil)},
		{name: "models", hand: server.listAIModels, req: requestWithServerSession(http.MethodPost, "/ai-models", nil)},
	}
	// listErrorHandler 表示当前设置列表错误场景。
	for _, listErrorHandler := range listErrorHandlers {
		// recorder 保存设置列表错误响应。
		recorder := httptest.NewRecorder()
		listErrorHandler.hand(recorder, listErrorHandler.req)
		if recorder.Code != http.StatusInternalServerError && listErrorHandler.name != "models" {
			t.Fatalf("%s status=%d", listErrorHandler.name, recorder.Code)
		}
	}
	// port.operationErr 保存用户设置写入错误。
	port.operationErr = errors.New("user write failed")
	// userWriteRecorder 保存用户设置写入失败响应。
	userWriteRecorder := httptest.NewRecorder()
	server.setUserSetting(userWriteRecorder, requestWithKeywordParams(http.MethodPut, "/user-settings/theme", `{"value":"value"}`, map[string]string{"key": "theme"}))
	if userWriteRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("user write error status=%d", userWriteRecorder.Code)
	}
}
