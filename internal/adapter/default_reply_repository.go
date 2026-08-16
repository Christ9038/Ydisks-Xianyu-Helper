package adapter

import (
	"context"
	"errors"

	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	"xianyu-go/internal/db"
)

// DefaultReplyRepository 将默认回复数据库仓储适配为应用层消费者定义的 Port。
type DefaultReplyRepository struct {
	// store 保存数据库聚合入口；敏感账号字段只由 Cookies 的非敏感归属方法访问。
	store *db.Store
}

// NewDefaultReplyRepository 创建默认回复数据库适配器。
func NewDefaultReplyRepository(store *db.Store) *DefaultReplyRepository {
	// repository 保存后续用例调用所需的数据库聚合入口。
	repository := &DefaultReplyRepository{store: store}
	return repository
}

// CheckOwnership 查询账号所有者，并把数据库缺失错误转换为应用错误。
func (r *DefaultReplyRepository) CheckOwnership(ctx context.Context, userID int64, cookieID string) (defaultreplyapp.AccountOwnership, error) {
	// err 表示适配器依赖缺失或账号所有权查询失败的原因。
	if err := r.validate(); err != nil {
		return defaultreplyapp.AccountOwnership{}, err
	}
	// owned 表示账号是否直接属于当前用户；查询不读取账号凭证。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
	if err != nil {
		return defaultreplyapp.AccountOwnership{}, err
	}
	if owned {
		return defaultreplyapp.AccountOwnership{OwnerID: userID}, nil
	}
	// ownerID 保存账号的非敏感所有者标识，用于区分不存在和跨用户访问。
	ownerID, err := r.store.Cookies.GetOwnerID(ctx, cookieID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return defaultreplyapp.AccountOwnership{}, defaultreplyapp.ErrAccountNotFound
		}
		return defaultreplyapp.AccountOwnership{}, err
	}
	return defaultreplyapp.AccountOwnership{OwnerID: ownerID}, nil
}

// Get 读取单个账号默认回复，并隐藏数据库模型与 ErrNotFound 语义。
func (r *DefaultReplyRepository) Get(ctx context.Context, cookieID string) (defaultreplyapp.Reply, error) {
	// err 表示适配器依赖缺失或默认回复读取失败的原因。
	if err := r.validate(); err != nil {
		return defaultreplyapp.Reply{}, err
	}
	// record、err 保存数据库默认回复记录及读取错误。
	record, err := r.store.DefaultReps.Get(ctx, cookieID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return defaultreplyapp.Reply{}, defaultreplyapp.ErrConfigNotFound
		}
		return defaultreplyapp.Reply{}, err
	}
	return defaultreplyapp.Reply{
		Enabled: record.Enabled, ReplyContent: record.ReplyContent,
		ReplyImageURL: record.ReplyImageURL, ReplyOnce: record.ReplyOnce,
	}, nil
}

// Upsert 将应用默认回复模型转换为数据库模型并保存。
func (r *DefaultReplyRepository) Upsert(ctx context.Context, cookieID string, reply defaultreplyapp.Reply) error {
	// err 表示适配器依赖缺失或默认回复写入失败的原因。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.DefaultReps.Upsert(ctx, cookieID, db.DefaultReply{
		Enabled: reply.Enabled, ReplyContent: reply.ReplyContent,
		ReplyImageURL: reply.ReplyImageURL, ReplyOnce: reply.ReplyOnce,
	})
}

// ListForUser 查询用户默认回复摘要并转换为应用模型。
func (r *DefaultReplyRepository) ListForUser(ctx context.Context, userID int64) ([]defaultreplyapp.Summary, error) {
	// err 表示适配器依赖缺失或默认回复列表查询失败的原因。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// records、err 保存数据库摘要列表及查询错误。
	records, err := r.store.DefaultReps.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 保存不暴露数据库模型的应用层默认回复摘要。
	result := make([]defaultreplyapp.Summary, 0, len(records))
	// record 表示当前待转换的数据库默认回复摘要。
	for _, record := range records {
		result = append(result, defaultreplyapp.Summary{
			CookieID: record.CookieID,
			Reply: defaultreplyapp.Reply{
				Enabled: record.Enabled, ReplyContent: record.ReplyContent,
				ReplyImageURL: record.ReplyImageURL, ReplyOnce: record.ReplyOnce,
			},
		})
	}
	return result, nil
}

// Delete 删除指定账号的默认回复配置。
func (r *DefaultReplyRepository) Delete(ctx context.Context, cookieID string) error {
	// err 表示适配器依赖缺失或默认回复删除失败的原因。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.DefaultReps.Delete(ctx, cookieID)
}

// ClearRecords 删除指定账号的默认回复投递记录。
func (r *DefaultReplyRepository) ClearRecords(ctx context.Context, cookieID string) error {
	// err 表示适配器依赖缺失或投递记录清理失败的原因。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.DefaultReps.ClearRecords(ctx, cookieID)
}

// validate 检查数据库聚合入口及默认回复子仓储是否已装配。
func (r *DefaultReplyRepository) validate() error {
	if r == nil || r.store == nil || r.store.DefaultReps == nil || r.store.Cookies == nil {
		return errors.New("默认回复数据库适配器未初始化")
	}
	return nil
}

var _ defaultreplyapp.Repository = (*DefaultReplyRepository)(nil)
