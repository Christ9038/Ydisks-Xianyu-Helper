// automation_recovery_pending_ship.go 待发货兜底扫描的持久化查询。
// 职责：为「平台付款消息丢失 / 运行未完成」的订单提供稳定游标的补触发扫描入口。
// 从 automation_recovery.go 按业务职责拆出，仅移动代码、不改变任何逻辑。

package db

import (
	"context"
	"database/sql"
)

// ObsoleteRunReasonMarker 标记「订单离开待发货时被安全取消、且从未触达任何外部动作」的付款运行。
// 订单经平台快照从退款中回到待发货后，只有带此标记的运行才允许被重领或不再阻断无运行兜底；
// 其余已取消运行（人工取消、已有发送副作用、动作结果未知）仍永久保持终态，避免重复发货。
const ObsoleteRunReasonMarker = "[obsolete-pending-ship]"

// reclaimableObsoleteRunSQL 是「可被新一轮付款发货接管的已取消运行」的行条件（列名不加别名，可用于 UPDATE 与子查询）。
// 要求：带安全取消标记，且无已发送内容、无动作游标进度、无进行中动作、无发货凭证。
const reclaimableObsoleteRunSQL = `status='canceled' AND error_message LIKE '` + ObsoleteRunReasonMarker + `%'
	            AND action_started=0 AND sent_count=0 AND action_cursor=0 AND delivery_proof=''`

// resumableObsoleteProgressRunSQL 是「订单离开待发货时被安全取消、且发卡/模板已确认完成」的运行行条件（列名不加别名）。
// 动作游标只在动作成功后推进且取消时没有进行中动作，因此买家已收到内容；订单退款撤回后只允许由冻结计划
// 判定为纯幂等收尾（确认发货）的运行续跑，绝不重发内容。
const resumableObsoleteProgressRunSQL = `status='canceled' AND error_message LIKE '` + ObsoleteRunReasonMarker + `%'
	            AND action_started=0 AND action_cursor>0`

// MissingProofReasonMarker 标记「退款撤回后本可续跑、但发货凭证缺失」而被转入人工核对的运行。
// 带此标记的运行不再进入自动续跑扫描，避免循环隔离，也避免用空凭证提交确认发货。
const MissingProofReasonMarker = "[missing-delivery-proof]"

// QuarantineCanceledRunMissingProof 把凭证缺失的可续跑已取消运行（含 07453c1 版本遗留的、取消时已清空凭证的记录）
// 转入人工核对；仅当运行仍与 attempt 代次一致、带安全取消标记、无进行中动作且凭证为空时生效。
// 返回是否真正隔离及数据库错误；零行是与其他调度器竞争的正常结果。
func (a *AutomationRules) QuarantineCanceledRunMissingProof(ctx context.Context, runID int64, attempt int, reason string) (bool, error) {
	// result、err 保存条件隔离结果。
	result, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
   SET status='needs_review',error_message=?,lease_expires_at=0,next_retry_at=0,updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND attempt_count=? AND `+resumableObsoleteProgressRunSQL+` AND delivery_proof=''`, MissingProofReasonMarker+reason, runID, attempt)
	if err != nil {
		return false, err
	}
	// affected、rowsErr 保存实际隔离行数与驱动错误。
	affected, rowsErr := result.RowsAffected()
	return affected == 1, rowsErr
}

// CancelObsoletePaidRecoveryRun 取消已不再处于待发货状态的付款运行恢复；除「退款中且发卡已完成」外清除可重放发货凭证。
// 更新同时校验扫描快照和订单当前事实；并发续租、游标推进或订单重新进入待发货都会让本次取消失效。
func (a *AutomationRules) CancelObsoletePaidRecoveryRun(ctx context.Context, run AutomationRun, reason string) (bool, error) {
	// 取消时动作结果未知（action_started=1）的运行不打可接管标记，订单重新待发货后也不得自动重放。
	if !run.ActionStarted {
		reason = ObsoleteRunReasonMarker + reason
	}
	// proofSQL 决定取消后的发货凭证：订单因退款中而离开待发货、且发卡/模板已完成（游标已推进、无进行中动作）时，
	// 退款撤回后的续跑需要用它恢复已发送的卡密文本/图片来确认发货，必须原样保留（仍为加密存储）；
	// 其余取消（订单已发货、完成、取消等）不再需要，照旧清空。
	proofSQL := "''"
	if !run.ActionStarted && run.ActionCursor > 0 {
		proofSQL = `CASE WHEN EXISTS (SELECT 1 FROM orders ro
                           WHERE ro.order_id=automation_runs.order_id AND ro.cookie_id=automation_runs.cookie_id
                             AND ro.order_status='refunding')
                      THEN delivery_proof ELSE '' END`
	}
	// result、err 保存基于运行快照和最新订单状态的条件取消结果。
	result, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
   SET status='canceled',action_started=0,lease_expires_at=0,next_retry_at=0,error_message=?,delivery_proof=`+proofSQL+`,updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND attempt_count=? AND status=? AND action_cursor=? AND action_started=? AND lease_expires_at=?
   AND trigger_type='order_paid'
   AND NOT EXISTS (SELECT 1 FROM orders o
                    WHERE o.order_id=automation_runs.order_id
                      AND o.cookie_id=automation_runs.cookie_id
                      AND o.deleted_at IS NULL
                      AND o.order_status IN `+pendingShipStatusSQLList()+`
                      AND o.system_shipped=0)`, reason, run.ID, run.AttemptCount, run.Status, run.ActionCursor, boolToInt(run.ActionStarted), run.LeaseExpiresAt)
	if err != nil {
		return false, err
	}
	// affected、rowsErr 表示当前扫描是否仍拥有取消权及驱动读取行数时的错误。
	affected, rowsErr := result.RowsAffected()
	return affected == 1, rowsErr
}

