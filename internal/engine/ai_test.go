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

// TestBuildSystemPrompt 自定义 prompt 替换 {item_title}；默认模板含商品信息与折扣上限。
func TestBuildSystemPrompt(t *testing.T) {
	// 自定义 prompt 优先，仅替换 {item_title}。
	got := buildSystemPrompt("你是卖{item_title}的客服", "iPhone", 0, "", 0, 0)
	if got != "你是卖iPhone的客服" {
		t.Fatalf("自定义 prompt 替换: got %q", got)
	}

	// 默认模板：折扣上限 <=0 时兜底 10% / 100 元。
	got = buildSystemPrompt("", "会员卡", 9.9, "月卡", 0, 0)
	if !strings.Contains(got, "标题：会员卡") || !strings.Contains(got, "价格：9.90 元") {
		t.Fatalf("默认模板缺商品信息: %q", got)
	}
	if !strings.Contains(got, "最多优惠 10% 或 100 元") {
		t.Fatalf("默认折扣上限未兜底: %q", got)
	}

	// 显式折扣上限。
	got = buildSystemPrompt("", "会员卡", 9.9, "月卡", 20, 50)
	if !strings.Contains(got, "最多优惠 20% 或 50 元") {
		t.Fatalf("显式折扣上限: %q", got)
	}
}

// newAIStore 构造一个带 admin + cookie 的测试 store，供 AIReplier 使用。
func newAIStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "ai.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d, db.DialectSQLite)
	ctx := context.Background()
	s.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	s.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=tk;", admin.ID)
	return s, func() { d.Close() }
}

// TestAIReply_DisabledReturnsNil AI 未启用 / 无 APIKey 时应返回 nil,nil（降级到下一级）。
func TestAIReply_DisabledReturnsNil(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	a := NewAIReplier("cid", s, nil)

	// 无配置记录 → 未启用 → nil。
	res, err := a.Reply(ctx, chatMsg("在吗", "item1", "chat1"))
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
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	a := NewAIReplier("cid", s, nil)

	res, err := a.Reply(ctx, chatMsg("在吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("无 APIKey 应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// mockOpenAIServer 启动一个返回固定 chat completion 响应的 HTTP 服务。
// status=0 表示返回成功响应；其余为 HTTP 状态码（用于失败降级测试）。
func mockOpenAIServer(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			http.Error(w, "upstream error", status)
			return
		}
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
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, http.StatusInternalServerError, "")

	// 启用 AI + 配 APIKey + 指向 mock 服务。
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)

	a := NewAIReplier("cid", s, nil)
	res, err := a.Reply(ctx, chatMsg("在吗", "item1", "chat1"))
	if err == nil {
		t.Fatalf("HTTP 500 应返回错误，got res=%+v", res)
	}
	if res != nil {
		t.Fatalf("失败时不应返回结果: %+v", res)
	}
}

// TestAIReply_EmptyChoicesReturnsNil 成功响应但无 choices → nil（不报错）。
func TestAIReply_EmptyChoicesReturnsNil(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	t.Cleanup(srv.Close)
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)

	a := NewAIReplier("cid", s, nil)
	res, err := a.Reply(ctx, chatMsg("在吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("空 choices 应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// TestAIReply_SuccessReturnsContent 正常调用返回 AI 文本。
func TestAIReply_SuccessReturnsContent(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, 0, "你好，在的哦")
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)

	a := NewAIReplier("cid", s, nil)
	res, err := a.Reply(ctx, chatMsg("在吗", "item1", "chat1"))
	if err != nil {
		t.Fatalf("成功调用不应报错: %v", err)
	}
	if res == nil || res.Text != "你好，在的哦" {
		t.Fatalf("应返回 AI 文本: %+v", res)
	}
}

// TestGlobalAIConfig 默认值兜底 + 显式设置覆盖。
func TestGlobalAIConfig(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
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

// TestAIReplierItemInfo 商品缺失时兜底占位；存在时取真实标题/价格/描述。
func TestAIReplierItemInfo(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	a := NewAIReplier("cid", s, nil)

	// 商品不存在 → 占位。
	title, price, desc := a.itemInfo(ctx, "no-such-item")
	if title != "商品信息获取失败" || desc != "暂无商品描述" || price != 0 {
		t.Fatalf("缺失商品应兜底: title=%q price=%v desc=%q", title, price, desc)
	}

	// 插入商品。
	s.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_price, item_detail) VALUES ('cid','item1','会员卡','9.90','月卡服务')`)
	title, price, desc = a.itemInfo(ctx, "item1")
	if title != "会员卡" || price != 9.9 || desc != "月卡服务" {
		t.Fatalf("真实商品: title=%q price=%v desc=%q", title, price, desc)
	}
}
