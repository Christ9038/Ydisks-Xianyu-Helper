package db

import (
	"context"
	"database/sql"
	"errors"
)

// 人工介入暂停范围决定会话在人工回复后的暂停窗口内跳过哪些自动回复来源。
const (
	// ManualTakeoverScopeAI 只暂停外部 Agent 与 AI 这类生成式回复；关键词、默认回复和商品首次回复照常。这是新账号默认值。
	ManualTakeoverScopeAI = "ai"
	// ManualTakeoverScopeAll 暂停该会话的全部自动回复，包括商品首次回复、关键词、Agent、AI 和默认回复。
	ManualTakeoverScopeAll = "all"
)

// MaxManualTakeoverMinutes 是人工介入暂停窗口允许配置的最大分钟数（24 小时），与引擎内存记录的保留时长一致。
const MaxManualTakeoverMinutes = 1440

// ManualTakeoverSetting 是账号级人工介入暂停配置；只含非敏感列，不触碰 Cookie、Token 或密码。
type ManualTakeoverSetting struct {
	// Minutes 是人工回复后该会话暂停自动回复的分钟数；0 表示关闭该功能。
	Minutes int
	// Scope 是 ManualTakeoverScope* 之一；数据库中的未知值按 ai 兼容处理。
	Scope string
}

// NormalizeManualTakeoverScope 把空值或未知历史值归一为 ai，避免手工改库让暂停范围意外扩大到全部自动回复。
func NormalizeManualTakeoverScope(scope string) string {
	if scope == ManualTakeoverScopeAll {
		return ManualTakeoverScopeAll
	}
	return ManualTakeoverScopeAI
}

// normalizeManualTakeoverMinutes 把越界的历史分钟数夹到 [0, MaxManualTakeoverMinutes]，负数视为关闭。
func normalizeManualTakeoverMinutes(minutes int) int {
	if minutes < 0 {
		return 0
	}
	if minutes > MaxManualTakeoverMinutes {
		return MaxManualTakeoverMinutes
	}
	return minutes
}

// GetManualTakeoverSetting 读取账号人工介入暂停配置；只读取两个非敏感列。账号不存在时返回 ErrNotFound。
func (c *Cookies) GetManualTakeoverSetting(ctx context.Context, cookieID string) (ManualTakeoverSetting, error) {
	// setting 保存数据库中的暂停分钟数与范围。
	var setting ManualTakeoverSetting
	// err 保存查询账号配置时的数据库错误。
	err := c.DB.QueryRowContext(ctx, `SELECT COALESCE(manual_takeover_minutes,0),COALESCE(manual_takeover_scope,'') FROM cookies WHERE id=?`, cookieID).Scan(&setting.Minutes, &setting.Scope)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ManualTakeoverSetting{}, ErrNotFound
		}
		return ManualTakeoverSetting{}, err
	}
	setting.Minutes = normalizeManualTakeoverMinutes(setting.Minutes)
	setting.Scope = NormalizeManualTakeoverScope(setting.Scope)
	return setting, nil
}

// appendManualTakeoverAssignments 把可选的暂停分钟数与范围加入账号设置 UPDATE 赋值；nil 字段保持原值。
// 输入合法性由应用层校验，这里仍做归一化，保证直接调用仓储也不会写入越界值。
func appendManualTakeoverAssignments(assignments []string, args []any, input AccountSettingsUpdate) ([]string, []any) {
	if input.ManualTakeoverMinutes != nil {
		assignments = append(assignments, "manual_takeover_minutes=?")
		args = append(args, normalizeManualTakeoverMinutes(*input.ManualTakeoverMinutes))
	}
	if input.ManualTakeoverScope != nil {
		assignments = append(assignments, "manual_takeover_scope=?")
		args = append(args, NormalizeManualTakeoverScope(*input.ManualTakeoverScope))
	}
	return assignments, args
}
