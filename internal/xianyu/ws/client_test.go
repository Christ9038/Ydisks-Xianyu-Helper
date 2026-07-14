package ws

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestWebsocketHeadersOnlyContainSanitizedCookie(t *testing.T) {
	got := websocketHeaders("a=1;\r\nb=2")
	want := map[string][]string{"Cookie": {"a=1;b=2"}}
	if !reflect.DeepEqual(map[string][]string(got), want) {
		t.Fatalf("websocket headers = %#v, want %#v", got, want)
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
	headers := map[string]any{"mid": "m1", "nil": nil}
	if got := ackVal(headers, "mid", "fallback"); got != "m1" {
		t.Fatalf("ackVal existing = %v", got)
	}
	if got := ackVal(headers, "nil", "fallback"); got != "fallback" {
		t.Fatalf("ackVal nil = %v", got)
	}
	if got := stripGoofish(" 123@goofish "); got != "123" {
		t.Fatalf("stripGoofish = %q", got)
	}
}
