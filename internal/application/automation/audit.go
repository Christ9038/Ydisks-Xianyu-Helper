package automation

import (
	"context"
	"strings"
)

const (
	// DefaultAuditPageSize 是规则日志分页的默认每页数量。
	DefaultAuditPageSize = 20
	// MaxAuditPageSize 是规则日志分页允许的最大每页数量。
	MaxAuditPageSize = 100
)

// auditStatuses 是规则日志允许过滤的状态集合，与审计表状态一一对应。
var auditStatuses = map[string]struct{}{
	"queued": {}, "running": {}, "success": {}, "failed": {},
	"skipped": {}, "needs_review": {}, "canceled": {},
}

// AuditEvent 是应用层的自动化行为审计事件，只含非敏感摘要，不含卡密正文、消息正文或凭证。
type AuditEvent struct {
	// ID 是事件标识。
	ID int64
	// CookieID 是触发事件的账号标识。
	CookieID string
	// OrderID 与 ItemID 是关联的订单和商品，可为空。
	OrderID string
	ItemID  string
	// RuleID 与 RunID 是关联规则和运行，未创建运行的跳过事件为零。
	RuleID int64
	RunID  int64
	// TriggerType 是触发类型。
	TriggerType string
	// Source 是事件来源：ws、scheduler、recovery 或 manual。
	Source string
	// EventType 是行为分类。
	EventType string
	// Status 是事件状态：queued、running、success、failed、skipped、needs_review 或 canceled。
	Status string
	// ActionType 是动作类型，运行级事件为空。
	ActionType string
	// Summary 是不含敏感内容的摘要。
	Summary string
	// Reason 是跳过或失败原因。
	Reason string
	// CreatedAt 是事件发生时刻（Unix 秒）。
	CreatedAt int64
}

// AuditQuery 是传给仓储的已规范化审计查询。
type AuditQuery struct {
	// UserID 是必填的所有者隔离条件。
	UserID int64
	// CookieID、Status、TriggerType 为空时不过滤对应维度。
	CookieID    string
	Status      string
	TriggerType string
	// RunID 大于零时只返回某次运行的时间线。
	RunID int64
	// Search 按订单号子串匹配。
	Search string
	// Limit 与 Offset 是已规范化的分页参数。
	Limit  int
	Offset int
	// Ascending 为 true 时按时间正序，用于运行时间线。
	Ascending bool
}

// AuditPage 是一页审计事件及符合条件的总数。
type AuditPage struct {
	// Events 是当前页事件。
	Events []AuditEvent
	// Total 是符合条件的事件总数。
	Total int64
	// Page 与 PageSize 是规范化后的分页参数。
	Page     int
	PageSize int
}

// AuditListInput 是调用方提交的规则日志查询条件。
type AuditListInput struct {
	// CookieID、Status、TriggerType 为空表示不过滤。
	CookieID    string
	Status      string
	TriggerType string
	// RunID 大于零时返回该运行的时间线（正序）。
	RunID int64
	// Search 是订单号搜索词。
	Search string
	// Page 从 1 开始，非法值按 1 处理。
	Page int
	// PageSize 非法或超限时取默认或最大值。
	PageSize int
}

// AuditRepository 定义规则日志用例所需的最小持久化能力。
type AuditRepository interface {
	// ListAuditEvents 按用户归属分页查询审计事件。
	ListAuditEvents(ctx context.Context, query AuditQuery) (AuditPage, error)
}

// AuditService 编排规则日志查询，不持有 HTTP 请求或数据库连接。
type AuditService struct {
	// repository 保存调用方注入的审计持久化端口。
	repository AuditRepository
}

// NewAuditService 构造规则日志应用服务；空端口会在调用时返回参数错误。
func NewAuditService(repository AuditRepository) *AuditService {
	return &AuditService{repository: repository}
}

// ListEvents 分页查询当前用户的规则日志；状态必须是已知状态，分页参数会被规范化。
func (s *AuditService) ListEvents(ctx context.Context, userID int64, input AuditListInput) (AuditPage, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return AuditPage{}, ErrInvalidInput
	}
	// status 是去除空白后的状态过滤条件，非空时必须属于已知状态。
	status := strings.TrimSpace(input.Status)
	// known 表示状态过滤值是否命中已知状态集合。
	if _, known := auditStatuses[status]; status != "" && !known {
		return AuditPage{}, ErrInvalidInput
	}
	// page、pageSize 是规范化后的分页参数。
	page, pageSize := input.Page, input.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultAuditPageSize
	}
	if pageSize > MaxAuditPageSize {
		pageSize = MaxAuditPageSize
	}
	// result、err 保存仓储返回的一页事件及错误。
	result, err := s.repository.ListAuditEvents(ctx, AuditQuery{
		UserID: userID, CookieID: strings.TrimSpace(input.CookieID), Status: status,
		TriggerType: strings.TrimSpace(input.TriggerType), RunID: input.RunID,
		Search: strings.TrimSpace(input.Search), Limit: pageSize, Offset: (page - 1) * pageSize,
		Ascending: input.RunID > 0,
	})
	if err != nil {
		return AuditPage{}, err
	}
	result.Page, result.PageSize = page, pageSize
	return result, nil
}
