package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// beginOwnershipWrite 为 a 的自动化写入建立事务；ctx 控制等待，cookieID/orderID 是任务声称的本地归属。
// 锁顺序与 RecoverSoldOwnership 一致：账号 → 订单 → 调用方的运行/延期记录。成功返回的事务必须由调用方提交或回滚。
// 事务不覆盖外部动作；运行先提交会阻止修复，修复先提交则本方法拒绝旧归属。无订单或尚无本地行的历史任务保持兼容。
func (a *AutomationRules) beginOwnershipWrite(ctx context.Context, cookieID, orderID string) (*sql.Tx, error) {
	// transaction、beginErr 保存本次短数据库事务；默认隔离下先写锁再查询，避免预先建立过期读取快照。
	transaction, beginErr := a.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return nil, beginErr
	}
	// lockErr 保存账号和订单归属锁定结果；失败事务不能泄漏到调用方。
	if lockErr := lockAutomationOwnership(ctx, transaction, cookieID, orderID); lockErr != nil {
		_ = transaction.Rollback()
		return nil, lockErr
	}
	return transaction, nil
}

// lockAutomationOwnership 在 transaction 内先占有 cookieID 账号，再占有 orderID 订单并检查归属；ctx 限制锁等待。
// 不改业务值或版本；账号锁也覆盖订单尚未入库或延期载荷无法提取订单号的兼容情况，防止与修复的账号锁交错。
// 本函数只返回非敏感仓储错误；调用方必须持锁到运行或延期记录写入提交后，禁止持锁执行网络 I/O。
func lockAutomationOwnership(ctx context.Context, transaction *sql.Tx, cookieID, orderID string) error {
	// result、lockErr 保存账号无值变更写锁结果；三方言均按匹配行计数，MySQL 连接已启用 clientFoundRows。
	result, lockErr := transaction.ExecContext(ctx, `UPDATE cookies SET id=id WHERE id=?`, cookieID)
	if lockErr != nil {
		return lockErr
	}
	// matched、countErr 保存账号存在性；不存在的接收账号不允许新建执行痕迹。
	matched, countErr := result.RowsAffected()
	if countErr != nil {
		return countErr
	}
	if matched != 1 {
		return ErrForbidden
	}
	if orderID == "" {
		return nil
	}
	// orderLockErr 保存订单行锁错误；不递增版本，避免把领取任务伪装成订单事实更新。
	if _, orderLockErr := transaction.ExecContext(ctx, `UPDATE orders SET version=version WHERE order_id=?`, orderID); orderLockErr != nil {
		return orderLockErr
	}
	// owner 保存包含软删除订单在内的真实归属，仅在本函数比较，不读取凭证或收货信息。
	var owner string
	// readErr 保存写锁之后的归属读取结果；无本地行的旧任务仍由账号锁保证与后续修复互斥。
	readErr := transaction.QueryRowContext(ctx, `SELECT COALESCE(cookie_id,'') FROM orders WHERE order_id=?`, orderID).Scan(&owner)
	if errors.Is(readErr, sql.ErrNoRows) {
		return nil
	}
	if readErr != nil {
		return readErr
	}
	if owner != cookieID {
		return ErrForbidden
	}
	return nil
}

// TryStartRun 在 a 中原子复核 run 订单归属并创建或重领运行；ctx 控制取消，返回运行 ID、是否取得执行权和错误。
// UNIQUE(rule_id,trigger_key) 仍负责防重；归属拒绝返回 ErrForbidden，任何失败都回滚，外部动作只能在提交成功后执行。
// 非空订单的新建、重领和任意状态的匹配既有运行都在同事务保留独立痕迹，物理删除规则不能再解除归属恢复保护。
func (a *AutomationRules) TryStartRun(ctx context.Context, run AutomationRun) (int64, bool, error) {
	// transaction、beginErr 保存已锁定归属的短事务。
	transaction, beginErr := a.beginOwnershipWrite(ctx, run.CookieID, run.OrderID)
	if beginErr != nil {
		return 0, false, beginErr
	}
	defer transaction.Rollback()
	// runID、started、startErr 保存同事务中的幂等创建/重领结果，未提交时不能向执行层返回成功。
	runID, started, startErr := a.tryStartRun(ctx, transaction, run)
	if startErr != nil {
		return 0, false, startErr
	}
	if run.OrderID != "" {
		// guardQuery 只从身份匹配的真实运行留存订单/账号，不依赖运行状态，也不要求订单已入库。
		// 联合主键忽略重复且不刷新首次记录时间；幂等键撞到其他身份时不为传入订单伪造执行痕迹。
		guardQuery := dialectInsertIgnorePrefix(a.Dialect) + ` INTO order_automation_guards(order_id,cookie_id)
			SELECT order_id,cookie_id FROM automation_runs
			WHERE rule_id=? AND trigger_key=? AND cookie_id=? AND order_id=?` + dialectInsertIgnore(a.Dialect, []string{"order_id", "cookie_id"})
		// guardErr 必须与创建或重领一起回滚，不能把未持久化保护的执行权交给外部动作。
		if _, guardErr := transaction.ExecContext(ctx, guardQuery, run.RuleID, run.TriggerKey, run.CookieID, run.OrderID); guardErr != nil {
			return 0, false, guardErr
		}
	}
	// commitErr 保存释放归属锁前的持久化结果，提交不确定时不授权动作。
	if commitErr := transaction.Commit(); commitErr != nil {
		return 0, false, commitErr
	}
	return runID, started, nil
}

