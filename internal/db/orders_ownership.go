package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// ErrOrderRecoveryUnsafe 表示订单存在外部动作或无法安全解释的任务痕迹，必须人工复核，不能自动迁移。
var ErrOrderRecoveryUnsafe = errors.New("订单存在执行痕迹，无法安全恢复归属")

// OrderOwnership 是已授权用户可读取的订单最小快照，包含软删除行，不包含账号凭证或收货信息。
type OrderOwnership struct {
	// OrderID 是平台订单标识；未经授权时不返回旧订单身份。
	OrderID string
	// CookieID 是旧本地账号标识，并非通用的平台 UID 证明。
	CookieID string
	// ItemID 是核对远端已售事实的商品标识。
	ItemID string
	// BuyerID 是旧订单买家标识，仅在多证据授权修复中与账号标识保守比较。
	BuyerID string
	// Version 是恢复事务必须匹配的旧乐观锁版本。
	Version int
	// Deleted 表示订单已软删除，普通列表仍保持不可见。
	Deleted bool
	// Owned 表示旧账号属于查询用户；false 时其他字段均为零值。
	Owned bool
}

// FindOwnership 在 o 中按 orderID 读取包括软删除行的最小快照；ctx 控制取消，userID 决定身份可见性。
// 不存在返回 ErrNotFound，越权返回零值且无错误；SQL 不读取或解密账号秘密及其他用户的旧身份。
func (o *Orders) FindOwnership(ctx context.Context, userID int64, orderID string) (OrderOwnership, error) {
	// result 保存数据库按用户权限投影后的非敏感快照。
	var result OrderOwnership
	// err 保存单行查询或扫描错误，缺失行转换为仓储统一错误。
	err := o.DB.QueryRowContext(ctx, `SELECT
		CASE WHEN c.user_id=? THEN o.order_id ELSE '' END,
		CASE WHEN c.user_id=? THEN COALESCE(o.cookie_id,'') ELSE '' END,
		CASE WHEN c.user_id=? THEN COALESCE(o.item_id,'') ELSE '' END,
		CASE WHEN c.user_id=? THEN COALESCE(o.buyer_id,'') ELSE '' END,
		CASE WHEN c.user_id=? THEN o.version ELSE 0 END,
		CASE WHEN c.user_id=? AND o.deleted_at IS NOT NULL THEN 1 ELSE 0 END,
		CASE WHEN c.user_id=? THEN 1 ELSE 0 END
		FROM orders o LEFT JOIN cookies c ON c.id=o.cookie_id WHERE o.order_id=?`,
		userID, userID, userID, userID, userID, userID, userID, orderID).
		Scan(&result.OrderID, &result.CookieID, &result.ItemID, &result.BuyerID, &result.Version, &result.Deleted, &result.Owned)
	if errors.Is(err, sql.ErrNoRows) {
		return OrderOwnership{}, ErrNotFound
	}
	return result, err
}

// orderOwnershipAudit 只保存恢复前的非秘密订单上下文，不包含收货信息、任务原文、发货卡密或账号凭证。
type orderOwnershipAudit struct {
	// Ownership 保存已核对的旧归属、版本与删除状态。
	Ownership OrderOwnership `json:"ownership"`
	// Status 保存合并前的订单阶段，以解释状态防倒退结果。
	Status string `json:"order_status"`
	// ChatID 保存被清除的买家侧会话标识，不包含聊天正文。
	ChatID string `json:"chat_id"`
	// PaidAt 保存被清除的买家事件付款时间。
	PaidAt string `json:"paid_at"`
	// ShippedAt 保存被清除的买家事件发货时间，不作为发货凭证。
	ShippedAt string `json:"shipped_at"`
	// CompletedAt 保存被清除的买家事件完成时间。
	CompletedAt string `json:"completed_at"`
	// BuyerReviewedAt 保存被清除的买家事件评价时间。
	BuyerReviewedAt string `json:"buyer_reviewed_at"`
	// DeletedAt 保存恢复前软删除时间，便于还原恢复前的可见性。
	DeletedAt string `json:"deleted_at"`
	// Amount 保存被已售金额合并前的普通十进制金额，不含支付账户信息。
	Amount string `json:"amount"`
	// Quantity 保存合并前的购买数量文本。
	Quantity string `json:"quantity"`
	// SpecName 保存合并前的商品规格维度。
	SpecName string `json:"spec_name"`
	// SpecValue 保存合并前的商品规格值，不包含交付卡密。
	SpecValue string `json:"spec_value"`
	// IsBargain 保存合并前砍价标记。
	IsBargain int `json:"is_bargain"`
	// CreatedAt 保存可能被卖家平台创建时间替换的旧记录时间。
	CreatedAt string `json:"created_at"`
	// UpdatedAt 保存修正前的最后更新时间。
	UpdatedAt string `json:"updated_at"`
}

