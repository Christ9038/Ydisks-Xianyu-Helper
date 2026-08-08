// Package ws 实现闲鱼 WebSocket 连接生命周期：握手、/reg 注册、心跳、ACK、消息解密。
package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/protocol"
)

// WSURL 闲鱼 IM WebSocket 地址。
const WSURL = "wss://wss-goofish.dingtalk.com:443"

const (
	wsOpenTimeout      = 30 * time.Second
	regResponseTimeout = 30 * time.Second
)

var (
	heartbeatResponseTimeout = 30 * time.Second
	batchConnectDelays       = []time.Duration{0, 200 * time.Millisecond, 900 * time.Millisecond, 1500 * time.Millisecond, 4 * time.Second}
)

// RegAppKey WS /reg 用的 app-key。
const RegAppKey = "444e9908a51d1cb236a27862abc769c9"

// Config 单账号 WS 连接所需的最小配置。
type Config struct {
	CookieStr   string // 完整 cookie 字符串
	DeviceID    string // generate_device_id(myid)
	AccessToken string // mtop token API 返回的 accessToken
	Recorder    func(direction, rawText, parsedJSON, parseStatus, errMsg string)
}

// Conn 包装一条已注册的 WebSocket 连接。
type Conn struct {
	ws         *websocket.Conn
	cfg        Config
	logger     *slog.Logger
	sendGate   chan struct{}
	recorderMu sync.RWMutex
	recorder   func(direction, rawText, parsedJSON, parseStatus, errMsg string)

	readCtx    context.Context
	readCancel context.CancelFunc
	readDone   chan struct{}
	initOnce   sync.Once
	readErrMu  sync.Mutex
	readErr    error

	pendingMu sync.Mutex
	pending   map[string]chan map[string]any
	pushes    chan incomingFrame
}

type incomingFrame struct {
	messageType websocket.MessageType
	data        []byte
	parsed      map[string]any
}

// SetRecorder 设置帧记录器。
func (c *Conn) SetRecorder(rec func(direction, rawText, parsedJSON, parseStatus, errMsg string)) {
	c.recorderMu.Lock()
	c.recorder = rec
	c.recorderMu.Unlock()
}

func (c *Conn) recorderSnapshot() func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
	c.recorderMu.RLock()
	recorder := c.recorder
	c.recorderMu.RUnlock()
	return recorder
}

