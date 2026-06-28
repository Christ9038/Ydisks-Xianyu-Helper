// delivery.go 实现卡密自动发货全流程。
// 移植自 Python _handle_auto_delivery + _auto_delivery：
//
//	商品归属校验 → 提取 order_id → 四重防重（延迟锁/冷却 + 加锁后复查）
//	→ 多规格/多数量检查 → 规则双向模糊匹配 → 延时 → 确认发货(mtop consign)
//	→ 入库 → 发消息 → 防重标记 → 通知
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// 自动发货触发关键字（买家"我已付款"等系统消息）。
var autoDeliveryKeywords = []string{
	"[我已付款，等待你发货]",
	"[已付款，待发货]",
	"我已付款，等待你发货",
	"[记得及时发货]",
}

// IsAutoDeliveryTrigger 消息文本是否触发自动发货。
func IsAutoDeliveryTrigger(text string) bool {
	for _, kw := range autoDeliveryKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// 发货锁与冷却配置。
const (
	DeliveryCooldown = 10 * time.Minute // 同一订单 10 分钟内不重复发货
	DeliveryLockHold = 10 * time.Minute // 发货后持有延迟锁 10 分钟
	ConfirmCooldown  = 30 * time.Second // 同一订单确认发货冷却
)

// DeliverySender 抽象"发送消息给买家"的能力（由 Account 注入，复用其 WS 连接）。
type DeliverySender interface {
	SendText(ctx context.Context, chatID, toUserID, text string) error
	SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64) error
}

// OrderDetailFetcher 抽象"取订单详情"（多规格/多数量需要，Phase 5 由 sidecar 提供）。
type OrderDetailFetcher interface {
	Fetch(ctx context.Context, cookieID, orderID, itemID, buyerID, cookieStr string) (*OrderDetail, error)
}

// OrderDetail 订单详情关键字段。
type OrderDetail struct {
	Quantity    string
	SpecName    string
	SpecValue   string
	Amount      string
	OrderStatus string
}

// Notifier 发货通知（Phase 3 后段实现，先接口占位）。
type Notifier interface {
	NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string)
}

// DeliveryService 单账号发货服务（每个 Account 持有一个）。
type DeliveryService struct {
	cookieID string
	store    *db.Store
	mtop     *mtop.Client
	sender   DeliverySender
	fetcher  OrderDetailFetcher // 可为 nil（未配置多规格/多数量时不调用）
	notifier Notifier           // 可为 nil
	logger   *slog.Logger

	// 防重状态
	mu             sync.Mutex
	locks          map[string]*deliveryLock // order_id → 延迟锁
	lastDelivery   map[string]time.Time     // order_id → 上次发货时间（冷却）
	confirmedOrder map[string]time.Time     // order_id → 上次确认发货时间

	// cookie 源/汇：与 Account 共享可变 cookie（发货/确认发货会刷新 cookie）
	cookieSrc  func() string
	cookieSink func(string)
}

type deliveryLock struct {
	locked     bool
	releasedAt time.Time
	timer      *time.Timer
	mux        sync.Mutex // per-order 串行锁
}

// NewDeliveryService 构造。
func NewDeliveryService(cookieID string, store *db.Store, sender DeliverySender,
	fetcher OrderDetailFetcher, notifier Notifier, logger *slog.Logger) *DeliveryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeliveryService{
		cookieID:       cookieID,
		store:          store,
		mtop:           &mtop.Client{},
		sender:         sender,
		fetcher:        fetcher,
		notifier:       notifier,
		logger:         logger.With("account", cookieID, "subsys", "delivery"),
		locks:          make(map[string]*deliveryLock),
		lastDelivery:   make(map[string]time.Time),
		confirmedOrder: make(map[string]time.Time),
	}
}

