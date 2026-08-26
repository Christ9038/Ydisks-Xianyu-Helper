package items

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// TestBatchPreviewCardActionParsingCoversDefaultsAndErrors 覆盖卡券动作默认值、兼容分隔符和各类格式错误。
func TestBatchPreviewCardActionParsingCoversDefaultsAndErrors(t *testing.T) {
	// emptyActions、emptyErr 保存空动作文本结果。
	emptyActions, emptyErr := parseCardActions(" ;\n")
	if emptyErr != "" || emptyActions != nil {
		t.Fatalf("空动作解析异常: actions=%v err=%q", emptyActions, emptyErr)
	}
	// actions、parseErr 保存合法动作解析结果。
	actions, parseErr := parseCardActions("9:2:3；10::")
	if parseErr != "" || len(actions) != 2 || actions[0].CardID != 9 || actions[0].DeliveryCount != 2 || actions[0].DelaySeconds != 3 || actions[1].DeliveryCount != 1 {
		t.Fatalf("合法动作解析异常: actions=%v err=%q", actions, parseErr)
	}
	// invalidCases 保存动作格式和字段值错误样例。
	invalidCases := []string{"9:1:2:3", ":1", "bad:1", "0:1", "9:0", "9:-1", "9:1:-1"}
	// raw 表示当前待拒绝的动作文本。
	for _, raw := range invalidCases {
		// actions、parseErr 保存当前错误样例的解析结果。
		actions, parseErr := parseCardActions(raw)
		if parseErr == "" || actions != nil {
			t.Errorf("非法动作未被拒绝 raw=%q actions=%v err=%q", raw, actions, parseErr)
		}
	}
}

// TestBatchPreviewMoneyParsingCoversSignsAndPrecision 覆盖金额符号、分值换算和精度错误边界。
func TestBatchPreviewMoneyParsingCoversSignsAndPrecision(t *testing.T) {
	// validCases 保存金额文本及其分值期望。
	validCases := map[string]int64{"": 0, "¥12.30": 1230, "￥1": 100, "+2.05": 205, "-1.2": -120}
	// raw、want 表示当前待解析金额及期望分值。
	for raw, want := range validCases {
		// cents、err 保存当前金额解析结果。
		cents, err := parseMoneyCents(raw)
		if err != nil || cents != want {
			t.Errorf("金额解析异常 raw=%q cents=%d err=%v want=%d", raw, cents, err, want)
		}
	}
	// invalidCases 保存金额语法和小数精度错误。
	invalidCases := []string{"abc", "1.234", "1.2.3"}
	// raw 表示当前待拒绝的金额文本。
	for _, raw := range invalidCases {
		// cents、err 保存当前错误金额解析结果。
		cents, err := parseMoneyCents(raw)
		if err == nil || cents != 0 {
			t.Errorf("非法金额未被拒绝 raw=%q cents=%d err=%v", raw, cents, err)
		}
	}
	// tooPreciseErr 保存超过两位小数的明确错误文本。
	_, tooPreciseErr := parseMoneyCents("1.234")
	if tooPreciseErr == nil || !strings.Contains(tooPreciseErr.Error(), "两位") {
		t.Fatalf("金额精度错误文本异常: %v", tooPreciseErr)
	}
}

// TestBatchPreviewXLSXPathAndCellIndexCoversMalformedInputs 覆盖 XLSX 内部路径与单元格列号边界。
func TestBatchPreviewXLSXPathAndCellIndexCoversMalformedInputs(t *testing.T) {
	// pathCases 保存关系目标及其安全校验期望。
	pathCases := map[string]bool{"xl/worksheets/sheet1.xml": true, "/xl/worksheets/sheet2.xml": true, "": false, "../secret.xml": false, "xl/sheet.txt": false}
	// raw、want 表示当前关系目标及其是否合法。
	for raw, want := range pathCases {
		// normalized、valid 保存目标路径归一结果。
		normalized, valid := xlsxWorkbookTargetPath(raw)
		if valid != want || (valid && normalized == "") {
			t.Errorf("XLSX 目标路径异常 raw=%q normalized=%q valid=%v want=%v", raw, normalized, valid, want)
		}
	}
	// indexCases 保存单元格引用和期望列下标。
	indexCases := map[string]int{"A1": 0, "B2": 1, "AA1": 26, "1": 0, "": 0}
	// reference、want 表示当前单元格引用及期望列下标。
	for reference, want := range indexCases {
		// got 保存当前单元格转换后的列下标。
		if got := xlsxCellIndex(reference); got != want {
			t.Errorf("XLSX 列下标异常 reference=%q got=%d want=%d", reference, got, want)
		}
	}
}

