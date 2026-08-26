package items

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrBatchPreviewNoRows 表示表格没有可供预检的商品行。
var ErrBatchPreviewNoRows = errors.New("表格中没有有效数据行")

// openXLSXPart 负责打开 XLSX 压缩分区；测试可替换它验证压缩算法和读取器错误。
var openXLSXPart = func(file *zip.File) (io.ReadCloser, error) {
	return file.Open()
}

// newBatchPreviewCSVReader 创建批量预检的 CSV 读取器；测试可替换底层输入验证读取错误传播。
var newBatchPreviewCSVReader = func(raw []byte) *csv.Reader {
	return csv.NewReader(bytes.NewReader(raw))
}

// BatchPreviewCategory 是批量预检使用的纯应用类目模型。
type BatchPreviewCategory struct {
	// CatID 是平台类目标识。
	CatID string `json:"cat_id"`
	// CatName 是平台类目名称。
	CatName string `json:"cat_name"`
	// ChannelCatID 是平台频道类目标识。
	ChannelCatID string `json:"channel_cat_id,omitempty"`
	// TBCatID 是可选的淘宝类目标识。
	TBCatID string `json:"tb_cat_id,omitempty"`
}

// BatchPreviewCardAction 是单条自动发货卡券动作。
type BatchPreviewCardAction struct {
	// CardID 是卡券组标识。
	CardID int64 `json:"card_id"`
	// DeliveryCount 是每件商品发送的卡密数量。
	DeliveryCount int `json:"delivery_count"`
	// DelaySeconds 是发送动作的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
}

// BatchPreviewCardAutomation 是自动发货卡券配置。
type BatchPreviewCardAutomation struct {
	// Enabled 表示是否启用该自动发货规则。
	Enabled bool `json:"enabled"`
	// Actions 是按顺序执行的卡券动作。
	Actions []BatchPreviewCardAction `json:"actions"`
	// ParseError 是原始动作文本的解析错误。
	ParseError string `json:"-"`
}

// BatchPreviewReviewRequest 是求评价自动化配置。
type BatchPreviewReviewRequest struct {
	// Enabled 表示是否启用求评价规则。
	Enabled bool `json:"enabled"`
	// AfterShippedHours 是发货后的等待小时数。
	AfterShippedHours int `json:"after_shipped_hours"`
	// Message 是发送给买家的求评价文案。
	Message string `json:"message"`
	// MaxAttempts 是最多提醒次数。
	MaxAttempts int `json:"max_attempts"`
	// DelaySeconds 是提醒动作的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
}

// BatchPreviewAutomation 是发布后自动化配置。
type BatchPreviewAutomation struct {
	// PaidDelivery 是付款后自动发货配置。
	PaidDelivery BatchPreviewCardAutomation `json:"paid_delivery"`
	// ReviewGift 是评价后赠品配置。
	ReviewGift BatchPreviewCardAutomation `json:"review_gift"`
	// ReviewRequest 是超时求评价配置。
	ReviewRequest BatchPreviewReviewRequest `json:"review_request"`
}

// BatchPreviewRow 是表格一行经归一化和校验后的应用模型。
type BatchPreviewRow struct {
	// RowNo 是原始表格中的稳定行号，从表头后一行开始计数。
	RowNo int
	// CookieID 是执行发布的账号标识。
	CookieID string
	// Title 是商品标题。
	Title string
	// Description 是商品描述；为空时回退为标题。
	Description string
	// Price 是商品售价文本。
	Price string
	// OriginalPrice 是商品原价文本。
	OriginalPrice string
	// Quantity 是商品库存数量。
	Quantity int
	// PostageMode 是归一化后的邮费模式。
	PostageMode string
	// Postage 是固定邮费文本。
	Postage string
	// Images 是商品图片引用列表。
	Images []string
	// Category 是行指定或批次兜底类目。
	Category BatchPreviewCategory
	// Automation 是发布后自动化配置。
	Automation BatchPreviewAutomation
	// Errors 是该行所有可展示的预检错误。
	Errors []string
	// Raw 是归一化前的原始字段副本。
	Raw map[string]any
}

