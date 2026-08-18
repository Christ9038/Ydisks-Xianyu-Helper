package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestLegacyAPITelemetryAddsDeprecationHeaders 验证历史 API 保留行为会提供可供客户端迁移的弃用元数据。
func TestLegacyAPITelemetryAddsDeprecationHeaders(t *testing.T) {
	// server 保存仅用于中间件测试的 HTTP 服务实例；该测试不依赖业务装配。
	server := &Server{}
	// next 保存模拟历史 handler 的下游响应实现。
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	// handler 保存附加旧 API 观测元数据后的请求处理链。
	handler := server.legacyAPITelemetry(next)
	// legacyRequest 保存命中历史订单入口的测试请求。
	legacyRequest := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	// legacyRecorder 保存历史入口的 HTTP 响应与弃用头。
	legacyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyRecorder, legacyRequest)
	// sunsetValue 是历史 API 响应写入的标准退场时间字符串。
	sunsetValue := legacyRecorder.Header().Get("Sunset")
	// sunsetTime、sunsetErr 分别是标准 HTTP-date 解析结果及其错误。
	sunsetTime, sunsetErr := http.ParseTime(sunsetValue)
	if legacyRecorder.Code != http.StatusNoContent || legacyRecorder.Header().Get("Deprecation") != "true" || sunsetErr != nil || sunsetTime.IsZero() || legacyRecorder.Header().Get("Link") != legacyAPISuccessorLink {
		t.Fatalf("历史 API 遥测头错误: status=%d header=%v", legacyRecorder.Code, legacyRecorder.Header())
	}
	// expectedSunset 是兼容矩阵当前约定的历史 API 最晚保留时间。
	expectedSunset, expectedSunsetErr := time.Parse(time.RFC1123, legacyAPISunsetDate)
	if expectedSunsetErr != nil || !sunsetTime.Equal(expectedSunset) {
		t.Fatalf("Sunset=%q，期望标准时间 %q", sunsetValue, legacyAPISunsetDate)
	}
}

// TestLegacyAPITelemetryLeavesVersionedAndPublicPathsUntouched 验证版本化与公开端点不会收到错误的历史 API 弃用信号。
func TestLegacyAPITelemetryDistinguishesVersionedAndSPARoutes(t *testing.T) {
	// server 保存仅用于中间件测试的 HTTP 服务实例。
	server := &Server{}
	// next 保存返回成功状态的固定下游处理器。
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) })
	// handler 保存当前待验证的中间件链。
	handler := server.legacyAPITelemetry(next)
	// requests 保存不应被判定为历史 API 的版本化、健康与 SPA 客户端路由请求。
	requests := []struct {
		// method 是当前测试请求的 HTTP 方法。
		method string
		// path 是当前测试请求的路径。
		path string
	}{
		{method: http.MethodGet, path: "/api/v1/orders"},
		{method: http.MethodGet, path: "/health"},
		{method: http.MethodGet, path: "/version"},
		{method: http.MethodGet, path: "/login"},
	}
	// requestCase 表示当前待验证的不弃用请求方法与路径组合。
	for _, requestCase := range requests {
		// requestCase 保存闭包独占的测试输入，避免循环变量复用。
		requestCase := requestCase
		t.Run(requestCase.method+" "+requestCase.path, func(t *testing.T) {
			// request 保存当前非历史 API 测试请求。
			request := httptest.NewRequest(requestCase.method, requestCase.path, nil)
			// recorder 保存当前响应头，确保中间件未写入弃用信息。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Header().Get("Deprecation") != "" || recorder.Header().Get("Sunset") != "" || recorder.Header().Get("Link") != "" {
				t.Fatalf("非历史路径不应有弃用头: %v", recorder.Header())
			}
		})
	}
}

// TestLegacyAPIRequestCoverage 验证所有保留历史入口类型都会进入遥测，认证入口不会误伤 SPA GET。
func TestLegacyAPIRequestCoverage(t *testing.T) {
	// cases 保存各类历史业务与认证请求的遥测预期。
	cases := []struct {
		// name 是子测试名称，包含方法与路径便于失败定位。
		name string
		// method 是当前测试请求的方法。
		method string
		// path 是当前测试请求的路径。
		path string
		// want 表示当前请求是否应产生历史 API 遥测。
		want bool
	}{
		{name: "legacy api", method: http.MethodGet, path: "/api/orders", want: true},
		{name: "legacy dashboard", method: http.MethodGet, path: "/dashboard/stats", want: true},
		{name: "legacy analytics", method: http.MethodGet, path: "/analytics/orders", want: true},
		{name: "legacy item replies", method: http.MethodGet, path: "/itemReplays", want: true},
		{name: "legacy login", method: http.MethodPost, path: "/login", want: true},
		{name: "legacy verify", method: http.MethodGet, path: "/verify", want: true},
		{name: "spa login", method: http.MethodGet, path: "/login", want: false},
	}
	// testCase 表示当前待验证的历史入口识别用例。
	for _, testCase := range cases {
		// testCase 保存闭包独占的用例值，避免循环变量复用。
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			// got 是当前请求经方法和路径判断后的遥测结果。
			got := isLegacyAPIRequest(testCase.method, testCase.path)
			if got != testCase.want {
				t.Fatalf("isLegacyAPIRequest(%q, %q)=%t, want %t", testCase.method, testCase.path, got, testCase.want)
			}
		})
	}
}
