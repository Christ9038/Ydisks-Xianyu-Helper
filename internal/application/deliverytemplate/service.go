// Package deliverytemplate 定义发货模板管理用例，不依赖 HTTP 或数据库模型。
package deliverytemplate

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidInput 表示调用方没有提供有效的用户或仓储依赖。
var ErrInvalidInput = errors.New("发货模板参数无效")

// ErrNotFound 表示模板不存在或不属于当前用户。
var ErrNotFound = errors.New("发货模板不存在")

// ErrReferenced 表示模板仍被自动化规则引用，不能删除。
var ErrReferenced = errors.New("发货模板仍被自动化规则引用")

// Template 是脱离数据库和 HTTP DTO 的发货模板应用模型。
type Template struct {
	// ID 是模板稳定标识。
	ID int64
	// Name 是用户可见的模板名称。
	Name string
	// Enabled 表示模板是否允许新规则引用。
	Enabled bool
	// Messages 是按发送顺序排列的消息列表。
	Messages []Message
	// Keys 是消息中按首次出现顺序提取的变量键。
	Keys []string
	// CustomKeys 是消息中引用的发货规则自定义变量键。
	CustomKeys []string
	// CreatedAt 是模板创建时间文本。
	CreatedAt string
	// UpdatedAt 是模板最近更新时间文本。
	UpdatedAt string
}

// Message 是模板中的一条独立发送消息。
type Message struct {
	// ID 是消息稳定标识。
	ID int64
	// SortOrder 是消息发送顺序。
	SortOrder int
	// Content 是消息正文。
	Content string
}

// Draft 是模板创建和更新允许提交的业务字段。
type Draft struct {
	// Name 是模板名称。
	Name string
	// Enabled 表示保存后的启用状态。
	Enabled bool
	// Messages 是按顺序提交的消息正文。
	Messages []string
}

// Repository 定义模板用例需要的最小持久化能力。
type Repository interface {
	// ListForUser 返回当前用户的未删除模板。
	ListForUser(context.Context, int64) ([]Template, error)
	// GetForUser 返回当前用户拥有的指定模板。
	GetForUser(context.Context, int64, int64) (Template, error)
	// Create 创建模板并返回新标识。
	Create(context.Context, int64, Draft) (int64, error)
	// Update 更新当前用户拥有的模板。
	Update(context.Context, int64, int64, Draft) error
	// Delete 逻辑删除当前用户拥有的模板。
	Delete(context.Context, int64, int64) error
}

// Service 编排用户归属校验和模板持久化。
type Service struct {
	// repository 保存模板数据的窄持久化端口。
	repository Repository
}

// NewService 构造发货模板应用服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// List 查询当前用户的全部模板。
func (s *Service) List(ctx context.Context, userID int64) ([]Template, error) {
	// err 保存服务或用户身份校验失败原因。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	return s.repository.ListForUser(ctx, userID)
}

// Get 查询当前用户拥有的指定模板。
func (s *Service) Get(ctx context.Context, userID, templateID int64) (Template, error) {
	// err 保存服务或用户身份校验失败原因。
	if err := s.validateUser(userID); err != nil {
		return Template{}, err
	}
	if templateID <= 0 {
		return Template{}, ErrInvalidInput
	}
	return s.repository.GetForUser(ctx, userID, templateID)
}

// Create 校验草稿并创建当前用户的模板。
func (s *Service) Create(ctx context.Context, userID int64, draft Draft) (int64, error) {
	// err 保存服务或用户身份校验失败原因。
	if err := s.validateUser(userID); err != nil {
		return 0, err
	}
	// err 保存模板草稿校验失败原因。
	if err := validateDraft(draft); err != nil {
		return 0, err
	}
	return s.repository.Create(ctx, userID, normalizeDraft(draft))
}

// Update 校验草稿并更新当前用户拥有的模板。
func (s *Service) Update(ctx context.Context, userID, templateID int64, draft Draft) error {
	// err 保存服务或用户身份校验失败原因。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	if templateID <= 0 {
		return ErrInvalidInput
	}
	// err 保存模板草稿校验失败原因。
	if err := validateDraft(draft); err != nil {
		return err
	}
	return s.repository.Update(ctx, userID, templateID, normalizeDraft(draft))
}

// Delete 删除当前用户拥有的模板，并把引用冲突保留为稳定业务错误。
func (s *Service) Delete(ctx context.Context, userID, templateID int64) error {
	// err 保存服务或用户身份校验失败原因。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	if templateID <= 0 {
		return ErrInvalidInput
	}
	return s.repository.Delete(ctx, userID, templateID)
}

// validateUser 检查服务和用户身份，避免请求期出现半初始化调用。
func (s *Service) validateUser(userID int64) error {
	if s == nil || s.repository == nil || userID <= 0 {
		return ErrInvalidInput
	}
	return nil
}

// validateDraft 校验模板名称和至少一条非空消息。
func validateDraft(draft Draft) error {
	if strings.TrimSpace(draft.Name) == "" || len(draft.Messages) == 0 {
		return errors.New("发货模板名称和消息不能为空")
	}
	return nil
}

// normalizeDraft 复制并清理模板名称和消息边界空白，不改变消息内部格式。
func normalizeDraft(draft Draft) Draft {
	draft.Name = strings.TrimSpace(draft.Name)
	draft.Messages = append([]string(nil), draft.Messages...)
	return draft
}
