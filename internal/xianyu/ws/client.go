// Package ws 实现闲鱼 WebSocket 连接生命周期：握手、/reg 注册、心跳、ACK、消息解密。
package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/protocol"
)

// WSURL 闲鱼 IM WebSocket 地址。
const WSURL = "wss://wss-goofish.dingtalk.com/"

// regUA WS /reg 注册消息用的 UA（含闲鱼网页客户端 DingTalk 标识）。
// 引用 xianyu.RegUA 保证与 HTTP 请求的 Chrome 版本号一致。
var regUA = xianyu.RegUA

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
	ws       *websocket.Conn
	cfg      Config
	logger   *slog.Logger
	sendMu   sync.Mutex
	recorder func(direction, rawText, parsedJSON, parseStatus, errMsg string)
}

// SetRecorder 设置帧记录器。
func (c *Conn) SetRecorder(rec func(direction, rawText, parsedJSON, parseStatus, errMsg string)) {
	c.recorder = rec
}

// Dial 建立并注册 WS 连接：握手 → /reg → /r/SyncStatus/ackDiff。
func Dial(ctx context.Context, cfg Config, logger *slog.Logger) (*Conn, error) {
	if logger == nil {
		logger = slog.Default()
	}
	// 握手头：Cookie + 浏览器指纹（Origin/Host 由 dialer 据 URL 自动设置，这里显式覆盖）。
	hdr := http.Header{}
	hdr.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	hdr.Set("Accept-Language", "zh-CN,zh;q=0.9")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Pragma", "no-cache")
	hdr.Set("User-Agent", xianyu.BrowserUA)
	hdr.Set("Origin", "https://www.goofish.com")
	hdr.Set("Cookie", cfg.CookieStr)

	logger.Info("正在连接闲鱼 WebSocket", "url", WSURL)
	c, _, err := websocket.Dial(ctx, WSURL, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		return nil, fmt.Errorf("WS dial: %w", err)
	}
	// 闲鱼消息多为文本 JSON。
	c.SetReadLimit(8 << 20) // 8 MiB，宽松上限

	conn := &Conn{ws: c, cfg: cfg, logger: logger, recorder: cfg.Recorder}
	if err := conn.register(ctx); err != nil {
		_ = c.CloseNow()
		return nil, err
	}
	return conn, nil
}

// register 发送 /reg 与 ackDiff。
func (c *Conn) register(ctx context.Context) error {
	reg := map[string]any{
		"lwp": "/reg",
		"headers": map[string]any{
			"cache-header": "app-key token ua wv",
			"app-key":      RegAppKey,
			"token":        c.cfg.AccessToken,
			"ua":           regUA,
			"dt":           "j",
			"wv":           "im:3,au:3,sy:6",
			"sync":         "0,0;0;0;",
			"did":          c.cfg.DeviceID,
			"mid":          protocol.GenerateMid(),
		},
	}
	if err := c.sendJSON(ctx, reg); err != nil {
		return fmt.Errorf("发送 /reg 失败: %w", err)
	}

	// /reg 后等待 1 秒再发送 ackDiff。
	select {
	case <-time.After(time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}

	now := time.Now().UnixMilli()
	ackDiff := map[string]any{
		"lwp":     "/r/SyncStatus/ackDiff",
		"headers": map[string]any{"mid": protocol.GenerateMid()},
		"body": []any{
			map[string]any{
				"pipeline":    "sync",
				"tooLong2Tag": "PNM,1",
				"channel":     "sync",
				"topic":       "sync",
				"highPts":     0,
				"pts":         now * 1000,
				"seq":         0,
				"timestamp":   now,
			},
		},
	}
	if err := c.sendJSON(ctx, ackDiff); err != nil {
		return fmt.Errorf("发送 ackDiff 失败: %w", err)
	}
	c.logger.Info("WS 注册完成")
	return nil
}

// HeartbeatLoop 周期性发送应用层心跳 {"lwp":"/!"}，直到 ctx 取消或连续失败 maxFailures 次。
func (c *Conn) HeartbeatLoop(ctx context.Context, interval time.Duration) error {
	const maxFailures = 3
	consecutive := 0
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		hb := map[string]any{
			"lwp":     "/!",
			"headers": map[string]any{"mid": protocol.GenerateMid()},
		}
		// 2s 超时保护。
		sendCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := c.sendJSON(sendCtx, hb)
		cancel()
		if err != nil {
			consecutive++
			c.logger.Error("心跳发送失败", "err", err, "consecutive", consecutive)
			if consecutive >= maxFailures {
				return fmt.Errorf("心跳连续失败 %d 次", maxFailures)
			}
			continue
		}
		consecutive = 0
	}
}

