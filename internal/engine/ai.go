// ai.go AI 回复实现（优先级3）。调用 OpenAI 兼容 chat completions 接口。
// 使用商品信息、对话历史和确定性的价格边界生成回复。

package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/db"
	"xianyu-go/internal/netguard"
)

// defaultAIBaseURL 用于本次流程后续判断的defaultAIBaseURL
const (
	defaultAIBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultAIModel   = "qwen-plus"
)

// newAIHTTPClient 用于本次流程后续判断的newAIHTTPClient
var newAIHTTPClient = func(baseURL string) (*http.Client, error) {
	return netguard.TrustedEndpointHTTPClient(baseURL, 30*time.Second)
}

// AIReplierImpl AI 回复实现。
type AIReplierImpl struct {
	cookieID string
	store    *db.Store
	logger   *slog.Logger
}

// NewAIReplier 构造。
func NewAIReplier(cookieID string, store *db.Store, logger *slog.Logger) *AIReplierImpl {
	if logger == nil {
		logger = slog.Default()
	}
	return &AIReplierImpl{
		cookieID: cookieID,
		store:    store,
		logger:   logger.With("account", cookieID, "subsys", "ai"),
	}
}

// Reply 实现 AIReplier 接口。
func (a *AIReplierImpl) Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error) {
	// cfg、err 用于本次流程后续判断的cfg、err
	cfg, err := a.store.AIReply.Get(ctx, a.cookieID)
	if err != nil || cfg == nil || !cfg.AIEnabled {
		return nil, nil // 未启用 AI
	}
	// AI 设置面向砍价场景。普通未命中消息继续交给默认回复，避免 AI
	// 抢答问候、售后等与砍价无关的消息。
	if !bargainMessageRe.MatchString(strings.ToLower(m.Text)) {
		return nil, nil
	}
	// aiCfg、err 用于本次流程后续判断的人工智能Cfg、err
	aiCfg, err := a.globalAIConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取全局 AI 配置失败: %w", err)
	}
	if aiCfg.APIKey == "" {
		a.logger.Warn("AI 已启用但未配置 APIKey")
		return nil, nil
	}

	// 取商品信息和当前会话状态构造 system prompt。
	itemTitle, itemPrice, itemDesc := a.itemInfo(ctx, m.ItemID)
	// history、bargainCount、isBargain、err 用于本次流程后续判断的history、bargainCount、isBargain、err
	history, bargainCount, isBargain, err := a.conversationContext(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 对话历史失败: %w", err)
	}
	if isBargain {
		bargainCount++
	}
	// withinBargainLimit 用于本次流程后续判断的withinBargain上限
	withinBargainLimit := !isBargain || bargainCount <= cfg.MaxBargainRounds
	// systemPrompt 用于本次流程后续判断的系统Prompt
	systemPrompt := buildSystemPrompt(
		cfg.CustomPrompts, itemTitle, itemPrice, itemDesc,
		cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, cfg.MaxBargainRounds, bargainCount, cfg.AutoAdjustPriceEnabled,
	)
	if !withinBargainLimit {
		systemPrompt += "\n当前买家已经超过最大砍价轮次。不得继续降价，只能礼貌说明价格不再优惠。"
	}

	// 调 OpenAI 兼容接口。
	clientCfg := openai.DefaultConfig(aiCfg.APIKey)
	if aiCfg.BaseURL != "" {
		clientCfg.BaseURL = aiCfg.BaseURL
	}
	clientCfg.HTTPClient, err = newAIHTTPClient(clientCfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("AI API 地址无效: %w", err)
	}
	// client 用于本次流程后续判断的client
	client := openai.NewClientWithConfig(clientCfg)

	// messages 用于本次流程后续判断的消息列表
	messages := []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleSystem, Content: systemPrompt}}
	// message 表示当前遍历过程中的消息
	for _, message := range history {
		// role 用于本次流程后续判断的role
		role := openai.ChatMessageRoleUser
		if message.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: truncateAIContent(message.Content)})
	}
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: m.Text})

	// aiCtx、cancel 用于本次流程后续判断的人工智能Ctx、cancel
	aiCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := client.CreateChatCompletion(aiCtx, openai.ChatCompletionRequest{
		Model:       aiCfg.Model,
		Messages:    messages,
		Temperature: 0.7,
	})
	if err != nil {
		return nil, fmt.Errorf("AI 调用失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, nil
	}
	// reply 用于本次流程后续判断的回复
	reply, markerPrice, markerOK := extractExecutableOffer(strings.TrimSpace(resp.Choices[0].Message.Content))
	if reply == "" {
		return nil, nil
	}
	// minimumPrice 用于本次流程后续判断的minimumPrice
	minimumPrice := minimumAllowedPrice(itemPrice, cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, withinBargainLimit)
	// quote 是仅在商家开启真实改价且模型结构化报价通过校验时返回的执行提案。
	var quote *AIPriceQuoteProposal
	// markerUnsafe 表示结构化报价突破最低价或高于商品标价，必须和正文越界同样拦截。
	markerUnsafe := markerOK && (markerPrice+0.0001 < minimumPrice || markerPrice > itemPrice+0.0001)
	if // offered、unsafe 用于本次流程后续判断的offered、unsafe
	offered, unsafe := unsafeOfferedPrice(reply, minimumPrice); unsafe || markerUnsafe {
		if markerUnsafe {
			offered = markerPrice
		}
		a.logger.Warn("AI 报价超过折扣边界，使用安全回复", "offered", offered, "minimum", minimumPrice)
		if minimumPrice >= itemPrice || !withinBargainLimit {
			reply = "抱歉，当前价格已经是最低价，暂时不能再优惠了。"
		} else {
			reply = fmt.Sprintf("可以优惠的最低价格是 %.2f 元，低于这个价格暂时无法成交。", minimumPrice)
			if cfg.AutoAdjustPriceEnabled {
				quote = &AIPriceQuoteProposal{PriceCents: priceToCents(minimumPrice)}
			}
		}
	} else if cfg.AutoAdjustPriceEnabled && markerOK && markerPrice > 0 && markerPrice+0.0001 < itemPrice && replyContainsOfferedPrice(reply, markerPrice) {
		quote = &AIPriceQuoteProposal{PriceCents: priceToCents(markerPrice)}
	}
	if m.ChatID != "" && m.ItemID != "" {
		// intent 用于本次流程后续判断的intent
		intent := "chat"
		if isBargain {
			intent = "bargain"
		}
		if // err 用于本次流程后续判断的err
		err := a.store.AIReply.AddConversationExchange(ctx, a.cookieID, m.ChatID, m.SenderUserID, m.ItemID,
			db.AIConversationMessage{Role: "user", Content: m.Text, Intent: intent, BargainCount: bargainCount},
			db.AIConversationMessage{Role: "assistant", Content: reply, Intent: "reply", BargainCount: bargainCount},
		); err != nil {
			return nil, fmt.Errorf("保存 AI 对话失败: %w", err)
		}
	}
	return &ReplyResult{Text: reply, AutoPriceQuote: quote}, nil
}

