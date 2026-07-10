package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// SenderProvider 根据账号 ID 提供当前在线账号的发送能力。
// 计划任务和 WS 事件都复用这个抽象，避免自动化中心直接依赖 account.Manager。
type SenderProvider interface {
	Sender(cookieID string) (MessageSender, bool)
}

// MessageSender 是自动化动作需要的最小发送接口。
type MessageSender interface {
	SendText(ctx context.Context, chatID, toUserID, text string) error
	SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64) error
	UpdateCookie(cookieStr string)
}

// OrderDetailFetcher 查询闲鱼订单详情。自动发货必须先拿到订单规格和购买数量，
// 再按规则里的规格映射发货。
type OrderDetailFetcher interface {
	FetchOrderDetail(ctx context.Context, cookieID, orderID, itemID, buyerID, cookieStr string) (*OrderDetail, error)
}

// Notifier 发货结果通知器（多渠道）。可选依赖，未注入时跳过通知。
type Notifier interface {
	NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string)
}

// Center 是统一自动化处理中心。
// 它只接收已经分类好的系统事件或计划任务，不处理用户消息；用户消息由 engine 的回复链处理。
type Center struct {
	store     *db.Store
	senders   SenderProvider
	fetcher   OrderDetailFetcher
	notifier  Notifier
	mtop      mtop.Client
	logger    *slog.Logger
	cookieSrc func(context.Context, string) (string, error)
	cardLocks sync.Map
}

// New 构造自动化中心。
func New(store *db.Store, senders SenderProvider, logger *slog.Logger) *Center {
	if logger == nil {
		logger = slog.Default()
	}
	return &Center{store: store, senders: senders, mtop: mtop.NewClient(), logger: logger.With("subsys", "automation")}
}

// SetMTop 注入 mtop 客户端（确认发货用）。未注入时使用默认 HTTP 实现。
func (c *Center) SetMTop(m mtop.Client) { c.mtop = m }

// SetOrderDetailFetcher 注入订单详情查询能力。
func (c *Center) SetOrderDetailFetcher(fetcher OrderDetailFetcher) {
	c.fetcher = fetcher
}

// SetNotifier 注入发货结果通知器。
func (c *Center) SetNotifier(n Notifier) {
	c.notifier = n
}

// SetCookieSource 覆盖 cookie 读取逻辑，便于测试。
func (c *Center) SetCookieSource(fn func(context.Context, string) (string, error)) {
	c.cookieSrc = fn
}

// HandleTask 处理一条自动化任务。无匹配规则时安全忽略。
func (c *Center) HandleTask(ctx context.Context, task Task) error {
	if c == nil || c.store == nil || c.store.Automation == nil {
		return nil
	}
	task.TriggerType = strings.TrimSpace(task.TriggerType)
	if task.TriggerType == "" || task.AccountID == "" {
		return nil
	}
	c.markEventFacts(ctx, task)
	rules, err := c.store.Automation.Match(ctx, task.AccountID, task.ItemID, task.TriggerType)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		c.logger.Info("无匹配自动化规则，忽略事件", "trigger", task.TriggerType, "order_id", task.OrderID, "item_id", task.ItemID)
		return nil
	}
	for _, rule := range rules {
		if err := c.executeRule(ctx, task, rule); err != nil {
			c.logger.Error("自动化规则执行失败", "rule_id", rule.ID, "trigger", task.TriggerType, "err", err)
		}
	}
	return nil
}

