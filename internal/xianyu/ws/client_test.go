package ws

import (
	"encoding/base64"
	"testing"

	"xianyu-go/internal/xianyu"
)

func TestWebsocketHeadersMatchBrowserHandshake(t *testing.T) {
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: "runtime-browser-ua"})
	got := websocketHeaders()
	if got.Get("Origin") != "https://www.goofish.com" || got.Get("User-Agent") != "runtime-browser-ua" {
		t.Fatalf("websocket headers = %#v", got)
	}
	if got.Get("Cookie") != "" {
		t.Fatalf("dingtalk WebSocket 不应收到 goofish Cookie: %#v", got)
	}
}

func TestOfficialRegistrationUAUsesRuntimeBrowserVersion(t *testing.T) {
	raw := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/138.0.7204.92 Safari/537.36"
	want := raw + " DingTalk(2.2.0) OS(Mac OS/10.15.7) Browser(Chrome/138.0.7204.92) DingWeb/2.2.0 DingWeb/2.2.0"
	if got := OfficialRegistrationUA(raw); got != want {
		t.Fatalf("OfficialRegistrationUA() = %q, want %q", got, want)
	}
}

func TestExtractSyncPayload(t *testing.T) {
	msg := map[string]any{"body": map[string]any{"syncPushPackage": map[string]any{
		"data": []any{map[string]any{"data": "payload"}},
	}}}
	if got, ok := extractSyncPayload(msg); !ok || got != "payload" {
		t.Fatalf("extractSyncPayload() = %q, %v", got, ok)
	}
	for _, invalid := range []map[string]any{{}, {"body": map[string]any{}}, {"body": map[string]any{"syncPushPackage": map[string]any{"data": []any{}}}}} {
		if _, ok := extractSyncPayload(invalid); ok {
			t.Fatalf("invalid payload accepted: %#v", invalid)
		}
	}
}

func TestDecodeSyncDataJSONAndInvalid(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte(`{"event":"paid","count":2}`))
	got, err := decodeSyncData(raw)
	if err != nil || got["event"] != "paid" || got["count"] != float64(2) {
		t.Fatalf("decodeSyncData() = %#v, %v", got, err)
	}
	if _, err := decodeSyncData("not-base64"); err == nil {
		t.Fatal("invalid payload should fail")
	}
}

func TestWSHelpers(t *testing.T) {
	if got := stripGoofish(" 123@goofish "); got != "123" {
		t.Fatalf("stripGoofish = %q", got)
	}
}