// BatchPreviewInput 是批量预检应用服务的输入。
type BatchPreviewInput struct {
	// UserID 是发起预检的用户标识。
	UserID int64
	// DefaultCookieID 是未指定账号行使用的默认账号。
	DefaultCookieID string
	// UploadDir 是本地图片引用校验使用的受控目录。
	UploadDir string
	// FallbackCategory 是未指定行类目使用的批次兜底类目。
	FallbackCategory BatchPreviewCategory
	// Rows 是表格解析得到的字段行。
	Rows []map[string]any
}

// BatchPreviewOwnershipPort 定义预检所需的非敏感归属查询。
type BatchPreviewOwnershipPort interface {
	// CookieOwned 判断账号是否属于指定用户。
	CookieOwned(context.Context, int64, string) (bool, error)
	// CardOwned 判断卡券组是否属于指定用户。
	CardOwned(context.Context, int64, int64) (bool, error)
}

// BatchPreviewImagePort 定义本地图片引用的安全校验能力。
type BatchPreviewImagePort interface {
	// ValidateImageReference 校验本地图片引用是否位于受控目录。
	ValidateImageReference(string, string) error
}

// BatchPreviewService 负责表格行归一化和与基础设施无关的预检规则。
type BatchPreviewService struct {
	// ownership 提供账号和卡券的非敏感归属查询。
	ownership BatchPreviewOwnershipPort
	// images 提供本地图片引用安全校验。
	images BatchPreviewImagePort
}

// CookieOwned 判断账号是否属于用户，供 HTTP 适配器复核批次默认账号。
func (service *BatchPreviewService) CookieOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	if service == nil || service.ownership == nil {
		return false, errors.New("批量预检服务未初始化")
	}
	return service.ownership.CookieOwned(ctx, userID, strings.TrimSpace(cookieID))
}

// NewBatchPreviewService 创建批量预检服务并校验必需 Port。
func NewBatchPreviewService(ownership BatchPreviewOwnershipPort, images BatchPreviewImagePort) (*BatchPreviewService, error) {
	if ownership == nil {
		return nil, errors.New("批量预检归属端口不能为空")
	}
	if images == nil {
		return nil, errors.New("批量预检图片端口不能为空")
	}
	return &BatchPreviewService{ownership: ownership, images: images}, nil
}

// ParseSheet 将 CSV、TSV 或 XLSX 字节解析为归一化字段映射。
func ParseSheet(raw []byte, filename string, maxRows int) ([]map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("导入内容为空")
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		return parseXLSXSheet(raw, maxRows)
	case ".csv":
		return parseDelimitedSheet(raw, ',', maxRows)
	case ".tsv":
		return parseDelimitedSheet(raw, '\t', maxRows)
	case ".xls":
		return nil, errors.New("暂不支持旧版 .xls，请另存为 .xlsx 或 CSV 后导入")
	default:
		return parseDelimitedSheet(raw, ',', maxRows)
	}
}

// Preview 归一化并校验所有表格行，保留逐行错误而不中断其他行。
func (service *BatchPreviewService) Preview(ctx context.Context, input BatchPreviewInput) ([]BatchPreviewRow, error) {
	if service == nil || service.ownership == nil || service.images == nil {
		return nil, errors.New("批量预检服务未初始化")
	}
	if len(input.Rows) == 0 {
		return nil, ErrBatchPreviewNoRows
	}
	// result 保存所有逐行预检结果。
	result := make([]BatchPreviewRow, 0, len(input.Rows))
	// index 和 fields 分别表示表格行索引及其字段。
	for index, fields := range input.Rows {
		// row 保存当前归一化后的预检行。
		row := service.parseRow(ctx, input, index, fields)
		result = append(result, row)
	}
	return result, nil
}