// Handle 收到一条聊天消息，若为自动发货触发关键字则执行发货流程。
// 由 Account 在防抖后调用（HandleChatMessage 适配）。
func (d *DeliveryService) Handle(ctx context.Context, m ChatMessage) error {
	if !IsAutoDeliveryTrigger(m.Text) {
		return nil
	}
	return d.handleAutoDelivery(ctx, m.Raw, m.ChatID, m.SenderUserID, m.SenderName, m.ItemID)
}

// handleAutoDelivery 发货主流程。
func (d *DeliveryService) handleAutoDelivery(ctx context.Context, raw map[string]any,
	chatID, sendUserID, sendUserName, itemID string) error {
	// 1. 商品归属校验。
	if itemID != "" && itemID != "未知商品" {
		if _, err := d.store.Items.Get(ctx, d.cookieID, itemID); err != nil {
			d.logger.Warn("商品不属于当前账号或查询失败，跳过发货", "item_id", itemID, "err", err)
			return nil
		}
	}

	// 2. 提取 order_id。
	orderID := extractOrderID(raw)
	if orderID == "" {
		d.logger.Warn("未能提取到订单ID，跳过自动发货")
		return nil
	}

	// 3. 四重防重（延迟锁 + 冷却 + 加锁后复查）。
	if d.isLockHeld(orderID) {
		d.logger.Info("订单延迟锁持有中，跳过发货", "order_id", orderID)
		return nil
	}
	if !d.canAutoDelivery(orderID) {
		d.logger.Info("订单冷却期内，跳过发货", "order_id", orderID)
		return nil
	}

	// 加订单级互斥锁（同一 order_id 串行处理）。
	unlock := d.acquireOrderLock(orderID)
	defer unlock()

	if d.isLockHeld(orderID) {
		d.logger.Info("获取锁后延迟锁仍持有，跳过", "order_id", orderID)
		return nil
	}
	if !d.canAutoDelivery(orderID) {
		d.logger.Info("获取锁后仍在冷却期，跳过", "order_id", orderID)
		return nil
	}

	// 4. 订单详情只抓取一次：用于补全实付金额，并按需读取规格/购买数量。
	quantity := 1
	specName, specValue := "", ""
	isMultiSpec := d.store.Items.IsMultiSpec(ctx, d.cookieID, itemID)
	isMultiQuantity := d.store.Items.MultiQuantityDelivery(ctx, d.cookieID, itemID)
	needDetail := isMultiSpec || isMultiQuantity
	if existing, err := d.store.Orders.Get(ctx, orderID); err != nil || strings.TrimSpace(existing.Amount) == "" {
		needDetail = true
	}
	if d.fetcher != nil && needDetail {
		od, err := d.fetcher.Fetch(ctx, d.cookieID, orderID, itemID, sendUserID, d.cookieStr())
		if err != nil {
			d.logger.Warn("获取订单详情失败，继续尝试自动发货", "order_id", orderID, "err", err)
		} else if od != nil {
			specName, specValue = od.SpecName, od.SpecValue
			_ = d.store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
				CookieID: d.cookieID, ItemID: itemID, BuyerID: sendUserID,
				SpecName: od.SpecName, SpecValue: od.SpecValue, Quantity: od.Quantity,
				Amount: od.Amount, OrderStatus: od.OrderStatus,
			})
			if isMultiQuantity {
				if q := parseIntSafe(od.Quantity); q > 1 {
					quantity = q
					d.logger.Info("多数量发货", "quantity", quantity, "order_id", orderID)
				}
			}
		}
	}

	// 5. 循环获取发货内容。
	contents, err := d.collectDeliveryContentsWithSpec(ctx, itemID, orderID, sendUserID, specName, specValue, quantity)
	if err != nil {
		d.notify(sendUserName, sendUserID, itemID, "自动发货处理异常: "+err.Error(), chatID)
		return err
	}
	if len(contents) == 0 {
		d.logger.Warn("未找到匹配的发货规则或获取发货内容失败", "order_id", orderID)
		d.notify(sendUserName, sendUserID, itemID, "未找到匹配的发货规则或获取发货内容失败", chatID)
		return nil
	}

	// 6. 标记已发货 + 延迟锁 + 入库 system_shipped。
	d.markDeliverySent(orderID)
	sysShip := true
	_ = d.store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
		SystemShipped: &sysShip,
		ChatID:        chatID,
		ItemID:        itemID,
		BuyerID:       sendUserID,
		CookieID:      d.cookieID,
	})

	// 7. 逐条发送，多数量间隔 1s。
	for i, c := range contents {
		if err := d.sendContent(ctx, c, chatID, sendUserID); err != nil {
			d.logger.Error("发送发货内容失败", "index", i, "err", err)
		}
		if len(contents) > 1 && i < len(contents)-1 {
			if sleepCtx(ctx, time.Second) != nil {
				return ctx.Err()
			}
		}
	}

	// 8. 通知。
	msg := "发货成功"
	if len(contents) > 1 {
		msg = fmt.Sprintf("多数量发货成功，共发送 %d 个卡券", len(contents))
	}
	d.notify(sendUserName, sendUserID, itemID, msg, chatID)
	d.logger.Info("自动发货完成", "order_id", orderID, "count", len(contents))
	return nil
}

