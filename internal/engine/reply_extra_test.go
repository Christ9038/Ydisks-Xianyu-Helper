package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/ws"
)

// fakeAPIReplier 可控的 API 回复 mock：返回预设结果或错误。
type fakeAPIReplier struct {
	result *ReplyResult
	err    error
	called int
}

// Reply 封装回复业务协调。
func (f *fakeAPIReplier) Reply(_ context.Context, _ ChatMessage) (*ReplyResult, error) {
	f.called++
	return f.result, f.err
}

// fakeAIReplier 可控的 AI 回复 mock。
type fakeAIReplier struct {
	result *ReplyResult
	err    error
	called int
}

// Reply 封装回复业务协调。
func (f *fakeAIReplier) Reply(_ context.Context, _ ChatMessage) (*ReplyResult, error) {
	f.called++
	return f.result, f.err
}

// recordingSender 记录发送的文本/图片，用于断言回复投递。
type recordingSender struct {
	texts    []textSent
	images   []imageSent
	textErr  error
	imageErr error
	// textCalls 统计文本发送尝试次数，包含返回错误的传输调用。
	textCalls int
	// beforeTextError 在文本发送返回错误前执行，用于模拟发送过程中取消请求上下文。
	beforeTextError func()
}

// SendReply 复现聊天应用的图片先发、文字后发顺序，供回复状态测试使用。
func (r *recordingSender) SendReply(ctx context.Context, message ReplyMessage) (ReplySendResult, error) {
	// result 保存已成功完成的平台分段。
	result := ReplySendResult{}
	if message.ImageURL != "" {
		// imageErr 保存图片分段发送结果。
		if imageErr := r.SendImage(ctx, message.ChatID, message.ToUserID, message.ImageURL, 0, 0, 0); imageErr != nil {
			result.Uncertain = replySendUncertain(imageErr)
			return result, imageErr
		}
		result.ImageSent = true
	}
	if message.Text != "" {
		// textErr 保存文字分段发送结果。
		if textErr := r.SendText(ctx, message.ChatID, message.ToUserID, message.Text); textErr != nil {
			result.Uncertain = replySendUncertain(textErr)
			return result, textErr
		}
		result.TextSent = true
	}
	return result, nil
}

// replySendUncertain 复现聊天应用对测试发送错误的确定性分类；普通本地错误和明确未发送错误都允许重试。
func replySendUncertain(err error) bool {
	if err == nil || errors.Is(err, automation.ErrMessageNotSent) {
		return false
	}
	// sendErr 保存可由协议层明确标记为不确定的发送错误。
	var sendErr *ws.SendError
	return errors.As(err, &sendErr) && ws.SendResultKind(err) == ws.SendUncertain
}

// recordingReplyDelivery 记录引擎交给聊天应用的完整回复，不模拟任何协议级图片尺寸逻辑。
type recordingReplyDelivery struct {
	// messages 保存完整回复消息及其字段。
	messages []ReplyMessage
	// result 保存聊天应用返回的分段确认结果。
	result ReplySendResult
	// err 保存聊天应用返回的发送错误。
	err error
}

// SendReply 记录完整回复并返回预设的应用层发送结果。
func (d *recordingReplyDelivery) SendReply(_ context.Context, message ReplyMessage) (ReplySendResult, error) {
	d.messages = append(d.messages, message)
	return d.result, d.err
}

// textSent 用于本次流程后续判断的文本Sent
type textSent struct {
	chatID, toUserID, text string
}

// imageSent 用于本次流程后续判断的图片Sent
type imageSent struct {
	chatID, toUserID, url string
	cardID                int64
	// width 和 height 保存交给 WebSocket 的图片像素尺寸。
	width, height int
}

// SendText 封装Send文本业务协调。
func (r *recordingSender) SendText(_ context.Context, chatID, toUserID, text string) error {
	r.textCalls++
	if r.beforeTextError != nil {
		r.beforeTextError()
	}
	if r.textErr != nil {
		return r.textErr
	}
	r.texts = append(r.texts, textSent{chatID, toUserID, text})
	return nil
}

