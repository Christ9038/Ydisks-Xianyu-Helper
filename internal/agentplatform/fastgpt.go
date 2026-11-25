package agentplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// fastGPTResponse 是 FastGPT 非流式响应里本包关心的字段。
type fastGPTResponse struct {
	// Choices 是候选回复，取第一条。
	Choices []struct {
		// Message 是助手消息；content 通常是字符串，交互节点等情况下是对象数组。
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// fastGPTError 是 FastGPT 非 2xx 响应的错误体。
type fastGPTError struct {
	// StatusText 是机器可读错误名，例如 chatIsGenerating。
	StatusText string `json:"statusText"`
}

// FastGPT 向 FastGPT 应用发送一次对话请求（非流式），chatId 由 request.SessionID 给出并让平台自己维护历史。
//
// 带 chatId 时平台只取 messages 的最后一条作为用户问题，所以这里只发送本轮合并后的输入。
// 不发送 customUid：该字段一旦绑定后必须每轮一致，省略则始终匹配。
func FastGPT(ctx context.Context, endpoint Endpoint, request Request) (Result, error) {
	// target、err 是补全后的接口地址；用户给出的是基础路径时默认使用 /api/v1。
	target, err := resolveURL(endpoint.BaseURL, "/chat/completions", "/api/v1/chat/completions",
		pathRule{"/api", "/v1/chat/completions"}, pathRule{"/api/v1", "/chat/completions"}, pathRule{"/api/v2", "/chat/completions"},
		pathRule{"/v1", "/chat/completions"}, pathRule{"/v2", "/chat/completions"})
	if err != nil {
		return Result{}, err
	}
	// result、err 保存带图片的首次调用结果；平台以 400 或 422 拒绝图片时去掉图片重试一次。
	result, err := fastGPTWithBusyRetry(ctx, endpoint, target, request, request.ImageURLs)
	if len(request.ImageURLs) > 0 && (StatusCode(err) == http.StatusBadRequest || StatusCode(err) == http.StatusUnprocessableEntity) {
		// retry、retryErr 保存纯文字重试结果。
		retry, retryErr := fastGPTWithBusyRetry(ctx, endpoint, target, request, nil)
		if retryErr == nil {
			retry.ImageDropped = true
		}
		return retry, retryErr
	}
	return result, err
}

// isFastGPTBusy 判断错误是否表示该会话仍在生成。文档写的是 HTTP 409，但实测 FastGPT v4.17.5 返回
// HTTP 500 加 statusText=chatIsGenerating，所以以平台错误名为准，同时兼容 409。
func isFastGPTBusy(err error) bool {
	// statusErr 保存错误链中的平台状态错误。
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.Code == http.StatusConflict || statusErr.Reason == "chatIsGenerating"
}

// fastGPTWithBusyRetry 在该会话仍在生成（常见于上一轮被新消息中断后）时有限次等待重试，
// 并在自动补全为 /v1 的接口返回 404 时改试 /v2（较新版本的路径）。
func fastGPTWithBusyRetry(ctx context.Context, endpoint Endpoint, target string, request Request, images []string) (Result, error) {
	// attempt 是当前已发生的忙碌重试次数。
	for attempt := 0; ; attempt++ {
		// result、err 保存本次请求结果。
		result, err := fastGPTOnce(ctx, endpoint, target, request, images)
		if StatusCode(err) == http.StatusNotFound && strings.Contains(target, "/v1/chat/completions") {
			// v2Result、v2Err 保存改用 /v2 路径的结果；仍失败时返回原始的 404 以免误导。
			v2Result, v2Err := fastGPTOnce(ctx, endpoint, strings.Replace(target, "/v1/chat/completions", "/v2/chat/completions", 1), request, images)
			if v2Err == nil || StatusCode(v2Err) != http.StatusNotFound {
				return v2Result, v2Err
			}
			return result, err
		}
		if !isFastGPTBusy(err) || attempt >= fastGPTBusyRetries {
			return result, err
		}
		// waitErr 是等待期间上下文结束的错误，此时直接放弃重试；单次等待线性增加并封顶。
		if waitErr := sleep(ctx, min(fastGPTBusyWait*time.Duration(attempt+1), fastGPTBusyWaitMax)); waitErr != nil {
			return Result{}, waitErr
		}
	}
}

// fastGPTOnce 发送一次不含重试的请求。
func fastGPTOnce(ctx context.Context, endpoint Endpoint, target string, request Request, images []string) (Result, error) {
	// content 是用户消息内容：没有图片时是纯字符串，有图片时是 OpenAI 风格的 text + image_url 数组。
	var content any = request.Text
	if len(images) > 0 {
		// parts 依次是文字块和各图片块。
		parts := []map[string]any{{"type": "text", "text": request.Text}}
		// image 是当前遍历的图片地址。
		for _, image := range images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": image}})
		}
		content = parts
	}
	// payload 是请求体；detail 关闭以保持 content 为字符串。
	payload := map[string]any{
		"chatId": request.SessionID, "stream": false, "detail": false,
		"messages": []map[string]any{{"role": "user", "content": content}},
	}
	// body、err 保存序列化结果。
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	// httpRequest 携带令牌，只在 Authorization 头出现。
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return Result{}, errors.New("平台地址无效")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if endpoint.Token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+endpoint.Token)
	}
	// response、err 保存响应与传输错误；传输错误不带请求地址和令牌。
	response, err := endpoint.HTTPClient.Do(httpRequest)
	if err != nil {
		return Result{}, sanitizeTransportError(err)
	}
	defer response.Body.Close()
	// raw、readErr 保存有上限的响应体。
	raw, readErr := readLimited(response.Body, maxResponseBytes)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		// parsedErr 保存错误体中的错误名；读取或解析失败按没有错误名处理。
		var parsedErr fastGPTError
		_ = json.Unmarshal(raw, &parsedErr)
		return Result{}, &StatusError{Code: response.StatusCode, Reason: parsedErr.StatusText}
	}
	if readErr != nil {
		return Result{}, sanitizeTransportError(readErr)
	}
	// parsed 保存解析出的响应。
	var parsed fastGPTResponse
	if err = json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 {
		return Result{}, ErrEmptyReply
	}
	// reply 是从 content 提取并去除首尾空白的回复文字。
	reply := strings.TrimSpace(fastGPTText(parsed.Choices[0].Message.Content))
	if reply == "" {
		return Result{}, ErrEmptyReply
	}
	return Result{Text: reply, SessionID: request.SessionID}, nil
}

// fastGPTText 从 content 提取文字：字符串直接返回；对象数组只拼接 type 为 text 的块（text 可为字符串或带 content 字段的对象）。
func fastGPTText(raw json.RawMessage) string {
	// asString 是 content 为字符串时的取值。
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return asString
	}
	// blocks 是 content 为数组时的各块。
	var blocks []struct {
		Type string          `json:"type"`
		Text json.RawMessage `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	// joined 依次拼接文字块。
	var joined strings.Builder
	// block 是当前遍历的 content 块。
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		// plain、object 分别是 text 字段为字符串或为 {content} 对象时的取值。
		var plain string
		// object 是 text 字段为对象时的解码目标。
		var object struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(block.Text, &plain) == nil {
			joined.WriteString(plain)
		} else if json.Unmarshal(block.Text, &object) == nil {
			joined.WriteString(object.Content)
		}
	}
	return joined.String()
}
