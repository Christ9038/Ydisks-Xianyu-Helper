package db

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// orderAmountPattern 保存订单AmountPattern，供当前处理流程使用
var orderAmountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// groupedOrderAmountPattern 保存grouped订单AmountPattern，供当前处理流程使用
var groupedOrderAmountPattern = regexp.MustCompile(`^[0-9]{1,3}(?:,[0-9]{3})+(?:\.[0-9]+)?$`)

// ErrOrderConflict 保存Err订单Conflict，供当前处理流程使用
var ErrOrderConflict = errors.New("订单被并发更新，请重试")

// maxOrderUpsertRetries 保存max订单UpsertRetries，供当前处理流程使用
const maxOrderUpsertRetries = 5

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

// OrderPatch 区分“字段未提供”(nil)与“显式清空”(指向空字符串)。
type OrderPatch struct {
	OrderStatus, ItemID, BuyerID, SpecName, SpecValue *string
	Quantity, Amount, ReceiverName, ReceiverPhone     *string
	ReceiverAddr, ReceiverCity, ChatID                *string
	SystemShipped                                     *bool
}

// sqlExecer 保存sqlExecer，供当前处理流程使用
type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// sqlQueryExecer 保存sql查询Execer，供当前处理流程使用
type sqlQueryExecer interface {
	sqlExecer
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Patch 按请求中实际出现的字段更新订单，允许显式清空字符串字段。
func (o *Orders) Patch(ctx context.Context, orderID string, patch OrderPatch) error {
	return patchOrder(ctx, o.DB, orderID, patch)
}

// PatchTx 负责PatchTx相关处理。
func (o *Orders) PatchTx(ctx context.Context, tx *sql.Tx, orderID string, patch OrderPatch) error {
	return patchOrder(ctx, tx, orderID, patch)
}

// patchOrder 负责patch订单相关处理。
func patchOrder(ctx context.Context, execer sqlExecer, orderID string, patch OrderPatch) error {
	if patch.Amount != nil {
		// normalized、ok 保存normalized、ok，供当前处理流程使用
		normalized, ok := NormalizeOrderAmount(*patch.Amount)
		if !ok {
			return errors.New("订单金额必须是普通格式的非负有限数字")
		}
		patch.Amount = &normalized
	}
	// set 保存set，供当前处理流程使用
	set := []string{}
	// args 保存args，供当前处理流程使用
	args := []any{}
	// addString 保存addString，供当前处理流程使用
	addString := func(column string, value *string) {
		if value != nil {
			set = append(set, column+"=?")
			args = append(args, *value)
		}
	}
	addString("order_status", patch.OrderStatus)
	addString("item_id", patch.ItemID)
	addString("buyer_id", patch.BuyerID)
	addString("spec_name", patch.SpecName)
	addString("spec_value", patch.SpecValue)
	addString("quantity", patch.Quantity)
	addString("amount", patch.Amount)
	addString("receiver_name", patch.ReceiverName)
	addString("receiver_phone", patch.ReceiverPhone)
	addString("receiver_address", patch.ReceiverAddr)
	addString("receiver_city", patch.ReceiverCity)
	addString("chat_id", patch.ChatID)
	if patch.SystemShipped != nil {
		set = append(set, "system_shipped=?")
		args = append(args, boolToInt(*patch.SystemShipped))
	}
	if len(set) == 0 {
		return nil
	}
	set = append(set, "updated_at=CURRENT_TIMESTAMP")
	args = append(args, orderID)
	// err 保存err，供当前处理流程使用
	_, err := execer.ExecContext(ctx, `UPDATE orders SET `+joinSet(set)+` WHERE order_id=?`, args...)
	return err
}

// Upsert 插入或更新订单。仅更新提供的非零字段（INSERT OR IGNORE 占位 + 动态 UPDATE）。
// version 参与乐观锁，保证 WebSocket、浏览器同步和 HTTP 并发更新不会绕过状态防倒退规则。
// Upsert 负责Upsert相关处理。
func (o *Orders) Upsert(ctx context.Context, orderID string, opts OrderUpsertOpts) error {
	return upsertOrder(ctx, o.DB, o.Dialect, orderID, opts)
}

// UpsertTx 在调用方事务内插入或更新订单。
func (o *Orders) UpsertTx(ctx context.Context, tx *sql.Tx, orderID string, opts OrderUpsertOpts) error {
	return upsertOrder(ctx, tx, o.Dialect, orderID, opts)
}

// SoftDelete 将订单标记为逻辑删除，保留历史数据供审计和后续恢复。
func (o *Orders) SoftDelete(ctx context.Context, orderID string) (bool, error) {
	// result、err 保存result、err，供当前处理流程使用
	result, err := o.DB.ExecContext(ctx,
		`UPDATE orders SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		  WHERE order_id=? AND deleted_at IS NULL`, orderID)
	if err != nil {
		return false, err
	}
	// changed、err 保存changed、err，供当前处理流程使用
	changed, err := result.RowsAffected()
	return changed > 0, err
}

// upsertOrder 负责upsert订单相关处理。
func upsertOrder(ctx context.Context, execer sqlQueryExecer, dialect Dialect, orderID string, opts OrderUpsertOpts) error {
	if orderID == "" {
		return errors.New("order_id 不能为空")
	}
	opts.ItemID = strings.TrimSpace(opts.ItemID)
	if opts.Amount != "" {
		// normalized、ok 保存normalized、ok，供当前处理流程使用
		normalized, ok := NormalizeOrderAmount(opts.Amount)
		if !ok {
			return errors.New("订单金额必须是普通格式的非负有限数字")
		}
		opts.Amount = normalized
	}
	// 先尝试插入占位（冲突忽略）。order_id 是主键。
	_, err := execer.ExecContext(ctx,
		dialectInsertIgnorePrefix(dialect)+` INTO orders (order_id, item_id, buyer_id, cookie_id, order_status, version)
		 VALUES (?, ?, ?, ?, 'unknown', 1)`+dialectInsertIgnore(dialect, []string{"order_id"}),
		orderID, opts.ItemID, opts.BuyerID, opts.CookieID)
	if err != nil {
		return err
	}

	for // attempt 保存尝试次数，供当前处理流程使用
	attempt := 0; attempt < maxOrderUpsertRetries; attempt++ {
		// existingCookie、existingStatus、deletedAt 保存existingCookie、existingStatus、deletedAt，供当前处理流程使用
		var existingCookie, existingStatus, deletedAt sql.NullString
		// version 保存version，供当前处理流程使用
		var version int
		if // err 保存err，供当前处理流程使用
		err := execer.QueryRowContext(ctx,
			`SELECT cookie_id,order_status,version,deleted_at FROM orders WHERE order_id=?`, orderID).
			Scan(&existingCookie, &existingStatus, &version, &deletedAt); err != nil {
			return err
		}
		if opts.CookieID != "" && existingCookie.Valid && existingCookie.String != "" && existingCookie.String != opts.CookieID {
			return ErrForbidden
		}

		// current 保存current，供当前处理流程使用
		current := opts
		if current.OrderStatus != "" && !shouldUpdateOrderStatus(existingStatus.String, current.OrderStatus) {
			current.OrderStatus = ""
		}
		// set、args 保存set、args，供当前处理流程使用
		set, args := orderUpsertAssignments(current)
		if deletedAt.Valid && deletedAt.String != "" {
			set = append(set, "deleted_at=NULL")
		}
		if len(set) == 0 {
			return nil
		}
		set = append(set, "version=version+1", "updated_at=CURRENT_TIMESTAMP")
		args = append(args, orderID, version)
		// res、err 保存res、err，供当前处理流程使用
		res, err := execer.ExecContext(ctx,
			`UPDATE orders SET `+joinSet(set)+` WHERE order_id=? AND version=?`, args...)
		if err != nil {
			return err
		}
		// n、rowsErr 保存n、rowsErr，供当前处理流程使用
		n, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if n == 1 {
			return nil
		}
	}
	return ErrOrderConflict
}

// orderUpsertAssignments 负责订单UpsertAssignments相关处理。
func orderUpsertAssignments(opts OrderUpsertOpts) ([]string, []any) {
	// set 保存set，供当前处理流程使用
	set := []string{}
	// args 保存args，供当前处理流程使用
	args := []any{}
	// add 保存add，供当前处理流程使用
	add := func(column string, value any, present bool) {
		if present {
			set = append(set, column+"=?")
			args = append(args, value)
		}
	}
	if opts.SystemShipped != nil {
		add("system_shipped", boolToInt(*opts.SystemShipped), true)
	}
	if opts.IsBargain != nil {
		add("is_bargain", boolToInt(*opts.IsBargain), true)
	}
	add("chat_id", opts.ChatID, opts.ChatID != "")
	add("item_id", opts.ItemID, opts.ItemID != "")
	add("buyer_id", opts.BuyerID, opts.BuyerID != "")
	add("cookie_id", opts.CookieID, opts.CookieID != "")
	add("order_status", opts.OrderStatus, opts.OrderStatus != "")
	add("spec_name", opts.SpecName, opts.SpecName != "")
	add("spec_value", opts.SpecValue, opts.SpecValue != "")
	add("quantity", opts.Quantity, opts.Quantity != "")
	add("amount", opts.Amount, opts.Amount != "")
	add("receiver_name", opts.ReceiverName, opts.ReceiverName != "")
	add("receiver_phone", opts.ReceiverPhone, opts.ReceiverPhone != "")
	add("receiver_address", opts.ReceiverAddr, opts.ReceiverAddr != "")
	add("receiver_city", opts.ReceiverCity, opts.ReceiverCity != "")
	return set, args
}

// NormalizeOrderAmount 把货币符号和千位分隔符规范为数据库及统计接口共同使用的十进制格式。
func NormalizeOrderAmount(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "¥") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "¥"))
	} else if strings.HasPrefix(raw, "￥") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "￥"))
	}
	if raw == "" {
		return "", true
	}
	if strings.Contains(raw, ",") {
		if !groupedOrderAmountPattern.MatchString(raw) {
			return "", false
		}
		raw = strings.ReplaceAll(raw, ",", "")
	} else if !orderAmountPattern.MatchString(raw) {
		return "", false
	}
	// value、err 保存value、err，供当前处理流程使用
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", false
	}
	return raw, true
}

