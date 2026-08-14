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
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// automationActionExecutor 负责把动作计划转换为确认发货、卡密发送或消息发送，并集中维护凭证快照、卡券库存和外部发送结果的边界。
type automationActionExecutor struct {
	// store 提供订单、卡券、账号 Cookie 和自动化事实的持久化能力。
	store *db.Store
	// senders 提供账号当前在线的消息发送器。
	senders SenderProvider
	// mtop 返回当前生效的 MTOP 客户端，支持测试替换和运行时注入。
	mtop func() mtop.Client
	// recoverer 返回当前生效的凭证恢复器。
	recoverer func() CredentialRecoverer
	// logger 记录确认发货和本地状态持久化异常。
	logger *slog.Logger
	// cookieSource 读取测试或运行时覆盖的账号 Cookie。
	cookieSource func(context.Context, string) (string, error)
	// wakeCredentialBlocked 在 Cookie 更新后唤醒凭证阻塞的自动化任务。
	wakeCredentialBlocked func(context.Context, string)
	// cardLocks 串行化同一卡密组的数据库存取和消费。
	cardLocks sync.Map
}

// executeAction 执行一个动作，并把消息发送错误分类为可安全重试或结果未知。
func (e *automationActionExecutor) executeAction(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	switch action.ActionType {
	case ActionConfirmShipment:
		return 0, e.confirmShipment(ctx, task)
	case ActionSendCard:
		return e.sendCard(ctx, task, action)
	case ActionSendText:
		// text 是渲染后的文字动作内容。
		text := renderTemplate(action.MessageTemplate, task)
		if strings.TrimSpace(text) == "" {
			return 0, nil
		}
		// sendErr 保存文字消息发送错误。
		if sendErr := e.sendText(ctx, task, text); sendErr != nil {
			if errors.Is(sendErr, ErrMessageNotSent) {
				return 0, sendErr
			}
			return 0, uncertainAction(sendErr)
		}
		return 1, nil
	default:
		return 0, fmt.Errorf("未知自动化动作: %s", action.ActionType)
	}
}

// confirmShipment 根据账号设置和任务强制标记决定是否确认订单发货。
func (e *automationActionExecutor) confirmShipment(ctx context.Context, task Task) error {
	if task.OrderID == "" {
		return fmt.Errorf("确认发货缺少订单ID")
	}
	// enabled 表示账号是否打开自动确认发货设置。
	// readErr 保存读取自动确认发货设置的错误。
	enabled, readErr := e.store.Cookies.GetAutoConfirm(ctx, task.AccountID)
	if readErr != nil {
		return fmt.Errorf("读取自动确认发货设置: %w", readErr)
	}
	if !enabled && !task.ForceConfirmShipment {
		return nil
	}
	return e.confirmShipmentAttempt(ctx, task, true)
}

