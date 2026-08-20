package items

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// batchPreviewOwnershipFake 是批量预检使用的账号与卡券归属替身。
type batchPreviewOwnershipFake struct {
	// cookieOwned 表示允许通过预检的账号标识。
	cookieOwned string
	// cardOwned 表示允许通过预检的卡券组标识。
	cardOwned int64
	// err 模拟归属查询故障。
	err error
}

// CookieOwned 返回预置的账号归属结果。
func (fake batchPreviewOwnershipFake) CookieOwned(_ context.Context, _ int64, cookieID string) (bool, error) {
	return cookieID == fake.cookieOwned, fake.err
}

// CardOwned 返回预置的卡券组归属结果。
func (fake batchPreviewOwnershipFake) CardOwned(_ context.Context, _ int64, cardID int64) (bool, error) {
	return cardID == fake.cardOwned, fake.err
}

// batchPreviewImageFake 是批量预检使用的图片引用校验替身。
type batchPreviewImageFake struct {
	// invalid 保存应被拒绝的图片引用。
	invalid string
}

// ValidateImageReference 拒绝预置的图片引用并接受其他引用。
func (fake batchPreviewImageFake) ValidateImageReference(_ string, reference string) error {
	if reference == fake.invalid {
		return errors.New("图片文件不存在: " + reference)
	}
	return nil
}