// TestReplyOnceMarksDefiniteFailureWithIndependentContext 验证取消发送上下文时确定未发送状态仍可被领取重试。
func TestReplyOnceMarksDefiniteFailureWithIndependentContext(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// setupErr 保存启用一次性默认回复的配置写入错误。
	if setupErr := store.DefaultReps.Upsert(context.Background(), "cid", db.DefaultReply{Enabled: true, ReplyOnce: true, ReplyContent: "欢迎"}); setupErr != nil {
		t.Fatal(setupErr)
	}
	// requestCtx、cancel 保存会在发送失败期间被取消的原始请求上下文。
	requestCtx, cancel := context.WithCancel(context.Background())
	// sender 模拟确定未发送错误，并在返回前取消原始请求上下文。
	sender := &recordingSender{textErr: automation.ErrMessageNotSent, beforeTextError: cancel}
	// service 使用真实状态仓储验证失败状态可恢复。
	service := NewReplyService("cid", store, sender, nil, nil, nil)
	// sendErr 保存确定未发送错误的回复结果。
	if sendErr := service.Handle(requestCtx, chatMsg("你好", "", "chat-definite-failure")); !errors.Is(sendErr, automation.ErrMessageNotSent) {
		t.Fatalf("确定未发送错误未透传: %v", sendErr)
	}
	// record、recordErr 保存第一次失败后的状态。
	record, recordErr := store.DefaultReps.Record(context.Background(), "cid", "chat-definite-failure")
	if recordErr != nil || record.Status != "failed" {
		t.Fatalf("取消上下文不应遗留 sending 状态 record=%+v err=%v", record, recordErr)
	}
	// sender 恢复成功发送，验证 failed 记录可被下一次请求重新领取。
	sender.textErr = nil
	// retryErr 保存重新领取失败状态后的回复结果。
	if retryErr := service.Handle(context.Background(), chatMsg("还在吗", "", "chat-definite-failure")); retryErr != nil || sender.textCalls != 2 {
		t.Fatalf("失败状态无法重新领取 retryErr=%v calls=%d", retryErr, sender.textCalls)
	}
}

// SendImage 记录聊天图片地址、关联卡密和像素尺寸，供回复投递测试断言。
// ctx 是发送上下文；chatID/toUserID/url/cardID 标识消息身份；width/height 是发送协议中的图片像素尺寸。
func (r *recordingSender) SendImage(_ context.Context, chatID, toUserID, url string, cardID int64, width, height int) error {
	if r.imageErr != nil {
		return r.imageErr
	}
	r.images = append(r.images, imageSent{chatID: chatID, toUserID: toUserID, url: url, cardID: cardID, width: width, height: height})
	return nil
}

// TestAIQuoteSavedOnlyAfterTextDelivery 验证 AI 报价只有在回复发送成功后才成为可执行报价。
func TestAIQuoteSavedOnlyAfterTextDelivery(t *testing.T) {
	// store、cleanup 是回复链测试仓储及清理函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是回复发送与报价领取共用的测试上下文。
	ctx := context.Background()
	// result 是模拟 AI 已给出 9.90 元有效报价的回复结果。
	result := &ReplyResult{Text: "可以，9.90 元成交", AutoPriceQuote: &AIPriceQuoteProposal{PriceCents: 990}}
	// failedSender 模拟文本没有成功交给买家。
	failedSender := &recordingSender{textErr: errors.New("send failed")}
	// failedService 是注入发送失败替身的 AI 回复链。
	failedService := NewReplyService("cid", store, failedSender, nil, &fakeAIReplier{result: result}, nil)
	// err 是模拟发送失败时必须向调用方返回的错误。
	if err := failedService.Handle(ctx, chatMsg("能便宜吗", "item-1", "chat-1")); err == nil {
		t.Fatal("发送失败应返回错误")
	}
	// failedQuote 是发送失败后尝试领取的报价，必须为空。
	failedQuote, err := store.AIReply.ClaimPendingQuote(ctx, "cid", "chat-1", "buyer1", "item-1", "order-failed", time.Now().Unix())
	if err != nil || failedQuote != nil {
		t.Fatalf("发送失败不应保存报价: quote=%+v err=%v", failedQuote, err)
	}
	// successService 是文本发送成功的 AI 回复链。
	successService := NewReplyService("cid", store, &recordingSender{}, nil, &fakeAIReplier{result: result}, nil)
	if err = successService.Handle(ctx, chatMsg("能便宜吗", "item-1", "chat-1")); err != nil {
		t.Fatal(err)
	}
	// successQuote 是发送成功后与订单事实匹配的可执行报价。
	successQuote, err := store.AIReply.ClaimPendingQuote(ctx, "cid", "chat-1", "buyer1", "item-1", "order-success", time.Now().Unix())
	if err != nil || successQuote == nil || successQuote.PriceCents != 990 {
		t.Fatalf("发送成功应保存报价: quote=%+v err=%v", successQuote, err)
	}
}

