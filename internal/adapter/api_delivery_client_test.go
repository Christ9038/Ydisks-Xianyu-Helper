package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/automation"
)

// TestAPICardTesterReturnsResponseDiagnostics 验证测试请求返回状态码、JSON 顶层字段和响应路径提取结果。
func TestAPICardTesterReturnsResponseDiagnostics(t *testing.T) {
	// server 是返回固定 JSON 结构的本地 API 测试端点。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"code":"TEST-CODE"},"message":"ok"}`))
	}))
	defer server.Close()
	// client 是不访问数据库的临时 API 测试适配器。
	client := &apiDeliveryClient{}
	// result、err 保存测试请求返回的诊断和错误。
	result, err := client.Test(context.Background(), cardsapp.APIRequestTestInput{Config: fmt.Sprintf(`{"url":%q,"method":"GET","timeout_seconds":10,"response_path":"data.code"}`, server.URL)})
	if err != nil {
		t.Fatalf("API 测试请求失败: %v", err)
	}
	if result.Status != "success" || result.StatusCode != http.StatusOK || result.ExtractedValue != "TEST-CODE" {
		t.Fatalf("API 测试诊断错误: %+v", result)
	}
	if strings.Join(result.ResponseFields, ",") != "data,message" {
		t.Fatalf("API 测试响应字段错误: %v", result.ResponseFields)
	}
}

// TestAPICardTesterPreservesRemoteFailureDiagnostics 验证远端非 2xx 仍作为完成结果返回状态码和限长预览。
func TestAPICardTesterPreservesRemoteFailureDiagnostics(t *testing.T) {
	// server 是返回明确失败状态的本地 API 端点。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("remote failure"))
	}))
	defer server.Close()
	// client 是不访问数据库的临时 API 测试适配器。
	client := &apiDeliveryClient{}
	// result、err 保存远端失败诊断和错误。
	result, err := client.Test(context.Background(), cardsapp.APIRequestTestInput{Config: fmt.Sprintf(`{"url":%q,"method":"GET","timeout_seconds":10}`, server.URL)})
	if err != nil {
		t.Fatalf("远端失败不应变为本地错误: %v", err)
	}
	if result.Status != "failed" || result.StatusCode != http.StatusBadGateway || result.ResponsePreview != "remote failure" {
		t.Fatalf("远端失败诊断错误: %+v", result)
	}
}

// TestAPIDeliveryClientGETAndTemplateReplacement 验证 GET 查询参数、递归变量替换和响应路径提取。
func TestAPIDeliveryClientGETAndTemplateReplacement(t *testing.T) {
	// receivedQuery、receivedHeader 保存服务端观察到的动态请求字段。
	var receivedQuery, receivedHeader string
	// server 是接收 GET 模板请求的本地测试服务。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedQuery = request.URL.Query().Get("order")
		receivedHeader = request.Header.Get("X-Unit")
		_, _ = writer.Write([]byte(`{"data":{"cards":[{"code":"CODE-1"}]}}`))
	}))
	defer server.Close()
	// client 是使用测试 HTTP 服务的普通 API 发货客户端。
	client := &apiDeliveryClient{}
	// result、err 保存单件 API 请求的提取结果及错误。
	result, err := client.Fetch(context.Background(), automation.APICardRequest{
		Config:     fmt.Sprintf(`{"url":%q,"method":"GET","timeout_seconds":10,"headers":{"X-Unit":"{delivery_unit_index}"},"params":{"order":"{order_id}","nested":{"buyer":"{buyer_id}"}},"response_path":"data.cards[0].code"}`, server.URL),
		TriggerKey: "trigger-1", ActionID: 2, CardID: 3, UnitIndex: 1, TotalUnits: 2,
		AccountID: "account", OrderID: "order-7", BuyerID: "buyer-8",
	})
	if err != nil || result.Content != "CODE-1" {
		t.Fatalf("GET API 发货结果错误 result=%+v err=%v", result, err)
	}
	if receivedQuery != "order-7" || receivedHeader != "1" {
		t.Fatalf("动态字段未替换 query=%q header=%q", receivedQuery, receivedHeader)
	}
}

