package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
	"xianyu-go/internal/llmclient"
)

// aiTextGenerationTimeout 是单次非聊天文本生成的总时长上限，避免阻塞自动化运行；
// 与聊天 AI 回复共用 llmclient.RequestTimeout，使 context 上限与 HTTP 客户端超时一致。
const aiTextGenerationTimeout = llmclient.RequestTimeout

// ErrAINotConfigured 表示全局 AI 尚未配置 API Key，调用方应回退到其他来源或明确失败。
var ErrAINotConfigured = errors.New("全局 AI 未配置 API Key")

// GenerateGlobalAIText 使用全局 AI 配置完成一次无历史的文本生成。
// cookieID 用于敏感设置读取审计；system 与 user 分别是系统提示词和用户输入；maxTokens 限制输出长度。
// 返回的文本已去除首尾空白；模型拒绝回答或返回空内容时报错，不返回部分结果。
func GenerateGlobalAIText(ctx context.Context, store *db.Store, cookieID, system, user string, maxTokens int) (string, error) {
	if store == nil {
		return "", errors.New("AI 文本生成缺少数据存储")
	}
	// cfg 是规范化后的全局 AI 配置。
	cfg, err := loadGlobalAIConfig(ctx, store, cookieID)
	if err != nil {
		return "", err
	}
	if cfg.APIKey == "" {
		return "", ErrAINotConfigured
	}
	// httpClient 是带出站信任校验的 AI 客户端，与聊天 AI 回复使用同一构造入口。
	httpClient, err := newAIHTTPClient(cfg.BaseURL)
	if err != nil {
		return "", fmt.Errorf("AI API 地址无效: %w", err)
	}
	// callCtx 带有总时长约束，随调用方取消而取消。
	callCtx, cancel := context.WithTimeout(ctx, aiTextGenerationTimeout)
	defer cancel()
	// completion 保存模型结果。
	completion, err := llmclient.Complete(callCtx, llmclient.Endpoint{Protocol: cfg.Protocol, BaseURL: cfg.BaseURL,
		APIKey: cfg.APIKey, Model: cfg.Model, HTTPClient: httpClient},
		llmclient.Request{System: system, UserText: user, MaxTokens: maxTokens})
	if err != nil {
		return "", fmt.Errorf("AI 调用失败: %w", err)
	}
	if completion.Refused {
		return "", errors.New("AI 模型拒绝生成")
	}
	// text 是去除空白后的模型输出。
	text := strings.TrimSpace(completion.Text)
	if text == "" {
		return "", errors.New("AI 返回了空内容")
	}
	return text, nil
}
