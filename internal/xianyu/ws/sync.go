package ws

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"xianyu-go/internal/xianyu/protocol"
)

// OutgoingEcho 是平台 sendByReceiverScope 成功响应中可被严格核验的一条出站消息摘要。
// 它不包含 Cookie、Token 或原始平台帧，只供账号运行时唤醒同一次自动化发送的确认等待器。
type OutgoingEcho struct {
	// RequestID 是本次 sendByReceiverScope 请求使用的 mid，用于把平台响应精确关联到发起请求。
	RequestID string
	// ChatID 是去除 IM 协议后缀后的平台会话标识。
	ChatID string
	// BuyerID 是本次发送时明确指定的会话对端平台标识。
	BuyerID string
	// MessageKey 是平台为已经接纳的消息分配的 PNM 标识。
	MessageKey string
	// MessageType 是与账号级回显等待器匹配的 text 或 image 类型。
	MessageType string
	// Text 是文本消息的规范正文；非文本消息保持空字符串。
	Text string
	// Content 是图片消息的首张 URL；非图片消息保持空字符串。
	Content string
}

// outgoingRequestIDContextKey 保存发送调用要求使用的请求级 mid；仅由本包内部读取，避免改变公共发送方法签名。
type outgoingRequestIDContextKey struct{}

// WithOutgoingRequestID 将请求级 mid 放入发送上下文，使响应观察器可以精确关联并发的相同正文消息。
func WithOutgoingRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, outgoingRequestIDContextKey{}, strings.TrimSpace(requestID))
}

// outgoingRequestID 从发送上下文读取调用方预先分配的请求级 mid；缺失时由请求层继续生成。
func outgoingRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	// requestID 保存上下文中的发送请求 mid；缺失或非字符串值按未提供处理。
	requestID, _ := ctx.Value(outgoingRequestIDContextKey{}).(string)
	return strings.TrimSpace(requestID)
}

