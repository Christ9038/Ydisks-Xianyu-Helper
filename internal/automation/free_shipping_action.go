package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/xianyu/mtop"
)

// freeShipBargain 在砍价“待刀成”阶段调用独立免拼接口；它不发送卡密、不确认发货，也不修改订单已发货状态。
func (e *automationActionExecutor) freeShipBargain(ctx context.Context, task Task) error {
	return e.freeShipBargainAttempt(ctx, task, true)
}

// freeShipBargainAttempt 在砍价“待刀成”阶段调用一次独立免拼接口；allowCredentialRecovery 只允许首次调用
// 在平台明确返回 Session 失效时恢复账号凭证并重试一次，避免把已恢复后仍失败的请求循环执行。
// 它不发送卡密、不确认发货，也不修改订单已发货状态。
func (e *automationActionExecutor) freeShipBargainAttempt(ctx context.Context, task Task, allowCredentialRecovery bool) error {
	if task.OrderID == "" || strings.TrimSpace(task.ItemID) == "" || strings.TrimSpace(task.BuyerID) == "" {
		return fmt.Errorf("%w: 免拼发货缺少订单ID、商品ID或买家ID", errActionNotPerformed)
	}
	// session 固定本次免拼请求的凭证视图，外部调用期间不持有账号凭证锁。
	session, err := e.openShipmentConsignSession(ctx, task.AccountID)
	if err != nil {
		return err
	}
	// freeShipping、supported 保存当前 MTOP 客户端的独立免拼能力。
	freeShipping, supported := e.mtop().(freeShippingClient)
	if !supported {
		return fmt.Errorf("%w: 当前 MTOP 客户端不支持免拼发货", errActionNotPerformed)
	}
	// succeeded、returns、updatedCookie、callErr 保存免拼接口的远端结果。
	succeeded, returns, updatedCookie, callErr := freeShipping.FreeShippingContext(session.requestContext, session.cookieStr, task.OrderID, task.ItemID, task.BuyerID)
	// result 统一 Cookie 持久化所需的远端调用结果。
	result := shipmentConsignResult{succeeded: succeeded, returns: returns, updatedCookie: updatedCookie, callErr: callErr}
	// cookiePersistence 保存响应 Cookie 的条件写回错误。
	cookiePersistence := e.persistShipmentConsignCookies(ctx, task.AccountID, session, result)
	// sessionErr 将请求层错误和远端业务失败统一为账号级凭证状态判断输入。
	sessionErr := result.callErr
	if sessionErr == nil && !result.succeeded {
		sessionErr = errors.New(strings.Join(result.returns, "; "))
	}
	if mtop.IsSessionExpiredErr(sessionErr) {
		// credentialLabel 标识唯一允许调用账号恢复器的 Session 失效；Token 已由 MTOP 内部刷新。
		credentialLabel := "Session"
		if len(cookiePersistence.errors) > 0 {
			return errors.Join(fmt.Errorf("免拼发货 %s 已失效: %w", credentialLabel, sessionErr), errors.Join(cookiePersistence.errors...))
		}
		// recoverer 保存本次判断使用的凭证恢复器快照，避免运行期间替换依赖改变重试归属。
		recoverer := e.recoverer()
		if allowCredentialRecovery && recoverer != nil && recoverer.RecoverExpiredCredential(ctx, task.AccountID) {
			e.logger.Info("免拼发货凭证恢复成功，重新执行免拼", "account", task.AccountID, "order_id", task.OrderID)
			return e.freeShipBargainAttempt(ctx, task, false)
		}
		if !allowCredentialRecovery {
			e.logger.Warn("免拼发货凭证恢复后仍返回 Session 失效，停止重复重试", "account", task.AccountID, "order_id", task.OrderID)
			return fmt.Errorf("%w: 免拼发货在凭证恢复后仍返回 %s 失效: %v", errActionNotPerformed, credentialLabel, sessionErr)
		}
		e.logger.Warn("免拼发货凭证恢复失败，停止自动重试并保留人工处理", "account", task.AccountID, "order_id", task.OrderID)
		return fmt.Errorf("%w: 免拼发货 %s 已失效且凭证恢复失败: %v", errActionNotPerformed, credentialLabel, sessionErr)
	}
	if result.callErr != nil {
		if len(cookiePersistence.errors) > 0 {
			return uncertainAction(errors.Join(result.callErr, errors.Join(cookiePersistence.errors...)))
		}
		return uncertainAction(result.callErr)
	}
	if !result.succeeded {
		// failure 表示平台明确拒绝免拼；该结果可由同阶段新 WS 事件重新尝试。
		failure := fmt.Errorf("免拼发货失败: %s", strings.Join(result.returns, "; "))
		if len(cookiePersistence.errors) > 0 {
			return errors.Join(failure, errors.Join(cookiePersistence.errors...))
		}
		return failure
	}
	if len(cookiePersistence.errors) > 0 {
		// 平台已明确返回免拼成功，响应 Cookie 未写回（含凭证被并发更新而跳过旧响应）只影响本地凭证，
		// 不改变已完成的平台动作；若判为结果未知会让阶段停在 needs_review，既不可重试也无法被后续兜底推进。
		e.logger.Warn("免拼已成功，但响应 Cookie 未写回，按成功处理", "account", task.AccountID, "order_id", task.OrderID, "err", errors.Join(cookiePersistence.errors...))
	}
	return nil
}
