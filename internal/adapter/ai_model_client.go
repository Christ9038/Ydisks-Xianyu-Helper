package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xianyu-go/internal/netguard"
)

// MaxAIModelsResponseBytes 限制远端模型目录响应大小，避免管理员配置的端点耗尽内存。
const MaxAIModelsResponseBytes = 4 << 20

// AIModelClient 通过管理员明确配置的端点读取 AI 模型目录。
type AIModelClient struct {
	// newHTTPClient 允许测试替换端点客户端，同时生产默认使用受信任端点策略。
	newHTTPClient func(baseURL string) (*http.Client, error)
}

// NewAIModelClient 构造 AI 模型目录适配器。
func NewAIModelClient() *AIModelClient {
	// client 保存生产环境使用的模型目录客户端。
	client := &AIModelClient{newHTTPClient: func(baseURL string) (*http.Client, error) {
		return netguard.ConfiguredEndpointHTTPClient(baseURL, 20*time.Second)
	}}
	return client
}

// Fetch 请求模型目录并只返回模型名称，不返回 API 密钥或原始响应内容。
func (c *AIModelClient) Fetch(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("AI API 地址为空")
	}
	if c == nil || c.newHTTPClient == nil {
		return nil, fmt.Errorf("AI 模型客户端未初始化")
	}
	// req 是带有请求上下文的模型目录请求；API 密钥只存在于请求头。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	// client 是经过地址校验的出站客户端。
	client, err := c.newHTTPClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("AI API 地址无效: %w", err)
	}
	// response 是远端模型目录 HTTP 响应。
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取模型失败: %w", err)
	}
	defer response.Body.Close()
	// raw 是限制大小后读取的响应内容。
	raw, err := ReadAIModelsBody(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("读取模型失败: HTTP %d %s", response.StatusCode, truncateAIModelBody(string(raw), 180))
	}
	// models、err 保存解析后的模型名称及解析错误。
	models, err := ParseAIModels(raw)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	return models, nil
}

// ReadAIModelsBody 读取并限制远端模型目录响应。
func ReadAIModelsBody(reader io.Reader) ([]byte, error) {
	// raw 是限制大小后读取的响应内容。
	raw, err := io.ReadAll(io.LimitReader(reader, MaxAIModelsResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxAIModelsResponseBytes {
		return nil, fmt.Errorf("模型列表响应超过 %d MiB", MaxAIModelsResponseBytes>>20)
	}
	return raw, nil
}

// ParseAIModels 从兼容 OpenAI 的多种响应形状提取去重模型名称。
func ParseAIModels(raw []byte) ([]string, error) {
	// payload 是远端模型目录的通用 JSON 载荷。
	var payload any
	// err 表示远端模型目录 JSON 解析错误。
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	// seen 保存已返回的模型名称，避免重复项污染下拉列表。
	seen := make(map[string]bool)
	// result 保存按远端出现顺序排列的模型名称。
	var result []string
	// addModel 将非空模型名称加入结果并去重。
	addModel := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		result = append(result, value)
	}
	// walk 递归解析 data、models、对象和字符串等兼容响应结构。
	var walk func(any)
	walk = func(value any) {
		// typed 是当前 JSON 节点的具体类型。
		switch typed := value.(type) {
		case []any:
			// item 是模型目录数组中的当前元素。
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			// id、ok 保存优先使用的模型标识及其类型判断结果。
			if id, ok := typed["id"].(string); ok && id != "" {
				addModel(id)
			} else if // name、ok 保存模型名称回退值及其类型判断结果。
			name, ok := typed["name"].(string); ok && name != "" {
				addModel(name)
			}
		case string:
			addModel(typed)
		}
	}
	// root、ok 保存顶层对象及对象类型判断结果。
	if root, ok := payload.(map[string]any); ok {
		// data、ok 保存兼容 OpenAI 的 data 字段及存在性判断结果。
		if data, ok := root["data"]; ok {
			walk(data)
		} else if // models、ok 保存兼容服务的 models 字段及存在性判断结果。
		models, ok := root["models"]; ok {
			walk(models)
		}
	} else {
		walk(payload)
	}
	return result, nil
}

// truncateAIModelBody 截取错误响应，避免把超大远端正文写入日志或 HTTP 错误。
func truncateAIModelBody(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

var _ interface {
	Fetch(context.Context, string, string) ([]string, error)
} = (*AIModelClient)(nil)
