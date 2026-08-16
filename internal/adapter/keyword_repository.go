package adapter

import (
	"context"
	"errors"

	keywordsapp "xianyu-go/internal/application/keywords"
	"xianyu-go/internal/db"
)

// KeywordRepository 将关键词和指定商品回复数据库操作适配为应用层 Port。
type KeywordRepository struct {
	// store 保存数据库聚合入口；敏感账号字段不会被本适配器读取。
	store *db.Store
}

// NewKeywordRepository 创建关键词数据库适配器。
func NewKeywordRepository(store *db.Store) *KeywordRepository {
	return &KeywordRepository{store: store}
}

// List 查询指定用户账号的关键词规则，并转换为应用模型。
func (r *KeywordRepository) List(ctx context.Context, userID int64, cookieID string) ([]keywordsapp.Keyword, error) {
	// err 表示账号归属校验失败，阻止跨用户读取规则。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return nil, err
	}
	// rows 保存数据库返回的关键词规则行。
	rows, err := r.store.Keywords.AllRows(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	// result 保存转换后的应用层关键词模型，不携带数据库对象。
	result := make([]keywordsapp.Keyword, 0, len(rows))
	// row 表示当前待转换的关键词数据库行。
	for _, row := range rows {
		result = append(result, keywordModel(row))
	}
	return result, nil
}

// Add 创建一条指定用户账号的关键词规则。
func (r *KeywordRepository) Add(ctx context.Context, userID int64, cookieID string, draft keywordsapp.Draft) (int64, error) {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return 0, err
	}
	return r.store.Keywords.Add(ctx, cookieID, draft.Keyword, draft.Reply, draft.ItemID, draft.Type, draft.ImageURL)
}

// Replace 原子覆盖指定用户账号的全部关键词规则。
func (r *KeywordRepository) Replace(ctx context.Context, userID int64, cookieID string, drafts []keywordsapp.Draft) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	// rows 保存转换后的数据库关键词写入行。
	rows := make([]db.KeywordRow, 0, len(drafts))
	// draft 表示当前待转换的应用层关键词草稿。
	for _, draft := range drafts {
		rows = append(rows, db.KeywordRow{
			CookieID: cookieID,
			Keyword:  draft.Keyword,
			Reply:    draft.Reply,
			ItemID:   draft.ItemID,
			Type:     draft.Type,
			ImageURL: draft.ImageURL,
		})
	}
	return r.store.Keywords.ReplaceForCookie(ctx, cookieID, rows)
}

// Update 更新指定用户账号的一条关键词规则。
func (r *KeywordRepository) Update(ctx context.Context, userID int64, cookieID string, id int64, draft keywordsapp.Draft) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	// err 表示数据库更新错误或目标规则不存在。
	err := r.store.Keywords.UpdateByID(ctx, db.KeywordRow{
		ID: id, CookieID: cookieID, Keyword: draft.Keyword, Reply: draft.Reply,
		ItemID: draft.ItemID, Type: draft.Type, ImageURL: draft.ImageURL,
	})
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	return err
}

// DeleteByID 按 ID 删除指定用户账号的一条关键词规则。
func (r *KeywordRepository) DeleteByID(ctx context.Context, userID int64, cookieID string, id int64) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	// err 表示数据库删除错误或目标规则不存在。
	err := r.store.Keywords.DeleteByID(ctx, cookieID, id)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	return err
}

// DeleteByIndex 按稳定 ID 顺序的零基索引删除关键词规则。
func (r *KeywordRepository) DeleteByIndex(ctx context.Context, userID int64, cookieID string, index int) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	// err 表示数据库删除错误或索引没有对应规则。
	err := r.store.Keywords.DeleteByIndex(ctx, cookieID, index)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	return err
}

