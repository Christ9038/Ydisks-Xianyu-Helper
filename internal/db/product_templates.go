package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ProductTemplate 对应 product_templates；JSON 字段由应用层校验后序列化，数据库层不解释其业务含义。
type ProductTemplate struct {
	// ID 是模板主键，新建前为 0。
	ID int64
	// UserID 是模板所属用户。
	UserID int64
	// Name 是模板在资源库中的备注名。
	Name string
	// Title 是发布时使用的商品标题。
	Title string
	// Description 是发布时使用的商品描述。
	Description string
	// PriceCents 是售价，单位为分；多规格时仅作展示。
	PriceCents int64
	// OriginalPriceCents 是原价，单位为分。
	OriginalPriceCents int64
	// Quantity 是库存；多规格时由各 SKU 库存汇总。
	Quantity int
	// PostageMode 是邮费模式。
	PostageMode string
	// PostageCents 是固定邮费，单位为分。
	PostageCents int64
	// CategoryJSON 是类目配置 JSON。
	CategoryJSON string
	// ImageIDsJSON 是模板主图的图片库主键 JSON 数组（顺序即主图顺序）；规格值图片的主键保存在 SpecsJSON 的 spec_image_ids 中，删除图片的引用检查需同时扫描两处。
	ImageIDsJSON string
	// SpecsJSON 是规格维度与 SKU 的 JSON，空对象表示单规格。
	SpecsJSON string
	// AutomationJSON 是发布后自动化配置 JSON。
	AutomationJSON string
	// CreatedAt 是创建时间文本。
	CreatedAt string
	// UpdatedAt 是最近更新时间文本。
	UpdatedAt string
}

// ProductTemplates 是商品模板的数据库访问入口。
type ProductTemplates struct {
	// DB 是共享数据库连接池。
	DB *sql.DB
	// Dialect 是当前数据库方言。
	Dialect Dialect
}

// productTemplateColumns 是读取模板时统一使用的列清单。
const productTemplateColumns = `id,user_id,name,title,description,price_cents,original_price_cents,quantity,postage_mode,postage_cents,category_json,image_ids_json,specs_json,automation_json,created_at,updated_at`

// scanProductTemplate 把一行查询结果读入模板模型。
func scanProductTemplate(scanner interface{ Scan(...any) error }) (ProductTemplate, error) {
	// t 保存当前扫描出的模板。
	var t ProductTemplate
	// err 保存扫描错误。
	err := scanner.Scan(&t.ID, &t.UserID, &t.Name, &t.Title, &t.Description, &t.PriceCents, &t.OriginalPriceCents, &t.Quantity,
		&t.PostageMode, &t.PostageCents, &t.CategoryJSON, &t.ImageIDsJSON, &t.SpecsJSON, &t.AutomationJSON, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// normalizeProductTemplateJSON 为空 JSON 字段补齐默认值，避免 MySQL 严格模式下写入空串。
func normalizeProductTemplateJSON(t *ProductTemplate) {
	if strings.TrimSpace(t.CategoryJSON) == "" {
		t.CategoryJSON = "{}"
	}
	if strings.TrimSpace(t.ImageIDsJSON) == "" {
		t.ImageIDsJSON = "[]"
	}
	if strings.TrimSpace(t.SpecsJSON) == "" {
		t.SpecsJSON = "{}"
	}
	if strings.TrimSpace(t.AutomationJSON) == "" {
		t.AutomationJSON = "{}"
	}
	if t.PostageMode == "" {
		t.PostageMode = "free"
	}
}

// List 返回用户的全部模板，按更新时间倒序。
func (p *ProductTemplates) List(ctx context.Context, userID int64) ([]ProductTemplate, error) {
	// rows、err 保存模板列表查询结果。
	rows, err := p.DB.QueryContext(ctx, `SELECT `+productTemplateColumns+` FROM product_templates WHERE user_id=? ORDER BY updated_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// templates 保存读取到的模板。
	templates := []ProductTemplate{}
	for rows.Next() {
		// t、scanErr 保存当前行及其扫描错误。
		t, scanErr := scanProductTemplate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// Get 按用户隔离读取单个模板；不存在或不属于该用户时返回 ErrNotFound。
func (p *ProductTemplates) Get(ctx context.Context, userID, id int64) (*ProductTemplate, error) {
	// t、err 保存查询结果。
	t, err := scanProductTemplate(p.DB.QueryRowContext(ctx, `SELECT `+productTemplateColumns+` FROM product_templates WHERE id=? AND user_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Create 写入新模板并返回主键。
func (p *ProductTemplates) Create(ctx context.Context, t ProductTemplate) (int64, error) {
	normalizeProductTemplateJSON(&t)
	return insertReturningID(ctx, p.DB, p.Dialect,
		`INSERT INTO product_templates (user_id,name,title,description,price_cents,original_price_cents,quantity,postage_mode,postage_cents,category_json,image_ids_json,specs_json,automation_json)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.UserID, t.Name, t.Title, t.Description, t.PriceCents, t.OriginalPriceCents, t.Quantity, t.PostageMode, t.PostageCents,
		t.CategoryJSON, t.ImageIDsJSON, t.SpecsJSON, t.AutomationJSON)
}

// Update 覆盖用户名下的模板；记录不存在返回 ErrNotFound。
func (p *ProductTemplates) Update(ctx context.Context, t ProductTemplate) error {
	normalizeProductTemplateJSON(&t)
	// res、err 保存更新结果。
	res, err := p.DB.ExecContext(ctx,
		`UPDATE product_templates SET name=?,title=?,description=?,price_cents=?,original_price_cents=?,quantity=?,postage_mode=?,postage_cents=?,
		 category_json=?,image_ids_json=?,specs_json=?,automation_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`,
		t.Name, t.Title, t.Description, t.PriceCents, t.OriginalPriceCents, t.Quantity, t.PostageMode, t.PostageCents,
		t.CategoryJSON, t.ImageIDsJSON, t.SpecsJSON, t.AutomationJSON, t.ID, t.UserID)
	if err != nil {
		return err
	}
	// affected 保存受影响行数；部分驱动在值未变化时返回 0，需再确认记录存在。
	if affected, _ := res.RowsAffected(); affected == 0 {
		// getErr 保存 Get 调用的失败原因。
		if _, getErr := p.Get(ctx, t.UserID, t.ID); getErr != nil {
			return getErr
		}
	}
	return nil
}

// Delete 删除用户名下的模板；记录不存在返回 ErrNotFound。
func (p *ProductTemplates) Delete(ctx context.Context, userID, id int64) error {
	// res、err 保存删除结果。
	res, err := p.DB.ExecContext(ctx, `DELETE FROM product_templates WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	// affected 保存受影响行数，用于识别不存在的模板。
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}