// TestReplyOnceRetriesOnlyFailedParts 封装Test回复OnceRetriesOnly失败Parts业务协调。
func TestReplyOnceRetriesOnlyFailedParts(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO default_replies
		(cookie_id,enabled,reply_content,reply_image_url,reply_once)
		VALUES ('cid',1,'文字','http://img/retry.png',1)`)

	// textFailure 用于本次流程后续判断的文本Failure
	textFailure := errors.New("text failed")
	// firstSender 用于本次流程后续判断的firstSender
	firstSender := &recordingSender{textErr: textFailure}
	// service 用于本次流程后续判断的service
	service := NewReplyService("cid", s, firstSender, nil, nil, nil)
	if // err 用于本次流程后续判断的err
	err := service.Handle(ctx, chatMsg("在吗", "", "chat-retry")); !errors.Is(err, textFailure) {
		t.Fatalf("first error=%v want text failure", err)
	}
	if len(firstSender.images) != 1 || len(firstSender.texts) != 0 {
		t.Fatalf("first delivery images=%+v texts=%+v", firstSender.images, firstSender.texts)
	}
	// record、err 用于本次流程后续判断的record、err
	record, err := s.DefaultReps.Record(ctx, "cid", "chat-retry")
	if err != nil || record.Status != "failed" || !record.ImageSent || record.TextSent {
		t.Fatalf("failed record=%+v err=%v", record, err)
	}

	// secondSender 用于本次流程后续判断的secondSender
	secondSender := &recordingSender{}
	service = NewReplyService("cid", s, secondSender, nil, nil, nil)
	if // err 用于本次流程后续判断的err
	err := service.Handle(ctx, chatMsg("再问", "", "chat-retry")); err != nil {
		t.Fatal(err)
	}
	if len(secondSender.images) != 0 || len(secondSender.texts) != 1 {
		t.Fatalf("retry should send text only: images=%+v texts=%+v", secondSender.images, secondSender.texts)
	}
	record, err = s.DefaultReps.Record(ctx, "cid", "chat-retry")
	if err != nil || record.Status != "sent" || !record.ImageSent || !record.TextSent {
		t.Fatalf("sent record=%+v err=%v", record, err)
	}
}

// TestReplyOnceQuarantinesUncertainSend 验证平台可能已送达时不允许一次性默认回复自动重发。
func TestReplyOnceQuarantinesUncertainSend(t *testing.T) {
	// store、cleanup 保存隔离数据库及其关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是默认回复配置和发送状态读写使用的无截止上下文。
	ctx := context.Background()
	// setupErr 保存启用一次性默认回复的配置写入错误。
	setupErr := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyOnce: true, ReplyContent: "欢迎"})
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	// sender 模拟平台连接在发送后断开，无法判断消息是否已经送达。
	sender := &recordingSender{textErr: &ws.SendError{Kind: ws.SendUncertain}}
	// service 使用真实投递记录存储验证不确定结果隔离。
	service := NewReplyService("cid", store, sender, nil, nil, nil)
	// firstErr 保存首次发送返回的不确定错误。
	firstErr := service.Handle(ctx, chatMsg("你好", "", "chat-uncertain"))
	if firstErr == nil {
		t.Fatal("不确定发送结果必须返回调用错误")
	}
	// secondErr 保存同一会话再次触发时的结果；它不应发出第二条消息。
	secondErr := service.Handle(ctx, chatMsg("还在吗", "", "chat-uncertain"))
	if secondErr != nil || sender.textCalls != 1 {
		t.Fatalf("不确定结果后不应重发 secondErr=%v calls=%d", secondErr, sender.textCalls)
	}
	// record、recordErr 保存最终隔离状态及查询错误。
	record, recordErr := store.DefaultReps.Record(ctx, "cid", "chat-uncertain")
	if recordErr != nil || record.Status != "uncertain" {
		t.Fatalf("不确定回复未隔离 record=%+v err=%v", record, recordErr)
	}
}

// TestReplyOnceQuarantinesWhenUncertainStateIsRejected 验证 uncertain 状态被数据库约束拒绝时，降级隔离仍阻止租约重发。
func TestReplyOnceQuarantinesWhenUncertainStateIsRejected(t *testing.T) {
	// store、cleanup 保存隔离数据库及其关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是默认回复配置、发送状态和租约更新共用的无截止上下文。
	ctx := context.Background()
	// setupErr 保存启用一次性默认回复的配置写入错误。
	if setupErr := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyOnce: true, ReplyContent: "欢迎"}); setupErr != nil {
		t.Fatal(setupErr)
	}
	// triggerErr 模拟数据库拒绝直接进入 uncertain 状态的约束错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER deny_uncertain_status BEFORE UPDATE OF status ON default_reply_records WHEN NEW.status='uncertain' BEGIN SELECT RAISE(FAIL,'fixture rejection'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// sender 让平台返回可能已送达的未知结果。
	sender := &recordingSender{textErr: &ws.SendError{Kind: ws.SendUncertain}}
	// service 使用真实投递记录存储验证降级隔离。
	service := NewReplyService("cid", store, sender, nil, nil, nil)
	// firstErr 保存首次发送返回的不确定错误。
	if firstErr := service.Handle(ctx, chatMsg("你好", "", "chat-uncertain-fallback")); firstErr == nil {
		t.Fatal("不确定发送结果必须返回调用错误")
	}
	// expireErr 保存租约到期模拟更新结果。
	if _, expireErr := store.DB.ExecContext(ctx, `UPDATE default_reply_records SET lease_expires_at=0 WHERE cookie_id=? AND chat_id=?`, "cid", "chat-uncertain-fallback"); expireErr != nil {
		t.Fatal(expireErr)
	}
	// secondErr 保存同一会话再次触发时的结果；降级隔离记录不应再次发送。
	secondErr := service.Handle(ctx, chatMsg("还在吗", "", "chat-uncertain-fallback"))
	if secondErr != nil || sender.textCalls != 1 {
		t.Fatalf("降级隔离后不应重发 secondErr=%v calls=%d", secondErr, sender.textCalls)
	}
	// record、recordErr 保存降级隔离后的记录状态及查询错误。
	record, recordErr := store.DefaultReps.Record(ctx, "cid", "chat-uncertain-fallback")
	if recordErr != nil || record.Status != "pending" {
		t.Fatalf("降级隔离记录状态异常 record=%+v err=%v", record, recordErr)
	}
}

// TestReplyOnceDoesNotReclaimWhenUncertainPersistenceFails 验证未知结果的两次状态写入都失败时仍不会自动重发。
func TestReplyOnceDoesNotReclaimWhenUncertainPersistenceFails(t *testing.T) {
	// store、cleanup 保存隔离数据库及其关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是默认回复配置、发送状态和租约更新共用的无截止上下文。
	ctx := context.Background()
	// setupErr 保存启用一次性默认回复的配置写入错误。
	if setupErr := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyOnce: true, ReplyContent: "欢迎"}); setupErr != nil {
		t.Fatal(setupErr)
	}
	// triggerErr 模拟数据库在未知结果写入期间完全拒绝状态更新。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER deny_reply_state_updates BEFORE UPDATE ON default_reply_records BEGIN SELECT RAISE(FAIL,'fixture write failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// sender 模拟平台可能已经送达但连接返回未知结果。
	sender := &recordingSender{textErr: &ws.SendError{Kind: ws.SendUncertain}}
	// service 使用真实投递记录存储验证 sending 状态的持久化保护。
	service := NewReplyService("cid", store, sender, nil, nil, nil)
	// firstErr 保存首次发送返回的不确定错误。
	if firstErr := service.Handle(ctx, chatMsg("你好", "", "chat-uncertain-db-down")); firstErr == nil {
		t.Fatal("不确定发送结果必须返回调用错误")
	}
	// dropErr 恢复租约更新能力，模拟数据库恢复后再次收到同一会话消息。
	if _, dropErr := store.DB.ExecContext(ctx, `DROP TRIGGER deny_reply_state_updates`); dropErr != nil {
		t.Fatal(dropErr)
	}
	// expireErr 模拟原领取租约已过期。
	if _, expireErr := store.DB.ExecContext(ctx, `UPDATE default_reply_records SET lease_expires_at=0 WHERE cookie_id=? AND chat_id=?`, "cid", "chat-uncertain-db-down"); expireErr != nil {
		t.Fatal(expireErr)
	}
	// secondErr 保存数据库恢复后的再次处理结果；sending 记录不得触发第二次外部发送。
	secondErr := service.Handle(ctx, chatMsg("还在吗", "", "chat-uncertain-db-down"))
	if secondErr != nil || sender.textCalls != 1 {
		t.Fatalf("数据库写入失败后不应重发 secondErr=%v calls=%d", secondErr, sender.textCalls)
	}
	// record、recordErr 保存仍需人工核对的发送状态及读取错误。
	record, recordErr := store.DefaultReps.Record(ctx, "cid", "chat-uncertain-db-down")
	if recordErr != nil || record.Status != "sending" {
		t.Fatalf("未知结果状态不应回到可重试 pending record=%+v err=%v", record, recordErr)
	}
}

// TestReply_APIPriorityAndError 外部 Agent 槽位位于关键词之后：关键词命中时不询问 Agent；
// 关键词未命中时 Agent 命中优先于后续规则，Agent 报错或返回 nil 时降级到后续规则；未设置来源标签时沿用 "API"。
func TestReply_APIPriorityAndError(t *testing.T) {
	// s、cleanup 是隔离的数据库聚合及清理函数。
	s, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','在吗','关键词回复','text')`)

	// 关键词命中 → 使用关键词，且不会询问 Agent。
	api := &fakeAPIReplier{result: &ReplyResult{Text: "API回复"}}
	// r 是只装配 Agent 槽位替身的回复服务。
	r := NewReplyService("cid", s, nil, api, nil, nil)
	// res 保存关键词命中时的解析结果。
	res := r.resolve(ctx, chatMsg("在吗", "", "chat1"))
	if res == nil || res.Source != "关键词" || res.Text != "关键词回复" || api.called != 0 {
		t.Fatalf("关键词应优先于 Agent 且不应调用 Agent，got %+v called=%d", res, api.called)
	}

	// 关键词未命中 → Agent 命中，沿用历史来源标签 API。
	res = r.resolve(ctx, chatMsg("没有任何规则命中", "", "chat1"))
	if res == nil || res.Source != "API" || res.Text != "API回复" || api.called != 1 {
		t.Fatalf("未命中关键词时 Agent 应接手，got %+v called=%d", res, api.called)
	}

	// Agent 自行设置来源标签时保持不变。
	labeled := &fakeAPIReplier{result: &ReplyResult{Skip: true, Source: "Agent"}}
	res = NewReplyService("cid", s, nil, labeled, nil, nil).resolve(ctx, chatMsg("没有任何规则命中", "", "chat1"))
	if res == nil || res.Source != "Agent" || !res.Skip {
		t.Fatalf("Agent 自带来源标签应保留，got %+v", res)
	}

	// Agent 报错 → 降级到 AI。
	aiFallback := &fakeAIReplier{result: &ReplyResult{Text: "AI兜底"}}
	// api2 是总是返回错误的 Agent 替身。
	api2 := &fakeAPIReplier{err: errors.New("upstream down")}
	// res2 保存 Agent 报错后降级到 AI 的解析结果。
	res2 := NewReplyService("cid", s, nil, api2, aiFallback, nil).resolve(ctx, chatMsg("没有任何规则命中", "", "chat1"))
	if res2 == nil || res2.Source != "AI" || api2.called != 1 {
		t.Fatalf("Agent 报错应降级到 AI，got %+v", res2)
	}

	// Agent 返回 nil（不接手）→ 降级到 AI；Agent 先于 AI。
	api3 := &fakeAPIReplier{result: nil}
	// res3 保存 Agent 返回 nil 后降级到 AI 的解析结果。
	res3 := NewReplyService("cid", s, nil, api3, aiFallback, nil).resolve(ctx, chatMsg("没有任何规则命中", "", "chat1"))
	if res3 == nil || res3.Source != "AI" {
		t.Fatalf("Agent nil 应降级到 AI，got %+v", res3)
	}
	// api4 是返回正常回复的 Agent 替身。
	api4 := &fakeAPIReplier{result: &ReplyResult{Text: "API回复"}}
	// aiNotCalled 是用来断言“Agent 接手后不再调用 AI”的 AI 替身。
	aiNotCalled := &fakeAIReplier{result: &ReplyResult{Text: "AI兜底"}}
	// res4 保存 Agent 接手时的解析结果。
	res4 := NewReplyService("cid", s, nil, api4, aiNotCalled, nil).resolve(ctx, chatMsg("没有任何规则命中", "", "chat1"))
	if res4 == nil || res4.Source != "API" || aiNotCalled.called != 0 {
		t.Fatalf("Agent 接手后不应再调用 AI，got %+v aiCalled=%d", res4, aiNotCalled.called)
	}
}

