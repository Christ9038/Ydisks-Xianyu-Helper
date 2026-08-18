package db

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
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
	// where 用于本次流程后续判断的where
	where := []string{"c.user_id=?", "o.deleted_at IS NULL"}
	// args 用于本次流程后续判断的args
	args := []any{f.UserID}
	if f.CookieID != "" {
		where = append(where, "o.cookie_id=?")
		args = append(args, f.CookieID)
	}
	if // statuses 用于本次流程后续判断的statuses
	statuses := normalizedStatusCandidates(f.Status); len(statuses) > 0 {
		// placeholders 用于本次流程后续判断的placeholders
		placeholders := make([]string, 0, len(statuses))
		// st 表示当前遍历过程中的st
		for _, st := range statuses {
			placeholders = append(placeholders, "?")
			args = append(args, st)
		}
		where = append(where, "o.order_status IN ("+strings.Join(placeholders, ",")+")")
	}
	if // search 用于本次流程后续判断的搜索
	search := strings.ToLower(strings.TrimSpace(f.Search)); search != "" {
		// pattern 用于本次流程后续判断的pattern
		pattern := "%" + search + "%"
		where = append(where, `(LOWER(o.order_id) LIKE ? OR LOWER(COALESCE(o.item_id,'')) LIKE ?
			OR LOWER(COALESCE(o.buyer_id,'')) LIKE ? OR LOWER(COALESCE(i.item_title,'')) LIKE ?
			OR LOWER(COALESCE(o.receiver_name,'')) LIKE ? OR LOWER(COALESCE(o.receiver_phone,'')) LIKE ?)`)
		for // i 用于本次流程后续判断的i
		i := 0; i < 6; i++ {
			args = append(args, pattern)
		}
	}
	// whereSQL 用于本次流程后续判断的whereSQL
	whereSQL := strings.Join(where, " AND ")

	// total 用于本次流程后续判断的总数
	var total int
	if // err 用于本次流程后续判断的err
	err := o.DB.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM orders o
		   JOIN cookies c ON c.id=o.cookie_id
		   LEFT JOIN item_info i ON i.cookie_id=o.cookie_id AND i.item_id=o.item_id
		  WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// queryArgs 用于本次流程后续判断的查询Args
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, f.Limit, f.Offset)
	// rows、err 用于本次流程后续判断的rows、err
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
	// out 用于本次流程后续判断的out
	out := []OrderRow{}
	for rows.Next() {
		// r 用于本次流程后续判断的r
		var r OrderRow
		// itemID、itemTitle、itemDetail、buyerID、specName、specValue、qty、amount、receiverName、receiverPhone、receiverAddr、receiverCity 保存商品ID、itemTitle、itemDetail、buyerID、specName、specValue、qty、amount、receiverName、receiverPhone、receiverAddr、receiverCity，供当前处理流程使用
		var itemID, itemTitle, itemDetail, buyerID, specName, specValue, qty, amount, receiverName, receiverPhone, receiverAddr, receiverCity sql.NullString
		// isBargain、sysShipped 用于本次流程后续判断的isBargain、sysShipped
		var isBargain, sysShipped int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&r.OrderID, &itemID, &itemTitle, &itemDetail, &buyerID, &specName, &specValue, &qty, &amount,
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

// normalizedStatusCandidates 封装normalized状态Candidates业务协调。
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
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := o.DB.QueryContext(ctx,
		`SELECT order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount,
		        order_status, is_bargain, system_shipped, receiver_name, receiver_phone,
		        receiver_address, receiver_city, created_at, updated_at
		 FROM orders WHERE cookie_id=? AND deleted_at IS NULL ORDER BY created_at DESC,order_id DESC LIMIT ? OFFSET ?`, cookieID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrderRows(rows, cookieID)
}

// ByCookieCursor 使用 created_at 与 order_id 复合游标扫描账号订单，避免大 OFFSET 的线性跳过成本。
func (o *Orders) ByCookieCursor(ctx context.Context, cookieID string, limit int, afterCreatedAt, afterOrderID string) ([]OrderRow, error) {
	if limit <= 0 {
		limit = 500
	}
	// query 保存按复合游标扫描订单的 SQL。
	query := `SELECT order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount,
		        order_status, is_bargain, system_shipped, receiver_name, receiver_phone,
		        receiver_address, receiver_city, created_at, updated_at
		 FROM orders WHERE cookie_id=? AND deleted_at IS NULL`
	// args 保存游标查询参数。
	args := []any{cookieID}
	if afterCreatedAt != "" || afterOrderID != "" {
		// cursorCreatedAt 将驱动层返回的 RFC3339 时间还原为数据库通用的时间文本格式。
		cursorCreatedAt := normalizeOrderCursorTime(afterCreatedAt)
		query += ` AND (created_at < ? OR (created_at = ? AND order_id < ?))`
		args = append(args, cursorCreatedAt, cursorCreatedAt, afterOrderID)
	}
	query += ` ORDER BY created_at DESC,order_id DESC LIMIT ?`
	args = append(args, limit)
	// rows、err 保存游标查询结果及错误。
	rows, err := o.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrderRows(rows, cookieID)
}

// normalizeOrderCursorTime 将订单行时间转换为跨数据库可比较的 UTC 文本。
func normalizeOrderCursorTime(value string) string {
	// parsed 表示驱动层返回的标准时间值。
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.UTC().Format("2006-01-02 15:04:05.999999999")
	}
	return strings.Replace(strings.TrimSuffix(value, "Z"), "T", " ", 1)
}