// confirmShipmentAttempt 在凭证锁内调用 Consign，并处理 Cookie 合并、凭证恢复和本地状态收口。
func (e *automationActionExecutor) confirmShipmentAttempt(ctx context.Context, task Task, allowCredentialRecovery bool) error {
	// credentialUnlock 是账号凭证锁的释放函数。
	credentialUnlock := e.store.LockAccountCredentials(task.AccountID)
	// credentialLocked 标记当前调用是否仍持有凭证锁。
	credentialLocked := true
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	// runtimeData 是确认发货所需的最小 Cookie 与 metadata 运行视图，不包含登录密码和账号资料。
	runtimeData, err := e.store.Cookies.GetCookieRuntimeData(ctx, task.AccountID)
	if err != nil {
		return err
	}
	// completeSnapshot 表示 metadata 中是否包含可恢复的完整 Cookie Jar。
	_, completeSnapshot := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON)
	if strings.TrimSpace(runtimeData.Value) == "" && !completeSnapshot {
		return fmt.Errorf("账号 %s Cookie 为空", task.AccountID)
	}
	// cookieStr 是本次确认发货使用的扁平 Cookie。
	cookieStr := runtimeData.Value
	// mtopCtx 是携带 Cookie Jar 或扁平 Cookie 的 MTOP 请求上下文。
	var mtopCtx context.Context
	// cookieSession 记录 MTOP 响应中的 Cookie 合并结果。
	var cookieSession *mtop.CookieSession
	// snapshot 是 metadata 中恢复出的完整 Cookie Jar。
	// snapshotOK 表示完整 Cookie Jar 是否存在。
	if snapshot, snapshotOK := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); snapshotOK {
		mtopCtx, cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
	} else {
		mtopCtx, cookieSession = mtop.WithFlatCookieSession(ctx, cookieStr)
	}
	// ok 表示远端确认发货是否成功。
	// ret 保存远端返回的业务错误文本。
	// updated 保存无权威 Cookie Jar 时的扁平 Cookie 更新结果。
	// callErr 保存 MTOP 请求层错误。
	// consignOK 表示远端确认发货是否成功。
	// ret 保存远端返回的业务错误文本。
	// updated 保存无权威 Cookie Jar 时的扁平 Cookie 更新结果。
	// callErr 保存 MTOP 请求层错误。
	consignOK, ret, updated, callErr := e.mtop().ConsignContext(mtopCtx, cookieStr, task.OrderID)
	// persistenceErrs 收集远端动作完成后本地状态写入失败。
	var persistenceErrs []error
	// runtimeCookie 保存需要同步到在线发送器的最新 Cookie。
	runtimeCookie := ""
	// runtimeCookieChanged 表示在线运行时是否需要替换 Cookie。
	runtimeCookieChanged := false
	// value 是 Cookie 会话合并后的扁平值。
	// snapshot 是 Cookie 会话合并后的完整快照。
	// changed 表示 Cookie 会话是否产生变化。
	value, snapshot, changed := cookieSession.State()
	// sessionHandled 表示本次请求是否由完整 Cookie Jar 接管。
	sessionHandled := snapshot != nil
	if changed {
		// metadata 是准备写回数据库的 Cookie metadata。
		metadata := cookierefresh.MetadataWithoutSnapshot(runtimeData.MetadataJSON)
		if snapshot != nil {
			metadata = cookierefresh.MetadataWithSnapshot(runtimeData.MetadataJSON, snapshot)
		}
		// saveErr 保存 Cookie Jar 写回数据库时的错误。
		if saveErr := e.store.Cookies.UpdateRenewalCookie(ctx, task.AccountID, value, metadata, time.Now().Unix()); saveErr != nil {
			persistenceErrs = append(persistenceErrs, fmt.Errorf("保存确认发货响应 Cookie Jar: %w", saveErr))
		} else if value != cookieStr {
			runtimeCookie = value
			runtimeCookieChanged = true
		}
	}
	if !sessionHandled && callErr == nil && updated != "" && updated != cookieStr {
		// 没有权威快照的历史账号保留扁平 Cookie 兼容路径，并清除可能过期的旧 Jar。
		metadata := cookierefresh.MetadataWithoutSnapshot(runtimeData.MetadataJSON)
		// saveErr 保存扁平 Cookie 写回数据库时的错误。
		if saveErr := e.store.Cookies.UpdateRenewalCookie(ctx, task.AccountID, updated, metadata, time.Now().Unix()); saveErr != nil {
			persistenceErrs = append(persistenceErrs, fmt.Errorf("保存刷新后的 Cookie: %w", saveErr))
		} else {
			runtimeCookie = updated
			runtimeCookieChanged = true
		}
	}
	credentialUnlock()
	credentialLocked = false
	if runtimeCookieChanged && e.senders != nil {
		// sender 是当前账号的在线消息发送器。
		// running 表示账号是否仍处于运行状态。
		if sender, running := e.senders.Sender(task.AccountID); running {
			sender.UpdateCookie(runtimeCookie)
		}
	}
	if runtimeCookieChanged {
		e.wakeCredentialBlocked(ctx, task.AccountID)
	}
	// sessionErr 将请求层错误和远端业务失败统一为凭证状态判断输入。
	sessionErr := callErr
	if sessionErr == nil && !consignOK {
		sessionErr = errors.New(strings.Join(ret, "; "))
	}
	if mtop.IsSessionExpiredErr(sessionErr) {
		if len(persistenceErrs) > 0 {
			return errors.Join(fmt.Errorf("确认发货 Session 已失效: %w", sessionErr), errors.Join(persistenceErrs...))
		}
		// recoverer 是当前生效的凭证恢复器快照，避免一次判断期间被替换两次。
		recoverer := e.recoverer()
		if allowCredentialRecovery && recoverer != nil && recoverer.RecoverExpiredCredential(ctx, task.AccountID) {
			e.logger.Info("确认发货凭证恢复成功，重新执行确认发货", "account", task.AccountID, "order_id", task.OrderID)
			return e.confirmShipmentAttempt(ctx, task, false)
		}
		if !allowCredentialRecovery {
			return fmt.Errorf("%w: 确认发货在凭证恢复后仍返回 Session 失效: %v", errActionNotPerformed, sessionErr)
		}
		return fmt.Errorf("%w: 确认发货 Session 已失效且凭证恢复失败: %v", errActionNotPerformed, sessionErr)
	}
	if callErr != nil {
		if len(persistenceErrs) > 0 {
			callErr = errors.Join(callErr, errors.Join(persistenceErrs...))
		}
		return uncertainAction(callErr)
	}
	if !consignOK {
		// failure 是远端拒绝确认发货的业务错误。
		failure := fmt.Errorf("确认发货失败: %s", strings.Join(ret, "; "))
		if len(persistenceErrs) > 0 {
			return errors.Join(failure, errors.Join(persistenceErrs...))
		}
		return failure
	}
	// sysShip 表示本地订单已由系统确认发货。
	sysShip := true
	// upsertErr 保存本地订单发货状态写入错误。
	if upsertErr := e.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:      task.AccountID,
		ItemID:        task.ItemID,
		BuyerID:       task.BuyerID,
		ChatID:        task.ChatID,
		OrderStatus:   "shipped",
		SystemShipped: &sysShip,
	}); upsertErr != nil {
		persistenceErrs = append(persistenceErrs, fmt.Errorf("保存本地订单发货状态: %w", upsertErr))
	}
	// eventErr 保存订单发货事实写入错误。
	if eventErr := e.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "shipped_at"); eventErr != nil {
		persistenceErrs = append(persistenceErrs, fmt.Errorf("保存订单发货时间: %w", eventErr))
	}
	if len(persistenceErrs) > 0 {
		return uncertainAction(fmt.Errorf("闲鱼已确认发货，但本地状态保存失败: %w", errors.Join(persistenceErrs...)))
	}
	return nil
}

