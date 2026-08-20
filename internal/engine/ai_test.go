package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestBuildSystemPrompt 自定义 prompt 替换变量，且始终追加价格与轮次安全约束。
func TestBuildSystemPrompt(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := buildSystemPrompt("你是卖{item_title}的客服，价格{item_price}", "iPhone", 100, "手机", 0, 0, 3, 1, false)
	if !strings.Contains(got, "你是卖iPhone的客服，价格100.00") {
		t.Fatalf("自定义 prompt 替换: got %q", got)
	}
	if !strings.Contains(got, "任一优惠上限为 0 时不得降价") || !strings.Contains(got, "当前砍价轮次 1") {
		t.Fatalf("自定义 prompt 缺少安全约束: %q", got)
	}

	// 0 必须保留为不允许优惠，不能静默改成默认值。
	got = buildSystemPrompt("", "会员卡", 9.9, "月卡", 0, 0, 3, 0, false)
	if !strings.Contains(got, "标题：会员卡") || !strings.Contains(got, "价格：9.90 元") {
		t.Fatalf("默认模板缺商品信息: %q", got)
	}
	if !strings.Contains(got, "最多优惠 0%") || !strings.Contains(got, "最多优惠 0 元") {
		t.Fatalf("零折扣配置被改写: %q", got)
	}

	// 显式折扣上限。
	got = buildSystemPrompt("", "会员卡", 9.9, "月卡", 20, 50, 4, 2, true)
	if !strings.Contains(got, "最多优惠 20%") || !strings.Contains(got, "最多优惠 50 元") {
		t.Fatalf("显式折扣上限: %q", got)
	}
	if !strings.Contains(got, "[[AUTO_PRICE:金额]]") {
		t.Fatalf("开启自动改价时缺少结构化报价约束: %q", got)
	}
}

// TestExtractExecutableOffer 验证内部报价标记不会发送给买家，且重复或非法标记不可执行。
func TestExtractExecutableOffer(t *testing.T) {
	// visible、price、ok 分别是买家可见文本、解析金额和可执行标记状态。
	visible, price, ok := extractExecutableOffer("可以，90 元成交。 [[AUTO_PRICE:90.00]]")
	if !ok || price != 90 || visible != "可以，90 元成交。" {
		t.Fatalf("结构化报价解析异常: visible=%q price=%v ok=%v", visible, price, ok)
	}
	// visible、price、ok 复用以验证重复标记会被全部隐藏但不会执行。
	visible, price, ok = extractExecutableOffer("90 元可以 [[AUTO_PRICE:90.00]] [[AUTO_PRICE:89.00]]")
	if ok || price != 0 || strings.Contains(visible, "AUTO_PRICE") {
		t.Fatalf("重复报价标记必须拒绝执行: visible=%q price=%v ok=%v", visible, price, ok)
	}
}

// TestReplyContainsOfferedPrice 验证隐藏执行价格必须与买家正文中看到的明确报价一致。
func TestReplyContainsOfferedPrice(t *testing.T) {
	if !replyContainsOfferedPrice("可以，90.00 元成交", 90) {
		t.Fatal("相同的正文报价和执行价格应允许")
	}
	if replyContainsOfferedPrice("可以，95 元成交", 90) {
		t.Fatal("隐藏执行价格与正文不一致时必须拒绝自动改价")
	}
}

// TestMinimumAllowedPriceAndUnsafeOffer 封装TestMinimumAllowedPriceAndUnsafeOffer业务协调。
func TestMinimumAllowedPriceAndUnsafeOffer(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := minimumAllowedPrice(100, 10, 20, true); got != 90 {
		t.Fatalf("minimum=%v want 90", got)
	}
	if // got 用于本次流程后续判断的got
	got := minimumAllowedPrice(100, 0, 20, true); got != 100 {
		t.Fatalf("zero percent minimum=%v want 100", got)
	}
	if // got 用于本次流程后续判断的got
	got := minimumAllowedPrice(99.99, 15, 100, true); got != 85 {
		t.Fatalf("最低价必须向上取整到分: got=%v want 85", got)
	}
	if // unsafe 用于本次流程后续判断的unsafe
	_, unsafe := unsafeOfferedPrice("最低可以 89 元", 90); !unsafe {
		t.Fatal("低于最低价的报价应被拦截")
	}
	if // unsafe 用于本次流程后续判断的unsafe
	_, unsafe := unsafeOfferedPrice("最低可以 90 元", 90); unsafe {
		t.Fatal("边界报价应允许")
	}
}

