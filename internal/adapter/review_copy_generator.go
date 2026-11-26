package adapter

import (
	"context"
	"fmt"
	"strings"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// reviewCopyMaxTokens 是好评文案生成的输出上限；文案最多 200 字，留出余量即可。
const reviewCopyMaxTokens = 400

// reviewCopyGenerator 复用全局 AI 配置生成好评文案，实现 automation.ReviewCopyGenerator。
// 只把商品标题与风格、字数要求发给 AI；买家、订单、金额等信息一律不发送。
type reviewCopyGenerator struct {
	// store 提供全局 AI 设置与商品标题查询。
	store *db.Store
}

// newReviewCopyGenerator 构造好评文案生成器；store 为构造期必需依赖。
func newReviewCopyGenerator(store *db.Store) automation.ReviewCopyGenerator {
	return &reviewCopyGenerator{store: store}
}

// Generate 生成一条好评文案；任何失败都返回错误，不返回部分文本。
func (g *reviewCopyGenerator) Generate(ctx context.Context, prompt automation.ReviewCopyPrompt) (string, error) {
	// title 是商品标题；查询失败或商品缺失时退化为通用说法，不阻断生成。
	title := "这件商品"
	// item 是本地商品记录，err 是读取失败原因；失败时只退化标题，不阻断生成。
	if item, err := g.store.Items.Get(ctx, prompt.AccountID, prompt.ItemID); err == nil && item != nil && strings.TrimSpace(item.ItemTitle) != "" {
		title = strings.TrimSpace(item.ItemTitle)
	}
	return engine.GenerateGlobalAIText(ctx, g.store, prompt.AccountID, reviewCopySystemPrompt(prompt), "商品：《"+title+"》", reviewCopyMaxTokens)
}

// reviewCopySystemPrompt 按风格与字数要求拼装系统提示词；附加要求只作为补充，不能放宽“只输出评价正文”的约束。
func reviewCopySystemPrompt(prompt automation.ReviewCopyPrompt) string {
	// style 是风格描述。
	style := "真诚自然"
	switch prompt.Style {
	case "warm":
		style = "热情洋溢"
	case "concise":
		style = "简洁务实"
	}
	// minChars、maxChars 是生效的字数范围。
	minChars, maxChars := prompt.MinChars, prompt.MaxChars
	if maxChars <= 0 {
		maxChars = 50
	}
	if minChars <= 0 || minChars > maxChars {
		minChars = max(maxChars/2, 5)
	}
	// builder 累积提示词。
	var builder strings.Builder
	fmt.Fprintf(&builder, "你是一位刚收到商品的买家，请为下面的商品写一条真实自然的好评，风格：%s，字数 %d 到 %d 字。", style, minChars, maxChars)
	builder.WriteString("只输出评价正文本身，不要引号、标题、编号或任何解释，不要提到“AI”“模板”。")
	// extra 是去除首尾空白后的规则附加要求。
	if extra := strings.TrimSpace(prompt.Extra); extra != "" {
		builder.WriteString("补充要求：" + extra)
	}
	return builder.String()
}
