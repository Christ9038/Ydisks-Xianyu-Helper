package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CardAPIConfigSummary 是数据库向普通查询路径提供的 API 卡脱敏摘要。
type CardAPIConfigSummary struct {
	// URL 是 API 端点地址，不包含请求模板内容。
	URL string `json:"url"`
	// Method 是 API 请求方法。
	Method string `json:"method"`
	// TimeoutSeconds 是请求超时时间，单位为秒。
	TimeoutSeconds int `json:"timeout_seconds"`
	// ResponsePath 是响应提取路径。
	ResponsePath string `json:"response_path,omitempty"`
	// RetryEnabled 表示是否启用幂等重试。
	RetryEnabled bool `json:"retry_enabled"`
	// HeadersConfigured 表示是否配置了请求头模板。
	HeadersConfigured bool `json:"headers_configured"`
	// ParamsConfigured 表示是否配置了请求参数模板。
	ParamsConfigured bool `json:"params_configured"`
	// Ready 表示配置能否进入自动化规则选择。
	Ready bool `json:"ready"`
	// ValidationError 是不包含秘密值的配置错误。
	ValidationError string `json:"validation_error,omitempty"`
}

// GetForDelivery 读取自动发货专用的完整卡券配置，普通查询不得调用此方法。
func (c *Cards) GetForDelivery(ctx context.Context, cardID int64) (*CardFull, error) {
	return c.Get(ctx, cardID)
}

// GetSummary 读取单个卡券的脱敏摘要，完整 API 模板不会离开数据库层。
func (c *Cards) GetSummary(ctx context.Context, cardID int64) (*CardFull, error) {
	// card 是内部完整读取后立即脱敏的卡券记录。
	card, err := c.Get(ctx, cardID)
	if err != nil {
		return nil, err
	}
	if card.Type == "api" {
		card.APIConfigSummary = summarizeCardAPIConfig(card.Type, card.APIConfig)
		card.APIConfig = ""
	}
	return card, nil
}

// AllForUserSummary 读取用户卡券列表的脱敏摘要，避免把 API 模板带入应用层。
func (c *Cards) AllForUserSummary(ctx context.Context, userID int64) ([]CardFull, error) {
	// cards 是内部完整读取后逐条脱敏的卡券记录。
	cards, err := c.AllForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// i 表示当前需要脱敏处理的卡券列表下标。
	for i := range cards {
		if cards[i].Type == "api" {
			cards[i].APIConfigSummary = summarizeCardAPIConfig(cards[i].Type, cards[i].APIConfig)
			cards[i].APIConfig = ""
		}
	}
	return cards, nil
}

// summarizeCardAPIConfig 将已解密的 API 配置转换为不含敏感模板的摘要。
func summarizeCardAPIConfig(cardType, raw string) *CardAPIConfigSummary {
	if cardType != "api" {
		return nil
	}
	// summary 是普通卡券查询使用的脱敏结果。
	summary := &CardAPIConfigSummary{}
	// fields 是只用于解析非敏感字段和模板是否存在的 JSON 对象。
	var fields map[string]json.RawMessage
	// err 表示 API 配置 JSON 解析错误。
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		summary.ValidationError = "API 配置 JSON 无效"
		return summary
	}
	summary.URL = summaryRawString(fields["url"])
	summary.Method = strings.ToUpper(summaryRawString(fields["method"]))
	if summary.Method == "" {
		summary.Method = "GET"
	}
	summary.TimeoutSeconds = parseSummaryTimeout(fields)
	summary.ResponsePath = summaryRawString(fields["response_path"])
	summary.RetryEnabled = strings.EqualFold(summaryRawString(fields["retry_enabled"]), "true")
	summary.HeadersConfigured = templateConfigured(fields["headers"])
	summary.ParamsConfigured = templateConfigured(fields["params"])
	// templates 是只在本地校验幂等占位符的临时 JSON 值，不进入返回结构。
	templates := make(map[string]any, 2)
	// name 表示当前待读取的敏感模板字段名。
	for _, name := range []string{"headers", "params"} {
		// value 保存模板的临时 JSON 值，函数返回前不会写入摘要。
		var value any
		// err 表示模板字段解析错误；错误模板不影响公开字段读取。
		if err := json.Unmarshal(fields[name], &value); err == nil {
			templates[name] = value
		}
	}
	// err 表示摘要公开字段或重试约束校验错误。
	if err := validateSummaryAPIConfig(*summary, templates); err != nil {
		summary.ValidationError = err.Error()
		return summary
	}
	summary.Ready = true
	return summary
}

// parseSummaryTimeout 兼容历史配置中的 timeout 字段并返回摘要超时。
func parseSummaryTimeout(fields map[string]json.RawMessage) int {
	// raw 保存优先使用的新字段或历史字段。
	raw := fields["timeout_seconds"]
	if len(raw) == 0 {
		raw = fields["timeout"]
	}
	// value 保存兼容超时字段的文本值。
	value := summaryRawString(raw)
	if value == "" {
		return 10
	}
	// timeout 保存解析后的秒数。
	timeout, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return timeout
}

// summaryRawString 读取摘要解析所需的 JSON 标量文本，不保留原始模板值。
func summaryRawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// value 保存字符串类型 JSON 标量。
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	// generic 保存数字或布尔类型 JSON 标量。
	var generic any
	if json.Unmarshal(raw, &generic) == nil {
		return fmt.Sprint(generic)
	}
	return ""
}

// templateConfigured 判断模板字段是否存在且不是空对象或空字符串。
func templateConfigured(raw json.RawMessage) bool {
	// value 保存模板字段的紧凑 JSON 形式，仅用于判断是否已配置。
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != "{}" && value != `""`
}

// validateSummaryAPIConfig 校验摘要中的公开字段及重试占位符约束。
func validateSummaryAPIConfig(summary CardAPIConfigSummary, templates map[string]any) error {
	// parsed 保存公开 API 地址解析结果。
	parsed, err := url.Parse(strings.TrimSpace(summary.URL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("API 地址必须是 HTTP(S) 地址且不能包含用户凭据")
	}
	if summary.Method != "GET" && summary.Method != "POST" {
		return errors.New("API 请求方法只能是 GET 或 POST")
	}
	if summary.TimeoutSeconds < 1 || summary.TimeoutSeconds > 60 {
		return errors.New("API 超时时间必须在 1 到 60 秒之间")
	}
	if summary.RetryEnabled && !summaryTemplateContains(templates, "{idempotency_key}") {
		return errors.New("启用 API 重试时，请求头或请求参数必须包含 {idempotency_key}")
	}
	return nil
}

// summaryTemplateContains 递归检查模板是否包含指定占位符。
func summaryTemplateContains(value any, placeholder string) bool {
	// current 保存当前递归节点的模板值。
	switch current := value.(type) {
	case string:
		return strings.Contains(current, placeholder)
	case map[string]any:
		// child 表示当前对象中的模板子节点。
		for _, child := range current {
			if summaryTemplateContains(child, placeholder) {
				return true
			}
		}
	case []any:
		// child 表示当前数组中的模板子节点。
		for _, child := range current {
			if summaryTemplateContains(child, placeholder) {
				return true
			}
		}
	}
	return false
}