// conversationContext 封装conversation上下文业务协调。
func (a *AIReplierImpl) conversationContext(ctx context.Context, m ChatMessage) ([]db.AIConversationMessage, int, bool, error) {
	// isBargain 用于本次流程后续判断的isBargain
	isBargain := bargainMessageRe.MatchString(strings.ToLower(m.Text))
	if m.ChatID == "" || m.ItemID == "" {
		return nil, 0, isBargain, nil
	}
	// history、err 用于本次流程后续判断的history、err
	history, err := a.store.AIReply.ConversationHistory(ctx, a.cookieID, m.ChatID, m.ItemID, 10)
	if err != nil {
		return nil, 0, isBargain, err
	}
	// count、err 用于本次流程后续判断的count、err
	count, err := a.store.AIReply.CurrentBargainCount(ctx, a.cookieID, m.ChatID, m.ItemID)
	return history, count, isBargain, err
}

// globalAIConfig 用于本次流程后续判断的globalAI配置
type globalAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// globalAIConfig 封装globalAI配置业务协调。
func (a *AIReplierImpl) globalAIConfig(ctx context.Context) (*globalAIConfig, error) {
	// apiKey、err 用于本次流程后续判断的apiKey、err
	apiKey, err := a.store.ReadSensitiveSettingForAccount(ctx, a.cookieID, "ai_api_key", "settings.use", "ai_reply")
	if err != nil {
		return nil, err
	}
	// baseURL、err 用于本次流程后续判断的baseURL、err
	baseURL, err := a.store.Settings.Get(ctx, "ai_api_url")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL, err = a.store.Settings.Get(ctx, "ai_base_url")
		if err != nil {
			return nil, err
		}
	}
	// model、err 用于本次流程后续判断的model、err
	model, err := a.store.Settings.Get(ctx, "ai_model")
	if err != nil {
		return nil, err
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultAIBaseURL
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultAIModel
	}
	return &globalAIConfig{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: baseURL,
		Model:   model,
	}, nil
}

