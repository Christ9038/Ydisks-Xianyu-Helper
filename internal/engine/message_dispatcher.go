package engine

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/logging"
)

// messageDispatcher 负责 WebSocket 消息的事实解析、去重、防抖、出站回显观察和并发投递。
// 各锁只保护本组件字段；持锁时不执行 handler、回复服务或数据库 I/O。
// messageDispatcher 用于本次流程后续判断的消息Dispatcher
type messageDispatcher struct {
	// dedupMu 保护 processed 去重时间表。
	dedupMu sync.Mutex
	// processed 保存最近已处理消息的稳定 ID 和时间。
	processed map[string]time.Time
	// debounceMu 保护 debounceTimers 防抖定时器表。
	debounceMu sync.Mutex
	// debounceTimers 保存每个聊天会话当前的防抖句柄。
	debounceTimers map[string]*debounceEntry
	// sem 限制同时执行的消息处理任务数量。
	sem chan struct{}
	// cookieID 是消息所属账号标识。
	cookieID string
	// currentCookie 返回当前账号 Cookie 快照，不在本组件中持有凭证。
	currentCookie func() string
	// currentHandler 返回最新的系统事件和聊天旁路处理器。
	currentHandler func() Handler
	// observeOutgoing 将账号自身回显交给账号级发送确认器；回调不得执行不可控外部 I/O。
	observeOutgoing func(OutgoingChatMessage)
	// reply 负责自动回复链，可能为空。
	reply *ReplyService
	// manualTakeover 是账号级人工介入跟踪器，判定自身回显是否为人工发送；为空时不识别人工介入。
	manualTakeover *manualTakeoverTracker
	// logger 记录消息分发和防抖错误。
	logger *slog.Logger
	// beginTask 登记账号生命周期任务，并返回该任务唯一的释放函数。
	beginTask func() (context.Context, func(), bool)
	// recordMessage 更新账号最近收消息时间。
	recordMessage func(time.Time)
}

// messageDispatcherConfig 描述消息分发组件所需的窄依赖。
type messageDispatcherConfig struct {
	// CookieID 是消息所属账号标识。
	CookieID string
	// CurrentCookie 返回当前 Cookie 快照。
	CurrentCookie func() string
	// CurrentHandler 返回最新的系统事件和聊天旁路处理器。
	CurrentHandler func() Handler
	// ObserveOutgoing 接收账号自身 WebSocket 回显，供自动化发送确认使用。
	ObserveOutgoing func(OutgoingChatMessage)
	// Reply 是自动回复服务。
	Reply *ReplyService
	// ManualTakeover 是账号级人工介入跟踪器；为空时自身回显只用于发送确认和落库。
	ManualTakeover *manualTakeoverTracker
	// Logger 记录分发过程中的错误和诊断信息。
	Logger *slog.Logger
	// BeginTask 登记账号生命周期任务，并返回该任务唯一的释放函数。
	BeginTask func() (context.Context, func(), bool)
	// RecordMessage 更新最近收到消息时间。
	RecordMessage func(time.Time)
}