// RecoverSoldOwnership 在 o 中原子修复已获应用层真实卖家响应授权的订单；不执行任何网络或外部动作。
// ctx 控制整个事务；userID 必须同时拥有旧和新 cookieID；expected 必须仍匹配，options 是已验证的卖家事实。
// 应用层另行验证卖家 UID、会话代次及远端买家/商品；ID 相等仅是此窄修复的保守条件，不能证明别名身份。
// 事务先锁定两个账号，再以旧归属/版本占有订单行，随后检查副作用、保存审计并合并事实；任一错误全部回滚。
func (o *Orders) RecoverSoldOwnership(ctx context.Context, userID int64, cookieID string, expected OrderOwnership, options OrderUpsertOpts) error {
	if !expected.Owned || userID <= 0 {
		return ErrForbidden
	}
	if expected.OrderID == "" || expected.CookieID == "" || expected.ItemID == "" || expected.BuyerID == "" ||
		expected.Version <= 0 || strings.TrimSpace(cookieID) == "" || cookieID == expected.CookieID ||
		expected.BuyerID != expected.CookieID || options.BuyerID != expected.BuyerID ||
		strings.TrimSpace(options.ItemID) != expected.ItemID || (options.CookieID != "" && options.CookieID != cookieID) {
		return ErrOrderConflict
	}
	if options.ChatID != "" || (options.SystemShipped != nil && *options.SystemShipped) {
		return ErrOrderRecoveryUnsafe
	}
	options.CookieID = cookieID
	options.ChatID = ""
	options.SystemShipped = nil
	// transaction、err 保存串行化事务和开启失败；事务内只进行数据库 I/O。
	transaction, err := o.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	// err 保存两个账号归属复核及行锁错误；账号按稳定顺序加锁。
	if err := o.lockRecoveryAccounts(ctx, transaction, userID, expected.CookieID, cookieID); err != nil {
		return err
	}
	// result、err 保存旧 cookie、版本及事实完全匹配时的写锁占有结果，防止慢核验覆盖新归属。
	result, err := transaction.ExecContext(ctx, `UPDATE orders SET version=version+1
		WHERE order_id=? AND cookie_id=? AND version=? AND item_id=? AND buyer_id=?
		AND (CASE WHEN deleted_at IS NOT NULL THEN 1 ELSE 0 END)=?`,
		expected.OrderID, expected.CookieID, expected.Version, expected.ItemID, expected.BuyerID, boolToInt(expected.Deleted))
	if err != nil {
		return err
	}
	// affected、err 保存 CAS 行数；零行表示旧快照已失效，不能自动重试旧证据。
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrOrderConflict
	}
	// audit 保存行锁保护下的旧账户上下文，恢复失败时不会持久化。
	audit := orderOwnershipAudit{Ownership: expected}
	// shipped、reviewCount、reviewAt 保存必须阻止迁移的发送状态，绝不清空它们来绕过幂等保护。
	var shipped, reviewCount int
	// reviewAt 保存已有催评发送时间，即使计数缺失也拒绝恢复。
	var reviewAt string
	// deletedAt 接收三方言可空的软删除时间，避免活跃订单扫描 NULL 失败。
	var deletedAt sql.NullString
	// createdAt、updatedAt 接收三方言的可空时间，使用原驱动表示保存旧审计语义。
	var createdAt, updatedAt sql.NullString
	// err 保存锁定订单的旧上下文读取错误，不允许缺失字段后继续迁移。
	if err := transaction.QueryRowContext(ctx, `SELECT COALESCE(order_status,''),COALESCE(chat_id,''),
		COALESCE(paid_at,''),COALESCE(shipped_at,''),COALESCE(completed_at,''),COALESCE(buyer_reviewed_at,''),
		deleted_at,COALESCE(system_shipped,0),review_request_count,COALESCE(last_review_request_at,''),
		COALESCE(amount,''),COALESCE(quantity,''),COALESCE(spec_name,''),COALESCE(spec_value,''),COALESCE(is_bargain,0),created_at,updated_at
		FROM orders WHERE order_id=?`, expected.OrderID).Scan(&audit.Status, &audit.ChatID, &audit.PaidAt, &audit.ShippedAt,
		&audit.CompletedAt, &audit.BuyerReviewedAt, &deletedAt, &shipped, &reviewCount, &reviewAt,
		&audit.Amount, &audit.Quantity, &audit.SpecName, &audit.SpecValue, &audit.IsBargain, &createdAt, &updatedAt); err != nil {
		return err
	}
	audit.DeletedAt = deletedAt.String
	audit.CreatedAt, audit.UpdatedAt = createdAt.String, updatedAt.String
	if shipped != 0 || reviewCount != 0 || reviewAt != "" {
		return ErrOrderRecoveryUnsafe
	}
	// err 保存履约痕迹检查结果，业务阻断或数据库错误均触发回滚。
	if err := o.checkRecoverySideEffects(ctx, transaction, expected.OrderID, expected.CookieID, cookieID); err != nil {
		return err
	}
	// oldFields、err 序列化具名审计模型；此模型没有账号秘密或任务载荷字段。
	oldFields, err := json.Marshal(audit)
	if err != nil {
		return err
	}
	// err 保存审计写入失败；必须在事务内和订单修正一起成功。
	if _, err := transaction.ExecContext(ctx, `INSERT INTO order_ownership_repairs
		(order_id,user_id,old_cookie_id,new_cookie_id,old_version,old_fields_json) VALUES(?,?,?,?,?,?)`,
		expected.OrderID, userID, expected.CookieID, cookieID, expected.Version, string(oldFields)); err != nil {
		return err
	}
	// 清理账户相关字段并切换归属后，通用 Upsert 只在同事务内合并卖家订单事实并恢复软删除。
	if _, err := transaction.ExecContext(ctx, `UPDATE orders SET cookie_id=?,chat_id='',paid_at='',shipped_at='',
		completed_at='',buyer_reviewed_at='' WHERE order_id=? AND cookie_id=? AND version=?`,
		cookieID, expected.OrderID, expected.CookieID, expected.Version+1); err != nil {
		return err
	}
	// err 保存卖家事实合并错误，金额等校验失败也会回滚已经写入的审计。
	if err := upsertOrder(ctx, transaction, o.Dialect, expected.OrderID, options); err != nil {
		return err
	}
	return transaction.Commit()
}

