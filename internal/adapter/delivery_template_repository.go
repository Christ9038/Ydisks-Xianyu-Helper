package adapter

import (
	"context"
	"errors"
	"fmt"

	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// DeliveryTemplateRepository 将模板数据库仓储转换为应用层模板 Port。
type DeliveryTemplateRepository struct {
	// store 保存基础设施聚合入口，仅在适配器内访问模板仓储。
	store *db.Store
}

// NewDeliveryTemplateRepository 构造发货模板数据库适配器。
func NewDeliveryTemplateRepository(store *db.Store) *DeliveryTemplateRepository {
	return &DeliveryTemplateRepository{store: store}
}

// ListForUser 查询并转换当前用户的模板。
func (r *DeliveryTemplateRepository) ListForUser(ctx context.Context, userID int64) ([]deliveryapp.Template, error) {
	// err 保存适配器初始化校验失败原因。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// items、err 保存数据库模板列表及读取错误。
	items, err := r.store.DeliveryTemplates.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 保存与数据库模型解耦的应用模板列表。
	result := make([]deliveryapp.Template, 0, len(items))
	for /* item 表示当前数据库模板记录。 */ _, item := range items {
		result = append(result, deliveryTemplateApplicationModel(item))
	}
	return result, nil
}

// GetForUser 查询单个模板并映射稳定错误。
func (r *DeliveryTemplateRepository) GetForUser(ctx context.Context, userID, templateID int64) (deliveryapp.Template, error) {
	// err 保存适配器初始化校验失败原因。
	if err := r.validate(); err != nil {
		return deliveryapp.Template{}, err
	}
	// item、err 保存单个数据库模板及读取错误。
	item, err := r.store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if errors.Is(err, db.ErrNotFound) {
		return deliveryapp.Template{}, deliveryapp.ErrNotFound
	}
	if err != nil {
		return deliveryapp.Template{}, err
	}
	return deliveryTemplateApplicationModel(*item), nil
}

// Create 将应用草稿转换为数据库输入并创建模板。
func (r *DeliveryTemplateRepository) Create(ctx context.Context, userID int64, draft deliveryapp.Draft) (int64, error) {
	// err 保存适配器初始化校验失败原因。
	if err := r.validate(); err != nil {
		return 0, err
	}
	// guardErr 保存图片库图片归属校验的失败原因，非空时拒绝写入。
	if guardErr := r.ensureLibraryImages(ctx, userID, draft.Messages); guardErr != nil {
		return 0, guardErr
	}
	return r.store.DeliveryTemplates.Create(ctx, db.DeliveryTemplateInput{UserID: userID, Name: draft.Name, Enabled: draft.Enabled, Messages: draft.Messages})
}

// Update 将应用草稿转换为数据库输入并更新模板。
func (r *DeliveryTemplateRepository) Update(ctx context.Context, userID, templateID int64, draft deliveryapp.Draft) error {
	// err 保存适配器初始化校验失败原因。
	if err := r.validate(); err != nil {
		return err
	}
	// guardErr 保存图片库图片归属校验的失败原因，非空时拒绝写入。
	if guardErr := r.ensureLibraryImages(ctx, userID, draft.Messages); guardErr != nil {
		return guardErr
	}
	// err 保存模板更新结果。
	err := r.store.DeliveryTemplates.Update(ctx, userID, templateID, db.DeliveryTemplateInput{UserID: userID, Name: draft.Name, Enabled: draft.Enabled, Messages: draft.Messages})
	if errors.Is(err, db.ErrNotFound) {
		return deliveryapp.ErrNotFound
	}
	if errors.Is(err, db.ErrDeliveryTemplateVariableConflict) {
		return deliveryapp.ErrVariableConflict
	}
	return err
}

// Delete 逻辑删除模板并映射引用冲突。
func (r *DeliveryTemplateRepository) Delete(ctx context.Context, userID, templateID int64) error {
	// err 保存适配器初始化校验失败原因。
	if err := r.validate(); err != nil {
		return err
	}
	// err 保存模板删除结果。
	err := r.store.DeliveryTemplates.Delete(ctx, userID, templateID)
	if errors.Is(err, db.ErrNotFound) {
		return deliveryapp.ErrNotFound
	}
	if errors.Is(err, db.ErrDeliveryTemplateReferenced) {
		return deliveryapp.ErrReferenced
	}
	return err
}

// validate 检查适配器是否绑定了模板数据库仓储。
func (r *DeliveryTemplateRepository) validate() error {
	if r == nil || r.store == nil || r.store.DeliveryTemplates == nil {
		return errors.New("发货模板数据库适配器未初始化")
	}
	return nil
}

// deliveryTemplateApplicationModel 将数据库模板转换为应用模型。
func deliveryTemplateApplicationModel(item db.DeliveryTemplate) deliveryapp.Template {
	// messages 保存脱离数据库模型的模板消息列表。
	messages := make([]deliveryapp.Message, 0, len(item.Messages))
	for /* message 表示当前模板消息。 */ _, message := range item.Messages {
		messages = append(messages, deliveryapp.Message{ID: message.ID, SortOrder: message.SortOrder, Content: message.Content})
	}
	return deliveryapp.Template{ID: item.ID, Name: item.Name, Enabled: item.Enabled, Messages: messages, Keys: append([]string(nil), item.Keys...), CustomKeys: append([]string(nil), item.CustomKeys...), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

var _ deliveryapp.Repository = (*DeliveryTemplateRepository)(nil)

// ensureLibraryImages 确认模板图片消息引用的图片库图片都属于当前用户，引用他人或已删除的图片按输入错误处理。
func (r *DeliveryTemplateRepository) ensureLibraryImages(ctx context.Context, userID int64, messages []string) error {
	// message 是当前模板消息；只有整条消息是图片令牌时才引用图片库。
	for _, message := range messages {
		// imageID、isImage 保存令牌解析结果。
		imageID, isImage := deliverytemplate.ImageID(message)
		if !isImage {
			continue
		}
		// owned、err 保存图片归属检查结果。
		owned, err := libraryImageOwnedByUser(ctx, r.store, userID, imageID)
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("%w: 图片库图片不存在", deliveryapp.ErrInvalidInput)
		}
	}
	return nil
}