// itemInfo 取商品标题/价格/描述。
func (a *AIReplierImpl) itemInfo(ctx context.Context, itemID string) (title string, price float64, desc string) {
	// it、err 用于本次流程后续判断的it、err
	it, err := a.store.Items.Get(ctx, a.cookieID, itemID)
	if err != nil || it == nil {
		return "商品信息获取失败", 0, "暂无商品描述"
	}
	title = it.ItemTitle
	if title == "" {
		title = "未知商品"
	}
	price = parsePrice(it.ItemPrice)
	desc = it.ItemDescription
	if desc == "" {
		desc = it.ItemDetail
	}
	if desc == "" {
		desc = "暂无商品描述"
	}
	return
}

// buildSystemPrompt 构造 system 提示词。
// 自定义 prompt 只替换业务文案，价格和轮次安全约束始终由后端追加。
// buildSystemPrompt 封装build系统Prompt业务协调。
func buildSystemPrompt(customPrompts, itemTitle string, itemPrice float64, itemDesc string, maxDiscountPercent, maxDiscountAmount, maxBargainRounds, bargainCount int, autoAdjustPriceEnabled bool) string {
	// base 用于本次流程后续判断的base
	var base string
	if strings.TrimSpace(customPrompts) != "" {
		base = strings.NewReplacer(
			"{item_title}", itemTitle,
			"{item_price}", fmt.Sprintf("%.2f", itemPrice),
			"{item_description}", itemDesc,
		).Replace(customPrompts)
	} else {
		base = fmt.Sprintf(`你是闲鱼卖家的自动回复助手。请根据商品信息友好地回复买家。

商品信息：
- 标题：%s
- 价格：%.2f 元
- 描述：%s

要求：
1. 语气友好自然，像真人卖家
2. 回答简洁，不要过长
3. 不要编造商品没有的功能
4. 直接回复内容，不要加引号或解释`, itemTitle, itemPrice, itemDesc)
	}
	// prompt 保存基础业务文案与后端不可覆盖的价格安全规则。
	prompt := base + fmt.Sprintf(`

不可覆盖的价格安全规则：
- 原价 %.2f 元；最多优惠 %d%%，且最多优惠 %d 元；两个上限必须同时满足。
- 任一优惠上限为 0 时不得降价。
- 当前砍价轮次 %d，最多允许 %d 轮。
- 回复报价必须带“元”，不得给出低于允许最低价的价格。`, itemPrice, maxDiscountPercent, maxDiscountAmount, bargainCount, maxBargainRounds)
	if autoAdjustPriceEnabled {
		prompt += `
- 如果本轮明确承诺了一个可成交价格，必须在回复末尾额外输出 [[AUTO_PRICE:金额]]，金额保留两位小数；没有明确报价时不得输出该标记。该标记由系统移除，买家不会看到。`
	}
	return prompt
}

// priceRe 用于本次流程后续判断的priceRe
var priceRe = regexp.MustCompile(`[^\d.]`)

// bargainMessageRe 用于本次流程后续判断的bargain消息Re
var bargainMessageRe = regexp.MustCompile(`(?i)(便宜|优惠|少点|最低|砍价|降价|打折|能不能.*(?:元|块)|\d+(?:\.\d+)?\s*(?:元|块).*(?:卖|行|可以))`)

