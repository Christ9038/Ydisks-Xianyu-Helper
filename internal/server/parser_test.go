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

// TestParseMoneyCents 负责TestParseMoneyCents相关处理。
func TestParseMoneyCents(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := map[string]int64{"1": 100, "1.2": 120, "¥12.34": 1234, "￥0.01": 1, "-0.50": -50, "+2.05": 205, "": 0}
	// raw、want 表示当前遍历过程中的raw、want
	for raw, want := range cases {
		// got、err 保存got、err，供当前处理流程使用
		got, err := parseMoneyCents(raw)
		if err != nil || got != want {
			t.Errorf("parseMoneyCents(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	if // err 保存err，供当前处理流程使用
	_, err := parseMoneyCents("1.2.3"); err == nil {
		t.Fatal("invalid money should fail")
	}
}

// TestOrderImportParsers 负责Test订单ImportParsers相关处理。
func TestOrderImportParsers(t *testing.T) {
	// csvData 保存csv数据，供当前处理流程使用
	csvData := []byte("订单号,商品ID,买家ID,金额,状态\no1,i1,b1,12.50,已付款\n")
	// rows、err 保存rows、err，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	_, err := parseImportedOrderBytes(nil, "orders.csv"); err == nil {
		t.Fatal("empty import should fail")
	}
}

// TestOrderImportFormats 覆盖 TSV、单对象 JSON、.xls 拒绝、无扩展名默认 CSV。
func TestOrderImportFormats(t *testing.T) {
	// TSV
	// tsv 保存tsv，供当前处理流程使用
	tsv := []byte("order_id\tamount\no1\t1.5\n")
	// rows、err 保存rows、err，供当前处理流程使用
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

// TestOrderImportRejectsTooManyRows 负责Test订单ImportRejectsTooManyRows相关处理。
func TestOrderImportRejectsTooManyRows(t *testing.T) {
	// b 保存b，供当前处理流程使用
	var b strings.Builder
	b.WriteString("order_id\n")
	for // i 保存i，供当前处理流程使用
	i := 0; i < maxOrderImportRows+1; i++ {
		_, _ = fmt.Fprintf(&b, "o%d\n", i)
	}
	if // err 保存err，供当前处理流程使用
	_, err := parseImportedOrderBytes([]byte(b.String()), "orders.csv"); err == nil {
		t.Fatal("too many import rows should fail")
	}

	b.Reset()
	b.WriteString("[")
	for // i 保存i，供当前处理流程使用
	i := 0; i < maxOrderImportRows+1; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		_, _ = fmt.Fprintf(&b, `{"order_id":"o%d"}`, i)
	}
	b.WriteString("]")
	if // err 保存err，供当前处理流程使用
	_, err := parseImportedOrderBytes([]byte(b.String()), "orders.json"); err == nil {
		t.Fatal("too many JSON import rows should fail")
	}
}

// TestParseXLSXOrders 构造一个最小 xlsx，验证 shared string + 数字 cell 解析。
func TestParseXLSXOrders(t *testing.T) {
	// xlsx 保存xlsx，供当前处理流程使用
	xlsx := buildMinimalXLSX(t, [][]string{{"order_id", "amount"}, {"o1", "12.5"}})
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := parseXLSXOrders(xlsx)
	if err != nil {
		t.Fatalf("parseXLSX: %v", err)
	}
	if len(rows) != 1 || rows[0]["order_id"] != "o1" || rows[0]["amount"] != "12.5" {
		t.Fatalf("xlsx rows = %#v", rows)
	}
}

// TestReadLimitedXLSXXMLRejectsOversizedPart 负责TestReadLimitedXLSXXMLRejectsOversizedPart相关处理。
func TestReadLimitedXLSXXMLRejectsOversizedPart(t *testing.T) {
	// err 保存err，供当前处理流程使用
	_, err := readLimitedXLSXXML(io.LimitReader(endlessByteReader{}, maxXLSXXMLPartBytes+1))
	if err == nil {
		t.Fatal("oversized xlsx XML should fail")
	}
}

// endlessByteReader 保存endlessByteReader，供当前处理流程使用
type endlessByteReader struct{}

// Read 读取当前值。
func (endlessByteReader) Read(p []byte) (int, error) {
	// i 表示当前遍历过程中的i
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// TestNormalizeImportHeader 表驱动验证中英文别名归一。
func TestNormalizeImportHeader(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 保存got，供当前处理流程使用
		got := normalizeImportHeader(in); got != want {
			t.Errorf("normalizeImportHeader(%q) = %q; want %q", in, got, want)
		}
	}
}

// buildMinimalXLSX 构造一个仅含 sheet1 + sharedStrings 的最小 .xlsx 字节流。
// cell 用 shared string（t="s"）引用，与 Excel 默认导出一致。
// buildMinimalXLSX 负责buildMinimalXLSX相关处理。
func buildMinimalXLSX(t *testing.T, grid [][]string) []byte {
	t.Helper()
	// shared 保存shared，供当前处理流程使用
	var shared []string
	// sharedIdx 保存sharedIdx，供当前处理流程使用
	sharedIdx := map[string]int{}
	// addShared 保存addShared，供当前处理流程使用
	addShared := func(s string) int {
		if // i、ok 保存i、ok，供当前处理流程使用
		i, ok := sharedIdx[s]; ok {
			return i
		}
		// i 保存i，供当前处理流程使用
		i := len(shared)
		shared = append(shared, s)
		sharedIdx[s] = i
		return i
	}

	// 构造 worksheet xml。
	var rowsXML strings.Builder
	rowsXML.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	// r、row 表示当前遍历过程中的r、row
	for r, row := range grid {
		fmt.Fprintf(&rowsXML, `<row r="%d">`, r+1)
		// c、val 表示当前遍历过程中的c、val
		for c, val := range row {
			// ref 保存ref，供当前处理流程使用
			ref := fmt.Sprintf("%c%d", 'A'+c, r+1)
			// idx 保存idx，供当前处理流程使用
			idx := addShared(val)
			fmt.Fprintf(&rowsXML, `<c r="%s" t="s"><v>%d</v></c>`, ref, idx)
		}
		rowsXML.WriteString(`</row>`)
	}
	rowsXML.WriteString(`</sheetData></worksheet>`)

	// sstXML 保存sstXML，供当前处理流程使用
	var sstXML strings.Builder
	sstXML.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	// s 表示当前遍历过程中的s
	for _, s := range shared {
		sstXML.WriteString(`<si><t>` + s + `</t></si>`)
	}
	sstXML.WriteString(`</sst>`)

	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// zw 保存zw，供当前处理流程使用
	zw := zip.NewWriter(&buf)
	// must 保存must，供当前处理流程使用
	must := func(name, content string) {
		// f、err 保存f、err，供当前处理流程使用
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if // err 保存err，供当前处理流程使用
		_, err := f.Write([]byte(content)); err != nil {
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

// TestPublishBatchPathAndZipSafety 负责Test发布批次路径AndZipSafety相关处理。
func TestPublishBatchPathAndZipSafety(t *testing.T) {
	// raw 表示当前遍历过程中的原始
	for _, raw := range []string{"../secret.png", "/etc/passwd", `..\\secret.png`, ""} {
		if // err 保存err，供当前处理流程使用
		_, err := safeZipPath(raw); err == nil {
			t.Errorf("safeZipPath(%q) should fail", raw)
		}
	}
	if // got、err 保存got、err，供当前处理流程使用
	got, err := safeZipPath("images/a.png"); err != nil || got != filepath.Join("images", "a.png") {
		t.Fatalf("safe path = %q, %v", got, err)
	}

	// dest 保存dest，供当前处理流程使用
	dest := t.TempDir()
	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// zw 保存zw，供当前处理流程使用
	zw := zip.NewWriter(&buf)
	// f 保存f，供当前处理流程使用
	f, _ := zw.Create("images/a.png")
	_, _ = f.Write([]byte("not-an-image"))
	_ = zw.Close()
	if // err 保存err，供当前处理流程使用
	err := extractPublishImagesZip(buf.Bytes(), dest); err != nil {
		t.Fatalf("extract non-image: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := os.Stat(filepath.Join(dest, "images", "a.png")); !os.IsNotExist(err) {
		t.Fatal("non-image must not be extracted")
	}

	buf.Reset()
	zw = zip.NewWriter(&buf)
	f, _ = zw.Create("../escape.png")
	_, _ = f.Write([]byte("x"))
	_ = zw.Close()
	if // err 保存err，供当前处理流程使用
	err := extractPublishImagesZip(buf.Bytes(), dest); err == nil {
		t.Fatal("zip traversal should fail")
	}
}

// TestPublishBatchHelpers 负责Test发布批次Helpers相关处理。
func TestPublishBatchHelpers(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := splitImageRefs("a.png； b.png\nc.png"); len(got) != 3 {
		t.Fatalf("splitImageRefs = %#v", got)
	}
	// value 表示当前遍历过程中的值
	for _, value := range []string{"1", "TRUE", "yes", "是", "启用"} {
		if !parseLooseBool(value) {
			t.Errorf("parseLooseBool(%q) = false", value)
		}
	}
	if // got 保存got，供当前处理流程使用
	got := atoiPublishDefault("2.9", 1); got != 2 {
		t.Fatalf("atoiPublishDefault = %d", got)
	}
}

// TestNormalizePublishCardHeader 验证批量发布卡密字段的历史表头兼容映射。
func TestNormalizePublishCardHeader(t *testing.T) {
	// got 表示历史中文表头归一化后的字段名。
	if got := normalizePublishHeader("付款后发送的卡密"); got != "paid_delivery_contents" {
		t.Fatalf("normalizePublishHeader=%q", got)
	}
}

// TestNormalizePublishHeaderCategoryFallbackLabels 负责TestNormalize发布Header分类FallbackLabels相关处理。
func TestNormalizePublishHeaderCategoryFallbackLabels(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := map[string]string{
		"类目ID":        "category_id",
		"类目名称":        "category_name",
		"频道类目ID":      "channel_category_id",
		"淘宝类目ID":      "tb_category_id",
		"category_id": "category_id",
	}
	// input、want 表示当前遍历过程中的input、want
	for input, want := range cases {
		if // got 保存got，供当前处理流程使用
		got := normalizePublishHeader(input); got != want {
			t.Fatalf("normalizePublishHeader(%q)=%q want %q", input, got, want)
		}
	}
}

// TestPublicIPValidation 负责TestPublicIPValidation相关处理。
func TestPublicIPValidation(t *testing.T) {
	// raw 表示当前遍历过程中的原始
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1"} {
		if isPublicIP(net.ParseIP(raw)) {
			t.Errorf("private IP accepted: %s", raw)
		}
	}
	if !isPublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP rejected")
	}
}
