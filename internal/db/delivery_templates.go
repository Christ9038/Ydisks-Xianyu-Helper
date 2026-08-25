package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"xianyu-go/internal/deliverytemplate"
)

// ErrDeliveryTemplateReferenced 表示模板仍被自动化动作引用，暂不能删除。
var ErrDeliveryTemplateReferenced = errors.New("发货模板仍被自动化规则引用")

// DeliveryTemplate 是发货模板的数据库读取模型和自动化执行摘要。
type DeliveryTemplate struct {
	// ID 是模板主键。
	ID int64
	// UserID 是模板所属用户。
	UserID int64
	// Name 是模板名称。
	Name string
	// Enabled 表示模板是否允许继续被新规则使用。
	Enabled bool
	// DeletedAt 是逻辑删除时间文本。
	DeletedAt string
	// CreatedAt 是创建时间文本。
	CreatedAt string
	// UpdatedAt 是更新时间文本。
	UpdatedAt string
	// Messages 是模板的有序消息。
	Messages []DeliveryTemplateMessage
	// Keys 是模板引用的卡密变量键。
	Keys []string
	// CustomKeys 是模板引用的发货规则自定义变量键。
	CustomKeys []string
}

// DeliveryTemplateMessage 是模板消息的数据库读取模型。
type DeliveryTemplateMessage struct {
	// ID 是消息主键。
	ID int64
	// TemplateID 是所属模板主键。
	TemplateID int64
	// SortOrder 是发送顺序。
	SortOrder int
	// Content 是消息内容。
	Content string
}

// DeliveryTemplateInput 是模板创建或更新的数据库写入模型。
type DeliveryTemplateInput struct {
	// UserID 是模板所有者。
	UserID int64
	// Name 是模板名称。
	Name string
	// Enabled 是模板启用状态。
	Enabled bool
	// Messages 是按顺序写入的消息正文。
	Messages []string
}

// DeliveryTemplateBinding 把模板变量键绑定到卡密组和每件发送数量。
type DeliveryTemplateBinding struct {
	// VariableKey 是模板中的变量键。
	VariableKey string
	// CardID 是被绑定的卡密组主键。
	CardID int64
	// CardName 是卡密组名称，仅用于规则展示。
	CardName string
	// DeliveryCount 是每购买一件需要准备的卡密份数。
	DeliveryCount int
}

// DeliveryTemplateStore 持有模板相关 SQL 操作和数据库方言。
type DeliveryTemplateStore struct {
	// DB 是模板数据所在数据库连接。
	DB *sql.DB
	// Dialect 决定自增主键返回方式。
	Dialect Dialect
}

