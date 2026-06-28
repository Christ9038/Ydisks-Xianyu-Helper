package engine

import (
	"context"
	"path/filepath"
	"testing"

	"xianyu-go/internal/db"
)

func newReplyStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d)
	s.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(context.Background(), "admin")
	s.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	return s, func() { d.Close() }
}

func chatMsg(text, itemID, chatID string) ChatMessage {
	return ChatMessage{
		AccountID: "cid", CookieStr: "",
		ChatID: chatID, SenderUserID: "buyer1", SenderName: "买家",
		Text: text, ItemID: itemID,
	}
}

// TestKeywordReply_MatchAndVariableSubst 关键词匹配 + 变量替换。
func TestKeywordReply_MatchAndVariableSubst(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	// 关键词 "在吗" → "你好{send_user_name}，在的，请问要{send_message}吗"
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','在吗','你好{send_user_name}，在的','text')`)

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	res := r.resolve(ctx, chatMsg("老板在吗", "item1", "chat1"))
	if res == nil || res.Source != "关键词" {
		t.Fatalf("应命中关键词，got %+v", res)
	}
	if res.Text != "你好买家，在的" {
		t.Errorf("变量替换: got %q want 你好买家，在的", res.Text)
	}
}

// TestKeywordReply_ItemIDPriority 商品ID关键词优先于通用关键词。
func TestKeywordReply_ItemIDPriority(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,item_id,type) VALUES
		('cid','价格','通用价格回复','','text'),
		('cid','价格','该商品专属价格回复','item1','text')`)

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	res := r.resolve(ctx, chatMsg("价格多少", "item1", "chat1"))
	if res == nil || res.Text != "该商品专属价格回复" {
		t.Errorf("应优先商品ID关键词，got %+v", res)
	}
	// 无 item_id 时走通用。
	res2 := r.resolve(ctx, chatMsg("价格多少", "", "chat1"))
	if res2 == nil || res2.Text != "通用价格回复" {
		t.Errorf("无itemID应走通用，got %+v", res2)
	}
}

// TestKeywordReply_EmptyReplySkip 匹配到空回复 → Skip。
func TestKeywordReply_EmptyReplySkip(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','静默','','text')`)
	r := NewReplyService("cid", s, nil, nil, nil, nil)
	res := r.resolve(ctx, chatMsg("静默一下", "item1", "chat1"))
	if res == nil || !res.Skip {
		t.Fatalf("空回复应 Skip=true，got %+v", res)
	}
}

// TestDefaultReply_ItemReplyFirst 指定商品回复优先于账号默认回复。
func TestDefaultReply_ItemReplyFirst(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_once) VALUES ('cid',1,'账号默认回复',0)`)
	s.DB.ExecContext(ctx, `INSERT INTO item_replay (item_id,cookie_id,reply_content) VALUES ('item1','cid','该商品专属默认回复')`)

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	res := r.resolve(ctx, chatMsg("任意消息", "item1", "chat1"))
	if res == nil || res.Text != "该商品专属默认回复" {
		t.Errorf("应优先指定商品回复，got %+v", res)
	}
}

// TestDefaultReply_ReplyOnce reply_once 防重复。
func TestDefaultReply_ReplyOnce(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_once) VALUES ('cid',1,'默认问候',1)`)

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	// 首次：应回复。
	res1 := r.resolve(ctx, chatMsg("在吗", "item1", "chatX"))
	if res1 == nil || res1.Text != "默认问候" {
		t.Fatalf("首次应回复，got %+v", res1)
	}
	// 第二次同 chat_id：reply_once 应跳过。
	res2 := r.resolve(ctx, chatMsg("还在吗", "item1", "chatX"))
	if res2 != nil {
		t.Fatalf("reply_once 应跳过第二次，got %+v", res2)
	}
	// 不同 chat_id：应回复。
	res3 := r.resolve(ctx, chatMsg("在吗", "item1", "chatY"))
	if res3 == nil || res3.Text != "默认问候" {
		t.Fatalf("不同 chat_id 应回复，got %+v", res3)
	}
}

// TestPriority_Order 四级优先级：关键词 > 默认（无API/AI时）。
func TestPriority_Order(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','在吗','关键词回复','text')`)
	s.DB.ExecContext(ctx, `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_once) VALUES ('cid',1,'默认回复',0)`)

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	// 命中关键词 → 关键词回复。
	res := r.resolve(ctx, chatMsg("在吗", "", "chat1"))
	if res == nil || res.Source != "关键词" {
		t.Errorf("应优先关键词，got %+v", res)
	}
	// 不命中关键词 → 默认回复。
	res2 := r.resolve(ctx, chatMsg("别的消息", "", "chat1"))
	if res2 == nil || res2.Source != "默认" {
		t.Errorf("无关键词应走默认，got %+v", res2)
	}
}

// TestFormatReply 变量替换。
func TestFormatReply(t *testing.T) {
	m := chatMsg("想买", "item1", "chat1")
	m.SenderName = "张三"
	got := formatReply("你好{send_user_name}，你说{send_message}", m)
	if got != "你好张三，你说想买" {
		t.Errorf("formatReply=%q", got)
	}
}

// TestParsePrice 价格解析。
func TestParsePrice(t *testing.T) {
	cases := map[string]float64{
		"￥99.5":   99.5,
		"100元":    100,
		"":        0,
		"abc":     0,
		"12.34":   12.34,
	}
	for in, want := range cases {
		if got := parsePrice(in); got != want {
			t.Errorf("parsePrice(%q)=%v want %v", in, got, want)
		}
	}
}