// collectDeliveryContents 调用 quantity 次 _auto_delivery 等价逻辑，收集发货内容。
func (d *DeliveryService) collectDeliveryContents(ctx context.Context, itemID, orderID, sendUserID string, quantity int) ([]string, error) {
	return d.collectDeliveryContentsWithSpec(ctx, itemID, orderID, sendUserID, "", "", quantity)
}

func (d *DeliveryService) collectDeliveryContentsWithSpec(ctx context.Context, itemID, orderID, sendUserID, specName, specValue string, quantity int) ([]string, error) {
	contents := make([]string, 0, quantity)
	for i := 0; i < quantity; i++ {
		batch, err := d.autoDeliveryOnce(ctx, itemID, orderID, sendUserID, specName, specValue)
		if err != nil {
			d.logger.Error("获取发货内容异常", "index", i, "err", err)
			continue
		}
		contents = append(contents, batch...)
	}
	return contents, nil
}

// autoDeliveryOnce 单次发货内容获取：匹配规则 → 延时 → 确认发货 → 卡券内容。
// 移植自 _auto_delivery。
func (d *DeliveryService) autoDeliveryOnce(ctx context.Context, itemID, orderID, sendUserID, knownSpecName, knownSpecValue string) ([]string, error) {
	// 搜索文本 = 商品标题 + 详情。
	searchText := d.buildSearchText(ctx, itemID)

	// 多规格处理。
	isMultiSpec := d.store.Items.IsMultiSpec(ctx, d.cookieID, itemID)
	specName, specValue := strings.TrimSpace(knownSpecName), strings.TrimSpace(knownSpecValue)
	if isMultiSpec && (specName == "" || specValue == "") && d.fetcher != nil {
		od, err := d.fetcher.Fetch(ctx, d.cookieID, orderID, itemID, sendUserID, d.cookieStr())
		if err != nil || od == nil || od.SpecName == "" || od.SpecValue == "" {
			d.logger.Warn("多规格商品但无规格信息，跳过", "order_id", orderID, "err", err)
			return nil, nil
		}
		specName, specValue = od.SpecName, od.SpecValue
	}
	if isMultiSpec && (specName == "" || specValue == "") {
		d.logger.Warn("多规格商品缺少规格信息，跳过", "order_id", orderID)
		return nil, nil
	}

	// 新规则按 item_id + 规格精确匹配；旧规则自动回退关键词。
	if !isMultiSpec {
		specName, specValue = "", ""
	}
	rules, err := d.store.DeliveryRules.MatchForOrder(ctx, d.cookieID, itemID, searchText, specName, specValue)
	if err != nil {
		return nil, fmt.Errorf("匹配发货规则失败: %w", err)
	}
	if len(rules) == 0 {
		d.logger.Warn("未找到匹配的发货规则", "search_text", trunc(searchText, 50))
		return nil, nil
	}
	if len(rules) > 1 {
		d.logger.Warn("匹配到多个发货规则，无法确定，跳过", "count", len(rules))
		return nil, nil
	}
	rule := rules[0]

	// 延时。
	if rule.CardDelaySeconds > 0 {
		d.logger.Info("执行发货延时", "seconds", rule.CardDelaySeconds)
		if sleepCtx(ctx, time.Duration(rule.CardDelaySeconds)*time.Second) != nil {
			return nil, ctx.Err()
		}
	}

	// 确认发货（mtop consign）。auto_confirm 仅控制平台订单状态，
	// 不影响后续卡券内容发送。
	if d.shouldAutoConfirm(ctx, orderID) {
		d.logger.Info("确认发货", "order_id", orderID)
		cookieStr := d.cookieStr()
		ok, ret, updated, cerr := d.mtop.ConsignContext(ctx, cookieStr, orderID)
		if cerr != nil {
			d.logger.Warn("确认发货请求失败，继续发送内容", "err", cerr)
		} else {
			if updated != cookieStr && updated != "" {
				d.setCookieStr(updated)
				if err := d.store.Cookies.Save(ctx, d.cookieID, updated, 0); err != nil {
					d.logger.Warn("保存确认发货更新后的 Cookie 失败", "err", err)
				}
			}
			if ok {
				d.recordConfirm(orderID)
			} else {
				d.logger.Warn("确认发货未成功，仍发送内容", "ret", ret)
			}
		}
	}

	// 卡券内容。
	if orderID == "" {
		d.logger.Info("无订单ID，跳过发货内容处理")
		return nil, nil
	}
	deliveryCount := rule.DeliveryCount
	if deliveryCount <= 0 {
		deliveryCount = 1
	}
	contents := make([]string, 0, deliveryCount)
	for i := 0; i < deliveryCount; i++ {
		content := d.cardContent(ctx, rule, orderID, itemID, sendUserID, specName, specValue)
		if content == "" {
			break
		}
		contents = append(contents, processDeliveryContentWithDescription(content, rule.CardDescription))
		_ = d.store.Cards.IncrementDeliveryTimes(ctx, rule.ID)
	}
	return contents, nil
}