// ManualFullDelivery 对已存在订单执行完整发货，和付款系统事件共用同一套
// 订单详情补全、规格匹配、按购买数量发卡、确认发货逻辑。
func (c *Center) ManualFullDelivery(ctx context.Context, order *db.Order) (int, error) {
	if c == nil || c.store == nil || order == nil {
		return 0, fmt.Errorf("自动化中心未初始化或订单为空")
	}
	if strings.TrimSpace(order.OrderID) == "" {
		return 0, fmt.Errorf("订单缺少订单ID")
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return 0, fmt.Errorf("订单缺少账号ID")
	}
	if strings.TrimSpace(order.ItemID) == "" {
		return 0, fmt.Errorf("订单缺少商品ID，无法匹配自动化规则")
	}
	if strings.TrimSpace(order.ChatID) == "" || strings.TrimSpace(order.BuyerID) == "" {
		return 0, fmt.Errorf("订单缺少 chat_id 或 buyer_id，无法发送卡券")
	}
	task := Task{
		Source:      "manual",
		AccountID:   order.CookieID,
		TriggerType: TriggerOrderPaid,
		ChatID:      order.ChatID,
		OrderID:     order.OrderID,
		ItemID:      order.ItemID,
		BuyerID:     order.BuyerID,
		SpecName:    order.SpecName,
		SpecValue:   order.SpecValue,
		Quantity:    order.Quantity,
		Amount:      order.Amount,
		OrderStatus: order.OrderStatus,
		Raw:         map[string]any{"manual": true},
	}
	task, err := c.prepareTask(ctx, task)
	if err != nil {
		return 0, err
	}
	rules, err := c.store.Automation.Match(ctx, task.AccountID, task.ItemID, TriggerOrderPaid)
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, fmt.Errorf("未匹配到付款后自动发货规则")
	}
	sent := 0
	for _, rule := range rules {
		if !hasMatchingSendCard(task, rule.Actions) {
			continue
		}
		n, err := c.executePaidDeliveryActions(ctx, task, rule.Actions)
		sent += n
		if err != nil {
			return sent, err
		}
		if n > 0 {
			break
		}
	}
	if sent == 0 {
		return 0, fmt.Errorf("未匹配到订单规格对应的卡密动作")
	}
	return sent, nil
}

func (c *Center) executeRule(ctx context.Context, task Task, rule db.AutomationRule) error {
	var err error
	task, err = c.prepareTask(ctx, task)
	if err != nil {
		return err
	}
	triggerKey := buildTriggerKey(task)
	if triggerKey == "" {
		return nil
	}
	rawJSON, _ := json.Marshal(task.Raw)
	runID, started, err := c.store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID:       rule.ID,
		CookieID:     task.AccountID,
		ItemID:       task.ItemID,
		OrderID:      task.OrderID,
		BuyerID:      task.BuyerID,
		ChatID:       task.ChatID,
		TriggerType:  task.TriggerType,
		TriggerKey:   triggerKey,
		RawEventJSON: string(rawJSON),
	})
	if err != nil || !started {
		return err
	}
	status := "success"
	errMsg := ""
	sent := 0
	defer func() {
		_ = c.store.Automation.FinishRun(context.Background(), runID, status, sent, errMsg)
		if c.notifier != nil {
			c.notifyResult(task, status, sent, errMsg)
		}
	}()
	if task.TriggerType == TriggerOrderPaid && !hasMatchingSendCard(task, rule.Actions) {
		status, errMsg = "failed", "未匹配到订单规格对应的卡密动作"
		return errors.New(errMsg)
	}
	if task.TriggerType == TriggerOrderPaid {
		sent, err = c.executePaidDeliveryActions(ctx, task, rule.Actions)
	} else {
		sent, err = c.executeRuleActions(ctx, task, rule.Actions)
	}
	if err != nil {
		status, errMsg = "failed", err.Error()
		return err
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		_ = c.store.Automation.IncrementReviewRequest(ctx, task.OrderID)
	}
	return nil
}

// notifyResult 根据规则执行结果发送通知。成功且实际发出了内容才通知，
// 避免无匹配动作的空跑刷屏；失败时发失败通知。
func (c *Center) notifyResult(task Task, status string, sent int, errMsg string) {
	triggerName := map[string]string{
		TriggerOrderPaid:            "付款发货",
		TriggerBuyerReviewed:        "评价赠品",
		TriggerReviewMissingTimeout: "求评价",
	}[task.TriggerType]
	if triggerName == "" {
		triggerName = task.TriggerType
	}
	if status == "success" {
		if sent <= 0 {
			return
		}
		msg := fmt.Sprintf("✅ %s成功（订单 %s，已发送 %d 条）", triggerName, task.OrderID, sent)
		c.notifier.NotifyDelivery(task.AccountID, "", task.BuyerID, task.ItemID, msg, task.ChatID)
		return
	}
	msg := fmt.Sprintf("🚨 %s失败（订单 %s）：%s", triggerName, task.OrderID, errMsg)
	c.notifier.NotifyDelivery(task.AccountID, "", task.BuyerID, task.ItemID, msg, task.ChatID)
}

