package server

// item_publish_sheet.go: parse publish-batch spreadsheet sources (CSV/TSV/XLSX) into generic maps.

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// parsePublishSheetBytes 负责parse发布SheetBytes相关处理。
func parsePublishSheetBytes(raw []byte, filename string) ([]map[string]any, error) {
	return parsePublishSheetBytesWithLimit(raw, filename, 0)
}

// parsePublishSheetBytesWithLimit 负责parse发布SheetBytesWith上限相关处理。
func parsePublishSheetBytesWithLimit(raw []byte, filename string, maxRows int) ([]map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("导入内容为空")
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		return parseXLSXPublishSheetWithLimit(raw, maxRows)
	case ".csv":
		return parseDelimitedPublishSheet(raw, ',', maxRows)
	case ".tsv":
		return parseDelimitedPublishSheet(raw, '\t', maxRows)
	case ".xls":
		return nil, fmt.Errorf("暂不支持旧版 .xls，请另存为 .xlsx 或 CSV 后导入")
	default:
		return parseDelimitedPublishSheet(raw, ',', maxRows)
	}
}

// parseDelimitedPublishSheet 负责parseDelimited发布Sheet相关处理。
func parseDelimitedPublishSheet(raw []byte, comma rune, maxRows int) ([]map[string]any, error) {
	// reader 保存reader，供当前处理流程使用
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.Comma = comma
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	// header、err 保存header、err，供当前处理流程使用
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("解析表格失败: %w", err)
	}
	// keys 保存keys，供当前处理流程使用
	keys := publishHeaderKeys(header)
	// rows 保存rows，供当前处理流程使用
	rows := make([]map[string]any, 0, 64)
	// seenDataRow 保存seen数据Row，供当前处理流程使用
	seenDataRow := false
	for {
		// record、err 保存record、err，供当前处理流程使用
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析表格失败: %w", err)
		}
		seenDataRow = true
		// row、nonEmpty 保存row、nonEmpty，供当前处理流程使用
		row, nonEmpty := publishRowToMap(keys, record)
		if !nonEmpty {
			continue
		}
		rows = append(rows, row)
		if maxRows > 0 && len(rows) > maxRows {
			return nil, fmt.Errorf("单次最多解析 %d 行数据", maxRows)
		}
	}
	if !seenDataRow {
		return nil, fmt.Errorf("表格至少需要表头和一行数据")
	}
	return rows, nil
}

// parseXLSXPublishSheet 负责parseXLSX发布Sheet相关处理。
func parseXLSXPublishSheet(raw []byte) ([]map[string]any, error) {
	return parseXLSXPublishSheetWithLimit(raw, 0)
}

// parseXLSXPublishSheetWithLimit 负责parseXLSX发布SheetWith上限相关处理。
func parseXLSXPublishSheetWithLimit(raw []byte, maxRows int) ([]map[string]any, error) {
	// zr、err 保存zr、err，供当前处理流程使用
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("解析 xlsx 失败: %w", err)
	}
	// shared、err 保存shared、err，供当前处理流程使用
	shared, err := xlsxSharedStrings(zr)
	if err != nil {
		return nil, err
	}
	// sheet 保存sheet，供当前处理流程使用
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
	// rawSheet、err 保存原始Sheet、err，供当前处理流程使用
	rawSheet, err := readXLSXPart(sheet)
	if err != nil {
		return nil, err
	}
	// ws 保存ws，供当前处理流程使用
	var ws xlsxWorksheet
	if // err 保存err，供当前处理流程使用
	err := xmlUnmarshalXLSX(rawSheet, &ws); err != nil {
		return nil, fmt.Errorf("解析工作表失败: %w", err)
	}
	// rows 保存rows，供当前处理流程使用
	rows := make([][]string, 0, len(ws.SheetData.Rows))
	// row 表示当前遍历过程中的row
	for _, row := range ws.SheetData.Rows {
		// values 保存values，供当前处理流程使用
		values := []string{}
		// cell 表示当前遍历过程中的cell
		for _, cell := range row.Cells {
			// idx 保存idx，供当前处理流程使用
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
	// out 保存out，供当前处理流程使用
	out := rowsToPublishMaps(rows[0], rows[1:])
	if maxRows > 0 && len(out) > maxRows {
		return nil, fmt.Errorf("单次最多解析 %d 行数据", maxRows)
	}
	return out, nil
}

// rowsToPublishMaps 负责rowsTo发布Maps相关处理。
func rowsToPublishMaps(headers []string, rows [][]string) []map[string]any {
	// keys 保存keys，供当前处理流程使用
	keys := publishHeaderKeys(headers)
	// out 保存out，供当前处理流程使用
	out := make([]map[string]any, 0, len(rows))
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		// m、nonEmpty 保存m、nonEmpty，供当前处理流程使用
		m, nonEmpty := publishRowToMap(keys, row)
		if nonEmpty {
			out = append(out, m)
		}
	}
	return out
}