// scanOrderRows 将订单查询行转换为统一的订单列表模型。
func scanOrderRows(rows *sql.Rows, cookieID string) ([]OrderRow, error) {
	// out 用于本次流程后续判断的out
	var out []OrderRow
	for rows.Next() {
		// r 用于本次流程后续判断的r
		var r OrderRow
		// itemID、buyerID、specName、specValue、qty、amount、receiverName、receiverPhone、receiverAddr、receiverCity 保存商品ID、buyerID、specName、specValue、qty、amount、receiverName、receiverPhone、receiverAddr、receiverCity，供当前处理流程使用
		var itemID, buyerID, specName, specValue, qty, amount, receiverName, receiverPhone, receiverAddr, receiverCity sql.NullString
		// isBargain、sysShipped 用于本次流程后续判断的isBargain、sysShipped
		var isBargain, sysShipped int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&r.OrderID, &itemID, &buyerID, &specName, &specValue, &qty, &amount,
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
// SoftDeleteMissingForCookie 封装SoftDeleteMissingFor登录凭证业务协调。
func (o *Orders) SoftDeleteMissingForCookie(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error) {
	if strings.TrimSpace(cookieID) == "" {
		return 0, errors.New("cookie_id 不能为空")
	}
	// args 保存批量 UPDATE 的参数，首个参数是账号 ID。
	args := []any{cookieID}
	// query 保存批量逻辑删除 SQL。
	query := `UPDATE orders SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND deleted_at IS NULL`
	if len(activeIDs) > 0 {
		// activeOrderIDs 按稳定顺序保存线上仍存在的订单 ID，便于日志和测试复现。
		activeOrderIDs := make([]string, 0, len(activeIDs))
		// orderID 表示当前线上仍存在的订单标识。
		for orderID := range activeIDs {
			activeOrderIDs = append(activeOrderIDs, orderID)
		}
		sort.Strings(activeOrderIDs)
		// placeholders 保存 NOT IN 子句所需的占位符。
		placeholders := make([]string, len(activeOrderIDs))
		// i、orderID 分别表示占位符序号和线上订单标识。
		for i, orderID := range activeOrderIDs {
			placeholders[i] = "?"
			args = append(args, orderID)
		}
		query += ` AND order_id NOT IN (` + strings.Join(placeholders, ",") + `)`
	}
	// result、err 保存批量逻辑删除结果及错误。
	result, err := o.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	// deleted 保存本次批量逻辑删除的订单数量。
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(deleted), nil
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
	if // t、ok 用于本次流程后续判断的t、ok
	t, ok := OrderStatusMap[s]; ok {
		return t
	}
	if s == "" {
		return "unknown"
	}
	return s
}

// AllTitles 取全部 item_id → item_title 映射（订单列表用）。
func (i *Items) AllTitles(ctx context.Context) (map[string]string, error) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := i.DB.QueryContext(ctx, `SELECT item_id, item_title FROM item_info`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// m 用于本次流程后续判断的m
	m := make(map[string]string)
	for rows.Next() {
		// id、title 用于本次流程后续判断的id、title
		var id, title sql.NullString
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&id, &title); err != nil {
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
	// exists 用于本次流程后续判断的exists
	var exists bool
	// err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id=? AND user_id=?)`, cardID, userID).Scan(&exists)
	return exists, err
}

// Get 取单个卡券。
func (c *Cards) Get(ctx context.Context, cardID int64) (*CardFull, error) {
	// cf 用于本次流程后续判断的cf
	var cf CardFull
	// enabled、isMultiSpec 用于本次流程后续判断的enabled、isMultiSpec
	var enabled, isMultiSpec int
	// apiCfg、textContent、dataContent、imageURL、specName、specValue、desc 用于本次流程后续判断的apiCfg、textContent、dataContent、imageURL、specName、specValue、desc
	var apiCfg, textContent, dataContent, imageURL, specName, specValue, desc sql.NullString
	// err 用于本次流程后续判断的err
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
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := c.DB.QueryContext(ctx,
		`SELECT id, name, type, api_config, text_content, data_content, image_url, description,
		        enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id
		 FROM cards WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	var out []CardFull
	for rows.Next() {
		// cf 用于本次流程后续判断的cf
		var cf CardFull
		// enabled、isMultiSpec 用于本次流程后续判断的enabled、isMultiSpec
		var enabled, isMultiSpec int
		// apiCfg、textContent、dataContent、imageURL、specName、specValue、desc 用于本次流程后续判断的apiCfg、textContent、dataContent、imageURL、specName、specValue、desc
		var apiCfg, textContent, dataContent, imageURL, specName, specValue, desc sql.NullString
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&cf.ID, &cf.Name, &cf.Type, &apiCfg, &textContent, &dataContent, &imageURL, &desc,
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
	// err 用于本次流程后续判断的err
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
	// err 用于本次流程后续判断的err
	_, err := c.DB.ExecContext(ctx, `DELETE FROM cards WHERE id=?`, cardID)
	return err
}

// nullable 封装nullable业务协调。
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
