// Package itemgreeting 定义商品首次回复规则用例及其消费者侧持久化 Port。
// 本包不依赖 HTTP、数据库模型或具体基础设施实现。
package itemgreeting

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// 以下输入上限保护存储与回复链路，超过即视为非法请求。
const (
	// MaxNameRunes 是规则备注名的最大字符数。
	MaxNameRunes = 100
	// MaxContentRunes 是首次回复文字的最大字符数。
	MaxContentRunes = 2000
	// MaxImageURLRunes 是首次回复图片地址的最大字符数。
	MaxImageURLRunes = 2048
	// MaxItemsPerRule 是单条规则可覆盖的商品数量上限。
	MaxItemsPerRule = 500
)

// ErrInvalidUser 表示调用方没有提供有效的本地用户身份。
var ErrInvalidUser = errors.New("商品首次回复用户身份无效")

// ErrInvalidCookieID 表示调用方没有提供有效的账号标识。
var ErrInvalidCookieID = errors.New("商品首次回复账号标识无效")

// ErrAccountNotFound 表示目标账号不存在。
var ErrAccountNotFound = errors.New("商品首次回复账号不存在")

// ErrForbidden 表示目标账号存在但不属于当前用户。
var ErrForbidden = errors.New("无权操作该账号的商品首次回复")

// ErrRuleNotFound 表示目标规则不存在。
var ErrRuleNotFound = errors.New("商品首次回复规则不存在")

// ErrInvalidInput 表示规则内容不满足业务约束；具体原因通过 InvalidInputError 的 Reason 提供。
var ErrInvalidInput = errors.New("商品首次回复规则参数无效")

// InvalidInputError 携带可直接展示给用户的参数错误原因，并匹配 ErrInvalidInput。
type InvalidInputError struct {
	// Reason 是不含敏感信息的中文原因。
	Reason string
}

// Error 返回参数错误原因。
func (e *InvalidInputError) Error() string { return e.Reason }

// Is 让 errors.Is(err, ErrInvalidInput) 对所有参数错误成立。
func (e *InvalidInputError) Is(target error) bool { return target == ErrInvalidInput }

// ItemConflictError 表示部分商品已被同账号的其他规则使用。
type ItemConflictError struct {
	// ItemIDs 是发生冲突的商品标识。
	ItemIDs []string
}

// Error 返回冲突说明。
func (e *ItemConflictError) Error() string {
	return "商品已被其他首次回复规则使用: " + strings.Join(e.ItemIDs, ",")
}

// Rule 是商品首次回复规则的应用层模型。
type Rule struct {
	// ID 是规则主键。
	ID int64
	// CookieID 是规则所属账号。
	CookieID string
	// Name 是规则备注名。
	Name string
	// Enabled 表示规则是否生效。
	Enabled bool
	// ReplyContent 是首次回复文字。
	ReplyContent string
	// ReplyImageURL 是首次回复图片地址。
	ReplyImageURL string
	// ReplyLibraryImageID 是首次回复引用的图片库图片主键；与 ReplyImageURL 互斥，0 表示未使用。
	ReplyLibraryImageID int64
	// ItemIDs 是规则覆盖的商品标识。
	ItemIDs []string
}

// Draft 是创建或更新规则时由调用方提交的内容。
type Draft struct {
	// CookieID 是目标账号；更新时必须与规则现有账号一致。
	CookieID string
	// Name 是规则备注名。
	Name string
	// Enabled 表示规则是否生效。
	Enabled bool
	// ReplyContent 是首次回复文字。
	ReplyContent string
	// ReplyImageURL 是首次回复图片地址。
	ReplyImageURL string
	// ReplyLibraryImageID 是首次回复引用的图片库图片主键；与 ReplyImageURL 互斥，0 表示未使用。
	ReplyLibraryImageID int64
	// ItemIDs 是规则覆盖的商品标识。
	ItemIDs []string
}

// AccountOwnership 是账号所有权 Port 返回的非敏感结果。
type AccountOwnership struct {
	// OwnerID 是账号所属本地用户标识；账号不存在时 Port 应返回 ErrAccountNotFound。
	OwnerID int64
}