// TestAPIDeliveryClientPOSTRetryAndStableIdempotencyKey 验证 POST JSON、三次重试和幂等键稳定复用。
func TestAPIDeliveryClientPOSTRetryAndStableIdempotencyKey(t *testing.T) {
	// oldGaps 保存全局退避配置，避免测试等待真实秒数。
	oldGaps := apiDeliveryRetryGaps
	apiDeliveryRetryGaps = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { apiDeliveryRetryGaps = oldGaps }()
	// calls 记录服务端实际收到的请求次数。
	var calls int32
	// keys 保存每次请求观察到的幂等键。
	var keys []string
	// server 是返回暂时性失败并记录幂等键的本地测试服务。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&calls, 1)
		// body 保存 POST 参数对象。
		var body map[string]any
		// err 表示测试服务解析 POST body 的错误。
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("POST JSON 解码失败: %v", err)
		}
		keys = append(keys, fmt.Sprint(body["key"]))
		if calls < 3 {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = writer.Write([]byte(`{"content":"POST-CODE"}`))
	}))
	defer server.Close()
	// client 是使用测试 HTTP 服务的普通 API 发货客户端。
	client := &apiDeliveryClient{}
	// result、err 保存带重试的 API 请求结果。
	result, err := client.Fetch(context.Background(), automation.APICardRequest{
		Config:     fmt.Sprintf(`{"url":%q,"method":"POST","timeout_seconds":10,"params":{"key":"{idempotency_key}","unit":"{delivery_unit_index}"},"retry_enabled":true}`, server.URL),
		TriggerKey: "trigger-2", ActionID: 4, CardID: 5, UnitIndex: 2, TotalUnits: 2,
	})
	if err != nil || result.Content != "POST-CODE" {
		t.Fatalf("POST API 发货结果错误 result=%+v err=%v", result, err)
	}
	if atomic.LoadInt32(&calls) != 3 || len(keys) != 3 || keys[0] == "" || keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("重试次数或幂等键错误 calls=%d keys=%v", atomic.LoadInt32(&calls), keys)
	}
}

// TestAPIDeliveryClientFormBodyAndContentType 验证非 JSON Content-Type 使用键值表单正文，而不是上传文件格式。
func TestAPIDeliveryClientFormBodyAndContentType(t *testing.T) {
	// receivedContentType、receivedBody 保存测试服务观察到的正文元数据和内容。
	var receivedContentType, receivedBody string
	// server 是接收表单正文并返回固定卡密的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedContentType = request.Header.Get("Content-Type")
		// body、readErr 保存服务端读取到的正文和读取错误。
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Errorf("读取表单正文失败: %v", readErr)
		}
		receivedBody = string(body)
		_, _ = writer.Write([]byte(`{"content":"FORM-CODE"}`))
	}))
	defer server.Close()
	// client 是使用本地测试服务的 API 发货客户端。
	client := &apiDeliveryClient{}
	// result、err 保存表单请求的发货结果及错误。
	result, err := client.Fetch(context.Background(), automation.APICardRequest{
		Config:  fmt.Sprintf(`{"url":%q,"method":"POST","timeout_seconds":10,"content_type":"application/x-www-form-urlencoded","body":{"order_id":"{order_id}","quantity":"{quantity}"}}`, server.URL),
		OrderID: "order-form", Quantity: "2",
	})
	if err != nil || result.Content != "FORM-CODE" {
		t.Fatalf("表单 API 发货结果错误 result=%+v err=%v", result, err)
	}
	if receivedContentType != "application/x-www-form-urlencoded" || receivedBody != "order_id=order-form&quantity=2" {
		t.Fatalf("表单正文编码错误 content_type=%q body=%q", receivedContentType, receivedBody)
	}
}

