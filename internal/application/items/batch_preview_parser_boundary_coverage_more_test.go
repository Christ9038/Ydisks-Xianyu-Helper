package items

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestBatchPreviewDelimitedParserCoversInputAndLimitErrors 覆盖 CSV 解析器的分隔符、空行和行数限制错误。
func TestBatchPreviewDelimitedParserCoversInputAndLimitErrors(t *testing.T) {
	// emptyErr 保存缺少表头时的 CSV 解析错误。
	if _, emptyErr := parseDelimitedSheet(nil, ',', 10); emptyErr == nil {
		t.Fatal("空 CSV 应返回表头错误")
	}
	// delimiterErr 保存非法分隔符导致的底层读取错误。
	if _, delimiterErr := parseDelimitedSheet([]byte("title\nitem\n"), '"', 10); delimiterErr == nil {
		t.Fatal("非法 CSV 分隔符应返回读取错误")
	}
	// headerOnlyErr 保存只有表头而没有数据记录的错误。
	if _, headerOnlyErr := parseDelimitedSheet([]byte("title,price\n"), ',', 10); headerOnlyErr == nil {
		t.Fatal("只有表头的 CSV 应被拒绝")
	}
	// rows 保存包含空行和有效行的解析结果，验证空行会跳过但不会终止读取。
	rows, rowsErr := parseDelimitedSheet([]byte("title,price\n,\nitem,1\n"), ',', 10)
	if rowsErr != nil || len(rows) != 1 || rows[0]["title"] != "item" {
		t.Fatalf("CSV 空行处理异常 rows=%v err=%v", rows, rowsErr)
	}
	// limitErr 保存超过单次解析上限的错误。
	if _, limitErr := parseDelimitedSheet([]byte("title\na\nb\n"), ',', 1); limitErr == nil {
		t.Fatal("超过 CSV 行数上限应返回错误")
	}
	// originalReader 保存生产 CSV 读取器，测试结束后必须恢复全局替身。
	originalReader := newBatchPreviewCSVReader
	defer func() { newBatchPreviewCSVReader = originalReader }()
	// newBatchPreviewCSVReader 返回首行成功、后续读取失败的 CSV 读取器。
	newBatchPreviewCSVReader = func([]byte) *csv.Reader {
		// reader 保存注入底层读取错误的 CSV 读取器。
		reader := csv.NewReader(&batchPreviewCSVReadErrorReader{first: []byte("title\n")})
		reader.FieldsPerRecord = -1
		reader.LazyQuotes = true
		return reader
	}
	// readerErr 保存 CSV 第二次读取时的底层错误。
	if _, readerErr := parseDelimitedSheet([]byte("ignored"), ',', 10); readerErr == nil {
		t.Fatal("CSV 底层读取错误应返回")
	}
}

// batchPreviewCSVReadErrorReader 首次返回表头，后续返回底层读取错误。
type batchPreviewCSVReadErrorReader struct {
	// first 保存首次读取要交给 CSV 解析器的表头字节。
	first []byte
	// err 表示后续读取失败的原因。
	err error
}

// Read 按阶段返回表头或底层读取错误。
func (reader *batchPreviewCSVReadErrorReader) Read(target []byte) (int, error) {
	if len(reader.first) > 0 {
		// count 保存本次复制到调用方缓冲区的字节数。
		count := copy(target, reader.first)
		reader.first = reader.first[count:]
		return count, nil
	}
	if reader.err == nil {
		reader.err = errors.New("读取 CSV 输入失败")
	}
	return 0, reader.err
}