// newAIStore 构造一个带 admin + cookie 的测试 store，供 AIReplier 使用。
func newAIStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "ai.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// s 用于本次流程后续判断的s
	s := db.NewStore(d, db.DialectSQLite)
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.Users.Create(ctx, "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	s.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=tk;", admin.ID)
	return s, func() { d.Close() }
}

// TestAIReply_DisabledReturnsNil AI 未启用 / 无 APIKey 时应返回 nil,nil（降级到下一级）。
func TestAIReply_DisabledReturnsNil(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)

	// 无配置记录 → 未启用 → nil。
	res, err := a.Reply(ctx, chatMsg("能便宜点吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("未配置应返回 nil,nil: res=%+v err=%v", res, err)
	}

	// 配置但 ai_enabled=0 → nil。
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 0, '')`)
	res, err = a.Reply(ctx, chatMsg("在吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("未启用应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// TestAIReply_NoAPIKeyReturnsNil 启用 AI 但全局未配 APIKey → nil（不报错降级）。
func TestAIReply_NoAPIKeyReturnsNil(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)

	// res、err 用于本次流程后续判断的res、err
	res, err := a.Reply(ctx, chatMsg("能便宜点吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("无 APIKey 应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// mockOpenAIServer 启动一个返回固定 chat completion 响应的 HTTP 服务。
// status=0 表示返回成功响应；其余为 HTTP 状态码（用于失败降级测试）。
// mockOpenAIServer 封装mockOpenAIServer业务协调。
func mockOpenAIServer(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			http.Error(w, "upstream error", status)
			return
		}
		// resp 用于本次流程后续判断的resp
		resp := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": content},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAIReply_HTTPErrorDegrades AI 调用 HTTP 500 → 返回错误（上层降级到默认回复）。
func TestAIReply_HTTPErrorDegrades(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// srv 用于本次流程后续判断的srv
	srv := mockOpenAIServer(t, http.StatusInternalServerError, "")

	// 启用 AI + 配 APIKey + 指向 mock 服务。
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)

	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)
	// res、err 用于本次流程后续判断的res、err
	res, err := a.Reply(ctx, chatMsg("还能优惠吗", "item1", "chat1"))
	if err == nil {
		t.Fatalf("HTTP 500 应返回错误，got res=%+v", res)
	}
	if res != nil {
		t.Fatalf("失败时不应返回结果: %+v", res)
	}
	// history、historyErr 用于本次流程后续判断的history、historyErr
	history, historyErr := s.AIReply.ConversationHistory(ctx, "cid", "chat1", "item1", 10)
	if historyErr != nil || len(history) != 0 {
		t.Fatalf("上游失败不应写入半轮历史: history=%+v err=%v", history, historyErr)
	}
}

// TestAIReply_EmptyChoicesReturnsNil 成功响应但无 choices → nil（不报错）。
func TestAIReply_EmptyChoicesReturnsNil(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	t.Cleanup(srv.Close)
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)

	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)
	// res、err 用于本次流程后续判断的res、err
	res, err := a.Reply(ctx, chatMsg("可以便宜一点吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("空 choices 应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// TestAIReply_SuccessReturnsContent 正常调用返回 AI 文本。
func TestAIReply_SuccessReturnsContent(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// srv 用于本次流程后续判断的srv
	srv := mockOpenAIServer(t, 0, "你好，在的哦")
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)

	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)
	// res、err 用于本次流程后续判断的res、err
	res, err := a.Reply(ctx, chatMsg("最低多少钱", "item1", "chat1"))
	if err != nil {
		t.Fatalf("成功调用不应报错: %v", err)
	}
	if res == nil || res.Text != "你好，在的哦" {
		t.Fatalf("应返回 AI 文本: %+v", res)
	}
}

// TestAIReplyBuildsExecutableQuote 验证自动改价开启时只返回通过边界校验且已从正文移除标记的报价。
func TestAIReplyBuildsExecutableQuote(t *testing.T) {
	// store、cleanup 是 AI 报价测试仓储及清理函数。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是模型调用和本地商品读取共用的测试上下文。
	ctx := context.Background()
	// server 返回包含买家可见价格和内部自动改价标记的模型响应。
	server := mockOpenAIServer(t, 0, "可以，90.00 元成交。 [[AUTO_PRICE:90.00]]")
	// err 是保存 AI 自动改价测试设置时不应出现的数据库错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings
		(cookie_id,ai_enabled,auto_adjust_price_enabled,max_discount_percent,max_discount_amount,max_bargain_rounds,custom_prompts)
		VALUES ('cid',1,1,10,20,3,'')`); err != nil {
		t.Fatal(err)
	}
	// err 是保存带原价商品时不应出现的数据库错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info
		(cookie_id,item_id,item_title,item_price,item_description) VALUES ('cid','item-quote','商品','100','描述')`); err != nil {
		t.Fatal(err)
	}
	// err 是保存模型测试密钥时不应出现的错误。
	if err := store.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatal(err)
	}
	// err 是保存本地模型服务地址时不应出现的错误。
	if err := store.Settings.Set(ctx, "ai_api_url", server.URL); err != nil {
		t.Fatal(err)
	}
	// result 是通过价格边界校验后的 AI 回复与可执行报价。
	result, err := NewAIReplier("cid", store, nil).Reply(ctx, chatMsg("能便宜点吗", "item-quote", "chat-quote"))
	if err != nil || result == nil || result.AutoPriceQuote == nil || result.AutoPriceQuote.PriceCents != 9000 {
		t.Fatalf("AI 可执行报价异常: result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Text, "AUTO_PRICE") || result.Text != "可以，90.00 元成交。" {
		t.Fatalf("内部报价标记不应发送给买家: %q", result.Text)
	}
}

// TestAIReply_NonBargainMessageFallsThrough 封装TestAI回复NonBargain消息FallsThrough业务协调。
func TestAIReply_NonBargainMessageFallsThrough(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", "http://127.0.0.1:1")

	// res、err 用于本次流程后续判断的res、err
	res, err := NewAIReplier("cid", s, nil).Reply(ctx, chatMsg("在吗，什么时候发货", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("非砍价消息应交给默认回复: res=%+v err=%v", res, err)
	}
}

// TestGlobalAIConfig 默认值兜底 + 显式设置覆盖。
func TestGlobalAIConfig(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)

	// 全空 → 默认 BaseURL + Model。
	cfg, err := a.globalAIConfig(ctx)
	if err != nil {
		t.Fatalf("globalAIConfig: %v", err)
	}
	if cfg.BaseURL != defaultAIBaseURL || cfg.Model != defaultAIModel || cfg.APIKey != "" {
		t.Fatalf("默认值异常: %+v", cfg)
	}

	// 显式设置。
	s.Settings.Set(ctx, "ai_api_key", "sk-x")
	s.Settings.Set(ctx, "ai_api_url", "https://example.com/v1/")
	s.Settings.Set(ctx, "ai_model", "gpt-4o")
	cfg, _ = a.globalAIConfig(ctx)
	if cfg.APIKey != "sk-x" || cfg.BaseURL != "https://example.com/v1" || cfg.Model != "gpt-4o" {
		t.Fatalf("显式设置异常: %+v", cfg)
	}
}

// TestGlobalAIConfigAuditsAPIKeyAccess 验证 AI 回复读取全局 API Key 时按账号所有者写入敏感访问审计。
func TestGlobalAIConfigAuditsAPIKeyAccess(t *testing.T) {
	// s、cleanup 保存 AI 审计测试数据库及清理函数。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 保存当前 AI 配置审计测试上下文。
	ctx := context.Background()
	// setErr 表示写入 AI 测试 API Key 时返回的错误。
	if setErr := s.Settings.Set(ctx, "ai_api_key", "sk-audit-only"); setErr != nil {
		t.Fatal(setErr)
	}
	// cfg、configErr 保存读取到的 AI 配置及读取错误。
	cfg, configErr := NewAIReplier("cid", s, nil).globalAIConfig(ctx)
	if configErr != nil || cfg.APIKey != "sk-audit-only" {
		t.Fatalf("AI 配置读取失败: cfg=%+v err=%v", cfg, configErr)
	}
	// ownerID、ownerErr 保存测试账号所有者及查询错误。
	ownerID, ownerErr := s.Cookies.GetOwnerID(ctx, "cid")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// records、listErr 保存 AI API Key 访问审计记录及查询错误。
	records, listErr := s.SecurityAudit.ListByUser(ctx, ownerID, 10)
	if listErr != nil || len(records) != 1 {
		t.Fatalf("AI API Key 审计记录异常: records=%+v err=%v", records, listErr)
	}
	if records[0].Action != "settings.use" || records[0].Resource != "ai_reply" || len(records[0].Keys) != 1 || records[0].Keys[0] != "ai_api_key" {
		t.Fatalf("AI API Key 审计上下文异常: %+v", records[0])
	}
}

// TestAIReplierItemInfo 商品缺失时兜底占位；存在时取真实标题/价格/描述。
func TestAIReplierItemInfo(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)

	// 商品不存在 → 占位。
	title, price, desc := a.itemInfo(ctx, "no-such-item")
	if title != "商品信息获取失败" || desc != "暂无商品描述" || price != 0 {
		t.Fatalf("缺失商品应兜底: title=%q price=%v desc=%q", title, price, desc)
	}

	// 插入商品。
	s.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_price, item_description, item_detail) VALUES ('cid','item1','会员卡','9.90','用户编辑描述','原始详情')`)
	title, price, desc = a.itemInfo(ctx, "item1")
	if title != "会员卡" || price != 9.9 || desc != "用户编辑描述" {
		t.Fatalf("真实商品: title=%q price=%v desc=%q", title, price, desc)
	}
}

