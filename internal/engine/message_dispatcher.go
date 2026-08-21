package engine

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"xianyu-go/internal/automation"
)

// messageDispatcher 负责 WebSocket 消息的事实解析、去重、防抖和并发投递。
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
	// reply 负责自动回复链，可能为空。
	reply *ReplyService
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
	// Reply 是自动回复服务。
	Reply *ReplyService
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
	return messageDispatcher{
		processed:      make(map[string]time.Time),
		debounceTimers: make(map[string]*debounceEntry),
		sem:            make(chan struct{}, MessageSemaphoreSize),
		cookieID:       config.CookieID,
		currentCookie:  currentCookie,
		currentHandler: currentHandler,
		reply:          config.Reply,
		logger:         logger,
		beginTask:      beginTask,
		recordMessage:  recordMessage,
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
	// receipt、ok 保存解析出的平台已读回执及是否命中已读事件格式。
	if receipt, ok := extractMessageReadEvent(decrypted); ok {
		receipt.AccountID = d.cookieID
		// handler 保存当前可选处理器；旧集成不实现回执端口时保持兼容。
		if handler := d.currentHandler(); handler != nil {
			// reader 是可消费已读回执的可选端口；supported 表示当前集成实现了该端口。
			if reader, supported := handler.(MessageReadHandler); supported {
				// err 保存回执持久化错误；失败不应中断 WebSocket 接收循环。
				if err := reader.HandleMessageRead(ctx, receipt); err != nil {
					d.logger.Warn("处理聊天已读回执失败", "err", err, "message_id", receipt.MessageID)
				}
			}
		}
		return
	}
	// 系统卡片和平台通知优先进入自动化中心，永远不进入 AI 回复范围。
	if task := automation.ExtractTaskFromWS(d.cookieID, d.currentCookie(), decrypted); task != nil {
		if // handler 用于本次流程后续判断的handler
		handler := d.currentHandler(); handler != nil {
			if // err 用于本次流程后续判断的err
			err := handler.HandleSystemEvent(ctx, *task); err != nil {
				d.logger.Error("处理系统自动化事件失败", "err", err, "trigger", task.TriggerType)
			}
		}
		return
	}
	// ownEcho 保存当前账号从官方客户端发出后回显的消息；它必须实时落库，但绝不能进入自动回复防抖链。
	if ownEcho := extractOwnWebSocketEcho(decrypted, d.cookieID, d.currentCookie()); ownEcho != nil {
		// handler 保存当前可选业务处理器；旧集成未实现出站观察能力时保持原有仅过滤语义。
		if handler := d.currentHandler(); handler != nil {
			// observer、supported 保存出站观察接口及其实现判断，避免扩大基础 Handler 的必选职责。
			if observer, supported := handler.(outgoingChatHandler); supported {
				// err 保存自身回显持久化或实时发布失败；失败不应影响 WebSocket 接收循环或误触发自动回复。
				if err := observer.HandleOutgoingChatMessage(ctx, *ownEcho); err != nil {
					d.logger.Warn("处理官方客户端出站消息回显失败", "err", err, "chat_id", ownEcho.ChatID)
				}
			}
		}
		return
	}

	// chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, d.cookieID, d.currentCookie())
	if chat != nil && chat.Text != "" {
		if !d.markAndCheckDedup(decrypted, chat) {
			return
		}
		d.scheduleDebouncedReply(*chat)
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

// scheduleDebouncedReply 为同一聊天会话保留最后一条消息，并在延迟后投递。
func (d *messageDispatcher) scheduleDebouncedReply(chat ChatMessage) {
	d.debounceMu.Lock()
	defer d.debounceMu.Unlock()
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now()
	if // old、ok 用于本次流程后续判断的old、ok
	old, ok := d.debounceTimers[chat.ChatID]; ok && old.timer != nil {
		old.timer.Stop()
	}
	// entry 用于本次流程后续判断的entry
	entry := &debounceEntry{lastMsg: chat, deadline: deadline}
	d.debounceTimers[chat.ChatID] = entry
	entry.timer = time.AfterFunc(MessageDebounceDelay, func() {
		d.debounceMu.Lock()
		// current、ok 用于本次流程后续判断的current、ok
		current, ok := d.debounceTimers[chat.ChatID]
		if !ok || current.deadline != deadline {
			d.debounceMu.Unlock()
			return
		}
		delete(d.debounceTimers, chat.ChatID)
		// lastMessage 用于本次流程后续判断的last消息
		lastMessage := current.lastMsg
		d.debounceMu.Unlock()
		// ctx、finish、ok 分别是防抖回复任务的上下文、释放函数及生命周期接纳结果。
		ctx, finish, ok := d.beginTask()
		if !ok {
			return
		}
		defer finish()
		if d.reply != nil {
			if // err 用于本次流程后续判断的err
			err := d.reply.Handle(ctx, lastMessage); err != nil {
				d.logger.Error("处理自动回复失败", "err", err, "chat_id", chat.ChatID)
			}
		}
		if // handler 用于本次流程后续判断的handler
		handler := d.currentHandler(); handler != nil {
			if // err 用于本次流程后续判断的err
			err := handler.HandleChatMessage(ctx, lastMessage); err != nil {
				d.logger.Error("处理聊天消息失败", "err", err, "chat_id", chat.ChatID)
			}
		}
	})
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