// buildSearchText 商品标题 + 空格 + 详情。
func (d *DeliveryService) buildSearchText(ctx context.Context, itemID string) string {
	it, err := d.store.Items.Get(ctx, d.cookieID, itemID)
	if err != nil || it == nil {
		return itemID
	}
	parts := []string{}
	if strings.TrimSpace(it.ItemTitle) != "" {
		parts = append(parts, strings.TrimSpace(it.ItemTitle))
	}
	if strings.TrimSpace(it.ItemDetail) != "" {
		parts = append(parts, strings.TrimSpace(it.ItemDetail))
	}
	if len(parts) == 0 {
		return itemID
	}
	return strings.Join(parts, " ")
}

// cardContent 按卡券类型取发货内容。
func (d *DeliveryService) cardContent(ctx context.Context, rule db.DeliveryRule, orderID, itemID, buyerID, specName, specValue string) string {
	switch rule.CardType {
	case "text":
		return rule.TextContent
	case "data":
		c, err := d.store.Cards.ConsumeBatchData(ctx, rule.CardID)
		if err != nil {
			d.logger.Error("消费批量数据失败", "card_id", rule.CardID, "err", err)
			return ""
		}
		return c
	case "image":
		if rule.ImageURL == "" {
			d.logger.Error("图片卡券缺少 URL", "card_id", rule.CardID)
			return ""
		}
		return fmt.Sprintf("__IMAGE_SEND__%d|%s", rule.CardID, rule.ImageURL)
	case "api":
		content, err := d.apiCardContent(ctx, rule, orderID, itemID, buyerID, specName, specValue)
		if err != nil {
			d.logger.Error("API 卡券调用失败", "card_id", rule.CardID, "err", err)
			return ""
		}
		return content
	}
	return ""
}

