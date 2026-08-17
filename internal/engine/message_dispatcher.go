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
// messageDispatcher 保存消息Dispatcher，供当前处理流程使用
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
	// beginTask 登记账号生命周期任务。
	beginTask func() (context.Context, bool)
	// finishTask 完成账号生命周期任务登记。
	finishTask func()
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
	// BeginTask 登记账号生命周期任务。
	BeginTask func() (context.Context, bool)
	// FinishTask 完成账号生命周期任务登记。
	FinishTask func()
	// RecordMessage 更新最近收到消息时间。
	RecordMessage func(time.Time)
}

// newMessageDispatcher 构造消息分发组件并初始化其有界状态。
func newMessageDispatcher(config messageDispatcherConfig) messageDispatcher {
	// logger 保存logger，供当前处理流程使用
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// currentCookie 保存current登录凭证，供当前处理流程使用
	currentCookie := config.CurrentCookie
	if currentCookie == nil {
		currentCookie = func() string { return "" }
	}
	// beginTask 保存begin任务，供当前处理流程使用
	beginTask := config.BeginTask
	if beginTask == nil {
		beginTask = func() (context.Context, bool) { return context.Background(), true }
	}
	// finishTask 保存finish任务，供当前处理流程使用
	finishTask := config.FinishTask
	if finishTask == nil {
		finishTask = func() {}
	}
	// recordMessage 保存record消息，供当前处理流程使用
	recordMessage := config.RecordMessage
	if recordMessage == nil {
		recordMessage = func(time.Time) {}
	}
	// currentHandler 保存currentHandler，供当前处理流程使用
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
		finishTask:     finishTask,
		recordMessage:  recordMessage,
	}
}

// dispatch 接收一条解密消息并安排系统事件或聊天消息处理。
func (d *messageDispatcher) dispatch(decrypted map[string]any) {
	// ctx、ok 保存ctx、ok，供当前处理流程使用
	ctx, ok := d.beginTask()
	if !ok {
		return
	}
	d.recordMessage(time.Now())
	// 系统业务事件不能丢弃：并发满时让 WS 读取产生背压，等待处理槽位。
	// 普通聊天仍采用非阻塞限流，避免聊天洪峰拖垮连接。
	// isSystemEvent 保存is系统Event，供当前处理流程使用
	isSystemEvent := automation.ExtractTaskFromWS(d.cookieID, d.currentCookie(), decrypted) != nil
	if isSystemEvent {
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			d.finishTask()
			return
		}
		go func() {
			defer d.finishTask()
			defer func() { <-d.sem }()
			d.handleMessageContext(ctx, decrypted)
		}()
		return
	}

	select {
	case d.sem <- struct{}{}:
	default:
		d.finishTask()
		d.logger.Warn("消息处理并发达上限，丢弃消息", "limit", MessageSemaphoreSize)
		return
	}
	go func() {
		defer d.finishTask()
		defer func() { <-d.sem }()
		d.handleMessageContext(ctx, decrypted)
	}()
}

// handleMessage 分类并投递消息，供 Account facade 和测试调用。
func (d *messageDispatcher) handleMessage(decrypted map[string]any) {
	d.handleMessageContext(context.Background(), decrypted)
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
		if // handler 保存handler，供当前处理流程使用
		handler := d.currentHandler(); handler != nil {
			if // err 保存err，供当前处理流程使用
			err := handler.HandleSystemEvent(ctx, *task); err != nil {
				d.logger.Error("处理系统自动化事件失败", "err", err, "trigger", task.TriggerType)
			}
		}
		return
	}

	// chat 保存聊天，供当前处理流程使用
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
	// msgID 保存msgID，供当前处理流程使用
	msgID := extractMessageID(decrypted)
	if msgID == "" {
		// 备用标识：chat_id + text + create_time。
		createTime := "0"
		if // m1、ok 保存m1、ok，供当前处理流程使用
		m1, ok := decrypted["1"].(map[string]any); ok {
			if // t、ok 保存t、ok，供当前处理流程使用
			t, ok := m1["5"]; ok {
				createTime = toString(t)
			}
		}
		msgID = chat.ChatID + "_" + chat.Text + "_" + createTime
	}

	d.dedupMu.Lock()
	defer d.dedupMu.Unlock()
	// now 保存now，供当前处理流程使用
	now := time.Now()
	if // last、ok 保存last、ok，供当前处理流程使用
	last, ok := d.processed[msgID]; ok {
		if now.Sub(last) < MessageExpireTime {
			d.logger.Info("消息已处理过，跳过", "msg_id", truncID(msgID))
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
	// entries 保存entries，供当前处理流程使用
	entries := make([]entry, 0, len(d.processed))
	// id、timestamp 表示当前遍历过程中的id、timestamp
	for id, timestamp := range d.processed {
		entries = append(entries, entry{id: id, at: timestamp})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
	// remove 保存remove，供当前处理流程使用
	remove := len(entries) / 2
	for // i 保存i，供当前处理流程使用
	i := 0; i < remove; i++ {
		delete(d.processed, entries[i].id)
	}
}

// scheduleDebouncedReply 为同一聊天会话保留最后一条消息，并在延迟后投递。
func (d *messageDispatcher) scheduleDebouncedReply(chat ChatMessage) {
	d.debounceMu.Lock()
	defer d.debounceMu.Unlock()
	// deadline 保存deadline，供当前处理流程使用
	deadline := time.Now()
	if // old、ok 保存old、ok，供当前处理流程使用
	old, ok := d.debounceTimers[chat.ChatID]; ok && old.timer != nil {
		old.timer.Stop()
	}
	// entry 保存entry，供当前处理流程使用
	entry := &debounceEntry{lastMsg: chat, deadline: deadline}
	d.debounceTimers[chat.ChatID] = entry
	entry.timer = time.AfterFunc(MessageDebounceDelay, func() {
		d.debounceMu.Lock()
		// current、ok 保存current、ok，供当前处理流程使用
		current, ok := d.debounceTimers[chat.ChatID]
		if !ok || current.deadline != deadline {
			d.debounceMu.Unlock()
			return
		}
		delete(d.debounceTimers, chat.ChatID)
		// lastMessage 保存last消息，供当前处理流程使用
		lastMessage := current.lastMsg
		d.debounceMu.Unlock()
		// ctx、ok 保存ctx、ok，供当前处理流程使用
		ctx, ok := d.beginTask()
		if !ok {
			return
		}
		defer d.finishTask()
		if d.reply != nil {
			if // err 保存err，供当前处理流程使用
			err := d.reply.Handle(ctx, lastMessage); err != nil {
				d.logger.Error("处理自动回复失败", "err", err, "chat_id", chat.ChatID)
			}
		}
		if // handler 保存handler，供当前处理流程使用
		handler := d.currentHandler(); handler != nil {
			if // err 保存err，供当前处理流程使用
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