// newMessageDispatcher 构造消息分发组件并初始化其有界状态。
func newMessageDispatcher(config messageDispatcherConfig) messageDispatcher {
	// logger 用于本次流程后续判断的logger
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// currentCookie 用于本次流程后续判断的current登录凭证
	currentCookie := config.CurrentCookie
	if currentCookie == nil {
		currentCookie = func() string { return "" }
	}
	// beginTask 用于本次流程后续判断的begin任务
	beginTask := config.BeginTask
	if beginTask == nil {
		// 缺少生命周期登记器时使用受限同步兼容预算，测试构造不能产生长期游离任务。
		beginTask = func() (context.Context, func(), bool) {
			// fallbackCtx 是没有账号 owner 时仅供一次消息分发使用的有限 Context。
			fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), bootstrapTaskTimeout)
			return fallbackCtx, fallbackCancel, true
		}
	}
	// recordMessage 用于本次流程后续判断的record消息
	recordMessage := config.RecordMessage
	if recordMessage == nil {
		recordMessage = func(time.Time) {}
	}
	// currentHandler 用于本次流程后续判断的currentHandler
	currentHandler := config.CurrentHandler
	if currentHandler == nil {
		currentHandler = func() Handler { return nil }
	}
	// observeOutgoing 保存账号级自身回显观察回调；未注入时保持旧版仅过滤回显语义。
	observeOutgoing := config.ObserveOutgoing
	if observeOutgoing == nil {
		observeOutgoing = func(OutgoingChatMessage) {}
	}
	return messageDispatcher{
		processed:       make(map[string]time.Time),
		debounceTimers:  make(map[string]*debounceEntry),
		sem:             make(chan struct{}, MessageSemaphoreSize),
		cookieID:        config.CookieID,
		currentCookie:   currentCookie,
		currentHandler:  currentHandler,
		observeOutgoing: observeOutgoing,
		reply:           config.Reply,
		manualTakeover:  config.ManualTakeover,
		logger:          logger,
		beginTask:       beginTask,
		recordMessage:   recordMessage,
	}
}

// dispatch 接收一条解密消息并安排系统事件或聊天消息处理。
func (d *messageDispatcher) dispatch(decrypted map[string]any) {
	// ctx、finish、ok 分别是当前消息任务的上下文、释放函数与生命周期接纳结果。
	ctx, finish, ok := d.beginTask()
	if !ok {
		return
	}
	d.recordMessage(time.Now())
	// 系统业务事件不能丢弃：并发满时让 WS 读取产生背压，等待处理槽位。
	// 普通聊天仍采用非阻塞限流，避免聊天洪峰拖垮连接。
	// isSystemEvent 用于本次流程后续判断的is系统Event
	isSystemEvent := automation.ExtractTaskFromWS(d.cookieID, d.currentCookie(), decrypted) != nil
	if isSystemEvent {
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			finish()
			return
		}
		go func() {
			defer finish()
			defer func() { <-d.sem }()
			d.handleMessageContext(ctx, decrypted)
		}()
		return
	}

	select {
	case d.sem <- struct{}{}:
	default:
		finish()
		d.logger.Warn("消息处理并发达上限，丢弃消息", "limit", MessageSemaphoreSize)
		return
	}
	go func() {
		defer finish()
		defer func() { <-d.sem }()
		d.handleMessageContext(ctx, decrypted)
	}()
}

// handleMessage 分类并投递消息，供 Account facade 和测试调用。
func (d *messageDispatcher) handleMessage(decrypted map[string]any) {
	// ctx、finish、accepted 分别是同步分发任务的上下文、释放函数及生命周期接纳结果。
	ctx, finish, accepted := d.beginTask()
	if !accepted {
		return
	}
	defer finish()
	d.handleMessageContext(ctx, decrypted)
}

