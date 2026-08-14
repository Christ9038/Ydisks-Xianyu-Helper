package server

import (
	"net/http"
	"time"
)

// init 负责init相关处理。
func init() {
	// 服务器单测通过 httptest 模拟 AI 端点；生产构造器仍使用 netguard。
	newSettingsOutboundHTTPClient = func(string) (*http.Client, error) {
		return &http.Client{Timeout: 20 * time.Second}, nil
	}
}
