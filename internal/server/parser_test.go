package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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

// TestOrderImportFormats 覆盖 TSV、单对象 JSON、.xls 拒绝、无扩展名默认 CSV。
func TestOrderImportFormats(t *testing.T) {
	// TSV
	tsv := []byte("order_id\tamount\no1\t1.5\n")
	rows, err := parseImportedOrderBytes(tsv, "orders.tsv")
	if err != nil || len(rows) != 1 || rows[0]["order_id"] != "o1" {
		t.Fatalf("parse tsv = %#v, %v", rows, err)
	}

	// 单对象 JSON
	rows, err = parseImportedOrderBytes([]byte(`{"order_id":"o3","amount":"3.0"}`), "orders.json")
	if err != nil || len(rows) != 1 || rows[0]["order_id"] != "o3" {
		t.Fatalf("parse single json = %#v, %v", rows, err)
	}

	// .xls 应被拒绝
	if _, err := parseImportedOrderBytes([]byte("x"), "old.xls"); err == nil {
		t.Fatal(".xls should be rejected")
	}

	// 无扩展名 + JSON 内容 → 走 JSON
	rows, err = parseImportedOrderBytes([]byte(`[{"order_id":"o4"}]`), "noext")
	if err != nil || len(rows) != 1 || rows[0]["order_id"] != "o4" {
		t.Fatalf("parse noext json = %#v, %v", rows, err)
	}

	// 无扩展名 + 非 JSON → 默认 CSV
	rows, err = parseImportedOrderBytes([]byte("order_id\no5\n"), "noext")
	if err != nil || len(rows) != 1 || rows[0]["order_id"] != "o5" {
		t.Fatalf("parse noext csv = %#v, %v", rows, err)
	}

	// 表格只有表头 → 报错
	if _, err := parseImportedOrderBytes([]byte("order_id,amount\n"), "orders.csv"); err == nil {
		t.Fatal("header-only csv should fail")
	}
}

func TestOrderImportRejectsTooManyRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("order_id\n")
	for i := 0; i < maxOrderImportRows+1; i++ {
		_, _ = fmt.Fprintf(&b, "o%d\n", i)
	}
	if _, err := parseImportedOrderBytes([]byte(b.String()), "orders.csv"); err == nil {
		t.Fatal("too many import rows should fail")
	}

	b.Reset()
	b.WriteString("[")
	for i := 0; i < maxOrderImportRows+1; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		_, _ = fmt.Fprintf(&b, `{"order_id":"o%d"}`, i)
	}
	b.WriteString("]")
	if _, err := parseImportedOrderBytes([]byte(b.String()), "orders.json"); err == nil {
		t.Fatal("too many JSON import rows should fail")
	}
}

// TestParseXLSXOrders 构造一个最小 xlsx，验证 shared string + 数字 cell 解析。
func TestParseXLSXOrders(t *testing.T) {
	xlsx := buildMinimalXLSX(t, [][]string{{"order_id", "amount"}, {"o1", "12.5"}})
	rows, err := parseXLSXOrders(xlsx)
	if err != nil {
		t.Fatalf("parseXLSX: %v", err)
	}
	if len(rows) != 1 || rows[0]["order_id"] != "o1" || rows[0]["amount"] != "12.5" {
		t.Fatalf("xlsx rows = %#v", rows)
	}
}

func TestReadLimitedXLSXXMLRejectsOversizedPart(t *testing.T) {
	_, err := readLimitedXLSXXML(io.LimitReader(endlessByteReader{}, maxXLSXXMLPartBytes+1))
	if err == nil {
		t.Fatal("oversized xlsx XML should fail")
	}
}

type endlessByteReader struct{}

func (endlessByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// TestNormalizeImportHeader 表驱动验证中英文别名归一。
func TestNormalizeImportHeader(t *testing.T) {
	cases := map[string]string{
		"订单号":           "order_id",
		"OrderID":       "order_id",
		"商品标题":          "item_title",
		"商品名称":          "item_title",
		"金额":            "amount",
		"收货地址":          "receiver_address",
		"chat_id":       "chat_id",
		"会话id":          "chat_id",
		"unknown_field": "unknown_field",
	}
	for in, want := range cases {
		if got := normalizeImportHeader(in); got != want {
			t.Errorf("normalizeImportHeader(%q) = %q; want %q", in, got, want)
		}
	}
}

// buildMinimalXLSX 构造一个仅含 sheet1 + sharedStrings 的最小 .xlsx 字节流。
// cell 用 shared string（t="s"）引用，与 Excel 默认导出一致。
func buildMinimalXLSX(t *testing.T, grid [][]string) []byte {
	t.Helper()
	var shared []string
	sharedIdx := map[string]int{}
	addShared := func(s string) int {
		if i, ok := sharedIdx[s]; ok {
			return i
		}
		i := len(shared)
		shared = append(shared, s)
		sharedIdx[s] = i
		return i
	}

	// 构造 worksheet xml。
	var rowsXML strings.Builder
	rowsXML.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range grid {
		fmt.Fprintf(&rowsXML, `<row r="%d">`, r+1)
		for c, val := range row {
			ref := fmt.Sprintf("%c%d", 'A'+c, r+1)
			idx := addShared(val)
			fmt.Fprintf(&rowsXML, `<c r="%s" t="s"><v>%d</v></c>`, ref, idx)
		}
		rowsXML.WriteString(`</row>`)
	}
	rowsXML.WriteString(`</sheetData></worksheet>`)

	var sstXML strings.Builder
	sstXML.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, s := range shared {
		sstXML.WriteString(`<si><t>` + s + `</t></si>`)
	}
	sstXML.WriteString(`</sst>`)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	must := func(name, content string) {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	must("xl/sharedStrings.xml", sstXML.String())
	must("xl/worksheets/sheet1.xml", rowsXML.String())
	// ContentTypes / rels 不是解析必需，跳过。
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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
