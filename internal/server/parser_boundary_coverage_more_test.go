package server

import (
	"bytes"
	"testing"
)

// TestServerParserHelpersCoverBoundaryInputs 验证导入解析器、XLSX 单元格取值、字节上限和容量计算边界。
func TestServerParserHelpersCoverBoundaryInputs(t *testing.T) {
	// headerCases 保存中英文订单导入表头及未知表头样例。
	headerCases := []struct {
		// raw 是用户上传文件中的原始表头。
		raw string
		// want 是归一化后的传输字段名。
		want string
	}{
		{raw: " 订单号 ", want: "order_id"},
		{raw: "item-title", want: "item_title"},
		{raw: "商品描述", want: "item_detail"},
		{raw: "未知字段", want: "未知字段"},
	}
	// headerCase 表示当前导入表头样例。
	for _, headerCase := range headerCases {
		// got 保存当前表头归一化结果。
		if got := normalizeImportHeader(headerCase.raw); got != headerCase.want {
			t.Errorf("表头=%q got=%q want=%q", headerCase.raw, got, headerCase.want)
		}
	}
	// normalizedHeaders 保存批量归一化结果，用于验证切片长度和字段顺序保持不变。
	normalizedHeaders := normalizeImportHeaders([]string{"订单号", "数量"})
	if len(normalizedHeaders) != 2 || normalizedHeaders[0] != "order_id" || normalizedHeaders[1] != "quantity" {
		t.Fatalf("批量表头归一化=%v", normalizedHeaders)
	}
	// cellCases 保存共享字符串、内联字符串、普通值和越界索引样例。
	cellCases := []struct {
		// cell 是待读取的 XLSX 单元格。
		cell xlsxCell
		// want 是单元格应解析出的文本值。
		want string
	}{
		{cell: xlsxCell{Type: "s", Value: "0"}, want: "shared"},
		{cell: xlsxCell{Type: "s", Value: "9"}, want: "9"},
		{cell: xlsxCell{Type: "inlineStr", InlineStr: " inline "}, want: "inline"},
		{cell: xlsxCell{Value: " 42 "}, want: "42"},
	}
	// cellCase 表示当前 XLSX 单元格解析样例。
	for _, cellCase := range cellCases {
		// got 保存当前单元格解析结果。
		if got := xlsxCellValue(cellCase.cell, []string{"shared"}); got != cellCase.want {
			t.Errorf("单元格=%+v got=%q want=%q", cellCase.cell, got, cellCase.want)
		}
	}
	// indexCases 保存 Excel 列引用的有效和无效样例。
	indexCases := []struct {
		// raw 是单元格引用文本。
		raw string
		// want 是零基列索引。
		want int
	}{
		{raw: "A1", want: 0},
		{raw: "C3", want: 2},
		{raw: "AA1", want: 26},
		{raw: "", want: 0},
	}
	// indexCase 表示当前 Excel 列引用样例。
	for _, indexCase := range indexCases {
		// got 保存当前单元格引用转换出的零基列索引。
		if got := xlsxCellIndex(indexCase.raw); got != indexCase.want {
			t.Errorf("列引用=%q got=%d want=%d", indexCase.raw, got, indexCase.want)
		}
	}
	// limitedData 保存未超过上限的原始请求体。
	limitedData, limited, limitedErr := readLimitedBytes(bytes.NewBufferString("abc"), 3)
	if limitedErr != nil || limited || string(limitedData) != "abc" {
		t.Fatalf("未超限读取 data=%q limited=%v err=%v", limitedData, limited, limitedErr)
	}
	// oversizedData 保存超过上限时的返回数据，函数应拒绝并标记超限。
	oversizedData, oversized, oversizedErr := readLimitedBytes(bytes.NewBufferString("abcd"), 3)
	if oversizedErr != nil || !oversized || oversizedData != nil {
		t.Fatalf("超限读取 data=%q limited=%v err=%v", oversizedData, oversized, oversizedErr)
	}
	if minInt(1, 2) != 1 || minInt(2, 1) != 1 || minInt(2, 2) != 2 {
		t.Fatal("minInt 边界结果错误")
	}
}
