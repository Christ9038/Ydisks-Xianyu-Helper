package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// 确认发货凭证模式决定调用平台确认发货接口时 tradeText/picList 携带什么内容。
const (
	// ConsignProofModeDelivery 携带买家实际收到的卡密或发货模板全文及图片；这是迁移前的历史行为，也是新账号默认值。
	ConsignProofModeDelivery = "delivery"
	// ConsignProofModeFixed 只携带账号设置中的固定文字，不携带卡密、模板全文和图片，用于规避平台敏感词审核。
	ConsignProofModeFixed = "fixed"
	// ConsignProofModeNone 不携带任何凭证，tradeText 为空、picList 为空数组。
	ConsignProofModeNone = "none"
)

// ErrConsignProofTextRequired 表示账号最终会处于“固定文字”模式但固定文字为空；该状态会让确认发货静默退化为不携带凭证，因此拒绝保存。
var ErrConsignProofTextRequired = errors.New("确认发货凭证选择固定文字时，固定文字不能为空")

// ConsignProofSetting 是账号确认发货凭证的非敏感配置；固定文字由用户填写，不含卡密或平台凭证。
type ConsignProofSetting struct {
	// Mode 是 ConsignProofMode* 之一；数据库中的未知值由读取方按 delivery 兼容处理。
	Mode string
	// FixedText 是固定文字模式下提交给平台的 tradeText；其他模式保留但不使用。
	FixedText string
}

// NormalizeConsignProofMode 把空值或未知历史值归一为 delivery，保证旧数据和手工改库不会让确认发货意外丢失凭证语义。
func NormalizeConsignProofMode(mode string) string {
	switch mode {
	case ConsignProofModeFixed, ConsignProofModeNone:
		return mode
	default:
		return ConsignProofModeDelivery
	}
}

// GetConsignProofSetting 读取账号确认发货凭证配置；只读取两个非敏感列，不触碰 Cookie、Token 或密码。
func (c *Cookies) GetConsignProofSetting(ctx context.Context, cookieID string) (ConsignProofSetting, error) {
	// setting 保存数据库中的模式与固定文字。
	var setting ConsignProofSetting
	// err 保存查询账号配置时的数据库错误；账号不存在映射为 ErrNotFound。
	err := c.DB.QueryRowContext(ctx, `SELECT consign_proof_mode,consign_proof_text FROM cookies WHERE id=?`, cookieID).Scan(&setting.Mode, &setting.FixedText)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConsignProofSetting{}, ErrNotFound
		}
		return ConsignProofSetting{}, err
	}
	setting.Mode = NormalizeConsignProofMode(setting.Mode)
	return setting, nil
}

// appendConsignProofAssignments 把可选的凭证模式与固定文字加入 UPDATE 赋值，并在同一事务内校验写入后的最终状态。
// currentMode、currentText 是 FOR UPDATE 读取的当前值；只有本次实际修改其中任一字段时才校验，避免历史数据阻塞无关设置的保存。
func appendConsignProofAssignments(assignments []string, args []any, input AccountSettingsUpdate, currentMode, currentText string) ([]string, []any, error) {
	if input.ConsignProofMode == nil && input.ConsignProofText == nil {
		return assignments, args, nil
	}
	// finalMode、finalText 是本次保存成功后账号将处于的凭证配置。
	finalMode, finalText := NormalizeConsignProofMode(currentMode), currentText
	if input.ConsignProofMode != nil {
		finalMode = NormalizeConsignProofMode(*input.ConsignProofMode)
		assignments = append(assignments, "consign_proof_mode=?")
		args = append(args, finalMode)
	}
	if input.ConsignProofText != nil {
		finalText = *input.ConsignProofText
		assignments = append(assignments, "consign_proof_text=?")
		args = append(args, finalText)
	}
	if finalMode == ConsignProofModeFixed && strings.TrimSpace(finalText) == "" {
		return assignments, args, ErrConsignProofTextRequired
	}
	return assignments, args, nil
}