// handleMessageContext 将系统事件和聊天消息分别交给对应业务链。
func (d *messageDispatcher) handleMessageContext(ctx context.Context, decrypted map[string]any) {
	// 每条消息的处理链都带账号标识，系统事件分支再补充订单与触发类型，聊天分支由回复链补充 chat_id。
	ctx = logging.With(ctx, slog.String("account", d.cookieID))
	// observedAt 在分类和防抖之前固定消息的本地接纳顺序，后续数据库写入延迟不会改变它与删除动作的先后关系。
	observedAt := time.Now().UTC().UnixMilli()
	// receipt、ok 保存解析出的平台已读回执及是否命中已读事件格式。
	if receipt, ok := extractMessageReadEvent(decrypted); ok {
		receipt.AccountID = d.cookieID
		// handler 保存当前可选处理器；旧集成不实现回执端口时保持兼容。
		if handler := d.currentHandler(); handler != nil {
			// reader 是可消费已读回执的可选端口；supported 表示当前集成实现了该端口。
			if reader, supported := handler.(MessageReadHandler); supported {
				// err 保存回执持久化错误；失败不应中断 WebSocket 接收循环。
				if err := reader.HandleMessageRead(ctx, receipt); err != nil {
					d.logger.WarnContext(ctx, "处理聊天已读回执失败", "err", err, "message_id", receipt.MessageID)
				}
			}
		}
		return
	}
	// 系统卡片和平台通知优先进入自动化中心，永远不进入 AI 回复范围。
	if task := automation.ExtractTaskFromWS(d.cookieID, d.currentCookie(), decrypted); task != nil {
		ctx = logging.With(ctx, slog.String("order_id", task.OrderID), slog.String("trigger", task.TriggerType))
		if // handler 用于本次流程后续判断的handler
		handler := d.currentHandler(); handler != nil {
			if // err 用于本次流程后续判断的err
			err := handler.HandleSystemEvent(ctx, *task); err != nil {
				d.logger.ErrorContext(ctx, "处理系统自动化事件失败", "err", err)
			}
		}
		return
	}
	// ownEcho 保存当前账号从官方客户端发出后回显的消息；它必须实时落库，但绝不能进入自动回复防抖链。
	if ownEcho := extractOwnWebSocketEcho(decrypted, d.cookieID, d.currentCookie()); ownEcho != nil {
		ownEcho.ObservedAt = observedAt
		// observeOutgoing 先于业务旁路执行，确保等待中的自动化发送不受数据库写入延迟影响。
		d.observeOutgoing(*ownEcho)
		// 未被本系统出站登记消费的新鲜自身回显来自官方客户端的人工发送，会话进入人工介入暂停窗口；判定只读写内存。
		if d.manualTakeover.observeOwnEcho(*ownEcho, echoCreatedAt(decrypted)) {
			d.logger.InfoContext(ctx, "检测到人工回复，会话进入人工介入暂停窗口", "chat_id", ownEcho.ChatID)
			// 人工已经回答：取消尚未开始的合并等待，并顶替正在生成但未提交的 AI/Agent 回复。
			d.cancelPendingReply(ownEcho.ChatID)
		}
		// handler 保存当前可选业务处理器；旧集成未实现出站观察能力时保持原有仅过滤语义。
		if handler := d.currentHandler(); handler != nil {
			// observer、supported 保存出站观察接口及其实现判断，避免扩大基础 Handler 的必选职责。
			if observer, supported := handler.(outgoingChatHandler); supported {
				// err 保存自身回显持久化或实时发布失败；失败不应影响 WebSocket 接收循环或误触发自动回复。
				if err := observer.HandleOutgoingChatMessage(ctx, *ownEcho); err != nil {
					d.logger.WarnContext(ctx, "处理官方客户端出站消息回显失败", "err", err, "chat_id", ownEcho.ChatID)
				}
			}
		}
		return
	}

	// chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, d.cookieID, d.currentCookie())
	if chat != nil && chat.Text != "" {
		chat.ObservedAt = observedAt
		if !d.markAndCheckDedup(decrypted, chat) {
			return
		}
		d.receiveChat(ctx, *chat)
	}
}

// markAndCheckDedup 提取消息 ID，检查有效期内是否已经处理。
func (d *messageDispatcher) markAndCheckDedup(decrypted map[string]any, chat *ChatMessage) bool {
	// msgID 用于本次流程后续判断的msgID
	msgID := extractMessageID(decrypted)
	if msgID == "" {
		// 备用标识：chat_id + text + create_time。
		createTime := "0"
		if // m1、ok 用于本次流程后续判断的m1、ok
		m1, ok := decrypted["1"].(map[string]any); ok {
			if // t、ok 用于本次流程后续判断的t、ok
			t, ok := m1["5"]; ok {
				createTime = toString(t)
			}
		}
		msgID = chat.ChatID + "_" + chat.Text + "_" + createTime
	}

	d.dedupMu.Lock()
	defer d.dedupMu.Unlock()
	// now 用于本次流程后续判断的now
	now := time.Now()
	if // last、ok 用于本次流程后续判断的last、ok
	last, ok := d.processed[msgID]; ok {
		if now.Sub(last) < MessageExpireTime {
			d.logger.Debug("消息已处理过，跳过", "msg_id", truncID(msgID))
			return false
		}
	}
	d.processed[msgID] = now
	if len(d.processed) > ProcessedIDsMaxSize {
		d.cleanupDedupLocked(now)
	}
	return true
}

