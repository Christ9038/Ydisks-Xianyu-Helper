// Package defaultreply 定义默认回复配置用例及其消费者侧持久化 Port。
// 本包不依赖 HTTP、数据库模型或具体基础设施实现。
package defaultreply

import (
	"context"
	"errors"
)

// ErrInvalidUser 表示调用方没有提供有效的本地用户身份。
var ErrInvalidUser = errors.New("默认回复用户身份无效")

// ErrInvalidCookieID 表示调用方没有提供有效的账号标识。
var ErrInvalidCookieID = errors.New("默认回复账号标识无效")

// ErrAccountNotFound 表示目标账号不存在。
var ErrAccountNotFound = errors.New("默认回复账号不存在")

// ErrForbidden 表示目标账号存在但不属于当前用户。
var ErrForbidden = errors.New("无权操作该账号的默认回复")

// ErrConfigNotFound 表示账号存在但尚未保存默认回复配置。
var ErrConfigNotFound = errors.New("默认回复配置不存在")

// Reply 是默认回复配置的应用层模型，不携带数据库或 HTTP 细节。
type Reply struct {
	// Enabled 表示是否启用默认回复。
	Enabled bool
	// ReplyContent 是默认回复使用的文字内容。
	ReplyContent string
	// ReplyImageURL 是默认回复使用的图片地址。
	ReplyImageURL string
	// ReplyOnce 表示同一聊天是否只发送一次默认回复。
	ReplyOnce bool
}

// Summary 是按用户列出的默认回复配置及其账号标识。
type Summary struct {
	// CookieID 是默认回复所属账号的稳定标识。
	CookieID string
	// Reply 是该账号的默认回复配置。
	Reply Reply
}

// AccountOwnership 是账号所有权 Port 返回的非敏感结果。
type AccountOwnership struct {
	// OwnerID 是账号所属本地用户标识；账号不存在时 Port 应返回 ErrAccountNotFound。
	OwnerID int64
}

// Repository 定义默认回复用例所需的最小持久化能力。
type Repository interface {
	// CheckOwnership 查询账号所有者，不读取或解密任何凭证字段。
	CheckOwnership(ctx context.Context, userID int64, cookieID string) (AccountOwnership, error)
	// Get 读取账号的默认回复配置；未配置时返回 ErrConfigNotFound。
	Get(ctx context.Context, cookieID string) (Reply, error)
	// Upsert 保存或覆盖账号的默认回复配置。
	Upsert(ctx context.Context, cookieID string, reply Reply) error
	// ListForUser 查询指定用户全部账号的默认回复配置。
	ListForUser(ctx context.Context, userID int64) ([]Summary, error)
	// Delete 删除账号的默认回复配置。
	Delete(ctx context.Context, cookieID string) error
	// ClearRecords 删除账号的默认回复投递记录。
	ClearRecords(ctx context.Context, cookieID string) error
}

// Service 编排默认回复的用户归属校验、配置读写和记录清理。
type Service struct {
	// repository 保存调用方注入的默认回复持久化 Port。
	repository Repository
}

// NewService 创建默认回复应用服务。
func NewService(repository Repository) *Service {
	// service 保存调用方提供的持久化 Port，实际调用时会校验其是否为空。
	service := &Service{repository: repository}
	return service
}

// Get 读取指定用户账号的默认回复配置，并区分账号不存在与尚未配置。
func (s *Service) Get(ctx context.Context, userID int64, cookieID string) (Reply, error) {
	// err 表示输入、所有权或持久化查询失败的原因。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return Reply{}, err
	}
	return s.repository.Get(ctx, cookieID)
}

// Upsert 校验账号归属后保存默认回复配置。
func (s *Service) Upsert(ctx context.Context, userID int64, cookieID string, reply Reply) error {
	// err 表示输入、所有权或持久化写入失败的原因。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return err
	}
	return s.repository.Upsert(ctx, cookieID, reply)
}

// List 查询指定用户拥有的全部默认回复配置。
func (s *Service) List(ctx context.Context, userID int64) ([]Summary, error) {
	// err 表示服务装配或用户身份校验失败的原因。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	return s.repository.ListForUser(ctx, userID)
}

// Delete 校验账号归属后删除默认回复配置。
func (s *Service) Delete(ctx context.Context, userID int64, cookieID string) error {
	// err 表示输入、所有权或持久化删除失败的原因。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return err
	}
	return s.repository.Delete(ctx, cookieID)
}

// ClearRecords 校验账号归属后清空默认回复投递记录。
func (s *Service) ClearRecords(ctx context.Context, userID int64, cookieID string) error {
	// err 表示输入、所有权或持久化清理失败的原因。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return err
	}
	return s.repository.ClearRecords(ctx, cookieID)
}

// validateUser 检查应用服务依赖和用户身份，避免无效请求进入 Port。
func (s *Service) validateUser(userID int64) error {
	if s == nil || s.repository == nil {
		return errors.New("默认回复 repository 未初始化")
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	return nil
}

// ensureOwned 统一执行账号标识校验和非敏感所有权判断。
func (s *Service) ensureOwned(ctx context.Context, userID int64, cookieID string) error {
	// err 表示服务装配或用户身份校验失败的原因。
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