func (d *DeliveryService) apiCardContent(ctx context.Context, rule db.DeliveryRule, orderID, itemID, buyerID, specName, specValue string) (string, error) {
	cfg := rule.APIConfigMap()
	if len(cfg) == 0 {
		return "", fmt.Errorf("API 配置为空")
	}
	endpoint := strings.TrimSpace(anyString(cfg["url"]))
	if endpoint == "" {
		return "", fmt.Errorf("API 配置缺少 url")
	}
	method := strings.ToUpper(strings.TrimSpace(anyString(cfg["method"])))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return "", fmt.Errorf("不支持的 HTTP 方法: %s", method)
	}
	timeoutSeconds := anyInt(cfg["timeout"])
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	if timeoutSeconds > 120 {
		timeoutSeconds = 120
	}
	headers := parseAPIMap(cfg["headers"])
	params := parseAPIMap(cfg["params"])
	params = d.replaceAPIParams(ctx, params, orderID, itemID, buyerID, specName, specValue)

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, time.Duration(attempt*2)*time.Second); err != nil {
				return "", err
			}
		}
		content, retry, err := d.callAPIOnce(ctx, endpoint, method, headers, params, time.Duration(timeoutSeconds)*time.Second)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return "", lastErr
}

func (d *DeliveryService) callAPIOnce(ctx context.Context, endpoint, method string, headers, params map[string]any, timeout time.Duration) (content string, retry bool, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if method == http.MethodPost {
		raw, err := json.Marshal(params)
		if err != nil {
			return "", false, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, endpoint, body)
	if err != nil {
		return "", false, err
	}
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, anyString(v))
	}
	if method == http.MethodPost && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodGet && len(params) > 0 {
		q := req.URL.Query()
		for k, v := range params {
			q.Set(k, anyString(v))
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", true, readErr
	}
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout
		return "", retry, fmt.Errorf("API 返回状态 %d: %s", resp.StatusCode, trunc(string(raw), 200))
	}
	content = parseAPIContent(raw)
	if strings.TrimSpace(content) == "" {
		return "", false, fmt.Errorf("API 返回内容为空")
	}
	return content, false, nil
}

func parseAPIMap(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	switch x := v.(type) {
	case map[string]any:
		return x
	case map[string]string:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = v
		}
		return out
	case string:
		x = strings.TrimSpace(x)
		if x == "" {
			return map[string]any{}
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(x), &m); err == nil {
			return m
		}
	}
	return map[string]any{}
}

func (d *DeliveryService) replaceAPIParams(ctx context.Context, params map[string]any, orderID, itemID, buyerID, specName, specValue string) map[string]any {
	mapping := map[string]string{
		"order_id":   orderID,
		"item_id":    itemID,
		"buyer_id":   buyerID,
		"cookie_id":  d.cookieID,
		"spec_name":  specName,
		"spec_value": specValue,
	}
	if orderID != "" {
		if ord, err := d.store.Orders.Get(ctx, orderID); err == nil && ord != nil {
			mapping["order_amount"] = ord.Amount
			mapping["order_quantity"] = ord.Quantity
		}
	}
	if itemID != "" {
		if it, err := d.store.Items.Get(ctx, d.cookieID, itemID); err == nil && it != nil {
			itemDetail := it.ItemDetail
			var detail map[string]any
			if json.Unmarshal([]byte(itemDetail), &detail) == nil {
				if d, ok := detail["detail"].(string); ok {
					itemDetail = d
				}
			}
			mapping["item_detail"] = itemDetail
		}
	}
	return replaceRecursive(params, mapping).(map[string]any)
}