// TestBatchPreviewXLSXParserCoversMalformedPartsAndSizeGuard 覆盖 XLSX 解析、共享字符串和内部 XML 大小保护。
func TestBatchPreviewXLSXParserCoversMalformedPartsAndSizeGuard(t *testing.T) {
	// rawErr 保存非法 ZIP 输入的解析错误。
	if _, rawErr := parseXLSXSheet([]byte("not-a-zip"), 10); rawErr == nil {
		t.Fatal("非法 XLSX 压缩包应返回错误")
	}
	// baseEntries 保存最小合法工作簿及其关系和工作表 XML。
	baseEntries := map[string]string{
		"xl/workbook.xml":            `<workbook><sheets><sheet name="Sheet1" id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>标题</t></is></c></row><row><c r="A2"><v>1</v></c></row></sheetData></worksheet>`,
	}
	// validArchiveBytes 保存合法工作簿压缩后的字节，供解析器重新打开。
	validArchiveBytes := xlsxArchiveBytes(t, baseEntries)
	// rows、rowsErr 保存合法 XLSX 的解析结果。
	rows, rowsErr := parseXLSXSheet(validArchiveBytes, 10)
	if rowsErr != nil || len(rows) != 1 || rows[0]["title"] != "1" {
		t.Fatalf("合法 XLSX 解析异常 rows=%v err=%v", rows, rowsErr)
	}
	// limitErr 保存 XLSX 数据行超过上限的错误。
	if _, limitErr := parseXLSXSheet(validArchiveBytes, 0); limitErr != nil {
		t.Fatalf("不设 XLSX 行数上限不应失败: %v", limitErr)
	}
	// maxRowsErr 保存 XLSX 数据行超过一行上限时的错误。
	tooManyEntries := map[string]string{
		"xl/workbook.xml":            baseEntries["xl/workbook.xml"],
		"xl/_rels/workbook.xml.rels": baseEntries["xl/_rels/workbook.xml.rels"],
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>标题</t></is></c></row><row><c r="A2"><v>1</v></c></row><row><c r="A3"><v>2</v></c></row></sheetData></worksheet>`,
	}
	// maxRowsErr 保存超过 XLSX 行数上限时的解析错误。
	if _, maxRowsErr := parseXLSXSheet(xlsxArchiveBytes(t, tooManyEntries), 1); maxRowsErr == nil {
		t.Fatal("超过 XLSX 行数上限应返回错误")
	}
	// malformedSheetEntries 保存工作表 XML 无法反序列化的压缩包。
	malformedSheetEntries := map[string]string{
		"xl/workbook.xml":            baseEntries["xl/workbook.xml"],
		"xl/_rels/workbook.xml.rels": baseEntries["xl/_rels/workbook.xml.rels"],
		"xl/worksheets/sheet1.xml":   `<worksheet>`,
	}
	// malformedErr 保存非法工作表 XML 的解析错误。
	if _, malformedErr := parseXLSXSheet(xlsxArchiveBytes(t, malformedSheetEntries), 10); malformedErr == nil {
		t.Fatal("非法工作表 XML 应返回错误")
	}
	// malformedSharedEntries 保存共享字符串 XML 无法反序列化的压缩包。
	malformedSharedEntries := map[string]string{
		"xl/sharedStrings.xml":       `<sst>`,
		"xl/workbook.xml":            baseEntries["xl/workbook.xml"],
		"xl/_rels/workbook.xml.rels": baseEntries["xl/_rels/workbook.xml.rels"],
		"xl/worksheets/sheet1.xml":   baseEntries["xl/worksheets/sheet1.xml"],
	}
	// malformedSharedErr 保存非法共享字符串 XML 的解析错误。
	if _, malformedSharedErr := parseXLSXSheet(xlsxArchiveBytes(t, malformedSharedEntries), 10); malformedSharedErr == nil {
		t.Fatal("非法共享字符串 XML 应返回错误")
	}
	// headerOnlyEntries 保存只有表头行的 XLSX 工作簿。
	headerOnlyEntries := map[string]string{
		"xl/workbook.xml":            baseEntries["xl/workbook.xml"],
		"xl/_rels/workbook.xml.rels": baseEntries["xl/_rels/workbook.xml.rels"],
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>标题</t></is></c></row></sheetData></worksheet>`,
	}
	// headerOnlyErr 保存 XLSX 缺少数据行的错误。
	if _, headerOnlyErr := parseXLSXSheet(xlsxArchiveBytes(t, headerOnlyEntries), 10); headerOnlyErr == nil {
		t.Fatal("只有表头的 XLSX 应被拒绝")
	}
	// oversizedContent 保存超过 32 MiB 的内部 XML 内容，验证读取上限不会被压缩绕过。
	oversizedContent := strings.Repeat("x", (32<<20)+1)
	// oversizedEntries 保存包含超大共享字符串部件的压缩包。
	oversizedEntries := map[string]string{"xl/sharedStrings.xml": oversizedContent}
	// oversizedArchive 保存超大分区的 ZIP 读取器。
	oversizedArchive := newXLSXArchive(t, oversizedEntries)
	// oversizedFile 保存超大共享字符串分区。
	var oversizedFile *zip.File
	// file 表示当前遍历到的压缩包分区候选。
	for _, file := range oversizedArchive.File {
		if file.Name == "xl/sharedStrings.xml" {
			oversizedFile = file
		}
	}
	if oversizedFile == nil {
		t.Fatal("未找到超大测试分区")
	}
	// oversizedErr 保存超大 XML 分区触发的大小限制错误。
	if _, oversizedErr := readXLSXPart(oversizedFile); oversizedErr == nil {
		t.Fatal("超大 XLSX XML 应返回大小限制错误")
	}
	// oversizedSharedEntries 保存共享字符串部件超限的完整工作簿。
	oversizedSharedEntries := map[string]string{
		"xl/sharedStrings.xml":       oversizedContent,
		"xl/workbook.xml":            baseEntries["xl/workbook.xml"],
		"xl/_rels/workbook.xml.rels": baseEntries["xl/_rels/workbook.xml.rels"],
		"xl/worksheets/sheet1.xml":   baseEntries["xl/worksheets/sheet1.xml"],
	}
	// oversizedSharedErr 保存共享字符串读取阶段的部件错误。
	if _, oversizedSharedErr := parseXLSXSheet(xlsxArchiveBytes(t, oversizedSharedEntries), 10); oversizedSharedErr == nil {
		t.Fatal("超大共享字符串部件应返回错误")
	}
}