// lockRecoveryAccounts 为 o 的事务 transaction 按 ID 顺序锁定 oldCookieID/newCookieID；ctx 控制取消。
// 只读取属于 userID 的账号 ID，恰有两个账号才返回成功；缺失或越权均返回 ErrForbidden。
func (o *Orders) lockRecoveryAccounts(ctx context.Context, transaction *sql.Tx, userID int64, oldCookieID, newCookieID string) error {
	// query 在行锁方言保持账号归属到提交不变；SQLite 后续 CAS 在串行化事务内取得写锁。
	query := `SELECT id FROM cookies WHERE user_id=? AND id IN (?,?) ORDER BY id`
	if o.Dialect != DialectSQLite {
		query += ` FOR UPDATE`
	}
	// rows、err 保存非敏感账号 ID 游标及查询失败。
	rows, err := transaction.QueryContext(ctx, query, userID, oldCookieID, newCookieID)
	if err != nil {
		return err
	}
	defer rows.Close()
	// count 统计锁定的同用户账号数，不读取 Cookie 或元数据。
	count := 0
	for rows.Next() {
		// accountID 是当前已复核归属的账号，不向调用方暴露额外信息。
		var accountID string
		// err 保存当前账号标识扫描错误；不允许漏掉账号后继续恢复。
		if err := rows.Scan(&accountID); err != nil {
			return err
		}
		count++
	}
	// err 保存账号游标读取失败，必须与缺少账号的业务结果区分。
	if err := rows.Err(); err != nil {
		return err
	}
	if count != 2 {
		return ErrForbidden
	}
	return nil
}

