package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// orderAmountPattern 用于本次流程后续判断的订单AmountPattern
var orderAmountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// groupedOrderAmountPattern 用于本次流程后续判断的grouped订单AmountPattern
var groupedOrderAmountPattern = regexp.MustCompile(`^[0-9]{1,3}(?:,[0-9]{3})+(?:\.[0-9]+)?$`)

// ErrOrderConflict 用于本次流程后续判断的Err订单Conflict
var ErrOrderConflict = errors.New("订单被并发更新，请重试")

// maxOrderUpsertRetries 限制单次订单写入在乐观锁竞争下的总尝试次数，避免持续竞争无限占用请求协程。
const maxOrderUpsertRetries = 8

// maxOrderUpsertRetryDelay 限制同一订单竞争时的单次退避时长；上限保持写入恢复及时，同时打破无间隔重试造成的活锁。
const maxOrderUpsertRetryDelay = 16 * time.Millisecond

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

// sqlExecer 用于本次流程后续判断的sqlExecer
type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// sqlQueryExecer 用于本次流程后续判断的sql查询Execer
type sqlQueryExecer interface {
	sqlExecer
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Patch 按请求中实际出现的字段更新订单，允许显式清空字符串字段。
func (o *Orders) Patch(ctx context.Context, orderID string, patch OrderPatch) error {
	return patchOrder(ctx, o.DB, orderID, patch)
}

// PatchTx 封装PatchTx业务协调。
func (o *Orders) PatchTx(ctx context.Context, tx *sql.Tx, orderID string, patch OrderPatch) error {
	return patchOrder(ctx, tx, orderID, patch)
}

// patchOrder 封装patch订单业务协调。
func patchOrder(ctx context.Context, execer sqlExecer, orderID string, patch OrderPatch) error {
	if patch.Amount != nil {
		// normalized、ok 用于本次流程后续判断的normalized、ok
		normalized, ok := NormalizeOrderAmount(*patch.Amount)
		if !ok {
			return errors.New("订单金额必须是普通格式的非负有限数字")
		}
		patch.Amount = &normalized
	}
	// set 用于本次流程后续判断的set
	set := []string{}
	// args 用于本次流程后续判断的args
	args := []any{}
	// addString 用于本次流程后续判断的addString
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
	// err 用于本次流程后续判断的err
	_, err := execer.ExecContext(ctx, `UPDATE orders SET `+joinSet(set)+` WHERE order_id=?`, args...)
	return err
}

// Upsert 插入或更新订单。仅更新提供的非零字段（INSERT OR IGNORE 占位 + 动态 UPDATE）。
// version 参与乐观锁，保证 WebSocket、浏览器同步和 HTTP 并发更新不会绕过状态防倒退规则。
// Upsert 封装Upsert业务协调。
func (o *Orders) Upsert(ctx context.Context, orderID string, opts OrderUpsertOpts) error {
	return upsertOrder(ctx, o.DB, o.Dialect, orderID, opts)
}

// UpsertTx 在调用方事务内插入或更新订单。
func (o *Orders) UpsertTx(ctx context.Context, tx *sql.Tx, orderID string, opts OrderUpsertOpts) error {
	return upsertOrder(ctx, tx, o.Dialect, orderID, opts)
}

// BatchOrderUpsert 描述订单刷新批量 UPSERT 的一行数据。
type BatchOrderUpsert struct {
	// OrderID 是订单业务标识。
	OrderID string
	// Options 是订单可选字段集合；空字符串字段不会覆盖已有值。
	Options OrderUpsertOpts
}

// UpsertMany 使用最多两条多值 UPSERT 写入一批订单，并保证分组写入的原子性。
func (o *Orders) UpsertMany(ctx context.Context, rows []BatchOrderUpsert) error {
	if len(rows) == 0 {
		return nil
	}
	// tx、err 保存混合 CreatedAt 批次共用的事务及开启错误，避免第二组写入失败留下半批数据。
	tx, err := o.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// err 保存批量订单分组写入错误。
	if err := upsertManyOrders(ctx, tx, o.Dialect, rows); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertManyTx 在调用方事务内使用最多两条多值 UPSERT 写入一批订单。
func (o *Orders) UpsertManyTx(ctx context.Context, tx *sql.Tx, rows []BatchOrderUpsert) error {
	return upsertManyOrders(ctx, tx, o.Dialect, rows)
}

// upsertManyOrders 校验批量订单并按 CreatedAt 是否存在分组执行跨方言 UPSERT。
func upsertManyOrders(ctx context.Context, execer sqlQueryExecer, dialect Dialect, rows []BatchOrderUpsert) error {
	if len(rows) == 0 {
		return nil
	}
	// seen 保存本批次已经出现的订单标识，避免 PostgreSQL 同一语句重复冲突。
	seen := make(map[string]struct{}, len(rows))
	// normalizedRows 保存金额和状态已归一化的批量订单。
	normalizedRows := make([]BatchOrderUpsert, 0, len(rows))
	// cookieIDs 保存需要执行归属冲突检查的账号集合。
	cookieIDs := make(map[string][]string)
	// row 是当前规范化处理的批量订单。
	for _, row := range rows {
		if strings.TrimSpace(row.OrderID) == "" {
			return errors.New("order_id 不能为空")
		}
		// exists 表示当前订单标识是否已经出现在本批次。
		if _, exists := seen[row.OrderID]; exists {
			return fmt.Errorf("批量订单包含重复 order_id: %s", row.OrderID)
		}
		seen[row.OrderID] = struct{}{}
		row.Options.ItemID = strings.TrimSpace(row.Options.ItemID)
		if row.Options.Amount != "" {
			// normalized、ok 保存当前订单金额归一化结果。
			normalized, ok := NormalizeOrderAmount(row.Options.Amount)
			if !ok {
				return errors.New("订单金额必须是普通格式的非负有限数字")
			}
			row.Options.Amount = normalized
		}
		if row.Options.OrderStatus == "" {
			row.Options.OrderStatus = "unknown"
		}
		normalizedRows = append(normalizedRows, row)
		if row.Options.CookieID != "" {
			cookieIDs[row.Options.CookieID] = append(cookieIDs[row.Options.CookieID], row.OrderID)
		}
	}
	// cookieID、orderIDs 保存当前账号及其订单标识集合。
	for cookieID, orderIDs := range cookieIDs {
		// placeholders、args 保存当前账号归属冲突检查 SQL 参数。
		placeholders := make([]string, len(orderIDs))
		// args 保存归属冲突检查查询参数。
		args := make([]any, 0, len(orderIDs)+1)
		// index、orderID 保存当前账号订单的下标和业务标识。
		for index, orderID := range orderIDs {
			placeholders[index] = "?"
			args = append(args, orderID)
		}
		args = append(args, cookieID)
		// conflictCount、err 保存跨账号订单数量及查询错误。
		var conflictCount int
		// query 保存归属冲突检查 SQL。
		query := `SELECT COUNT(*) FROM orders WHERE order_id IN (` + strings.Join(placeholders, ",") + `) AND cookie_id IS NOT NULL AND cookie_id<>?`
		// err 保存归属冲突查询错误。
		if err := execer.QueryRowContext(ctx, query, args...).Scan(&conflictCount); err != nil {
			return err
		}
		if conflictCount > 0 {
			return ErrForbidden
		}
	}

	// rowsWithCreatedAt 和 rowsWithoutCreatedAt 分别保存有、无平台创建时间的订单组。
	rowsWithCreatedAt := make([]BatchOrderUpsert, 0, len(normalizedRows))
	// rowsWithoutCreatedAt 保存未提供平台创建时间的订单组。
	rowsWithoutCreatedAt := make([]BatchOrderUpsert, 0, len(normalizedRows))
	// row 表示当前待按创建时间分组的订单。
	for _, row := range normalizedRows {
		if row.Options.CreatedAt == "" {
			rowsWithoutCreatedAt = append(rowsWithoutCreatedAt, row)
			continue
		}
		rowsWithCreatedAt = append(rowsWithCreatedAt, row)
	}
	if len(rowsWithoutCreatedAt) > 0 {
		// err 保存无平台创建时间订单组的 UPSERT 错误。
		if err := upsertManyOrderGroup(ctx, execer, dialect, rowsWithoutCreatedAt, false); err != nil {
			return err
		}
	}
	if len(rowsWithCreatedAt) > 0 {
		// err 保存显式平台创建时间订单组的 UPSERT 错误。
		if err := upsertManyOrderGroup(ctx, execer, dialect, rowsWithCreatedAt, true); err != nil {
			return err
		}
	}
	return nil
}

// upsertManyOrderGroup 为同一 CreatedAt 语义的订单组构造多值 UPSERT。
func upsertManyOrderGroup(ctx context.Context, execer sqlQueryExecer, dialect Dialect, rows []BatchOrderUpsert, includeCreatedAt bool) error {
	// columns 保存多值 INSERT 的公共列集合；无平台时间时省略 created_at 以保留已有值和数据库默认值。
	columns := []string{"order_id", "item_id", "buyer_id", "cookie_id"}
	if includeCreatedAt {
		columns = append(columns, "created_at")
	}
	columns = append(columns, "order_status", "is_bargain", "spec_name", "spec_value", "quantity", "amount", "receiver_name", "receiver_phone", "receiver_address", "receiver_city", "version")
	// values 保存多行占位符和参数。
	values := make([]string, 0, len(rows))
	// args 保存批量插入参数。
	args := make([]any, 0, len(rows)*len(columns))
	// row 是当前待插入的批量订单。
	for _, row := range rows {
		// valueExpressions 保存当前批量行的 SQL 占位表达式。
		valueExpressions := make([]string, len(columns))
		// columnIndex 表示当前 SQL 占位表达式在订单列集合中的下标。
		for columnIndex := range valueExpressions {
			valueExpressions[columnIndex] = "?"
		}
		values = append(values, "("+strings.Join(valueExpressions, ",")+")")
		// isBargain 保存批量订单是否包含砍价标记。
		isBargain := 0
		if row.Options.IsBargain != nil && *row.Options.IsBargain {
			isBargain = 1
		}
		args = append(args, row.OrderID, row.Options.ItemID, row.Options.BuyerID, row.Options.CookieID)
		if includeCreatedAt {
			args = append(args, row.Options.CreatedAt)
		}
		args = append(args, row.Options.OrderStatus, isBargain, row.Options.SpecName, row.Options.SpecValue, row.Options.Quantity, row.Options.Amount, row.Options.ReceiverName, row.Options.ReceiverPhone, row.Options.ReceiverAddr, row.Options.ReceiverCity, 1)
	}
	// currentColumn 返回冲突更新时目标表已有列的表达式；PostgreSQL 需要表限定，其他方言保留原列名兼容语法。
	currentColumn := func(column string) string {
		if dialect == DialectPostgres {
			return "orders." + column
		}
		return column
	}
	// excludedValue 返回当前数据库方言读取插入候选值的表达式。
	excludedValue := func(column string) string {
		if dialect == DialectMySQL {
			return "VALUES(" + column + ")"
		}
		return "EXCLUDED." + column
	}
	// mergeValue 生成仅在候选值非空时覆盖旧值的表达式。
	mergeValue := func(column string) string {
		// incoming 保存当前列候选值表达式。
		incoming := excludedValue(column)
		// current 保存冲突更新时目标表当前列的限定表达式，供候选值为空时保留原值。
		current := currentColumn(column)
		return "CASE WHEN " + incoming + " IS NOT NULL AND " + incoming + "<>'' THEN " + incoming + " ELSE " + current + " END"
	}
	// incomingStatus 保存候选订单状态表达式。
	incomingStatus := excludedValue("order_status")
	// currentStatus 保存冲突更新时目标表已有订单状态；PostgreSQL 需要使用 orders.order_status 消除列名歧义。
	currentStatus := currentColumn("order_status")
	// statusAssignment 保存防止状态倒退的跨方言状态表达式。
	statusAssignment := "CASE WHEN " + incomingStatus + " IS NULL OR " + incomingStatus + "='' OR (" + incomingStatus + "='unknown' AND " + currentStatus + "<>'unknown') THEN " + currentStatus + " WHEN " + currentStatus + "='unknown' OR " + currentStatus + "=" + incomingStatus + " THEN " + incomingStatus + " WHEN " + incomingStatus + " IN ('processing','pending_ship') AND " + currentStatus + " IN ('shipped','completed','refunding','cancelled') THEN " + currentStatus + " WHEN " + incomingStatus + "='shipped' AND " + currentStatus + " IN ('completed','cancelled') THEN " + currentStatus + " ELSE " + incomingStatus + " END"
	// incomingBargain 保存候选订单砍价标记表达式。
	incomingBargain := excludedValue("is_bargain")
	// assignments 保存批量 UPSERT 的更新列表达式。
	assignments := map[string]string{
		"item_id":          mergeValue("item_id"),
		"buyer_id":         mergeValue("buyer_id"),
		"cookie_id":        mergeValue("cookie_id"),
		"order_status":     statusAssignment,
		"is_bargain":       "CASE WHEN " + incomingBargain + "=1 THEN 1 ELSE " + currentColumn("is_bargain") + " END",
		"spec_name":        mergeValue("spec_name"),
		"spec_value":       mergeValue("spec_value"),
		"quantity":         mergeValue("quantity"),
		"amount":           mergeValue("amount"),
		"receiver_name":    mergeValue("receiver_name"),
		"receiver_phone":   mergeValue("receiver_phone"),
		"receiver_address": mergeValue("receiver_address"),
		"receiver_city":    mergeValue("receiver_city"),
		"deleted_at":       "NULL",
		"version":          currentColumn("version") + "+1",
		"updated_at":       "CURRENT_TIMESTAMP",
	}
	if includeCreatedAt {
		// created_at 仅在平台提供时间的批次中参与更新，避免详情补全覆盖原有创建时间。
		// incomingCreatedAt 保存显式时间批次中的候选创建时间表达式；时间类型不能与空字符串比较。
		incomingCreatedAt := excludedValue("created_at")
		assignments["created_at"] = "CASE WHEN " + incomingCreatedAt + " IS NOT NULL THEN " + incomingCreatedAt + " ELSE " + currentColumn("created_at") + " END"
	}
	// query 保存多值 UPSERT SQL。
	query := "INSERT INTO orders (" + strings.Join(columns, ",") + ") VALUES " + strings.Join(values, ",") + dialectUpsert(dialect, []string{"order_id"}, assignments)
	// err 保存多值 UPSERT 执行错误。
	_, err := execer.ExecContext(ctx, query, args...)
	return err
}

// SoftDelete 将订单标记为逻辑删除，保留历史数据供审计和后续恢复。
func (o *Orders) SoftDelete(ctx context.Context, orderID string) (bool, error) {
	// result、err 用于本次流程后续判断的result、err
	result, err := o.DB.ExecContext(ctx,
		`UPDATE orders SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		  WHERE order_id=? AND deleted_at IS NULL`, orderID)
	if err != nil {
		return false, err
	}
	// changed、err 用于本次流程后续判断的changed、err
	changed, err := result.RowsAffected()
	return changed > 0, err
}

// upsertOrder 写入一条订单并以版本列防止并发更新覆盖状态推进。
// ctx 控制数据库操作及竞争退避；同一版本竞争会在有界、可取消的等待后重读并重试，超出次数返回 ErrOrderConflict。
func upsertOrder(ctx context.Context, execer sqlQueryExecer, dialect Dialect, orderID string, opts OrderUpsertOpts) error {
	if orderID == "" {
		return errors.New("order_id 不能为空")
	}
	opts.ItemID = strings.TrimSpace(opts.ItemID)
	if opts.Amount != "" {
		// normalized、ok 用于本次流程后续判断的normalized、ok
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

	for // attempt 用于本次流程后续判断的尝试次数
	attempt := 0; attempt < maxOrderUpsertRetries; attempt++ {
		// existingCookie、existingStatus、deletedAt 用于本次流程后续判断的existingCookie、existingStatus、deletedAt
		var existingCookie, existingStatus, deletedAt sql.NullString
		// version 用于本次流程后续判断的version
		var version int
		if // err 用于本次流程后续判断的err
		err := execer.QueryRowContext(ctx,
			`SELECT cookie_id,order_status,version,deleted_at FROM orders WHERE order_id=?`, orderID).
			Scan(&existingCookie, &existingStatus, &version, &deletedAt); err != nil {
			return err
		}
		if opts.CookieID != "" && existingCookie.Valid && existingCookie.String != "" && existingCookie.String != opts.CookieID {
			return ErrForbidden
		}

		// current 用于本次流程后续判断的current
		current := opts
		if current.OrderStatus != "" && !shouldUpdateOrderStatus(existingStatus.String, current.OrderStatus) {
			current.OrderStatus = ""
		}
		// set、args 用于本次流程后续判断的set、args
		set, args := orderUpsertAssignments(current)
		if deletedAt.Valid && deletedAt.String != "" {
			set = append(set, "deleted_at=NULL")
		}
		if len(set) == 0 {
			return nil
		}
		set = append(set, "version=version+1", "updated_at=CURRENT_TIMESTAMP")
		args = append(args, orderID, version)
		// res、err 用于本次流程后续判断的res、err
		res, err := execer.ExecContext(ctx,
			`UPDATE orders SET `+joinSet(set)+` WHERE order_id=? AND version=?`, args...)
		if err != nil {
			return err
		}
		// n、rowsErr 用于本次流程后续判断的n、rowsErr
		n, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if n == 1 {
			return nil
		}
		if attempt+1 < maxOrderUpsertRetries {
			// retryErr 保存等待下一次乐观锁读取期间的取消错误；调用方取消后不得继续占用数据库连接。
			retryErr := waitOrderUpsertRetry(ctx, attempt)
			if retryErr != nil {
				return retryErr
			}
		}
	}
	return ErrOrderConflict
}

// waitOrderUpsertRetry 在订单版本比较失败后执行有上限的指数退避。
// ctx 由 Upsert 调用方拥有；返回其取消或截止错误，成功返回 nil 供调用方重新读取最新版本。
func waitOrderUpsertRetry(ctx context.Context, attempt int) error {
	// delay 保存本次冲突后的等待时间，避免两个写入协程立即重复读取同一旧版本。
	delay := orderUpsertRetryDelay(attempt)
	// timer 负责在退避到期后唤醒当前调用；函数返回前停止计时器以释放运行时资源。
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// orderUpsertRetryDelay 根据已经失败的次数计算下一次订单乐观锁重试的等待时间。
// attempt 小于零按首次失败处理；返回值不超过 maxOrderUpsertRetryDelay，避免高竞争时产生不可接受的请求延迟。
func orderUpsertRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	// delay 保存以 1ms 为起点的指数退避时间；超过上限时统一使用上限，防止位移计算放大等待。
	delay := time.Millisecond << min(attempt, 4)
	if delay > maxOrderUpsertRetryDelay {
		return maxOrderUpsertRetryDelay
	}
	return delay
}

// orderUpsertAssignments 封装订单UpsertAssignments业务协调。
func orderUpsertAssignments(opts OrderUpsertOpts) ([]string, []any) {
	// set 用于本次流程后续判断的set
	set := []string{}
	// args 用于本次流程后续判断的args
	args := []any{}
	// add 用于本次流程后续判断的add
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
	add("created_at", opts.CreatedAt, opts.CreatedAt != "")
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
	// value、err 用于本次流程后续判断的value、err
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", false
	}
	return raw, true
}

// shouldUpdateOrderStatus 防止延迟或重复的“已付款”事件把已发货/已完成订单回退。
// 退款、取消等分支并非线性流程，因此这里只拦截明确的历史阶段倒退。
// shouldUpdateOrderStatus 封装shouldUpdate订单状态业务协调。
func shouldUpdateOrderStatus(current, incoming string) bool {
	current = NormalizeOrderStatus(current)
	incoming = NormalizeOrderStatus(incoming)
	if current == incoming || current == "unknown" {
		return true
	}
	// early 用于本次流程后续判断的early
	early := incoming == "processing" || incoming == "paid" || incoming == "pending_ship"
	// advanced 用于本次流程后续判断的advanced
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
	ItemID   string
	BuyerID  string
	CookieID string
	// CreatedAt 是平台订单创建时间；为空时不覆盖数据库已有时间。
	CreatedAt     string
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

// maxOrderBatchLookupSize 限制批量订单查询的 IN 参数数量，兼容 SQLite 参数上限。
const maxOrderBatchLookupSize = 500

// FindByIDs 按订单标识批量读取未删除订单，避免订单发现阶段逐单查询。
func (o *Orders) FindByIDs(ctx context.Context, orderIDs []string) (map[string]*Order, error) {
	// result 保存按订单标识索引的本地订单。
	result := make(map[string]*Order, len(orderIDs))
	// normalizedIDs 保存去重后的非空订单标识。
	normalizedIDs := make([]string, 0, len(orderIDs))
	// seen 保存已经加入查询的订单标识。
	seen := make(map[string]struct{}, len(orderIDs))
	// orderID 是当前待规范化的订单标识。
	for _, orderID := range orderIDs {
		orderID = strings.TrimSpace(orderID)
		if orderID == "" {
			continue
		}
		// exists 表示当前订单标识是否已经加入批量查询。
		if _, exists := seen[orderID]; exists {
			continue
		}
		seen[orderID] = struct{}{}
		normalizedIDs = append(normalizedIDs, orderID)
	}
	// start 是当前批量查询的起始下标。
	for start := 0; start < len(normalizedIDs); start += maxOrderBatchLookupSize {
		// end 保存当前批量查询的结束下标。
		end := start + maxOrderBatchLookupSize
		if end > len(normalizedIDs) {
			end = len(normalizedIDs)
		}
		// batchIDs 保存当前批量查询的订单标识。
		batchIDs := normalizedIDs[start:end]
		// placeholders 保存当前查询的占位符。
		placeholders := make([]string, len(batchIDs))
		// args 保存当前查询的订单标识参数。
		args := make([]any, len(batchIDs))
		// index、orderID 保存当前查询参数的下标和订单标识。
		for index, orderID := range batchIDs {
			placeholders[index] = "?"
			args[index] = orderID
		}
		// query 保存当前批量订单查询 SQL。
		query := `SELECT order_id,item_id,buyer_id,quantity,amount,order_status,cookie_id,is_bargain,
		        receiver_name,receiver_phone,receiver_address,receiver_city
		 FROM orders WHERE order_id IN (` + strings.Join(placeholders, ",") + `) AND deleted_at IS NULL`
		// rows 保存当前批量订单查询结果集。
		rows, err := o.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		// rowOrder 保存当前结果集扫描出的订单。
		for rows.Next() {
			// orderIDValue、itemID、buyerID、quantity、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity 保存可空订单文本字段。
			var orderIDValue, itemID, buyerID, quantity, amount, status, cookieID, receiverName, receiverPhone, receiverAddr, receiverCity sql.NullString
			// isBargain 保存当前订单砍价标记。
			var isBargain sql.NullInt64
			// scanErr 保存当前订单行扫描错误。
			if scanErr := rows.Scan(&orderIDValue, &itemID, &buyerID, &quantity, &amount, &status, &cookieID, &isBargain, &receiverName, &receiverPhone, &receiverAddr, &receiverCity); scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			// rowOrder 保存当前结果集扫描出的订单。
			rowOrder := &Order{OrderID: orderIDValue.String, ItemID: itemID.String, BuyerID: buyerID.String, Quantity: quantity.String, Amount: amount.String, OrderStatus: status.String, CookieID: cookieID.String, IsBargain: int(isBargain.Int64), ReceiverName: receiverName.String, ReceiverPhone: receiverPhone.String, ReceiverAddr: receiverAddr.String, ReceiverCity: receiverCity.String}
			result[rowOrder.OrderID] = rowOrder
		}
		// rowsErr 保存当前批量订单结果集遍历错误。
		rowsErr := rows.Err()
		_ = rows.Close()
		if rowsErr != nil {
			return nil, rowsErr
		}
	}
	return result, nil
}

// Get 按 order_id 查询。
func (o *Orders) Get(ctx context.Context, orderID string) (*Order, error) {
	// ord 用于本次流程后续判断的ord
	var ord Order
	// isBargain、version、sysShipped 用于本次流程后续判断的isBargain、version、sysShipped
	var isBargain, version, sysShipped int
	// itemID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID、paidAt、shippedAt、completedAt、buyerReviewedAt、lastReviewRequestAt、createdAt、updatedAt 保存商品ID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID、paidAt、shippedAt、completedAt、buyerReviewedAt、lastReview请求At、createdAt、updatedAt，供当前处理流程使用
	var itemID, buyerID, specName, specValue, qty, amount, status, cookieID, receiverName, receiverPhone, receiverAddr, receiverCity, chatID, paidAt, shippedAt, completedAt, buyerReviewedAt, lastReviewRequestAt, createdAt, updatedAt sql.NullString
	// err 用于本次流程后续判断的err
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

// boolToInt 封装boolToInt业务协调。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// joinSet 封装joinSet业务协调。
func joinSet(parts []string) string {
	// out 用于本次流程后续判断的out
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