// TestReply_AIPriorityOverDefault AI 回复优先于默认回复；AI 报错降级到默认。
func TestReply_AIPriorityOverDefault(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_once) VALUES ('cid',1,'默认回复',0)`)

	// ai 用于本次流程后续判断的人工智能
	ai := &fakeAIReplier{result: &ReplyResult{Text: "AI回复"}}
	// r 用于本次流程后续判断的r
	r := NewReplyService("cid", s, nil, nil, ai, nil)
	// res 用于本次流程后续判断的响应
	res := r.resolve(ctx, chatMsg("复杂问题", "", "chat1"))
	if res == nil || res.Source != "AI" || res.Text != "AI回复" {
		t.Fatalf("AI 命中应优先于默认，got %+v", res)
	}

	// AI 报错 → 降级到默认。
	ai2 := &fakeAIReplier{err: errors.New("model timeout")}
	// r2 用于本次流程后续判断的r2
	r2 := NewReplyService("cid", s, nil, nil, ai2, nil)
	// res2 用于本次流程后续判断的res2
	res2 := r2.resolve(ctx, chatMsg("复杂问题", "", "chat1"))
	if res2 == nil || res2.Source != "默认" || res2.Text != "默认回复" {
		t.Fatalf("AI 报错应降级到默认，got %+v", res2)
	}
}

// TestReply_ImageKeyword 图片类型关键词返回 ImageURL。
func TestReply_ImageKeyword(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,image_url,type) VALUES ('cid','看图','','http://img/x.png','image')`)

	// r 用于本次流程后续判断的r
	r := NewReplyService("cid", s, nil, nil, nil, nil)
	// res 用于本次流程后续判断的响应
	res := r.resolve(ctx, chatMsg("发看图", "item1", "chat1"))
	if res == nil || res.Source != "关键词" || res.ImageURL != "http://img/x.png" {
		t.Fatalf("图片关键词应返回 ImageURL，got %+v", res)
	}
}

