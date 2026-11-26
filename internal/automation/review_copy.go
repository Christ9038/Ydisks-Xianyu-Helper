package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	"xianyu-go/internal/db"
)

const (
	// reviewCopySourcePool 表示好评文案只来自固定模板池。
	reviewCopySourcePool = "pool"
	// reviewCopySourceAI 表示好评文案只由全局 AI 配置实时生成。
	reviewCopySourceAI = "ai"
	// reviewCopySourceMixed 表示按 AI 比例在 AI 生成与固定模板池之间随机选择。
	reviewCopySourceMixed = "mixed"
	// reviewCopyRawKey 是任务快照中保存已确定好评文案的键；延迟任务与重试沿用同一条文案。
	reviewCopyRawKey = "review_copy_text"
	// reviewCopyDefaultMaxChars 是 AI 文案缺省的最大字数。
	reviewCopyDefaultMaxChars = 50
	// maxActionJitterSeconds 是动作延迟随机上浮的上限，与应用层校验保持一致。
	maxActionJitterSeconds = 600
)

// ErrReviewCopyUnavailable 表示没有任何可用来源能产生好评文案，运行不得发送半套消息。
var ErrReviewCopyUnavailable = errors.New("好评文案生成失败")

// ReviewCopyPrompt 是生成好评文案时交给 AI 的最小输入；刻意不含买家昵称、订单号、金额等隐私信息。
type ReviewCopyPrompt struct {
	// AccountID 用于读取该账号可用的全局 AI 配置。
	AccountID string
	// ItemID 用于由生成器查询商品标题，标题是唯一发给 AI 的商品信息。
	ItemID string
	// Style 是评价风格：sincere（真诚自然）、warm（热情洋溢）、concise（简洁务实）。
	Style string
	// MinChars 与 MaxChars 是期望的字数范围，单位为字符。
	MinChars, MaxChars int
	// Extra 是规则里填写的附加要求，可为空。
	Extra string
}

// ReviewCopyGenerator 由消费方（自动化中心）定义，生产环境由 adapter 复用全局 AI 配置实现。
type ReviewCopyGenerator interface {
	// Generate 返回一条好评文案；ctx 取消或 AI 失败时返回错误，不得返回部分文本。
	Generate(ctx context.Context, prompt ReviewCopyPrompt) (string, error)
}

// reviewCopyConfig 是 send_review_copy 动作 config_json 中与文案来源相关的配置。
type reviewCopyConfig struct {
	// Source 是文案来源，取值 pool、ai 或 mixed。
	Source string
	// Pool 是固定模板池。
	Pool []string
	// Random 表示模板池随机选取；false 时按规则运行次数顺序循环。
	Random bool
	// AIRatio 是混合模式下使用 AI 的概率百分比。
	AIRatio int
	// AIStyle、AIMinChars、AIMaxChars、AIPrompt 是交给 AI 的风格、字数和附加要求。
	AIStyle    string
	AIMinChars int
	AIMaxChars int
	AIPrompt   string
}

// parseReviewCopyConfig 解析动作配置；无法解析时按空模板池处理，由调用方报告“无可用来源”。
func parseReviewCopyConfig(raw string) reviewCopyConfig {
	// wire 对应 config_json 的字段；Random 用指针区分“未配置”（默认随机）与显式 false。
	var wire struct {
		Source     string   `json:"source"`
		Pool       []string `json:"pool"`
		Random     *bool    `json:"random"`
		AIRatio    int      `json:"ai_ratio"`
		AIStyle    string   `json:"ai_style"`
		AIMinChars int      `json:"ai_min_chars"`
		AIMaxChars int      `json:"ai_max_chars"`
		AIPrompt   string   `json:"ai_prompt"`
	}
	_ = json.Unmarshal([]byte(raw), &wire)
	// cfg 保存清洗后的配置，空白模板被丢弃。
	cfg := reviewCopyConfig{Source: strings.TrimSpace(wire.Source), Random: wire.Random == nil || *wire.Random,
		AIRatio: wire.AIRatio, AIStyle: strings.TrimSpace(wire.AIStyle), AIMinChars: wire.AIMinChars,
		AIMaxChars: wire.AIMaxChars, AIPrompt: strings.TrimSpace(wire.AIPrompt)}
	for /* item 表示配置中的一条模板。 */ _, item := range wire.Pool {
		if text := strings.TrimSpace(item); text != "" {
			cfg.Pool = append(cfg.Pool, text)
		}
	}
	if cfg.AIMaxChars <= 0 {
		cfg.AIMaxChars = reviewCopyDefaultMaxChars
	}
	return cfg
}

// sanitizeReviewCopy 清理 AI 输出：去掉首尾空白与成对引号，并按最大字数裁剪；清理后为空视为失败。
func sanitizeReviewCopy(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "\"“”'‘’「」")
	text = strings.TrimSpace(text)
	if maxChars > 0 {
		// runes 按字符而非字节裁剪，避免截断中文。
		if runes := []rune(text); len(runes) > maxChars {
			text = string(runes[:maxChars])
		}
	}
	return text
}

// pickReviewCopyFromPool 从模板池取一条：随机模式用 intn，顺序模式按 position 循环。
func pickReviewCopyFromPool(cfg reviewCopyConfig, position int, intn func(int) int) (string, bool) {
	if len(cfg.Pool) == 0 {
		return "", false
	}
	if cfg.Random {
		return cfg.Pool[intn(len(cfg.Pool))], true
	}
	if position < 0 {
		position = 0
	}
	return cfg.Pool[position%len(cfg.Pool)], true
}

