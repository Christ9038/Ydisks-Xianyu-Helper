package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/agentclient"
)

// 以下常量是买家分段消息合并的硬性上限，防止异常会话让内存或模型请求无限增长。
const (
	// replyPendingMaxMessages 是单个会话同时等待合并的买家消息条数上限；超出时丢弃最早的消息。
	replyPendingMaxMessages = 10
	// replyMergedMaxRunes 是合并后文字的最大字符数；超出时从最早的行开始丢弃，保证最新的话一定保留。
	replyMergedMaxRunes = 2000
	// replyMergedMaxImages 是一次合并最多发给模型或 Agent 的买家图片数，取最近的图片。
	replyMergedMaxImages = 3
	// replyPendingTTL 是等待合并的消息在没有回复轮次消费时的最长保留时间，防止遗留条目长期占用内存。
	replyPendingTTL = 30 * time.Minute
	// replyMaxTrackedChats 是协调器同时跟踪的会话数上限；超出时淘汰最久未活动且没有进行中轮次的会话。
	replyMaxTrackedChats = 2000
)

// pendingMessage 是一条等待被回复轮次消费的买家消息。
type pendingMessage struct {
	// seq 是协调器分配的单调递增序号，用于精确界定“某个轮次消费到哪一条”。
	seq uint64
	// msg 是买家消息本身。
	msg ChatMessage
	// at 是进入等待队列的时间，用于 TTL 清理。
	at time.Time
}

// chatReplyState 是单个会话的合并状态，受 replyTurnCoordinator.mu 保护。
type chatReplyState struct {
	// pending 按到达顺序保存尚未被任何回复轮次确认消费的买家消息。
	pending []pendingMessage
	// turn 是该会话当前正在运行的回复轮次；没有则为 nil。同一会话同一时刻最多一个。
	turn *ReplyTurn
	// replaced 是最近一个被新消息顶替的轮次标识；下一个轮次会把它作为 replaces_turn_id 告知 Agent。
	replaced string
	// touched 是该会话最近一次活动时间，用于容量淘汰。
	touched time.Time
}

// replyTurnCoordinator 保证同一会话同一时刻只有一个回复轮次，并把买家的分段消息合并后一起回复。
//
// 所有权：由 ReplyService 持有，随账号运行时存在；不创建任何 goroutine，轮次在调用方（防抖定时器回调）goroutine 内同步运行。
// 锁：mu 保护 chats 映射、每个会话的状态以及每个 ReplyTurn 的 committed/superseded 等标志；
// 持锁期间只做内存操作和 context.CancelFunc 调用（非阻塞），等待旧轮次退出、数据库和网络 I/O 都在释放 mu 之后进行。
// 零值可直接使用。
type replyTurnCoordinator struct {
	// mu 保护本结构体全部字段以及其中所有 ReplyTurn 的可变标志。
	mu sync.Mutex
	// chats 以聊天会话标识为键保存合并状态；既没有等待消息也没有运行轮次的会话会被删除。
	chats map[string]*chatReplyState
	// seq 是下一个等待消息使用的序号，只在持有 mu 时递增。
	seq uint64
	// now 是可注入时钟；为 nil 时使用 time.Now，测试用来模拟 TTL 过期。
	now func() time.Time
}

// ReplyTurn 是一个会话的一次回复尝试，由 replyTurnCoordinator 创建，经 ChatMessage.Turn 交给回复链。
//
// 它提供三项能力：取得随“新消息到达”而取消的生成上下文、在发送第一条回复前提交（提交后不再被新消息打断，
// 除非调用方声明自己是可中断的多段轮询），以及读取本轮标识。所有方法对 nil 接收者安全，
// 此时表现为“没有协调器”的直接调用：上下文原样返回、提交总是成功。
type ReplyTurn struct {
	// coordinator 是创建本轮次的协调器；标志位由它的 mu 保护。
	coordinator *replyTurnCoordinator
	// chatID 是本轮次所属会话。
	chatID string
	// id 是本轮次的随机标识，作为 Agent 协议的 turn_id。
	id string
	// replacesID 是被本轮次顶替的上一轮次标识，没有则为空。创建后只读。
	replacesID string
	// parent 是账号生命周期上下文；已提交的非中断发送使用它，不受新消息取消影响。
	parent context.Context
	// ctx 是随“新消息到达”而取消的上下文，供模型生成和 Agent 轮询使用。
	ctx context.Context
	// cancel 取消 ctx；finish 时一定调用以释放资源。
	cancel context.CancelFunc
	// done 在轮次登记结束后关闭，等待方据此确认旧轮次已完全退出。
	done chan struct{}
	// maxSeq 是本轮次拍下的等待消息中最大的序号；提交或结束时消费序号不超过它的消息。创建后只读。
	maxSeq uint64
	// committed 表示本轮次已确认要发送回复，其等待消息已被消费。受 coordinator.mu 保护。
	committed bool
	// interruptible 表示提交后仍允许被新消息顶替（Agent v1 的多段轮询）。受 coordinator.mu 保护。
	interruptible bool
	// superseded 表示本轮次已被新消息或人工介入顶替，应尽快放弃。受 coordinator.mu 保护。
	superseded bool
	// sendCtx 是提交后发送回复使用的上下文：可中断时为 ctx，否则为 parent。受 coordinator.mu 保护。
	sendCtx context.Context
	// finishOnce 保证 finish 幂等。
	finishOnce sync.Once
}