// Dial 保留旧的一步式入口；新账号主循环使用 Open → 获取 token → Register，
// 从而与官网 authConnect 的顺序一致。
func Dial(ctx context.Context, cfg Config, logger *slog.Logger) (*Conn, error) {
	conn, err := Open(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	if err := conn.Register(ctx, cfg.DeviceID, cfg.AccessToken); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// Open 按官网 batchConnectWs 策略并行打开最多五条原生 WebSocket，由最先
// settle 的成功或失败决定本轮结果，并关闭迟到连接。此阶段不请求 token，
// 也不发送 /reg。
func Open(ctx context.Context, cfg Config, logger *slog.Logger) (*Conn, error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("正在连接闲鱼 WebSocket", "url", WSURL)
	return openBatch(ctx, WSURL, cfg, logger)
}

func websocketHeaders() http.Header {
	hdr := http.Header{}
	hdr.Set("Origin", "https://www.goofish.com")
	if ua := xianyu.CurrentBrowserFingerprint().UserAgent; ua != "" {
		hdr.Set("User-Agent", ua)
	}
	return hdr
}

var (
	chromeVersionPattern  = regexp.MustCompile(`(?:Chrome|CriOS)/([\d.]+)`)
	headlessChromePattern = regexp.MustCompile(`HeadlessChrome/([\d.]+)`)
	edgeVersionPattern    = regexp.MustCompile(`Edg(?:e|A|iOS)?/([\d.]+)`)
	firefoxVersionPattern = regexp.MustCompile(`Firefox/([\d.]+)`)
	safariVersionPattern  = regexp.MustCompile(`Version/([\d.]+).*Safari`)
	macVersionPattern     = regexp.MustCompile(`Mac OS X[ /]([\d_\.]+)`)
	windowsVersionPattern = regexp.MustCompile(`Windows NT ([\d.]+)`)
	androidVersionPattern = regexp.MustCompile(`Android[ /]([\d.]+)`)
)

// OfficialRegistrationUA mirrors IMPaaS 2.2.0's ua-parser-js composition.
// The raw UA (and therefore its browser version) comes from local Chromium;
// all wrapper fields and ordering are fixed to the official web implementation.
func OfficialRegistrationUA(rawUA string) string {
	rawUA = strings.TrimSpace(rawUA)
	if rawUA == "" {
		return ""
	}
	osName, osVersion := parseOfficialOS(rawUA)
	browserName, browserVersion := parseOfficialBrowser(rawUA)
	return strings.Join([]string{
		rawUA,
		"DingTalk(2.2.0)",
		fmt.Sprintf("OS(%s/%s)", osName, osVersion),
		fmt.Sprintf("Browser(%s/%s)", browserName, browserVersion),
		"DingWeb/2.2.0",
		"IMPaaS",
		"DingWeb/2.2.0",
	}, " ")
}

func parseOfficialOS(ua string) (string, string) {
	if match := macVersionPattern.FindStringSubmatch(ua); len(match) == 2 {
		return "Mac OS", strings.ReplaceAll(match[1], "_", ".")
	}
	if match := windowsVersionPattern.FindStringSubmatch(ua); len(match) == 2 {
		versions := map[string]string{"10.0": "10", "6.3": "8.1", "6.2": "8", "6.1": "7", "6.0": "Vista", "5.1": "XP"}
		if version := versions[match[1]]; version != "" {
			return "Windows", version
		}
		return "Windows", match[1]
	}
	if match := androidVersionPattern.FindStringSubmatch(ua); len(match) == 2 {
		return "Android", match[1]
	}
	if strings.Contains(ua, "Linux") {
		return "Linux", "other"
	}
	return "other", "other"
}

func parseOfficialBrowser(ua string) (string, string) {
	for _, candidate := range []struct {
		name    string
		pattern *regexp.Regexp
	}{{"Edge", edgeVersionPattern}, {"Chrome Headless", headlessChromePattern}, {"Chrome", chromeVersionPattern}, {"Firefox", firefoxVersionPattern}, {"Safari", safariVersionPattern}} {
		if match := candidate.pattern.FindStringSubmatch(ua); len(match) == 2 {
			return candidate.name, match[1]
		}
	}
	return "other", "other"
}

type dialResult struct {
	conn *websocket.Conn
	err  error
}

func openBatch(ctx context.Context, target string, cfg Config, logger *slog.Logger) (*Conn, error) {
	delays := append([]time.Duration(nil), batchConnectDelays...)
	if len(delays) == 0 {
		return nil, fmt.Errorf("WS dial: batchConnect 未配置竞速连接")
	}
	batchCtx, cancel := context.WithCancel(ctx)
	results := make(chan dialResult, len(delays))
	for _, delay := range delays {
		delay := delay
		go func() {
			if delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-batchCtx.Done():
					results <- dialResult{err: batchCtx.Err()}
					return
				case <-timer.C:
				}
			}
			dialCtx, dialCancel := context.WithTimeout(batchCtx, wsOpenTimeout)
			defer dialCancel()
			conn, _, err := websocket.Dial(dialCtx, target, &websocket.DialOptions{HTTPHeader: websocketHeaders()})
			results <- dialResult{conn: conn, err: err}
		}()
	}

	// 官网使用 Promise.race：第一条完成的连接无论成功或失败都会决定本轮
	// batchConnect 的结果；不会在先收到失败后继续等待其他竞速连接。
	result := <-results
	go func() {
		defer cancel()
		for i := 1; i < len(delays); i++ {
			late := <-results
			if late.conn != nil {
				_ = late.conn.CloseNow()
			}
		}
	}()
	if result.err != nil {
		if result.conn != nil {
			_ = result.conn.CloseNow()
		}
		logger.Warn("闲鱼 WebSocket 握手失败", "url", target, "err", result.err)
		return nil, fmt.Errorf("WS dial: %w", result.err)
	}
	result.conn.SetReadLimit(8 << 20)
	logger.Info("闲鱼 WebSocket 握手成功", "url", target)
	return newConn(result.conn, cfg, logger), nil
}

