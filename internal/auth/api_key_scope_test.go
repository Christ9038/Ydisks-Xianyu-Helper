package auth

import (
	"net/http"
	"testing"

	"xianyu-go/internal/application/apikey"
)

// TestRequiredAPIKeyScopeUsesExactMethodAndPathAllowlist 验证每项外部令牌权限按现有业务路由和 HTTP 方法精确授权。
func TestRequiredAPIKeyScopeUsesExactMethodAndPathAllowlist(t *testing.T) {
	// cases 保存必须允许或默认拒绝的路由样本。
	cases := []struct {
		name      string
		method    string
		path      string
		wantScope string
		wantAllow bool
	}{
		{name: "account-detail-read", method: http.MethodGet, path: "/api/v1/accounts/acc-1", wantScope: apikey.ScopeAccountsRead, wantAllow: true},
		{name: "account-credentials-route-is-not-exposed", method: http.MethodPut, path: "/api/v1/accounts/acc-1/settings", wantAllow: false},
		{name: "item-read", method: http.MethodGet, path: "/api/v1/items/acc-1/item-1", wantScope: apikey.ScopeItemsRead, wantAllow: true},
		{name: "item-read-rejects-empty-account-segment", method: http.MethodGet, path: "/api/v1/items//item-1", wantAllow: false},
		{name: "item-read-rejects-trailing-slash", method: http.MethodGet, path: "/api/v1/items/acc-1/item-1/", wantAllow: false},
		{name: "account-read-rejects-empty-account-id", method: http.MethodGet, path: "/api/v1/accounts/", wantAllow: false},
		{name: "item-ai-settings-read", method: http.MethodGet, path: "/api/v1/items/acc-1/item-1/ai-settings", wantScope: apikey.ScopeRepliesManage, wantAllow: true},
		{name: "item-ai-settings-write", method: http.MethodPut, path: "/api/v1/items/acc-1/item-1/ai-settings", wantScope: apikey.ScopeRepliesManage, wantAllow: true},
		{name: "item-publish-batch-read", method: http.MethodGet, path: "/api/v1/items/publish-batches/batch-1", wantScope: apikey.ScopeItemsRead, wantAllow: true},
		{name: "item-publish-batch-preview", method: http.MethodPost, path: "/api/v1/items/publish-batches/preview", wantScope: apikey.ScopeItemsPublish, wantAllow: true},
		{name: "nested-batch-cancel-is-not-allowlisted", method: http.MethodPost, path: "/api/v1/items/publish-batches/a/b/cancel", wantAllow: false},
		{name: "item-platform-unpublish", method: http.MethodPost, path: "/api/v1/items/acc-1/item-1/unpublish", wantScope: apikey.ScopeItemsUnpublish, wantAllow: true},
		{name: "local-delete-is-not-platform-unpublish", method: http.MethodDelete, path: "/api/v1/items/acc-1/item-1", wantAllow: false},
		{name: "chat-message-read", method: http.MethodGet, path: "/api/v1/chat/messages", wantScope: apikey.ScopeChatRead, wantAllow: true},
		{name: "chat-websocket-is-not-exposed-to-bearer-tokens", method: http.MethodGet, path: "/api/v1/chat/ws", wantAllow: false},
		{name: "chat-image-send-is-not-in-text-send-scope", method: http.MethodPost, path: "/api/v1/chat/images", wantAllow: false},
		{name: "chat-message-send", method: http.MethodPost, path: "/api/v1/chat/messages", wantScope: apikey.ScopeChatSend, wantAllow: true},
		{name: "orders-list-read", method: http.MethodGet, path: "/api/v1/orders", wantScope: apikey.ScopeOrdersRead, wantAllow: true},
		{name: "order-detail-read", method: http.MethodGet, path: "/api/v1/orders/order-1", wantScope: apikey.ScopeOrdersRead, wantAllow: true},
		{name: "delivered-card-secret-is-not-exposed", method: http.MethodGet, path: "/api/v1/orders/order-1/delivered-cards", wantAllow: false},
		{name: "manual-takeover-only-route", method: http.MethodPut, path: "/api/v1/chat/manual-takeover/acc-1", wantScope: apikey.ScopeChatTakeover, wantAllow: true},
		{name: "general-account-settings-not-chat-takeover", method: http.MethodPut, path: "/api/v1/accounts/acc-1/settings", wantAllow: false},
		{name: "reply-keyword-update", method: http.MethodPut, path: "/api/v1/reply-rules/acc-1/typed/7", wantScope: apikey.ScopeRepliesManage, wantAllow: true},
		{name: "reply-keyword-list", method: http.MethodGet, path: "/api/v1/reply-rules/acc-1/typed", wantScope: apikey.ScopeRepliesManage, wantAllow: true},
		{name: "reply-keyword-list-rejects-empty-account-segment", method: http.MethodGet, path: "/api/v1/reply-rules//typed", wantAllow: false},
		{name: "unknown-reply-subresource-denied", method: http.MethodGet, path: "/api/v1/reply-rules/acc-1/secret", wantAllow: false},
		{name: "agent-reply-test", method: http.MethodPost, path: "/api/v1/agent-replies/acc-1/test", wantScope: apikey.ScopeRepliesManage, wantAllow: true},
		{name: "automation-audit-read", method: http.MethodGet, path: "/api/v1/automation-audit-events", wantScope: apikey.ScopeAutomationRead, wantAllow: true},
		{name: "account-task-schedule-read", method: http.MethodGet, path: "/api/v1/account-tasks/acc-1", wantScope: apikey.ScopeAutomationRead, wantAllow: true},
		{name: "account-task-runs-read", method: http.MethodGet, path: "/api/v1/account-tasks/acc-1/runs", wantScope: apikey.ScopeAutomationRead, wantAllow: true},
		{name: "unknown-account-task-subresource-denied", method: http.MethodGet, path: "/api/v1/account-tasks/acc-1/secret", wantAllow: false},
		{name: "account-task-schedule-update", method: http.MethodPut, path: "/api/v1/account-tasks/acc-1", wantScope: apikey.ScopeAutomationManage, wantAllow: true},
		{name: "system-settings-never-exposed", method: http.MethodGet, path: "/api/v1/settings/system", wantAllow: false},
		{name: "unknown-method-on-agent-route", method: http.MethodDelete, path: "/api/v1/agent-replies/acc-1", wantAllow: false},
	}
	// testCase 是当前需要验证的 HTTP 方法、路径和预期最小权限组合。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// scope、allowed 是当前 HTTP 方法和路径的最小权限映射结果。
			scope, allowed := requiredAPIKeyScope(testCase.method, testCase.path)
			if allowed != testCase.wantAllow || scope != testCase.wantScope {
				t.Fatalf("requiredAPIKeyScope(%s %s) = (%q, %v), want (%q, %v)", testCase.method, testCase.path, scope, allowed, testCase.wantScope, testCase.wantAllow)
			}
		})
	}
}