// TestBatchPreviewXLSXPartReaderCoversOpenAndReadErrors 覆盖 XLSX 分区打开失败和流读取失败。
func TestBatchPreviewXLSXPartReaderCoversOpenAndReadErrors(t *testing.T) {
	// entries 保存供读取器定位分区的最小压缩包。
	entries := map[string]string{"xl/test.xml": "内容"}
	// archive 保存可重复打开的测试压缩包。
	archive := newXLSXArchive(t, entries)
	// file 保存测试 XML 分区。
	file := archive.File[0]
	// originalOpen 保存生产读取器，测试结束后必须恢复全局替身。
	originalOpen := openXLSXPart
	defer func() { openXLSXPart = originalOpen }()
	// openErr 保存模拟分区打开失败的错误。
	openErr := errors.New("打开 XLSX 分区失败")
	openXLSXPart = func(*zip.File) (io.ReadCloser, error) { return nil, openErr }
	// err 保存分区打开失败的读取结果。
	if _, err := readXLSXPart(file); !errors.Is(err, openErr) {
		t.Fatalf("分区打开错误未返回: %v", err)
	}
	// parseOpenXLSXPart 在工作表分区阶段返回打开错误，验证解析入口的错误传播。
	parseOpenXLSXPart := func(candidate *zip.File) (io.ReadCloser, error) {
		if candidate.Name == "xl/worksheets/sheet1.xml" {
			return nil, openErr
		}
		return candidate.Open()
	}
	// baseEntries 保存解析入口所需的 workbook、关系和工作表分区。
	baseEntries := map[string]string{
		"xl/workbook.xml":            `<workbook><sheets><sheet name="Sheet1" id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c r="A1"><v>标题</v></c></row><row><c r="A2"><v>1</v></c></row></sheetData></worksheet>`,
	}
	openXLSXPart = parseOpenXLSXPart
	// err 保存工作表分区打开失败的解析结果。
	if _, err := parseXLSXSheet(xlsxArchiveBytes(t, baseEntries), 10); !errors.Is(err, openErr) {
		t.Fatalf("工作表分区打开错误未传播: %v", err)
	}
	// readErr 保存模拟 XML 流读取失败的错误。
	readErr := errors.New("读取 XLSX 分区失败")
	openXLSXPart = func(*zip.File) (io.ReadCloser, error) { return &batchPreviewReadErrorCloser{err: readErr}, nil }
	// err 保存分区流读取失败的结果。
	if _, err := readXLSXPart(file); !errors.Is(err, readErr) {
		t.Fatalf("分区读取错误未返回: %v", err)
	}
	// namedErr 保存按名称读取分区时的打开错误。
	openXLSXPart = func(*zip.File) (io.ReadCloser, error) { return nil, openErr }
	// err 保存命名分区打开失败的读取结果。
	if _, err := readXLSXPartNamed(archive, "xl/test.xml"); !errors.Is(err, openErr) {
		t.Fatalf("命名分区打开错误未返回: %v", err)
	}
	// missingErr 保存按名称读取不存在分区时的错误。
	// err 保存不存在命名分区的读取结果。
	if _, err := readXLSXPartNamed(archive, "xl/missing.xml"); err == nil {
		t.Fatal("不存在的命名分区应返回错误")
	}
	// openXLSXPart 恢复生产读取器，保证后续结构定位测试能进入目标分区查找分支。
	openXLSXPart = originalOpen
	// missingSheetEntries 保存 workbook 关系存在但目标 XML 分区缺失的压缩包。
	missingSheetEntries := map[string]string{
		"xl/workbook.xml":            `<workbook><sheets><sheet name="Sheet1" id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/missing.xml"/></Relationships>`,
	}
	// missingSheetErr 保存工作表目标不存在时的结构错误。
	if _, missingSheetErr := parseXLSXSheet(xlsxArchiveBytes(t, missingSheetEntries), 10); missingSheetErr == nil {
		t.Fatal("缺失工作表目标应返回结构错误")
	}
	// missingSheetArchive 保存用于直接验证唯一工作表定位器的压缩包。
	missingSheetArchiveBytes := xlsxArchiveBytes(t, missingSheetEntries)
	// missingSheetArchive、missingSheetArchiveErr 保存直接打开缺失工作表测试压缩包的结果。
	missingSheetArchive, missingSheetArchiveErr := zip.NewReader(bytes.NewReader(missingSheetArchiveBytes), int64(len(missingSheetArchiveBytes)))
	if missingSheetArchiveErr != nil {
		t.Fatal(missingSheetArchiveErr)
	}
	// missingSheetLookupErr 保存工作表定位器返回的缺失目标错误。
	if _, missingSheetLookupErr := xlsxSingleWorksheet(missingSheetArchive); missingSheetLookupErr == nil {
		t.Fatal("工作表定位器应拒绝缺失目标")
	}
}