// TestParseSheetNormalizesCSV 验证 CSV 表头、字段和行上限归一化。
func TestParseSheetNormalizesCSV(t *testing.T) {
	// rows 和 err 表示解析后的字段行及解析错误。
	rows, err := ParseSheet([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,a.png\n"), "products.csv", 2)
	if err != nil {
		t.Fatalf("ParseSheet() error = %v", err)
	}
	if len(rows) != 1 || rows[0]["cookie_id"] != "acc1" || rows[0]["title"] != "商品A" {
		t.Fatalf("ParseSheet() rows = %#v", rows)
	}
}

// TestParseSheetRejectsEmptyAndUnsupportedInput 验证空输入、旧版 XLS 和表头缺行错误。
func TestParseSheetRejectsEmptyAndUnsupportedInput(t *testing.T) {
	// cases 保存不同无效表格输入。
	cases := []struct {
		// name 是当前输入场景名称。
		name string
		// raw 是待解析的表格内容。
		raw []byte
		// filename 是用于选择解析器的文件名。
		filename string
	}{
		{name: "empty", raw: []byte("  \n"), filename: "x.csv"},
		{name: "xls", raw: []byte("x"), filename: "x.xls"},
		{name: "header-only", raw: []byte("标题,价格\n"), filename: "x.csv"},
	}
	// testCase 表示当前无效输入样例。
	for _, testCase := range cases {
		// err 表示当前样例的解析错误。
		if _, err := ParseSheet(testCase.raw, testCase.filename, 0); err == nil {
			t.Errorf("ParseSheet(%s) expected error", testCase.name)
		}
	}
}

// TestParseSheetUsesWorkbookRelationship 验证解析器按 workbook 关系读取唯一表，而不是误取 ZIP 内先出现的旧表。
func TestParseSheetUsesWorkbookRelationship(t *testing.T) {
	// xlsx 保存含旧 sheet1 与当前 sheet9 的单工作表 XLSX；workbook 只声明 sheet9。
	xlsx := buildBatchPreviewXLSX(t, []batchPreviewSheetFixture{{Name: "当前数据", RelationshipID: "rId9", Target: "worksheets/sheet9.xml", Rows: [][]string{{"标题", "价格"}, {"新商品", "9.9"}}}}, map[string][][]string{
		"xl/worksheets/sheet1.xml": {{"标题", "价格"}, {"旧商品", "1.0"}},
	})
	// rows 和 err 分别保存解析结果及异常。
	rows, err := ParseSheet(xlsx, "products.xlsx", 2)
	if err != nil {
		t.Fatalf("ParseSheet() error = %v", err)
	}
	if len(rows) != 1 || rows[0]["title"] != "新商品" {
		t.Fatalf("解析结果误读旧工作表: %#v", rows)
	}
}

// TestParseSheetRejectsMultipleXLSXWorksheets 验证隐藏历史表与可见表并存时会被拒绝，避免预检读取非用户修改内容。
func TestParseSheetRejectsMultipleXLSXWorksheets(t *testing.T) {
	// xlsx 保存两个 workbook 声明的工作表；第二个表代表复制文件遗留的隐藏历史表。
	xlsx := buildBatchPreviewXLSX(t, []batchPreviewSheetFixture{
		{Name: "当前数据", RelationshipID: "rId1", Target: "worksheets/sheet1.xml", Rows: [][]string{{"标题", "价格"}, {"新商品", "9.9"}}},
		{Name: "历史数据", RelationshipID: "rId2", Target: "worksheets/sheet2.xml", State: "hidden", Rows: [][]string{{"标题", "价格"}, {"旧商品", "1.0"}}},
	}, nil)
	// err 保存多工作表输入的预期拒绝错误。
	_, err := ParseSheet(xlsx, "products.xlsx", 2)
	if err == nil || !strings.Contains(err.Error(), "仅支持一个工作表") {
		t.Fatalf("多工作表应被拒绝，err=%v", err)
	}
}

// TestParseSheetRejectsInvalidXLSXWorkbookRelationship 验证 sheet 引用缺少关系目标时不会回退到 ZIP 内任意工作表。
func TestParseSheetRejectsInvalidXLSXWorkbookRelationship(t *testing.T) {
	// xlsx 保存唯一 sheet 声明指向不存在关系的损坏工作簿。
	xlsx := buildBatchPreviewXLSX(t, []batchPreviewSheetFixture{{Name: "当前数据", RelationshipID: "missing", Target: "worksheets/sheet1.xml", Rows: [][]string{{"标题", "价格"}, {"商品", "9.9"}}}}, nil)
	// err 保存损坏关系的预期结构错误。
	_, err := ParseSheet(xlsx, "products.xlsx", 2)
	if err == nil || !strings.Contains(err.Error(), "工作表结构无效") {
		t.Fatalf("无效关系应报结构错误，err=%v", err)
	}
}

// batchPreviewSheetFixture 描述测试 XLSX 中一个 workbook 声明及其对应的单元格文本。
type batchPreviewSheetFixture struct {
	// Name 是 workbook 中显示的工作表名称。
	Name string
	// RelationshipID 是 workbook sheet 节点引用的关系标识。
	RelationshipID string
	// Target 是关系文件中指向 worksheet XML 的相对路径。
	Target string
	// State 是可选的 Excel 工作表可见性状态，用于覆盖隐藏历史表。
	State string
	// Rows 是按行列写入 worksheet 的内联字符串文本。
	Rows [][]string
}

// buildBatchPreviewXLSX 创建带 workbook 关系的最小 XLSX，用于覆盖工作表选择和结构错误场景。
func buildBatchPreviewXLSX(t *testing.T, sheets []batchPreviewSheetFixture, extraParts map[string][][]string) []byte {
	t.Helper()
	// buffer 保存最终 ZIP 格式 XLSX 字节。
	var buffer bytes.Buffer
	// writer 负责写入 XLSX 的 workbook、关系和工作表 XML 分区。
	writer := zip.NewWriter(&buffer)
	// workbookBuilder 保存 workbook.xml 的 sheet 声明。
	var workbookBuilder strings.Builder
	workbookBuilder.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	// relationshipsBuilder 保存 workbook.xml.rels 的关系记录。
	var relationshipsBuilder strings.Builder
	relationshipsBuilder.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	// partName 和 rows 分别表示先写入 ZIP 的历史工作表分区及其旧内容，用于验证不能按物理条目顺序读取。
	for partName, rows := range extraParts {
		writeBatchPreviewXLSXPart(t, writer, partName, batchPreviewWorksheetXML(rows))
	}
	// sheetIndex 和 sheet 分别表示当前 sheet 序号与对应的测试定义。
	for sheetIndex, sheet := range sheets {
		// stateAttribute 保存有隐藏状态时写入 sheet XML 属性的片段。
		stateAttribute := ""
		if sheet.State != "" {
			stateAttribute = fmt.Sprintf(` state=%q`, sheet.State)
		}
		_, _ = fmt.Fprintf(&workbookBuilder, `<sheet name=%q sheetId="%d" r:id=%q%s/>`, sheet.Name, sheetIndex+1, sheet.RelationshipID, stateAttribute)
		if sheet.RelationshipID != "missing" {
			_, _ = fmt.Fprintf(&relationshipsBuilder, `<Relationship Id=%q Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target=%q/>`, sheet.RelationshipID, sheet.Target)
		}
		// partName 保存当前工作表在 ZIP 内的完整分区路径。
		partName := "xl/" + sheet.Target
		writeBatchPreviewXLSXPart(t, writer, partName, batchPreviewWorksheetXML(sheet.Rows))
	}
	workbookBuilder.WriteString(`</sheets></workbook>`)
	relationshipsBuilder.WriteString(`</Relationships>`)
	writeBatchPreviewXLSXPart(t, writer, "xl/workbook.xml", workbookBuilder.String())
	writeBatchPreviewXLSXPart(t, writer, "xl/_rels/workbook.xml.rels", relationshipsBuilder.String())
	// err 保存关闭 ZIP 写入器并写出末尾目录时的错误。
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 XLSX 写入器失败: %v", err)
	}
	return buffer.Bytes()
}

// writeBatchPreviewXLSXPart 在测试 XLSX 中写入一个 XML 分区，任一写入失败都立即终止测试。
func writeBatchPreviewXLSXPart(t *testing.T, writer *zip.Writer, name, content string) {
	t.Helper()
	// part 和 err 分别保存新建 ZIP 分区的写入器及创建错误。
	part, err := writer.Create(name)
	if err != nil {
		t.Fatalf("创建 XLSX 分区失败: %v", err)
	}
	// err 保存将 XML 内容写入当前 ZIP 分区时的错误。
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("写入 XLSX 分区失败: %v", err)
	}
}

