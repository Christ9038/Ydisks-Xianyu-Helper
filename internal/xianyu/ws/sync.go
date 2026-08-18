package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"xianyu-go/internal/xianyu/protocol"
)

// handleSyncExtra 处理服务端增量同步帧并在需要时回传确认，返回解码或发送错误。
func (c *Conn) handleSyncExtra(ctx context.Context, msg map[string]any) error {
	// body 用于本次流程后续判断的请求体
	body, _ := msg["body"].(map[string]any)
	// extra 用于本次流程后续判断的extra
	extra, _ := body["syncExtraType"].(map[string]any)
	// typeCode、ok 用于本次流程后续判断的类型Code、ok
	typeCode, ok := responseCode(extra["type"])
	if !ok || (typeCode != 1 && typeCode != 2) {
		return nil
	}
	// state、err 用于本次流程后续判断的state、err
	state, err := c.request(ctx, "/r/SyncStatus/getState", map[string]any{}, []any{map[string]any{"topic": "sync"}}, regResponseTimeout)
	if err != nil {
		return fmt.Errorf("getState: %w", err)
	}
	if // code、ok 用于本次流程后续判断的code、ok
	code, ok := responseCode(state["code"]); !ok || code != http.StatusOK || state["body"] == nil {
		return fmt.Errorf("getState 返回异常: code=%v", state["code"])
	}
	// response、err 用于本次流程后续判断的response、err
	response, err := c.request(ctx, "/r/SyncStatus/ackDiff", map[string]any{}, []any{state["body"]}, regResponseTimeout)
	if err != nil {
		return fmt.Errorf("ackDiff: %w", err)
	}
	if // code、ok 用于本次流程后续判断的code、ok
	code, ok := responseCode(response["code"]); ok && code != http.StatusOK {
		return fmt.Errorf("ackDiff 返回异常: code=%d", code)
	}
	return nil
}

// sendACK 回复 {"code":200, headers:<服务端完整 headers>}。
func (c *Conn) sendACK(ctx context.Context, msg map[string]any) {
	// headers 用于本次流程后续判断的headers
	headers, _ := msg["headers"].(map[string]any)
	// ackHeaders 用于本次流程后续判断的ackHeaders
	ackHeaders := make(map[string]any, len(headers))
	// key、value 表示当前遍历过程中的key、value
	for key, value := range headers {
		ackHeaders[key] = value
	}
	// ack 用于本次流程后续判断的ack
	ack := map[string]any{
		"code":    200,
		"headers": ackHeaders,
	}
	// ACK 失败不阻塞主循环。
	ackCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_ = c.sendJSON(ackCtx, ack)
	cancel()
}

// extractSyncPayload 取出 body.syncPushPackage.data[0].data（字符串）。
func extractSyncPayload(msg map[string]any) (string, bool) {
	// body 用于本次流程后续判断的请求体
	body, _ := msg["body"].(map[string]any)
	if body == nil {
		return "", false
	}
	// pkg 用于本次流程后续判断的pkg
	pkg, _ := body["syncPushPackage"].(map[string]any)
	if pkg == nil {
		return "", false
	}
	// arr 用于本次流程后续判断的arr
	arr, _ := pkg["data"].([]any)
	if len(arr) == 0 {
		return "", false
	}
	// first 用于本次流程后续判断的first
	first, _ := arr[0].(map[string]any)
	if first == nil {
		return "", false
	}
	// d、ok 用于本次流程后续判断的d、ok
	d, ok := first["data"].(string)
	return d, ok && d != ""
}

// decodeSyncData 先尝试 base64+JSON（未加密系统消息），失败则 base64+msgpack 解密。
func decodeSyncData(data string) (map[string]any, error) {
	// 1) base64 解码后尝试解析 JSON。
	if dec, err := base64.StdEncoding.DecodeString(data); err == nil {
		// parsed 用于本次流程后续判断的解析结果
		var parsed map[string]any
		if // jsonErr 用于本次流程后续判断的jsonErr
		jsonErr := json.Unmarshal(dec, &parsed); jsonErr == nil {
			return parsed, nil
		}
	}
	// 2) JSON 解析失败 → msgpack 解密
	out, err := protocol.Decrypt(data)
	if err != nil {
		return nil, err
	}
	// parsed 用于本次流程后续判断的解析结果
	var parsed map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("解密后非 JSON: %w", err)
	}
	return parsed, nil
}