// batchPreviewReadErrorCloser 是始终返回错误的 XLSX 流替身。
type batchPreviewReadErrorCloser struct {
	// err 保存流读取时返回的错误。
	err error
}

// Read 返回预置错误，覆盖 io.ReadAll 的错误路径。
func (reader *batchPreviewReadErrorCloser) Read([]byte) (int, error) { return 0, reader.err }

// Close 关闭测试流并保持幂等成功。
func (reader *batchPreviewReadErrorCloser) Close() error { return nil }

// TestBatchPreviewFieldHelpersCoversShortRowsAndFallbackTypes 覆盖字段映射的短行、空键和未知类型分支。
func TestBatchPreviewFieldHelpersCoversShortRowsAndFallbackTypes(t *testing.T) {
	// fields、nonEmpty 保存键多于值时的字段映射结果。
	fields, nonEmpty := rowMap([]string{"title", "", "price"}, []string{"商品"})
	if !nonEmpty || fields["title"] != "商品" {
		t.Fatalf("短行映射异常 fields=%v nonEmpty=%v", fields, nonEmpty)
	}
	// fallbackValue 保存未知表格类型的通用文本转换结果。
	if fallbackValue := stringValue(struct{ Name string }{Name: "custom"}); fallbackValue != "{custom}" {
		t.Fatalf("未知表格类型转换异常: %q", fallbackValue)
	}
	// parseErr 保存小数部分包含非数字字符时的金额错误。
	if _, parseErr := parseMoneyCents("1.aa"); parseErr == nil {
		t.Fatal("非法小数部分应返回金额解析错误")
	}
}

// xlsxArchiveBytes 将测试分区编码为 ZIP 字节，供 XLSX 解析入口重复打开。
func xlsxArchiveBytes(t *testing.T, entries map[string]string) []byte {
	// buffer 保存压缩包完整字节。
	var buffer bytes.Buffer
	// writer 负责创建测试压缩包条目。
	writer := zip.NewWriter(&buffer)
	// name、content 表示当前条目名称和内容。
	for name, content := range entries {
		// file、createErr 保存当前条目写入器及创建错误。
		file, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		// writeErr 保存当前条目内容写入错误。
		if _, writeErr := file.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	// closeErr 保存压缩包目录收尾错误。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return buffer.Bytes()
}
