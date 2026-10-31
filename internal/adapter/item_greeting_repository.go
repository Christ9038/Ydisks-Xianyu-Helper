package adapter

import (
	"context"
	"errors"

	itemgreetingapp "xianyu-go/internal/application/itemgreeting"
	"xianyu-go/internal/db"
)

// ItemGreetingRepository 将商品首次回复数据库仓储适配为应用层消费者定义的 Port。
type ItemGreetingRepository struct {
	// store 保存数据库聚合入口；账号所有权只通过 Cookies 的非敏感方法判断。
	store *db.Store
}

// NewItemGreetingRepository 创建商品首次回复数据库适配器。
func NewItemGreetingRepository(store *db.Store) *ItemGreetingRepository {
	return &ItemGreetingRepository{store: store}
}

// CheckOwnership 查询账号所有者，并把数据库缺失错误转换为应用错误。
func (r *ItemGreetingRepository) CheckOwnership(ctx context.Context, userID int64, cookieID string) (itemgreetingapp.AccountOwnership, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return itemgreetingapp.AccountOwnership{}, err
	}
	// owned 表示账号是否直接属于当前用户；查询不读取账号凭证。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
	if err != nil {
		return itemgreetingapp.AccountOwnership{}, err
	}
	if owned {
		return itemgreetingapp.AccountOwnership{OwnerID: userID}, nil
	}
	// ownerID 保存账号的非敏感所有者标识，用于区分不存在和跨用户访问。
	ownerID, err := r.store.Cookies.GetOwnerID(ctx, cookieID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return itemgreetingapp.AccountOwnership{}, itemgreetingapp.ErrAccountNotFound
		}
		return itemgreetingapp.AccountOwnership{}, err
	}
	return itemgreetingapp.AccountOwnership{OwnerID: ownerID}, nil
}

// Get 读取规则，并把 ErrNotFound 转换为应用错误。
func (r *ItemGreetingRepository) Get(ctx context.Context, id int64) (itemgreetingapp.Rule, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return itemgreetingapp.Rule{}, err
	}
	// rule、err 保存数据库规则及读取错误。
	rule, err := r.store.ItemGreetings.Get(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return itemgreetingapp.Rule{}, itemgreetingapp.ErrRuleNotFound
	}
	if err != nil {
		return itemgreetingapp.Rule{}, err
	}
	return itemGreetingRuleModel(*rule), nil
}

// ListByAccount 查询账号规则。
func (r *ItemGreetingRepository) ListByAccount(ctx context.Context, cookieID string) ([]itemgreetingapp.Rule, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// err、rows 保存 ListByAccount 的返回值及错误。
	rows, err := r.store.ItemGreetings.ListByAccount(ctx, cookieID)
	return itemGreetingRuleModels(rows), err
}

// ListForUser 查询用户名下全部账号规则。
func (r *ItemGreetingRepository) ListForUser(ctx context.Context, userID int64) ([]itemgreetingapp.Rule, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// err、rows 保存 ListForUser 的返回值及错误。
	rows, err := r.store.ItemGreetings.ListForUser(ctx, userID)
	return itemGreetingRuleModels(rows), err
}

// Create 写入规则并转换商品冲突错误。
func (r *ItemGreetingRepository) Create(ctx context.Context, rule itemgreetingapp.Rule) (int64, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return 0, err
	}
	// guardErr 保存图片库图片归属校验的失败原因，非空时拒绝写入。
	if guardErr := r.ensureLibraryImage(ctx, rule); guardErr != nil {
		return 0, guardErr
	}
	// err、id 保存 Create 的返回值及错误。
	id, err := r.store.ItemGreetings.Create(ctx, itemGreetingDBModel(rule))
	return id, itemGreetingError(err)
}

// Update 覆盖规则并转换商品冲突与缺失错误。
func (r *ItemGreetingRepository) Update(ctx context.Context, rule itemgreetingapp.Rule) error {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return err
	}
	// guardErr 保存图片库图片归属校验的失败原因，非空时拒绝写入。
	if guardErr := r.ensureLibraryImage(ctx, rule); guardErr != nil {
		return guardErr
	}
	return itemGreetingError(r.store.ItemGreetings.Update(ctx, itemGreetingDBModel(rule)))
}

// Delete 删除规则。
func (r *ItemGreetingRepository) Delete(ctx context.Context, id int64) error {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.ItemGreetings.Delete(ctx, id)
}

// ClearRecords 清空账号的首次回复投递记录。
func (r *ItemGreetingRepository) ClearRecords(ctx context.Context, cookieID string) error {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.ItemGreetings.ClearRecords(ctx, cookieID)
}

// validate 确认适配器依赖已注入。
func (r *ItemGreetingRepository) validate() error {
	if r == nil || r.store == nil || r.store.ItemGreetings == nil || r.store.Cookies == nil {
		return errors.New("商品首次回复数据库依赖未初始化")
	}
	return nil
}

// ensureLibraryImage 确认规则引用的图片库图片属于账号所有者，引用他人或已删除的图片按输入错误处理。
func (r *ItemGreetingRepository) ensureLibraryImage(ctx context.Context, rule itemgreetingapp.Rule) error {
	// owned、err 保存图片归属检查结果。
	owned, err := libraryImageOwnedByAccount(ctx, r.store, rule.CookieID, rule.ReplyLibraryImageID)
	if err != nil {
		return err
	}
	if !owned {
		return &itemgreetingapp.InvalidInputError{Reason: "图片库图片不存在"}
	}
	return nil
}

// itemGreetingError 把数据库错误转换为应用层错误。
func itemGreetingError(err error) error {
	// conflict 保存数据库层的商品冲突错误。
	var conflict *db.ItemGreetingConflictError
	switch {
	case errors.As(err, &conflict):
		return &itemgreetingapp.ItemConflictError{ItemIDs: conflict.ItemIDs}
	case errors.Is(err, db.ErrNotFound):
		return itemgreetingapp.ErrRuleNotFound
	}
	return err
}

// itemGreetingRuleModel 将数据库规则转换为应用层模型。
func itemGreetingRuleModel(rule db.ItemGreetingRule) itemgreetingapp.Rule {
	return itemgreetingapp.Rule{
		ID: rule.ID, CookieID: rule.CookieID, Name: rule.Name, Enabled: rule.Enabled,
		ReplyContent: rule.ReplyContent, ReplyImageURL: rule.ReplyImageURL, ReplyLibraryImageID: rule.ReplyLibraryImageID, ItemIDs: rule.ItemIDs,
	}
}

// itemGreetingRuleModels 批量转换数据库规则。
func itemGreetingRuleModels(rows []db.ItemGreetingRule) []itemgreetingapp.Rule {
	// result 保存转换结果，空结果也返回非 nil 切片。
	result := make([]itemgreetingapp.Rule, 0, len(rows))
	// row 是当前待转换的数据库规则。
	for _, row := range rows {
		result = append(result, itemGreetingRuleModel(row))
	}
	return result
}

// itemGreetingDBModel 将应用层规则转换为数据库模型。
func itemGreetingDBModel(rule itemgreetingapp.Rule) db.ItemGreetingRule {
	return db.ItemGreetingRule{
		ID: rule.ID, CookieID: rule.CookieID, Name: rule.Name, Enabled: rule.Enabled,
		ReplyContent: rule.ReplyContent, ReplyImageURL: rule.ReplyImageURL, ReplyLibraryImageID: rule.ReplyLibraryImageID, ItemIDs: rule.ItemIDs,
	}
}

var _ itemgreetingapp.Repository = (*ItemGreetingRepository)(nil)
