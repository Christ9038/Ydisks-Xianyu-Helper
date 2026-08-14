package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// OrderRow 订单列表展示行（含 item_title）。
type OrderRow struct {
	OrderID       string
	ItemID        string
	ItemTitle     string
	ItemDetail    string
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

// OrderListFilter 是订单列表分页查询条件。
type OrderListFilter struct {
	UserID   int64
	CookieID string
	Status   string
	Search   string
	Limit    int
	Offset   int
}

// ListForUser 按用户隔离分页查询订单，并带出商品标题/详情。
func (o *Orders) ListForUser(ctx context.Context, f OrderListFilter) ([]OrderRow, int, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	where := []string{"c.user_id=?", "o.deleted_at IS NULL"}
	args := []any{f.UserID}
	if f.CookieID != "" {
		where = append(where, "o.cookie_id=?")
		args = append(args, f.CookieID)
	}
	if statuses := normalizedStatusCandidates(f.Status); len(statuses) > 0 {
		placeholders := make([]string, 0, len(statuses))
		for _, st := range statuses {
			placeholders = append(placeholders, "?")
			args = append(args, st)
		}
		where = append(where, "o.order_status IN ("+strings.Join(placeholders, ",")+")")
	}
	if search := strings.ToLower(strings.TrimSpace(f.Search)); search != "" {
		pattern := "%" + search + "%"
		where = append(where, `(LOWER(o.order_id) LIKE ? OR LOWER(COALESCE(o.item_id,'')) LIKE ?
			OR LOWER(COALESCE(o.buyer_id,'')) LIKE ? OR LOWER(COALESCE(i.item_title,'')) LIKE ?
			OR LOWER(COALESCE(o.receiver_name,'')) LIKE ? OR LOWER(COALESCE(o.receiver_phone,'')) LIKE ?)`)
		for i := 0; i < 6; i++ {
			args = append(args, pattern)
		}
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := o.DB.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM orders o
		   JOIN cookies c ON c.id=o.cookie_id
		   LEFT JOIN item_info i ON i.cookie_id=o.cookie_id AND i.item_id=o.item_id
		  WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, f.Limit, f.Offset)
	rows, err := o.DB.QueryContext(ctx,
		`SELECT o.order_id, o.item_id, COALESCE(i.item_title,''), COALESCE(i.item_detail,''),
		        o.buyer_id, o.spec_name, o.spec_value, o.quantity, o.amount,
		        o.order_status, o.cookie_id, o.is_bargain, o.system_shipped,
		        o.receiver_name, o.receiver_phone, o.receiver_address, o.receiver_city,
		        o.created_at, o.updated_at
		   FROM orders o
		   JOIN cookies c ON c.id=o.cookie_id
		   LEFT JOIN item_info i ON i.cookie_id=o.cookie_id AND i.item_id=o.item_id
		  WHERE `+whereSQL+`
		  ORDER BY o.created_at DESC, o.order_id DESC
		  LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []OrderRow{}
	for rows.Next() {
		var r OrderRow
		var itemID, itemTitle, itemDetail, buyerID, specName, specValue, qty, amount, receiverName, receiverPhone, receiverAddr, receiverCity sql.NullString
		var isBargain, sysShipped int
		if err := rows.Scan(&r.OrderID, &itemID, &itemTitle, &itemDetail, &buyerID, &specName, &specValue, &qty, &amount,
			&r.OrderStatus, &r.CookieID, &isBargain, &sysShipped, &receiverName, &receiverPhone, &receiverAddr,
			&receiverCity, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		r.ItemID = itemID.String
		r.ItemTitle = itemTitle.String
		r.ItemDetail = itemDetail.String
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
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func normalizedStatusCandidates(status string) []string {
	status = strings.TrimSpace(status)
	if status == "" || status == "all" {
		return nil
	}
	switch NormalizeOrderStatus(status) {
	case "processing":
		return []string{"processing", "1"}
	case "pending_ship":
		return []string{"pending_ship", "paid", "2"}
	case "shipped":
		return []string{"shipped", "3"}
	case "completed":
		return []string{"completed", "4", "11"}
	case "refunding":
		return []string{"refunding", "5", "7", "9"}
	case "cancelled":
		return []string{"cancelled", "6", "8", "10", "12"}
	case "unknown":
		return []string{status}
	default:
		return []string{status}
	}
}

// ByCookie 取某账号的订单（limit 上限）。
func (o *Orders) ByCookie(ctx context.Context, cookieID string, limit int) ([]OrderRow, error) {
	if limit <= 0 {
		limit = 1000
	}
	return o.ByCookiePage(ctx, cookieID, limit, 0)
}

// ByCookiePage 分页读取账号订单，供需要完整扫描的后台任务使用。
func (o *Orders) ByCookiePage(ctx context.Context, cookieID string, limit, offset int) ([]OrderRow, error) {
	if limit <= 0 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := o.DB.QueryContext(ctx,
		`SELECT order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount,
		        order_status, is_bargain, system_shipped, receiver_name, receiver_phone,
		        receiver_address, receiver_city, created_at, updated_at
		 FROM orders WHERE cookie_id=? AND deleted_at IS NULL ORDER BY created_at DESC,order_id DESC LIMIT ? OFFSET ?`, cookieID, limit, offset)
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

// SoftDeleteMissingForCookie 将本次完整卖家订单同步中未出现的本地订单逻辑删除。
// activeIDs 为空表示线上已确认没有任何卖家订单；调用方必须确保同步完整成功后再调用。
func (o *Orders) SoftDeleteMissingForCookie(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error) {
	if strings.TrimSpace(cookieID) == "" {
		return 0, errors.New("cookie_id 不能为空")
	}
	rows, err := o.DB.QueryContext(ctx,
		`SELECT order_id FROM orders WHERE cookie_id=? AND deleted_at IS NULL`, cookieID)
	if err != nil {
		return 0, err
	}
	orderIDs := make([]string, 0)
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	deleted := 0
	for _, orderID := range orderIDs {
		if _, ok := activeIDs[orderID]; ok {
			continue
		}
		result, err := o.DB.ExecContext(ctx, `UPDATE orders
			SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
			WHERE cookie_id=? AND order_id=? AND deleted_at IS NULL`, cookieID, orderID)
		if err != nil {
			return deleted, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += int(changed)
	}
	return deleted, nil
}

// OrderStatusMap 将数字状态码转换为文本状态。
var OrderStatusMap = map[string]string{
	"paid": "pending_ship",
	"1":    "processing", "2": "pending_ship", "3": "shipped", "4": "completed",
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

// ExistsOwned 判断卡密组是否属于指定用户。
func (c *Cards) ExistsOwned(ctx context.Context, cardID, userID int64) (bool, error) {
	var exists bool
	err := c.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id=? AND user_id=?)`, cardID, userID).Scan(&exists)
	return exists, err
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
		if errors.Is(err, sql.ErrNoRows) {
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