// parseRow 将一条原始字段映射转换为应用行并执行所有确定性校验。
func (service *BatchPreviewService) parseRow(ctx context.Context, input BatchPreviewInput, index int, fields map[string]any) BatchPreviewRow {
	// row 保存当前输入映射转换出的应用行。
	row := BatchPreviewRow{
		RowNo: index + 2, CookieID: firstString(fields, "cookie_id", "账号ID", "账号id", "账号"),
		Title:       firstString(fields, "title", "标题", "商品标题", "商品名称"),
		Description: firstString(fields, "description", "描述", "商品描述", "商品详情"),
		Price:       firstString(fields, "price", "价格", "商品价格"), OriginalPrice: firstString(fields, "original_price", "原价"),
		PostageMode: firstString(fields, "postage_mode", "邮费模式"), Postage: firstString(fields, "postage", "邮费"), Raw: fields,
	}
	if row.CookieID == "" {
		row.CookieID = input.DefaultCookieID
	}
	if row.Description == "" {
		row.Description = row.Title
	}
	if row.PostageMode == "" {
		row.PostageMode = "free"
	}
	row.PostageMode = normalizePostageMode(row.PostageMode)
	row.Quantity = parseIntDefault(firstString(fields, "quantity", "库存", "数量"), 1)
	row.Category = parseCategory(fields, input.FallbackCategory)
	row.Automation = parseAutomation(fields)
	row.Images = splitImageReferences(firstString(fields, "images", "image", "图片", "商品图片"))
	if hasCategoryFields(fields) && (row.Category.CatID == "" || row.Category.CatName == "" || row.Category.ChannelCatID == "") {
		row.Errors = append(row.Errors, "指定行类目时必须同时填写类目ID、类目名称和频道类目ID")
	}
	service.validateRow(ctx, input, &row)
	return row
}

// validateRow 将归属、金额、库存、图片和自动化规则错误写入当前行。
func (service *BatchPreviewService) validateRow(ctx context.Context, input BatchPreviewInput, row *BatchPreviewRow) {
	if row.CookieID == "" {
		row.Errors = append(row.Errors, "缺少账号ID")
	} else { // owned 和 err 表示账号归属查询结果。
		owned, err := service.ownership.CookieOwned(ctx, input.UserID, row.CookieID)
		if err != nil || !owned {
			row.Errors = append(row.Errors, "账号不存在或不属于当前用户")
		}
	}
	if strings.TrimSpace(row.Title) == "" {
		row.Errors = append(row.Errors, "缺少标题")
	}
	// cents 和 err 表示售价的分值及解析错误。
	if cents, err := parseMoneyCents(row.Price); err != nil || cents <= 0 {
		row.Errors = append(row.Errors, "价格必须大于 0")
	}
	if strings.TrimSpace(row.OriginalPrice) != "" {
		// cents 和 err 表示原价的分值及解析错误。
		if cents, err := parseMoneyCents(row.OriginalPrice); err != nil || cents <= 0 {
			row.Errors = append(row.Errors, "原价格式错误")
		}
	}
	if row.Quantity <= 0 {
		row.Errors = append(row.Errors, "库存必须大于 0")
	}
	if row.PostageMode != "free" && row.PostageMode != "fixed" {
		row.Errors = append(row.Errors, "邮费模式必须是 free 或 fixed")
	}
	if row.PostageMode == "fixed" {
		// cents 和 err 表示固定邮费的分值及解析错误。
		if cents, err := parseMoneyCents(row.Postage); err != nil || cents < 0 {
			row.Errors = append(row.Errors, "固定邮费格式错误")
		}
	}
	if len(row.Images) == 0 {
		row.Errors = append(row.Errors, "缺少图片")
	}
	if len(row.Images) > 9 {
		row.Errors = append(row.Errors, "商品图片最多 9 张")
	}
	// reference 表示当前待校验的图片引用。
	for _, reference := range row.Images {
		// err 表示图片引用安全校验错误。
		if err := service.images.ValidateImageReference(input.UploadDir, reference); err != nil {
			row.Errors = append(row.Errors, err.Error())
		}
	}
	row.Errors = append(row.Errors, service.validateAutomation(ctx, input.UserID, row.Automation)...)
}