func newConn(raw *websocket.Conn, cfg Config, logger *slog.Logger) *Conn {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Conn{
		ws:       raw,
		cfg:      cfg,
		logger:   logger,
		sendGate: make(chan struct{}, 1),
		recorder: cfg.Recorder,
	}
	c.ensureReadPump()
	return c
}

func (c *Conn) ensureReadPump() {
	c.initOnce.Do(func() {
		if c.logger == nil {
			c.logger = slog.Default()
		}
		c.readCtx, c.readCancel = context.WithCancel(context.Background())
		c.readDone = make(chan struct{})
		c.pending = make(map[string]chan map[string]any)
		c.pushes = make(chan incomingFrame, 128)
		go c.readPump()
	})
}

// Register 发送官网最终态 /reg headers。注册后不主动构造 ackDiff。
func (c *Conn) Register(ctx context.Context, deviceID, accessToken string) error {
	c.ensureReadPump()
	c.cfg.DeviceID = deviceID
	// 官网 authConnect 在 _auth 前对 MTOP accessToken 执行
	// decodeURIComponent。保留原始值供重试，再把解码值写入 /reg。
	c.cfg.AccessToken = accessToken
	decodedAccessToken, err := url.PathUnescape(accessToken)
	if err != nil {
		return fmt.Errorf("解码 WebSocket accessToken 失败: %w", err)
	}
	if !utf8.ValidString(decodedAccessToken) {
		return fmt.Errorf("解码 WebSocket accessToken 失败: 非法 UTF-8")
	}
	response, err := c.request(ctx, "/reg", map[string]any{
		"cache-header": "app-key token ua wv",
		"app-key":      RegAppKey,
		"token":        decodedAccessToken,
		"ua":           OfficialRegistrationUA(xianyu.CurrentBrowserFingerprint().UserAgent),
		"dt":           "j",
		"wv":           "im:3,au:3,sy:6",
		"sync":         "0,0;0;0;",
		"did":          deviceID,
	}, nil, regResponseTimeout)
	if err != nil {
		return fmt.Errorf("等待 /reg 响应失败: %w", err)
	}
	code, ok := responseCode(response["code"])
	if ok && code == 200 {
		c.logger.Info("WS 注册完成")
		return nil
	}
	return newRegError(code, response)
}

// register 兼容包内旧测试。
func (c *Conn) register(ctx context.Context) error {
	return c.Register(ctx, c.cfg.DeviceID, c.cfg.AccessToken)
}

