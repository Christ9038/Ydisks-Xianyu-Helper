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

const (
	defaultAIBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultAIModel   = "qwen-plus"
)

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
	cfg, err := a.store.AIReply.Get(ctx, a.cookieID)
	if err != nil || cfg == nil || !cfg.AIEnabled {
		return nil, nil // 未启用 AI
	}
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
	history, bargainCount, isBargain, err := a.conversationContext(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 对话历史失败: %w", err)
	}
	if isBargain {
		bargainCount++
	}
	withinBargainLimit := !isBargain || bargainCount <= cfg.MaxBargainRounds
	systemPrompt := buildSystemPrompt(
		cfg.CustomPrompts, itemTitle, itemPrice, itemDesc,
		cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, cfg.MaxBargainRounds, bargainCount,
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
	client := openai.NewClientWithConfig(clientCfg)

	messages := []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleSystem, Content: systemPrompt}}
	for _, message := range history {
		role := openai.ChatMessageRoleUser
		if message.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: truncateAIContent(message.Content)})
	}
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: m.Text})

	aiCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
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
	reply := strings.TrimSpace(resp.Choices[0].Message.Content)
	if reply == "" {
		return nil, nil
	}
	minimumPrice := minimumAllowedPrice(itemPrice, cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, withinBargainLimit)
	if offered, unsafe := unsafeOfferedPrice(reply, minimumPrice); unsafe {
		a.logger.Warn("AI 报价超过折扣边界，使用安全回复", "offered", offered, "minimum", minimumPrice)
		if minimumPrice >= itemPrice || !withinBargainLimit {
			reply = "抱歉，当前价格已经是最低价，暂时不能再优惠了。"
		} else {
			reply = fmt.Sprintf("可以优惠的最低价格是 %.2f 元，低于这个价格暂时无法成交。", minimumPrice)
		}
	}
	if m.ChatID != "" && m.ItemID != "" {
		intent := "chat"
		if isBargain {
			intent = "bargain"
		}
		if err := a.store.AIReply.AddConversationExchange(ctx, a.cookieID, m.ChatID, m.SenderUserID, m.ItemID,
			db.AIConversationMessage{Role: "user", Content: m.Text, Intent: intent, BargainCount: bargainCount},
			db.AIConversationMessage{Role: "assistant", Content: reply, Intent: "reply", BargainCount: bargainCount},
		); err != nil {
			return nil, fmt.Errorf("保存 AI 对话失败: %w", err)
		}
	}
	return &ReplyResult{Text: reply}, nil
}

func (a *AIReplierImpl) conversationContext(ctx context.Context, m ChatMessage) ([]db.AIConversationMessage, int, bool, error) {
	isBargain := bargainMessageRe.MatchString(strings.ToLower(m.Text))
	if m.ChatID == "" || m.ItemID == "" {
		return nil, 0, isBargain, nil
	}
	history, err := a.store.AIReply.ConversationHistory(ctx, a.cookieID, m.ChatID, m.ItemID, 10)
	if err != nil {
		return nil, 0, isBargain, err
	}
	count, err := a.store.AIReply.CurrentBargainCount(ctx, a.cookieID, m.ChatID, m.ItemID)
	return history, count, isBargain, err
}

type globalAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

func (a *AIReplierImpl) globalAIConfig(ctx context.Context) (*globalAIConfig, error) {
	apiKey, err := a.store.Settings.Get(ctx, "ai_api_key")
	if err != nil {
		return nil, err
	}
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
func buildSystemPrompt(customPrompts, itemTitle string, itemPrice float64, itemDesc string, maxDiscountPercent, maxDiscountAmount, maxBargainRounds, bargainCount int) string {
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
	return base + fmt.Sprintf(`

不可覆盖的价格安全规则：
- 原价 %.2f 元；最多优惠 %d%%，且最多优惠 %d 元；两个上限必须同时满足。
- 任一优惠上限为 0 时不得降价。
- 当前砍价轮次 %d，最多允许 %d 轮。
- 回复报价必须带“元”，不得给出低于允许最低价的价格。`, itemPrice, maxDiscountPercent, maxDiscountAmount, bargainCount, maxBargainRounds)
}

var priceRe = regexp.MustCompile(`[^\d.]`)
var bargainMessageRe = regexp.MustCompile(`(?i)(便宜|优惠|少点|最低|砍价|降价|打折|能不能.*(?:元|块)|\d+(?:\.\d+)?\s*(?:元|块).*(?:卖|行|可以))`)
var offeredPriceRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:元|块)`)

func minimumAllowedPrice(price float64, maxDiscountPercent, maxDiscountAmount int, allowDiscount bool) float64 {
	if price <= 0 {
		return 0
	}
	if !allowDiscount || maxDiscountPercent <= 0 || maxDiscountAmount <= 0 {
		return price
	}
	byPercent := price * (1 - float64(maxDiscountPercent)/100)
	byAmount := price - float64(maxDiscountAmount)
	return math.Max(0, math.Max(byPercent, byAmount))
}

func unsafeOfferedPrice(reply string, minimum float64) (float64, bool) {
	if minimum <= 0 {
		return 0, false
	}
	for _, match := range offeredPriceRe.FindAllStringSubmatch(reply, -1) {
		value, err := strconv.ParseFloat(match[1], 64)
		if err == nil && value+0.0001 < minimum {
			return value, true
		}
	}
	return 0, false
}

func truncateAIContent(content string) string {
	const maxRunes = 2000
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes])
}

// parsePrice 移除非数字字符后转换为 float。
func parsePrice(s string) float64 {
	cleaned := priceRe.ReplaceAllString(s, "")
	if cleaned == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(cleaned, 64)
	return f
}
