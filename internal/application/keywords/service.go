// Package keywords 提供关键词回复和指定商品回复的应用层用例。
// 本包只依赖消费者定义的持久化 Port，不依赖 HTTP、数据库模型或具体数据库实现。
package keywords

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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

// KeywordItemIDSeparator 是关键词规则关联多个商品时使用的持久化分隔符。
// 逗号分隔与历史单值格式完全兼容：单值规则的 item_id 天然是单元素集合。
const KeywordItemIDSeparator = ","

// KeywordMatchTypeContains 表示对每个表达式执行大小写不敏感的普通包含匹配。
const KeywordMatchTypeContains = "contains"

// KeywordMatchTypeRegexp 表示对每个表达式执行大小写不敏感的 Go/RE2 正则匹配。
const KeywordMatchTypeRegexp = "regexp"

// Keyword 是关键词回复的应用层模型，不携带数据库连接或敏感凭证。
type Keyword struct {
	// ID 是关键词规则的持久化标识。
	ID int64
	// CookieID 是规则所属账号标识。
	CookieID string
	// Keyword 是第一条表达式，用于兼容历史单关键词接口。
	Keyword string
	// Expressions 是同一条规则共享回复的匹配表达式集合，按 OR 语义执行。
	Expressions []string
	// MatchType 是 contains 或 regexp，空值只在兼容旧数据时回退为 contains。
	MatchType string
	// Reply 是文字回复内容。
	Reply string
	// ItemID 是关联商品标识的持久化形式；多选时为逗号分隔串，空串表示账号级规则。
	ItemID string
	// Type 是 text 或 image 回复类型。
	Type string
	// ImageURL 是 image 类型回复使用的图片地址。
	ImageURL string
}

// SplitItemIDs 把持久化的商品范围字段拆分为去空白的商品标识集合。
// 空字段返回空集合，代表账号级规则；重复项按首次出现顺序去重。
func SplitItemIDs(raw string) []string {
	// parts 是原始字段按分隔符切分后的片段。
	parts := strings.Split(raw, KeywordItemIDSeparator)
	// seen 记录已收录的商品标识，用于剔除重复项。
	seen := make(map[string]struct{}, len(parts))
	// result 保存去重且去除空白后的商品标识集合。
	result := make([]string, 0, len(parts))
	// part 表示当前待规整的商品标识片段。
	for _, part := range parts {
		// itemID 是去除首尾空白后的商品标识。
		itemID := strings.TrimSpace(part)
		if itemID == "" {
			continue
		}
		// ok 表示该商品标识是否已被收录。
		if _, ok := seen[itemID]; ok {
			continue
		}
		seen[itemID] = struct{}{}
		result = append(result, itemID)
	}
	return result
}

// JoinItemIDs 把商品标识集合规整为持久化的逗号分隔字段。
// 空集合返回空串以保持账号级规则语义，重复项与空白项会被剔除。
func JoinItemIDs(itemIDs []string) string {
	return strings.Join(SplitItemIDs(strings.Join(itemIDs, KeywordItemIDSeparator)), KeywordItemIDSeparator)
}