// batchPreviewWorksheetXML 把二维单元格文本编码为解析器可读取的内联字符串工作表 XML。
func batchPreviewWorksheetXML(rows [][]string) string {
	// builder 保存逐行拼接的 worksheet XML。
	var builder strings.Builder
	builder.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	// rowIndex 和 row 分别表示当前行的零基下标及单元格集合。
	for rowIndex, row := range rows {
		_, _ = fmt.Fprintf(&builder, `<row r="%d">`, rowIndex+1)
		// columnIndex 和 value 分别表示当前列的零基下标及其可见文本。
		for columnIndex, value := range row {
			// reference 保存 A1 形式的单元格坐标；测试列数很小，仅覆盖单字母列。
			reference := fmt.Sprintf("%c%d", 'A'+rune(columnIndex), rowIndex+1)
			_, _ = fmt.Fprintf(&builder, `<c r=%q t="inlineStr"><is><t>%s</t></is></c>`, reference, value)
		}
		builder.WriteString(`</row>`)
	}
	builder.WriteString(`</sheetData></worksheet>`)
	return builder.String()
}

// TestBatchPreviewValidatesBusinessRules 验证预检服务的归属、金额、类目、图片和自动化规则。
func TestBatchPreviewValidatesBusinessRules(t *testing.T) {
	// service 和 err 表示批量预检服务及构造错误。
	service, err := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "acc1", cardOwned: 9}, batchPreviewImageFake{invalid: "missing.png"})
	if err != nil {
		t.Fatalf("NewBatchPreviewService() error = %v", err)
	}
	// rows 和 err 表示逐行预检结果及执行错误。
	rows, err := service.Preview(context.Background(), BatchPreviewInput{
		UserID: 7, DefaultCookieID: "acc1", UploadDir: "/tmp/uploads",
		FallbackCategory: BatchPreviewCategory{CatID: "5001", CatName: "虚拟商品", ChannelCatID: "6001"},
		Rows: []map[string]any{
			{"标题": "商品A", "价格": "12.50", "图片": "a.png", "付款发货启用": "true", "付款发货内容": "9:2:10"},
			{"账号ID": "other", "标题": "", "价格": "0", "库存": "0", "图片": "missing.png", "类目ID": "1"},
		},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Preview() rows = %#v", rows)
	}
	if len(rows[0].Errors) != 0 || rows[0].CookieID != "acc1" || rows[0].Category.CatID != "5001" || len(rows[0].Automation.PaidDelivery.Actions) != 1 {
		t.Fatalf("valid row = %#v", rows[0])
	}
	// joined 保存无效行的拼接错误文本。
	joined := strings.Join(rows[1].Errors, "|")
	// expected 表示当前必须出现的错误片段。
	for _, expected := range []string{"账号不存在或不属于当前用户", "缺少标题", "价格必须大于 0", "库存必须大于 0", "图片文件不存在", "指定行类目时必须同时填写"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("invalid row errors %q missing %q", joined, expected)
		}
	}
}