// TestAIReplyTracksBargainRoundsAndBlocksUnsafePrice 封装TestAI回复TracksBargainRoundsAndBlocksUnsafePrice业务协调。
func TestAIReplyTracksBargainRoundsAndBlocksUnsafePrice(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// srv 用于本次流程后续判断的srv
	srv := mockOpenAIServer(t, 0, "可以，80 元成交")
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings
		(cookie_id,ai_enabled,max_discount_percent,max_discount_amount,max_bargain_rounds,custom_prompts)
		VALUES ('cid',1,10,20,1,'')`)
	s.DB.ExecContext(ctx, `INSERT INTO item_info
		(cookie_id,item_id,item_title,item_price,item_description) VALUES ('cid','item1','商品','100','描述')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)

	// a 用于本次流程后续判断的a
	a := NewAIReplier("cid", s, nil)
	// first、err 用于本次流程后续判断的first、err
	first, err := a.Reply(ctx, chatMsg("能便宜点吗", "item1", "chat1"))
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || !strings.Contains(first.Text, "90.00 元") {
		t.Fatalf("越界报价应替换成安全价格: %+v", first)
	}
	// second、err 用于本次流程后续判断的second、err
	second, err := a.Reply(ctx, chatMsg("再便宜一点", "item1", "chat1"))
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || !strings.Contains(second.Text, "已经是最低价") {
		t.Fatalf("超过最大轮次应拒绝继续降价: %+v", second)
	}
	// count、err 用于本次流程后续判断的count、err
	count, err := s.AIReply.CurrentBargainCount(ctx, "cid", "chat1", "item1")
	if err != nil || count != 2 {
		t.Fatalf("bargain count=%d err=%v want 2", count, err)
	}
	// history、err 用于本次流程后续判断的history、err
	history, err := s.AIReply.ConversationHistory(ctx, "cid", "chat1", "item1", 10)
	if err != nil || len(history) != 4 {
		t.Fatalf("history len=%d err=%v", len(history), err)
	}
}