func replaceRecursive(v any, mapping map[string]string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = replaceRecursive(v, mapping)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = replaceRecursive(v, mapping)
		}
		return out
	case string:
		out := x
		for k, v := range mapping {
			out = strings.ReplaceAll(out, "{"+k+"}", v)
		}
		return out
	default:
		return v
	}
}

func parseAPIContent(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return strings.TrimSpace(string(raw))
	}
	if m, ok := v.(map[string]any); ok {
		for _, key := range []string{"data", "content", "card"} {
			if val, ok := m[key]; ok && val != nil {
				return anyString(val)
			}
		}
	}
	return anyString(v)
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func anyInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

// sendContent 发送一条发货内容（文本或图片）。
func (d *DeliveryService) sendContent(ctx context.Context, content, chatID, toUserID string) error {
	if d.sender == nil {
		return nil
	}
	if strings.HasPrefix(content, "__IMAGE_SEND__") {
		rest := strings.TrimPrefix(content, "__IMAGE_SEND__")
		var cardID int64
		imageURL := rest
		if i := strings.Index(rest, "|"); i >= 0 {
			cardID = int64(parseIntSafe(rest[:i]))
			imageURL = rest[i+1:]
		}
		return d.sender.SendImage(ctx, chatID, toUserID, imageURL, cardID)
	}
	return d.sender.SendText(ctx, chatID, toUserID, content)
}

// processDeliveryContentWithDescription 复刻 Python 备注/变量替换逻辑。
func processDeliveryContentWithDescription(content, description string) string {
	if strings.HasPrefix(content, "__IMAGE_SEND__") {
		return content
	}
	desc := strings.TrimSpace(description)
	if desc == "" {
		return content
	}
	if strings.Contains(desc, "{DELIVERY_CONTENT}") {
		return strings.ReplaceAll(desc, "{DELIVERY_CONTENT}", content)
	}
	return desc + "\n\n" + content
}

func (d *DeliveryService) notify(buyerName, buyerID, itemID, message, chatID string) {
	if d.notifier != nil {
		d.notifier.NotifyDelivery(d.cookieID, buyerName, buyerID, itemID, message, chatID)
	}
}

// ---- 防重状态机 ----

func (d *DeliveryService) isLockHeld(orderID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	l, ok := d.locks[orderID]
	return ok && l.locked
}

func (d *DeliveryService) canAutoDelivery(orderID string) bool {
	if orderID == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if last, ok := d.lastDelivery[orderID]; ok {
		if time.Since(last) < DeliveryCooldown {
			return false
		}
	}
	return true
}

func (d *DeliveryService) markDeliverySent(orderID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastDelivery[orderID] = time.Now()
	// 设置延迟锁，10 分钟后释放。
	l := &deliveryLock{locked: true}
	if old, ok := d.locks[orderID]; ok && old.timer != nil {
		old.timer.Stop()
	}
	l.timer = time.AfterFunc(DeliveryLockHold, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if cur, ok := d.locks[orderID]; ok {
			cur.locked = false
			cur.releasedAt = time.Now()
		}
	})
	d.locks[orderID] = l
}

// acquireOrderLock 返回释放函数，对同一 order_id 串行化。
// 简化实现：用一个 per-order 互斥（map 里存 *sync.Mutex）。
func (d *DeliveryService) acquireOrderLock(orderID string) func() {
	d.mu.Lock()
	l, ok := d.locks[orderID]
	if !ok {
		l = &deliveryLock{}
		d.locks[orderID] = l
	}
	d.mu.Unlock()
	l.mux.Lock()
	return l.mux.Unlock
}

func (d *DeliveryService) shouldConfirm(orderID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if last, ok := d.confirmedOrder[orderID]; ok {
		if time.Since(last) < ConfirmCooldown {
			return false
		}
	}
	return true
}