// ReceiveLoop 阻塞读取消息：回 ACK，对同步包解密并回调 handler。
// 非 JSON / 非同步包消息仅记录后跳过。返回 ctx 错误或致命读取错误。
func (c *Conn) ReceiveLoop(ctx context.Context, onMessage func(decrypted map[string]any)) error {
	for {
		msgType, data, err := c.ws.Read(ctx)
		if err != nil {
			return fmt.Errorf("WS read: %w", err)
		}
		_ = msgType
		rawText := string(data)
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			if c.recorder != nil {
				c.recorder("in", rawText, "", "non_json", err.Error())
			}
			c.logger.Debug("非 JSON 消息，跳过", "err", err)
			continue
		}
		if c.recorder != nil {
			if b, e := json.Marshal(raw); e == nil {
				c.recorder("in", rawText, string(b), "json", "")
			} else {
				c.recorder("in", rawText, "", "json", e.Error())
			}
		}

		// 心跳响应等直接跳过。
		if code, ok := raw["code"]; ok && code != nil {
			c.logger.Debug("收到响应", "code", code)
		}

		// 收到消息后先回复 ACK。
		c.sendACK(ctx, raw)

		// 仅处理同步包：body.syncPushPackage.data[0].data
		syncData, ok := extractSyncPayload(raw)
		if !ok {
			if c.recorder != nil {
				c.recorder("in", rawText, "", "skip_non_sync", "")
			}
			continue
		}
		decoded, err := decodeSyncData(syncData)
		if err != nil {
			if c.recorder != nil {
				c.recorder("in", rawText, "", "decrypt_failed", err.Error())
			}
			c.logger.Error("消息解密失败", "err", err)
			continue
		}
		if c.recorder != nil {
			if b, e := json.Marshal(decoded); e == nil {
				c.recorder("in", rawText, string(b), "decrypted", "")
			}
		}
		if onMessage != nil {
			onMessage(decoded)
		}
	}
}

// sendACK 回复 {"code":200, headers:{mid,sid,...}}，失败时仅记录日志。
func (c *Conn) sendACK(ctx context.Context, msg map[string]any) {
	headers, _ := msg["headers"].(map[string]any)
	ack := map[string]any{
		"code": 200,
		"headers": map[string]any{
			"mid": ackVal(headers, "mid", protocol.GenerateMid()),
			"sid": ackVal(headers, "sid", ""),
		},
	}
	ackHeaders := ack["headers"].(map[string]any)
	for _, k := range []string{"app-key", "ua", "dt"} {
		if v, ok := headers[k]; ok && v != nil {
			ackHeaders[k] = v
		}
	}
	// ACK 失败不阻塞主循环。
	ackCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_ = c.sendJSON(ackCtx, ack)
	cancel()
}

// ackVal 取 header 中的 key，缺失或为 nil 时返回 fallback。
func ackVal(headers map[string]any, key string, fallback any) any {
	if v, ok := headers[key]; ok && v != nil {
		return v
	}
	return fallback
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
	if c.recorder != nil {
		c.recorder("out", string(b), string(b), "json", "")
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// SetAccessToken 在线更新 token（仅在连接保持期间用于定时刷新）。
func (c *Conn) SetAccessToken(token string) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.cfg.AccessToken = token
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
	return c.ws.Close(websocket.StatusNormalClosure, "bye")
}