// ID 返回本轮次标识；nil 接收者返回空串，调用方应改用自己生成的标识。
func (t *ReplyTurn) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

// ReplacesID 返回被本轮次顶替的上一轮次标识；没有顶替或 nil 接收者返回空串。
func (t *ReplyTurn) ReplacesID() string {
	if t == nil {
		return ""
	}
	return t.replacesID
}

// Context 返回随新消息到达而取消的生成上下文；nil 接收者原样返回 fallback。
func (t *ReplyTurn) Context(fallback context.Context) context.Context {
	if t == nil {
		return fallback
	}
	return t.ctx
}

// Superseded 报告本轮次是否已被新消息或人工介入顶替；nil 接收者恒为 false。
func (t *ReplyTurn) Superseded() bool {
	if t == nil {
		return false
	}
	t.coordinator.mu.Lock()
	defer t.coordinator.mu.Unlock()
	return t.superseded
}

// Commit 在发送第一条回复之前调用：确认本轮次要发送，消费本轮次拍下的等待消息，并返回发送使用的上下文。
//
// 已被顶替时返回 (nil, false)，调用方必须放弃发送，等待消息会留给顶替它的下一轮合并重发。
// interruptible 为 false 时返回的上下文脱离“新消息取消”，发送中途不会因买家又发话而变成结果不确定；
// 为 true（Agent v1 多段轮询）时仍返回可取消的上下文，两次发送之间被新消息顶替就停止。
// 重复调用返回首次提交的结果。nil 接收者返回 (fallback, true)。
func (t *ReplyTurn) Commit(fallback context.Context, interruptible bool) (context.Context, bool) {
	if t == nil {
		return fallback, true
	}
	return t.coordinator.commit(t, interruptible)
}

// clock 返回协调器当前时间；未注入时钟时使用系统时间。
func (c *replyTurnCoordinator) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// stateLocked 返回会话状态，不存在时创建；调用方必须持有 mu。
func (c *replyTurnCoordinator) stateLocked(chatID string) *chatReplyState {
	if c.chats == nil {
		c.chats = make(map[string]*chatReplyState)
	}
	// state、ok 保存已有的会话状态及是否存在。
	state, ok := c.chats[chatID]
	if !ok {
		state = &chatReplyState{}
		c.chats[chatID] = state
	}
	return state
}

// pruneLocked 清理过期的等待消息并在会话数超限时淘汰最久未活动的空闲会话；调用方必须持有 mu。
func (c *replyTurnCoordinator) pruneLocked(now time.Time) {
	// chatID、state 表示当前遍历的会话及其状态。
	for chatID, state := range c.chats {
		// kept 保存仍在保留期内的等待消息。
		kept := state.pending[:0]
		// item 表示当前遍历的等待消息。
		for _, item := range state.pending {
			if now.Sub(item.at) <= replyPendingTTL {
				kept = append(kept, item)
			}
		}
		state.pending = kept
		if len(state.pending) == 0 && state.turn == nil {
			delete(c.chats, chatID)
		}
	}
	for len(c.chats) > replyMaxTrackedChats {
		// oldestID、oldestAt 是最久未活动的空闲会话及其活动时间；found 表示是否找到可淘汰项。
		var oldestID string
		// oldestAt 是当前最旧空闲会话的最后活动时间。
		var oldestAt time.Time
		// found 表示是否已找到可淘汰的空闲会话。
		found := false
		// chatID、state 是遍历到的会话 ID 与其协调状态。
		for chatID, state := range c.chats {
			if state.turn != nil {
				continue
			}
			if !found || state.touched.Before(oldestAt) {
				oldestID, oldestAt, found = chatID, state.touched, true
			}
		}
		if !found {
			return
		}
		delete(c.chats, oldestID)
	}
}

