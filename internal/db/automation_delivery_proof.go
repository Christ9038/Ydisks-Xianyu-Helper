package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// GetRun 返回自动化运行及动作检查点，并在执行动作前恢复发货凭证。
func (a *AutomationRules) GetRun(ctx context.Context, id int64) (*AutomationRun, error) {
	// run 保存读取到的自动化运行。
	var run AutomationRun
	// actionStarted 保存数据库整数布尔值。
	var actionStarted int
	// deliveryProofJSON 保存数据库中的加密凭证 JSON。
	var deliveryProofJSON string
	// err 保存自动化运行读取错误。
	err := a.DB.QueryRowContext(ctx, `SELECT id,rule_id,cookie_id,item_id,order_id,buyer_id,chat_id,trigger_type,trigger_key,
		status,sent_count,error_message,raw_event_json,delivery_proof,lease_expires_at,attempt_count,next_retry_at,action_cursor,action_started
		FROM automation_runs WHERE id=?`, id).Scan(&run.ID, &run.RuleID, &run.CookieID, &run.ItemID, &run.OrderID,
		&run.BuyerID, &run.ChatID, &run.TriggerType, &run.TriggerKey, &run.Status, &run.SentCount,
		&run.ErrorMessage, &run.RawEventJSON, &deliveryProofJSON, &run.LeaseExpiresAt, &run.AttemptCount, &run.NextRetryAt,
		&run.ActionCursor, &actionStarted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	run.ActionStarted = actionStarted != 0
	if err != nil {
		return &run, err
	}
	// proof、proofErr 保存解密和校验后的短期发货凭证。
	proof, proofErr := a.decodeDeliveryProof(run.ID, deliveryProofJSON)
	if proofErr != nil {
		return nil, proofErr
	}
	run.DeliveryProof = proof
	return &run, nil
}

// AdvanceRunAction 在动作明确成功后原子推进游标、累计数量并保存或清除发货凭证。
func (a *AutomationRules) AdvanceRunAction(ctx context.Context, advance AutomationRunActionAdvance) error {
	return a.advanceRunAction(ctx, advance)
}

// advanceRunAction 原子推进动作游标、累计发送数并保存或清空确认发货凭证。
func (a *AutomationRules) advanceRunAction(ctx context.Context, advance AutomationRunActionAdvance) error {
	// assignments 保存本次检查点需要更新的列。
	assignments := "action_cursor=?,action_started=0,sent_count=sent_count+?,updated_at=CURRENT_TIMESTAMP"
	// args 保存检查点 SQL 的参数。
	args := []any{advance.Cursor + 1, advance.SentDelta}
	if advance.ClearDeliveryProof {
		assignments = "delivery_proof=''," + assignments
	} else if advance.DeliveryProof != nil {
		// encryptedProof 保存按运行作用域加密后的完整凭证 JSON。
		encryptedProof, err := a.encodeDeliveryProof(advance.RunID, *advance.DeliveryProof)
		if err != nil {
			return err
		}
		assignments = "delivery_proof=?," + assignments
		args = append([]any{encryptedProof}, args...)
	}
	args = append(args, advance.RunID, advance.Attempt, advance.Cursor)
	// res、err 保存检查点更新结果及数据库错误。
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET `+assignments+`
		WHERE id=? AND attempt_count=? AND status='running' AND action_cursor=? AND action_started=1`, args...)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// encodeDeliveryProof 将短期发货凭证编码并按自动化运行作用域加密。
func (a *AutomationRules) encodeDeliveryProof(runID int64, proof AutomationDeliveryProof) (string, error) {
	if a == nil || a.codec == nil {
		return "", errors.New("自动化发货凭证缺少加密编解码器")
	}
	if a.codec.currentAEAD() == nil {
		return "", errors.New("自动化发货凭证缺少持久化数据密钥")
	}
	// payload 保存稳定的凭证 JSON 结构。
	payload, err := json.Marshal(proof)
	if err != nil {
		return "", fmt.Errorf("编码自动化发货凭证失败: %w", err)
	}
	// encrypted 保存按运行作用域加密后的凭证，禁止新凭证以明文写入数据库。
	encrypted, err := a.codec.encrypt("automation-delivery-proof", fmt.Sprint(runID), string(payload))
	if err != nil {
		return "", fmt.Errorf("加密自动化发货凭证失败: %w", err)
	}
	return encrypted, nil
}

// decodeDeliveryProof 解密并校验自动化运行中的发货凭证。
func (a *AutomationRules) decodeDeliveryProof(runID int64, raw string) (AutomationDeliveryProof, error) {
	if strings.TrimSpace(raw) == "" {
		return AutomationDeliveryProof{}, nil
	}
	// plain 保存按运行作用域解密后的凭证 JSON。
	plain, err := a.codec.decrypt("automation-delivery-proof", fmt.Sprint(runID), raw)
	if err != nil {
		return AutomationDeliveryProof{}, fmt.Errorf("读取自动化发货凭证失败: %w", err)
	}
	// proof 保存解密并解析后的确认发货凭证。
	var proof AutomationDeliveryProof
	// err 保存凭证 JSON 解析错误。
	if err := json.Unmarshal([]byte(plain), &proof); err != nil {
		return AutomationDeliveryProof{}, fmt.Errorf("解析自动化发货凭证失败: %w", err)
	}
	return proof, nil
}
