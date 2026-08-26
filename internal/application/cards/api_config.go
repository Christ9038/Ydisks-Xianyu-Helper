package cards

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// APIRequestTestInput 保存前端临时测试的完整配置文本；测试不会写入卡券库存。
type APIRequestTestInput struct {
	// Config 是与卡券保存接口相同的 API 配置 JSON 文本。
	Config string
}

// APIRequestTestResult 保存一次测试请求的非敏感诊断结果。
type APIRequestTestResult struct {
	// Status 表示远端请求是否返回 2xx。
	Status string `json:"status"`
	// StatusCode 是远端 HTTP 状态码；网络错误时为 0。
	StatusCode int `json:"status_code"`
	// ResponseContentType 是远端响应声明的媒体类型。
	ResponseContentType string `json:"response_content_type"`
	// ResponseFields 是 JSON 顶层字段名称，不包含字段值。
	ResponseFields []string `json:"response_fields"`
	// ExtractedValue 是按响应路径提取的限长文本。
	ExtractedValue string `json:"extracted_value,omitempty"`
	// ResponsePreview 是限长响应预览，便于用户诊断返回格式。
	ResponsePreview string `json:"response_preview,omitempty"`
}

// APIRequestTester 定义卡密 API 测试请求的最小应用能力。
type APIRequestTester interface {
	Test(context.Context, APIRequestTestInput) (APIRequestTestResult, error)
}

// apiConfigDocument 是 API 卡券持久化使用的规范化 JSON 文档。
type apiConfigDocument struct {
	URL          string         `json:"url"`
	Method       string         `json:"method"`
	Timeout      int            `json:"timeout_seconds"`
	Headers      map[string]any `json:"headers"`
	Params       map[string]any `json:"params"`
	Body         map[string]any `json:"body"`
	ContentType  string         `json:"content_type"`
	ResponsePath string         `json:"response_path,omitempty"`
	RetryEnabled bool           `json:"retry_enabled,omitempty"`
}

// APIConfig 是自动发货适配器读取的规范化 API 请求配置；Headers 与 Params 只在服务端内部流转。
type APIConfig = apiConfigDocument

// ParseAPIConfig 解析并校验完整 API 卡券配置，供自动化适配器读取已解密的专用配置。
func ParseAPIConfig(raw string) (APIConfig, error) {
	// normalized 保存兼容旧字段、补齐默认值后的规范化 JSON。
	normalized, err := normalizeAPIConfig(raw, "")
	if err != nil {
		return APIConfig{}, err
	}
	// fields 保存规范化 API 配置的 JSON 字段。
	var fields map[string]json.RawMessage
	// normalized 已由 normalizeAPIConfig 通过 json.Marshal 生成，解析失败不可能发生。
	_ = json.Unmarshal([]byte(normalized), &fields)
	// document 保存解码后的 API 执行配置。
	var document APIConfig
	// fields 来自规范化文档，兼容字段解码和最终校验已在 normalizeAPIConfig 中完成。
	_ = decodeAPIConfig(fields, &document)
	_ = validateAPIConfig(document)
	return document, nil
}