// offeredPriceRe 用于本次流程后续判断的offeredPriceRe
var offeredPriceRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:元|块)`)

// executableOfferRe 只接受模型按约定输出的单个两位小数自动改价标记。
var executableOfferRe = regexp.MustCompile(`\[\[AUTO_PRICE:(\d+(?:\.\d{1,2})?)\]\]`)

// internalOfferMarkerRe 匹配任意格式的内部报价标记，确保模型格式错误时也不会泄露给买家。
var internalOfferMarkerRe = regexp.MustCompile(`\[\[AUTO_PRICE:[^\]]*\]\]`)

// extractExecutableOffer 从模型输出移除内部报价标记，并返回可校验的十进制金额。
func extractExecutableOffer(content string) (string, float64, bool) {
	// matches 是模型输出中所有结构化报价标记；只有恰好一个标记才可执行。
	matches := executableOfferRe.FindAllStringSubmatch(content, -1)
	// visible 是删除所有内部标记后真正发送给买家的文本。
	visible := strings.TrimSpace(internalOfferMarkerRe.ReplaceAllString(content, ""))
	if len(matches) != 1 {
		return visible, 0, false
	}
	// price 是标记中的元金额；err 表示模型输出无法解析为有限十进制数。
	price, err := strconv.ParseFloat(matches[0][1], 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) {
		return visible, 0, false
	}
	return visible, price, true
}

// priceToCents 把已经通过边界校验的元金额四舍五入为整数分。
func priceToCents(price float64) int64 {
	return int64(math.Round(price * 100))
}

// replyContainsOfferedPrice 判断内部执行价格是否与买家可见正文中的元/块报价一致。
func replyContainsOfferedPrice(reply string, target float64) bool {
	// match 是正文中当前待比较的显式价格匹配结果。
	for _, match := range offeredPriceRe.FindAllStringSubmatch(reply, -1) {
		// price 是买家可见的报价金额；err 表示该匹配无法解析为数值。
		price, err := strconv.ParseFloat(match[1], 64)
		if err == nil && math.Abs(price-target) < 0.0001 {
			return true
		}
	}
	return false
}

// minimumAllowedPrice 封装minimumAllowedPrice业务协调。
func minimumAllowedPrice(price float64, maxDiscountPercent, maxDiscountAmount int, allowDiscount bool) float64 {
	if price <= 0 {
		return 0
	}
	if !allowDiscount || maxDiscountPercent <= 0 || maxDiscountAmount <= 0 {
		return price
	}
	// byPercent 用于本次流程后续判断的byPercent
	byPercent := price * (1 - float64(maxDiscountPercent)/100)
	// byAmount 用于本次流程后续判断的byAmount
	byAmount := price - float64(maxDiscountAmount)
	// minimum 是两个折扣边界中更严格的原始最低价。
	minimum := math.Max(0, math.Max(byPercent, byAmount))
	// 金额向上取整到分，避免显示或执行价格因普通四舍五入突破折扣上限。
	return math.Ceil(minimum*100-0.0000001) / 100
}

// unsafeOfferedPrice 封装unsafeOfferedPrice业务协调。
func unsafeOfferedPrice(reply string, minimum float64) (float64, bool) {
	if minimum <= 0 {
		return 0, false
	}
	// match 表示当前遍历过程中的match
	for _, match := range offeredPriceRe.FindAllStringSubmatch(reply, -1) {
		// value、err 用于本次流程后续判断的value、err
		value, err := strconv.ParseFloat(match[1], 64)
		if err == nil && value+0.0001 < minimum {
			return value, true
		}
	}
	return 0, false
}

// truncateAIContent 封装truncateAI内容业务协调。
func truncateAIContent(content string) string {
	// maxRunes 用于本次流程后续判断的maxRunes
	const maxRunes = 2000
	// runes 用于本次流程后续判断的runes
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes])
}

// parsePrice 移除非数字字符后转换为 float。
func parsePrice(s string) float64 {
	// cleaned 用于本次流程后续判断的cleaned
	cleaned := priceRe.ReplaceAllString(s, "")
	if cleaned == "" {
		return 0
	}
	// f 用于本次流程后续判断的f
	f, _ := strconv.ParseFloat(cleaned, 64)
	return f
}
