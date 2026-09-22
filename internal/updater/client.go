package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// clientBaseURL 是 Unix transport 使用的占位 HTTP 地址，不参与实际网络连接。
	clientBaseURL = "http://unix"
	// maximumDaemonResponseBytes 限制本机 daemon 响应大小。
	maximumDaemonResponseBytes = 1 << 20
)

// Client 是应用进程调用固定宿主机 updater Unix API 的客户端。
//
// Client 不暴露 Socket 路径、镜像、服务或命令配置；并发调用由 http.Client 安全承载。
type Client struct {
	// httpClient 使用只连接 DefaultSocketPath 的 Unix transport。
	httpClient *http.Client
}

// NewClient 创建固定连接 DefaultSocketPath 的应用侧 updater 客户端。
func NewClient() *Client {
	return newClient(DefaultSocketPath)
}

// newClient 创建包内测试客户端；socketPath 不通过生产 API、环境变量或命令行暴露。
func newClient(socketPath string) *Client {
	// dialer 为 Unix Socket 建立受 Context 控制的本机连接。
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	// transport 忽略 HTTP URL 主机并始终连接包内给定的 Unix Socket。
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{httpClient: &http.Client{Transport: transport, Timeout: 35 * time.Second}}
}

// Status 返回 daemon 最近操作和当前固定部署状态。
func (client *Client) Status(ctx context.Context) (StatusResponse, error) {
	// response 保存 status API 解码结果。
	var response StatusResponse
	// requestErr 表示 status 请求未能完成或响应不符合契约。
	if /* requestErr 表示 status 请求未能完成或响应不符合契约。 */ requestErr := client.request(ctx, http.MethodGet, "/status", &response); requestErr != nil {
		return StatusResponse{}, requestErr
	}
	return response, nil
}

// Check 返回最新稳定 Release 与当前部署之间的只读更新决策。
func (client *Client) Check(ctx context.Context) (CheckResult, error) {
	// response 保存 check API 解码结果。
	var response CheckResult
	// requestErr 表示 check 请求未能完成或响应不符合契约。
	if /* requestErr 表示 check 请求未能完成或响应不符合契约。 */ requestErr := client.request(ctx, http.MethodGet, "/check", &response); requestErr != nil {
		return CheckResult{}, requestErr
	}
	return response, nil
}

// ApplyLatestStable 请求 daemon 更新到其自行验证的最新正式版本，不发送请求体或目标参数。
func (client *Client) ApplyLatestStable(ctx context.Context) (ApplyResponse, error) {
	// response 保存 daemon 成功受理的异步操作状态。
	var response ApplyResponse
	// requestErr 表示更新请求未能完成或 daemon 拒绝受理。
	if /* requestErr 表示更新请求未能完成或 daemon 拒绝受理。 */ requestErr := client.request(ctx, http.MethodPost, "/apply_latest_stable", &response); requestErr != nil {
		return ApplyResponse{}, requestErr
	}
	return response, nil
}

// APIError 表示 daemon 返回的稳定非 2xx 错误。
type APIError struct {
	// StatusCode 是 Unix HTTP 响应状态码。
	StatusCode int
	// Code 是 daemon 的机器可读错误码。
	Code string
	// Message 是不包含敏感宿主机信息的错误说明。
	Message string
}

// Error 返回适合日志和 UI 的 updater API 错误摘要。
func (apiError *APIError) Error() string {
	return fmt.Sprintf("updater API %s: %s", apiError.Code, apiError.Message)
}

// request 调用固定 Unix API 路径并将成功 JSON 解码到 destination。
func (client *Client) request(ctx context.Context, method, path string, destination any) error {
	if client == nil || client.httpClient == nil {
		return errors.New("updater client 未初始化")
	}
	// request 是不包含请求参数或请求体的 Unix HTTP 调用。
	request, requestErr := http.NewRequestWithContext(ctx, method, clientBaseURL+path, bytes.NewReader(nil))
	if requestErr != nil {
		return requestErr
	}
	// response 是宿主机 daemon 返回的受限 JSON 响应。
	response, responseErr := client.httpClient.Do(request)
	if responseErr != nil {
		return responseErr
	}
	defer response.Body.Close()
	// payload 保存受大小限制的完整 daemon 响应。
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maximumDaemonResponseBytes+1))
	if readErr != nil {
		return readErr
	}
	if len(payload) > maximumDaemonResponseBytes {
		return errors.New("updater daemon 响应超过大小限制")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// errorResponse 保存 daemon 公开错误契约，不回显原始响应体。
		var errorResponse ErrorResponse
		// decodeErr 表示 daemon 错误载荷无法解析为 code/message 契约。
		if decodeErr := json.Unmarshal(payload, &errorResponse); decodeErr != nil {
			return &APIError{StatusCode: response.StatusCode, Code: "invalid_error_response", Message: "updater daemon 返回了无效错误响应"}
		}
		return &APIError{StatusCode: response.StatusCode, Code: errorResponse.Code, Message: errorResponse.Message}
	}
	// decodeErr 表示成功响应不是调用方要求的 JSON 模型。
	if decodeErr := json.Unmarshal(payload, destination); decodeErr != nil {
		return fmt.Errorf("解析 updater daemon 响应失败: %w", decodeErr)
	}
	return nil
}