// TestReply_HandleSendsImageThenText 验证 Handle 通过完整消息端口先发图片后发文本，且 Skip 不发送；t 管理本测试。
func TestReply_HandleSendsImageThenText(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_image_url,reply_once) VALUES ('cid',1,'文字','http://img/y.png',0)`)

	// sender 用于本次流程后续判断的sender
	sender := &recordingSender{}
	// r 用于本次流程后续判断的r
	r := NewReplyService("cid", s, sender, nil, nil, nil)
	if // err 用于本次流程后续判断的err
	err := r.Handle(ctx, chatMsg("在吗", "", "chat9")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(sender.images) != 1 || sender.images[0].url != "http://img/y.png" || sender.images[0].width != 0 || sender.images[0].height != 0 {
		t.Fatalf("应先发图片，got %+v", sender.images)
	}
	if len(sender.texts) != 1 || sender.texts[0].text != "文字" {
		t.Fatalf("再发文本，got %+v", sender.texts)
	}

	// Skip（空默认回复）不应发送任何内容。
	s.DB.ExecContext(ctx, `UPDATE default_replies SET reply_content='', reply_image_url='' WHERE cookie_id='cid'`)
	// sender2 用于本次流程后续判断的sender2
	sender2 := &recordingSender{}
	// r2 用于本次流程后续判断的r2
	r2 := NewReplyService("cid", s, sender2, nil, nil, nil)
	if // err 用于本次流程后续判断的err
	err := r2.Handle(ctx, chatMsg("在吗", "", "chat9")); err != nil {
		t.Fatalf("Handle skip: %v", err)
	}
	if len(sender2.texts) != 0 || len(sender2.images) != 0 {
		t.Fatalf("Skip 不应发送，got texts=%+v images=%+v", sender2.texts, sender2.images)
	}
}

// TestReply_HandleDelegatesImageURLWithoutDimensionProbe 验证图片 URL 原样交给完整消息端口而不在引擎读取尺寸；t 管理本测试。
func TestReply_HandleDelegatesImageURLWithoutDimensionProbe(t *testing.T) {
	// store 和 cleanup 保存当前测试独占的回复数据库及关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是当前测试回复读取和投递使用的无截止上下文。
	ctx := context.Background()
	// setupErr 保存图片关键词回复配置写入结果。
	_, setupErr := store.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,image_url,type) VALUES ('cid','照片','','https://images.example/photo.png','image')`)
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	// sender 记录完整消息端口收到的图片地址和尺寸占位。
	sender := &recordingSender{}
	// reply 使用统一完整消息端口发送图片，不配置任何引擎级尺寸解析器。
	reply := NewReplyService("cid", store, sender, nil, nil, nil)
	// sendErr 保存完整消息发送结果。
	if sendErr := reply.Handle(ctx, chatMsg("给我照片", "", "chat-image-dimensions-fallback")); sendErr != nil {
		t.Fatalf("图片回复不应被引擎尺寸解析阻断: %v", sendErr)
	}
	if len(sender.images) != 1 || sender.images[0].width != 0 || sender.images[0].height != 0 {
		t.Fatalf("引擎不应自行设置图片尺寸，实际发送=%+v", sender.images)
	}
}