func (c *Center) executeRuleActions(ctx context.Context, task Task, actions []db.AutomationAction) (int, error) {
	sent := 0
	for _, action := range actions {
		n, err := c.executeActionWithDelay(ctx, task, action)
		sent += n
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}

func (c *Center) executePaidDeliveryActions(ctx context.Context, task Task, actions []db.AutomationAction) (int, error) {
	// 同一商品或同一规格允许配置多条发货内容：先按顺序执行全部匹配的
	// send_card 动作，任意一条失败都停止，只有全部成功后才确认发货。
	sent := 0
	for _, action := range actions {
		if action.ActionType != ActionSendCard || !actionMatchesOrderSpec(task, action) {
			continue
		}
		n, err := c.executeActionWithDelay(ctx, task, action)
		sent += n
		if err != nil {
			return sent, err
		}
	}
	if sent == 0 {
		return 0, errors.New("未匹配到订单规格对应的卡密动作")
	}
	for _, action := range actions {
		if action.ActionType != ActionConfirmShipment {
			continue
		}
		if _, err := c.executeActionWithDelay(ctx, task, action); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

func hasMatchingSendCard(task Task, actions []db.AutomationAction) bool {
	for _, action := range actions {
		if action.Enabled && action.ActionType == ActionSendCard && actionMatchesOrderSpec(task, action) {
			return true
		}
	}
	return false
}

func (c *Center) executeActionWithDelay(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	if !action.Enabled {
		return 0, nil
	}
	if action.DelaySeconds > 0 {
		if err := sleepCtx(ctx, time.Duration(action.DelaySeconds)*time.Second); err != nil {
			return 0, err
		}
	}
	return c.executeAction(ctx, task, action)
}

func (c *Center) prepareTask(ctx context.Context, task Task) (Task, error) {
	if task.OrderID == "" {
		return task, nil
	}
	_ = c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID: task.AccountID,
		ItemID:   task.ItemID,
		BuyerID:  task.BuyerID,
		ChatID:   task.ChatID,
	})
	needsDetail := task.TriggerType == TriggerOrderPaid || task.TriggerType == TriggerBuyerReviewed
	if existing, err := c.store.Orders.Get(ctx, task.OrderID); err == nil && existing != nil {
		task = mergeOrderIntoTask(task, existing)
		if existing.Quantity == "" || existing.Amount == "" {
			needsDetail = true
		}
		// 规则是否多规格由 action.config_json 决定；这里无法提前知道命中的 action，
		// 因此交易类事件统一补齐规格，确保后续规格映射有事实依据。
		if existing.SpecName == "" || existing.SpecValue == "" {
			needsDetail = true
		}
	}
	if !needsDetail || c.fetcher == nil {
		return task, nil
	}
	cookieStr := task.CookieStr
	if strings.TrimSpace(cookieStr) == "" {
		var err error
		cookieStr, err = c.cookieValue(ctx, task.AccountID)
		if err != nil {
			return task, err
		}
	}
	detail, err := c.fetcher.FetchOrderDetail(ctx, task.AccountID, task.OrderID, task.ItemID, task.BuyerID, cookieStr)
	if err != nil {
		return task, err
	}
	if detail == nil {
		return task, nil
	}
	if detail.Quantity != "" {
		task.Quantity = detail.Quantity
	}
	if detail.SpecName != "" {
		task.SpecName = detail.SpecName
	}
	if detail.SpecValue != "" {
		task.SpecValue = detail.SpecValue
	}
	if detail.Amount != "" {
		task.Amount = detail.Amount
	}
	if detail.OrderStatus != "" {
		task.OrderStatus = detail.OrderStatus
	}
	_ = c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:    task.AccountID,
		ItemID:      task.ItemID,
		BuyerID:     task.BuyerID,
		ChatID:      task.ChatID,
		SpecName:    task.SpecName,
		SpecValue:   task.SpecValue,
		Quantity:    task.Quantity,
		Amount:      task.Amount,
		OrderStatus: task.OrderStatus,
	})
	return task, nil
}

func mergeOrderIntoTask(task Task, order *db.Order) Task {
	if task.ItemID == "" {
		task.ItemID = order.ItemID
	}
	if task.BuyerID == "" {
		task.BuyerID = order.BuyerID
	}
	if task.ChatID == "" {
		task.ChatID = order.ChatID
	}
	if task.SpecName == "" {
		task.SpecName = order.SpecName
	}
	if task.SpecValue == "" {
		task.SpecValue = order.SpecValue
	}
	if task.Quantity == "" {
		task.Quantity = order.Quantity
	}
	if task.Amount == "" {
		task.Amount = order.Amount
	}
	if task.OrderStatus == "" {
		task.OrderStatus = order.OrderStatus
	}
	return task
}