// validateAutomation 校验卡券归属、动作范围和求评价配置。
func (service *BatchPreviewService) validateAutomation(ctx context.Context, userID int64, config BatchPreviewAutomation) []string {
	// errorsFound 保存当前行的自动化校验提示。
	var errorsFound []string
	// validateCards 校验一组卡券动作及其用户归属。
	validateCards := func(cardConfig BatchPreviewCardAutomation, label string) {
		if !cardConfig.Enabled {
			return
		}
		if cardConfig.ParseError != "" {
			errorsFound = append(errorsFound, label+cardConfig.ParseError)
			return
		}
		if len(cardConfig.Actions) == 0 {
			errorsFound = append(errorsFound, label+"需要至少配置一条发货内容")
			return
		}
		// index 和 action 分别表示动作下标及动作配置。
		for index, action := range cardConfig.Actions {
			// prefix 是当前动作的用户可见错误前缀。
			prefix := fmt.Sprintf("%s第%d项", label, index+1)
			// owned 和 err 表示卡券归属查询结果。
			owned, err := service.ownership.CardOwned(ctx, userID, action.CardID)
			if err != nil || !owned {
				errorsFound = append(errorsFound, prefix+"卡密组不存在或不属于当前用户")
			}
			if action.DeliveryCount <= 0 {
				errorsFound = append(errorsFound, prefix+"每件份数必须大于0")
			}
			if action.DelaySeconds < 0 || action.DelaySeconds > 3600 {
				errorsFound = append(errorsFound, prefix+"延迟秒必须在 0 到 3600 之间")
			}
		}
	}
	validateCards(config.PaidDelivery, "付款发货")
	validateCards(config.ReviewGift, "评价赠品")
	if config.ReviewRequest.Enabled {
		if config.ReviewRequest.AfterShippedHours <= 0 {
			errorsFound = append(errorsFound, "求评价等待小时必须大于 0")
		}
		if strings.TrimSpace(config.ReviewRequest.Message) == "" {
			errorsFound = append(errorsFound, "求评价文案不能为空")
		}
		if config.ReviewRequest.MaxAttempts <= 0 {
			errorsFound = append(errorsFound, "求评价最多次数必须大于 0")
		}
	}
	return errorsFound
}

// parseDelimitedSheet 解析带逗号或制表符分隔的表格内容。
func parseDelimitedSheet(raw []byte, delimiter rune, maxRows int) ([]map[string]any, error) {
	// reader 负责读取分隔符表格。
	reader := newBatchPreviewCSVReader(raw)
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	// header 和 err 表示表头及读取错误。
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("解析表格失败: %w", err)
	}
	// keys 保存归一化后的表头名称。
	keys := normalizeHeaders(header)
	// rows 保存非空数据行。
	rows := make([]map[string]any, 0, 64)
	// seenDataRow 表示是否读取到表头之后的记录。
	seenDataRow := false
	for {
		// record 和 readErr 表示当前记录及其读取错误。
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("解析表格失败: %w", readErr)
		}
		seenDataRow = true
		// row 和 nonEmpty 表示当前字段映射及是否包含数据。
		row, nonEmpty := rowMap(keys, record)
		if !nonEmpty {
			continue
		}
		rows = append(rows, row)
		if maxRows > 0 && len(rows) > maxRows {
			return nil, fmt.Errorf("单次最多解析 %d 行数据", maxRows)
		}
	}
	if !seenDataRow {
		return nil, errors.New("表格至少需要表头和一行数据")
	}
	return rows, nil
}

// parseXLSXSheet 解析 XLSX 唯一工作表中的单元格文本；多工作表文件会被拒绝以避免误读历史数据。
func parseXLSXSheet(raw []byte, maxRows int) ([]map[string]any, error) {
	// archive 和 err 表示 XLSX 压缩包及打开错误。
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("解析 xlsx 失败: %w", err)
	}
	// shared 和 err 表示共享字符串表及读取错误。
	shared, err := xlsxSharedStrings(archive)
	if err != nil {
		return nil, err
	}
	// sheet 保存由 workbook 关系明确定位的唯一工作表，不能按 ZIP 条目顺序猜测用户编辑的数据页。
	sheet, err := xlsxSingleWorksheet(archive)
	if err != nil {
		return nil, err
	}
	// part 和 err 表示工作表 XML 内容及读取错误。
	part, err := readXLSXPart(sheet)
	if err != nil {
		return nil, err
	}
	// worksheet 保存反序列化后的工作表结构。
	var worksheet xlsxWorksheet
	// err 表示工作表 XML 反序列化错误。
	if err := xml.Unmarshal(part, &worksheet); err != nil {
		return nil, fmt.Errorf("解析工作表失败: %w", err)
	}
	// rows 保存工作表的文本行。
	rows := make([][]string, 0, len(worksheet.SheetData.Rows))
	// sheetRow 表示当前工作表行。
	for _, sheetRow := range worksheet.SheetData.Rows {
		// values 保存当前行按列展开的文本值。
		values := []string{}
		// cell 表示当前行的单元格。
		for _, cell := range sheetRow.Cells {
			// index 表示单元格的零基列号。
			index := xlsxCellIndex(cell.Ref)
			for len(values) <= index {
				values = append(values, "")
			}
			values[index] = xlsxCellValue(cell, shared)
		}
		rows = append(rows, values)
	}
	if len(rows) < 2 {
		return nil, errors.New("xlsx 至少需要表头和一行数据")
	}
	// keys 保存归一化后的表头名称。
	keys := normalizeHeaders(rows[0])
	// result 保存非空的应用字段行。
	result := make([]map[string]any, 0, len(rows)-1)
	// values 表示当前数据行的单元格文本。
	for _, values := range rows[1:] {
		// row 和 nonEmpty 表示当前字段映射及是否包含数据。
		row, nonEmpty := rowMap(keys, values)
		if nonEmpty {
			result = append(result, row)
		}
	}
	if maxRows > 0 && len(result) > maxRows {
		return nil, fmt.Errorf("单次最多解析 %d 行数据", maxRows)
	}
	return result, nil
}