// sendJSON 发送一条 JSON 文本帧。
func (c *Conn) sendJSON(ctx context.Context, v any) error {
	// b、err 用于本次流程后续判断的b、err
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if // recorder 用于本次流程后续判断的recorder
	recorder := c.recorderSnapshot(); recorder != nil {
		recorder("out", string(b), string(b), "json", "")
	}
	select {
	case c.sendGate <- struct{}{}:
		defer func() { <-c.sendGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// SendText 发送一条闲鱼聊天文本消息。
func (c *Conn) SendText(ctx context.Context, myID, cid, toID, text string) error {
	// content 用于本次流程后续判断的内容
	content := map[string]any{
		"contentType": 1,
		"text": map[string]any{
			"text": text,
		},
	}
	return c.sendChatContent(ctx, myID, cid, toID, content)
}

// MarkChatRead 将当前会话的 PNM 消息 ID 上报为已读。
// ctx 控制远端请求生命周期；cid 仅用于本地可观测日志；messageIDs 为待上报消息对象。
// 返回值仅报告远端调用失败，平台拒绝会被记录为告警以保留既有调用兼容性。
func (c *Conn) MarkChatRead(ctx context.Context, cid string, messageIDs []map[string]any) error {
	// ids 是剔除空值后的 PNM ID 列表，按平台 MessageStatusService 的参数格式发送。
	ids := make([]string, 0, len(messageIDs))
	// item 为调用方传入的一条待读消息对象，可能缺少平台消息 ID。
	for _, item := range messageIDs {
		// id 是当前对象中可上报的非空 PNM 消息 ID。
		if id := strings.TrimSpace(fmt.Sprint(item["messageId"])); id != "" && id != "<nil>" {
			ids = append(ids, id)
		}
	}
	c.logger.Info("准备上报闲鱼已读", "cid", cid, "message_count", len(ids), "message_ids", ids)
	// response 保存平台响应；err 表示请求或传输失败。服务只接受一个 string 列表参数。
	response, err := c.request(ctx, "/r/MessageStatus/read", map[string]any{}, []any{ids}, regResponseTimeout)
	if err == nil {
		// code 是平台业务状态码；ok 表示响应中的状态码可被规范解析。
		if code, ok := responseCode(response["code"]); ok && code >= 400 {
			c.logger.Warn("闲鱼已读上报被拒绝", "cid", cid, "message_count", len(ids), "code", code, "body", response["body"])
		} else {
			c.logger.Info("闲鱼已读上报成功", "cid", cid, "message_count", len(ids), "message_ids", ids, "code", response["code"])
		}
	}
	return err
}

// SendImage 发送一条闲鱼聊天图片消息。imageURL 应为闲鱼可访问的 CDN/公网 URL。
func (c *Conn) SendImage(ctx context.Context, myID, cid, toID, imageURL string, width, height int) error {
	if width <= 0 {
		width = 800
	}
	if height <= 0 {
		height = 600
	}
	// content 用于本次流程后续判断的内容
	content := map[string]any{
		"contentType": 2,
		"image": map[string]any{
			"pics": []map[string]any{{
				"height": height,
				"type":   0,
				"url":    imageURL,
				"width":  width,
			}},
		},
	}
	return c.sendChatContent(ctx, myID, cid, toID, content)
}

// sendChatContent 封装send聊天内容业务协调。
func (c *Conn) sendChatContent(ctx context.Context, myID, cid, toID string, content any) error {
	myID = stripGoofish(myID)
	cid = stripGoofish(cid)
	toID = stripGoofish(toID)
	if myID == "" || cid == "" || toID == "" {
		return fmt.Errorf("发送消息缺少必要参数: myID=%q cid=%q toID=%q", myID, cid, toID)
	}
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	// encoded 用于本次流程后续判断的encoded
	encoded := base64.StdEncoding.EncodeToString(raw)
	// msg 用于本次流程后续判断的msg
	msg := map[string]any{
		"lwp": "/r/MessageSend/sendByReceiverScope",
		"headers": map[string]any{
			"mid": protocol.GenerateMid(),
		},
		"body": []any{
			map[string]any{
				"uuid":             protocol.GenerateUUID(),
				"cid":              cid + "@goofish",
				"conversationType": 1,
				"content": map[string]any{
					"contentType": 101,
					"custom": map[string]any{
						"type": 1,
						"data": encoded,
					},
				},
				"redPointPolicy": 0,
				"extension": map[string]any{
					"extJson": "{}",
				},
				"ctx": map[string]any{
					"appVersion": "1.0",
					"platform":   "web",
				},
				"mtags":                map[string]any{},
				"msgReadStatusSetting": 1,
			},
			map[string]any{
				"actualReceivers": []string{
					toID + "@goofish",
					myID + "@goofish",
				},
			},
		},
	}
	return c.sendJSON(ctx, msg)
}

// stripGoofish 封装stripGoofish业务协调。
func stripGoofish(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSuffix(s, "@goofish")
}

// Close 关闭连接。
func (c *Conn) Close() error {
	c.ensureReadPump()
	c.readCancel()
	return c.ws.Close(websocket.StatusNormalClosure, "bye")
}