func (d *DeliveryService) shouldAutoConfirm(ctx context.Context, orderID string) bool {
	if orderID == "" {
		return false
	}
	enabled, err := d.store.Cookies.GetAutoConfirm(ctx, d.cookieID)
	if err != nil {
		d.logger.Warn("读取自动确认发货设置失败，跳过确认发货", "err", err)
		return false
	}
	if !enabled {
		d.logger.Info("自动确认发货已关闭，跳过确认发货", "order_id", orderID)
		return false
	}
	return d.shouldConfirm(orderID)
}

func (d *DeliveryService) recordConfirm(orderID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.confirmedOrder[orderID] = time.Now()
}

// SetCookieSource 让 DeliveryService 与 Account 共享可变 cookie。
func (d *DeliveryService) SetCookieSource(src func() string, sink func(string)) {
	d.cookieSrc = src
	d.cookieSink = sink
}

// cookieStr 取当前 cookie。
func (d *DeliveryService) cookieStr() string {
	if d.cookieSrc != nil {
		return d.cookieSrc()
	}
	return ""
}

func (d *DeliveryService) setCookieStr(s string) {
	if d.cookieSink != nil {
		d.cookieSink(s)
	}
}

// ---- 提取与辅助 ----

// 订单 ID 提取正则（复刻 Python _extract_order_id 的多模式兜底）。
var orderIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`orderId[=:](\d{10,})`),
	regexp.MustCompile(`order_detail\?id=(\d{10,})`),
	regexp.MustCompile(`bizOrderId[=:](\d{10,})`),
}

// extractOrderID 从解密消息中提取订单 ID。
// 优先从 message["1"]["6"]["3"]["5"]（dxCard JSON）的 button.targetUrl 中提取。
func extractOrderID(decrypted map[string]any) string {
	// 方法1：从 button/main targetUrl 提取。
	if contentJSON := nestedString(decrypted, "1", "6", "3", "5"); contentJSON != "" {
		if id := extractOrderIDFromContent(contentJSON); id != "" {
			return id
		}
	}
	// 方法2：兜底——整个消息转字符串搜模式。
	raw, _ := json.Marshal(decrypted)
	rawStr := string(raw)
	for _, p := range orderIDPatterns {
		if m := p.FindStringSubmatch(rawStr); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func extractOrderIDFromContent(contentJSON string) string {
	var c map[string]any
	if err := json.Unmarshal([]byte(contentJSON), &c); err != nil {
		return ""
	}
	// dxCard.item.main.exContent.button.targetUrl → orderId=xxx
	buttonURL := nestedString(c, "dxCard", "item", "main", "exContent", "button", "targetUrl")
	if id := matchOrderID(buttonURL); id != "" {
		return id
	}
	// dxCard.item.main.targetUrl → order_detail?id=xxx
	mainURL := nestedString(c, "dxCard", "item", "main", "targetUrl")
	if id := matchOrderID(mainURL); id != "" {
		return id
	}
	// dynamicOperation.changeContent.dxCard...
	dynURL := nestedString(c, "dynamicOperation", "changeContent", "dxCard", "item", "main", "exContent", "button", "targetUrl")
	return matchOrderID(dynURL)
}

func matchOrderID(url string) string {
	if url == "" {
		return ""
	}
	for _, p := range orderIDPatterns {
		if m := p.FindStringSubmatch(url); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// nestedString 沿 path 取嵌套 map 中的字符串值。
func nestedString(m map[string]any, path ...string) string {
	cur := m
	for i, k := range path {
		if i == len(path)-1 {
			if s, ok := cur[k].(string); ok {
				return s
			}
			return ""
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return ""
		}
		cur = next
	}
	return ""
}

func filterMultiSpec(rules []db.DeliveryRule, multiSpec bool) []db.DeliveryRule {
	out := rules[:0]
	for _, r := range rules {
		if r.IsMultiSpec == multiSpec {
			out = append(out, r)
		}
	}
	return out
}

func parseIntSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