// sendCard 按规格和购买数量分配文本、图片或数据卡密。
func (e *automationActionExecutor) sendCard(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	if !actionMatchesOrderSpec(task, action) {
		return 0, nil
	}
	if action.CardID <= 0 {
		return 0, fmt.Errorf("发送卡密动作缺少卡密组ID")
	}
	// count 是当前订单需要发送的卡密数量。
	count := deliverySendCount(task, action)
	// card 是待发送的卡密组完整配置。
	card, err := e.store.Cards.Get(ctx, action.CardID)
	if err != nil {
		return 0, err
	}
	if !card.Enabled {
		return 0, fmt.Errorf("卡密组 %d 已停用", card.ID)
	}
	if card.Type == "data" {
		return e.sendDataCard(ctx, task, card, count)
	}
	// sent 是已经成功发送的卡密数量。
	sent := 0
	// i 表示当前卡密发送序号。
	for i := 0; i < count; i++ {
		// content 是当前卡密文本内容。
		// imageURL 是当前卡密图片地址。
		content, imageURL, readErr := e.cardContent(ctx, card)
		if readErr != nil {
			return sent, readErr
		}
		if imageURL != "" {
			// sendErr 保存图片消息发送错误。
			if sendErr := e.sendImage(ctx, task, imageURL, card.ID); sendErr != nil {
				return sent, classifyMessageSendError(sendErr)
			}
		}
		if strings.TrimSpace(content) != "" {
			// sendErr 保存文字消息发送错误。
			if sendErr := e.sendText(ctx, task, renderTemplate(content, task)); sendErr != nil {
				return sent, classifyMessageSendError(sendErr)
			}
		}
		if strings.TrimSpace(content) == "" && strings.TrimSpace(imageURL) == "" {
			return sent, fmt.Errorf("卡密组 %d 没有可发送内容", card.ID)
		}
		sent++
	}
	return sent, nil
}

// sendDataCard 在卡券锁内原子消费数据卡密，并在确定未发送时恢复库存。
func (e *automationActionExecutor) sendDataCard(ctx context.Context, task Task, card *db.CardFull, count int) (int, error) {
	// unlock 释放当前卡密组的并发消费锁。
	unlock := e.lockCard(card.ID)
	defer unlock()
	// sent 是已经成功发送的数据卡密数量。
	sent := 0
	// i 表示当前数据卡密消费序号。
	for i := 0; i < count; i++ {
		// content 是从库存中原子消费出的数据卡密。
		content, err := e.store.Cards.ConsumeBatchData(ctx, card.ID)
		if err != nil {
			return sent, err
		}
		if strings.TrimSpace(content) != "" {
			// sendErr 保存数据卡密消息发送错误。
			if sendErr := e.sendText(ctx, task, renderTemplate(content, task)); sendErr != nil {
				if errors.Is(sendErr, ErrMessageNotSent) {
					// restoreErr 保存确定未发送时恢复库存的错误。
					if restoreErr := e.store.Cards.RestoreBatchData(ctx, card.ID, content); restoreErr != nil {
						return sent, uncertainAction(errors.Join(sendErr, fmt.Errorf("恢复未发送卡密库存: %w", restoreErr)))
					}
					return sent, sendErr
				}
				// 请求已交给传输层后无法判断远端是否收到，保留消费状态并人工核对。
				return sent, uncertainAction(sendErr)
			}
		}
		sent++
	}
	return sent, nil
}