// TestXLSXSingleWorksheetCoversArchiveStructureGuards 覆盖 XLSX 工作簿、关系文件和目标路径的结构保护。
func TestXLSXSingleWorksheetCoversArchiveStructureGuards(t *testing.T) {
	// validEntries 保存唯一合法工作表所需的最小压缩包分区。
	validEntries := map[string]string{
		"xl/workbook.xml":            `<workbook><sheets><sheet name="Sheet1" id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData/></worksheet>`,
	}
	// validArchive 保存合法 XLSX 压缩包读取器。
	validArchive := newXLSXArchive(t, validEntries)
	// sheetFile、sheetErr 保存唯一工作表定位结果。
	sheetFile, sheetErr := xlsxSingleWorksheet(validArchive)
	if sheetErr != nil || sheetFile == nil || sheetFile.Name != "xl/worksheets/sheet1.xml" {
		t.Fatalf("valid worksheet file=%v err=%v", sheetFile, sheetErr)
	}
	// invalidEntries 保存应被工作表结构校验拒绝的压缩包变体。
	invalidEntries := []map[string]string{
		{},
		{"xl/workbook.xml": `<workbook>`},
		{"xl/workbook.xml": `<workbook><sheets/></workbook>`},
		{"xl/workbook.xml": `<workbook><sheets><sheet name="A" id="1"/><sheet name="B" id="2"/></sheets></workbook>`},
		{"xl/workbook.xml": `<workbook><sheets><sheet id="1"/></sheets></workbook>`},
		{"xl/workbook.xml": `<workbook><sheets><sheet name="A" id="1"/></sheets></workbook>`},
		{"xl/workbook.xml": `<workbook><sheets><sheet name="A" id="1"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<Relationships>`},
		{"xl/workbook.xml": `<workbook><sheets><sheet name="A" id="1"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="other" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/workbook.xml": `<workbook><sheets><sheet name="A" id="1"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="1" Target="https://example.com/sheet.xml" TargetMode="External"/></Relationships>`},
		{"xl/workbook.xml": `<workbook><sheets><sheet name="A" id="1"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="1" Target="../secret.xml"/></Relationships>`},
	}
	// entries 表示当前待校验的压缩包分区集合。
	for _, entries := range invalidEntries {
		// archive 保存当前非法结构的压缩包读取器。
		archive := newXLSXArchive(t, entries)
		// invalidFile、invalidErr 保存非法结构的定位结果。
		invalidFile, invalidErr := xlsxSingleWorksheet(archive)
		if invalidErr == nil || invalidFile != nil {
			t.Fatalf("invalid worksheet file=%v err=%v entries=%v", invalidFile, invalidErr, entries)
		}
	}
}

// newXLSXArchive 创建只包含指定 XML 分区的内存压缩包，供结构校验测试复用。
func newXLSXArchive(t *testing.T, entries map[string]string) *zip.Reader {
	// archiveBuffer 保存测试压缩包的完整字节。
	archiveBuffer := bytes.NewBuffer(nil)
	// archiveWriter 负责把测试分区写入内存压缩包。
	archiveWriter := zip.NewWriter(archiveBuffer)
	// name、content 表示当前分区名称和 XML 内容。
	for name, content := range entries {
		// fileWriter、createErr 保存当前分区写入器及创建错误。
		fileWriter, createErr := archiveWriter.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		// writeErr 保存当前分区内容写入错误。
		if _, writeErr := fileWriter.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	// closeErr 保存压缩包目录收尾错误。
	if closeErr := archiveWriter.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// archive、openErr 保存可供业务代码读取的压缩包及解析错误。
	archive, openErr := zip.NewReader(bytes.NewReader(archiveBuffer.Bytes()), int64(archiveBuffer.Len()))
	if openErr != nil {
		t.Fatal(openErr)
	}
	return archive
}
