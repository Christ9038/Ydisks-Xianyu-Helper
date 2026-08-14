package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/engine"
)

// TestVersionedAccountRoutesPreserveLegacyContracts 验证账号版本化入口复用旧 handler 并保留旧路径。
func TestVersionedAccountRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证版本化账号路由的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// listReq 是读取版本化账号摘要 ID 列表的请求。
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	listReq.AddCookie(sessionCookie)
	// listRecorder 是捕获账号摘要响应的记录器。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("versioned account list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	// accountIDs 是版本化账号摘要 ID 列表。
	var accountIDs []string
	// listDecodeErr 是账号摘要响应反序列化失败的原因。
	if listDecodeErr := json.Unmarshal(listRecorder.Body.Bytes(), &accountIDs); listDecodeErr != nil {
		t.Fatalf("decode versioned account list: %v", listDecodeErr)
	}
	if len(accountIDs) != 1 || accountIDs[0] != "acc1" {
		t.Fatalf("versioned account list=%+v", accountIDs)
	}

	// detailsReq 是读取版本化账号非敏感详情的请求。
	detailsReq := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/details", nil)
	detailsReq.AddCookie(sessionCookie)
	// detailsRecorder 是捕获账号详情响应的记录器。
	detailsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(detailsRecorder, detailsReq)
	if detailsRecorder.Code != http.StatusOK {
		t.Fatalf("versioned account details status=%d body=%s", detailsRecorder.Code, detailsRecorder.Body.String())
	}
	// detailsValue 是版本化账号详情 DTO 集合。
	var detailsValue []cookieSummaryResponse
	// detailsDecodeErr 是账号详情响应反序列化失败的原因。
	if detailsDecodeErr := json.Unmarshal(detailsRecorder.Body.Bytes(), &detailsValue); detailsDecodeErr != nil {
		t.Fatalf("decode versioned account details: %v", detailsDecodeErr)
	}
	if len(detailsValue) != 1 || detailsValue[0].ID != "acc1" || !detailsValue[0].HasCookie {
		t.Fatalf("versioned account details=%+v", detailsValue)
	}
	if strings.Contains(detailsRecorder.Body.String(), `"value"`) || strings.Contains(detailsRecorder.Body.String(), `"password"`) {
		t.Fatalf("versioned account details exposes credential: %s", detailsRecorder.Body.String())
	}

	// runtimeReq 是读取版本化账号运行状态的请求。
	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/runtime-status", nil)
	runtimeReq.AddCookie(sessionCookie)
	// runtimeRecorder 是捕获账号运行状态响应的记录器。
	runtimeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runtimeRecorder, runtimeReq)
	if runtimeRecorder.Code != http.StatusOK {
		t.Fatalf("versioned runtime status=%d body=%s", runtimeRecorder.Code, runtimeRecorder.Body.String())
	}
	// runtimeValue 是版本化账号运行状态映射。
	var runtimeValue map[string]engine.RuntimeStatus
	// runtimeDecodeErr 是运行状态响应反序列化失败的原因。
	if runtimeDecodeErr := json.Unmarshal(runtimeRecorder.Body.Bytes(), &runtimeValue); runtimeDecodeErr != nil {
		t.Fatalf("decode versioned runtime status: %v", runtimeDecodeErr)
	}
	if runtimeValue["acc1"].State != engine.RuntimeError {
		t.Fatalf("versioned runtime status=%+v", runtimeValue)
	}

	// detailReq 是读取版本化单账号详情的请求。
	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/acc1", nil)
	detailReq.AddCookie(sessionCookie)
	// detailRecorder 是捕获单账号详情响应的记录器。
	detailRecorder := httptest.NewRecorder()
	handler.ServeHTTP(detailRecorder, detailReq)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("versioned account detail status=%d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	// detailValue 是版本化单账号详情 DTO。
	var detailValue cookieDetailResponse
	// detailDecodeErr 是单账号详情响应反序列化失败的原因。
	if detailDecodeErr := json.Unmarshal(detailRecorder.Body.Bytes(), &detailValue); detailDecodeErr != nil {
		t.Fatalf("decode versioned account detail: %v", detailDecodeErr)
	}
	if detailValue.ID != "acc1" || !detailValue.HasCookie {
		t.Fatalf("versioned account detail=%+v", detailValue)
	}

	// statusReq 是通过版本化入口停用账号的请求。
	statusReq := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/status", strings.NewReader(`{"enabled":false}`))
	statusReq.AddCookie(sessionCookie)
	// statusRecorder 是捕获账号状态变更响应的记录器。
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("versioned account status=%d body=%s", statusRecorder.Code, statusRecorder.Body.String())
	}
	// statusValue 是账号状态变更具名响应 DTO。
	var statusValue operationResponse
	// statusDecodeErr 是状态变更响应反序列化失败的原因。
	if statusDecodeErr := json.Unmarshal(statusRecorder.Body.Bytes(), &statusValue); statusDecodeErr != nil {
		t.Fatalf("decode versioned account status: %v", statusDecodeErr)
	}
	if !statusValue.Success {
		t.Fatalf("versioned account status=%+v", statusValue)
	}

	// legacyReq 是验证旧账号详情入口仍可用的请求。
	legacyReq := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	legacyReq.AddCookie(sessionCookie)
	// legacyRecorder 是捕获旧账号详情响应的记录器。
	legacyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyRecorder, legacyReq)
	if legacyRecorder.Code != http.StatusOK {
		t.Fatalf("legacy account details status=%d body=%s", legacyRecorder.Code, legacyRecorder.Body.String())
	}

	// restoreReq 是恢复测试账号状态的请求，避免影响同一测试中的后续检查。
	restoreReq := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(`{"enabled":true}`))
	restoreReq.AddCookie(sessionCookie)
	// restoreRecorder 是捕获旧入口恢复响应的记录器。
	restoreRecorder := httptest.NewRecorder()
	handler.ServeHTTP(restoreRecorder, restoreReq)
	if restoreRecorder.Code != http.StatusOK {
		t.Fatalf("restore account status=%d body=%s", restoreRecorder.Code, restoreRecorder.Body.String())
	}
}

