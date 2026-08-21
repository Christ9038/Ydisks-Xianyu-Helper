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
			TimeoutSeconds: record.APIConfigSummary.TimeoutSeconds, ResponsePath: record.APIConfigSummary.ResponsePath,
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
