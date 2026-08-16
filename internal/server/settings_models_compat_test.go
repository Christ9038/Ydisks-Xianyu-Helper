package server

import (
	"context"
	"io"

	"xianyu-go/internal/adapter"
)

// maxOpenAIModelsResponseBytes 保留旧测试名称，实际限制由适配器统一维护。
const maxOpenAIModelsResponseBytes = adapter.MaxAIModelsResponseBytes

// fetchOpenAIModels 保留旧测试辅助入口，生产请求已经由设置应用服务编排。
func fetchOpenAIModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	return adapter.NewAIModelClient().Fetch(ctx, baseURL, apiKey)
}

// readOpenAIModelsBody 保留旧测试辅助入口，实际读取逻辑由适配器维护。
func readOpenAIModelsBody(reader io.Reader) ([]byte, error) {
	return adapter.ReadAIModelsBody(reader)
}

// parseOpenAIModels 保留旧测试辅助入口，实际解析逻辑由适配器维护。
func parseOpenAIModels(raw []byte) ([]string, error) {
	return adapter.ParseAIModels(raw)
}