// cleanupDedupLocked 清理过期去重记录，并在仍超限时删除最旧的一半。
func (d *messageDispatcher) cleanupDedupLocked(now time.Time) {
	// id、timestamp 表示当前遍历过程中的id、timestamp
	for id, timestamp := range d.processed {
		if now.Sub(timestamp) > MessageExpireTime {
			delete(d.processed, id)
		}
	}
	if len(d.processed) <= ProcessedIDsMaxSize {
		return
	}
	// entry 是按时间排序的去重记录临时项。
	type entry struct {
		id string
		at time.Time
	}
	// entries 用于本次流程后续判断的entries
	entries := make([]entry, 0, len(d.processed))
	// id、timestamp 表示当前遍历过程中的id、timestamp
	for id, timestamp := range d.processed {
		entries = append(entries, entry{id: id, at: timestamp})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
	// remove 用于本次流程后续判断的remove
	remove := len(entries) / 2
	for // i 用于本次流程后续判断的i
	i := 0; i < remove; i++ {
		delete(d.processed, entries[i].id)
	}
}

// scheduleDebouncedReply 为一条已通过去重的买家消息登记生命周期任务并交给 receiveChat；
// 生产路径的 handleMessageContext 已经处于任务内，直接调用 receiveChat，本方法供测试和独立入口使用。
func (d *messageDispatcher) scheduleDebouncedReply(chat ChatMessage) {
	// ctx、finish、ok 分别是本条消息接收任务的上下文、释放函数及生命周期接纳结果。
	ctx, finish, ok := d.beginTask()
	if !ok {
		return
	}
	defer finish()
	d.receiveChat(ctx, chat)
}

// receiveChat 接收一条已通过去重的买家消息：立即交给业务处理器落库和通知（每条消息都处理，不再被防抖吞掉），
// 登记为等待合并的消息并顶替该会话尚未提交的回复生成，然后重置“合并等待”安静计时。
// 买家在账号配置的合并等待时长内没有再发消息，计时器到期后才把这一批消息合并回复；
// 从一批消息的第一条算起最迟 MessageDebounceMaxWait 必定到期，避免买家持续连发导致一直不回复。
// ctx 必须来自账号生命周期任务；chat 是已完成去重的买家入站消息。
func (d *messageDispatcher) receiveChat(ctx context.Context, chat ChatMessage) {
	// 接收链路的日志统一带账号与会话，落库失败与回复决策无需再逐条补参。
	ctx = logging.With(ctx, slog.String("account", d.cookieID), slog.String("chat_id", chat.ChatID), slog.String("item_id", chat.ItemID))
	// handler 是当前业务处理器，负责消息落库与新消息通知。
	if handler := d.currentHandler(); handler != nil {
		// err 是落库或通知失败原因；失败时不进入自动回复，与历史行为一致。
		if err := handler.HandleChatMessage(ctx, chat); err != nil {
			d.logger.ErrorContext(ctx, "处理聊天消息失败", "err", err)
			return
		}
	}
	if d.reply == nil {
		return
	}
	// 先登记等待消息再读取窗口配置：登记会立刻取消同会话尚未提交的生成，不必等数据库读取。
	d.reply.enqueueIncoming(chat)
	// window 是账号配置的合并等待时长。
	window := d.reply.mergeWindow(ctx)

	d.debounceMu.Lock()
	defer d.debounceMu.Unlock()
	// now、firstAt 是本条消息到达时刻与这一批消息第一条的到达时刻。
	now := time.Now()
	// firstAt 是这一批消息第一条的到达时刻，用于计算硬上限。
	firstAt := now
	// old、ok 是同会话已有的防抖条目；沿用它的批次起点并停止旧计时器。
	if old, ok := d.debounceTimers[chat.ChatID]; ok {
		if old.timer != nil {
			old.timer.Stop()
		}
		firstAt = old.firstAt
	}
	// delay 是距离计时器到期的时长：合并窗口，但不得超过从批次起点算起的硬上限。
	delay := window
	// remaining 是距离硬上限还剩的时长，小于合并窗口时缩短计时。
	if remaining := MessageDebounceMaxWait - now.Sub(firstAt); remaining < delay {
		delay = max(remaining, 0)
	}
	// entry 是本次计时的防抖条目；deadline 兼作代际标识，防止被停止的旧计时器误触发。
	entry := &debounceEntry{lastMsg: chat, deadline: now, firstAt: firstAt}
	d.debounceTimers[chat.ChatID] = entry
	entry.timer = time.AfterFunc(delay, func() { d.fireDebounced(chat.ChatID, entry.deadline) })
}

// fireDebounced 在合并等待到期后开始一个回复轮次；deadline 不匹配说明计时器已被更新的消息替换，直接忽略。
func (d *messageDispatcher) fireDebounced(chatID string, deadline time.Time) {
	d.debounceMu.Lock()
	// current、ok 是该会话当前的防抖条目及其是否存在。
	current, ok := d.debounceTimers[chatID]
	if !ok || current.deadline != deadline {
		d.debounceMu.Unlock()
		return
	}
	delete(d.debounceTimers, chatID)
	// lastMessage 是这一批消息中最新的一条，用于自动回复资格核验。
	lastMessage := current.lastMsg
	d.debounceMu.Unlock()
	// ctx、finish、accepted 分别是回复任务的上下文、释放函数及生命周期接纳结果。
	ctx, finish, accepted := d.beginTask()
	if !accepted {
		return
	}
	defer finish()
	// 回复链路的日志统一带账号与会话，回复决策与发送失败无需再逐条补参。
	ctx = logging.With(ctx, slog.String("account", d.cookieID), slog.String("chat_id", lastMessage.ChatID), slog.String("item_id", lastMessage.ItemID))
	if !d.canAutoReply(ctx, lastMessage) {
		// 不允许自动回复的会话（商品不属于本账号、缺少身份等）不会有回复轮次消费等待消息，必须主动丢弃以免滞留。
		d.reply.discardPending(chatID)
		return
	}
	// err 是回复链失败原因，只记录日志，不影响后续消息。
	if err := d.reply.HandleBatch(ctx, chatID); err != nil {
		d.logger.ErrorContext(ctx, "处理自动回复失败", "err", err)
	}
}

// cancelPendingReply 在人工已经回复该会话时取消尚未开始的合并等待并丢弃已等待的消息、顶替未提交的回复生成，
// 避免助手紧接着人工再发一条自动回复。不持有 debounceMu 调用回复服务。
func (d *messageDispatcher) cancelPendingReply(chatID string) {
	d.debounceMu.Lock()
	// entry、ok 是该会话的防抖条目及其是否存在。
	if entry, ok := d.debounceTimers[chatID]; ok {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		delete(d.debounceTimers, chatID)
	}
	d.debounceMu.Unlock()
	if d.reply != nil {
		d.reply.discardPending(chatID)
	}
}

// stop 取消所有防抖定时器，不持锁等待回调任务；回调任务由 Account lifecycle 等待。
func (d *messageDispatcher) stop() {
	d.debounceMu.Lock()
	defer d.debounceMu.Unlock()
	// entry 表示当前遍历过程中的entry
	for _, entry := range d.debounceTimers {
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	d.debounceTimers = make(map[string]*debounceEntry)
}
