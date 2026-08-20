package chat

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// quickReplyLimit 限制单个账号可保存的人工快捷回复数量，防止聊天侧栏无限增长。
const quickReplyLimit = 50

// chatMetadataMaxCharacters 限制快捷回复和买家备注的 Unicode 字符数量，与聊天输入上限保持一致。
const chatMetadataMaxCharacters = 2000

// ErrMetadataUnavailable 表示当前聊天服务未装配快捷回复和买家备注持久化端口。
var ErrMetadataUnavailable = errors.New("聊天元数据服务未启用")

// ErrMetadataForbidden 表示当前用户无权访问目标账号的聊天元数据。
var ErrMetadataForbidden = errors.New("无权访问该账号的聊天元数据")

// ErrQuickReplyLimitReached 表示目标账号已保存允许数量的快捷回复。
var ErrQuickReplyLimitReached = errors.New("快捷回复数量已达上限")

// ErrQuickReplyNotFound 表示待删除的快捷回复不存在于目标账号。
var ErrQuickReplyNotFound = errors.New("快捷回复不存在")

// QuickReply 是账号级人工快捷回复的非敏感应用层模型。
type QuickReply struct {
	// ID 是快捷回复的稳定数据库标识。
	ID int64
	// AccountID 是快捷回复所属账号标识，不包含账号凭证。
	AccountID string
	// Content 是用户手动发送到聊天会话的文本模板。
	Content string
	// CreatedAt 是快捷回复创建时的 Unix 秒时间戳。
	CreatedAt int64
}

// BuyerNote 是按账号和买家 ID 隔离的聊天备注模型。
type BuyerNote struct {
	// AccountID 是备注所属的账号标识。
	AccountID string
	// BuyerID 是平台买家稳定标识，不使用易变的聊天会话 ID。
	BuyerID string
	// Content 是完整备注正文；空值表示该买家尚未填写备注。
	Content string
	// UpdatedAt 是最近保存时的 Unix 秒时间戳；空备注为零。
	UpdatedAt int64
}

// MetadataRepository 定义聊天快捷回复与买家备注用例所需的最小持久化能力。
type MetadataRepository interface {
	// ListQuickReplies 读取账号下按创建时间倒序排列的快捷回复。
	ListQuickReplies(ctx context.Context, accountID string) ([]QuickReply, error)
	// CreateQuickReply 原子地创建快捷回复；达到账号上限时返回 ErrQuickReplyLimitReached。
	CreateQuickReply(ctx context.Context, accountID, content string) (QuickReply, error)
	// DeleteQuickReply 删除账号下指定快捷回复；不存在时返回 false。
	DeleteQuickReply(ctx context.Context, accountID string, quickReplyID int64) (bool, error)
	// GetBuyerNote 读取买家备注；没有记录时返回 found=false 而非错误。
	GetBuyerNote(ctx context.Context, accountID, buyerID string) (BuyerNote, bool, error)
	// SaveBuyerNote 保存完整备注；content 为空时删除已有记录。
	SaveBuyerNote(ctx context.Context, note BuyerNote) (BuyerNote, error)
}

// ListQuickReplies 查询当前用户有权访问账号的人工快捷回复。
func (s *Service) ListQuickReplies(ctx context.Context, userID int64, accountID string) ([]QuickReply, error) {
	// normalizedAccountID 保存去除空白后的账号标识，避免不同 HTTP 表达产生不同的数据分区。
	normalizedAccountID := strings.TrimSpace(accountID)
	// repository 保存已验证装配的聊天元数据持久化端口。
	repository, err := s.metadataRepository(ctx, userID, normalizedAccountID)
	if err != nil {
		return nil, err
	}
	return repository.ListQuickReplies(ctx, normalizedAccountID)
}

// CreateQuickReply 校验归属和内容后，为账号保存一条人工快捷回复。
func (s *Service) CreateQuickReply(ctx context.Context, userID int64, accountID, content string) (QuickReply, error) {
	// normalizedAccountID 保存归一化后的账号标识。
	normalizedAccountID := strings.TrimSpace(accountID)
	// normalizedContent 保存去除首尾无意义空白后的用户回复模板，同时保留内部换行。
	normalizedContent := strings.TrimSpace(content)
	if normalizedContent == "" || utf8.RuneCountInString(normalizedContent) > chatMetadataMaxCharacters {
		return QuickReply{}, ErrInvalidInput
	}
	// repository 保存已完成归属校验的元数据持久化端口。
	repository, err := s.metadataRepository(ctx, userID, normalizedAccountID)
	if err != nil {
		return QuickReply{}, err
	}
	return repository.CreateQuickReply(ctx, normalizedAccountID, normalizedContent)
}

