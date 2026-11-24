package agentplatform

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// difyEvent 是 Dify 流式响应中一条 data 事件里本包关心的字段。
type difyEvent struct {
	// Event 是事件类型，例如 message、agent_message、message_replace、message_end、error。
	Event string `json:"event"`
	// Answer 是该事件携带的回复文字片段。
	Answer string `json:"answer"`
	// ConversationID 是平台会话标识，几乎每个事件都会携带。
	ConversationID string `json:"conversation_id"`
	// Status、Code 是 error 事件携带的状态与机器可读错误码。
	Status int    `json:"status"`
	Code   string `json:"code"`
}

// difyErrorBody 是 Dify 非 2xx 响应的错误体。
type difyErrorBody struct {
	// Code 是机器可读错误码，例如 not_found、conversation_completed。
	Code string `json:"code"`
}

// Dify 向 Dify 应用发送一次聊天消息，使用流式模式收集完整回复。
//
// 选择流式是因为 Dify 的 Agent 类应用只支持流式，Chatbot 与 Chatflow 两种模式也都支持，一个实现覆盖全部应用类型。
// inputs 固定为空对象，所以应用里“必填的开始节点变量”会让平台拒绝请求，需要在 Dify 中改成可选或给默认值。
func Dify(ctx context.Context, endpoint Endpoint, request Request) (Result, error) {
	// target、err 是补全后的接口地址及地址错误；云端为 https://api.dify.ai/v1，自建为 https://主机/v1。
	target, err := resolveURL(endpoint.BaseURL, "/chat-messages", "/v1/chat-messages", pathRule{"/v1", "/chat-messages"})
	if err != nil {
		return Result{}, err
	}
	// result、err 保存带图片的首次调用结果；平台以 400 拒绝图片时去掉图片重试一次。
	result, err := difyOnce(ctx, endpoint, target, request, request.ImageURLs)
	if len(request.ImageURLs) > 0 && StatusCode(err) == http.StatusBadRequest {
		// retry、retryErr 保存纯文字重试结果。
		retry, retryErr := difyOnce(ctx, endpoint, target, request, nil)
		if retryErr == nil {
			retry.ImageDropped = true
		}
		return retry, retryErr
	}
	return result, err
}

// difyOnce 发送一次不含重试的请求；images 为空时不带 files 字段。
func difyOnce(ctx context.Context, endpoint Endpoint, target string, request Request, images []string) (Result, error) {
	// files 是 Dify 的图片文件引用，传输方式固定为远程地址，由 Dify 自行下载。
	files := make([]map[string]string, 0, len(images))
	// image 是当前遍历的图片地址。
	for _, image := range images {
		files = append(files, map[string]string{"type": "image", "transfer_method": "remote_url", "url": image})
	}
	// payload 是请求体；conversation_id 为空串表示开启新会话。
	payload := map[string]any{
		"inputs": map[string]any{}, "query": request.Text, "response_mode": "streaming",
		"user": request.User, "conversation_id": request.SessionID, "auto_generate_name": false,
	}
	if len(files) > 0 {
		payload["files"] = files
	}
	// body、err 保存序列化结果；对 map 序列化不会失败，仍按错误返回以免吞掉异常。
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
	httpRequest.Header.Set("Accept", "text/event-stream")
	if endpoint.Token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+endpoint.Token)
	}
	// response、err 保存响应与传输错误；传输错误不带请求地址和令牌。
	response, err := endpoint.HTTPClient.Do(httpRequest)
	if err != nil {
		return Result{}, sanitizeTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, difyStatusError(response, request.SessionID != "")
	}
	return readDifyStream(response.Body, request.SessionID)
}

// difyStatusError 把非 200 响应归类：会话不存在或已结束返回 ErrSessionGone（仅在确实带了会话标识时），其余返回状态错误。
func difyStatusError(response *http.Response, hadSession bool) error {
	// raw 是有上限的错误体；读取失败按没有错误码处理。
	raw, _ := readLimited(response.Body, maxErrorBodyBytes)
	// parsed 保存错误体中的错误码。
	var parsed difyErrorBody
	_ = json.Unmarshal(raw, &parsed)
	if hadSession && (response.StatusCode == http.StatusNotFound || parsed.Code == "conversation_completed") {
		return ErrSessionGone
	}
	return &StatusError{Code: response.StatusCode, Reason: parsed.Code}
}

// readDifyStream 解析 SSE 流并拼出完整回复：message 与 agent_message 的片段依次拼接，message_replace 整体替换，
// Agent 应用结尾那条重复完整答案的 message 事件在出现过 agent_message 后被忽略。
func readDifyStream(body io.Reader, previousSession string) (Result, error) {
	// reader 按行读取事件流，整体受 maxResponseBytes 约束。
	reader := bufio.NewReader(io.LimitReader(body, maxResponseBytes))
	// text 累积回复文字；session 记录平台会话标识；sawAgent 表示已出现 agent_message。
	var text strings.Builder
	// sawAgent 表示是否已出现 agent_message 事件；session 是当前累计的平台会话标识。
	session, sawAgent := previousSession, false
	for {
		// line、readErr 是一行原始内容及读取错误；最后一行可能没有换行。
		line, readErr := reader.ReadString('\n')
		// data 是读取到的数据；ok 表示相应的存在性或类型判断是否成立。
		if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			// event 保存解析出的事件；无法解析的行（如 ping 注释）被忽略。
			var event difyEvent
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &event) == nil {
				if event.ConversationID != "" {
					session = event.ConversationID
				}
				switch event.Event {
				case "message":
					if !sawAgent {
						text.WriteString(event.Answer)
					}
				case "agent_message":
					sawAgent = true
					text.WriteString(event.Answer)
				case "message_replace":
					text.Reset()
					text.WriteString(event.Answer)
				case "error":
					// status 是错误事件携带的状态；缺失时按网关错误处理。
					status := event.Status
					if status == 0 {
						status = http.StatusBadGateway
					}
					return Result{}, &StatusError{Code: status, Reason: event.Code}
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return Result{}, sanitizeTransportError(readErr)
			}
			break
		}
	}
	// reply 是去除首尾空白后的完整回复。
	reply := strings.TrimSpace(text.String())
	if reply == "" {
		return Result{}, ErrEmptyReply
	}
	return Result{Text: reply, SessionID: session}, nil
}

// sanitizeTransportError 返回不含请求地址的传输层错误：保留取消与超时语义，其余统一为“无法连接平台”。
func sanitizeTransportError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("无法连接平台")
}
