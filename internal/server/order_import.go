package server

// 订单导入解析：支持 JSON / CSV / TSV / XLSX 四种格式，统一输出 []map[string]any。
// 表头经 normalizeImportHeader 归一（中英文别名 → 标准字段名），供 order_handlers 上层消费。
//
// 抽离自 order_handlers.go，便于独立单测。

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// parseImportedOrders 从 HTTP 请求体解析导入的订单。
// 支持两种入口：multipart/form-data 上传文件（按扩展名分流），或 raw body（按内容分流）。
// 上限 maxOrderImportBytes（32 MiB），由调用方在更上层用 MaxBytesReader 强制。
// parseImportedOrders 封装parseImported订单列表业务协调。
func parseImportedOrders(w http.ResponseWriter, r *http.Request) ([]map[string]any, error) {
	// ct 用于本次流程后续判断的ct
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		// 订单文件上限保持 32 MiB，总请求额外预留 multipart 元数据空间。
		r.Body = http.MaxBytesReader(w, r.Body, maxOrderImportMultipartBytes)
		// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
		if err := r.ParseMultipartForm(maxOrderImportBytes); err != nil {
			// maxBytesErr 表示上传文件和表单元数据之和超过请求配额。
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				return nil, fmt.Errorf("上传内容不能超过 33 MiB")
			}
			return nil, fmt.Errorf("上传表单损坏，请检查后重试")
		}
		// file、header、err 用于本次流程后续判断的file、header、err
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("缺少上传文件")
		}
		defer file.Close()
		// raw、err 用于本次流程后续判断的raw、err
		raw, err := io.ReadAll(io.LimitReader(file, int64(maxOrderImportBytes)+1))
		if err != nil {
			return nil, fmt.Errorf("读取上传文件失败: %w", err)
		}
		if len(raw) > maxOrderImportBytes {
			return nil, fmt.Errorf("上传文件不能超过 32 MiB")
		}
		return parseImportedOrderBytes(raw, header.Filename)
	}
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(maxOrderImportBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("读取请求失败: %w", err)
	}
	if len(raw) > maxOrderImportBytes {
		return nil, fmt.Errorf("导入内容不能超过 32 MiB")
	}
	return parseImportedOrderBytes(raw, "orders.json")
}

// parseImportedOrderBytes 按文件扩展名/内容分流到具体解析器。
func parseImportedOrderBytes(raw []byte, filename string) ([]map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("导入内容为空")
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		return parseXLSXOrders(raw)
	case ".csv":
		return parseDelimitedOrders(raw, ',')
	case ".tsv":
		return parseDelimitedOrders(raw, '\t')
	case ".xls":
		return nil, fmt.Errorf("暂不支持旧版 .xls，请另存为 .xlsx 或 CSV 后导入")
	default:
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) || bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			return parseJSONOrders(raw)
		}
		return parseDelimitedOrders(raw, ',')
	}
}

// parseJSONOrders 解析 JSON 数组或单对象。
func parseJSONOrders(raw []byte) ([]map[string]any, error) {
	// trimmed 用于本次流程后续判断的trimmed
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("导入内容为空")
	}
	if trimmed[0] == '[' {
		return parseJSONOrderArray(trimmed)
	}
	// single 用于本次流程后续判断的single
	var single map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(trimmed, &single); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return normalizeImportOrderMaps([]map[string]any{single}), nil
}

// parseJSONOrderArray 封装parseJSON订单Array业务协调。
func parseJSONOrderArray(raw []byte) ([]map[string]any, error) {
	// dec 用于本次流程后续判断的dec
	dec := json.NewDecoder(bytes.NewReader(raw))
	// tok、err 用于本次流程后续判断的tok、err
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	if // delim、ok 用于本次流程后续判断的delim、ok
	delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("解析 JSON 失败: 不是数组")
	}
	// out 用于本次流程后续判断的out
	out := make([]map[string]any, 0, 256)
	for dec.More() {
		// row 用于本次流程后续判断的row
		var row map[string]any
		if // err 用于本次流程后续判断的err
		err := dec.Decode(&row); err != nil {
			return nil, fmt.Errorf("解析 JSON 失败: %w", err)
		}
		out = append(out, normalizeImportOrderMap(row))
		if len(out) > maxOrderImportRows {
			return nil, fmt.Errorf("单次最多导入 %d 条订单", maxOrderImportRows)
		}
	}
	if tok, err = dec.Token(); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	} else if // delim、ok 用于本次流程后续判断的delim、ok
	delim, ok := tok.(json.Delim); !ok || delim != ']' {
		return nil, fmt.Errorf("解析 JSON 失败: 数组未闭合")
	}
	// extra 用于本次流程后续判断的extra
	var extra any
	if // err 用于本次流程后续判断的err
	err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("解析 JSON 失败: 存在额外内容")
		}
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return out, nil
}

