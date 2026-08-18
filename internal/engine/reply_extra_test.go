package engine

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
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
}

// textSent 用于本次流程后续判断的文本Sent
type textSent struct {
	chatID, toUserID, text string
}

// imageSent 用于本次流程后续判断的图片Sent
type imageSent struct {
	chatID, toUserID, url string
	cardID                int64
}

// SendText 封装Send文本业务协调。
func (r *recordingSender) SendText(_ context.Context, chatID, toUserID, text string) error {
	if r.textErr != nil {
		return r.textErr
	}
	r.texts = append(r.texts, textSent{chatID, toUserID, text})
	return nil
}

// SendImage 封装Send图片业务协调。
func (r *recordingSender) SendImage(_ context.Context, chatID, toUserID, url string, cardID int64, _, _ int) error {
	if r.imageErr != nil {
		return r.imageErr
	}
	r.images = append(r.images, imageSent{chatID, toUserID, url, cardID})
	return nil
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

// TestReply_APIPriorityAndError API 回复命中时优先级最高；API 报错时降级到关键词。
func TestReply_APIPriorityAndError(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','在吗','关键词回复','text')`)

	// API 返回结果 → 用 API。
	api := &fakeAPIReplier{result: &ReplyResult{Text: "API回复"}}
	// r 用于本次流程后续判断的r
	r := NewReplyService("cid", s, nil, api, nil, nil)
	// res 用于本次流程后续判断的响应
	res := r.resolve(ctx, chatMsg("在吗", "", "chat1"))
	if res == nil || res.Source != "API" || res.Text != "API回复" {
		t.Fatalf("API 命中应优先，got %+v", res)
	}

	// API 报错 → 降级到关键词。
	api2 := &fakeAPIReplier{err: errors.New("upstream down")}
	// r2 用于本次流程后续判断的r2
	r2 := NewReplyService("cid", s, nil, api2, nil, nil)
	// res2 用于本次流程后续判断的res2
	res2 := r2.resolve(ctx, chatMsg("在吗", "", "chat1"))
	if res2 == nil || res2.Source != "关键词" || res2.Text != "关键词回复" {
		t.Fatalf("API 报错应降级到关键词，got %+v", res2)
	}

	// API 返回 nil（无回复）→ 降级到关键词。
	api3 := &fakeAPIReplier{result: nil}
	// r3 用于本次流程后续判断的r3
	r3 := NewReplyService("cid", s, nil, api3, nil, nil)
	// res3 用于本次流程后续判断的res3
	res3 := r3.resolve(ctx, chatMsg("在吗", "", "chat1"))
	if res3 == nil || res3.Source != "关键词" {
		t.Fatalf("API nil 应降级到关键词，got %+v", res3)
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

// TestReply_HandleSendsImageThenText Handle 先发图片后发文本；Skip 不发送。
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
	if len(sender.images) != 1 || sender.images[0].url != "http://img/y.png" {
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