// Draft 是创建、更新或批量替换关键词规则的业务输入。
type Draft struct {
	// Keyword 是第一条表达式的兼容单值字段。
	Keyword string
	// Expressions 是同一条规则的匹配表达式集合，保存后共享同一条回复。
	Expressions []string
	// MatchType 是 contains 或 regexp，历史调用方缺省时使用 contains。
	MatchType string
	// Reply 是文字回复内容。
	Reply string
	// ItemID 是关联商品范围的持久化字段；多选时由 ItemIDs 合并而来。
	ItemID string
	// ItemIDs 是关联商品范围标识集合；去重后的空集合表示账号级回复。
	ItemIDs []string
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

// normalizeDraft 统一表达式、匹配模式、回复类型、商品范围和内容字段，并拒绝不完整输入。
// 商品范围同时接受兼容的单值 ItemID 与多值 ItemIDs，去重后合并写回 ItemID，
// 使一条规则可以关联多个商品，同时保持历史单值数据的原样可读。
func normalizeDraft(draft Draft) (Draft, error) {
	// normalizedExpressions 保存去空白、去重后的规则表达式集合。
	normalizedExpressions, err := normalizeExpressions(draft.Keyword, draft.Expressions)
	if err != nil {
		return Draft{}, err
	}
	// normalizedMatchType 保存校验并兼容旧别名后的匹配模式。
	normalizedMatchType, err := normalizeMatchType(draft.MatchType, normalizedExpressions)
	if err != nil {
		return Draft{}, err
	}
	draft.Expressions = normalizedExpressions
	draft.Keyword = normalizedExpressions[0]
	draft.MatchType = normalizedMatchType
	draft.Type = strings.ToLower(strings.TrimSpace(draft.Type))
	draft.Reply = strings.TrimSpace(draft.Reply)
	draft.ImageURL = strings.TrimSpace(draft.ImageURL)
	draft.ItemID = JoinItemIDs(mergeItemIDs(draft.ItemID, draft.ItemIDs))
	draft.ItemIDs = SplitItemIDs(draft.ItemID)
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

// normalizeExpressions 规范化兼容单值字段和多表达式字段，并拒绝空表达式；keyword 是历史单值输入，expressions 是优先采用的新集合。
func normalizeExpressions(keyword string, expressions []string) ([]string, error) {
	// source 是优先使用的新多表达式输入；只有字段缺省为 nil 时才回退历史 keyword 字段。
	source := expressions
	if source == nil {
		if strings.TrimSpace(keyword) == "" {
			return nil, &ValidationError{Message: "至少填写一个表达式"}
		}
		source = []string{keyword}
	}
	if len(source) == 0 {
		return nil, &ValidationError{Message: "至少填写一个表达式"}
	}
	// normalized 保存去空白、去重后的表达式集合。
	normalized := make([]string, 0, len(source))
	// seen 记录已经加入集合的表达式，避免同一规则重复编译或匹配。
	seen := make(map[string]struct{}, len(source))
	// expressionIndex、rawExpression 表示当前待校验的表达式位置和原始值。
	for expressionIndex, rawExpression := range source {
		// expression 是去除首尾空白后的匹配文本。
		expression := strings.TrimSpace(rawExpression)
		if expression == "" {
			return nil, &ValidationError{Message: fmt.Sprintf("第 %d 个表达式不能为空", expressionIndex+1)}
		}
		// exists 表示当前表达式是否已经按首次出现顺序收录。
		_, exists := seen[expression]
		if exists {
			continue
		}
		seen[expression] = struct{}{}
		normalized = append(normalized, expression)
	}
	if len(normalized) == 0 {
		return nil, &ValidationError{Message: "至少填写一个表达式"}
	}
	return normalized, nil
}

// normalizeMatchType 根据 raw 校验匹配模式，并在 regexp 模式下预编译 expressions；返回可持久化的规范模式或可展示的表达式位置错误。
func normalizeMatchType(raw string, expressions []string) (string, error) {
	// matchType 是去空白并转为小写后的匹配模式。
	matchType := strings.ToLower(strings.TrimSpace(raw))
	switch matchType {
	case "", "fuzzy", "exact", KeywordMatchTypeContains:
		return KeywordMatchTypeContains, nil
	case KeywordMatchTypeRegexp, "regex":
		// expressionIndex、expression 表示当前待预编译的正则位置和内容。
		for expressionIndex, expression := range expressions {
			// pattern 是保持大小写不敏感语义的 Go/RE2 包装表达式。
			pattern := "(?i:" + expression + ")"
			// compileErr 表示当前正则预编译是否失败；底层错误正文不会返回给调用方。
			_, compileErr := regexp.Compile(pattern)
			if compileErr != nil {
				return "", &ValidationError{Message: fmt.Sprintf("第 %d 个正则表达式无效", expressionIndex+1)}
			}
		}
		return KeywordMatchTypeRegexp, nil
	default:
		return "", &ValidationError{Message: "匹配模式必须是 contains 或 regexp"}
	}
}

// mergeItemIDs 合并兼容单值和多值商品标识，按输入顺序展开以便统一去重。
func mergeItemIDs(single string, multiple []string) []string {
	// merged 保存按“先单值后多值”顺序展开的待去重商品标识。
	merged := make([]string, 0, len(multiple)+1)
	if strings.TrimSpace(single) != "" {
		merged = append(merged, single)
	}
	merged = append(merged, multiple...)
	return merged
}