// resolveReviewCopyText 按来源产出一条好评文案。position 是顺序循环位置；intn 是可注入随机源；
// generate 调用 AI，返回的文本会被清理，清理后为空视为失败。
func resolveReviewCopyText(ctx context.Context, cfg reviewCopyConfig, position int, intn func(int) int, generate func(context.Context) (string, error)) (string, error) {
	// tryAI 调用 AI 并清理结果，generate 为空表示没有装配生成器。
	tryAI := func() (string, error) {
		if generate == nil {
			return "", errors.New("未装配 AI 文案生成器")
		}
		// text 是生成器返回的原始文案；err 是生成失败原因。
		text, err := generate(ctx)
		if err != nil {
			return "", err
		}
		// cleaned 是清理引号并按最大字数裁剪后的文案，空串视为失败。
		if cleaned := sanitizeReviewCopy(text, cfg.AIMaxChars); cleaned != "" {
			return cleaned, nil
		}
		return "", errors.New("AI 返回内容为空")
	}
	switch cfg.Source {
	case reviewCopySourcePool:
		// text 是模板池选出的文案；ok 表示模板池非空。
		if text, ok := pickReviewCopyFromPool(cfg, position, intn); ok {
			return text, nil
		}
		return "", fmt.Errorf("%w: 模板池为空", ErrReviewCopyUnavailable)
	case reviewCopySourceAI:
		// text 是 AI 生成的文案；err 是 AI 失败原因。
		text, err := tryAI()
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrReviewCopyUnavailable, err)
		}
		return text, nil
	case reviewCopySourceMixed:
		if intn(100) < cfg.AIRatio {
			// text 是 AI 生成的文案；err 是 AI 失败原因，失败时回退模板池。
			text, err := tryAI()
			if err == nil {
				return text, nil
			}
			// AI 失败时回退模板池；两者都不可用才报错。
			if fallback, ok := pickReviewCopyFromPool(cfg, position, intn); ok {
				return fallback, nil
			}
			return "", fmt.Errorf("%w: %w", ErrReviewCopyUnavailable, err)
		}
		// text 是模板池选出的文案；ok 表示模板池非空。
		if text, ok := pickReviewCopyFromPool(cfg, position, intn); ok {
			return text, nil
		}
		// text 是模板池为空时改用 AI 生成的文案；err 是 AI 失败原因。
		text, err := tryAI()
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrReviewCopyUnavailable, err)
		}
		return text, nil
	default:
		return "", fmt.Errorf("%w: 未知文案来源 %q", ErrReviewCopyUnavailable, cfg.Source)
	}
}

// resolveReviewCopy 为一次求评价运行确定好评文案：规则内的模板池位置按该规则已创建的运行数推算。
func (c *Center) resolveReviewCopy(ctx context.Context, ruleID int64, task Task, action db.AutomationAction) (string, error) {
	// cfg 是该动作的文案来源配置。
	cfg := parseReviewCopyConfig(action.ConfigJSON)
	// position 是顺序循环位置；仅在非随机的模板池或混合模式下需要读库。
	position := 0
	if !cfg.Random && cfg.Source != reviewCopySourceAI {
		// total 是该规则已创建的运行数量；err 是读库失败原因。
		total, err := c.store.Automation.CountRunsForRule(ctx, ruleID)
		if err != nil {
			return "", fmt.Errorf("读取好评文案轮换位置: %w", err)
		}
		position = max(total-1, 0)
	}
	// generator 是构造期固定的 AI 生成器，可能为空。
	generator := c.dependencies.reviewCopyGenerator
	// generate 把提示词交给生成器；没有生成器时保持 nil，让纯模板池规则不受影响。
	var generate func(context.Context) (string, error)
	if generator != nil {
		generate = func(callCtx context.Context) (string, error) {
			return generator.Generate(callCtx, ReviewCopyPrompt{AccountID: task.AccountID, ItemID: task.ItemID, Style: cfg.AIStyle,
				MinChars: cfg.AIMinChars, MaxChars: cfg.AIMaxChars, Extra: cfg.AIPrompt})
		}
	}
	return resolveReviewCopyText(ctx, cfg, position, rand.IntN, generate)
}

// actionJitterSeconds 读取动作 config_json 中的 delay_jitter_seconds 并在 [0, jitter] 内取随机值。
// 仅对文本类动作生效；非法或越界值按 0 / 上限处理，intn 是可注入随机源。
func actionJitterSeconds(action db.AutomationAction, intn func(int) int) int {
	if action.ActionType != ActionSendText && action.ActionType != ActionSendReviewCopy {
		return 0
	}
	// wire 只取抖动字段。
	var wire struct {
		Jitter int `json:"delay_jitter_seconds"`
	}
	if json.Unmarshal([]byte(action.ConfigJSON), &wire) != nil || wire.Jitter <= 0 {
		return 0
	}
	return intn(min(wire.Jitter, maxActionJitterSeconds) + 1)
}

// rawReviewCopy 读取任务快照中已确定的好评文案。
func rawReviewCopy(task Task) string {
	if task.Raw == nil {
		return ""
	}
	// text 是任务快照中保存的好评文案，类型不符时为空串。
	text, _ := task.Raw[reviewCopyRawKey].(string)
	return text
}