// TestBatchPreviewParsesMultipleAutomationActions 验证应用层保留多条卡密动作和开关语义。
func TestBatchPreviewParsesMultipleAutomationActions(t *testing.T) {
	// service 和 err 表示批量预检服务及构造错误。
	service, err := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "acc1", cardOwned: 101}, batchPreviewImageFake{})
	if err != nil {
		t.Fatalf("NewBatchPreviewService() error = %v", err)
	}
	// rows 和 previewErr 表示应用层解析后的逐行结果。
	rows, previewErr := service.Preview(context.Background(), BatchPreviewInput{
		UserID: 7, DefaultCookieID: "acc1", UploadDir: "/tmp/uploads",
		Rows: []map[string]any{{"标题": "商品", "价格": "1", "图片": "a.png", "付款发货启用": "是", "付款发货内容": "101:1:0;102:2:3"}},
	})
	if previewErr != nil || len(rows) != 1 {
		t.Fatalf("Preview() rows=%#v err=%v", rows, previewErr)
	}
	// actions 保存应用层解析出的付款发货动作顺序和参数。
	actions := rows[0].Automation.PaidDelivery.Actions
	if len(actions) != 2 || actions[0].CardID != 101 || actions[1].DeliveryCount != 2 || actions[1].DelaySeconds != 3 {
		t.Fatalf("parsed actions=%#v", actions)
	}
}

// TestBatchPreviewRejectsMissingPorts 验证服务构造时拒绝缺失基础设施 Port。
func TestBatchPreviewRejectsMissingPorts(t *testing.T) {
	// images 和 ownership 是缺失 Port 检查使用的替身。
	images := batchPreviewImageFake{}
	// ownership 是账号与卡券归属查询替身。
	ownership := batchPreviewOwnershipFake{}
	// err 表示缺失归属 Port 的构造结果。
	if _, err := NewBatchPreviewService(nil, images); err == nil {
		t.Error("missing ownership port should fail")
	}
	// err 表示缺失图片 Port 的构造结果。
	if _, err := NewBatchPreviewService(ownership, nil); err == nil {
		t.Error("missing image port should fail")
	}
	// service 和 err 表示完整 Port 构造结果。
	service, err := NewBatchPreviewService(ownership, images)
	if err != nil {
		t.Fatalf("NewBatchPreviewService() error = %v", err)
	}
	// err 表示空输入的预检错误。
	if _, err := service.Preview(context.Background(), BatchPreviewInput{}); !errors.Is(err, ErrBatchPreviewNoRows) {
		t.Fatalf("empty preview error = %v", err)
	}
}