// TestEncodeAPIDeliveryBodyTextAndXML 验证非表单 Content-Type 的键值正文编码和转义边界。
func TestEncodeAPIDeliveryBodyTextAndXML(t *testing.T) {
	// values 保存测试使用的普通字段和包含 XML 特殊字符的字段。
	values := map[string]any{"z": "last", "a": "<&"}
	// textBody、textErr 保存纯文本正文和编码错误。
	textBody, textErr := encodeAPIDeliveryBody(values, "text/plain")
	if textErr != nil || string(textBody) != "a=<&\nz=last" {
		t.Fatalf("纯文本正文错误 body=%q err=%v", textBody, textErr)
	}
	// xmlBody、xmlErr 保存 XML 正文和编码错误。
	xmlBody, xmlErr := encodeAPIDeliveryBody(values, "application/xml")
	if xmlErr != nil || string(xmlBody) != "<body><a>&lt;&amp;</a><z>last</z></body>" {
		t.Fatalf("XML 正文错误 body=%q err=%v", xmlBody, xmlErr)
	}
}

// TestAPIDeliveryClientResponseAndSizeErrors 验证文本响应、非 2xx 和超大响应不会泄漏正文且不误提取。
func TestAPIDeliveryClientResponseAndSizeErrors(t *testing.T) {
	// cases 保存不同响应场景及预期错误判定。
	cases := []struct {
		// name 是场景名称。
		name string
		// status 是服务端返回状态码。
		status int
		// body 是服务端返回的响应体。
		body string
		// wantContent 是预期的成功文本。
		wantContent string
		// wantError 表示是否应返回错误。
		wantError bool
	}{
		{name: "plain", status: http.StatusOK, body: "PLAIN-CODE", wantContent: "PLAIN-CODE"},
		{name: "bad-request", status: http.StatusBadRequest, body: "secret-error", wantError: true},
		{name: "too-large", status: http.StatusOK, body: strings.Repeat("x", int(apiDeliveryResponseLimit)+1), wantError: true},
	}
	// testCase 保存当前响应场景及其预期行为。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// server 是当前场景使用的本地响应服务器。
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(testCase.body))
			}))
			defer server.Close()
			// client 是当前场景使用的 API 发货客户端。
			client := &apiDeliveryClient{}
			// result、err 保存当前场景的执行结果。
			result, err := client.Fetch(context.Background(), automation.APICardRequest{
				Config: fmt.Sprintf(`{"url":%q,"method":"GET","timeout_seconds":10}`, server.URL),
			})
			if (err != nil) != testCase.wantError || result.Content != testCase.wantContent {
				t.Fatalf("响应判定错误 result=%+v err=%v", result, err)
			}
			if testCase.name == "bad-request" && result.Dispatched {
				t.Fatal("明确 4xx 拒绝不应被标记为结果未知")
			}
		})
	}
}

// TestAPIDeliveryClientPreservesJSONNumbers 验证大整数卡密不会因 JSON 浮点转换发生舍入。
func TestAPIDeliveryClientPreservesJSONNumbers(t *testing.T) {
	// server 是返回超出 float64 精确整数范围的 JSON 数字端点。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"data":{"card":{"code":9007199254740993}}}`))
	}))
	t.Cleanup(server.Close)
	// client 是本测试使用的零值 API 发货客户端。
	client := &apiDeliveryClient{}
	// result、err 保存 JSON 数字提取结果及错误。
	result, err := client.Fetch(context.Background(), automation.APICardRequest{
		Config: fmt.Sprintf(`{"url":%q,"method":"GET","response_path":"data.card.code"}`, server.URL),
	})
	if err != nil || result.Content != "9007199254740993" {
		t.Fatalf("JSON 大整数被错误转换 result=%+v err=%v", result, err)
	}
}