func midKey(mid string) string {
	fields := strings.Fields(mid)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func responseCode(value any) (int, bool) {
	switch code := value.(type) {
	case float64:
		return int(code), true
	case int:
		return code, true
	case json.Number:
		parsed, err := code.Int64()
		return int(parsed), err == nil
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(code), "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func (c *Conn) request(ctx context.Context, path string, headers map[string]any, body any, timeout time.Duration) (map[string]any, error) {
	c.ensureReadPump()
	requestCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	if headers == nil {
		headers = make(map[string]any)
	}
	mid := strings.TrimSpace(fmt.Sprint(headers["mid"]))
	if mid == "" || mid == "<nil>" {
		mid = protocol.GenerateMid()
		headers["mid"] = mid
	}
	key := midKey(mid)
	started := time.Now()
	c.logger.Debug("WS 请求发送", "path", path, "mid", key)
	responseCh := make(chan map[string]any, 1)
	c.pendingMu.Lock()
	c.pending[key] = responseCh
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
	}()

	frame := map[string]any{"lwp": path, "headers": headers}
	if body != nil {
		frame["body"] = body
	}
	if err := c.sendJSON(requestCtx, frame); err != nil {
		c.logger.Warn("WS 请求发送失败", "path", path, "mid", key, "duration", time.Since(started).Round(time.Millisecond), "err", err)
		return nil, err
	}
	select {
	case response := <-responseCh:
		code, _ := responseCode(response["code"])
		c.logResponse(path, key, code, time.Since(started))
		return response, nil
	case <-c.readDone:
		// readPump always dispatches a decoded response before it can observe the
		// following close. Prefer that already-resolved response over readDone,
		// matching browser event ordering (message before close).
		select {
		case response := <-responseCh:
			code, _ := responseCode(response["code"])
			c.logResponse(path, key, code, time.Since(started))
			return response, nil
		default:
		}
		err := c.connectionReadError()
		c.logger.Warn("WS 请求因连接结束失败", "path", path, "mid", key, "duration", time.Since(started).Round(time.Millisecond), "err", err)
		return nil, err
	case <-requestCtx.Done():
		err := requestCtx.Err()
		if errors.Is(err, context.Canceled) {
			c.logger.Debug("WS 请求取消", "path", path, "mid", key, "duration", time.Since(started).Round(time.Millisecond), "err", err)
		} else {
			c.logger.Warn("WS 请求超时", "path", path, "mid", key, "duration", time.Since(started).Round(time.Millisecond), "err", err)
		}
		return nil, err
	}
}

// ListUserMessages retrieves one page of official IM history for a conversation.
// The cursor is opaque to callers; zero selects the newest page.
func (c *Conn) ListUserMessages(ctx context.Context, cid string, cursor int64, limit int) (map[string]any, error) {
	cid = strings.TrimSpace(cid)
	if cid == "" {
		return nil, errors.New("聊天历史缺少会话 ID")
	}
	if !strings.Contains(cid, "@") {
		cid += "@goofish"
	}
	if cursor <= 0 {
		cursor = 9007199254740991
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	response, err := c.request(ctx, "/r/MessageManager/listUserMessages", nil,
		[]any{cid, false, cursor, limit, false}, regResponseTimeout)
	if err != nil {
		return nil, err
	}
	if code, ok := responseCode(response["code"]); ok && code != http.StatusOK {
		return nil, fmt.Errorf("聊天历史接口返回状态 %d", code)
	}
	body, ok := response["body"].(map[string]any)
	if !ok {
		return nil, errors.New("聊天历史接口响应缺少 body")
	}
	if reason := strings.TrimSpace(fmt.Sprint(body["reason"])); reason != "" && reason != "<nil>" {
		return nil, fmt.Errorf("聊天历史接口失败: %s", reason)
	}
	return body, nil
}

// ListConversations retrieves one page of the account's official IM contacts.
func (c *Conn) ListConversations(ctx context.Context, cursor int64, limit int) (map[string]any, error) {
	if cursor <= 0 {
		cursor = 9007199254740991
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	response, err := c.request(ctx, "/r/Conversation/listNewestPagination", nil, []any{cursor, limit}, regResponseTimeout)
	if err != nil {
		return nil, err
	}
	if code, ok := responseCode(response["code"]); ok && code != http.StatusOK {
		return nil, fmt.Errorf("会话列表接口返回状态 %d", code)
	}
	body, ok := response["body"].(map[string]any)
	if !ok {
		return nil, errors.New("会话列表接口响应缺少 body")
	}
	if reason := strings.TrimSpace(fmt.Sprint(body["reason"])); reason != "" && reason != "<nil>" {
		return nil, fmt.Errorf("会话列表接口失败: %s", reason)
	}
	return body, nil
}

func (c *Conn) logResponse(path, mid string, code int, duration time.Duration) {
	attrs := []any{"path", path, "mid", mid, "code", code, "duration", duration.Round(time.Millisecond)}
	if code >= 400 {
		c.logger.Warn("WS 业务响应异常", attrs...)
		return
	}
	c.logger.Debug("WS 响应收到", attrs...)
}

func (c *Conn) readPump() {
	defer close(c.readDone)
	for {
		messageType, data, err := c.ws.Read(c.readCtx)
		if err != nil {
			c.readErrMu.Lock()
			c.readErr = err
			c.readErrMu.Unlock()
			return
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			if recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", string(data), "", "non_json", err.Error())
			}
			continue
		}
		c.recordParsedIncoming(data, parsed)
		if _, hasCode := parsed["code"]; hasCode {
			if _, hasHeaders := parsed["headers"].(map[string]any); hasHeaders {
				c.dispatchResponse(parsed)
				continue
			}
		}
		lwp, hasLWP := parsed["lwp"].(string)
		_, hasHeaders := parsed["headers"].(map[string]any)
		if !hasLWP || strings.TrimSpace(lwp) == "" || !hasHeaders {
			if recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", string(data), string(data), "skip_invalid_lwp", "")
			}
			continue
		}
		incoming := incomingFrame{messageType: messageType, data: append([]byte(nil), data...), parsed: parsed}
		select {
		case c.pushes <- incoming:
		case <-c.readCtx.Done():
			return
		}
	}
}

func (c *Conn) dispatchResponse(frame map[string]any) bool {
	headers, _ := frame["headers"].(map[string]any)
	key := midKey(strings.TrimSpace(fmt.Sprint(headers["mid"])))
	c.pendingMu.Lock()
	ch := c.pending[key]
	c.pendingMu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- frame:
	default:
	}
	return true
}

