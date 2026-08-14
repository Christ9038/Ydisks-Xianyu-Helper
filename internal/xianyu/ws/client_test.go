package ws

import (
	"encoding/base64"
	"testing"

	"xianyu-go/internal/xianyu"
)

// TestWebsocketHeadersMatchBrowserHandshake 负责TestWebsocketHeadersMatch浏览器Handshake相关处理。
func TestWebsocketHeadersMatchBrowserHandshake(t *testing.T) {
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: "runtime-browser-ua"})
	// got 保存got，供当前处理流程使用
	got := websocketHeaders()
	if got.Get("Origin") != "https://www.goofish.com" || got.Get("User-Agent") != "runtime-browser-ua" {
		t.Fatalf("websocket headers = %#v", got)
	}
	if got.Get("Cookie") != "" {
		t.Fatalf("dingtalk WebSocket 不应收到 goofish Cookie: %#v", got)
	}
}

// TestOfficialRegistrationUAUsesRuntimeBrowserVersion 负责TestOfficialRegistrationUAUsesRuntime浏览器Version相关处理。
func TestOfficialRegistrationUAUsesRuntimeBrowserVersion(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/138.0.7204.92 Safari/537.36"
	// want 保存want，供当前处理流程使用
	want := raw + " DingTalk(2.2.0) OS(Mac OS/10.15.7) Browser(Chrome/138.0.7204.92) DingWeb/2.2.0 IMPaaS DingWeb/2.2.0"
	if // got 保存got，供当前处理流程使用
	got := OfficialRegistrationUA(raw); got != want {
		t.Fatalf("OfficialRegistrationUA() = %q, want %q", got, want)
	}
}

// TestOfficialRegistrationUARecognizesHeadlessChrome 负责TestOfficialRegistrationUARecognizesHeadlessChrome相关处理。
func TestOfficialRegistrationUARecognizesHeadlessChrome(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 HeadlessChrome/138.0.7204.92 Safari/537.36"
	// want 保存want，供当前处理流程使用
	want := raw + " DingTalk(2.2.0) OS(Linux/other) Browser(Chrome Headless/138.0.7204.92) DingWeb/2.2.0 IMPaaS DingWeb/2.2.0"
	if // got 保存got，供当前处理流程使用
	got := OfficialRegistrationUA(raw); got != want {
		t.Fatalf("OfficialRegistrationUA() = %q, want %q", got, want)
	}
}

// TestExtractSyncPayload 负责TestExtractSync请求载荷相关处理。
func TestExtractSyncPayload(t *testing.T) {
	// msg 保存msg，供当前处理流程使用
	msg := map[string]any{"body": map[string]any{"syncPushPackage": map[string]any{
		"data": []any{map[string]any{"data": "payload"}},
	}}}
	if // got、ok 保存got、ok，供当前处理流程使用
	got, ok := extractSyncPayload(msg); !ok || got != "payload" {
		t.Fatalf("extractSyncPayload() = %q, %v", got, ok)
	}
	// invalid 表示当前遍历过程中的invalid
	for _, invalid := range []map[string]any{{}, {"body": map[string]any{}}, {"body": map[string]any{"syncPushPackage": map[string]any{"data": []any{}}}}} {
		if // ok 保存ok，供当前处理流程使用
		_, ok := extractSyncPayload(invalid); ok {
			t.Fatalf("invalid payload accepted: %#v", invalid)
		}
	}
}

// TestDecodeSyncDataJSONAndInvalid 负责TestDecodeSync数据JSONAndInvalid相关处理。
func TestDecodeSyncDataJSONAndInvalid(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := base64.StdEncoding.EncodeToString([]byte(`{"event":"paid","count":2}`))
	// got、err 保存got、err，供当前处理流程使用
	got, err := decodeSyncData(raw)
	if err != nil || got["event"] != "paid" || got["count"] != float64(2) {
		t.Fatalf("decodeSyncData() = %#v, %v", got, err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := decodeSyncData("not-base64"); err == nil {
		t.Fatal("invalid payload should fail")
	}
}

// TestWSHelpers 负责TestWSHelpers相关处理。
func TestWSHelpers(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := stripGoofish(" 123@goofish "); got != "123" {
		t.Fatalf("stripGoofish = %q", got)
	}
}
