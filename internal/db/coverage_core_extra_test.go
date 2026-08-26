package db

import (
	"context"
	"errors"
	"testing"
)

// TestAIReplySettingsAndConversationBranches 覆盖 AI 配置、会话历史和轮次统计的确定性路径。
func TestAIReplySettingsAndConversationBranches(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存 AI 配置所属账号。
	userID, cookieID := seedAccount(t, store)

	// enabled、enabledErr 验证未配置账号按关闭处理。
	enabled, enabledErr := store.AIReply.IsEnabled(ctx, cookieID)
	if enabledErr != nil || enabled {
		t.Fatalf("missing IsEnabled=%v err=%v", enabled, enabledErr)
	}
	// aiEnabled、autoAdjustEnabled、modeErr 验证未配置账号的双开关默认关闭。
	aiEnabled, autoAdjustEnabled, modeErr := store.AIReply.PricingMode(ctx, cookieID)
	if modeErr != nil || aiEnabled || autoAdjustEnabled {
		t.Fatalf("missing PricingMode=%v/%v err=%v", aiEnabled, autoAdjustEnabled, modeErr)
	}
	// missing、missingErr 验证读取不存在的完整配置返回领域未找到错误。
	missing, missingErr := store.AIReply.Get(ctx, cookieID)
	if missing != nil || !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("missing Get=%+v err=%v", missing, missingErr)
	}

	// settings 保存一组包含开关、约束和提示词的 AI 配置。
	settings := AIReplySettings{
		AIEnabled:              true,
		AutoAdjustPriceEnabled: true,
		ModelName:              "model",
		BaseURL:                "https://example.test/v1",
		MaxDiscountPercent:     12,
		MaxDiscountAmount:      88,
		MaxBargainRounds:       3,
		CustomPrompts:          "prompt",
	}
	// err 表示 AI 配置写入错误。
	if err := store.AIReply.UpsertSettings(ctx, cookieID, settings); err != nil {
		t.Fatal(err)
	}
	// loaded、loadedErr 保存重新读取后的非敏感 AI 配置。
	loaded, loadedErr := store.AIReply.Get(ctx, cookieID)
	if loadedErr != nil || loaded == nil || !loaded.AIEnabled || !loaded.AutoAdjustPriceEnabled || loaded.ModelName != "qwen-plus" || loaded.BaseURL == "" || loaded.CustomPrompts != "prompt" {
		t.Fatalf("loaded=%+v err=%v", loaded, loadedErr)
	}
	// listed、listErr 验证用户列表只返回非敏感配置。
	listed, listErr := store.AIReply.ListForUser(ctx, userID)
	if listErr != nil || len(listed) != 1 || !listed[0].AIEnabled || listed[0].APIKey != "" {
		t.Fatalf("listed=%+v err=%v", listed, listErr)
	}
	// replaced 验证空提示词的持久化兼容分支。
	settings.CustomPrompts = ""
	// replaced 保存空提示词转换后的数据库值。
	replaced := nullableAIString(settings.CustomPrompts)
	if replaced != nil {
		t.Fatalf("nullable empty=%v", replaced)
	}

	// userMessage、assistantMessage 保存一轮对话的两条消息。
	userMessage := AIConversationMessage{Role: "user", Content: "你好", Intent: "bargain", BargainCount: 1}
	// assistantMessage 保存 AI 对用户消息的报价回复。
	assistantMessage := AIConversationMessage{Role: "assistant", Content: "报价", Intent: "offer", BargainCount: 1}
	// err 表示原子保存双消息时的数据库错误。
	if err := store.AIReply.AddConversationExchange(ctx, cookieID, "chat", "buyer", "item", userMessage, assistantMessage); err != nil {
		t.Fatal(err)
	}
	// extraMessage 验证单条追加消息路径。
	extraMessage := AIConversationMessage{Role: "user", Content: "再议", BargainCount: 2}
	// err 表示单条会话消息追加错误。
	if err := store.AIReply.AddConversation(ctx, cookieID, "chat", "buyer", "item", extraMessage); err != nil {
		t.Fatal(err)
	}
	// history、historyErr 保存按时间正序返回的会话历史。
	history, historyErr := store.AIReply.ConversationHistory(ctx, cookieID, "chat", "item", 0)
	if historyErr != nil || len(history) != 3 || history[0].Content != "你好" || history[2].Content != "再议" {
		t.Fatalf("history=%+v err=%v", history, historyErr)
	}
	// count、countErr 保存当前会话最高砍价轮次。
	count, countErr := store.AIReply.CurrentBargainCount(ctx, cookieID, "chat", "item")
	if countErr != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, countErr)
	}
}

// TestUserSettingsRiskLogsAndHealth 覆盖用户设置、风控日志和健康探针的基本业务路径。
func TestUserSettingsRiskLogsAndHealth(t *testing.T) {
	// store、cleanup 提供真实 SQLite 数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号归属。
	userID, cookieID := seedAccount(t, store)

	// err 表示首次用户设置写入错误。
	if err := store.UserSettings.SetForUser(ctx, userID, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	// value、valueErr 保存用户单项设置读取结果。
	value, valueErr := store.UserSettings.GetForUser(ctx, userID, "theme")
	if valueErr != nil || value != "dark" {
		t.Fatalf("value=%q err=%v", value, valueErr)
	}
	// missing、missingErr 验证不存在设置返回空值。
	missing, missingErr := store.UserSettings.GetForUser(ctx, userID, "missing")
	if missingErr != nil || missing != "" {
		t.Fatalf("missing=%q err=%v", missing, missingErr)
	}
	// err 表示覆盖用户设置写入错误。
	if err := store.UserSettings.SetForUser(ctx, userID, "theme", "light"); err != nil {
		t.Fatal(err)
	}
	// all、allErr 保存用户全部设置，验证覆盖写入结果。
	all, allErr := store.UserSettings.AllForUser(ctx, userID)
	if allErr != nil || all["theme"] != "light" {
		t.Fatalf("all=%v err=%v", all, allErr)
	}

	// logID、logErr 保存使用默认事件与状态写入的风控日志。
	logID, logErr := store.RiskLogs.Add(ctx, RiskControlLog{CookieID: cookieID, EventDescription: "验证"})
	if logErr != nil || logID == 0 {
		t.Fatalf("logID=%d err=%v", logID, logErr)
	}
	// err 表示风控日志终态更新错误。
	if err := store.RiskLogs.Update(ctx, logID, RiskControlLog{ProcessingResult: "ok", ProcessingStatus: "success", CaptchaEngine: "test", DurationMS: 12}); err != nil {
		t.Fatal(err)
	}
	// err 表示零 ID 安全跳过更新的返回结果。
	if err := store.RiskLogs.Update(ctx, 0, RiskControlLog{}); err != nil {
		t.Fatal(err)
	}
	// probe 保存当前数据库的健康检查入口。
	probe := store.HealthProbe()
	// err 表示已初始化数据库探测结果。
	if err := probe.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	// nilProbe 覆盖空数据库探针的稳定错误路径。
	nilProbe := newHealthProbe(nil)
	// err 表示空数据库探针返回的初始化错误。
	if err := nilProbe.Ping(ctx); err == nil {
		t.Fatal("nil probe should fail")
	}
	// nilStoreProbe 覆盖空 Store 接收者的健康探针路径。
	var nilStore *Store
	// err 表示空 Store 探针返回的初始化错误。
	if err := nilStore.HealthProbe().Ping(ctx); err == nil {
		t.Fatal("nil store probe should fail")
	}
}