// sendText 向账号在线发送器发送文字消息，并保留确定未发送的错误标记。
func (e *automationActionExecutor) sendText(ctx context.Context, task Task, text string) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("%w: 发送消息缺少 chat_id 或 buyer_id", ErrMessageNotSent)
	}
	if e.senders == nil {
		return fmt.Errorf("%w: 账号发送器未初始化", ErrMessageNotSent)
	}
	// sender 是当前账号的在线消息发送器。
	// senderOK 表示是否找到在线发送器。
	sender, senderOK := e.senders.Sender(task.AccountID)
	if !senderOK {
		return fmt.Errorf("%w: 账号未在线，无法发送自动化消息", ErrMessageNotSent)
	}
	return sender.SendText(ctx, task.ChatID, task.BuyerID, text)
}

// sendImage 向账号在线发送器发送图片消息，并标记关联卡密组。
func (e *automationActionExecutor) sendImage(ctx context.Context, task Task, imageURL string, cardID int64) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("%w: 发送图片缺少 chat_id 或 buyer_id", ErrMessageNotSent)
	}
	if e.senders == nil {
		return fmt.Errorf("%w: 账号发送器未初始化", ErrMessageNotSent)
	}
	// sender 是当前账号的在线图片发送器。
	// senderOK 表示是否找到在线发送器。
	sender, senderOK := e.senders.Sender(task.AccountID)
	if !senderOK {
		return fmt.Errorf("%w: 账号未在线，无法发送自动化图片", ErrMessageNotSent)
	}
	return sender.SendImage(ctx, task.ChatID, task.BuyerID, imageURL, cardID)
}

// cardContent 读取卡密组内容并拒绝不支持的卡密类型。
func (e *automationActionExecutor) cardContent(_ context.Context, card *db.CardFull) (text, imageURL string, err error) {
	switch card.Type {
	case "text":
		if strings.TrimSpace(card.TextContent) == "" {
			return "", "", fmt.Errorf("文本卡密组缺少内容")
		}
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

// cookieValue 读取账号 Cookie，优先使用测试或运行时覆盖的数据源。
func (e *automationActionExecutor) cookieValue(ctx context.Context, cookieID string) (string, error) {
	return e.cookieSource(ctx, cookieID)
}

// lockCard 获取指定卡密组的进程内互斥锁。
func (e *automationActionExecutor) lockCard(cardID int64) func() {
	// raw 是按卡密组 ID 保存的锁实例。
	raw, _ := e.cardLocks.LoadOrStore(cardID, &sync.Mutex{})
	// mu 是当前卡密组的互斥锁。
	mu := raw.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// actionMatchesOrderSpec 判断动作配置的规格是否匹配订单。
func actionMatchesOrderSpec(task Task, action db.AutomationAction) bool {
	// cfg 保存卡密动作的规格过滤配置。
	var cfg struct {
		SpecName  string `json:"spec_name"`
		SpecValue string `json:"spec_value"`
	}
	if json.Unmarshal([]byte(action.ConfigJSON), &cfg) != nil {
		return false
	}
	if strings.TrimSpace(cfg.SpecName) == "" && strings.TrimSpace(cfg.SpecValue) == "" {
		return true
	}
	return strings.TrimSpace(task.SpecName) == strings.TrimSpace(cfg.SpecName) &&
		strings.TrimSpace(task.SpecValue) == strings.TrimSpace(cfg.SpecValue)
}

// deliverySendCount 根据动作单份数量和订单购买数量计算实际发送数量。
func deliverySendCount(task Task, action db.AutomationAction) int {
	// perUnit 是每个购买单位对应的卡密数量。
	perUnit := action.DeliveryCount
	if perUnit <= 0 {
		perUnit = 1
	}
	// qty 是订单购买数量。
	qty := parsePositiveInt(task.Quantity)
	if qty <= 0 {
		qty = 1
	}
	return perUnit * qty
}

// parsePositiveInt 将数量文本解析为非负整数，非法值返回零。
func parsePositiveInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// n 是解析后的数量值。
	// err 是数量文本解析错误。
	// n 是解析后的数量值。
	// parseErr 是数量文本解析错误。
	n, parseErr := strconv.Atoi(raw)
	if parseErr != nil || n < 0 {
		return 0
	}
	return n
}

// classifyMessageSendError 将消息发送错误统一分类为确定未发送或结果未知。
func classifyMessageSendError(err error) error {
	if err == nil || errors.Is(err, ErrMessageNotSent) {
		return err
	}
	return uncertainAction(err)
}

// renderTemplate 替换自动化消息模板中的订单字段。
func renderTemplate(tpl string, task Task) string {
	// out 是经过字段替换后的模板文本。
	out := tpl
	// repl 保存模板占位符与订单字段的映射。
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
	// key 和 value 分别表示当前占位符及其替换值。
	for key, value := range repl {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}
