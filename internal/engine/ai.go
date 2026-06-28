// ai.go AI 回复实现（优先级3）。调用 OpenAI 兼容 chat completions 接口。
// 移植自 Python ai_reply_engine.generate_reply 的核心：用商品信息 + 自定义 prompt 生成回复。
// 砍价/对话历史追踪（bargain_count、ai_conversations）留后续完善，本实现为单轮无状态回复。
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/db"
)

const (
	defaultAIBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultAIModel   = "qwen-plus"
)

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

	// 取商品信息构造 system prompt。
	itemTitle, itemPrice, itemDesc := a.itemInfo(ctx, m.ItemID)
	systemPrompt := buildSystemPrompt(cfg.CustomPrompts, itemTitle, itemPrice, itemDesc, cfg.MaxDiscountPercent, cfg.MaxDiscountAmount)

	// 调 OpenAI 兼容接口。
	clientCfg := openai.DefaultConfig(aiCfg.APIKey)
	if aiCfg.BaseURL != "" {
		clientCfg.BaseURL = aiCfg.BaseURL
	}
	client := openai.NewClientWithConfig(clientCfg)

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: aiCfg.Model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: m.Text},
		},
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
	return &ReplyResult{Text: reply}, nil
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
	desc = it.ItemDetail
	if desc == "" {
		desc = "暂无商品描述"
	}
	return
}

// buildSystemPrompt 构造 system 提示词。
// 优先用自定义 prompt，否则用默认模板（含商品信息）。
func buildSystemPrompt(customPrompts, itemTitle string, itemPrice float64, itemDesc string, maxDiscountPercent, maxDiscountAmount int) string {
	if strings.TrimSpace(customPrompts) != "" {
		return strings.ReplaceAll(customPrompts, "{item_title}", itemTitle)
	}
	if maxDiscountPercent <= 0 {
		maxDiscountPercent = 10
	}
	if maxDiscountAmount <= 0 {
		maxDiscountAmount = 100
	}
	return fmt.Sprintf(`你是闲鱼卖家的自动回复助手。请根据商品信息友好地回复买家。

商品信息：
- 标题：%s
- 价格：%.2f 元
- 描述：%s

要求：
1. 语气友好自然，像真人卖家
2. 回答简洁，不要过长
3. 涉及价格让步时，最多优惠 %.0f%% 或 %.0f 元
4. 不要编造商品没有的功能
5. 直接回复内容，不要加引号或解释`, itemTitle, itemPrice, itemDesc, float64(maxDiscountPercent), float64(maxDiscountAmount))
}

var priceRe = regexp.MustCompile(`[^\d.]`)

// parsePrice 复刻 Python _parse_price：移除非数字字符后转 float。
func parsePrice(s string) float64 {
	cleaned := priceRe.ReplaceAllString(s, "")
	if cleaned == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(cleaned, "%f", &f)
	return f
}