// handleSyncExtra 处理服务端增量同步帧并在需要时回传确认，返回解码或发送错误。
func (c *Conn) handleSyncExtra(ctx context.Context, msg map[string]any) error {
	if !syncExtraNeedsRecovery(msg) {
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

// syncExtraNeedsRecovery 判断 Push 是否携带需要 getState/ackDiff 恢复的 syncExtraType（类型 1 或 2）。
func syncExtraNeedsRecovery(msg map[string]any) bool {
	// body 是 Push 的请求体，缺失时按空对象处理。
	body, _ := msg["body"].(map[string]any)
	// extra 是请求体中的同步附加类型。
	extra, _ := body["syncExtraType"].(map[string]any)
	// typeCode、ok 是附加类型码及其是否可解析。
	typeCode, ok := responseCode(extra["type"])
	return ok && (typeCode == 1 || typeCode == 2)
}

// startSyncRecovery 在后台合并执行同步状态恢复，保证同一连接上最多只有一个恢复 goroutine。
//
// 生命周期：goroutine 由 bg 登记，owner 是 Conn；Context 取自 readCtx，Close 先置 syncClosed 拒绝新任务，
// 再取消 readCtx 并 Wait，因此不会在关闭后遗留。并发契约：syncMu 保护 syncClosed、syncRunning、syncRerun。
// 恢复进行中又收到新的需恢复 Push 时只记录一次补跑，当前一轮结束后再执行一次，避免并发重复 getState/ackDiff 又不丢事件。
func (c *Conn) startSyncRecovery(msg map[string]any) {
	if !syncExtraNeedsRecovery(msg) {
		return
	}
	c.syncMu.Lock()
	if c.syncClosed {
		c.syncMu.Unlock()
		return
	}
	if c.syncRunning {
		c.syncRerun = true
		c.syncMu.Unlock()
		return
	}
	c.syncRunning = true
	// Add 在持锁且连接未关闭时执行，保证不会与 Close 中的 Wait 并发。
	c.bg.Add(1)
	c.syncMu.Unlock()
	go func() {
		defer c.bg.Done()
		for {
			// err 是单轮同步恢复（getState/ackDiff）的错误；连接已关闭导致的取消不记录。
			if err := c.handleSyncExtra(c.readCtx, msg); err != nil && c.readCtx.Err() == nil {
				c.logger.Error("同步状态恢复失败", "err", err)
			}
			c.syncMu.Lock()
			if c.syncRerun && !c.syncClosed && c.readCtx.Err() == nil {
				c.syncRerun = false
				c.syncMu.Unlock()
				continue
			}
			c.syncRunning, c.syncRerun = false, false
			c.syncMu.Unlock()
			return
		}
	}()
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

// syncPayloadEntry 表示同步帧 data 数组中的一个待解码条目。
// index 保留其在原始帧中的顺序，data 只在 valid 为真时可传给协议解码器，invalidReason 用于不记录原始密文的告警。
type syncPayloadEntry struct {
	index         int
	data          string
	valid         bool
	invalidReason string
}

// extractSyncPayloads 取出 body.syncPushPackage.data 中的全部条目，保留无效条目以便调用方留下可观测告警。
func extractSyncPayloads(msg map[string]any) ([]syncPayloadEntry, bool) {
	// body 用于本次流程后续判断的请求体
	body, _ := msg["body"].(map[string]any)
	if body == nil {
		return nil, false
	}
	// pkg 用于本次流程后续判断的pkg
	pkg, _ := body["syncPushPackage"].(map[string]any)
	if pkg == nil {
		return nil, false
	}
	// arr 用于本次流程后续判断的arr
	arr, ok := pkg["data"].([]any)
	if !ok || len(arr) == 0 {
		return nil, false
	}
	// entries 按原始顺序保存所有同步条目，使同一帧的后续付款卡片不会被 data[0] 遮蔽。
	entries := make([]syncPayloadEntry, 0, len(arr))
	// index 和 rawEntry 分别表示条目在平台帧中的位置及其未可信的原始对象。
	for index, rawEntry := range arr {
		// entry 是符合平台对象形状的同步条目；非对象条目无法交给解码器。
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			entries = append(entries, syncPayloadEntry{index: index, invalidReason: "条目不是对象"})
			continue
		}
		// data 和 dataOK 分别保存协议密文及其字符串类型校验结果。
		data, dataOK := entry["data"].(string)
		if !dataOK || strings.TrimSpace(data) == "" {
			entries = append(entries, syncPayloadEntry{index: index, invalidReason: "缺少非空字符串 data"})
			continue
		}
		entries = append(entries, syncPayloadEntry{index: index, data: data, valid: true})
	}
	return entries, true
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
	// ids 是剔除空值后的平台消息 ID 列表，按 MessageStatusService 的参数格式发送。
	ids := make([]string, 0, len(messageIDs))
	// item 为调用方传入的一条待读消息对象，可能缺少平台消息 ID。
	for _, item := range messageIDs {
		// id 是当前对象中可上报的非空 PNM 消息 ID。
		if id := strings.TrimSpace(fmt.Sprint(item["messageId"])); id != "" && id != "<nil>" {
			// 闲鱼已读接口只接受消息模型返回的 PNM 标识；关联 UUID 会导致整批请求被平台拒绝。
			if !strings.HasSuffix(id, ".PNM") {
				continue
			}
			ids = append(ids, id)
		}
	}
	c.logger.Debug("准备上报闲鱼已读", "cid", cid, "message_count", len(ids), "message_ids", ids)
	if len(ids) == 0 {
		return nil
	}
	// 每条消息单独请求，严格匹配官方页面的 body:[[messageId]] 形式，避免批量请求只推进到部分消息。
	for _, id := range ids {
		// response 保存当前单条平台响应；err 表示请求或传输失败。
		response, err := c.request(ctx, "/r/MessageStatus/read", map[string]any{}, []any{[]string{id}}, regResponseTimeout)
		if err != nil {
			return err
		}
		// code 是平台业务状态码；ok 表示响应中的状态码可被规范解析。
		if code, ok := responseCode(response["code"]); ok && code >= 400 {
			c.logger.Warn("闲鱼已读上报被拒绝", "cid", cid, "message_count", 1, "code", code, "body", response["body"])
			continue
		}
		c.logger.Debug("闲鱼已读上报成功", "cid", cid, "message_count", 1, "message_ids", []string{id}, "code", response["code"])
	}
	return nil
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

// SendItemCard 发送一条个人会话商品卡片，载荷与闲鱼 PC IM contentType=7 协议保持一致。
func (c *Conn) SendItemCard(ctx context.Context, myID, cid, toID, itemID, title, imageURL, price string) error {
	// normalizedItemID 和 normalizedTitle 是去除首尾空白的商品身份字段。
	normalizedItemID, normalizedTitle := strings.TrimSpace(itemID), strings.TrimSpace(title)
	// normalizedImageURL 和 normalizedPrice 是去除首尾空白及已有货币符号的展示字段。
	normalizedImageURL, normalizedPrice := strings.TrimSpace(imageURL), strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(price), "¥"))
	if normalizedItemID == "" || normalizedTitle == "" || normalizedImageURL == "" || normalizedPrice == "" {
		return fmt.Errorf("发送商品卡片缺少必要字段")
	}
	// content 是官网个人会话商品卡片的内层消息正文。
	content := map[string]any{
		"contentType": 7,
		"itemCard": map[string]any{
			"itemTip": "我想要",
			"item":    map[string]any{"itemId": normalizedItemID, "mainPic": normalizedImageURL, "price": "¥" + normalizedPrice, "title": normalizedTitle},
		},
	}
	return c.sendChatContent(ctx, myID, cid, toID, content)
}

// sendChatContent 封装send聊天内容业务协调。
func (c *Conn) sendChatContent(ctx context.Context, myID, cid, toID string, content any) error {
	// err 保存发送开始前的取消状态。
	if err := ctx.Err(); err != nil {
		return &SendError{Kind: SendNotSent, Err: err}
	}
	myID = stripGoofish(myID)
	cid = stripGoofish(cid)
	toID = stripGoofish(toID)
	if myID == "" || cid == "" || toID == "" {
		return &SendError{Kind: SendNotSent, Err: fmt.Errorf("发送消息缺少必要参数")}
	}
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := json.Marshal(content)
	if err != nil {
		return &SendError{Kind: SendNotSent, Err: err}
	}
	// encoded 用于本次流程后续判断的encoded
	encoded := base64.StdEncoding.EncodeToString(raw)
	// mid 保存一次平台发送使用的请求级标识；自动化调用由上层注入，相同请求链路必须复用它。
	mid := outgoingRequestID(ctx)
	if mid == "" {
		mid = protocol.GenerateMid()
	}
	// headers 保存一次平台发送使用的 mid；请求确认必须使用同一个 mid。
	headers := map[string]any{"mid": mid}
	// body 保存与既有协议完全一致的消息载荷。
	body := []any{
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
	}
	// response 保存平台对本次发送的确认；该请求不会额外创建连接或增加闲鱼调用次数。
	response, err := c.request(ctx, "/r/MessageSend/sendByReceiverScope", headers, body, regResponseTimeout)
	if err != nil {
		return &SendError{Kind: SendUncertain, Err: err}
	}
	// code 和 ok 保存平台发送确认状态码及其严格可解析性。
	code, ok := strictChatSendResponseCode(response["code"])
	if !ok {
		return &SendError{Kind: SendUncertain, Code: code}
	}
	if code == http.StatusOK {
		// 仅在响应中的平台正文与本次发送内容逐字节一致时观察自身回显，不能把裸 200 ACK 误当作投递确认。
		c.observeOutgoingResponse(response, mid, myID, cid, toID, raw)
		return nil
	}
	if code >= http.StatusBadRequest && code < http.StatusInternalServerError && code != http.StatusRequestTimeout {
		return &SendError{Kind: SendRejected, Code: code}
	}
	return &SendError{Kind: SendUncertain, Code: code}
}

// observeOutgoingResponse 将带有可核验正文的成功发送响应转换为账号运行时可消费的出站观察事件。
// response 属于当前请求的同一 mid；myID、cid、toID 和 sentContent 是刚刚写入该请求的身份与正文快照。
// 校验失败时保持旧的同步推送回显路径，绝不把未知响应当作自动化成功。
func (c *Conn) observeOutgoingResponse(response map[string]any, requestID, myID, cid, toID string, sentContent []byte) {
	if c == nil {
		return
	}
	// observe 是当前连接配置的消费回调；在锁外调用以免观察器反向触发账号运行时锁竞争。
	observe := c.cfg.ObserveOutgoing
	if observe == nil {
		return
	}
	// echo 和 ok 表示平台响应是否完整、归属正确且携带与本次请求完全一致的消息正文。
	echo, ok := outgoingEchoFromResponse(response, requestID, myID, cid, toID, sentContent)
	if !ok {
		return
	}
	observe(echo)
}

// outgoingEchoFromResponse 严格验证 sendByReceiverScope 响应中的消息正文、发送者和 PNM ID。
// 只有消息正文与本次请求的已编码载荷完全相同时才返回观察结果，防止并发响应或平台异常回包误唤醒等待器。
func outgoingEchoFromResponse(response map[string]any, requestID, myID, cid, toID string, sentContent []byte) (OutgoingEcho, bool) {
	// body 和 bodyOK 保存平台成功响应的业务主体；缺失主体的 200 仍只表示请求确认。
	body, bodyOK := response["body"].(map[string]any)
	if !bodyOK {
		return OutgoingEcho{}, false
	}
	// messageKey 是平台接纳消息后分配的 PNM 标识；空值时无法把响应作为可审计回显。
	messageKey := strings.TrimSpace(fmt.Sprint(body["messageId"]))
	if messageKey == "" || messageKey == "<nil>" {
		return OutgoingEcho{}, false
	}
	// extension 和 extensionOK 保存展示与归属字段；发送者必须与当前请求账号一致。
	extension, extensionOK := body["extension"].(map[string]any)
	if !extensionOK {
		return OutgoingEcho{}, false
	}
	// senderID 是响应声明的出站发送者，统一移除 IM 后缀后与请求账号比较。
	senderID := stripGoofish(fmt.Sprint(extension["senderUserId"]))
	if senderID == "" || senderID != stripGoofish(myID) {
		return OutgoingEcho{}, false
	}
	// responseContent 和 contentOK 保存平台回显的 101 外层内容；其中 custom.data 必须与本次原始正文一致。
	responseContent, contentOK := body["content"].(map[string]any)
	if !contentOK {
		return OutgoingEcho{}, false
	}
	// custom 和 customOK 保存外层内容中的 Base64 编码内层消息正文。
	custom, customOK := responseContent["custom"].(map[string]any)
	if !customOK {
		return OutgoingEcho{}, false
	}
	// encoded 是平台回显的内层消息正文；它必须可以解码并逐字节等于本次请求载荷。
	encoded, encodedOK := custom["data"].(string)
	if !encodedOK {
		return OutgoingEcho{}, false
	}
	// echoedContent 和 decodeErr 保存回显正文的 Base64 解码结果及其格式错误。
	echoedContent, decodeErr := base64.StdEncoding.DecodeString(encoded)
	if decodeErr != nil || !bytes.Equal(echoedContent, sentContent) {
		return OutgoingEcho{}, false
	}
	// messageType、text、content 和 parsed 表示仅支持自动化确认所需的文本或图片正文；其它类型继续等待正常同步推送。
	messageType, text, content, parsed := outgoingEchoContent(sentContent)
	if !parsed {
		return OutgoingEcho{}, false
	}
	return OutgoingEcho{RequestID: strings.TrimSpace(requestID), ChatID: stripGoofish(cid), BuyerID: stripGoofish(toID), MessageKey: messageKey, MessageType: messageType, Text: text, Content: content}, true
}

// outgoingEchoContent 从已经验证的请求正文提取等待器的比较字段。
// 返回 false 表示内容不是当前自动化确认支持的文本或单图消息，调用方必须保留同步推送回显的原有语义。
func outgoingEchoContent(sentContent []byte) (messageType, text, content string, ok bool) {
	// payload 保存发送时序列化的内层 JSON 正文。
	var payload map[string]any
	// decodeErr 是内层 JSON 正文的解析错误；解析失败时不能据此确认自动化投递。
	if decodeErr := json.Unmarshal(sentContent, &payload); decodeErr != nil {
		return "", "", "", false
	}
	// typeCode 和 typeOK 保存内层消息类型；只接受完整整数形式以避免兼容字段截断。
	typeCode, typeOK := strictChatSendResponseCode(payload["contentType"])
	if !typeOK {
		return "", "", "", false
	}
	switch typeCode {
	case 1:
		// textBody 和 textOK 保存文字消息节点；空白文本不具备自动化确认价值。
		textBody, textOK := payload["text"].(map[string]any)
		if !textOK {
			return "", "", "", false
		}
		// textValue 是需要与等待器严格比较的文本正文。
		textValue := strings.TrimSpace(fmt.Sprint(textBody["text"]))
		if textValue == "" || textValue == "<nil>" {
			return "", "", "", false
		}
		return "text", textValue, "", true
	case 2:
		// image 和 imageOK 保存图片消息节点；当前自动化每次仅确认第一张上传后的远程图片。
		image, imageOK := payload["image"].(map[string]any)
		if !imageOK {
			return "", "", "", false
		}
		// pictures 和 picturesOK 保存平台图片列表；空列表无法确认已投递的目标媒体。
		pictures, picturesOK := image["pics"].([]any)
		if !picturesOK || len(pictures) == 0 {
			return "", "", "", false
		}
		// firstPicture 和 pictureOK 保存发送顺序中的第一张图片属性。
		firstPicture, pictureOK := pictures[0].(map[string]any)
		if !pictureOK {
			return "", "", "", false
		}
		// imageURL 是图片回显等待器使用的稳定远程地址。
		imageURL := strings.TrimSpace(fmt.Sprint(firstPicture["url"]))
		if imageURL == "" || imageURL == "<nil>" {
			return "", "", "", false
		}
		return "image", "", imageURL, true
	default:
		return "", "", "", false
	}
}

// strictChatSendResponseCode 只接受完整整数形式的聊天发送状态码，避免截断浮点数或接受带尾随字符的字符串。
func strictChatSendResponseCode(value any) (int, bool) {
	switch // code 是当前待严格校验的平台状态码具体类型和值。
	code := value.(type) {
	case int:
		return code, true
	case float64:
		if math.IsNaN(code) || math.IsInf(code, 0) || code != math.Trunc(code) || code > float64(math.MaxInt) || code < float64(math.MinInt) {
			return 0, false
		}
		return int(code), true
	case json.Number:
		// parsed 和 err 保存不允许小数形式的 JSON 整数及解析错误。
		parsed, err := code.Int64()
		if err != nil || int64(int(parsed)) != parsed {
			return 0, false
		}
		return int(parsed), true
	case string:
		// raw 保存去除协议外围空白后的完整状态码文本。
		raw := strings.TrimSpace(code)
		if raw == "" {
			return 0, false
		}
		// digit 是当前参与严格数字校验的字符，不允许符号、小数点或尾随文本。
		for _, digit := range raw {
			if digit < '0' || digit > '9' {
				return 0, false
			}
		}
		// parsed 和 err 保存完整十进制状态码及溢出错误。
		parsed, err := strconv.Atoi(raw)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// stripGoofish 封装stripGoofish业务协调。
func stripGoofish(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSuffix(s, "@goofish")
}

// Close 关闭连接。
// 关闭后会等待同步恢复 goroutine 退出；它们使用 readCtx，取消后在有限时间内返回。可重复调用。
func (c *Conn) Close() error {
	c.ensureReadPump()
	c.syncMu.Lock()
	c.syncClosed = true
	c.syncMu.Unlock()
	c.readCancel()
	// closeErr 是 WebSocket 关闭握手的结果。
	closeErr := c.ws.Close(websocket.StatusNormalClosure, "bye")
	c.bg.Wait()
	return closeErr
}
