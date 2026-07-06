package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// OrderRow 订单列表展示行（含 item_title）。
type OrderRow struct {
	OrderID       string
	ItemID        string
	ItemTitle     string
	BuyerID       string
	SpecName      string
	SpecValue     string
	Quantity      string
	Amount        string
	OrderStatus   string
	CookieID      string
	IsBargain     int
	SystemShipped bool
	ReceiverName  string
	ReceiverPhone string
	ReceiverAddr  string
	ReceiverCity  string
	CreatedAt     string
	UpdatedAt     string
}

// ByCookie 取某账号的订单（limit 上限）。
func (o *Orders) ByCookie(ctx context.Context, cookieID string, limit int) ([]OrderRow, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := o.DB.QueryContext(ctx,
		`SELECT order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount,
		        order_status, is_bargain, system_shipped, receiver_name, receiver_phone,
		        receiver_address, receiver_city, created_at, updated_at
		 FROM orders WHERE cookie_id=? ORDER BY created_at DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrderRow
	for rows.Next() {
		var r OrderRow
		var itemID, buyerID, specName, specValue, qty, amount, receiverName, receiverPhone, receiverAddr, receiverCity sql.NullString
		var isBargain, sysShipped int
		if err := rows.Scan(&r.OrderID, &itemID, &buyerID, &specName, &specValue, &qty, &amount,
			&r.OrderStatus, &isBargain, &sysShipped, &receiverName, &receiverPhone, &receiverAddr,
			&receiverCity, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.ItemID = itemID.String
		r.BuyerID = buyerID.String
		r.SpecName = specName.String
		r.SpecValue = specValue.String
		r.Quantity = qty.String
		r.Amount = amount.String
		r.IsBargain = isBargain
		r.SystemShipped = sysShipped != 0
		r.ReceiverName = receiverName.String
		r.ReceiverPhone = receiverPhone.String
		r.ReceiverAddr = receiverAddr.String
		r.ReceiverCity = receiverCity.String
		r.CookieID = cookieID
		out = append(out, r)
	}
	return out, rows.Err()
}

// OrderStatusMap 将数字状态码转换为文本状态。
var OrderStatusMap = map[string]string{
	"1": "processing", "2": "pending_ship", "3": "shipped", "4": "completed",
	"5": "refunding", "6": "cancelled", "7": "refunding", "8": "cancelled",
	"9": "refunding", "10": "cancelled", "11": "completed", "12": "cancelled",
}

// NormalizeOrderStatus 数字码归一为文本。
func NormalizeOrderStatus(s string) string {
	if t, ok := OrderStatusMap[s]; ok {
		return t
	}
	if s == "" {
		return "unknown"
	}
	return s
}

// AllTitles 取全部 item_id → item_title 映射（订单列表用）。
func (i *Items) AllTitles(ctx context.Context) (map[string]string, error) {
	rows, err := i.DB.QueryContext(ctx, `SELECT item_id, item_title FROM item_info`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var id, title sql.NullString
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		m[id.String] = title.String
	}
	return m, rows.Err()
}

// 卡券 CRUD 辅助。

// CardFull 卡券完整信息（CRUD 用）。
type CardFull struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	APIConfig    string `json:"api_config"`
	TextContent  string `json:"text_content"`
	DataContent  string `json:"data_content"`
	ImageURL     string `json:"image_url"`
	Description  string `json:"description"`
	Enabled      bool   `json:"enabled"`
	DelaySeconds int    `json:"delay_seconds"`
	IsMultiSpec  bool   `json:"is_multi_spec"`
	SpecName     string `json:"spec_name"`
	SpecValue    string `json:"spec_value"`
	UserID       int64  `json:"user_id"`
}

// Get 取单个卡券。
func (c *Cards) Get(ctx context.Context, cardID int64) (*CardFull, error) {
	var cf CardFull
	var enabled, isMultiSpec int
	var apiCfg, textContent, dataContent, imageURL, specName, specValue, desc sql.NullString
	err := c.DB.QueryRowContext(ctx,
		`SELECT id, name, type, api_config, text_content, data_content, image_url, description,
		        enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id
		 FROM cards WHERE id=?`, cardID).Scan(
		&cf.ID, &cf.Name, &cf.Type, &apiCfg, &textContent, &dataContent, &imageURL, &desc,
		&enabled, &cf.DelaySeconds, &isMultiSpec, &specName, &specValue, &cf.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	cf.APIConfig = apiCfg.String
	cf.TextContent = textContent.String
	cf.DataContent = dataContent.String
	cf.ImageURL = imageURL.String
	cf.Description = desc.String
	cf.Enabled = enabled != 0
	cf.IsMultiSpec = isMultiSpec != 0
	cf.SpecName = specName.String
	cf.SpecValue = specValue.String
	return &cf, nil
}

// AllForUser 取某用户全部卡券。
func (c *Cards) AllForUser(ctx context.Context, userID int64) ([]CardFull, error) {
	rows, err := c.DB.QueryContext(ctx,
		`SELECT id, name, type, api_config, text_content, data_content, image_url, description,
		        enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id
		 FROM cards WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardFull
	for rows.Next() {
		var cf CardFull
		var enabled, isMultiSpec int
		var apiCfg, textContent, dataContent, imageURL, specName, specValue, desc sql.NullString
		if err := rows.Scan(&cf.ID, &cf.Name, &cf.Type, &apiCfg, &textContent, &dataContent, &imageURL, &desc,
			&enabled, &cf.DelaySeconds, &isMultiSpec, &specName, &specValue, &cf.UserID); err != nil {
			return nil, err
		}
		cf.APIConfig = apiCfg.String
		cf.TextContent = textContent.String
		cf.DataContent = dataContent.String
		cf.ImageURL = imageURL.String
		cf.Description = desc.String
		cf.Enabled = enabled != 0
		cf.IsMultiSpec = isMultiSpec != 0
		cf.SpecName = specName.String
		cf.SpecValue = specValue.String
		out = append(out, cf)
	}
	return out, rows.Err()
}

// Create 创建卡券，返回新 ID。
func (c *Cards) Create(ctx context.Context, cf *CardFull) (int64, error) {
	return insertReturningID(ctx, c.DB, c.Dialect,
		`INSERT INTO cards (name, type, api_config, text_content, data_content, image_url, description,
		    enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		cf.Name, cf.Type, nullable(cf.APIConfig), nullable(cf.TextContent), nullable(cf.DataContent),
		nullable(cf.ImageURL), nullable(cf.Description), boolToInt(cf.Enabled), cf.DelaySeconds,
		boolToInt(cf.IsMultiSpec), nullable(cf.SpecName), nullable(cf.SpecValue), cf.UserID)
}

// Update 更新卡券。
func (c *Cards) Update(ctx context.Context, cf *CardFull) error {
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cards SET name=?, type=?, api_config=?, text_content=?, data_content=?, image_url=?,
		    description=?, enabled=?, delay_seconds=?, is_multi_spec=?, spec_name=?, spec_value=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		cf.Name, cf.Type, nullable(cf.APIConfig), nullable(cf.TextContent), nullable(cf.DataContent),
		nullable(cf.ImageURL), nullable(cf.Description), boolToInt(cf.Enabled), cf.DelaySeconds,
		boolToInt(cf.IsMultiSpec), nullable(cf.SpecName), nullable(cf.SpecValue), cf.ID)
	return err
}

// Delete 删除卡券。
func (c *Cards) Delete(ctx context.Context, cardID int64) error {
	_, err := c.DB.ExecContext(ctx, `DELETE FROM cards WHERE id=?`, cardID)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// 占位防未用 fmt（未来扩展）。
var _ = fmt.Sprintf
var _ = strings.TrimSpace