// xlsxWorksheet 保存 XLSX 工作表结构。
type xlsxWorksheet struct {
	// SheetData 是工作表中的行集合。
	SheetData struct {
		// Rows 是工作表行集合。
		Rows []xlsxRow `xml:"row"`
	} `xml:"sheetData"`
}

// xlsxRow 保存一行 XLSX 单元格。
type xlsxRow struct {
	// Cells 是当前行的非空单元格集合。
	Cells []xlsxCell `xml:"c"`
}

// xlsxCell 保存 XLSX 单元格原始值。
type xlsxCell struct {
	// Ref 是单元格引用，如 A1。
	Ref string `xml:"r,attr"`
	// Type 是 XLSX 单元格类型。
	Type string `xml:"t,attr"`
	// Value 是普通或共享字符串值。
	Value string `xml:"v"`
	// InlineStr 是内联字符串值。
	InlineStr string `xml:"is>t"`
}

// xlsxSST 保存 XLSX 共享字符串表。
type xlsxSST struct {
	// Items 是共享字符串条目。
	Items []struct {
		// Inner 是富文本条目的 XML 内容。
		Inner string `xml:",innerxml"`
	} `xml:"si"`
}

// xlsxSharedStrings 读取 XLSX 共享字符串表。
func xlsxSharedStrings(archive *zip.Reader) ([]string, error) {
	// file 表示当前遍历到的共享字符串压缩条目。
	for _, file := range archive.File {
		if file.Name != "xl/sharedStrings.xml" {
			continue
		}
		// raw 和 err 表示共享字符串 XML 内容及读取错误。
		raw, err := readXLSXPart(file)
		if err != nil {
			return nil, err
		}
		// table 保存反序列化后的共享字符串表。
		var table xlsxSST
		// err 表示共享字符串 XML 反序列化错误。
		if err := xml.Unmarshal(raw, &table); err != nil {
			return nil, err
		}
		// values 保存可直接引用的共享字符串。
		values := make([]string, 0, len(table.Items))
		// item 表示当前共享字符串条目。
		for _, item := range table.Items {
			values = append(values, xmlCharData(item.Inner))
		}
		return values, nil
	}
	return nil, nil
}