// parseDelimitedOrders 解析 CSV/TSV（按 comma 分隔）。首行为表头，其余为数据行。
// 全空行跳过。
// parseDelimitedOrders 封装parseDelimited订单列表业务协调。
func parseDelimitedOrders(raw []byte, comma rune) ([]map[string]any, error) {
	// reader 用于本次流程后续判断的reader
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.Comma = comma
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	// headerRow、err 用于本次流程后续判断的headerRow、err
	headerRow, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("解析表格失败: %w", err)
	}
	// headers 用于本次流程后续判断的headers
	headers := normalizeImportHeaders(headerRow)
	// rows 用于本次流程后续判断的rows
	rows := make([][]string, 0, 256)
	for {
		// record、err 用于本次流程后续判断的record、err
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析表格失败: %w", err)
		}
		rows = append(rows, record)
		if len(rows) > maxOrderImportRows {
			return nil, fmt.Errorf("单次最多导入 %d 条订单", maxOrderImportRows)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("表格至少需要表头和一行数据")
	}
	return rowsToMaps(headers, rows), nil
}

// parseXLSXOrders 解析 .xlsx（OOXML）：读取 sharedStrings + 第一个 worksheet，
// 按 cell ref（如 "C3"）定位列索引，支持共享字符串、内联字符串、数字三种 cell 类型。
// parseXLSXOrders 封装parseXLSX订单列表业务协调。
func parseXLSXOrders(raw []byte) ([]map[string]any, error) {
	// zr、err 用于本次流程后续判断的zr、err
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("解析 xlsx 失败: %w", err)
	}
	// shared、err 用于本次流程后续判断的shared、err
	shared, err := xlsxSharedStrings(zr)
	if err != nil {
		return nil, err
	}
	// sheet 用于本次流程后续判断的sheet
	var sheet *zip.File
	// f 表示当前遍历过程中的f
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheet = f
			break
		}
	}
	if sheet == nil {
		return nil, fmt.Errorf("xlsx 中未找到工作表")
	}
	// rawSheet、err 用于本次流程后续判断的原始Sheet、err
	rawSheet, err := readXLSXPart(sheet)
	if err != nil {
		return nil, err
	}
	// ws 用于本次流程后续判断的ws
	var ws xlsxWorksheet
	if // err 用于本次流程后续判断的err
	err := xmlUnmarshalXLSX(rawSheet, &ws); err != nil {
		return nil, fmt.Errorf("解析工作表失败: %w", err)
	}
	// rows 用于本次流程后续判断的rows
	rows := make([][]string, 0, len(ws.SheetData.Rows))
	// row 表示当前遍历过程中的row
	for _, row := range ws.SheetData.Rows {
		// values 用于本次流程后续判断的values
		values := []string{}
		// cell 表示当前遍历过程中的cell
		for _, cell := range row.Cells {
			// idx 用于本次流程后续判断的idx
			idx := xlsxCellIndex(cell.Ref)
			for len(values) <= idx {
				values = append(values, "")
			}
			values[idx] = xlsxCellValue(cell, shared)
		}
		rows = append(rows, values)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("xlsx 至少需要表头和一行数据")
	}
	// headers 用于本次流程后续判断的headers
	headers := normalizeImportHeaders(rows[0])
	// out 用于本次流程后续判断的out
	out := rowsToMaps(headers, rows[1:])
	if len(out) > maxOrderImportRows {
		return nil, fmt.Errorf("单次最多导入 %d 条订单", maxOrderImportRows)
	}
	return out, nil
}

