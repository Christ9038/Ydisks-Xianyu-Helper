package server

import (
	"archive/zip"
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMoneyCents(t *testing.T) {
	cases := map[string]int64{"1": 100, "1.2": 120, "¥12.34": 1234, "￥0.01": 1, "": 0}
	for raw, want := range cases {
		got, err := parseMoneyCents(raw)
		if err != nil || got != want {
			t.Errorf("parseMoneyCents(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	if _, err := parseMoneyCents("1.2.3"); err == nil {
		t.Fatal("invalid money should fail")
	}
}

func TestOrderImportParsers(t *testing.T) {
	csvData := []byte("订单号,商品ID,买家ID,金额,状态\no1,i1,b1,12.50,已付款\n")
	rows, err := parseImportedOrderBytes(csvData, "orders.csv")
	if err != nil || len(rows) != 1 {
		t.Fatalf("parse csv = %#v, %v", rows, err)
	}
	if rows[0]["order_id"] != "o1" || rows[0]["item_id"] != "i1" {
		t.Fatalf("normalized row = %#v", rows[0])
	}
	rows, err = parseImportedOrderBytes([]byte(`[{"order_id":"o2","amount":"9.9"}]`), "orders.json")
	if err != nil || len(rows) != 1 || rows[0]["order_id"] != "o2" {
		t.Fatalf("parse json = %#v, %v", rows, err)
	}
	if _, err := parseImportedOrderBytes(nil, "orders.csv"); err == nil {
		t.Fatal("empty import should fail")
	}
}

func TestPublishBatchPathAndZipSafety(t *testing.T) {
	for _, raw := range []string{"../secret.png", "/etc/passwd", `..\\secret.png`, ""} {
		if _, err := safeZipPath(raw); err == nil {
			t.Errorf("safeZipPath(%q) should fail", raw)
		}
	}
	if got, err := safeZipPath("images/a.png"); err != nil || got != filepath.Join("images", "a.png") {
		t.Fatalf("safe path = %q, %v", got, err)
	}

	dest := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("images/a.png")
	_, _ = f.Write([]byte("not-an-image"))
	_ = zw.Close()
	if err := extractPublishImagesZip(buf.Bytes(), dest); err != nil {
		t.Fatalf("extract non-image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "images", "a.png")); !os.IsNotExist(err) {
		t.Fatal("non-image must not be extracted")
	}

	buf.Reset()
	zw = zip.NewWriter(&buf)
	f, _ = zw.Create("../escape.png")
	_, _ = f.Write([]byte("x"))
	_ = zw.Close()
	if err := extractPublishImagesZip(buf.Bytes(), dest); err == nil {
		t.Fatal("zip traversal should fail")
	}
}

func TestPublishBatchHelpers(t *testing.T) {
	if got := splitImageRefs("a.png； b.png\nc.png"); len(got) != 3 {
		t.Fatalf("splitImageRefs = %#v", got)
	}
	for _, value := range []string{"1", "TRUE", "yes", "是", "启用"} {
		if !parseLooseBool(value) {
			t.Errorf("parseLooseBool(%q) = false", value)
		}
	}
	if got := atoiPublishDefault("2.9", 1); got != 2 {
		t.Fatalf("atoiPublishDefault = %d", got)
	}
}

func TestParsePublishCardActions(t *testing.T) {
	actions, parseErr := parsePublishCardActions("101:1:0; 102:2:3")
	if parseErr != "" {
		t.Fatalf("parsePublishCardActions: %s", parseErr)
	}
	if len(actions) != 2 {
		t.Fatalf("actions=%+v", actions)
	}
	if actions[0].CardID != 101 || actions[0].DeliveryCount != 1 || actions[0].DelaySeconds != 0 {
		t.Fatalf("actions[0]=%+v", actions[0])
	}
	if actions[1].CardID != 102 || actions[1].DeliveryCount != 2 || actions[1].DelaySeconds != 3 {
		t.Fatalf("actions[1]=%+v", actions[1])
	}
	if _, parseErr := parsePublishCardActions("101:0"); parseErr == "" {
		t.Fatal("每件份数为0时应返回格式错误")
	}
	if got := normalizePublishHeader("付款后发送的卡密"); got != "paid_delivery_contents" {
		t.Fatalf("normalizePublishHeader=%q", got)
	}
}

func TestParsePublishAutomationSupportsMultipleCards(t *testing.T) {
	cfg := parsePublishAutomation(map[string]any{
		"paid_delivery_enabled":  "是",
		"paid_delivery_contents": "101:1:0;102:2:0",
		"review_gift_enabled":    "true",
		"review_gift_contents":   "201:1",
	})
	if !cfg.PaidDelivery.Enabled || len(cfg.PaidDelivery.Actions) != 2 {
		t.Fatalf("paid delivery=%+v", cfg.PaidDelivery)
	}
	if !cfg.ReviewGift.Enabled || len(cfg.ReviewGift.Actions) != 1 {
		t.Fatalf("review gift=%+v", cfg.ReviewGift)
	}
}

func TestPublicIPValidation(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1"} {
		if isPublicIP(net.ParseIP(raw)) {
			t.Errorf("private IP accepted: %s", raw)
		}
	}
	if !isPublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP rejected")
	}
}