// PendingShipOrdersWithoutPaidRunAfter 用不可变的订单 ID 作为稳定游标分页扫描「已付款待发货、但尚无任何
// order_paid 运行」的订单，供调度器在付款系统消息丢失时补触发自动发货。
//
// 兜底边界：
//   - 只取 order_status 属于待发货取值集合（pending_ship、paid、2、pending_delivery，见 pendingShipStatuses），即平台侧已确认付款、只差我方发货；
//   - 只取**没有任何有效** order_paid 运行的订单；已有运行的订单交给失败的运行恢复扫描处理，
//     避免与恢复链重复触发、也避免形成第二个执行入口。唯一例外是订单离开待发货时被安全取消、
//     且无任何发送副作用的运行（ObsoleteRunReasonMarker），退款撤回后该订单重新进入候选并由同一幂等键重领；
//   - 砍价订单只有在“待刀成”阶段已成功免拼后才允许进入候选集合；调度器不得调用免拼接口，
//     也不得根据订单的砍价标记推断平台阶段；
//   - 要求账号下存在启用的 order_paid 规则且商品匹配，否则没有可执行的动作；
//   - 必须有 chat_id，否则无法向买家发起会话发送。
func (a *AutomationRules) PendingShipOrdersWithoutPaidRunAfter(ctx context.Context, afterOrderID string, limit int) ([]Order, error) {
	if limit <= 0 {
		limit = 200
	}
	// cursorSQL 用于本次流程后续判断的游标SQL
	cursorSQL := ""
	// args 用于本次流程后续判断的args
	args := []any{}
	if afterOrderID != "" {
		cursorSQL = " AND o.order_id>?"
		args = append(args, afterOrderID)
	}
	args = append(args, limit)
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
SELECT order_id,item_id,buyer_id,spec_name,spec_value,quantity,amount,order_status,cookie_id,is_bargain,
       receiver_name,receiver_phone,receiver_address,receiver_city,version,chat_id,system_shipped,
       COALESCE(paid_at,''),COALESCE(shipped_at,''),COALESCE(completed_at,''),
       COALESCE(buyer_reviewed_at,''),COALESCE(last_review_request_at,''),review_request_count,
       created_at,updated_at
  FROM orders o
WHERE o.order_status IN `+pendingShipStatusSQLList()+`
   AND o.deleted_at IS NULL
   AND COALESCE(o.chat_id,'')<>''
	   AND (o.is_bargain=0 OR EXISTS (SELECT 1 FROM bargain_free_shipping_stages bfs
	                                  WHERE bfs.order_id=o.order_id AND bfs.cookie_id=o.cookie_id AND bfs.status IN ('ready','succeeded')))
   AND NOT EXISTS (SELECT 1 FROM automation_runs r
                    WHERE r.order_id=o.order_id AND r.trigger_type='order_paid'
                      AND NOT (r.status='canceled' AND r.error_message LIKE '`+ObsoleteRunReasonMarker+`%'
                               AND r.action_started=0 AND r.sent_count=0 AND r.action_cursor=0 AND r.delivery_proof=''))
   AND EXISTS (SELECT 1 FROM automation_rules r2
                WHERE r2.cookie_id=o.cookie_id AND r2.trigger_type='order_paid'
                  AND r2.deleted_at IS NULL AND r2.enabled=1
                  AND (r2.item_id=o.item_id OR r2.item_id=''))`+cursorSQL+`
 ORDER BY o.order_id ASC
 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	out := []Order{}
	for rows.Next() {
		// ord 用于本次流程后续判断的ord
		var ord Order
		// isBargain、version、sysShipped 用于本次流程后续判断的isBargain、version、sysShipped
		var isBargain, version, sysShipped int
		// itemID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID 保存商品ID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID，供当前处理流程使用
		var itemID, buyerID, specName, specValue, qty, amount, status, cookieID,
			receiverName, receiverPhone, receiverAddr, receiverCity, chatID sql.NullString
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&ord.OrderID, &itemID, &buyerID, &specName, &specValue, &qty, &amount,
			&status, &cookieID, &isBargain, &receiverName, &receiverPhone, &receiverAddr,
			&receiverCity, &version, &chatID, &sysShipped, &ord.PaidAt,
			&ord.ShippedAt, &ord.CompletedAt, &ord.BuyerReviewedAt, &ord.LastReviewRequestAt,
			&ord.ReviewRequestCount, &ord.CreatedAt, &ord.UpdatedAt); err != nil {
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
		ord.ReceiverName = receiverName.String
		ord.ReceiverPhone = receiverPhone.String
		ord.ReceiverAddr = receiverAddr.String
		ord.ReceiverCity = receiverCity.String
		ord.ChatID = chatID.String
		ord.IsBargain = isBargain
		ord.Version = version
		ord.SystemShipped = sysShipped != 0
		out = append(out, ord)
	}
	return out, rows.Err()
}