// rowsToMaps 把表头 + 数据行转为 map 列表，全空行跳过。
func rowsToMaps(headers []string, rows [][]string) []map[string]any {
	// out 用于本次流程后续判断的out
	out := make([]map[string]any, 0, len(rows))
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		// m 用于本次流程后续判断的m
		m := make(map[string]any)
		// nonEmpty 用于本次流程后续判断的nonEmpty
		nonEmpty := false
		// i、h 表示当前遍历过程中的i、h
		for i, h := range headers {
			if h == "" || i >= len(row) {
				continue
			}
			// v 用于本次流程后续判断的v
			v := strings.TrimSpace(row[i])
			if v != "" {
				nonEmpty = true
			}
			m[h] = v
		}
		if nonEmpty {
			out = append(out, m)
		}
	}
	return out
}

// ---- XLSX OOXML 内部结构 ----

// xlsxWorksheet 用于本次流程后续判断的xlsxWorksheet
type xlsxWorksheet struct {
	SheetData struct {
		Rows []xlsxRow `xml:"row"`
	} `xml:"sheetData"`
}

// xlsxRow 用于本次流程后续判断的xlsxRow
type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

// xlsxCell 用于本次流程后续判断的xlsxCell
type xlsxCell struct {
	Ref       string `xml:"r,attr"`
	Type      string `xml:"t,attr"`
	Value     string `xml:"v"`
	InlineStr string `xml:"is>t"`
}

// xlsxSST 用于本次流程后续判断的xlsxSST
type xlsxSST struct {
	Items []struct {
		Inner string `xml:",innerxml"`
	} `xml:"si"`
}

// xlsxSharedStrings 读取 xl/sharedStrings.xml（共享字符串表），供 cell type="s" 引用。
func xlsxSharedStrings(zr *zip.Reader) ([]string, error) {
	// f 表示当前遍历过程中的f
	for _, f := range zr.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		// raw、err 用于本次流程后续判断的raw、err
		raw, err := readXLSXPart(f)
		if err != nil {
			return nil, err
		}
		// sst 用于本次流程后续判断的sst
		var sst xlsxSST
		if // err 用于本次流程后续判断的err
		err := xmlUnmarshalXLSX(raw, &sst); err != nil {
			return nil, err
		}
		// out 用于本次流程后续判断的out
		out := make([]string, 0, len(sst.Items))
		// item 表示当前遍历过程中的商品
		for _, item := range sst.Items {
			out = append(out, xmlCharData(item.Inner))
		}
		return out, nil
	}
	return nil, nil
}

// readXLSXPart 封装readXLSXPart业务协调。
func readXLSXPart(f *zip.File) ([]byte, error) {
	// rc、err 用于本次流程后续判断的rc、err
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return readLimitedXLSXXML(rc)
}

