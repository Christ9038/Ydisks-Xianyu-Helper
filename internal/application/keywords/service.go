// Package keywords 提供关键词回复和指定商品回复的应用层用例。
// 本包只依赖消费者定义的持久化 Port，不依赖 HTTP、数据库模型或具体数据库实现。
package keywords

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidInput 表示关键词回复用例缺少有效的用户、账号或请求参数。
var ErrInvalidInput = errors.New("关键词回复参数无效")

// ErrInvalidUser 表示调用方没有提供正数用户标识。
var ErrInvalidUser = errors.New("关键词回复用户身份无效")

// ErrNotFound 表示目标账号、关键词或指定商品回复不存在。
var ErrNotFound = errors.New("关键词回复不存在")

// ErrForbidden 表示目标资源存在但不属于当前用户。
var ErrForbidden = errors.New("无权操作该关键词回复")

// ValidationError 表示可安全展示给 HTTP 调用方的稳定输入错误。
type ValidationError struct {
	// Message 是不包含数据库或凭证信息的用户提示。
	Message string
}

// Error 返回稳定的输入错误提示。
func (e *ValidationError) Error() string {
	if e == nil || e.Message == "" {
		return "关键词回复输入无效"
	}
	return e.Message
}

// Keyword 是关键词回复的应用层模型，不携带数据库连接或敏感凭证。
type Keyword struct {
	// ID 是关键词规则的持久化标识。
	ID int64
	// CookieID 是规则所属账号标识。
	CookieID string
	// Keyword 是触发匹配文本。
	Keyword string
	// Reply 是文字回复内容。
	Reply string
	// ItemID 是可选的商品范围标识。
	ItemID string
	// Type 是 text 或 image 回复类型。
	Type string
	// ImageURL 是 image 类型回复使用的图片地址。
	ImageURL string
}

// Draft 是创建、更新或批量替换关键词规则的业务输入。
type Draft struct {
	// Keyword 是触发匹配文本。
	Keyword string
	// Reply 是文字回复内容。
	Reply string
	// ItemID 是可选的商品范围标识。
	ItemID string
	// Type 是 text 或 image 回复类型；空值按 text 处理。
	Type string
	// ImageURL 是 image 类型回复使用的图片地址。
	ImageURL string
}

// ItemReply 是指定商品回复的应用层模型。
type ItemReply struct {
	// ItemID 是指定商品标识。
	ItemID string
	// CookieID 是回复所属账号标识。
	CookieID string
	// ReplyContent 是商品命中后的回复正文。
	ReplyContent string
}

// Repository 定义关键词用例所需的最小持久化能力。
// userID 必须由实现用于归属隔离，避免应用层把跨用户资源交给数据库操作。
type Repository interface {
	// List 返回指定用户账号的关键词规则。
	List(ctx context.Context, userID int64, cookieID string) ([]Keyword, error)
	// Add 创建一条已规范化的关键词规则。
	Add(ctx context.Context, userID int64, cookieID string, draft Draft) (int64, error)
	// Replace 删除并重建指定用户账号的全部关键词规则。
	Replace(ctx context.Context, userID int64, cookieID string, drafts []Draft) error
	// Update 更新指定用户账号中的关键词规则。
	Update(ctx context.Context, userID int64, cookieID string, id int64, draft Draft) error
	// DeleteByID 按持久化标识删除指定用户账号中的关键词规则。
	DeleteByID(ctx context.Context, userID int64, cookieID string, id int64) error
	// DeleteByIndex 按稳定 ID 顺序的零基索引删除规则。
	DeleteByIndex(ctx context.Context, userID int64, cookieID string, index int) error
	// ListItemReplies 返回指定用户全部账号的商品回复。
	ListItemReplies(ctx context.Context, userID int64) ([]ItemReply, error)
	// GetItemReply 读取指定用户账号和商品的回复。
	GetItemReply(ctx context.Context, userID int64, cookieID, itemID string) (ItemReply, error)
	// SetItemReply 覆盖指定用户账号和商品的回复。
	SetItemReply(ctx context.Context, userID int64, cookieID, itemID, content string) error
	// DeleteItemReply 删除指定用户账号和商品的回复。
	DeleteItemReply(ctx context.Context, userID int64, cookieID, itemID string) error
}

// Service 编排关键词输入校验、账号归属和持久化操作。
type Service struct {
	// repository 保存由适配器实现的最小关键词持久化 Port。
	repository Repository
}

// NewService 创建关键词回复应用服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// List 查询指定用户账号的全部关键词规则。
func (s *Service) List(ctx context.Context, userID int64, cookieID string) ([]Keyword, error) {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return nil, err
	}
	return s.repository.List(ctx, userID, cookieID)
}

// Add 校验并创建一条关键词规则。
func (s *Service) Add(ctx context.Context, userID int64, cookieID string, draft Draft) (int64, error) {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return 0, err
	}
	// normalized、err 保存规范化后的规则输入及校验结果。
	normalized, err := normalizeDraft(draft)
	if err != nil {
		return 0, err
	}
	return s.repository.Add(ctx, userID, cookieID, normalized)
}

