package adapter

import (
	"context"
	"errors"

	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/db"
)

// CardsRepository 将卡券数据库仓储适配为应用层消费者定义的 CRUD Port。
type CardsRepository struct {
	// store 保存数据库聚合入口，但只通过 Cards 子仓储访问卡券数据。
	store *db.Store
}

// NewCardsRepository 构造卡券数据库适配器；装配缺失会在方法调用时返回错误。
func NewCardsRepository(store *db.Store) *CardsRepository {
	return &CardsRepository{store: store}
}

// ListForUser 查询指定用户的卡券组并转换为应用模型。
func (r *CardsRepository) ListForUser(ctx context.Context, userID int64) ([]cardsapp.Card, error) {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// records 是数据库仓储按倒序返回的卡券完整记录。
	records, err := r.store.Cards.AllForUserSummary(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 是与数据库模型解耦的应用卡券列表。
	result := make([]cardsapp.Card, 0, len(records))
	// record 是当前待转换的数据库卡券记录。
	for _, record := range records {
		result = append(result, cardApplicationModel(record))
	}
	return result, nil
}

// Get 查询单个卡券组，并把数据库缺失错误映射为应用层稳定错误。
func (r *CardsRepository) Get(ctx context.Context, cardID int64) (cardsapp.Card, error) {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return cardsapp.Card{}, err
	}
	// record 是数据库返回的完整卡券记录。
	record, err := r.store.Cards.GetSummary(ctx, cardID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return cardsapp.Card{}, cardsapp.ErrNotFound
		}
		return cardsapp.Card{}, err
	}
	return cardApplicationModel(*record), nil
}

// GetFull 读取更新时需要保留的完整 API 模板，禁止用于普通查询响应。
func (r *CardsRepository) GetFull(ctx context.Context, cardID int64) (cardsapp.Card, error) {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return cardsapp.Card{}, err
	}
	// record、err 保存自动发货专用完整卡券及读取错误。
	record, err := r.store.Cards.GetForDelivery(ctx, cardID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return cardsapp.Card{}, cardsapp.ErrNotFound
		}
		return cardsapp.Card{}, err
	}
	return cardApplicationModel(*record), nil
}

// Create 将已校验的应用卡券模型转换为数据库模型并返回新标识。
func (r *CardsRepository) Create(ctx context.Context, card cardsapp.Card) (int64, error) {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return 0, err
	}
	// record 是仅在适配器内存在的数据库写入模型。
	record := cardDatabaseModel(card)
	return r.store.Cards.Create(ctx, &record)
}

// Update 将应用层可编辑字段写入数据库，不向上层暴露数据库模型。
func (r *CardsRepository) Update(ctx context.Context, card cardsapp.Card) error {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return err
	}
	// record 是仅在适配器内存在的数据库更新模型。
	record := cardDatabaseModel(card)
	return r.store.Cards.Update(ctx, &record)
}

// UpdateIfDataUnchanged 仅在数据库库存仍等于 expectedData 时整体更新卡券，返回是否写入。
func (r *CardsRepository) UpdateIfDataUnchanged(ctx context.Context, card cardsapp.Card, expectedData string) (bool, error) {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	// record 是仅在适配器内存在的数据库更新模型。
	record := cardDatabaseModel(card)
	return r.store.Cards.UpdateIfDataUnchanged(ctx, &record, expectedData)
}

// UpdateDataMetadata 更新 data 卡券元数据，数据库始终保留当前库存正文。
func (r *CardsRepository) UpdateDataMetadata(ctx context.Context, card cardsapp.Card) error {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.Cards.UpdateDataMetadata(ctx, &db.CardFull{ID: card.ID, Name: card.Name, Type: card.Type, Description: card.Description, Enabled: card.Enabled, DelaySeconds: card.DelaySeconds, IsMultiSpec: card.IsMultiSpec, SpecName: card.SpecName, SpecValue: card.SpecValue})
}

// Delete 删除指定卡券组，并原样返回数据库约束或连接错误。
func (r *CardsRepository) Delete(ctx context.Context, cardID int64) error {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.Cards.Delete(ctx, cardID)
}

// AppendData 将 data 卡券组的逐行库存追加能力适配到应用 Port。
func (r *CardsRepository) AppendData(ctx context.Context, cardID int64, content string) (int, error) {
	// err 表示卡券适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return 0, err
	}
	return r.store.Cards.AppendBatchData(ctx, cardID, content)
}

// SaleCounts 返回用户每个卡密组已确认售出的记录数。
func (r *CardsRepository) SaleCounts(ctx context.Context, userID int64) (map[int64]int64, error) {
	// err 保存仓储依赖校验错误，依赖缺失时直接拒绝请求。
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.store.Cards.SaleCounts(ctx, userID)
}