// readXLSXPart 读取单个 XLSX XML 部件并限制其大小。
func readXLSXPart(file *zip.File) ([]byte, error) {
	// reader 和 err 表示 XML 部件读取器及打开错误。
	reader, err := openXLSXPart(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	// maxXMLPartBytes 限制单个 XML 部件的内存读取规模，并保持上传解析的 32 MiB 上限。
	const maxXMLPartBytes = 32 << 20
	// raw 和 err 表示 XML 部件内容及读取错误。
	raw, err := io.ReadAll(io.LimitReader(reader, maxXMLPartBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxXMLPartBytes {
		return nil, fmt.Errorf("xlsx 内部 XML 超过 %d MiB", maxXMLPartBytes>>20)
	}
	return raw, nil
}

// xlsxCellValue 按单元格类型提取可用于预检的文本值。
func xlsxCellValue(cell xlsxCell, shared []string) string {
	switch cell.Type {
	case "s":
		// index 表示共享字符串表中的索引。
		index, _ := strconv.Atoi(strings.TrimSpace(cell.Value))
		if index >= 0 && index < len(shared) {
			return shared[index]
		}
	case "inlineStr":
		return strings.TrimSpace(cell.InlineStr)
	}
	return strings.TrimSpace(cell.Value)
}

// xlsxCellIndex 将 XLSX 单元格引用转换为零基列索引。
func xlsxCellIndex(reference string) int {
	// index 保存转换后的列索引。
	index := 0
	// char 表示单元格引用中的当前列字符。
	for _, char := range reference {
		if char < 'A' || char > 'Z' {
			break
		}
		index = index*26 + int(char-'A'+1)
	}
	if index == 0 {
		return 0
	}
	return index - 1
}

// xmlCharData 从富文本 XML 中拼接所有字符数据。
func xmlCharData(inner string) string {
	// decoder 负责读取富文本 XML 字符数据。
	decoder := xml.NewDecoder(strings.NewReader("<x>" + inner + "</x>"))
	// parts 保存拼接前的字符片段。
	var parts []string
	for {
		// token 和 err 表示当前 XML 令牌及读取错误。
		token, err := decoder.Token()
		if err != nil {
			break
		}
		// character 和 ok 表示字符数据及类型判断结果。
		if character, ok := token.(xml.CharData); ok {
			parts = append(parts, string(character))
		}
	}
	return strings.Join(parts, "")
}

// parseIntDefault 将数字文本转换为整数，失败时返回默认值。
func parseIntDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	// value 和 err 表示解析后的数字及转换错误。
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return fallback
	}
	return int(value)
}

// normalizePostageMode 将用户邮费模式映射为平台使用的稳定值。
func normalizePostageMode(raw string) string {
	// value 保存归一化前的邮费模式文本。
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "包邮", "free_shipping":
		return "free"
	case "固定邮费", "一口价邮费":
		return "fixed"
	default:
		return value
	}
}

// parseCategory 解析行类目并在未指定时使用批次兜底类目。
func parseCategory(fields map[string]any, fallback BatchPreviewCategory) BatchPreviewCategory {
	// category 保存行指定或兜底前的类目配置。
	category := BatchPreviewCategory{CatID: firstString(fields, "category_id", "类目ID", "商品类目ID"), CatName: firstString(fields, "category_name", "类目名称", "商品类目名称", "类目"), ChannelCatID: firstString(fields, "channel_category_id", "频道类目ID"), TBCatID: firstString(fields, "tb_category_id", "淘宝类目ID")}
	if category.CatID == "" && category.CatName == "" && category.ChannelCatID == "" && category.TBCatID == "" {
		return fallback
	}
	return category
}

// hasCategoryFields 判断原始行是否显式提供了类目字段。
func hasCategoryFields(fields map[string]any) bool {
	return firstString(fields, "category_id", "类目ID", "商品类目ID") != "" || firstString(fields, "category_name", "类目名称", "商品类目名称", "类目") != "" || firstString(fields, "channel_category_id", "频道类目ID") != "" || firstString(fields, "tb_category_id", "淘宝类目ID") != ""
}

// parseAutomation 解析表格中的自动化配置文本。
func parseAutomation(fields map[string]any) BatchPreviewAutomation {
	// paidActions 和 paidError 表示付款发货动作及解析错误。
	paidActions, paidError := parseCardActions(firstString(fields, "paid_delivery_contents", "付款发货内容"))
	// reviewActions 和 reviewError 表示评价赠品动作及解析错误。
	reviewActions, reviewError := parseCardActions(firstString(fields, "review_gift_contents", "评价赠品内容"))
	return BatchPreviewAutomation{
		PaidDelivery:  BatchPreviewCardAutomation{Enabled: parseBool(firstString(fields, "paid_delivery_enabled", "付款发货启用")), Actions: paidActions, ParseError: paidError},
		ReviewGift:    BatchPreviewCardAutomation{Enabled: parseBool(firstString(fields, "review_gift_enabled", "评价赠品启用")), Actions: reviewActions, ParseError: reviewError},
		ReviewRequest: BatchPreviewReviewRequest{Enabled: parseBool(firstString(fields, "review_request_enabled", "求评价启用")), AfterShippedHours: parseIntDefault(firstString(fields, "review_request_after_hours", "求评价等待小时"), 72), Message: firstString(fields, "review_request_message", "求评价文案"), MaxAttempts: parseIntDefault(firstString(fields, "review_request_max_attempts", "求评价最多次数"), 1), DelaySeconds: parseIntDefault(firstString(fields, "review_request_delay_seconds", "求评价延迟秒"), 0)},
	}
}