// ListForUser 返回用户未删除的模板，并加载消息和变量键。
func (d *DeliveryTemplateStore) ListForUser(ctx context.Context, userID int64) ([]DeliveryTemplate, error) {
	// err 保存模板仓储初始化校验错误。
	if err := d.validate(); err != nil {
		return nil, err
	}
	// rows、err 保存模板列表查询结果及错误。
	rows, err := d.DB.QueryContext(ctx, `SELECT id,user_id,name,enabled,created_at,updated_at FROM delivery_templates WHERE user_id=? AND deleted_at IS NULL ORDER BY updated_at DESC,id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// templates 保存用户可见模板列表。
	templates := make([]DeliveryTemplate, 0)
	for rows.Next() {
		// template 保存当前扫描到的模板。
		var template DeliveryTemplate
		// enabled 保存数据库中的整数布尔值。
		var enabled int
		// err 保存当前模板行扫描错误。
		if err := rows.Scan(&template.ID, &template.UserID, &template.Name, &enabled, &template.CreatedAt, &template.UpdatedAt); err != nil {
			return nil, err
		}
		template.Enabled = enabled != 0
		// err 保存当前模板消息加载错误。
		if err := d.loadMessages(ctx, &template); err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, rows.Err()
}

// GetForUser 返回用户拥有的单个未删除模板。
func (d *DeliveryTemplateStore) GetForUser(ctx context.Context, userID, templateID int64) (*DeliveryTemplate, error) {
	// err 保存模板仓储初始化校验错误。
	if err := d.validate(); err != nil {
		return nil, err
	}
	// template 保存查询到的模板。
	var template DeliveryTemplate
	// enabled 保存数据库中的整数布尔值。
	var enabled int
	// err 保存单个模板查询错误。
	err := d.DB.QueryRowContext(ctx, `SELECT id,user_id,name,enabled,created_at,updated_at FROM delivery_templates WHERE id=? AND user_id=? AND deleted_at IS NULL`, templateID, userID).Scan(&template.ID, &template.UserID, &template.Name, &enabled, &template.CreatedAt, &template.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	template.Enabled = enabled != 0
	// err 保存当前模板消息加载错误。
	if err := d.loadMessages(ctx, &template); err != nil {
		return nil, err
	}
	return &template, nil
}

// loadMessages 加载模板消息，并从消息内容解析变量键。
func (d *DeliveryTemplateStore) loadMessages(ctx context.Context, template *DeliveryTemplate) error {
	// rows、err 保存模板消息查询结果及错误。
	rows, err := d.DB.QueryContext(ctx, `SELECT id,template_id,sort_order,content FROM delivery_template_messages WHERE template_id=? ORDER BY sort_order ASC,id ASC`, template.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	// contents 保存供解析器使用的有序消息正文。
	contents := make([]string, 0)
	template.Messages = make([]DeliveryTemplateMessage, 0)
	for rows.Next() {
		// message 保存当前扫描到的消息。
		var message DeliveryTemplateMessage
		// err 保存当前消息行扫描错误。
		if err := rows.Scan(&message.ID, &message.TemplateID, &message.SortOrder, &message.Content); err != nil {
			return err
		}
		template.Messages = append(template.Messages, message)
		contents = append(contents, message.Content)
	}
	// err 保存模板消息遍历错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// parsed 保存模板变量解析结果；历史脏数据在展示时仍保留原消息。
	parsed, parseErr := deliverytemplate.Parse(contents)
	if parseErr == nil {
		template.Keys = parsed.Keys
		template.CustomKeys = parsed.CustomKeys
	}
	return nil
}

// Create 校验并事务性创建模板及其消息。
func (d *DeliveryTemplateStore) Create(ctx context.Context, input DeliveryTemplateInput) (int64, error) {
	// err 保存模板仓储初始化校验错误。
	if err := d.validate(); err != nil {
		return 0, err
	}
	// parsed 保存消息规范化和变量提取结果。
	// parsed、err 保存消息解析结果及错误。
	parsed, err := deliverytemplate.Parse(input.Messages)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return 0, errors.New("发货模板名称不能为空")
	}
	// tx、err 保存模板创建事务及开启错误。
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// templateID 保存新模板的数据库主键。
	templateID, err := insertReturningID(ctx, tx, d.Dialect, `INSERT INTO delivery_templates (user_id,name,enabled) VALUES (?,?,?)`, input.UserID, strings.TrimSpace(input.Name), boolToInt(input.Enabled))
	if err != nil {
		return 0, err
	}
	// err 保存模板消息批量写入错误。
	if err := insertDeliveryTemplateMessages(ctx, tx, templateID, parsed.Messages); err != nil {
		return 0, err
	}
	// err 保存模板创建事务提交错误。
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return templateID, nil
}

// Update 校验并事务性替换模板名称、状态和消息列表。
func (d *DeliveryTemplateStore) Update(ctx context.Context, userID, templateID int64, input DeliveryTemplateInput) error {
	// err 保存模板仓储初始化校验错误。
	if err := d.validate(); err != nil {
		return err
	}
	// parsed 保存消息规范化结果。
	// parsed、err 保存消息解析结果及错误。
	parsed, err := deliverytemplate.Parse(input.Messages)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("发货模板名称不能为空")
	}
	// tx、err 保存模板更新事务及开启错误。
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// res、err 保存模板基础字段更新结果及错误。
	res, err := tx.ExecContext(ctx, `UPDATE delivery_templates SET name=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=? AND deleted_at IS NULL`, strings.TrimSpace(input.Name), boolToInt(input.Enabled), templateID, userID)
	if err != nil {
		return err
	}
	// affected 保存本次更新实际命中的模板数量。
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	// err 保存旧模板消息清理错误。
	if _, err := tx.ExecContext(ctx, `DELETE FROM delivery_template_messages WHERE template_id=?`, templateID); err != nil {
		return err
	}
	// err 保存新模板消息写入错误。
	if err := insertDeliveryTemplateMessages(ctx, tx, templateID, parsed.Messages); err != nil {
		return err
	}
	return tx.Commit()
}

// insertDeliveryTemplateMessages 按顺序插入模板消息。
func insertDeliveryTemplateMessages(ctx context.Context, tx *sql.Tx, templateID int64, messages []string) error {
	for /* index 表示消息顺序；content 表示消息正文。 */ index, content := range messages {
		// err 保存单条模板消息写入错误。
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_template_messages (template_id,sort_order,content) VALUES (?,?,?)`, templateID, index+1, content); err != nil {
			return err
		}
	}
	return nil
}

// Delete 仅在没有动作引用时逻辑删除模板。
func (d *DeliveryTemplateStore) Delete(ctx context.Context, userID, templateID int64) error {
	// err 保存模板仓储初始化校验错误。
	if err := d.validate(); err != nil {
		return err
	}
	// owned 保存模板是否属于当前用户，先做归属校验避免泄露其他用户的引用状态。
	var owned bool
	// err 保存模板归属查询错误。
	if err := d.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM delivery_templates WHERE id=? AND user_id=? AND deleted_at IS NULL)`, templateID, userID).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return ErrNotFound
	}
	// references 保存仍引用该模板的动作数量。
	var references int
	// err 保存模板引用计数查询错误。
	if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rule_actions WHERE delivery_template_id=?`, templateID).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return ErrDeliveryTemplateReferenced
	}
	// res、err 保存模板逻辑删除结果及错误。
	res, err := d.DB.ExecContext(ctx, `UPDATE delivery_templates SET deleted_at=CURRENT_TIMESTAMP,enabled=0,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=? AND deleted_at IS NULL`, templateID, userID)
	if err != nil {
		return err
	}
	// affected 保存本次删除实际命中的模板数量。
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// validate 检查模板仓储是否已经绑定数据库。
func (d *DeliveryTemplateStore) validate() error {
	if d == nil || d.DB == nil {
		return errors.New("发货模板仓储未初始化")
	}
	return nil
}