// ListSaleRecords 把应用层分页条件转换为台账查询并映射回应用模型。
func (r *CardsRepository) ListSaleRecords(ctx context.Context, userID int64, query cardsapp.SaleListQuery) (cardsapp.SalePage, error) {
	// err 保存仓储依赖校验错误，依赖缺失时直接拒绝请求。
	if err := r.validate(); err != nil {
		return cardsapp.SalePage{}, err
	}
	// page、err 保存数据库台账分页结果及错误。
	page, err := r.store.Cards.ListSaleRecords(ctx, db.CardSaleQuery{
		UserID: userID, CardID: query.CardID, Status: query.Status, Search: query.Search, Limit: query.Limit, Offset: query.Offset,
	})
	if err != nil {
		return cardsapp.SalePage{}, err
	}
	return cardsapp.SalePage{Records: saleApplicationModels(page.Records), Total: page.Total}, nil
}

// AllSaleRecords 返回卡密组的全部台账，供导出使用。
func (r *CardsRepository) AllSaleRecords(ctx context.Context, userID, cardID int64) ([]cardsapp.SaleRecord, error) {
	// err 保存仓储依赖校验错误，依赖缺失时直接拒绝请求。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// records、err 保存全部台账及错误。
	records, err := r.store.Cards.AllSaleRecords(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}
	return saleApplicationModels(records), nil
}

// SaleRecordsByOrder 返回某账号某订单已发出的卡密台账。
func (r *CardsRepository) SaleRecordsByOrder(ctx context.Context, userID int64, cookieID, orderID string) ([]cardsapp.SaleRecord, error) {
	// err 保存仓储依赖校验错误，依赖缺失时直接拒绝请求。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// records、err 保存订单维度台账及错误。
	records, err := r.store.Cards.ListSaleRecordsByOrder(ctx, userID, cookieID, orderID)
	if err != nil {
		return nil, err
	}
	return saleApplicationModels(records), nil
}

// saleApplicationModels 把数据库台账记录转换为不依赖数据库包的应用模型。
func saleApplicationModels(records []db.CardSaleRecord) []cardsapp.SaleRecord {
	// result 保存转换后的应用模型，空输入返回非 nil 空切片以便 JSON 编码为 []。
	result := make([]cardsapp.SaleRecord, 0, len(records))
	for /* record 是当前待转换的数据库台账 */ _, record := range records {
		result = append(result, cardsapp.SaleRecord{
			ID: record.ID, CardID: record.CardID, CardName: record.CardName, CardType: record.CardType, Content: record.Content,
			Status: record.Status, CookieID: record.CookieID, OrderID: record.OrderID, ItemID: record.ItemID,
			BuyerID: record.BuyerID, CreatedAt: record.CreatedAt, SoldAt: record.SoldAt,
		})
	}
	return result
}

// validate 检查卡券适配器是否具备 Store 与 Cards 子仓储。
func (r *CardsRepository) validate() error {
	if r == nil || r.store == nil || r.store.Cards == nil {
		return errors.New("卡券数据库适配器未初始化")
	}
	return nil
}

// cardApplicationModel 将数据库卡券记录转换为基础设施无关的应用模型。
func cardApplicationModel(record db.CardFull) cardsapp.Card {
	// summary 保存数据库摘要对应的应用层脱敏摘要。
	var summary *cardsapp.APIConfigSummary
	if record.APIConfigSummary != nil {
		// summaryValue 保存当前记录的独立摘要副本，避免共享数据库对象。
		summaryValue := cardsapp.APIConfigSummary{
			URL: record.APIConfigSummary.URL, Method: record.APIConfigSummary.Method,
			TimeoutSeconds: record.APIConfigSummary.TimeoutSeconds, ContentType: record.APIConfigSummary.ContentType, ResponsePath: record.APIConfigSummary.ResponsePath,
			RetryEnabled: record.APIConfigSummary.RetryEnabled, HeadersConfigured: record.APIConfigSummary.HeadersConfigured,
			ParamsConfigured: record.APIConfigSummary.ParamsConfigured, Ready: record.APIConfigSummary.Ready,
			ValidationError: record.APIConfigSummary.ValidationError,
		}
		summary = &summaryValue
	}
	return cardsapp.Card{
		ID: record.ID, Name: record.Name, Type: record.Type, APIConfig: record.APIConfig, APIConfigSummary: summary,
		TextContent: record.TextContent, DataContent: record.DataContent, ImageURL: record.ImageURL,
		Description: record.Description, Enabled: record.Enabled, DelaySeconds: record.DelaySeconds,
		IsMultiSpec: record.IsMultiSpec, SpecName: record.SpecName, SpecValue: record.SpecValue, UserID: record.UserID,
	}
}

// cardDatabaseModel 将应用卡券模型转换为数据库仓储要求的写入模型。
func cardDatabaseModel(card cardsapp.Card) db.CardFull {
	return db.CardFull{
		ID: card.ID, Name: card.Name, Type: card.Type, APIConfig: card.APIConfig,
		TextContent: card.TextContent, DataContent: card.DataContent, ImageURL: card.ImageURL,
		Description: card.Description, Enabled: card.Enabled, DelaySeconds: card.DelaySeconds,
		IsMultiSpec: card.IsMultiSpec, SpecName: card.SpecName, SpecValue: card.SpecValue, UserID: card.UserID,
	}
}

var _ cardsapp.Repository = (*CardsRepository)(nil)
