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

// wsOpenTimeout 保存wsOpenTimeout，供当前处理流程使用
const (
	wsOpenTimeout      = 30 * time.Second
	regResponseTimeout = 30 * time.Second
)

// heartbeatResponseTimeout 保存heartbeat响应Timeout，供当前处理流程使用
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

// incomingFrame 保存incomingFrame，供当前处理流程使用
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

// recorderSnapshot 负责recorderSnapshot相关处理。
func (c *Conn) recorderSnapshot() func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
	c.recorderMu.RLock()
	// recorder 保存recorder，供当前处理流程使用
	recorder := c.recorder
	c.recorderMu.RUnlock()
	return recorder
}

// Dial 保留旧的一步式入口；新账号主循环使用 Open → 获取 token → Register，
// 从而与官网 authConnect 的顺序一致。
// Dial 负责Dial相关处理。
func Dial(ctx context.Context, cfg Config, logger *slog.Logger) (*Conn, error) {
	// conn、err 保存conn、err，供当前处理流程使用
	conn, err := Open(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	if // err 保存err，供当前处理流程使用
	err := conn.Register(ctx, cfg.DeviceID, cfg.AccessToken); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// Open 按官网 batchConnectWs 策略并行打开最多五条原生 WebSocket，由最先
// settle 的成功或失败决定本轮结果，并关闭迟到连接。此阶段不请求 token，
// 也不发送 /reg。
// Open 打开当前值。
func Open(ctx context.Context, cfg Config, logger *slog.Logger) (*Conn, error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("正在连接闲鱼 WebSocket", "url", WSURL)
	return openBatch(ctx, WSURL, cfg, logger)
}

// websocketHeaders 负责websocketHeaders相关处理。
func websocketHeaders() http.Header {
	// hdr 保存hdr，供当前处理流程使用
	hdr := http.Header{}
	hdr.Set("Origin", "https://www.goofish.com")
	if // ua 保存ua，供当前处理流程使用
	ua := xianyu.CurrentBrowserFingerprint().UserAgent; ua != "" {
		hdr.Set("User-Agent", ua)
	}
	return hdr
}

// chromeVersionPattern 保存chromeVersionPattern，供当前处理流程使用
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
// OfficialRegistrationUA 负责OfficialRegistrationUA相关处理。
func OfficialRegistrationUA(rawUA string) string {
	rawUA = strings.TrimSpace(rawUA)
	if rawUA == "" {
		return ""
	}
	// osName、osVersion 保存osName、osVersion，供当前处理流程使用
	osName, osVersion := parseOfficialOS(rawUA)
	// browserName、browserVersion 保存浏览器Name、browserVersion，供当前处理流程使用
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

// parseOfficialOS 负责parseOfficialOS相关处理。
func parseOfficialOS(ua string) (string, string) {
	if // match 保存match，供当前处理流程使用
	match := macVersionPattern.FindStringSubmatch(ua); len(match) == 2 {
		return "Mac OS", strings.ReplaceAll(match[1], "_", ".")
	}
	if // match 保存match，供当前处理流程使用
	match := windowsVersionPattern.FindStringSubmatch(ua); len(match) == 2 {
		// versions 保存versions，供当前处理流程使用
		versions := map[string]string{"10.0": "10", "6.3": "8.1", "6.2": "8", "6.1": "7", "6.0": "Vista", "5.1": "XP"}
		if // version 保存version，供当前处理流程使用
		version := versions[match[1]]; version != "" {
			return "Windows", version
		}
		return "Windows", match[1]
	}
	if // match 保存match，供当前处理流程使用
	match := androidVersionPattern.FindStringSubmatch(ua); len(match) == 2 {
		return "Android", match[1]
	}
	if strings.Contains(ua, "Linux") {
		return "Linux", "other"
	}
	return "other", "other"
}

// parseOfficialBrowser 负责parseOfficial浏览器相关处理。
func parseOfficialBrowser(ua string) (string, string) {
	// candidate 表示当前遍历过程中的candidate
	for _, candidate := range []struct {
		name    string
		pattern *regexp.Regexp
	}{{"Edge", edgeVersionPattern}, {"Chrome Headless", headlessChromePattern}, {"Chrome", chromeVersionPattern}, {"Firefox", firefoxVersionPattern}, {"Safari", safariVersionPattern}} {
		if // match 保存match，供当前处理流程使用
		match := candidate.pattern.FindStringSubmatch(ua); len(match) == 2 {
			return candidate.name, match[1]
		}
	}
	return "other", "other"
}

// dialResult 保存dial结果，供当前处理流程使用
type dialResult struct {
	conn *websocket.Conn
	err  error
}

// openBatch 负责open批次相关处理。
func openBatch(ctx context.Context, target string, cfg Config, logger *slog.Logger) (*Conn, error) {
	// delays 保存delays，供当前处理流程使用
	delays := append([]time.Duration(nil), batchConnectDelays...)
	if len(delays) == 0 {
		return nil, fmt.Errorf("WS dial: batchConnect 未配置竞速连接")
	}
	// batchCtx、cancel 保存批次Ctx、cancel，供当前处理流程使用
	batchCtx, cancel := context.WithCancel(ctx)
	// results 保存results，供当前处理流程使用
	results := make(chan dialResult, len(delays))
	// delay 表示当前遍历过程中的延迟
	for _, delay := range delays {
		// delay 保存延迟，供当前处理流程使用
		delay := delay
		go func() {
			if delay > 0 {
				// timer 保存定时器，供当前处理流程使用
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-batchCtx.Done():
					results <- dialResult{err: batchCtx.Err()}
					return
				case <-timer.C:
				}
			}
			// dialCtx、dialCancel 保存dialCtx、dial取消，供当前处理流程使用
			dialCtx, dialCancel := context.WithTimeout(batchCtx, wsOpenTimeout)
			defer dialCancel()
			// conn、err 保存conn、err，供当前处理流程使用
			conn, _, err := websocket.Dial(dialCtx, target, &websocket.DialOptions{HTTPHeader: websocketHeaders()})
			results <- dialResult{conn: conn, err: err}
		}()
	}

	// 官网使用 Promise.race：第一条完成的连接无论成功或失败都会决定本轮
	// batchConnect 的结果；不会在先收到失败后继续等待其他竞速连接。
	// result 保存结果，供当前处理流程使用
	result := <-results
	go func() {
		defer cancel()
		for // i 保存i，供当前处理流程使用
		i := 1; i < len(delays); i++ {
			// late 保存late，供当前处理流程使用
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

// newConn 负责newConn相关处理。
func newConn(raw *websocket.Conn, cfg Config, logger *slog.Logger) *Conn {
	if logger == nil {
		logger = slog.Default()
	}
	// c 保存c，供当前处理流程使用
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

// ensureReadPump 负责ensureReadPump相关处理。
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
	// decodedAccessToken、err 保存decodedAccessToken、err，供当前处理流程使用
	decodedAccessToken, err := url.PathUnescape(accessToken)
	if err != nil {
		return fmt.Errorf("解码 WebSocket accessToken 失败: %w", err)
	}
	if !utf8.ValidString(decodedAccessToken) {
		return fmt.Errorf("解码 WebSocket accessToken 失败: 非法 UTF-8")
	}
	// response、err 保存response、err，供当前处理流程使用
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
	// code、ok 保存code、ok，供当前处理流程使用
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

// midKey 负责midKey相关处理。
func midKey(mid string) string {
	// fields 保存字段列表，供当前处理流程使用
	fields := strings.Fields(mid)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// responseCode 负责响应Code相关处理。
func responseCode(value any) (int, bool) {
	switch // code 保存code，供当前处理流程使用
	code := value.(type) {
	case float64:
		return int(code), true
	case int:
		return code, true
	case json.Number:
		// parsed、err 保存parsed、err，供当前处理流程使用
		parsed, err := code.Int64()
		return int(parsed), err == nil
	case string:
		// parsed 保存解析结果，供当前处理流程使用
		var parsed int
		if // err 保存err，供当前处理流程使用
		_, err := fmt.Sscanf(strings.TrimSpace(code), "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// request 负责请求相关处理。
func (c *Conn) request(ctx context.Context, path string, headers map[string]any, body any, timeout time.Duration) (map[string]any, error) {
	c.ensureReadPump()
	// requestCtx 保存请求Ctx，供当前处理流程使用
	requestCtx := ctx
	// cancel 保存取消，供当前处理流程使用
	cancel := func() {}
	if timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	if headers == nil {
		headers = make(map[string]any)
	}
	// mid 保存mid，供当前处理流程使用
	mid := strings.TrimSpace(fmt.Sprint(headers["mid"]))
	if mid == "" || mid == "<nil>" {
		mid = protocol.GenerateMid()
		headers["mid"] = mid
	}
	// key 保存key，供当前处理流程使用
	key := midKey(mid)
	// started 保存started，供当前处理流程使用
	started := time.Now()
	c.logger.Debug("WS 请求发送", "path", path, "mid", key)
	// responseCh 保存响应Ch，供当前处理流程使用
	responseCh := make(chan map[string]any, 1)
	c.pendingMu.Lock()
	c.pending[key] = responseCh
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
	}()

	// frame 保存frame，供当前处理流程使用
	frame := map[string]any{"lwp": path, "headers": headers}
	if body != nil {
		frame["body"] = body
	}
	if // err 保存err，供当前处理流程使用
	err := c.sendJSON(requestCtx, frame); err != nil {
		c.logger.Warn("WS 请求发送失败", "path", path, "mid", key, "duration", time.Since(started).Round(time.Millisecond), "err", err)
		return nil, err
	}
	select {
	case // response 保存响应，供当前处理流程使用
	response := <-responseCh:
		// code 保存code，供当前处理流程使用
		code, _ := responseCode(response["code"])
		c.logResponse(path, key, code, time.Since(started))
		return response, nil
	case <-c.readDone:
		// readPump always dispatches a decoded response before it can observe the
		// following close. Prefer that already-resolved response over readDone,
		// matching browser event ordering (message before close).
		select {
		case // response 保存响应，供当前处理流程使用
		response := <-responseCh:
			// code 保存code，供当前处理流程使用
			code, _ := responseCode(response["code"])
			c.logResponse(path, key, code, time.Since(started))
			return response, nil
		default:
		}
		// err 保存err，供当前处理流程使用
		err := c.connectionReadError()
		c.logger.Warn("WS 请求因连接结束失败", "path", path, "mid", key, "duration", time.Since(started).Round(time.Millisecond), "err", err)
		return nil, err
	case <-requestCtx.Done():
		// err 保存err，供当前处理流程使用
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
// ListUserMessages 读取用户消息列表。
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
	// response、err 保存response、err，供当前处理流程使用
	response, err := c.request(ctx, "/r/MessageManager/listUserMessages", nil,
		[]any{cid, false, cursor, limit, false}, regResponseTimeout)
	if err != nil {
		return nil, err
	}
	if // code、ok 保存code、ok，供当前处理流程使用
	code, ok := responseCode(response["code"]); ok && code != http.StatusOK {
		return nil, fmt.Errorf("聊天历史接口返回状态 %d", code)
	}
	// body、ok 保存body、ok，供当前处理流程使用
	body, ok := response["body"].(map[string]any)
	if !ok {
		return nil, errors.New("聊天历史接口响应缺少 body")
	}
	if // reason 保存原因，供当前处理流程使用
	reason := strings.TrimSpace(fmt.Sprint(body["reason"])); reason != "" && reason != "<nil>" {
		return nil, fmt.Errorf("聊天历史接口失败: %s", reason)
	}
	return body, nil
}

// ListConversations retrieves one page of the account's official IM contacts.
// ListConversations 读取Conversations。
func (c *Conn) ListConversations(ctx context.Context, cursor int64, limit int) (map[string]any, error) {
	if cursor <= 0 {
		cursor = 9007199254740991
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	// response、err 保存response、err，供当前处理流程使用
	response, err := c.request(ctx, "/r/Conversation/listNewestPagination", nil, []any{cursor, limit}, regResponseTimeout)
	if err != nil {
		return nil, err
	}
	if // code、ok 保存code、ok，供当前处理流程使用
	code, ok := responseCode(response["code"]); ok && code != http.StatusOK {
		return nil, fmt.Errorf("会话列表接口返回状态 %d", code)
	}
	// body、ok 保存body、ok，供当前处理流程使用
	body, ok := response["body"].(map[string]any)
	if !ok {
		return nil, errors.New("会话列表接口响应缺少 body")
	}
	if // reason 保存原因，供当前处理流程使用
	reason := strings.TrimSpace(fmt.Sprint(body["reason"])); reason != "" && reason != "<nil>" {
		return nil, fmt.Errorf("会话列表接口失败: %s", reason)
	}
	return body, nil
}

// logResponse 负责log响应相关处理。
func (c *Conn) logResponse(path, mid string, code int, duration time.Duration) {
	// attrs 保存attrs，供当前处理流程使用
	attrs := []any{"path", path, "mid", mid, "code", code, "duration", duration.Round(time.Millisecond)}
	if code >= 400 {
		c.logger.Warn("WS 业务响应异常", attrs...)
		return
	}
	c.logger.Debug("WS 响应收到", attrs...)
}

// readPump 负责readPump相关处理。
func (c *Conn) readPump() {
	defer close(c.readDone)
	for {
		// messageType、data、err 保存消息Type、data、err，供当前处理流程使用
		messageType, data, err := c.ws.Read(c.readCtx)
		if err != nil {
			c.readErrMu.Lock()
			c.readErr = err
			c.readErrMu.Unlock()
			return
		}
		// parsed 保存解析结果，供当前处理流程使用
		var parsed map[string]any
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal(data, &parsed); err != nil {
			if // recorder 保存recorder，供当前处理流程使用
			recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", string(data), "", "non_json", err.Error())
			}
			continue
		}
		c.recordParsedIncoming(data, parsed)
		if // hasCode 保存hasCode，供当前处理流程使用
		_, hasCode := parsed["code"]; hasCode {
			if // hasHeaders 保存hasHeaders，供当前处理流程使用
			_, hasHeaders := parsed["headers"].(map[string]any); hasHeaders {
				c.dispatchResponse(parsed)
				continue
			}
		}
		// lwp、hasLWP 保存lwp、hasLWP，供当前处理流程使用
		lwp, hasLWP := parsed["lwp"].(string)
		// hasHeaders 保存hasHeaders，供当前处理流程使用
		_, hasHeaders := parsed["headers"].(map[string]any)
		if !hasLWP || strings.TrimSpace(lwp) == "" || !hasHeaders {
			if // recorder 保存recorder，供当前处理流程使用
			recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", string(data), string(data), "skip_invalid_lwp", "")
			}
			continue
		}
		// incoming 保存incoming，供当前处理流程使用
		incoming := incomingFrame{messageType: messageType, data: append([]byte(nil), data...), parsed: parsed}
		select {
		case c.pushes <- incoming:
		case <-c.readCtx.Done():
			return
		}
	}
}

// dispatchResponse 负责dispatch响应相关处理。
func (c *Conn) dispatchResponse(frame map[string]any) bool {
	// headers 保存headers，供当前处理流程使用
	headers, _ := frame["headers"].(map[string]any)
	// key 保存key，供当前处理流程使用
	key := midKey(strings.TrimSpace(fmt.Sprint(headers["mid"])))
	c.pendingMu.Lock()
	// ch 保存ch，供当前处理流程使用
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

// connectionReadError 负责connectionRead错误相关处理。
func (c *Conn) connectionReadError() error {
	c.readErrMu.Lock()
	defer c.readErrMu.Unlock()
	if c.readErr != nil {
		return c.readErr
	}
	return fmt.Errorf("WebSocket 读取循环已结束")
}

// recordParsedIncoming 负责record解析结果Incoming相关处理。
func (c *Conn) recordParsedIncoming(data []byte, parsed map[string]any) {
	// recorder 保存recorder，供当前处理流程使用
	recorder := c.recorderSnapshot()
	if recorder == nil {
		return
	}
	// parsedJSON 保存解析结果JSON，供当前处理流程使用
	parsedJSON := string(data)
	if // normalized、err 保存normalized、err，供当前处理流程使用
	normalized, err := json.Marshal(parsed); err == nil {
		parsedJSON = string(normalized)
	}
	recorder("in", string(data), parsedJSON, "json", "")
}

// HeartbeatLoop 对齐官网：注册后以固定 15 秒节拍发送 /!，即使上一请求仍在
// 等待也不推迟下一次；任一请求失败或 30 秒无响应即结束连接。官网只以
// Promise 是否 reject 判断心跳，不因已收到的非 200 响应主动断线。
// HeartbeatLoop 负责HeartbeatLoop相关处理。
func (c *Conn) HeartbeatLoop(ctx context.Context, interval time.Duration) error {
	c.ensureReadPump()
	// heartbeatCtx、cancel 保存heartbeatCtx、cancel，供当前处理流程使用
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// ticker 保存ticker，供当前处理流程使用
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// heartbeatErr 保存heartbeatErr，供当前处理流程使用
	heartbeatErr := make(chan error, 1)
	for {
		select {
		case <-heartbeatCtx.Done():
			return heartbeatCtx.Err()
		case <-c.readDone:
			return c.connectionReadError()
		case // err 保存err，供当前处理流程使用
		err := <-heartbeatErr:
			_ = c.Close()
			return fmt.Errorf("心跳响应失败: %w", err)
		case <-ticker.C:
			go func() {
				// err 保存err，供当前处理流程使用
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
// ReceiveLoop 负责ReceiveLoop相关处理。
func (c *Conn) ReceiveLoop(ctx context.Context, onMessage func(decrypted map[string]any)) error {
	c.ensureReadPump()
	for {
		// frame 保存frame，供当前处理流程使用
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
		// raw 保存原始，供当前处理流程使用
		raw := frame.parsed
		// rawText 保存原始文本，供当前处理流程使用
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
			if // err 保存err，供当前处理流程使用
			err := c.handleSyncExtra(c.readCtx, message); err != nil && c.readCtx.Err() == nil {
				c.logger.Error("同步状态恢复失败", "err", err)
			}
		}(raw)

		// 仅处理同步包：body.syncPushPackage.data[0].data
		syncData, ok := extractSyncPayload(raw)
		if !ok {
			c.sendACK(ctx, raw)
			if // recorder 保存recorder，供当前处理流程使用
			recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", rawText, "", "skip_non_sync", "")
			}
			continue
		}
		// decoded、err 保存decoded、err，供当前处理流程使用
		decoded, err := decodeSyncData(syncData)
		if err != nil {
			c.sendACK(ctx, raw)
			if // recorder 保存recorder，供当前处理流程使用
			recorder := c.recorderSnapshot(); recorder != nil {
				recorder("in", rawText, "", "decrypt_failed", err.Error())
			}
			c.logger.Error("消息解密失败", "err", err)
			continue
		}
		if // recorder 保存recorder，供当前处理流程使用
		recorder := c.recorderSnapshot(); recorder != nil {
			if // b、e 保存b、e，供当前处理流程使用
			b, e := json.Marshal(decoded); e == nil {
				recorder("in", rawText, string(b), "decrypted", "")
			}
		}
		c.sendACK(ctx, raw)
		if onMessage != nil {
			onMessage(decoded)
		}
	}
}

// handleSyncExtra 负责handleSyncExtra相关处理。
func (c *Conn) handleSyncExtra(ctx context.Context, msg map[string]any) error {
	// body 保存请求体，供当前处理流程使用
	body, _ := msg["body"].(map[string]any)
	// extra 保存extra，供当前处理流程使用
	extra, _ := body["syncExtraType"].(map[string]any)
	// typeCode、ok 保存类型Code、ok，供当前处理流程使用
	typeCode, ok := responseCode(extra["type"])
	if !ok || (typeCode != 1 && typeCode != 2) {
		return nil
	}
	// state、err 保存state、err，供当前处理流程使用
	state, err := c.request(ctx, "/r/SyncStatus/getState", map[string]any{}, []any{map[string]any{"topic": "sync"}}, regResponseTimeout)
	if err != nil {
		return fmt.Errorf("getState: %w", err)
	}
	if // code、ok 保存code、ok，供当前处理流程使用
	code, ok := responseCode(state["code"]); !ok || code != http.StatusOK || state["body"] == nil {
		return fmt.Errorf("getState 返回异常: code=%v", state["code"])
	}
	// response、err 保存response、err，供当前处理流程使用
	response, err := c.request(ctx, "/r/SyncStatus/ackDiff", map[string]any{}, []any{state["body"]}, regResponseTimeout)
	if err != nil {
		return fmt.Errorf("ackDiff: %w", err)
	}
	if // code、ok 保存code、ok，供当前处理流程使用
	code, ok := responseCode(response["code"]); ok && code != http.StatusOK {
		return fmt.Errorf("ackDiff 返回异常: code=%d", code)
	}
	return nil
}

// sendACK 回复 {"code":200, headers:<服务端完整 headers>}。
func (c *Conn) sendACK(ctx context.Context, msg map[string]any) {
	// headers 保存headers，供当前处理流程使用
	headers, _ := msg["headers"].(map[string]any)
	// ackHeaders 保存ackHeaders，供当前处理流程使用
	ackHeaders := make(map[string]any, len(headers))
	// key、value 表示当前遍历过程中的key、value
	for key, value := range headers {
		ackHeaders[key] = value
	}
	// ack 保存ack，供当前处理流程使用
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
	// body 保存请求体，供当前处理流程使用
	body, _ := msg["body"].(map[string]any)
	if body == nil {
		return "", false
	}
	// pkg 保存pkg，供当前处理流程使用
	pkg, _ := body["syncPushPackage"].(map[string]any)
	if pkg == nil {
		return "", false
	}
	// arr 保存arr，供当前处理流程使用
	arr, _ := pkg["data"].([]any)
	if len(arr) == 0 {
		return "", false
	}
	// first 保存first，供当前处理流程使用
	first, _ := arr[0].(map[string]any)
	if first == nil {
		return "", false
	}
	// d、ok 保存d、ok，供当前处理流程使用
	d, ok := first["data"].(string)
	return d, ok && d != ""
}

// decodeSyncData 先尝试 base64+JSON（未加密系统消息），失败则 base64+msgpack 解密。
func decodeSyncData(data string) (map[string]any, error) {
	// 1) base64 解码后尝试解析 JSON。
	if dec, err := base64.StdEncoding.DecodeString(data); err == nil {
		// parsed 保存解析结果，供当前处理流程使用
		var parsed map[string]any
		if // jsonErr 保存jsonErr，供当前处理流程使用
		jsonErr := json.Unmarshal(dec, &parsed); jsonErr == nil {
			return parsed, nil
		}
	}
	// 2) JSON 解析失败 → msgpack 解密
	out, err := protocol.Decrypt(data)
	if err != nil {
		return nil, err
	}
	// parsed 保存解析结果，供当前处理流程使用
	var parsed map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("解密后非 JSON: %w", err)
	}
	return parsed, nil
}

// sendJSON 发送一条 JSON 文本帧。
func (c *Conn) sendJSON(ctx context.Context, v any) error {
	// b、err 保存b、err，供当前处理流程使用
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if // recorder 保存recorder，供当前处理流程使用
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
	// content 保存内容，供当前处理流程使用
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
	// content 保存内容，供当前处理流程使用
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

// sendChatContent 负责send聊天内容相关处理。
func (c *Conn) sendChatContent(ctx context.Context, myID, cid, toID string, content any) error {
	myID = stripGoofish(myID)
	cid = stripGoofish(cid)
	toID = stripGoofish(toID)
	if myID == "" || cid == "" || toID == "" {
		return fmt.Errorf("发送消息缺少必要参数: myID=%q cid=%q toID=%q", myID, cid, toID)
	}
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	// encoded 保存encoded，供当前处理流程使用
	encoded := base64.StdEncoding.EncodeToString(raw)
	// msg 保存msg，供当前处理流程使用
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

// stripGoofish 负责stripGoofish相关处理。
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
