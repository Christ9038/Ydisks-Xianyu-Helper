package engine

import (
	"net/http"
	"time"
)

func init() {
	// AI 单测通过 httptest 模拟兼容端点；生产构造器仍使用 netguard。
	newAIHTTPClient = func(string) (*http.Client, error) {
		return &http.Client{Timeout: 30 * time.Second}, nil
	}
}