// APIConfigSummary 是卡券查询接口允许返回的 API 配置脱敏摘要。
type APIConfigSummary struct {
	URL               string `json:"url"`
	Method            string `json:"method"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	ContentType       string `json:"content_type"`
	ResponsePath      string `json:"response_path,omitempty"`
	RetryEnabled      bool   `json:"retry_enabled"`
	HeadersConfigured bool   `json:"headers_configured"`
	ParamsConfigured  bool   `json:"params_configured"`
	Ready             bool   `json:"ready"`
	ValidationError   string `json:"validation_error,omitempty"`
}

// SummarizeAPIConfig 将完整配置转换为不含请求模板、密钥或响应正文的查询摘要。
func SummarizeAPIConfig(raw string) APIConfigSummary {
	// summary 保存不包含请求模板值的脱敏状态。
	var summary APIConfigSummary
	// normalized 保存兼容旧字段、补齐默认值后的规范化 JSON。
	normalized, err := normalizeAPIConfig(raw, "")
	if err != nil {
		summary.ValidationError = err.Error()
		return summary
	}
	// fields 保存规范化配置的 JSON 字段。
	var fields map[string]json.RawMessage
	// normalized 已由 normalizeAPIConfig 通过 json.Marshal 生成，解析失败不可能发生。
	_ = json.Unmarshal([]byte(normalized), &fields)
	// document 保存摘要解析出的执行配置；规范化阶段已保证解码成功。
	document, _ := decodeAPIConfigForSummary(fields)
	summary.URL, summary.Method, summary.TimeoutSeconds = document.URL, document.Method, document.Timeout
	summary.ResponsePath, summary.RetryEnabled = document.ResponsePath, document.RetryEnabled
	summary.ContentType = document.ContentType
	summary.HeadersConfigured, summary.ParamsConfigured = len(document.Headers) > 0, len(document.Params) > 0
	// document 已由 normalizeAPIConfig 校验通过，摘要阶段不会再产生新的校验错误。
	_ = validateAPIConfig(document)
	summary.Ready = true
	return summary
}

// decodeAPIConfigForSummary 在不泄漏字段值的前提下解析摘要所需的配置结构。
func decodeAPIConfigForSummary(fields map[string]json.RawMessage) (apiConfigDocument, error) {
	// document 保存摘要使用的执行配置模型。
	var document apiConfigDocument
	// err 表示配置字段解码错误。
	if err := decodeAPIConfig(fields, &document); err != nil {
		return apiConfigDocument{}, err
	}
	return document, nil
}

// normalizeAPIConfig 将新旧 API 配置归一化，并在更新时保留未提交的敏感模板。
func normalizeAPIConfig(raw, existing string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = existing
	}
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("API 卡密缺少配置")
	}
	// fields 保存新旧 API 配置字段，后续会清理兼容控制字段。
	var fields map[string]json.RawMessage
	// err 表示配置 JSON 解析错误。
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return "", fmt.Errorf("API 配置 JSON 无效: %w", err)
	}
	if fields == nil {
		return "", errors.New("API 配置必须是 JSON 对象")
	}
	// previous 保存更新前的敏感模板，用于 retain 三态操作。
	var previous map[string]json.RawMessage
	if strings.TrimSpace(existing) != "" {
		_ = json.Unmarshal([]byte(existing), &previous)
	}
	// action 表示请求头模板的三态更新意图。
	if err := mergeAPISecretField(fields, previous, "headers"); err != nil {
		return "", err
	}
	// action 表示请求参数模板的三态更新意图。
	if err := mergeAPISecretField(fields, previous, "params"); err != nil {
		return "", err
	}
	delete(fields, "headers_action")
	delete(fields, "params_action")
	// timeout 表示历史配置使用的兼容超时字段。
	if timeout := rawString(fields["timeout"]); timeout != "" && fields["timeout_seconds"] == nil {
		fields["timeout_seconds"] = json.RawMessage(strconv.Itoa(parseIntDefault(timeout, 10)))
	}
	if fields["method"] == nil {
		fields["method"] = json.RawMessage(`"GET"`)
	}
	if fields["timeout_seconds"] == nil {
		fields["timeout_seconds"] = json.RawMessage("10")
	}
	if fields["headers"] == nil {
		fields["headers"] = json.RawMessage(`{}`)
	}
	if fields["params"] == nil {
		fields["params"] = json.RawMessage(`{}`)
	}
	if fields["body"] == nil {
		fields["body"] = json.RawMessage(`{}`)
	}
	if fields["content_type"] == nil {
		fields["content_type"] = json.RawMessage(`"application/json"`)
	}
	// document 保存规范化后的 API 执行配置。
	var document apiConfigDocument
	// err 表示兼容字段解码错误。
	if err := decodeAPIConfig(fields, &document); err != nil {
		return "", err
	}
	// err 表示规范化配置校验错误。
	if err := validateAPIConfig(document); err != nil {
		return "", err
	}
	// document 由 JSON 字段解码而来，只包含可编码的基础值，重新编码不会失败。
	encoded, _ := json.Marshal(document)
	return string(encoded), nil
}

// mergeAPISecretField 在请求没有提交敏感模板时保留数据库中的旧值。
func mergeAPISecretField(fields, previous map[string]json.RawMessage, name string) error {
	// action 表示当前模板字段的三态更新意图。
	action := strings.ToLower(strings.TrimSpace(rawString(fields[name+"_action"])))
	switch action {
	case "":
		if fields[name] == nil && previous != nil && previous[name] != nil {
			fields[name] = previous[name]
		}
	case "retain":
		if previous != nil && previous[name] != nil {
			fields[name] = previous[name]
		} else if fields[name] == nil {
			fields[name] = json.RawMessage(`{}`)
		}
	case "replace":
		if fields[name] == nil {
			return fmt.Errorf("%s_action=replace 时必须提供 JSON 对象", name)
		}
	case "clear":
		fields[name] = json.RawMessage(`{}`)
	default:
		return fmt.Errorf("%s_action 必须是 retain、replace 或 clear", name)
	}
	return nil
}

// decodeAPIConfig 把兼容字段映射为执行器使用的规范化模型。
func decodeAPIConfig(fields map[string]json.RawMessage, document *apiConfigDocument) error {
	// name 表示需要兼容旧版 JSON 字符串表示的敏感模板字段。
	for _, name := range []string{"headers", "params"} {
		// raw 保存当前模板字段的原始 JSON。
		raw := fields[name]
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		// encodedString、encodedObject 分别保存旧字符串模板和规范对象模板。
		var encodedString string
		// encodedObject 保存旧版字符串字段解析后的 JSON 对象。
		var encodedObject map[string]any
		if json.Unmarshal(raw, &encodedString) == nil {
			if strings.TrimSpace(encodedString) == "" {
				fields[name] = json.RawMessage(`{}`)
				continue
			}
			// err 表示旧版模板字符串不是 JSON 对象的错误。
			if err := json.Unmarshal([]byte(encodedString), &encodedObject); err != nil {
				return fmt.Errorf("解析 API %s 模板失败: %w", name, err)
			}
			// encodedObject 由 JSON 解码得到，只包含可编码的基础值，转换编码不会失败。
			converted, _ := json.Marshal(encodedObject)
			fields[name] = converted
		}
	}
	// encoded、err 保存字段集合重新编码后的 JSON 和编码错误。
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	// err 表示规范字段解码到执行模型时的错误。
	if err := json.Unmarshal(encoded, document); err != nil {
		return fmt.Errorf("解析 API 配置失败: %w", err)
	}
	if document.Headers == nil {
		document.Headers = map[string]any{}
	}
	if document.Params == nil {
		document.Params = map[string]any{}
	}
	document.Method = strings.ToUpper(strings.TrimSpace(document.Method))
	return nil
}

// validateAPIConfig 校验 API 地址、方法、超时和幂等重试约束。
func validateAPIConfig(document apiConfigDocument) error {
	// parsed、err 保存 API 地址解析结果及地址格式错误。
	parsed, err := url.Parse(strings.TrimSpace(document.URL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("API 地址必须是 HTTP(S) 地址且不能包含用户凭据")
	}
	if document.Method != "GET" && document.Method != "POST" {
		return errors.New("API 请求方法只能是 GET 或 POST")
	}
	if document.Timeout < 1 || document.Timeout > 60 {
		return errors.New("API 超时时间必须在 1 到 60 秒之间")
	}
	if document.RetryEnabled && !containsAPIPlaceholder(document.Headers, "{idempotency_key}") && !containsAPIPlaceholder(document.Params, "{idempotency_key}") {
		return errors.New("启用 API 重试时，请求头或请求参数必须包含 {idempotency_key}")
	}
	return nil
}

// containsAPIPlaceholder 递归判断 JSON 模板中是否包含指定占位符。
func containsAPIPlaceholder(value any, placeholder string) bool {
	// current 保存当前递归节点，用于查找幂等键变量。
	switch current := value.(type) {
	case string:
		return strings.Contains(current, placeholder)
	case map[string]any:
		// child 表示对象中的递归模板值。
		for _, child := range current {
			if containsAPIPlaceholder(child, placeholder) {
				return true
			}
		}
	case []any:
		// child 表示数组中的递归模板值。
		for _, child := range current {
			if containsAPIPlaceholder(child, placeholder) {
				return true
			}
		}
	}
	return false
}

// rawString 读取 JSON 字段的字符串表示，兼容数字和旧版字符串字段。
func rawString(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	// value 保存字符串类型的 JSON 字段值。
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	// generic 保存非字符串 JSON 字段的通用值。
	var generic any
	if json.Unmarshal(raw, &generic) == nil {
		return fmt.Sprint(generic)
	}
	return ""
}

// parseIntDefault 将兼容字符串数字转为整数并提供稳定默认值。
func parseIntDefault(raw string, fallback int) int {
	// value、err 保存兼容超时字段的整数值及解析错误。
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}