// TestVersionedAccountSettingsRoutesPreserveLegacyContracts 验证账号设置、资料和旧路径兼容。
func TestVersionedAccountSettingsRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证版本化账号设置路由的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// settingsReq 是提交版本化聚合账号设置的请求。
	settingsReq := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/settings", strings.NewReader(`{"remark":"版本化备注","pause_duration":10,"auto_confirm":true}`))
	settingsReq.AddCookie(sessionCookie)
	// settingsRecorder 是捕获聚合设置响应的记录器。
	settingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingsRecorder, settingsReq)
	if settingsRecorder.Code != http.StatusOK {
		t.Fatalf("versioned settings status=%d body=%s", settingsRecorder.Code, settingsRecorder.Body.String())
	}
	// settingsValue 是聚合账号设置具名响应 DTO。
	var settingsValue cookieSettingsResponse
	// settingsDecodeErr 是聚合设置响应反序列化失败的原因。
	if settingsDecodeErr := json.Unmarshal(settingsRecorder.Body.Bytes(), &settingsValue); settingsDecodeErr != nil {
		t.Fatalf("decode versioned settings: %v", settingsDecodeErr)
	}
	if !settingsValue.Success {
		t.Fatalf("versioned settings=%+v", settingsValue)
	}

	// remarkReq 是更新版本化账号备注的请求。
	remarkReq := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/remark", strings.NewReader(`{"remark":"新备注"}`))
	remarkReq.AddCookie(sessionCookie)
	// remarkRecorder 是捕获备注响应的记录器。
	remarkRecorder := httptest.NewRecorder()
	handler.ServeHTTP(remarkRecorder, remarkReq)
	if remarkRecorder.Code != http.StatusOK {
		t.Fatalf("versioned remark status=%d body=%s", remarkRecorder.Code, remarkRecorder.Body.String())
	}
	// remarkValue 是备注变更具名响应 DTO。
	var remarkValue operationResponse
	// remarkDecodeErr 是备注响应反序列化失败的原因。
	if remarkDecodeErr := json.Unmarshal(remarkRecorder.Body.Bytes(), &remarkValue); remarkDecodeErr != nil {
		t.Fatalf("decode versioned remark: %v", remarkDecodeErr)
	}
	if !remarkValue.Success {
		t.Fatalf("versioned remark=%+v", remarkValue)
	}

	// autoConfirmReq 是更新版本化自动确认设置的请求。
	autoConfirmReq := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/auto-confirm", strings.NewReader(`{"auto_confirm":false}`))
	autoConfirmReq.AddCookie(sessionCookie)
	// autoConfirmRecorder 是捕获自动确认响应的记录器。
	autoConfirmRecorder := httptest.NewRecorder()
	handler.ServeHTTP(autoConfirmRecorder, autoConfirmReq)
	if autoConfirmRecorder.Code != http.StatusOK {
		t.Fatalf("versioned auto-confirm status=%d body=%s", autoConfirmRecorder.Code, autoConfirmRecorder.Body.String())
	}

	// pauseReq 是更新版本化暂停时长的请求。
	pauseReq := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/pause-duration", strings.NewReader(`{"pause_duration":15}`))
	pauseReq.AddCookie(sessionCookie)
	// pauseRecorder 是捕获暂停时长响应的记录器。
	pauseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pauseRecorder, pauseReq)
	if pauseRecorder.Code != http.StatusOK {
		t.Fatalf("versioned pause status=%d body=%s", pauseRecorder.Code, pauseRecorder.Body.String())
	}
	// pauseValue 是暂停时长具名响应 DTO。
	var pauseValue cookieSettingsResponse
	// pauseDecodeErr 是暂停响应反序列化失败的原因。
	if pauseDecodeErr := json.Unmarshal(pauseRecorder.Body.Bytes(), &pauseValue); pauseDecodeErr != nil {
		t.Fatalf("decode versioned pause: %v", pauseDecodeErr)
	}
	if !pauseValue.Success {
		t.Fatalf("versioned pause=%+v", pauseValue)
	}

	// autoConfirmGetReq 是读取版本化自动确认设置的请求。
	autoConfirmGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/acc1/auto-confirm", nil)
	autoConfirmGetReq.AddCookie(sessionCookie)
	// autoConfirmGetRecorder 是捕获自动确认查询响应的记录器。
	autoConfirmGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(autoConfirmGetRecorder, autoConfirmGetReq)
	if autoConfirmGetRecorder.Code != http.StatusOK {
		t.Fatalf("versioned auto-confirm get status=%d body=%s", autoConfirmGetRecorder.Code, autoConfirmGetRecorder.Body.String())
	}
	// autoConfirmValue 是自动确认查询具名响应 DTO。
	var autoConfirmValue autoConfirmResponse
	// autoConfirmDecodeErr 是自动确认查询响应反序列化失败的原因。
	if autoConfirmDecodeErr := json.Unmarshal(autoConfirmGetRecorder.Body.Bytes(), &autoConfirmValue); autoConfirmDecodeErr != nil {
		t.Fatalf("decode versioned auto-confirm: %v", autoConfirmDecodeErr)
	}
	if autoConfirmValue.AutoConfirm {
		t.Fatalf("versioned auto-confirm=%+v", autoConfirmValue)
	}

	// pauseGetReq 是读取版本化暂停时长的请求。
	pauseGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/acc1/pause-duration", nil)
	pauseGetReq.AddCookie(sessionCookie)
	// pauseGetRecorder 是捕获暂停查询响应的记录器。
	pauseGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pauseGetRecorder, pauseGetReq)
	if pauseGetRecorder.Code != http.StatusOK {
		t.Fatalf("versioned pause get status=%d body=%s", pauseGetRecorder.Code, pauseGetRecorder.Body.String())
	}
	// pauseGetValue 是暂停查询具名响应 DTO。
	var pauseGetValue pauseDurationResponse
	// pauseGetDecodeErr 是暂停查询响应反序列化失败的原因。
	if pauseGetDecodeErr := json.Unmarshal(pauseGetRecorder.Body.Bytes(), &pauseGetValue); pauseGetDecodeErr != nil {
		t.Fatalf("decode versioned pause get: %v", pauseGetDecodeErr)
	}
	if pauseGetValue.PauseDuration != 15 {
		t.Fatalf("versioned pause get=%+v", pauseGetValue)
	}

	// profileReq 是刷新版本化账号资料的请求。
	profileReq := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/acc1/refresh-profile", nil)
	profileReq.AddCookie(sessionCookie)
	// profileRecorder 是捕获资料刷新响应的记录器。
	profileRecorder := httptest.NewRecorder()
	handler.ServeHTTP(profileRecorder, profileReq)
	if profileRecorder.Code != http.StatusOK {
		t.Fatalf("versioned profile status=%d body=%s", profileRecorder.Code, profileRecorder.Body.String())
	}
	// profileValue 是资料刷新具名响应 DTO。
	var profileValue cookieProfileResponse
	// profileDecodeErr 是资料刷新响应反序列化失败的原因。
	if profileDecodeErr := json.Unmarshal(profileRecorder.Body.Bytes(), &profileValue); profileDecodeErr != nil {
		t.Fatalf("decode versioned profile: %v", profileDecodeErr)
	}
	if !profileValue.Success || profileValue.ID != "acc1" {
		t.Fatalf("versioned profile=%+v", profileValue)
	}

	// legacySettingsReq 是验证旧账号设置入口仍可用的请求。
	legacySettingsReq := httptest.NewRequest(http.MethodPut, "/cookies/acc1/settings", strings.NewReader(`{"remark":"旧路径备注"}`))
	legacySettingsReq.AddCookie(sessionCookie)
	// legacySettingsRecorder 是捕获旧设置响应的记录器。
	legacySettingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacySettingsRecorder, legacySettingsReq)
	if legacySettingsRecorder.Code != http.StatusOK {
		t.Fatalf("legacy settings status=%d body=%s", legacySettingsRecorder.Code, legacySettingsRecorder.Body.String())
	}
}