// StartRunAction 为 a 中 runID 的 attempt 代次和 cursor 游标领取动作；ctx 控制取消，leaseExpiresAt 是 UTC 租约截止秒数。
// 返回领取结果和数据库错误。先读取不含秘密的运行身份，再按账号 → 订单 → 运行顺序锁定，避免更新运行后反向等订单锁。
// 归属不符或运行不存在均不授权动作；提交完成后才允许外部 I/O，已有运行在动作期间持续阻止 RecoverSoldOwnership。
func (a *AutomationRules) StartRunAction(ctx context.Context, runID int64, attempt, cursor int, leaseExpiresAt int64) (bool, error) {
	// cookieID、orderID 是运行创建后固定的身份，只用于归属检查，不解析运行快照或卡密凭据。
	var cookieID, orderID string
	// readErr 保存运行身份读取结果；已删除的运行没有可领取的动作。
	readErr := a.DB.QueryRowContext(ctx, `SELECT cookie_id,COALESCE(order_id,'') FROM automation_runs WHERE id=?`, runID).Scan(&cookieID, &orderID)
	if errors.Is(readErr, sql.ErrNoRows) {
		return false, nil
	}
	if readErr != nil {
		return false, readErr
	}
	// transaction、beginErr 保存动作领取的归属锁事务，归属不符沿用未领取的布尔语义。
	transaction, beginErr := a.beginOwnershipWrite(ctx, cookieID, orderID)
	if errors.Is(beginErr, ErrForbidden) {
		return false, nil
	}
	if beginErr != nil {
		return false, beginErr
	}
	defer transaction.Rollback()
	// started、startErr 保存同事务中的代次和游标 CAS 结果，不使用事务外的身份读取授权动作。
	started, startErr := a.startRunAction(ctx, transaction, runID, attempt, cursor, leaseExpiresAt)
	if startErr != nil {
		return false, startErr
	}
	// commitErr 保存动作检查点提交结果，提交失败不能启动外部动作。
	if commitErr := transaction.Commit(); commitErr != nil {
		return false, commitErr
	}
	return started, nil
}

// DeferTask 在 a 中将 task 原子写入延期队列；ctx 控制事务。订单号仅从最小字段投影读取，任务原文不进入日志。
// 当前 OrderID 和历史 order_id 都参与归属校验；二者冲突则拒绝。坏 JSON 仍沿用原有存储兼容，但账号锁与修复互斥。
func (a *AutomationRules) DeferTask(ctx context.Context, task DeferredAutomationTask) error {
	// identity 只解析归属检查所需的订单号，不读取凭证、卡密或消息内容。
	var identity struct {
		// OrderID 是当前 Task 默认编码的订单标识。
		OrderID string `json:"OrderID"`
		// LegacyOrderID 是历史延期载荷中的订单标识。
		LegacyOrderID string `json:"order_id"`
	}
	_ = json.Unmarshal([]byte(task.TaskJSON), &identity)
	if identity.OrderID != "" && identity.LegacyOrderID != "" && identity.OrderID != identity.LegacyOrderID {
		return ErrForbidden
	}
	if identity.OrderID == "" {
		identity.OrderID = identity.LegacyOrderID
	}
	// transaction、beginErr 保存按账号、订单顺序锁定的延期写入事务。
	transaction, beginErr := a.beginOwnershipWrite(ctx, task.CookieID, identity.OrderID)
	if beginErr != nil {
		return beginErr
	}
	defer transaction.Rollback()
	// writeErr 保存已有延期 UPSERT 的结果，失败时不提交任何更新。
	if writeErr := a.deferTask(ctx, transaction, task); writeErr != nil {
		return writeErr
	}
	return transaction.Commit()
}