func (c *Conn) connectionReadError() error {
	c.readErrMu.Lock()
	defer c.readErrMu.Unlock()
	if c.readErr != nil {
		return c.readErr
	}
	return fmt.Errorf("WebSocket 读取循环已结束")
}

func (c *Conn) recordParsedIncoming(data []byte, parsed map[string]any) {
	recorder := c.recorderSnapshot()
	if recorder == nil {
		return
	}
	parsedJSON := string(data)
	if normalized, err := json.Marshal(parsed); err == nil {
		parsedJSON = string(normalized)
	}
	recorder("in", string(data), parsedJSON, "json", "")
}

// HeartbeatLoop 对齐官网：注册后以固定 15 秒节拍发送 /!，即使上一请求仍在
// 等待也不推迟下一次；任一请求失败或 30 秒无响应即结束连接。官网只以
// Promise 是否 reject 判断心跳，不因已收到的非 200 响应主动断线。
func (c *Conn) HeartbeatLoop(ctx context.Context, interval time.Duration) error {
	c.ensureReadPump()
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	heartbeatErr := make(chan error, 1)
	for {
		select {
		case <-heartbeatCtx.Done():
			return heartbeatCtx.Err()
		case <-c.readDone:
			return c.connectionReadError()
		case err := <-heartbeatErr:
			_ = c.Close()
			return fmt.Errorf("心跳响应失败: %w", err)
		case <-ticker.C:
			go func() {
				_, err := c.request(heartbeatCtx, "/!", map[string]any{}, nil, heartbeatResponseTimeout)
				if err == nil || heartbeatCtx.Err() != nil {
					return
				}
				select {
				case heartbeatErr <- err:
				default:
				}
			}()
		}
	}
}

// ReceiveLoop 消费 readPump 分发的 Push。响应帧永远不会进入这里，因此不会被
// 错误 ACK；Push ACK 原样复用服务端完整 headers。
func (c *Conn) ReceiveLoop(ctx context.Context, onMessage func(decrypted map[string]any)) error {
	c.ensureReadPump()
	for {
		var frame incomingFrame
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.readDone:
			// onmessage is delivered before the subsequent onclose in browsers.
			// If readPump already queued a final push/control frame, consume it
			// before surfacing the close.
			select {
			case frame = <-c.pushes:
			default:
				return fmt.Errorf("WS read: %w", c.connectionReadError())
			}
		case frame = <-c.pushes:
		}
		raw := frame.parsed
		rawText := string(frame.data)
		switch strings.TrimSpace(fmt.Sprint(raw["lwp"])) {
		case "/push/kickout":
			c.readCancel()
			_ = c.ws.CloseNow()
			return &RegError{Kind: RegErrorAuthentication, Code: http.StatusUnauthorized, Reason: "server kickout"}
		case "/s/session/remove":
			c.readCancel()
			_ = c.ws.CloseNow()
			return &RegError{Kind: RegErrorConnectLimit, Code: http.StatusOK, Reason: "session remove"}
		}
		// 官网异步启动 sync state 恢复，并立即完成当前 Push handler；不能
		// 为 getState/ackDiff 最多阻塞 Push ACK 60 秒。
		go func(message map[string]any) {
			if err := c.handleSyncExtra(c.readCtx, message); err != nil && c.readCtx.Err() == nil {
				c.logger.Error("同步状态恢复失败", "err", err)
			}
		}(raw)

		// 仅处理同步包：body.syncPushPackage.data[0].data
		syncData, ok := extractSyncPayload(raw)
		if !ok {
			c.sendACK(ctx, raw)
			if recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", rawText, "", "skip_non_sync", "")
			}
			continue
		}
		decoded, err := decodeSyncData(syncData)
		if err != nil {
			c.sendACK(ctx, raw)
			if recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", rawText, "", "decrypt_failed", err.Error())
			}
			c.logger.Error("消息解密失败", "err", err)
			continue
		}
		if recorder := c.recorderSnapshot(); recorder != nil {
			if b, e := json.Marshal(decoded); e == nil {
				recorder("in", rawText, string(b), "decrypted", "")
			}
		}
		c.sendACK(ctx, raw)
		if onMessage != nil {
			onMessage(decoded)
		}
	}
}