// ListItemReplies 查询当前用户所有账号的商品回复，并保持数据库返回顺序。
func (r *KeywordRepository) ListItemReplies(ctx context.Context, userID int64) ([]keywordsapp.ItemReply, error) {
	// err 表示适配器依赖或用户身份校验失败。
	if err := r.validateUser(userID); err != nil {
		return nil, err
	}
	// cookieIDs 保存当前用户拥有的账号标识，不包含账号凭证。
	cookieIDs, err := r.store.Cookies.ListOwnedIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 保存跨账号聚合后的商品回复应用模型。
	result := make([]keywordsapp.ItemReply, 0)
	// cookieID 表示当前遍历的用户账号。
	for _, cookieID := range cookieIDs {
		// queryErr 表示当前账号商品回复读取失败。
		// rows 保存当前账号的商品回复数据库行。
		rows, queryErr := r.store.ItemReps.AllForUser(ctx, cookieID)
		if queryErr != nil {
			return nil, queryErr
		}
		// row 表示当前待转换的商品回复行。
		for _, row := range rows {
			result = append(result, itemReplyModel(row))
		}
	}
	return result, nil
}

// GetItemReply 读取指定用户账号和商品的商品回复。
func (r *KeywordRepository) GetItemReply(ctx context.Context, userID int64, cookieID, itemID string) (keywordsapp.ItemReply, error) {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return keywordsapp.ItemReply{}, err
	}
	// row、err 保存商品回复数据库行及读取错误。
	row, err := r.store.ItemReps.Get(ctx, cookieID, itemID)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ItemReply{}, keywordsapp.ErrNotFound
	}
	if err != nil {
		return keywordsapp.ItemReply{}, err
	}
	return itemReplyModel(*row), nil
}

// SetItemReply 覆盖指定用户账号和商品的商品回复。
func (r *KeywordRepository) SetItemReply(ctx context.Context, userID int64, cookieID, itemID, content string) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	return r.store.ItemReps.Set(ctx, cookieID, itemID, content)
}

// DeleteItemReply 删除指定用户账号和商品的商品回复。
func (r *KeywordRepository) DeleteItemReply(ctx context.Context, userID int64, cookieID, itemID string) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	return r.store.ItemReps.Delete(ctx, cookieID, itemID)
}

// authorize 只查询账号归属，不读取或解密任何凭证字段。
func (r *KeywordRepository) authorize(ctx context.Context, userID int64, cookieID string) error {
	// err 表示适配器依赖或用户身份校验失败。
	if err := r.validateUser(userID); err != nil {
		return err
	}
	if cookieID == "" {
		return keywordsapp.ErrInvalidInput
	}
	// owned、err 保存当前用户对账号的直接归属结果及查询错误。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
	if err != nil {
		return err
	}
	if owned {
		return nil
	}
	// ownerID、err 保存账号所有者标识及读取错误，用于区分不存在和跨用户访问。
	ownerID, err := r.store.Cookies.GetOwnerID(ctx, cookieID)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	if err != nil {
		return err
	}
	if ownerID != userID {
		return keywordsapp.ErrForbidden
	}
	return nil
}

// validateUser 检查数据库适配器和用户身份是否可用。
func (r *KeywordRepository) validateUser(userID int64) error {
	if r == nil || r.store == nil || r.store.Cookies == nil || r.store.Keywords == nil || r.store.ItemReps == nil {
		return errors.New("关键词数据库适配器未初始化")
	}
	if userID <= 0 {
		return keywordsapp.ErrInvalidUser
	}
	return nil
}

// keywordModel 将数据库关键词行转换为应用模型。
func keywordModel(row db.KeywordRow) keywordsapp.Keyword {
	return keywordsapp.Keyword{ID: row.ID, CookieID: row.CookieID, Keyword: row.Keyword, Reply: row.Reply, ItemID: row.ItemID, Type: row.Type, ImageURL: row.ImageURL}
}

// itemReplyModel 将数据库商品回复行转换为应用模型。
func itemReplyModel(row db.ItemReply) keywordsapp.ItemReply {
	return keywordsapp.ItemReply{ItemID: row.ItemID, CookieID: row.CookieID, ReplyContent: row.ReplyContent}
}

var _ keywordsapp.Repository = (*KeywordRepository)(nil)