// parseCardActions 解析卡券组 ID、数量和延迟秒组成的动作文本。
func parseCardActions(raw string) ([]BatchPreviewCardAction, string) {
	// entries 保存按分隔符拆出的卡券动作文本。
	entries := strings.FieldsFunc(strings.TrimSpace(raw), func(char rune) bool { return char == ';' || char == '；' || char == '\n' || char == '\r' })
	if len(entries) == 0 {
		return nil, ""
	}
	// actions 保存解析后的卡券动作。
	actions := make([]BatchPreviewCardAction, 0, len(entries))
	// index 和 entry 表示动作序号及原始动作文本。
	for index, entry := range entries {
		// parts 保存动作的 ID、数量和延迟字段。
		parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(entry), "：", ":"), ":")
		if len(parts) < 1 || len(parts) > 3 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Sprintf("第%d项格式错误，应为 卡密组ID:每件份数:延迟秒", index+1)
		}
		// cardID 和 err 表示卡券组标识及解析错误。
		cardID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil || cardID <= 0 {
			return nil, fmt.Sprintf("第%d项卡密组ID无效", index+1)
		}
		// count 和 delay 保存默认的每件数量及延迟秒数。
		count, delay := 1, 0
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			count, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || count <= 0 {
				return nil, fmt.Sprintf("第%d项每件份数必须大于0", index+1)
			}
		}
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			delay, err = strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil || delay < 0 {
				return nil, fmt.Sprintf("第%d项延迟秒不能小于0", index+1)
			}
		}
		actions = append(actions, BatchPreviewCardAction{CardID: cardID, DeliveryCount: count, DelaySeconds: delay})
	}
	return actions, ""
}

// parseBool 将常见真值文本转换为布尔值。
func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on", "是", "开启", "启用":
		return true
	default:
		return false
	}
}

// splitImageReferences 将分号或换行分隔的图片引用转换为列表。
func splitImageReferences(raw string) []string {
	// value 保存统一使用分号分隔的图片引用文本。
	value := strings.ReplaceAll(strings.ReplaceAll(raw, "\n", ";"), "；", ";")
	// parts 保存拆分后的原始图片引用。
	parts := strings.Split(value, ";")
	// result 保存去除空白后的图片引用。
	result := make([]string, 0, len(parts))
	// part 表示当前原始图片引用。
	for _, part := range parts {
		// text 表示当前图片引用的裁剪文本。
		if text := strings.TrimSpace(part); text != "" {
			result = append(result, text)
		}
	}
	return result
}

// parseMoneyCents 将金额文本转换为分值并拒绝超过两位小数的输入。
func parseMoneyCents(raw string) (int64, error) {
	// value 保存移除货币符号后的金额文本。
	value := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "¥"), "￥")
	if value == "" {
		return 0, nil
	}
	// sign 保存金额正负号。
	sign := int64(1)
	if strings.HasPrefix(value, "-") {
		sign, value = -1, strings.TrimPrefix(value, "-")
	} else {
		value = strings.TrimPrefix(value, "+")
	}
	// parts 保存整数和小数部分。
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return 0, errors.New("金额格式错误")
	}
	// yuan 和 err 表示整数元及解析错误。
	yuan, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, err
	}
	// frac 保存最多两位的小数部分。
	frac := ""
	if len(parts) == 2 {
		frac = strings.TrimSpace(parts[1])
		if len(frac) > 2 {
			return 0, errors.New("金额最多支持两位小数")
		}
	}
	for len(frac) < 2 {
		frac += "0"
	}
	// cents 保存最终的分值结果。
	cents := int64(0)
	if frac != "" {
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return sign * (yuan*100 + cents), nil
}