func (c *Center) executeAction(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	switch action.ActionType {
	case ActionConfirmShipment:
		return 0, c.confirmShipment(ctx, task)
	case ActionSendCard:
		return c.sendCard(ctx, task, action)
	case ActionSendText:
		text := renderTemplate(action.MessageTemplate, task)
		if strings.TrimSpace(text) == "" {
			return 0, nil
		}
		return 1, c.sendText(ctx, task, text)
	default:
		return 0, fmt.Errorf("未知自动化动作: %s", action.ActionType)
	}
}

func (c *Center) confirmShipment(ctx context.Context, task Task) error {
	if task.OrderID == "" {
		return fmt.Errorf("确认发货缺少订单ID")
	}
	enabled, err := c.store.Cookies.GetAutoConfirm(ctx, task.AccountID)
	if err == nil && !enabled {
		return nil
	}
	cookieStr := task.CookieStr
	if strings.TrimSpace(cookieStr) == "" {
		cookieStr, err = c.cookieValue(ctx, task.AccountID)
		if err != nil {
			return err
		}
	}
	ok, ret, updated, err := c.mtop.ConsignContext(ctx, cookieStr, task.OrderID)
	if err != nil {
		return err
	}
	if updated != "" && updated != cookieStr {
		_ = c.store.Cookies.Save(ctx, task.AccountID, updated, 0)
		if c.senders != nil {
			if sender, running := c.senders.Sender(task.AccountID); running {
				sender.UpdateCookie(updated)
			}
		}
	}
	if !ok {
		return fmt.Errorf("确认发货失败: %s", strings.Join(ret, "; "))
	}
	sysShip := true
	_ = c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:      task.AccountID,
		ItemID:        task.ItemID,
		BuyerID:       task.BuyerID,
		ChatID:        task.ChatID,
		OrderStatus:   "shipped",
		SystemShipped: &sysShip,
	})
	_ = c.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "shipped_at")
	return nil
}

func (c *Center) sendCard(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	if !actionMatchesOrderSpec(task, action) {
		return 0, nil
	}
	if action.CardID <= 0 {
		return 0, fmt.Errorf("发送卡密动作缺少卡密组ID")
	}
	count := deliverySendCount(task, action)
	card, err := c.store.Cards.Get(ctx, action.CardID)
	if err != nil {
		return 0, err
	}
	if card.Type == "data" {
		return c.sendDataCard(ctx, task, card, count)
	}
	sent := 0
	for i := 0; i < count; i++ {
		content, imageURL, err := c.cardContent(ctx, card)
		if err != nil {
			return sent, err
		}
		if imageURL != "" {
			if err := c.sendImage(ctx, task, imageURL, card.ID); err != nil {
				return sent, err
			}
		}
		if strings.TrimSpace(content) != "" {
			if err := c.sendText(ctx, task, renderTemplate(content, task)); err != nil {
				return sent, err
			}
		}
		sent++
	}
	return sent, nil
}

