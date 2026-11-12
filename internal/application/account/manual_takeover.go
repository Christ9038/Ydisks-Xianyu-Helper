package account

import (
	"errors"
	"fmt"
)

// 人工介入暂停范围与数据库取值一致；应用层单独声明以免依赖 internal/db。
const (
	// ManualTakeoverScopeAI 表示人工回复后只暂停外部 Agent 与 AI 生成式回复（默认）。
	ManualTakeoverScopeAI = "ai"
	// ManualTakeoverScopeAll 表示人工回复后暂停该会话的全部自动回复。
	ManualTakeoverScopeAll = "all"
	// ManualTakeoverMaxMinutes 是人工介入暂停窗口允许配置的最大分钟数（24 小时）。
	ManualTakeoverMaxMinutes = 1440
)

// ErrInvalidManualTakeover 是人工介入暂停设置非法时的哨兵错误，HTTP 层映射为 400。
var ErrInvalidManualTakeover = errors.New("人工介入暂停设置无效")

// validateManualTakeoverInput 校验本次请求中显式提供的暂停分钟数与范围；nil 字段表示保持原值，不参与校验。
func validateManualTakeoverInput(minutes *int, scope *string) error {
	if minutes != nil && (*minutes < 0 || *minutes > ManualTakeoverMaxMinutes) {
		return fmt.Errorf("%w: 暂停分钟数必须在 0 到 %d 之间", ErrInvalidManualTakeover, ManualTakeoverMaxMinutes)
	}
	if scope != nil && *scope != ManualTakeoverScopeAI && *scope != ManualTakeoverScopeAll {
		return fmt.Errorf("%w: 未知的暂停范围 %q", ErrInvalidManualTakeover, *scope)
	}
	return nil
}