// Repository 定义商品首次回复用例所需的最小持久化能力。
type Repository interface {
	// CheckOwnership 查询账号所有者，不读取或解密任何凭证字段。
	CheckOwnership(ctx context.Context, userID int64, cookieID string) (AccountOwnership, error)
	// Get 读取规则；不存在返回 ErrRuleNotFound。
	Get(ctx context.Context, id int64) (Rule, error)
	// ListByAccount 查询账号的全部规则。
	ListByAccount(ctx context.Context, cookieID string) ([]Rule, error)
	// ListForUser 查询用户名下全部账号的规则。
	ListForUser(ctx context.Context, userID int64) ([]Rule, error)
	// Create 写入新规则并返回主键；商品冲突返回 *ItemConflictError。
	Create(ctx context.Context, rule Rule) (int64, error)
	// Update 覆盖规则；商品冲突返回 *ItemConflictError，规则不存在返回 ErrRuleNotFound。
	Update(ctx context.Context, rule Rule) error
	// Delete 删除规则及其商品集合。
	Delete(ctx context.Context, id int64) error
	// ClearRecords 清空账号的首次回复投递记录。
	ClearRecords(ctx context.Context, cookieID string) error
}

// Service 编排商品首次回复的归属校验、输入校验和规则读写。
type Service struct {
	// repository 保存调用方注入的持久化 Port。
	repository Repository
}

// NewService 创建商品首次回复应用服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// List 查询规则；cookieID 为空时返回用户名下全部账号的规则。
func (s *Service) List(ctx context.Context, userID int64, cookieID string) ([]Rule, error) {
	// err 表示服务依赖未装配或用户身份无效的错误。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	if cookieID == "" {
		return s.repository.ListForUser(ctx, userID)
	}
	// err 表示账号标识无效、不存在或不属于当前用户的错误。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return nil, err
	}
	return s.repository.ListByAccount(ctx, cookieID)
}

// Create 校验归属与输入后创建规则，返回带主键的规则。
func (s *Service) Create(ctx context.Context, userID int64, draft Draft) (Rule, error) {
	// err 表示账号标识无效、不存在或不属于当前用户的错误。
	if err := s.ensureOwned(ctx, userID, draft.CookieID); err != nil {
		return Rule{}, err
	}
	// rule 保存规整后的规则；err 表示输入校验失败原因。
	rule, err := normalizeDraft(draft)
	if err != nil {
		return Rule{}, err
	}
	// id 保存新规则主键。
	id, err := s.repository.Create(ctx, rule)
	if err != nil {
		return Rule{}, err
	}
	rule.ID = id
	return rule, nil
}

// Update 校验规则及账号归属后覆盖规则；不允许把规则迁移到其他账号。
func (s *Service) Update(ctx context.Context, userID int64, id int64, draft Draft) (Rule, error) {
	// existing 保存更新前的规则，用于确认归属与账号不变。
	existing, err := s.ownedRule(ctx, userID, id)
	if err != nil {
		return Rule{}, err
	}
	if draft.CookieID != "" && draft.CookieID != existing.CookieID {
		return Rule{}, &InvalidInputError{Reason: "不能修改规则所属账号"}
	}
	draft.CookieID = existing.CookieID
	// err、rule 保存 normalizeDraft 的返回值及错误。
	rule, err := normalizeDraft(draft)
	if err != nil {
		return Rule{}, err
	}
	rule.ID = id
	// err 保存 Update 的返回错误，非空时按该分支处理。
	if err := s.repository.Update(ctx, rule); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// Delete 校验归属后删除规则。
func (s *Service) Delete(ctx context.Context, userID int64, id int64) error {
	// err 保存 ownedRule 的返回错误，非空时按该分支处理。
	if _, err := s.ownedRule(ctx, userID, id); err != nil {
		return err
	}
	return s.repository.Delete(ctx, id)
}

// ClearRecords 校验账号归属后清空首次回复投递记录。
func (s *Service) ClearRecords(ctx context.Context, userID int64, cookieID string) error {
	// err 表示账号标识无效、不存在或不属于当前用户的错误。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return err
	}
	return s.repository.ClearRecords(ctx, cookieID)
}

