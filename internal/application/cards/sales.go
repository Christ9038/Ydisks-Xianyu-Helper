package cards

import (
	"bytes"
	"context"
	"encoding/csv"
	"strconv"
	"strings"
	"time"
)

const (
	// SaleStatusReserved 表示卡密已从库存扣除但买家是否收到尚未确认，需要人工核对。
	SaleStatusReserved = "reserved"
	// SaleStatusSold 表示卡密已确认发给买家。
	SaleStatusSold = "sold"

	// DefaultSalePageSize 是售出记录分页的默认每页数量。
	DefaultSalePageSize = 20
	// MaxSalePageSize 是售出记录分页允许的最大每页数量。
	MaxSalePageSize = 100
)

// ExportScopeUnsold 与 ExportScopeSold 是导出范围：未售导出当前库存，已售导出售出台账。
const (
	ExportScopeUnsold = "unsold"
	ExportScopeSold   = "sold"
)

// SaleRecord 是应用层的卡密售出记录，Content 为已发出的卡密明文，只允许所有者读取。
type SaleRecord struct {
	// ID 是售出记录标识。
	ID int64
	// CardID 是售出时的卡密组标识；卡密组之后被删除时仍保留。
	CardID int64
	// CardName 与 CardType 是售出时的卡密组名称和类型快照。
	CardName string
	CardType string
	// Content 是实际发给买家的卡密正文或 API 返回内容。
	Content string
	// Status 取 SaleStatusReserved 或 SaleStatusSold。
	Status string
	// CookieID、OrderID、ItemID、BuyerID 是售出时的订单上下文。
	CookieID string
	OrderID  string
	ItemID   string
	BuyerID  string
	// CreatedAt 是扣减库存时刻，SoldAt 是确认售出时刻，均为 Unix 秒，未确认时 SoldAt 为零。
	CreatedAt int64
	SoldAt    int64
}

// SaleListQuery 是传给仓储的已规范化分页查询。
type SaleListQuery struct {
	// CardID 限定卡密组，必须大于零。
	CardID int64
	// Status 为空表示不限状态。
	Status string
	// Search 按订单号精确或卡密内容子串搜索。
	Search string
	// Limit 与 Offset 是已规范化的分页参数。
	Limit  int
	Offset int
}

// SalePage 是一页售出记录及符合条件的总数。
type SalePage struct {
	// Records 是当前页记录，按售出先后倒序。
	Records []SaleRecord
	// Total 是符合条件的记录总数。
	Total int64
	// Page 与 PageSize 是规范化后的分页参数。
	Page     int
	PageSize int
}

// SaleListInput 是调用方提交的售出记录查询条件。
type SaleListInput struct {
	// Status 为空、reserved 或 sold。
	Status string
	// Search 是订单号或卡密内容搜索词。
	Search string
	// Page 从 1 开始，非法值按 1 处理。
	Page int
	// PageSize 非法或超限时取默认或最大值。
	PageSize int
}

// Export 是一次导出生成的文件内容。
type Export struct {
	// Filename 是建议的下载文件名，包含卡密组名称和范围。
	Filename string
	// ContentType 是 text/plain 或 text/csv，均为 UTF-8。
	ContentType string
	// Body 是完整文件内容；CSV 带 UTF-8 BOM 以便 Excel 正确识别中文。
	Body []byte
}

// ListSaleRecords 分页查询 cardID 的售出记录；卡密组必须属于 userID。
func (s *Service) ListSaleRecords(ctx context.Context, userID, cardID int64, input SaleListInput) (SalePage, error) {
	// err 保存用户校验错误，用户非法时拒绝查询。
	if err := s.validateUser(userID); err != nil {
		return SalePage{}, err
	}
	// err 保存卡密组归属校验错误，卡密组不属于该用户时拒绝查询。
	if _, err := s.ownedCard(ctx, userID, cardID); err != nil {
		return SalePage{}, err
	}
	if input.Status != "" && input.Status != SaleStatusReserved && input.Status != SaleStatusSold {
		return SalePage{}, &ValidationError{Message: "状态必须为 reserved 或 sold"}
	}
	// page、pageSize 是规范化后的分页参数。
	page, pageSize := input.Page, input.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultSalePageSize
	}
	if pageSize > MaxSalePageSize {
		pageSize = MaxSalePageSize
	}
	// result、err 保存仓储返回的一页记录及错误。
	result, err := s.repository.ListSaleRecords(ctx, userID, SaleListQuery{
		CardID: cardID, Status: input.Status, Search: strings.TrimSpace(input.Search),
		Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return SalePage{}, err
	}
	result.Page, result.PageSize = page, pageSize
	return result, nil
}

