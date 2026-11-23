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
	// ReplyMergeMinSeconds 与 ReplyMergeMaxSeconds 是买家分段消息“合并等待”秒数允许配置的闭区间，与数据库归一化范围一致。
	ReplyMergeMinSeconds = 1
	// ReplyMergeMaxSeconds 是合并等待秒数的上限。
	ReplyMergeMaxSeconds = 10
)

// ErrInvalidManualTakeover 是人工介入暂停设置非法时的哨兵错误，HTTP 层映射为 400。
var ErrInvalidManualTakeover = errors.New("人工介入暂停设置无效")

// ErrInvalidReplyMerge 是合并等待秒数非法时的哨兵错误，HTTP 层映射为 400。
var ErrInvalidReplyMerge = errors.New("合并等待设置无效")

// validateReplyMergeInput 校验本次请求中显式提供的合并等待秒数；nil 表示保持原值，不参与校验。
func validateReplyMergeInput(seconds *int) error {
	if seconds != nil && (*seconds < ReplyMergeMinSeconds || *seconds > ReplyMergeMaxSeconds) {
		return fmt.Errorf("%w: 合并等待秒数必须在 %d 到 %d 之间", ErrInvalidReplyMerge, ReplyMergeMinSeconds, ReplyMergeMaxSeconds)
	}
	return nil
}

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