// TestReply_HandleDelegatesCompleteMessageToChatDelivery 验证自动回复只生成完整消息并交给聊天应用统一发送。
func TestReply_HandleDelegatesCompleteMessageToChatDelivery(t *testing.T) {
	// store、cleanup 保存一次性默认回复状态的隔离数据库。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// setupErr 保存完整图片文字默认回复的配置写入结果。
	if setupErr := store.DefaultReps.Upsert(context.Background(), "cid", db.DefaultReply{Enabled: true, ReplyOnce: true, ReplyContent: "文字", ReplyImageURL: "https://origin.example/reply.jpg"}); setupErr != nil {
		t.Fatal(setupErr)
	}
	// delivery 记录引擎提交的完整消息，并模拟两个分段都已由平台确认。
	delivery := &recordingReplyDelivery{result: ReplySendResult{ImageSent: true, TextSent: true}}
	// reply 使用生产构造路径，不能访问旧版直接发送器或自动回复尺寸探测器。
	reply := NewReplyService("cid", store, delivery, nil, nil, nil)
	// sendErr 保存统一聊天应用发送结果。
	if sendErr := reply.Handle(context.Background(), chatMsg("你好", "", "chat-complete")); sendErr != nil {
		t.Fatalf("Handle: %v", sendErr)
	}
	if len(delivery.messages) != 1 {
		t.Fatalf("完整回复调用次数=%d", len(delivery.messages))
	}
	// message 是交给消息页面的完整图片文字回复，原始 URL 不应携带猜测尺寸。
	message := delivery.messages[0]
	if message.AccountID != "cid" || message.ChatID != "chat-complete" || message.ToUserID != "buyer1" || message.Text != "文字" || message.ImageURL != "https://origin.example/reply.jpg" {
		t.Fatalf("完整回复内容错误=%+v", message)
	}
	// record、recordErr 保存聊天应用成功后的一次性分段状态。
	record, recordErr := store.DefaultReps.Record(context.Background(), "cid", "chat-complete")
	if recordErr != nil || record.Status != "sent" || !record.ImageSent || !record.TextSent {
		t.Fatalf("reply_once 状态错误 record=%+v err=%v", record, recordErr)
	}
}