// enqueue 登记一条新到达的买家消息，并顶替该会话尚未提交（或可中断）的回复轮次。
// 它不触发回复：回复由防抖定时器到期后调用 begin 开始。
func (c *replyTurnCoordinator) enqueue(m ChatMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// now 是本消息进入队列的时间。
	now := c.clock()
	c.pruneLocked(now)
	// state 是该会话的合并状态。
	state := c.stateLocked(m.ChatID)
	c.seq++
	state.pending = append(state.pending, pendingMessage{seq: c.seq, msg: m, at: now})
	// overflow 是超出等待消息条数上限的条数，超出的最旧消息被丢弃。
	if overflow := len(state.pending) - replyPendingMaxMessages; overflow > 0 {
		state.pending = append(state.pending[:0], state.pending[overflow:]...)
	}
	state.touched = now
	// turn 是该会话正在运行的轮次；未提交或可中断的轮次被顶替并立刻取消，不再浪费模型调用。
	if turn := state.turn; turn != nil && !turn.superseded && (!turn.committed || turn.interruptible) {
		turn.superseded = true
		turn.cancel()
	}
}

// begin 为 chatID 开始一个回复轮次：若旧轮次仍在运行则等待其退出（不持锁），随后拍下全部等待消息。
// 没有等待消息时返回 (nil, nil, nil)；等待期间 parent 被取消（账号停止）时返回其错误。
// 返回的轮次必须由调用方 defer finish。
func (c *replyTurnCoordinator) begin(parent context.Context, chatID string) (*ReplyTurn, []ChatMessage, error) {
	for {
		c.mu.Lock()
		// state 是该会话的合并状态；不存在或没有等待消息说明无需回复。
		state := c.chats[chatID]
		if state == nil || len(state.pending) == 0 {
			c.mu.Unlock()
			return nil, nil, nil
		}
		if state.turn == nil {
			break
		}
		// done 是旧轮次的退出信号，在释放锁之后等待。
		done := state.turn.done
		c.mu.Unlock()
		select {
		case <-done:
		case <-parent.Done():
			return nil, nil, parent.Err()
		}
	}
	defer c.mu.Unlock()
	// state 是已确认存在且空闲的会话状态；messages 是本轮次拍下的全部等待消息。
	state := c.chats[chatID]
	// messages 是本轮消费的等待消息快照，按到达顺序排列。
	messages := make([]ChatMessage, 0, len(state.pending))
	// item 表示当前遍历的等待消息。
	for _, item := range state.pending {
		messages = append(messages, item.msg)
	}
	// turnCtx、cancel 是本轮次随新消息取消的上下文及其取消函数。
	turnCtx, cancel := context.WithCancel(parent)
	// turn 是新建的轮次登记项。
	turn := &ReplyTurn{
		coordinator: c, chatID: chatID, id: agentclient.NewTurnID(), replacesID: state.replaced,
		parent: parent, ctx: turnCtx, cancel: cancel, done: make(chan struct{}),
		maxSeq: state.pending[len(state.pending)-1].seq,
	}
	state.replaced = ""
	state.turn = turn
	state.touched = c.clock()
	return turn, messages, nil
}

// consumeLocked 删除会话中序号不超过 maxSeq 的等待消息；调用方必须持有 mu。
func (c *replyTurnCoordinator) consumeLocked(state *chatReplyState, maxSeq uint64) {
	// kept 保存序号更大（晚于本轮次拍下）的消息，它们留给下一轮。
	kept := state.pending[:0]
	// item 表示当前遍历的等待消息。
	for _, item := range state.pending {
		if item.seq > maxSeq {
			kept = append(kept, item)
		}
	}
	state.pending = kept
}

// commit 实现 ReplyTurn.Commit。
func (c *replyTurnCoordinator) commit(t *ReplyTurn, interruptible bool) (context.Context, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.committed {
		return t.sendCtx, true
	}
	if t.superseded {
		return nil, false
	}
	t.committed, t.interruptible = true, interruptible
	if interruptible {
		t.sendCtx = t.ctx
	} else {
		t.sendCtx = t.parent
	}
	// state 是该会话的协调状态，已被淘汰时为 nil。
	if state := c.chats[t.chatID]; state != nil {
		c.consumeLocked(state, t.maxSeq)
	}
	return t.sendCtx, true
}