func (c *Center) sendDataCard(ctx context.Context, task Task, card *db.CardFull, count int) (int, error) {
	unlock := c.lockCard(card.ID)
	defer unlock()
	sent := 0
	for i := 0; i < count; i++ {
		content, snapshot, err := c.store.Cards.FirstBatchData(ctx, card.ID)
		if err != nil {
			return sent, err
		}
		if strings.TrimSpace(content) != "" {
			if err := c.sendText(ctx, task, renderTemplate(content, task)); err != nil {
				return sent, err
			}
		}
		if err := c.store.Cards.CommitFirstBatchData(ctx, card.ID, snapshot); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

func (c *Center) lockCard(cardID int64) func() {
	raw, _ := c.cardLocks.LoadOrStore(cardID, &sync.Mutex{})
	mu := raw.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func actionMatchesOrderSpec(task Task, action db.AutomationAction) bool {
	var cfg struct {
		SpecName  string `json:"spec_name"`
		SpecValue string `json:"spec_value"`
	}
	if json.Unmarshal([]byte(action.ConfigJSON), &cfg) != nil {
		return true
	}
	if strings.TrimSpace(cfg.SpecName) == "" && strings.TrimSpace(cfg.SpecValue) == "" {
		return true
	}
	return strings.TrimSpace(task.SpecName) == strings.TrimSpace(cfg.SpecName) &&
		strings.TrimSpace(task.SpecValue) == strings.TrimSpace(cfg.SpecValue)
}

func deliverySendCount(task Task, action db.AutomationAction) int {
	perUnit := action.DeliveryCount
	if perUnit <= 0 {
		perUnit = 1
	}
	qty := parsePositiveInt(task.Quantity)
	if qty <= 0 {
		qty = 1
	}
	return perUnit * qty
}

func parsePositiveInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (c *Center) cardContent(ctx context.Context, card *db.CardFull) (text, imageURL string, err error) {
	switch card.Type {
	case "text":
		return card.TextContent, "", nil
	case "data":
		return "", "", fmt.Errorf("data 卡密必须通过 sendDataCard 发送")
	case "image":
		if strings.TrimSpace(card.ImageURL) == "" {
			return "", "", fmt.Errorf("图片卡密组缺少图片 URL")
		}
		return "", card.ImageURL, nil
	case "api":
		return "", "", fmt.Errorf("自动化中心暂不支持 API 卡密动作")
	default:
		return "", "", fmt.Errorf("未知卡密类型: %s", card.Type)
	}
}

func (c *Center) sendText(ctx context.Context, task Task, text string) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("发送消息缺少 chat_id 或 buyer_id")
	}
	if c.senders == nil {
		return fmt.Errorf("账号发送器未初始化")
	}
	sender, ok := c.senders.Sender(task.AccountID)
	if !ok {
		return fmt.Errorf("账号未在线，无法发送自动化消息")
	}
	return sender.SendText(ctx, task.ChatID, task.BuyerID, text)
}

func (c *Center) sendImage(ctx context.Context, task Task, imageURL string, cardID int64) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("发送图片缺少 chat_id 或 buyer_id")
	}
	if c.senders == nil {
		return fmt.Errorf("账号发送器未初始化")
	}
	sender, ok := c.senders.Sender(task.AccountID)
	if !ok {
		return fmt.Errorf("账号未在线，无法发送自动化图片")
	}
	return sender.SendImage(ctx, task.ChatID, task.BuyerID, imageURL, cardID)
}

func (c *Center) cookieValue(ctx context.Context, cookieID string) (string, error) {
	if c.cookieSrc != nil {
		return c.cookieSrc(ctx, cookieID)
	}
	d, err := c.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		return "", err
	}
	return d.Value, nil
}

func (c *Center) markEventFacts(ctx context.Context, task Task) {
	if task.OrderID == "" {
		return
	}
	if err := c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:    task.AccountID,
		ItemID:      task.ItemID,
		BuyerID:     task.BuyerID,
		ChatID:      task.ChatID,
		OrderStatus: task.OrderStatus,
		SpecName:    task.SpecName,
		SpecValue:   task.SpecValue,
		Quantity:    task.Quantity,
		Amount:      task.Amount,
	}); err != nil {
		c.logger.Warn("记录自动化事件事实前创建订单失败", "order_id", task.OrderID, "trigger", task.TriggerType, "err", err)
		return
	}
	switch task.TriggerType {
	case TriggerOrderPaid:
		_ = c.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "paid_at")
	case TriggerBuyerReviewed:
		_ = c.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "buyer_reviewed_at")
	}
}

func buildTriggerKey(task Task) string {
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		if attempt, ok := task.Raw["attempt"]; ok {
			return fmt.Sprintf("%s:%s:%v", task.TriggerType, task.OrderID, attempt)
		}
	}
	if task.OrderID != "" {
		return task.TriggerType + ":" + task.OrderID
	}
	if task.UpdateKey != "" {
		return task.TriggerType + ":" + task.UpdateKey
	}
	return ""
}

func renderTemplate(tpl string, task Task) string {
	out := tpl
	repl := map[string]string{
		"{order_id}":     task.OrderID,
		"{item_id}":      task.ItemID,
		"{buyer_id}":     task.BuyerID,
		"{chat_id}":      task.ChatID,
		"{trigger_type}": task.TriggerType,
		"{spec_name}":    task.SpecName,
		"{spec_value}":   task.SpecValue,
		"{quantity}":     task.Quantity,
		"{amount}":       task.Amount,
	}
	for k, v := range repl {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
