package db

import (
	"context"
	"strings"
	"testing"
)

// TestSecurityAuditLogsNeverStoreSecretValues 验证敏感访问审计只保存键名和操作元数据。
func TestSecurityAuditLogsNeverStoreSecretValues(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存审计测试上下文。
	ctx := context.Background()
	// err 保存审计记录写入错误。
	if err := store.SecurityAudit.Add(ctx, SecurityAuditLog{UserID: 1, Action: "settings.write", Resource: "system_settings", Keys: []string{"smtp_password", "ai_api_key"}, Outcome: "accepted", CreatedAt: 123}); err != nil {
		t.Fatalf("写入敏感访问审计失败: %v", err)
	}
	// records、err 保存读取到的审计记录及错误。
	records, err := store.SecurityAudit.ListByUser(ctx, 1, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("读取敏感访问审计失败: records=%+v err=%v", records, err)
	}
	if records[0].Action != "settings.write" || records[0].Resource != "system_settings" || len(records[0].Keys) != 2 || records[0].CreatedAt != 123 {
		t.Fatalf("敏感访问审计字段异常: %+v", records[0])
	}
	// raw 保存数据库中的审计 JSON，用于确认不会出现秘密值。
	var raw string
	// err 保存读取原始审计 JSON 的数据库错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT keys_json FROM security_audit_logs WHERE id=?`, records[0].ID).Scan(&raw); err != nil {
		t.Fatalf("读取原始审计 JSON 失败: %v", err)
	}
	if raw == "" || containsAny(raw, "sk-plain", "smtp-secret") {
		t.Fatalf("审计内容疑似包含秘密值: %q", raw)
	}
}

// containsAny 判断文本是否包含任意敏感片段，供审计测试复用。
func containsAny(value string, fragments ...string) bool {
	// fragment 是当前待匹配的敏感片段。
	for _, fragment := range fragments {
		if fragment != "" && strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