// checkRecoverySideEffects 检查 o 的 transaction 中 orderID 的所有运行和未完成补偿，以及买卖账号待执行任务。
// ctx 控制读取；运行记录包含发货卡密凭据，任何状态均阻止；任务原文只在本函数解析，禁止审计或输出。
// 通知 outbox 是通知历史，不是履约证据；修复保留其原归属与投递状态，不读取正文或渠道凭证。
func (o *Orders) checkRecoverySideEffects(ctx context.Context, transaction *sql.Tx, orderID, oldCookieID, newCookieID string) error {
	// unsafe 表示任意历史执行守卫、运行记录（包括发卡凭据）或未完成补偿存在；规则级联删除不能消除守卫。
	var unsafe bool
	// err 保存仅存在性检查的数据库错误，不读取发货凭据或外部动作载荷。
	if err := transaction.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM order_automation_guards WHERE order_id=?) OR
		EXISTS(SELECT 1 FROM automation_runs WHERE order_id=?) OR
		EXISTS(SELECT 1 FROM order_reconciliations WHERE order_id=? AND status<>'resolved')`, orderID, orderID, orderID).Scan(&unsafe); err != nil {
		return err
	}
	if unsafe {
		return ErrOrderRecoveryUnsafe
	}
	// query 只检查两个已授权账号的全状态任务，不读取其他用户载荷，也不锁住全局任务队列。
	query := `SELECT cookie_id,task_key,task_json FROM automation_pending_tasks WHERE cookie_id IN (?,?)`
	if o.Dialect != DialectSQLite {
		query += ` FOR UPDATE`
	}
	// rows、err 保存任务游标及数据库读取错误，数据库错误必须触发上层回滚。
	rows, err := transaction.QueryContext(ctx, query, oldCookieID, newCookieID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		// accountID、taskKey、raw 保存当前任务定位信息和临时 JSON；不输出原文或其中的秘密。
		var accountID, taskKey, raw string
		// err 保存相关账号任务扫描错误，失败即回滚，不输出任务原文。
		if err := rows.Scan(&accountID, &taskKey, &raw); err != nil {
			return err
		}
		// task 只反序列化必要订单标识，忽略消息、凭证、动作内容等字段。
		var task struct {
			// OrderID 兼容当前 Go Task 默认 JSON 字段名称。
			OrderID string `json:"OrderID"`
			// LegacyOrderID 兼容历史蛇形订单标识字段。
			LegacyOrderID string `json:"order_id"`
		}
		// decodeErr 表示任务无法可靠读取；相关账号的坏任务必须保守拒绝。
		decodeErr := json.Unmarshal([]byte(raw), &task)
		if task.OrderID == orderID || task.LegacyOrderID == orderID || strings.HasSuffix(taskKey, ":"+orderID) ||
			((decodeErr != nil || (task.OrderID == "" && task.LegacyOrderID == "")) && (accountID == oldCookieID || accountID == newCookieID)) {
			return ErrOrderRecoveryUnsafe
		}
	}
	return rows.Err()
}
