package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// setupGapAI 创建启用 AI 且已配置密钥与模型服务地址的账号；mode 是写入的 ai_mode 工作模式。
func setupGapAI(t *testing.T, mode string) (*AIReplierImpl, *db.Store, string) {
	t.Helper()
	// store、cleanup 是隔离数据库及其释放函数。
	store, cleanup := newAIStore(t)
	t.Cleanup(cleanup)
	// ctx 是测试配置阶段使用的上下文。
	ctx := context.Background()
	// server 是返回固定回复的本地模型服务。
	server := mockOpenAIServer(t, 0, "好的")
	// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts, ai_mode) VALUES ('cid', 1, '', ?)`, mode); err != nil {
		t.Fatal(err)
	}
	// err 保存 Set 调用返回的错误，随后断言它是否符合该场景的预期
	if err := store.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatal(err)
	}
	// err 保存 Set 调用返回的错误，随后断言它是否符合该场景的预期
	if err := store.Settings.Set(ctx, "ai_api_url", server.URL); err != nil {
		t.Fatal(err)
	}
	return NewAIReplier("cid", store, nil), store, server.URL
}

// TestAIReplyStoreFailures 验证账号配置、全局配置、对话历史与对话保存失败都返回带语境的错误。
func TestAIReplyStoreFailures(t *testing.T) {
	// ctx 是测试使用的背景上下文。
	ctx := context.Background()
	// cases 描述每个故障注入：破坏的 SQL 语句、触发消息、期望的错误片段。
	cases := []struct {
		// name 是子测试名称。
		name string
		// breakSQL 是在配置完成后破坏某个存储依赖的 SQL。
		breakSQL string
		// itemID、chatID 决定是否进入对话历史读写路径。
		itemID, chatID string
		// want 是期望错误中包含的片段。
		want string
		// persistFails 为 true 时，故障发生在提交可发送之后的对话历史保存阶段，而此前 Reply 本身已经成功。
		persistFails bool
	}{
		{name: "账号配置", breakSQL: `DROP TABLE ai_reply_settings`, want: "读取账号 AI 配置失败"},
		{name: "全局配置", breakSQL: `DROP TABLE system_settings`, want: "读取全局 AI 配置失败"},
		{name: "对话历史", breakSQL: `DROP TABLE ai_conversations`, itemID: "item", chatID: "chat", want: "读取 AI 对话历史失败"},
		{name: "保存对话", breakSQL: `CREATE TRIGGER block_ai_insert BEFORE INSERT ON ai_conversations BEGIN SELECT RAISE(ABORT, 'blocked'); END`, itemID: "item", chatID: "chat", want: "保存 AI 对话失败", persistFails: true},
	}
	// tc 是当前故障注入用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// replier、store 是已配置完成的回复器及其数据库。
			replier, store, _ := setupGapAI(t, "full_service")
			// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
			if _, err := store.DB.ExecContext(ctx, tc.breakSQL); err != nil {
				t.Fatal(err)
			}
			// result、err 是故障下的回复结果。
			result, err := replier.Reply(ctx, chatMsg("能便宜点吗", tc.itemID, tc.chatID))
			if tc.persistFails {
				// 对话历史推迟到提交后保存：生成本身成功，保存失败由 Persist 返回，调用方据此不发送回复。
				if err != nil || result == nil || result.Persist == nil {
					t.Fatalf("生成应成功并携带 Persist: result=%+v err=%v", result, err)
				}
				// persistErr 是提交后保存对话历史的错误，用于断言失败文案。
				if persistErr := result.Persist(ctx); persistErr == nil || !strings.Contains(persistErr.Error(), tc.want) {
					t.Fatalf("Persist 应返回 %q 错误: %v", tc.want, persistErr)
				}
				return
			}
			if err == nil || result != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应返回 %q 错误: result=%+v err=%v", tc.want, result, err)
			}
		})
	}
}

// TestAIReplyRejectsInvalidEndpoint 验证 AI 地址未通过网络防护时不调用模型并返回明确错误。
func TestAIReplyRejectsInvalidEndpoint(t *testing.T) {
	// original 保存原始 HTTP 客户端工厂，测试结束后恢复。
	original := newAIHTTPClient
	t.Cleanup(func() { newAIHTTPClient = original })
	newAIHTTPClient = func(string) (*http.Client, error) { return nil, errors.New("blocked endpoint") }
	// replier 是已配置完成的回复器。
	replier, _, _ := setupGapAI(t, "full_service")
	// result、err 是地址被拒绝时的回复结果。
	result, err := replier.Reply(context.Background(), chatMsg("你好", "", ""))
	if err == nil || result != nil || !strings.Contains(err.Error(), "AI API 地址无效") {
		t.Fatalf("无效地址应报错: result=%+v err=%v", result, err)
	}
}

// TestGlobalAIConfigDefaultsAndErrors 验证全局配置缺省值回填和密钥读取失败。
func TestGlobalAIConfigDefaultsAndErrors(t *testing.T) {
	// store、cleanup 是隔离数据库及其释放函数。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是配置读取使用的上下文。
	ctx := context.Background()
	// err 保存 Set 调用返回的错误，随后断言它是否符合该场景的预期
	if err := store.Settings.Set(ctx, "ai_api_key", " sk "); err != nil {
		t.Fatal(err)
	}
	// replier 是绑定测试账号的回复器。
	replier := NewAIReplier("cid", store, nil)
	// config、err 是仅配置密钥时的全局配置。
	config, err := replier.globalAIConfig(ctx)
	if err != nil || config.BaseURL != defaultAIBaseURL || config.Model != defaultAIModel || config.APIKey != "sk" {
		t.Fatalf("缺省值回填异常: %+v err=%v", config, err)
	}
	// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
	if _, err := store.DB.ExecContext(ctx, `DROP TABLE system_settings`); err != nil {
		t.Fatal(err)
	}
	// err 保存 globalAIConfig 调用返回的错误，随后断言它是否符合该场景的预期
	if _, err := replier.globalAIConfig(ctx); err == nil {
		t.Fatal("设置表不可读时应返回错误")
	}
}

// TestAIItemInfoDefaultDescriptionAndOfferParseOverflow 验证商品描述和详情都为空时的兜底，以及超出范围的报价标记被拒绝执行。
func TestAIItemInfoDefaultDescriptionAndOfferParseOverflow(t *testing.T) {
	// store、cleanup 是隔离数据库及其释放函数。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是查询使用的上下文。
	ctx := context.Background()
	// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,item_price,item_description,item_detail) VALUES ('cid','bare','T','5','','')`); err != nil {
		t.Fatal(err)
	}
	// description 保存商品描述兜底后的查询结果
	if _, _, description := NewAIReplier("cid", store, nil).itemInfo(ctx, "bare"); description != "暂无商品描述" {
		t.Fatalf("描述兜底异常: %q", description)
	}
	// visible、price、ok 是超大金额标记的解析结果，必须不可执行且标记不外泄。
	visible, price, ok := extractExecutableOffer("可以 [[AUTO_PRICE:" + strings.Repeat("9", 400) + "]]")
	if ok || price != 0 || strings.Contains(visible, "AUTO_PRICE") {
		t.Fatalf("越界金额不应可执行: visible=%q price=%v ok=%v", visible, price, ok)
	}
}
