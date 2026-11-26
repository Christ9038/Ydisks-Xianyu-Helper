package automation

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	// maxReviewCopyPool 是固定好评模板池的数量上限。
	maxReviewCopyPool = 100
	// maxReviewCopyLength 是单条好评模板的字符上限。
	maxReviewCopyLength = 500
	// maxActionJitterSeconds 是动作延迟随机上浮的上限秒数。
	maxActionJitterSeconds = 600
)

// validateReviewCopyConfig 校验好评文案动作的 config_json：来源、模板池、AI 比例与字数范围。
// 不检查全局 AI 是否已配置，因为那属于运行期环境，缺失时执行阶段会明确失败并进入重试。
func validateReviewCopyConfig(configJSON string) error {
	// wire 保存需要校验的字段；ratio 与字数用指针区分未提供。
	var wire struct {
		Source     string   `json:"source"`
		Pool       []string `json:"pool"`
		AIRatio    *int     `json:"ai_ratio"`
		AIMinChars int      `json:"ai_min_chars"`
		AIMaxChars int      `json:"ai_max_chars"`
		AIStyle    string   `json:"ai_style"`
	}
	// err 是好评文案配置 JSON 解码错误。
	if err := json.Unmarshal([]byte(configJSON), &wire); err != nil {
		return errors.New("好评文案配置格式无效")
	}
	// poolCount 是去除空白条目后的有效模板数。
	poolCount := 0
	for /* item 表示一条模板。 */ _, item := range wire.Pool {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > maxReviewCopyLength {
			return errors.New("单条好评模板不能超过 500 个字符")
		}
		poolCount++
	}
	if poolCount > maxReviewCopyPool {
		return errors.New("好评模板最多 100 条")
	}
	switch wire.Source {
	case "pool":
		if poolCount == 0 {
			return errors.New("固定模板来源至少需要一条好评模板")
		}
	case "ai":
	case "mixed":
		if poolCount == 0 {
			return errors.New("混合来源至少需要一条好评模板作为兜底")
		}
		if wire.AIRatio == nil || *wire.AIRatio < 0 || *wire.AIRatio > 100 {
			return errors.New("AI 使用比例必须在 0 到 100 之间")
		}
	default:
		return errors.New("好评文案来源必须是 pool、ai 或 mixed")
	}
	if wire.Source != "pool" {
		if wire.AIMinChars != 0 && (wire.AIMinChars < 5 || wire.AIMinChars > 200) {
			return errors.New("AI 文案最少字数必须在 5 到 200 之间")
		}
		if wire.AIMaxChars != 0 && (wire.AIMaxChars < 5 || wire.AIMaxChars > 200) {
			return errors.New("AI 文案最多字数必须在 5 到 200 之间")
		}
		if wire.AIMinChars > 0 && wire.AIMaxChars > 0 && wire.AIMinChars > wire.AIMaxChars {
			return errors.New("AI 文案最少字数不能大于最多字数")
		}
		switch wire.AIStyle {
		case "", "sincere", "warm", "concise":
		default:
			return errors.New("不支持的 AI 评价风格")
		}
	}
	return nil
}

// validateActionJitter 校验文本类动作 config_json 中的 delay_jitter_seconds：0 到 600 秒，且与固定延迟之和不超过 3600 秒。
func validateActionJitter(action ActionDraft) error {
	// wire 只取抖动字段。
	var wire struct {
		Jitter int `json:"delay_jitter_seconds"`
	}
	if json.Unmarshal([]byte(action.ConfigJSON), &wire) != nil || wire.Jitter == 0 {
		return nil
	}
	if action.ActionType != ActionSendText && action.ActionType != ActionSendReviewCopy {
		return errors.New("随机延迟只能用于文本类动作")
	}
	if wire.Jitter < 0 || wire.Jitter > maxActionJitterSeconds {
		return errors.New("随机延迟必须在 0 到 600 秒之间")
	}
	if action.DelaySeconds+wire.Jitter > 3600 {
		return errors.New("动作延时与随机延迟之和不能超过 3600 秒")
	}
	return nil
}