// ownedRule 读取规则并校验其账号归属当前用户。
func (s *Service) ownedRule(ctx context.Context, userID int64, id int64) (Rule, error) {
	// err 表示服务依赖未装配或用户身份无效的错误。
	if err := s.validateUser(userID); err != nil {
		return Rule{}, err
	}
	// err、rule 保存 Get 的返回值及错误。
	rule, err := s.repository.Get(ctx, id)
	if err != nil {
		return Rule{}, err
	}
	// err 表示账号标识无效、不存在或不属于当前用户的错误。
	if err := s.ensureOwned(ctx, userID, rule.CookieID); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// normalizeDraft 去除空白、去重商品并校验长度与必填项。
func normalizeDraft(draft Draft) (Rule, error) {
	// rule 保存规整后的规则。
	rule := Rule{
		CookieID: draft.CookieID, Name: strings.TrimSpace(draft.Name), Enabled: draft.Enabled,
		ReplyContent: strings.TrimSpace(draft.ReplyContent), ReplyImageURL: strings.TrimSpace(draft.ReplyImageURL),
		ReplyLibraryImageID: draft.ReplyLibraryImageID,
	}
	// seen 记录已加入的商品，保持首次出现顺序并去重。
	seen := make(map[string]struct{}, len(draft.ItemIDs))
	// raw 是调用方提交的未规整商品标识。
	for _, raw := range draft.ItemIDs {
		// itemID 是当前处理的商品标识。
		itemID := strings.TrimSpace(raw)
		if itemID == "" {
			continue
		}
		// dup 表示该商品是否已在本次规整中出现过。
		if _, dup := seen[itemID]; dup {
			continue
		}
		seen[itemID] = struct{}{}
		rule.ItemIDs = append(rule.ItemIDs, itemID)
	}
	switch {
	case len(rule.ItemIDs) == 0:
		return Rule{}, &InvalidInputError{Reason: "至少选择一个商品"}
	case len(rule.ItemIDs) > MaxItemsPerRule:
		return Rule{}, &InvalidInputError{Reason: "单条规则覆盖的商品过多"}
	case rule.ReplyLibraryImageID < 0 || (rule.ReplyLibraryImageID > 0 && rule.ReplyImageURL != ""):
		return Rule{}, &InvalidInputError{Reason: "图片只能选择图片库图片或填写 URL 之一"}
	case rule.ReplyContent == "" && rule.ReplyImageURL == "" && rule.ReplyLibraryImageID == 0:
		return Rule{}, &InvalidInputError{Reason: "回复文字和图片至少填写一项"}
	case utf8.RuneCountInString(rule.Name) > MaxNameRunes:
		return Rule{}, &InvalidInputError{Reason: "规则名称过长"}
	case utf8.RuneCountInString(rule.ReplyContent) > MaxContentRunes:
		return Rule{}, &InvalidInputError{Reason: "回复文字过长"}
	case utf8.RuneCountInString(rule.ReplyImageURL) > MaxImageURLRunes:
		return Rule{}, &InvalidInputError{Reason: "图片地址过长"}
	}
	return rule, nil
}

// validateUser 检查应用服务依赖和用户身份，避免无效请求进入 Port。
func (s *Service) validateUser(userID int64) error {
	if s == nil || s.repository == nil {
		return errors.New("商品首次回复 repository 未初始化")
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	return nil
}

// ensureOwned 统一执行账号标识校验和非敏感所有权判断。
func (s *Service) ensureOwned(ctx context.Context, userID int64, cookieID string) error {
	// err 表示服务依赖未装配或用户身份无效的错误。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	if cookieID == "" {
		return ErrInvalidCookieID
	}
	// ownership 保存 Port 返回的账号所有者信息，不包含 Cookie 或登录秘密。
	ownership, err := s.repository.CheckOwnership(ctx, userID, cookieID)
	if err != nil {
		return err
	}
	if ownership.OwnerID != userID {
		return ErrForbidden
	}
	return nil
}