// Replace 校验并原子替换指定账号的全部关键词规则。
func (s *Service) Replace(ctx context.Context, userID int64, cookieID string, drafts []Draft) error {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return err
	}
	// normalized 保存全部通过校验的批量规则输入。
	normalized := make([]Draft, 0, len(drafts))
	// draft 表示当前待规范化的批量规则输入。
	for _, draft := range drafts {
		// item、err 保存规范化规则及校验结果。
		item, err := normalizeDraft(draft)
		if err != nil {
			return err
		}
		normalized = append(normalized, item)
	}
	return s.repository.Replace(ctx, userID, cookieID, normalized)
}

// Update 校验并更新指定 ID 的关键词规则。
func (s *Service) Update(ctx context.Context, userID int64, cookieID string, id int64, draft Draft) error {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return err
	}
	if id <= 0 {
		return &ValidationError{Message: "无效关键词ID"}
	}
	// normalized、err 保存规范化后的规则输入及校验结果。
	normalized, err := normalizeDraft(draft)
	if err != nil {
		return err
	}
	return s.repository.Update(ctx, userID, cookieID, id, normalized)
}

// DeleteByID 删除指定 ID 的关键词规则。
func (s *Service) DeleteByID(ctx context.Context, userID int64, cookieID string, id int64) error {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return err
	}
	if id <= 0 {
		return &ValidationError{Message: "无效关键词ID"}
	}
	return s.repository.DeleteByID(ctx, userID, cookieID, id)
}

// DeleteByIndex 按规则列表中的零基索引删除关键词。
func (s *Service) DeleteByIndex(ctx context.Context, userID int64, cookieID string, index int) error {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return err
	}
	if index < 0 {
		return &ValidationError{Message: "无效关键词索引"}
	}
	return s.repository.DeleteByIndex(ctx, userID, cookieID, index)
}

// ListItemReplies 查询当前用户拥有账号的指定商品回复。
func (s *Service) ListItemReplies(ctx context.Context, userID int64) ([]ItemReply, error) {
	// err 表示服务依赖或用户身份校验结果。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	return s.repository.ListItemReplies(ctx, userID)
}

// GetItemReply 查询指定商品回复；不存在时返回 ErrNotFound。
func (s *Service) GetItemReply(ctx context.Context, userID int64, cookieID, itemID string) (ItemReply, error) {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return ItemReply{}, err
	}
	if strings.TrimSpace(itemID) == "" {
		return ItemReply{}, &ValidationError{Message: "商品ID不能为空"}
	}
	return s.repository.GetItemReply(ctx, userID, cookieID, itemID)
}

// SetItemReply 校验商品标识并覆盖指定商品回复。
func (s *Service) SetItemReply(ctx context.Context, userID int64, cookieID, itemID, content string) error {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return err
	}
	if strings.TrimSpace(itemID) == "" {
		return &ValidationError{Message: "商品ID不能为空"}
	}
	return s.repository.SetItemReply(ctx, userID, cookieID, itemID, content)
}

// DeleteItemReply 删除指定商品回复。
func (s *Service) DeleteItemReply(ctx context.Context, userID int64, cookieID, itemID string) error {
	// err 表示服务依赖、用户身份或账号标识校验结果。
	if err := s.validate(userID, cookieID); err != nil {
		return err
	}
	if strings.TrimSpace(itemID) == "" {
		return &ValidationError{Message: "商品ID不能为空"}
	}
	return s.repository.DeleteItemReply(ctx, userID, cookieID, itemID)
}

// validate 检查服务依赖、用户身份和账号标识。
func (s *Service) validate(userID int64, cookieID string) error {
	// err 表示服务依赖或用户身份校验结果。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	if s.repository == nil || strings.TrimSpace(cookieID) == "" {
		return ErrInvalidInput
	}
	return nil
}

// validateUser 检查服务依赖和用户身份。
func (s *Service) validateUser(userID int64) error {
	if s == nil || s.repository == nil {
		return ErrInvalidInput
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	return nil
}

// normalizeDraft 统一回复类型和内容字段，并拒绝不完整输入。
func normalizeDraft(draft Draft) (Draft, error) {
	draft.Keyword = strings.TrimSpace(draft.Keyword)
	draft.Type = strings.ToLower(strings.TrimSpace(draft.Type))
	draft.ItemID = strings.TrimSpace(draft.ItemID)
	draft.Reply = strings.TrimSpace(draft.Reply)
	draft.ImageURL = strings.TrimSpace(draft.ImageURL)
	if draft.Keyword == "" {
		return Draft{}, &ValidationError{Message: "keyword 必填"}
	}
	if draft.Type == "" {
		draft.Type = "text"
	}
	switch draft.Type {
	case "text":
		if draft.Reply == "" {
			return Draft{}, &ValidationError{Message: "文字回复内容不能为空"}
		}
		draft.ImageURL = ""
	case "image":
		if draft.ImageURL == "" {
			return Draft{}, &ValidationError{Message: "图片回复 URL 不能为空"}
		}
		draft.Reply = ""
	default:
		return Draft{}, &ValidationError{Message: "回复类型必须是 text 或 image"}
	}
	return draft, nil
}
