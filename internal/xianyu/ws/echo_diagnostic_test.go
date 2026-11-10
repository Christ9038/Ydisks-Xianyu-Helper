package ws

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
)

// echoDiagnosticSentContent 是诊断测试使用的发送正文；字段顺序固定，便于构造字节不同但语义相同的回显。
var echoDiagnosticSentContent = []byte(`{"contentType":1,"text":{"text":"你好"}}`)

// echoDiagnosticResponse 构造一个携带指定回显正文的 code=200 发送响应；echoed 是将被 Base64 编码写入 custom.data 的回显字节。
func echoDiagnosticResponse(echoed []byte) map[string]any {
	return map[string]any{"body": map[string]any{
		"messageId": "pnm-1",
		"extension": map[string]any{"senderUserId": "me@goofish"},
		"content":   map[string]any{"custom": map[string]any{"data": base64.StdEncoding.EncodeToString(echoed)}},
	}}
}

// TestParseOutgoingEchoResponseReasons 验证每个严格核验失败分支返回稳定原因码，且通过时原因为空。
func TestParseOutgoingEchoResponseReasons(t *testing.T) {
	// reordered 是与发送正文语义相同、但字段顺序不同的回显字节。
	reordered := []byte(`{"text":{"text":"你好"},"contentType":1}`)
	// cases 列出被测响应及期望的原因码与诊断字段。
	cases := []struct {
		// name 是用例名称。
		name string
		// response 是被测平台响应。
		response map[string]any
		// reason 是期望的稳定原因码。
		reason string
		// semanticEqual 是期望的语义相等标志。
		semanticEqual bool
	}{
		{name: "ok", response: echoDiagnosticResponse(echoDiagnosticSentContent), reason: ""},
		{name: "missing-body", response: map[string]any{"code": 200}, reason: "missing_body"},
		{name: "missing-message-id", response: map[string]any{"body": map[string]any{}}, reason: "missing_message_id"},
		{name: "missing-extension", response: map[string]any{"body": map[string]any{"messageId": "p"}}, reason: "missing_extension"},
		{name: "sender-mismatch", response: map[string]any{"body": map[string]any{"messageId": "p", "extension": map[string]any{"senderUserId": "other"}}}, reason: "sender_mismatch"},
		{name: "missing-content", response: map[string]any{"body": map[string]any{"messageId": "p", "extension": map[string]any{"senderUserId": "me"}}}, reason: "missing_content"},
		{name: "missing-custom", response: map[string]any{"body": map[string]any{"messageId": "p", "extension": map[string]any{"senderUserId": "me"}, "content": map[string]any{}}}, reason: "missing_custom"},
		{name: "missing-custom-data", response: map[string]any{"body": map[string]any{"messageId": "p", "extension": map[string]any{"senderUserId": "me"}, "content": map[string]any{"custom": map[string]any{}}}}, reason: "missing_custom_data"},
		{name: "bad-base64", response: map[string]any{"body": map[string]any{"messageId": "p", "extension": map[string]any{"senderUserId": "me"}, "content": map[string]any{"custom": map[string]any{"data": "***"}}}}, reason: "custom_data_not_base64"},
		{name: "reordered", response: echoDiagnosticResponse(reordered), reason: "content_bytes_differ", semanticEqual: true},
		{name: "different", response: echoDiagnosticResponse([]byte(`{"contentType":1,"text":{"text":"别的"}}`)), reason: "content_bytes_differ"},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		// echo、diagnostic 是被测解析结果及诊断摘要。
		echo, diagnostic := parseOutgoingEchoResponse(tc.response, "rid", "me", "c@goofish", "peer@goofish", echoDiagnosticSentContent)
		if diagnostic.reason != tc.reason || diagnostic.semanticEqual != tc.semanticEqual {
			t.Fatalf("%s: diagnostic=%+v", tc.name, diagnostic)
		}
		if tc.reason == "" && echo.MessageKey != "pnm-1" {
			t.Fatalf("%s: 核验通过时应返回回显 echo=%+v", tc.name, echo)
		}
	}
	// unsupported 是可逐字节匹配但不属于自动化确认支持类型的正文。
	unsupported := []byte(`{"contentType":9}`)
	// diagnostic 是不支持类型正文的诊断摘要。
	if _, diagnostic := parseOutgoingEchoResponse(echoDiagnosticResponse(unsupported), "r", "me", "c", "p", unsupported); diagnostic.reason != "unsupported_content_type" || diagnostic.echoedBytes != len(unsupported) {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	// diagnostic 是缺少 messageId 时的摘要，body 顶层字段名必须有序输出。
	if _, diagnostic := parseOutgoingEchoResponse(map[string]any{"body": map[string]any{"z": 1, "a": 2}}, "r", "me", "c", "p", nil); strings.Join(diagnostic.bodyKeys, ",") != "a,z" || diagnostic.echoedBytes != -1 {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	if jsonSemanticallyEqual([]byte("not json"), []byte("{}")) {
		t.Fatal("非法 JSON 不应判为语义相同")
	}
}

// TestObserveOutgoingResponseLogsDiagnosticWithoutContent 验证核验失败时输出诊断日志、不唤醒观察器且日志不含消息正文；核验通过时不输出诊断。
func TestObserveOutgoingResponseLogsDiagnosticWithoutContent(t *testing.T) {
	// buffer 收集诊断日志输出。
	var buffer bytes.Buffer
	// observed 统计观察器被唤醒的次数。
	observed := 0
	// conn 是只配置观察器与日志器的最小连接。
	conn := &Conn{cfg: Config{ObserveOutgoing: func(OutgoingEcho) { observed++ }}, logger: slog.New(slog.NewTextHandler(&buffer, nil))}
	// reordered 是字节不同但语义相同的回显正文。
	reordered := []byte(`{"text":{"text":"你好"},"contentType":1}`)
	conn.observeOutgoingResponse(echoDiagnosticResponse(reordered), "mid-1", "me", "c@goofish", "peer@goofish", echoDiagnosticSentContent)
	// output 是第一次调用后的日志文本。
	output := buffer.String()
	if observed != 0 || !strings.Contains(output, "reason=content_bytes_differ") || !strings.Contains(output, "request_mid=mid-1") || !strings.Contains(output, "semantic_equal=true") {
		t.Fatalf("observed=%d output=%s", observed, output)
	}
	if strings.Contains(output, "你好") {
		t.Fatal("诊断日志不得包含消息正文")
	}
	buffer.Reset()
	conn.observeOutgoingResponse(echoDiagnosticResponse(echoDiagnosticSentContent), "mid-2", "me", "c@goofish", "peer@goofish", echoDiagnosticSentContent)
	if observed != 1 || buffer.Len() != 0 {
		t.Fatalf("核验通过应唤醒观察器且不输出诊断 observed=%d output=%s", observed, buffer.String())
	}
	// noLogger 是未配置日志器的连接，诊断输出必须安全跳过。
	noLogger := &Conn{cfg: Config{ObserveOutgoing: func(OutgoingEcho) {}}}
	noLogger.observeOutgoingResponse(map[string]any{}, "", "", "", "", nil)
}
