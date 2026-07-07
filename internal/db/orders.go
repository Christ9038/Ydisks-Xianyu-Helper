package db

import (
	"context"
	"database/sql"
	"errors"
)

// Order 对应 orders 表。
type Order struct {
	OrderID             string
	ItemID              string
	BuyerID             string
	SpecName            string
	SpecValue           string
	Quantity            string
	Amount              string
	OrderStatus         string
	CookieID            string
	IsBargain           int
	ReceiverName        string
	ReceiverPhone       string
	ReceiverAddr        string
	ReceiverCity        string
	Version             int
	ChatID              string
	SystemShipped       bool
	PaidAt              string
	ShippedAt           string
	CompletedAt         string
	BuyerReviewedAt     string
	LastReviewRequestAt string
	ReviewRequestCount  int
	CreatedAt           string
	UpdatedAt           string
}

// Orders 订单操作。
type Orders struct {
	DB      *sql.DB
	Dialect Dialect
}

// Upsert 插入或更新订单。仅更新提供的非零字段（INSERT OR IGNORE 占位 + 动态 UPDATE）。
// 注：orders.version 列当前仅作占位读取，不参与并发控制——SQLite 单写者序列化写入，
// 且本场景无多进程/多协程并发更新同一订单的需求，故未实现乐观锁以避免引入失败重试复杂度。
func (o *Orders) Upsert(ctx context.Context, orderID string, opts OrderUpsertOpts) error {
	if orderID == "" {
		return errors.New("order_id 不能为空")
	}
	// 先尝试插入占位（冲突忽略）。order_id 是主键。
	_, err := o.DB.ExecContext(ctx,
		dialectInsertIgnorePrefix(o.Dialect)+` INTO orders (order_id, item_id, buyer_id, cookie_id, order_status, version)
		 VALUES (?, ?, ?, ?, 'unknown', 1)`+dialectInsertIgnore(o.Dialect, []string{"order_id"}),
		orderID, opts.ItemID, opts.BuyerID, opts.CookieID)
	if err != nil {
		return err
	}
	if opts.CookieID != "" {
		var existing sql.NullString
		if err := o.DB.QueryRowContext(ctx, `SELECT cookie_id FROM orders WHERE order_id=?`, orderID).Scan(&existing); err != nil {
			return err
		}
		if existing.Valid && existing.String != "" && existing.String != opts.CookieID {
			return ErrForbidden
		}
	}

	// 动态构造 UPDATE。
	set := []string{}
	args := []any{}
	if opts.SystemShipped != nil {
		set = append(set, "system_shipped=?")
		args = append(args, boolToInt(*opts.SystemShipped))
	}
	if opts.ChatID != "" {
		set = append(set, "chat_id=?")
		args = append(args, opts.ChatID)
	}
	if opts.ItemID != "" {
		set = append(set, "item_id=?")
		args = append(args, opts.ItemID)
	}
	if opts.BuyerID != "" {
		set = append(set, "buyer_id=?")
		args = append(args, opts.BuyerID)
	}
	if opts.CookieID != "" {
		set = append(set, "cookie_id=?")
		args = append(args, opts.CookieID)
	}
	if opts.OrderStatus != "" {
		set = append(set, "order_status=?")
		args = append(args, opts.OrderStatus)
	}
	if opts.SpecName != "" {
		set = append(set, "spec_name=?")
		args = append(args, opts.SpecName)
	}
	if opts.SpecValue != "" {
		set = append(set, "spec_value=?")
		args = append(args, opts.SpecValue)
	}
	if opts.Quantity != "" {
		set = append(set, "quantity=?")
		args = append(args, opts.Quantity)
	}
	if opts.Amount != "" {
		set = append(set, "amount=?")
		args = append(args, opts.Amount)
	}
	if opts.ReceiverName != "" {
		set = append(set, "receiver_name=?")
		args = append(args, opts.ReceiverName)
	}
	if opts.ReceiverPhone != "" {
		set = append(set, "receiver_phone=?")
		args = append(args, opts.ReceiverPhone)
	}
	if opts.ReceiverAddr != "" {
		set = append(set, "receiver_address=?")
		args = append(args, opts.ReceiverAddr)
	}
	if opts.ReceiverCity != "" {
		set = append(set, "receiver_city=?")
		args = append(args, opts.ReceiverCity)
	}
	if len(set) == 0 {
		return nil // 无字段可更新
	}
	set = append(set, "updated_at=CURRENT_TIMESTAMP")
	args = append(args, orderID)
	q := `UPDATE orders SET ` + joinSet(set) + ` WHERE order_id=?`
	_, err = o.DB.ExecContext(ctx, q, args...)
	return err
}

// OrderUpsertOpts Upsert 的可选字段。
type OrderUpsertOpts struct {
	ItemID        string
	BuyerID       string
	CookieID      string
	OrderStatus   string
	SpecName      string
	SpecValue     string
	Quantity      string
	Amount        string
	ReceiverName  string
	ReceiverPhone string
	ReceiverAddr  string
	ReceiverCity  string
	ChatID        string
	SystemShipped *bool
}

// Get 按 order_id 查询。
func (o *Orders) Get(ctx context.Context, orderID string) (*Order, error) {
	var ord Order
	var isBargain, version, sysShipped int
	var itemID, buyerID, specName, specValue, qty, amount, status, cookieID, receiverName, receiverPhone, receiverAddr, receiverCity, chatID, paidAt, shippedAt, completedAt, buyerReviewedAt, lastReviewRequestAt, createdAt, updatedAt sql.NullString
	err := o.DB.QueryRowContext(ctx,
		`SELECT order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount,
		        order_status, cookie_id, is_bargain, receiver_name, receiver_phone, receiver_address,
		        receiver_city, version, chat_id, system_shipped,
		        COALESCE(paid_at,''),COALESCE(shipped_at,''),COALESCE(completed_at,''),
		        COALESCE(buyer_reviewed_at,''),COALESCE(last_review_request_at,''),review_request_count,
		        created_at, updated_at
		 FROM orders WHERE order_id=?`, orderID).Scan(
		&ord.OrderID, &itemID, &buyerID, &specName, &specValue, &qty, &amount,
		&status, &cookieID, &isBargain, &receiverName, &receiverPhone, &receiverAddr,
		&receiverCity, &version, &chatID, &sysShipped, &paidAt, &shippedAt, &completedAt,
		&buyerReviewedAt, &lastReviewRequestAt, &ord.ReviewRequestCount, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ord.ItemID = itemID.String
	ord.BuyerID = buyerID.String
	ord.SpecName = specName.String
	ord.SpecValue = specValue.String
	ord.Quantity = qty.String
	ord.Amount = amount.String
	ord.OrderStatus = status.String
	ord.CookieID = cookieID.String
	ord.IsBargain = isBargain
	ord.ReceiverName = receiverName.String
	ord.ReceiverPhone = receiverPhone.String
	ord.ReceiverAddr = receiverAddr.String
	ord.ReceiverCity = receiverCity.String
	ord.Version = version
	ord.ChatID = chatID.String
	ord.SystemShipped = sysShipped != 0
	ord.PaidAt = paidAt.String
	ord.ShippedAt = shippedAt.String
	ord.CompletedAt = completedAt.String
	ord.BuyerReviewedAt = buyerReviewedAt.String
	ord.LastReviewRequestAt = lastReviewRequestAt.String
	ord.CreatedAt = createdAt.String
	ord.UpdatedAt = updatedAt.String
	return &ord, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func joinSet(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
