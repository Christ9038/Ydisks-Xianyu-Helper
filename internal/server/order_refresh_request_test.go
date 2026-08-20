package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseOrderRefreshRequestSupportsJSONAndMultipart 验证新版 JSON 和旧版 multipart 进入同一个具名订单刷新 DTO。
func TestParseOrderRefreshRequestSupportsJSONAndMultipart(t *testing.T) {
	// jsonRequest 是新版前端发送的 JSON 筛选请求。
	jsonRequest := httptest.NewRequest(http.MethodPost, "/api/v1/orders/refresh", strings.NewReader(`{"cookie_id":"acc-json","status":"pending_ship"}`))
	jsonRequest.Header.Set("Content-Type", "application/json")
	// jsonRecorder 为 MaxBytesReader 提供错误写入目标，不参与正常 JSON 响应。
	jsonRecorder := httptest.NewRecorder()
	// jsonDTO、jsonErr 保存 JSON 解析后的统一 DTO 和可能的格式错误。
	jsonDTO, jsonErr := parseOrderRefreshRequest(jsonRecorder, jsonRequest)
	if jsonErr != nil {
		t.Fatalf("parse JSON order refresh request: %v", jsonErr)
	}
	if jsonDTO.CookieID != "acc-json" || jsonDTO.Status != "pending_ship" {
		t.Fatalf("JSON DTO=%+v", jsonDTO)
	}

	// multipartBody 保存旧客户端 multipart 请求体，writer 负责生成真实 boundary 和字段编码。
	var multipartBody bytes.Buffer
	// multipartWriter 负责把旧客户端筛选字段写入 multipart 流。
	multipartWriter := multipart.NewWriter(&multipartBody)
	// writeErr 保存写入 cookie_id 字段到 multipart 测试流的失败原因。
	if writeErr := multipartWriter.WriteField("cookie_id", "acc-multipart"); writeErr != nil {
		t.Fatalf("write multipart cookie_id: %v", writeErr)
	}
	// writeErr 保存写入状态字段到 multipart 测试流的失败原因。
	if writeErr := multipartWriter.WriteField("status", "shipped"); writeErr != nil {
		t.Fatalf("write multipart status: %v", writeErr)
	}
	// closeErr 保存结束 multipart 测试流和写出最终 boundary 时的失败原因。
	if closeErr := multipartWriter.Close(); closeErr != nil {
		t.Fatalf("close multipart request: %v", closeErr)
	}
	// multipartRequest 是兼容客户端实际发送的表单请求。
	multipartRequest := httptest.NewRequest(http.MethodPost, "/api/v1/orders/refresh", &multipartBody)
	multipartRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	// multipartRecorder 为 multipart 总请求大小限制提供错误写入目标。
	multipartRecorder := httptest.NewRecorder()
	// multipartDTO、multipartErr 保存旧表单解析后的统一 DTO 和可能的格式错误。
	multipartDTO, multipartErr := parseOrderRefreshRequest(multipartRecorder, multipartRequest)
	if multipartErr != nil {
		t.Fatalf("parse multipart order refresh request: %v", multipartErr)
	}
	if multipartDTO.CookieID != "acc-multipart" || multipartDTO.Status != "shipped" {
		t.Fatalf("multipart DTO=%+v", multipartDTO)
	}
}

// TestOrderRefreshRejectsMalformedInputBeforeCreatingJob 验证损坏载荷不会被解释为空筛选，更不会创建全账号刷新任务。
func TestOrderRefreshRejectsMalformedInputBeforeCreatingJob(t *testing.T) {
	// srv、store、cleanup 提供完整认证路由和可检查后台任务数量的测试数据库。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试用的完整 HTTP 路由树。
	handler := srv.Router()
	// sessionCookie 是管理员认证通过后附加到版本化请求的会话 Cookie。
	sessionCookie := loginHelper(t, handler)
	// malformedRequest 是带有截断 JSON 的刷新请求，过去会因 FormValue 失败而退化成全账号刷新。
	malformedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/orders/refresh", strings.NewReader(`{"cookie_id":`))
	malformedRequest.Header.Set("Content-Type", "application/json")
	malformedRequest.AddCookie(sessionCookie)
	// malformedRecorder 保存接口拒绝损坏 JSON 的 HTTP 响应。
	malformedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(malformedRecorder, malformedRequest)
	if malformedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status=%d body=%s", malformedRecorder.Code, malformedRecorder.Body.String())
	}
	// jobCount 是损坏请求之后数据库中已创建的刷新任务数量，必须保持为零。
	var jobCount int
	// queryErr 保存读取后台刷新任务总数时的数据库错误。
	if queryErr := store.DB.QueryRow(`SELECT COUNT(*) FROM order_refresh_jobs`).Scan(&jobCount); queryErr != nil {
		t.Fatalf("count order refresh jobs: %v", queryErr)
	}
	if jobCount != 0 {
		t.Fatalf("malformed request unexpectedly created %d refresh jobs", jobCount)
	}

	// brokenMultipartRequest 是声明 multipart 但缺少实际 boundary 的请求，必须和 JSON 格式错误一样拒绝。
	brokenMultipartRequest := httptest.NewRequest(http.MethodPost, "/api/v1/orders/refresh", strings.NewReader("--missing\r\n"))
	brokenMultipartRequest.Header.Set("Content-Type", "multipart/form-data; boundary=expected")
	brokenMultipartRequest.AddCookie(sessionCookie)
	// brokenMultipartRecorder 保存损坏 boundary 的 HTTP 结果。
	brokenMultipartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(brokenMultipartRecorder, brokenMultipartRequest)
	if brokenMultipartRecorder.Code != http.StatusBadRequest {
		t.Fatalf("broken multipart status=%d body=%s", brokenMultipartRecorder.Code, brokenMultipartRecorder.Body.String())
	}
}