// shouldUpdateOrderStatus 防止延迟或重复的“已付款”事件把已发货/已完成订单回退。
// 退款、取消等分支并非线性流程，因此这里只拦截明确的历史阶段倒退。
// shouldUpdateOrderStatus 负责shouldUpdate订单状态相关处理。
func shouldUpdateOrderStatus(current, incoming string) bool {
	current = NormalizeOrderStatus(current)
	incoming = NormalizeOrderStatus(incoming)
	if current == incoming || current == "unknown" {
		return true
	}
	// early 保存early，供当前处理流程使用
	early := incoming == "processing" || incoming == "paid" || incoming == "pending_ship"
	// advanced 保存advanced，供当前处理流程使用
	advanced := current == "shipped" || current == "completed" || current == "refunding" || current == "cancelled"
	if early && advanced {
		return false
	}
	if incoming == "shipped" && (current == "completed" || current == "cancelled") {
		return false
	}
	return true
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
	IsBargain     *bool
	SystemShipped *bool
}

// Get 按 order_id 查询。
func (o *Orders) Get(ctx context.Context, orderID string) (*Order, error) {
	// ord 保存ord，供当前处理流程使用
	var ord Order
	// isBargain、version、sysShipped 保存isBargain、version、sysShipped，供当前处理流程使用
	var isBargain, version, sysShipped int
	// itemID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID、paidAt、shippedAt、completedAt、buyerReviewedAt、lastReviewRequestAt、createdAt、updatedAt 保存商品ID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID、paidAt、shippedAt、completedAt、buyerReviewedAt、lastReview请求At、createdAt、updatedAt，供当前处理流程使用
	var itemID, buyerID, specName, specValue, qty, amount, status, cookieID, receiverName, receiverPhone, receiverAddr, receiverCity, chatID, paidAt, shippedAt, completedAt, buyerReviewedAt, lastReviewRequestAt, createdAt, updatedAt sql.NullString
	// err 保存err，供当前处理流程使用
	err := o.DB.QueryRowContext(ctx,
		`SELECT order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount,
		        order_status, cookie_id, is_bargain, receiver_name, receiver_phone, receiver_address,
		        receiver_city, version, chat_id, system_shipped,
		        COALESCE(paid_at,''),COALESCE(shipped_at,''),COALESCE(completed_at,''),
		        COALESCE(buyer_reviewed_at,''),COALESCE(last_review_request_at,''),review_request_count,
		        created_at, updated_at
		 FROM orders WHERE order_id=? AND deleted_at IS NULL`, orderID).Scan(
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

// boolToInt 负责boolToInt相关处理。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// joinSet 负责joinSet相关处理。
func joinSet(parts []string) string {
	// out 保存out，供当前处理流程使用
	out := ""
	// i、p 表示当前遍历过程中的i、p
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
