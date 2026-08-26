package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestWSPureHelpersCoversCodesAndUserAgents 覆盖 WebSocket 响应码及官方 UA 解析分支。
func TestWSPureHelpersCoversCodesAndUserAgents(t *testing.T) {
	// codeCases 保存不同协议响应码表示形式及其解析预期。
	codeCases := []struct {
		// value 是协议响应码原始值。
		value any
		// want 是解析后的数字响应码。
		want int
		// wantOK 表示原始值是否可解析。
		wantOK bool
	}{
		{value: float64(200), want: 200, wantOK: true},
		{value: 201, want: 201, wantOK: true},
		{value: json.Number("202"), want: 202, wantOK: true},
		{value: json.Number("bad"), wantOK: false},
		{value: " 203 ", want: 203, wantOK: true},
		{value: "bad", wantOK: false},
		{value: nil, wantOK: false},
	}
	// codeCase 保存当前响应码解析样本。
	for _, codeCase := range codeCases {
		// got、gotOK 保存当前样本解析结果。
		got, gotOK := responseCode(codeCase.value)
		if got != codeCase.want || gotOK != codeCase.wantOK {
			t.Fatalf("responseCode(%v)=(%d,%v), want=(%d,%v)", codeCase.value, got, gotOK, codeCase.want, codeCase.wantOK)
		}
	}
	// userAgents 保存需要识别的操作系统和浏览器组合。
	userAgents := []string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Edg/120.0 Chrome/120.0",
		"Mozilla/5.0 (X11; Linux x86_64) HeadlessChrome/120.0",
		"Mozilla/5.0 (Linux; Android 14) Firefox/120.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Version/17.0 Safari/605.1",
		"unknown-agent",
	}
	// userAgent 表示当前待解析的浏览器标识。
	for _, userAgent := range userAgents {
		if OfficialRegistrationUA(userAgent) == "" {
			t.Fatalf("UA should produce registration value: %q", userAgent)
		}
	}
	if OfficialRegistrationUA(" ") != "" {
		t.Fatal("blank UA should remain blank")
	}
}

// TestWSDecodeAndSyncHelpersCoversSuccessAndFailure 覆盖同步数据 JSON 解码和确认请求分支。
func TestWSDecodeAndSyncHelpersCoversSuccessAndFailure(t *testing.T) {
	// encodedJSON 保存 base64 编码的合法同步 JSON。
	encodedJSON := base64.StdEncoding.EncodeToString([]byte(`{"kind":"sync"}`))
	// decoded、decodeErr 保存同步 JSON 解码结果。
	decoded, decodeErr := decodeSyncData(encodedJSON)
	if decodeErr != nil || decoded["kind"] != "sync" {
		t.Fatalf("decoded=%v err=%v", decoded, decodeErr)
	}
	// payload、payloadOK 保存同步推送包提取结果。
	payload, payloadOK := extractSyncPayload(map[string]any{"body": map[string]any{"syncPushPackage": map[string]any{"data": []any{map[string]any{"data": "payload"}}}}})
	if !payloadOK || payload != "payload" {
		t.Fatalf("payload=%q ok=%v", payload, payloadOK)
	}
	// invalidErr 保存非法同步数据的解码错误。
	if _, invalidErr := decodeSyncData("not-base64"); invalidErr == nil {
		t.Fatal("invalid encrypted payload should fail")
	}
	// invalidJSONPayload 保存能通过 base64 解码但不是 JSON 的同步数据。
	invalidJSONPayload := base64.StdEncoding.EncodeToString([]byte("not-json"))
	// invalidJSONErr 保存该数据回退到协议解密后的错误结果。
	if _, invalidJSONErr := decodeSyncData(invalidJSONPayload); invalidJSONErr == nil {
		t.Fatal("非法 JSON 同步数据应失败")
	}
	// invalidSyncConnection 保存需要执行 getState 和 ackDiff 的本地连接。
	invalidSyncConnection, _ := newAPIResponseConn(t, map[string]any{"state": "ok"}, 200)
	// syncErr 保存同步状态确认错误。
	if syncErr := invalidSyncConnection.handleSyncExtra(context.Background(), map[string]any{"body": map[string]any{"syncExtraType": map[string]any{"type": 1}}}); syncErr != nil {
		t.Fatalf("handle sync extra=%v", syncErr)
	}
	// noOpConnection 保存无需确认的同步类型连接。
	noOpConnection, _ := newAPIResponseConn(t, nil, 200)
	// noOpErr 保存无需确认的同步类型错误。
	if noOpErr := noOpConnection.handleSyncExtra(context.Background(), map[string]any{"body": map[string]any{"syncExtraType": map[string]any{"type": 9}}}); noOpErr != nil {
		t.Fatalf("unsupported sync type=%v", noOpErr)
	}
}

// TestWSCancelledDialEntrancesCoversOpenAndDial 验证已取消上下文会快速终止外部连接入口。
func TestWSCancelledDialEntrancesCoversOpenAndDial(t *testing.T) {
	// canceledContext、cancelContext 保存已取消的连接上下文。
	canceledContext, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	// opened、openErr 保存 Open 入口的取消结果。
	opened, openErr := Open(canceledContext, Config{}, nilLogger())
	if opened != nil || openErr == nil {
		t.Fatalf("open result=%v err=%v", opened, openErr)
	}
	// dialed、dialErr 保存 Dial 入口的取消结果。
	dialed, dialErr := Dial(canceledContext, Config{}, nilLogger())
	if dialed != nil || dialErr == nil {
		t.Fatalf("dial result=%v err=%v", dialed, dialErr)
	}
}