// publishHeaderKeys 负责发布HeaderKeys相关处理。
func publishHeaderKeys(headers []string) []string {
	// keys 保存keys，供当前处理流程使用
	keys := make([]string, len(headers))
	// i、h 表示当前遍历过程中的i、h
	for i, h := range headers {
		keys[i] = normalizePublishHeader(h)
	}
	return keys
}

// publishRowToMap 负责发布RowToMap相关处理。
func publishRowToMap(keys []string, row []string) (map[string]any, bool) {
	// m 保存m，供当前处理流程使用
	m := map[string]any{}
	// nonEmpty 保存nonEmpty，供当前处理流程使用
	nonEmpty := false
	// i、key 表示当前遍历过程中的i、key
	for i, key := range keys {
		if key == "" || i >= len(row) {
			continue
		}
		// value 保存值，供当前处理流程使用
		value := strings.TrimSpace(row[i])
		if value != "" {
			nonEmpty = true
		}
		m[key] = value
	}
	return m, nonEmpty
}

// normalizePublishHeader 负责normalize发布Header相关处理。
func normalizePublishHeader(header string) string {
	// h 保存h，供当前处理流程使用
	h := strings.ToLower(strings.TrimSpace(header))
	h = strings.NewReplacer(" ", "", "_", "", "-", "", "（", "(", "）", ")").Replace(h)
	switch h {
	case "cookieid", "账号id", "账号", "闲鱼账号":
		return "cookie_id"
	case "title", "itemtitle", "标题", "商品标题", "商品名称":
		return "title"
	case "description", "desc", "itemdescription", "描述", "商品描述", "商品详情":
		return "description"
	case "price", "itemprice", "价格", "商品价格":
		return "price"
	case "originalprice", "原价":
		return "original_price"
	case "quantity", "库存", "数量":
		return "quantity"
	case "postagemode", "邮费模式":
		return "postage_mode"
	case "postage", "邮费":
		return "postage"
	case "images", "image", "图片", "商品图片":
		return "images"
	case "categoryid", "catid", "类目id", "商品类目id":
		return "category_id"
	case "categoryname", "catname", "类目名称", "商品类目名称", "类目":
		return "category_name"
	case "channelcategoryid", "channelcatid", "频道类目id":
		return "channel_category_id"
	case "tbcategoryid", "tbcatid", "淘宝类目id":
		return "tb_category_id"
	case "paiddeliveryenabled", "付款发货启用", "付款后自动发货":
		return "paid_delivery_enabled"
	case "paiddeliverycontents", "付款发货内容", "付款后发送的卡密":
		return "paid_delivery_contents"
	case "reviewgiftenabled", "评价赠品启用", "评价后发送赠品":
		return "review_gift_enabled"
	case "reviewgiftcontents", "评价赠品内容", "评价后发送的卡密":
		return "review_gift_contents"
	case "reviewrequestenabled", "求评价启用", "超时未评价时提醒":
		return "review_request_enabled"
	case "reviewrequestafterhours", "求评价等待小时", "发货几小时后提醒":
		return "review_request_after_hours"
	case "reviewrequestmessage", "求评价文案", "提醒内容":
		return "review_request_message"
	case "reviewrequestmaxattempts", "求评价最多次数", "最多提醒几次":
		return "review_request_max_attempts"
	case "reviewrequestdelayseconds", "求评价延迟秒":
		return "review_request_delay_seconds"
	default:
		return strings.TrimSpace(header)
	}
}