// PendingShipResume 描述一条尚可断点续跑的待发货订单运行。
type PendingShipResume struct {
	// Order 是平台侧已确认付款、仍需我方发货的订单事实。
	Order Order
	// RunID 是尚未完成全部动作的自动化运行主键。
	RunID int64
	// RuleID 是该运行所属规则，用于校验归属规则是否仍处于启用状态。
	RuleID int64
	// RawEventJSON 是运行创建时冻结的任务快照，内含不可变的动作计划；
	// 调用方必须用它判定「剩余动作是否只含幂等状态动作」，不得改用现行规则的动作。
	RawEventJSON string
	// Attempt 是运行当前代次；重开运行必须带上它做 CAS。
	Attempt int
	// ActionCursor 是尚未执行的下一个动作下标。
	ActionCursor int
	// HasDeliveryProof 表示运行是否保存了发货凭证；canceled 候选缺失凭证时不得自动确认发货。
	HasDeliveryProof bool
	// Status 是运行当前状态（needs_review、failed，或退款撤回后可续跑的 canceled）。
	Status string
}

// PendingShipResumableRunsAfter 用订单 ID 作为稳定游标分页扫描「已付款待发货、且存在剩余动作
// 全部为幂等状态动作的未完成 order_paid 运行」的订单，供调度器从检查点继续执行。
//
// 与 PendingShipOrdersWithoutPaidRunAfter 的分工：
//   - 前者覆盖「完全没有运行记录」的订单（付款事件在准备阶段就被卡死）；
//   - 本方法覆盖「有运行但没做完」的订单（例如消息动作结果不确定后运行被隔离）。
//
// **安全边界**：本方法只做「运行自身」可判定的过滤——状态、代次上限、订单仍在待发货、
// **归属规则仍处于启用状态**。真正的放行判据（剩余动作是否只含幂等状态动作）必须由调用方
// 依据运行快照里冻结的动作计划判定：`automation_rule_actions` 是会被管理员编辑的现行配置，
// 用它判定会把数字游标套用到改过的规则上，从而改变历史订单的发货义务。
// 订单退款撤回后，带 ObsoleteRunReasonMarker 且游标已推进的已取消运行同样纳入，由调用方的冻结计划检查
// 保证只会补做确认发货，不会重发内容。
// 因此本方法额外返回 RawEventJSON，供调用方恢复冻结计划。
func (a *AutomationRules) PendingShipResumableRunsAfter(ctx context.Context, afterOrderID string, maxAttempts, limit int) ([]PendingShipResume, error) {
	if limit <= 0 {
		limit = 200
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	// cursorSQL 保存稳定游标分页片段。
	cursorSQL := ""
	// args 保存查询参数，顺序与 SQL 中的占位符一致。
	args := []any{maxAttempts}
	if afterOrderID != "" {
		cursorSQL = " AND o.order_id>?"
		args = append(args, afterOrderID)
	}
	args = append(args, limit)
	// rows、err 保存待续跑运行查询结果及数据库错误。
	rows, err := a.DB.QueryContext(ctx, `
SELECT o.order_id,o.item_id,o.buyer_id,o.spec_name,o.spec_value,o.quantity,o.amount,o.order_status,o.cookie_id,o.is_bargain,
       o.receiver_name,o.receiver_phone,o.receiver_address,o.receiver_city,o.version,o.chat_id,o.system_shipped,
       COALESCE(o.paid_at,''),COALESCE(o.shipped_at,''),COALESCE(o.completed_at,''),
       COALESCE(o.buyer_reviewed_at,''),COALESCE(o.last_review_request_at,''),o.review_request_count,
       o.created_at,o.updated_at,
       r.id,r.rule_id,COALESCE(r.raw_event_json,''),r.attempt_count,r.action_cursor,r.status,
       CASE WHEN r.delivery_proof<>'' THEN 1 ELSE 0 END
  FROM orders o
  JOIN automation_runs r ON r.order_id=o.order_id AND r.trigger_type='order_paid'
WHERE o.order_status IN `+pendingShipStatusSQLList()+`
   AND o.deleted_at IS NULL
   AND COALESCE(o.chat_id,'')<>''
   AND (o.is_bargain=0 OR EXISTS (SELECT 1 FROM bargain_free_shipping_stages bfs
                                  WHERE bfs.order_id=o.order_id AND bfs.cookie_id=o.cookie_id AND bfs.status IN ('ready','succeeded')))
   AND r.cookie_id=o.cookie_id
   AND (r.status IN ('needs_review','failed') AND r.error_message NOT LIKE '`+MissingProofReasonMarker+`%'
        OR (r.status='canceled' AND r.error_message LIKE '`+ObsoleteRunReasonMarker+`%'
            AND r.action_started=0 AND r.action_cursor>0))
   AND r.attempt_count<?
   AND r.id=(SELECT latest.id
               FROM automation_runs latest
              WHERE latest.order_id=o.order_id
                AND latest.trigger_type='order_paid'
              ORDER BY latest.updated_at DESC,latest.id DESC
              LIMIT 1)
   AND NOT EXISTS (SELECT 1 FROM automation_runs active
                    WHERE active.order_id=o.order_id
                      AND active.trigger_type='order_paid'
                      AND active.status='running')
   AND EXISTS (SELECT 1 FROM automation_rules owner
                WHERE owner.id=r.rule_id AND owner.cookie_id=o.cookie_id
                  AND owner.trigger_type='order_paid'
                  AND owner.deleted_at IS NULL AND owner.enabled=1)`+cursorSQL+`
 ORDER BY o.order_id ASC
 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存本次扫描到的可续跑运行。
	out := []PendingShipResume{}
	for rows.Next() {
		// candidate 保存当前行的订单事实与运行检查点。
		var candidate PendingShipResume
		// isBargain、version、sysShipped 保存订单行的整型事实字段。
		var isBargain, version, sysShipped, hasProof int
		// 以下 NullString 保存可能为空的订单文本字段。
		var itemID, buyerID, specName, specValue, qty, amount, status, cookieID,
			receiverName, receiverPhone, receiverAddr, receiverCity, chatID sql.NullString
		// err 保存本行扫描错误；非空时交由调用方决定是否重试整轮查询。
		if err := rows.Scan(&candidate.Order.OrderID, &itemID, &buyerID, &specName, &specValue, &qty, &amount,
			&status, &cookieID, &isBargain, &receiverName, &receiverPhone, &receiverAddr,
			&receiverCity, &version, &chatID, &sysShipped, &candidate.Order.PaidAt,
			&candidate.Order.ShippedAt, &candidate.Order.CompletedAt, &candidate.Order.BuyerReviewedAt,
			&candidate.Order.LastReviewRequestAt, &candidate.Order.ReviewRequestCount,
			&candidate.Order.CreatedAt, &candidate.Order.UpdatedAt,
			&candidate.RunID, &candidate.RuleID, &candidate.RawEventJSON, &candidate.Attempt, &candidate.ActionCursor, &candidate.Status, &hasProof); err != nil {
			return nil, err
		}
		candidate.HasDeliveryProof = hasProof != 0
		candidate.Order.ItemID = itemID.String
		candidate.Order.BuyerID = buyerID.String
		candidate.Order.SpecName = specName.String
		candidate.Order.SpecValue = specValue.String
		candidate.Order.Quantity = qty.String
		candidate.Order.Amount = amount.String
		candidate.Order.OrderStatus = status.String
		candidate.Order.CookieID = cookieID.String
		candidate.Order.ReceiverName = receiverName.String
		candidate.Order.ReceiverPhone = receiverPhone.String
		candidate.Order.ReceiverAddr = receiverAddr.String
		candidate.Order.ReceiverCity = receiverCity.String
		candidate.Order.ChatID = chatID.String
		candidate.Order.IsBargain = isBargain
		candidate.Order.Version = version
		candidate.Order.SystemShipped = sysShipped != 0
		out = append(out, candidate)
	}
	return out, rows.Err()
}