// DeleteQuickReply 校验归属后删除指定账号下的快捷回复。
func (s *Service) DeleteQuickReply(ctx context.Context, userID int64, accountID string, quickReplyID int64) error {
	// normalizedAccountID 保存归一化后的账号标识。
	normalizedAccountID := strings.TrimSpace(accountID)
	if quickReplyID <= 0 {
		return ErrInvalidInput
	}
	// repository 保存已完成归属校验的元数据持久化端口。
	repository, err := s.metadataRepository(ctx, userID, normalizedAccountID)
	if err != nil {
		return err
	}
	// deleted 和 deleteErr 保存删除是否命中目标记录及数据库错误。
	deleted, deleteErr := repository.DeleteQuickReply(ctx, normalizedAccountID, quickReplyID)
	if deleteErr != nil {
		return deleteErr
	}
	if !deleted {
		return ErrQuickReplyNotFound
	}
	return nil
}

// GetBuyerNote 返回当前用户账号下买家的完整备注；没有保存记录时返回可编辑的空备注。
func (s *Service) GetBuyerNote(ctx context.Context, userID int64, accountID, buyerID string) (BuyerNote, error) {
	// normalizedAccountID 和 normalizedBuyerID 保存去除空白后的隔离键。
	normalizedAccountID, normalizedBuyerID := strings.TrimSpace(accountID), strings.TrimSpace(buyerID)
	if normalizedBuyerID == "" {
		return BuyerNote{}, ErrInvalidInput
	}
	// repository 保存已完成归属校验的元数据持久化端口。
	repository, err := s.metadataRepository(ctx, userID, normalizedAccountID)
	if err != nil {
		return BuyerNote{}, err
	}
	// note、found 和 readErr 保存持久化备注、存在状态及读取错误。
	note, found, readErr := repository.GetBuyerNote(ctx, normalizedAccountID, normalizedBuyerID)
	if readErr != nil {
		return BuyerNote{}, readErr
	}
	if !found {
		return BuyerNote{AccountID: normalizedAccountID, BuyerID: normalizedBuyerID}, nil
	}
	return note, nil
}

// SaveBuyerNote 校验归属和长度后更新按账号隔离的买家备注。
func (s *Service) SaveBuyerNote(ctx context.Context, userID int64, accountID, buyerID, content string) (BuyerNote, error) {
	// normalizedAccountID、normalizedBuyerID 和 normalizedContent 保存规范化后的备注隔离键及正文。
	normalizedAccountID, normalizedBuyerID, normalizedContent := strings.TrimSpace(accountID), strings.TrimSpace(buyerID), strings.TrimSpace(content)
	if normalizedBuyerID == "" || utf8.RuneCountInString(normalizedContent) > chatMetadataMaxCharacters {
		return BuyerNote{}, ErrInvalidInput
	}
	// repository 保存已完成归属校验的元数据持久化端口。
	repository, err := s.metadataRepository(ctx, userID, normalizedAccountID)
	if err != nil {
		return BuyerNote{}, err
	}
	return repository.SaveBuyerNote(ctx, BuyerNote{AccountID: normalizedAccountID, BuyerID: normalizedBuyerID, Content: normalizedContent})
}

// metadataRepository 验证聊天服务装配、账号归属和元数据持久化端口。
func (s *Service) metadataRepository(ctx context.Context, userID int64, accountID string) (MetadataRepository, error) {
	if s == nil || s.repository == nil || userID <= 0 || accountID == "" {
		return nil, ErrInvalidInput
	}
	// repository 保存从聊天基础仓储断言得到的元数据能力。
	repository, ok := s.repository.(MetadataRepository)
	if !ok {
		return nil, ErrMetadataUnavailable
	}
	// owned 和 ownershipErr 保存非敏感账号所有权判定及查询错误。
	owned, ownershipErr := s.OwnsAccount(ctx, userID, accountID)
	if ownershipErr != nil {
		return nil, ownershipErr
	}
	if !owned {
		return nil, ErrMetadataForbidden
	}
	return repository, nil
}