// finish 登记轮次结束；幂等。被顶替的轮次不消费等待消息（留给下一轮合并），并记下自己的标识供下一轮告知 Agent；
// 正常结束但从未提交（没有任何规则回复）的轮次消费自己拍下的消息，避免旧消息被无限重发。
func (t *ReplyTurn) finish() {
	if t == nil {
		return
	}
	t.finishOnce.Do(func() {
		// c 是本轮所属的协调器。
		c := t.coordinator
		c.mu.Lock()
		// state 是该会话的合并状态；可能已因 discard 被删除。
		state := c.chats[t.chatID]
		if state != nil {
			if state.turn == t {
				state.turn = nil
			}
			if t.superseded && len(state.pending) > 0 {
				state.replaced = t.id
			}
			if !t.committed && !t.superseded {
				c.consumeLocked(state, t.maxSeq)
			}
			if len(state.pending) == 0 && state.turn == nil {
				delete(c.chats, t.chatID)
			}
		}
		c.mu.Unlock()
		t.cancel()
		close(t.done)
	})
}

// discard 丢弃会话全部等待消息并顶替尚未提交（或可中断）的轮次，用于人工已经回复或消息不应自动回复的场景。
// 已提交且不可中断的发送不受影响。
func (c *replyTurnCoordinator) discard(chatID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// state 是该会话的合并状态；不存在说明没有需要丢弃的内容。
	state := c.chats[chatID]
	if state == nil {
		return
	}
	state.pending = nil
	state.replaced = ""
	// turn 是该会话当前正在运行的轮次，可被新消息顶替时才取消。
	if turn := state.turn; turn != nil && !turn.superseded && (!turn.committed || turn.interruptible) {
		turn.superseded = true
		turn.cancel()
	}
	if state.turn == nil {
		delete(c.chats, chatID)
	}
}

// mergeChatMessages 把一批等待消息合并为交给回复链的单条消息：Text 是按行合并的文字（图片占位符不计入，
// 全是图片时保留占位符），Images 是最近的买家图片地址，Parts 保存原始消息，其余字段取自最新一条。
// 合并文字超过 replyMergedMaxRunes 时从最早的行开始丢弃；最新一行即使单独超长也保留。
func mergeChatMessages(parts []ChatMessage, turn *ReplyTurn) ChatMessage {
	// latest 是批次中最新的一条消息，作为合并结果的身份与元数据来源。
	latest := parts[len(parts)-1]
	// lines 保存非图片消息的文字行；images 保存按时间顺序提取到的图片地址；hasImage 表示批次中是否有图片消息。
	lines := make([]string, 0, len(parts))
	// images 是合并后保留的买家图片地址，最多取最近几张。
	var images []string
	// hasImage 表示批次中是否出现过图片消息。
	hasImage := false
	// part 表示当前遍历的原始买家消息。
	for _, part := range parts {
		// imageURL 是该消息携带的公网图片地址；占位符消息即使取不到地址也视为图片。
		imageURL := buyerImageURL(part)
		if imageURL != "" || strings.TrimSpace(part.Text) == agentImagePlaceholder {
			hasImage = true
			if imageURL != "" {
				images = append(images, imageURL)
			}
			continue
		}
		// text 是去除首尾空白后的消息文字，空文字不参与合并。
		if text := strings.TrimSpace(part.Text); text != "" {
			lines = append(lines, text)
		}
	}
	if len(images) > replyMergedMaxImages {
		images = images[len(images)-replyMergedMaxImages:]
	}
	// total 是当前保留文字的字符数；start 是保留区间的起始行下标。
	total, start := 0, len(lines)
	// index 是从最新一条向前遍历的位置。
	for index := len(lines) - 1; index >= 0; index-- {
		// size 是该行字符数，加上换行符。
		size := len([]rune(lines[index])) + 1
		if total+size > replyMergedMaxRunes && start != len(lines) {
			break
		}
		total += size
		start = index
	}
	// text 是合并后的文字；没有文字但有图片时保留占位符，保持与单条图片消息一致的语义。
	text := strings.Join(lines[start:], "\n")
	if text == "" && hasImage {
		text = agentImagePlaceholder
	}
	// merged 复制最新消息的全部元数据后覆盖文字、图片和批次信息。
	merged := latest
	merged.Text = text
	merged.Parts = parts
	merged.Images = images
	merged.Turn = turn
	return merged
}