// readLimitedXLSXXML 封装readLimitedXLSXXML业务协调。
func readLimitedXLSXXML(r io.Reader) ([]byte, error) {
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := io.ReadAll(io.LimitReader(r, maxXLSXXMLPartBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxXLSXXMLPartBytes {
		return nil, fmt.Errorf("xlsx 内部 XML 超过 %d MiB", maxXLSXXMLPartBytes>>20)
	}
	return raw, nil
}

// xmlUnmarshalXLSX 封装xmlUnmarshalXLSX业务协调。
func xmlUnmarshalXLSX(raw []byte, v any) error {
	return xml.Unmarshal(raw, v)
}

// xlsxCellValue 按 cell 类型取值：s=共享字符串索引、inlineStr=内联字符串、其余=原始值。
func xlsxCellValue(cell xlsxCell, shared []string) string {
	switch cell.Type {
	case "s":
		// idx 用于本次流程后续判断的idx
		idx, _ := strconv.Atoi(strings.TrimSpace(cell.Value))
		if idx >= 0 && idx < len(shared) {
			return shared[idx]
		}
	case "inlineStr":
		return strings.TrimSpace(cell.InlineStr)
	}
	return strings.TrimSpace(cell.Value)
}

// xlsxCellIndex 把 cell ref 的列部分（如 "C3" → "C"）转为 0-based 列索引。
func xlsxCellIndex(ref string) int {
	// idx 用于本次流程后续判断的idx
	idx := 0
	// r 表示当前遍历过程中的r
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		idx = idx*26 + int(r-'A'+1)
	}
	if idx == 0 {
		return 0
	}
	return idx - 1
}

// xmlCharData 从 innerxml 片段中抽取所有字符数据（拼接富文本格式的单元格内容）。
func xmlCharData(inner string) string {
	// dec 用于本次流程后续判断的dec
	dec := xml.NewDecoder(strings.NewReader("<x>" + inner + "</x>"))
	// parts 用于本次流程后续判断的parts
	var parts []string
	for {
		// tok、err 用于本次流程后续判断的tok、err
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if // data、ok 用于本次流程后续判断的data、ok
		data, ok := tok.(xml.CharData); ok {
			parts = append(parts, string(data))
		}
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

// ---- 表头归一 ----

// normalizeImportOrderMaps 归一化 JSON 解析结果的 key（中文/英文别名 → 标准字段名）。
func normalizeImportOrderMaps(in []map[string]any) []map[string]any {
	// out 用于本次流程后续判断的out
	out := make([]map[string]any, 0, len(in))
	// raw 表示当前遍历过程中的原始
	for _, raw := range in {
		out = append(out, normalizeImportOrderMap(raw))
	}
	return out
}

// normalizeImportOrderMap 封装normalizeImport订单Map业务协调。
func normalizeImportOrderMap(raw map[string]any) map[string]any {
	// m 用于本次流程后续判断的m
	m := make(map[string]any)
	// k、v 表示当前遍历过程中的k、v
	for k, v := range raw {
		m[normalizeImportHeader(k)] = v
	}
	return m
}

// normalizeImportHeaders 封装normalizeImportHeaders业务协调。
func normalizeImportHeaders(headers []string) []string {
	// out 用于本次流程后续判断的out
	out := make([]string, len(headers))
	// i、h 表示当前遍历过程中的i、h
	for i, h := range headers {
		out[i] = normalizeImportHeader(h)
	}
	return out
}

// normalizeImportHeader 把表头归一为小写无分隔的标准字段名。
// 支持中英文别名（如 "订单号"→"order_id"、"商品标题"→"item_title"）。
// normalizeImportHeader 封装normalizeImportHeader业务协调。
func normalizeImportHeader(header string) string {
	// h 用于本次流程后续判断的h
	h := strings.ToLower(strings.TrimSpace(header))
	h = strings.NewReplacer(" ", "", "_", "", "-", "", "（", "(", "）", ")").Replace(h)
	switch h {
	case "orderid", "订单号", "订单id", "订单编号":
		return "order_id"
	case "cookieid", "账号id", "账号", "闲鱼账号":
		return "cookie_id"
	case "itemid", "商品id", "商品编号":
		return "item_id"
	case "itemtitle", "商品标题", "商品名称":
		return "item_title"
	case "itemprice", "商品价格":
		return "item_price"
	case "itemdetail", "itemdescription", "商品描述", "商品详情":
		return "item_detail"
	case "buyerid", "买家id":
		return "buyer_id"
	case "status", "orderstatus", "订单状态", "状态":
		return "status"
	case "specname", "规格名":
		return "spec_name"
	case "specvalue", "规格值":
		return "spec_value"
	case "quantity", "数量":
		return "quantity"
	case "amount", "金额", "订单金额":
		return "amount"
	case "receivername", "收件人", "收货人":
		return "receiver_name"
	case "receiverphone", "手机号", "收件电话", "收货电话":
		return "receiver_phone"
	case "receiveraddress", "地址", "收件地址", "收货地址":
		return "receiver_address"
	case "receivercity", "城市", "收件城市", "收货城市":
		return "receiver_city"
	case "chatid", "会话id":
		return "chat_id"
	default:
		return header
	}
}

// firstImportString 从归一化后的 map 中按候选 key 顺序取首个非空字符串。
func firstImportString(m map[string]any, keys ...string) string {
	// k 表示当前遍历过程中的k
	for _, k := range keys {
		if // v、ok 用于本次流程后续判断的v、ok
		v, ok := m[k]; ok {
			// s 用于本次流程后续判断的s
			s := strings.TrimSpace(stringFromAny(v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

// stringFromAny 把 any 安全转为字符串（nil→""，数字/布尔格式化）。
func stringFromAny(v any) string {
	switch // x 用于本次流程后续判断的x
	x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}