func (c *Conn) handleSyncExtra(ctx context.Context, msg map[string]any) error {
	body, _ := msg["body"].(map[string]any)
	extra, _ := body["syncExtraType"].(map[string]any)
	typeCode, ok := responseCode(extra["type"])
	if !ok || (typeCode != 1 && typeCode != 2) {
		return nil
	}
	state, err := c.request(ctx, "/r/SyncStatus/getState", map[string]any{}, []any{map[string]any{"topic": "sync"}}, regResponseTimeout)
	if err != nil {
		return fmt.Errorf("getState: %w", err)
	}
	if code, ok := responseCode(state["code"]); !ok || code != http.StatusOK || state["body"] == nil {
		return fmt.Errorf("getState 返回异常: code=%v", state["code"])
	}
	response, err := c.request(ctx, "/r/SyncStatus/ackDiff", map[string]any{}, []any{state["body"]}, regResponseTimeout)
	if err != nil {
		return fmt.Errorf("ackDiff: %w", err)
	}
	if code, ok := responseCode(response["code"]); ok && code != http.StatusOK {
		return fmt.Errorf("ackDiff 返回异常: code=%d", code)
	}
	return nil
}

// sendACK 回复 {"code":200, headers:<服务端完整 headers>}。
func (c *Conn) sendACK(ctx context.Context, msg map[string]any) {
	headers, _ := msg["headers"].(map[string]any)
	ackHeaders := make(map[string]any, len(headers))
	for key, value := range headers {
		ackHeaders[key] = value
	}
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
	body, _ := msg["body"].(map[string]any)
	if body == nil {
		return "", false
	}
	pkg, _ := body["syncPushPackage"].(map[string]any)
	if pkg == nil {
		return "", false
	}
	arr, _ := pkg["data"].([]any)
	if len(arr) == 0 {
		return "", false
	}
	first, _ := arr[0].(map[string]any)
	if first == nil {
		return "", false
	}
	d, ok := first["data"].(string)
	return d, ok && d != ""
}

// decodeSyncData 先尝试 base64+JSON（未加密系统消息），失败则 base64+msgpack 解密。
func decodeSyncData(data string) (map[string]any, error) {
	// 1) base64 解码后尝试解析 JSON。
	if dec, err := base64.StdEncoding.DecodeString(data); err == nil {
		var parsed map[string]any
		if jsonErr := json.Unmarshal(dec, &parsed); jsonErr == nil {
			return parsed, nil
		}
	}
	// 2) JSON 解析失败 → msgpack 解密
	out, err := protocol.Decrypt(data)
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("解密后非 JSON: %w", err)
	}
	return parsed, nil
}

// sendJSON 发送一条 JSON 文本帧。
func (c *Conn) sendJSON(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if recorder := c.recorderSnapshot(); recorder != nil {
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
	content := map[string]any{
		"contentType": 1,
		"text": map[string]any{
			"text": text,
		},
	}
	return c.sendChatContent(ctx, myID, cid, toID, content)
}

// SendImage 发送一条闲鱼聊天图片消息。imageURL 应为闲鱼可访问的 CDN/公网 URL。
func (c *Conn) SendImage(ctx context.Context, myID, cid, toID, imageURL string, width, height int) error {
	if width <= 0 {
		width = 800
	}
	if height <= 0 {
		height = 600
	}
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

func (c *Conn) sendChatContent(ctx context.Context, myID, cid, toID string, content any) error {
	myID = stripGoofish(myID)
	cid = stripGoofish(cid)
	toID = stripGoofish(toID)
	if myID == "" || cid == "" || toID == "" {
		return fmt.Errorf("发送消息缺少必要参数: myID=%q cid=%q toID=%q", myID, cid, toID)
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
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