// ExportUnsold 导出 data 卡密组当前未售库存，一行一条，可直接再次导入；非 data 类型返回 ErrNotDataType。
func (s *Service) ExportUnsold(ctx context.Context, userID, cardID int64) (Export, error) {
	// err 保存用户校验错误，用户非法时拒绝导出。
	if err := s.validateUser(userID); err != nil {
		return Export{}, err
	}
	// card、err 保存已通过归属校验的卡密组。
	card, err := s.ownedCard(ctx, userID, cardID)
	if err != nil {
		return Export{}, err
	}
	if card.Type != "data" {
		return Export{}, ErrNotDataType
	}
	// body 保存去掉空行并统一换行后的库存。
	var body bytes.Buffer
	for /* line 是库存中的一行 */ _, line := range strings.Split(strings.ReplaceAll(card.DataContent, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			body.WriteString(strings.TrimSpace(line))
			body.WriteString("\n")
		}
	}
	return Export{Filename: exportFilename(card.Name, "未售", "txt"), ContentType: "text/plain; charset=utf-8", Body: body.Bytes()}, nil
}

// ExportSold 导出卡密组的全部售出记录为 CSV；API 与 data 类型都支持。
func (s *Service) ExportSold(ctx context.Context, userID, cardID int64) (Export, error) {
	// err 保存用户校验错误，用户非法时拒绝导出。
	if err := s.validateUser(userID); err != nil {
		return Export{}, err
	}
	// card、err 保存通过归属校验的卡密组及校验错误。
	card, err := s.ownedCard(ctx, userID, cardID)
	if err != nil {
		return Export{}, err
	}
	// records、listErr 保存卡密组的全部售出记录，按售出先后排列。
	records, listErr := s.repository.AllSaleRecords(ctx, userID, cardID)
	if listErr != nil {
		return Export{}, listErr
	}
	// body、writeErr 保存 CSV 内容及写入错误。
	body, writeErr := buildSoldCSV(records)
	if writeErr != nil {
		return Export{}, writeErr
	}
	return Export{Filename: exportFilename(card.Name, "已售", "csv"), ContentType: "text/csv; charset=utf-8", Body: body}, nil
}

// SaleRecordsByOrder 返回某账号某订单已发出的卡密；调用方必须已确认订单属于 userID。
func (s *Service) SaleRecordsByOrder(ctx context.Context, userID int64, cookieID, orderID string) ([]SaleRecord, error) {
	// err 保存用户校验错误，用户非法时拒绝查询。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cookieID) == "" || strings.TrimSpace(orderID) == "" {
		return nil, &ValidationError{Message: "账号和订单号不能为空"}
	}
	return s.repository.SaleRecordsByOrder(ctx, userID, cookieID, orderID)
}

// saleCSVHeader 是已售导出的列头；时间按北京时间展示。
var saleCSVHeader = []string{"卡密", "状态", "卡密组", "账号", "订单号", "商品ID", "买家ID", "售出时间(北京时间)"}

// beijingZone 是导出时间使用的固定 UTC+8 时区，避免容器默认 UTC 造成时间偏差。
var beijingZone = time.FixedZone("UTC+8", 8*3600)

// buildSoldCSV 生成带 UTF-8 BOM 的已售记录 CSV；所有文本单元格都做公式注入防护。
func buildSoldCSV(records []SaleRecord) ([]byte, error) {
	// buffer 保存 BOM 与 CSV 正文。
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	// writer 是 CSV 编码器，写入 buffer 并负责逗号与引号转义。
	writer := csv.NewWriter(&buffer)
	// err 保存表头写入错误。
	if err := writer.Write(saleCSVHeader); err != nil {
		return nil, err
	}
	for /* record 是一条售出记录 */ _, record := range records {
		if err := writer.Write([]string{
			csvSafe(record.Content), saleStatusLabel(record.Status), csvSafe(record.CardName), csvSafe(record.CookieID),
			csvSafe(record.OrderID), csvSafe(record.ItemID), csvSafe(record.BuyerID), formatSaleTime(record),
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}

// saleStatusLabel 把台账状态转换为导出中的中文标签。
func saleStatusLabel(status string) string {
	if status == SaleStatusSold {
		return "已售"
	}
	return "待核对"
}

// formatSaleTime 返回已售时间；待核对记录没有确认时间，退回扣库存时间并保持同一格式。
func formatSaleTime(record SaleRecord) string {
	// moment 是展示用的 Unix 秒。
	moment := record.SoldAt
	if moment == 0 {
		moment = record.CreatedAt
	}
	if moment == 0 {
		return ""
	}
	return time.Unix(moment, 0).In(beijingZone).Format("2006-01-02 15:04:05")
}

// csvSafe 防止电子表格把以公式前缀开头的单元格当作公式执行：前缀 `'` 强制按文本显示。
func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

// exportFilename 生成 "<卡密组名>-<范围>-<日期>.<扩展名>" 形式的下载文件名，并去掉路径与控制字符。
func exportFilename(name, scope, extension string) string {
	// cleaned 去掉文件名中不安全或有特殊含义的字符。
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if cleaned == "" {
		cleaned = "卡密" + strconv.FormatInt(time.Now().Unix(), 10)
	}
	return cleaned + "-" + scope + "-" + time.Now().In(beijingZone).Format("20060102") + "." + extension
}