// TestParseMessageIDFromJSON bizTag/extJson 中提取 messageId。
func TestParseMessageIDFromJSON(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		`{"messageId":"abc123"}`: "abc123",
		`{"sourceId":"x"}`:       "",
		`not json`:               "",
		`{}`:                     "",
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := parseMessageIDFromJSON(in); got != want {
			t.Errorf("parseMessageIDFromJSON(%q)=%q want %q", in, got, want)
		}
	}
}

// TestExtractMessageID 优先实时消息的 PNM ID，其次兼容 bizTag/extJson，无则空。
func TestExtractMessageID(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := extractMessageID(map[string]any{
		"1": map[string]any{
			"3": "4263141580162.PNM",
			"10": map[string]any{
				"bizTag":  `{"messageId":"biz-id"}`,
				"extJson": `{"messageId":"ext-id"}`,
			},
		},
	}); got != "4263141580162.PNM" {
		t.Errorf("PNM 优先: got %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := extractMessageID(map[string]any{
		"1": map[string]any{
			"10": map[string]any{
				"extJson": `{"messageId":"ext-id"}`,
			},
		},
	}); got != "ext-id" {
		t.Errorf("extJson 兜底: got %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := extractMessageID(map[string]any{"1": map[string]any{}}); got != "" {
		t.Errorf("无 ID: got %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := extractMessageID(map[string]any{"1": map[string]any{"10": map[string]any{"extJson": `{"messageId":"legacy-uuid"}`, "nested": map[string]any{"messageId": "4269999999999.PNM"}}}}); got != "4269999999999.PNM" {
		t.Errorf("嵌套 PNM 优先: got %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := extractMessageID(map[string]any{"1": map[string]any{"10": map[string]any{"payload": `{"messageId":"4270000000000.PNM"}`}}}); got != "4270000000000.PNM" {
		t.Errorf("JSON 字符串中的 PNM: got %q", got)
	}
}

// TestMessageContentType extJson 优先，其次 m6.3.4，再其次 m6.3.5 内嵌 JSON。
func TestMessageContentType(t *testing.T) {
	// extJson 命中。
	if got := messageContentType(
		map[string]any{},
		map[string]any{"extJson": `{"contentType":"14"}`},
	); got != "14" {
		t.Errorf("extJson: got %q", got)
	}
	// m6.3.4 数字字段（toString 把 float64 转 "26"）。
	if got := messageContentType(
		map[string]any{"6": map[string]any{"3": map[string]any{"4": float64(26)}}},
		map[string]any{},
	); got != "26" {
		t.Errorf("m6.3.4: got %q", got)
	}
	// m6.3.5 内嵌 JSON。
	if got := messageContentType(
		map[string]any{"6": map[string]any{"3": map[string]any{"5": `{"contentType":"14"}`}}},
		map[string]any{},
	); got != "14" {
		t.Errorf("m6.3.5: got %q", got)
	}
	// 都没有 → 空。
	if got := messageContentType(map[string]any{}, map[string]any{}); got != "" {
		t.Errorf("空: got %q", got)
	}
}

// TestIsNonUserChatNotice contentType 14/26 为系统提示，应过滤。
func TestIsNonUserChatNotice(t *testing.T) {
	if !isNonUserChatNotice(map[string]any{}, map[string]any{"extJson": `{"contentType":"14"}`}, "[提示]") {
		t.Error("contentType=14 应判为系统提示")
	}
	if !isNonUserChatNotice(map[string]any{}, map[string]any{"extJson": `{"contentType":"26"}`}, "[卡片]") {
		t.Error("contentType=26 应判为系统提示")
	}
	if !isNonUserChatNotice(map[string]any{}, map[string]any{"extJson": `{"contentType":"25"}`}, "快给ta一个评价吧～") {
		t.Error("contentType=25 评价提醒应判为系统提示")
	}
	if !isNonUserChatNotice(map[string]any{}, map[string]any{}, "快给ta一个评价吧～") {
		t.Error("评价提醒文案即使缺少扩展字段也不应进入聊天回复")
	}
	if isNonUserChatNotice(map[string]any{}, map[string]any{}, "[买家说你好]") {
		t.Error("普通消息不应判为系统提示")
	}
	if !isNonUserChatNotice(map[string]any{}, map[string]any{"sessionType": "24"}, "售后问卷") {
		t.Error("非真人会话不应进入买家聊天列表")
	}
}

// TestIsNonUserChatNoticeFiltersOfficialSenderAndPlaceholder 封装TestIsNon用户聊天NoticeFiltersOfficialSenderAndPlaceholder业务协调。
func TestIsNonUserChatNoticeFiltersOfficialSenderAndPlaceholder(t *testing.T) {
	if !isNonUserChatNotice(map[string]any{}, map[string]any{"senderUserId": "1400", "reminderContent": "邀您填写售后问卷"}, "邀您填写售后问卷") {
		t.Error("闲小蜜消息应判为官方系统消息")
	}
	if !isNonUserChatNotice(map[string]any{}, map[string]any{"senderUserId": "peer-1"}, "发来一条新消息") {
		t.Error("官方通知占位文本不应进入聊天回复")
	}
}

// TestToStringAndTrimFloatInt 数字/字符串安全转换。
func TestToStringAndTrimFloatInt(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := toString(float64(26)); got != "26" {
		t.Errorf("toString(float64 26)=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := toString("hello"); got != "hello" {
		t.Errorf("toString(string)=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := toString(nil); got != "" {
		t.Errorf("toString(nil)=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := trimFloatInt(12.00); got != "12" {
		t.Errorf("trimFloatInt(12.00)=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := trimFloatInt(12.50); got != "12.5" {
		t.Errorf("trimFloatInt(12.50)=%q", got)
	}
}

// TestContains 大小写不敏感包含。
func TestContains(t *testing.T) {
	if !contains("Hello World", "world") {
		t.Error("应大小写不敏感命中")
	}
	if contains("Hello", "xyz") {
		t.Error("不应误命中")
	}
}

// 编译期保证 db 包被引用（测试构造 store 时使用）。
var _ = db.DialectSQLite
