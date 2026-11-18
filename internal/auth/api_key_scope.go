package auth

import (
	"net/http"
	"strings"

	"xianyu-go/internal/application/apikey"
)

// requiredAPIKeyScope 将请求方法和版本化路径映射为外部令牌所需的最小权限。
// 未显式列出的 API 对 Bearer Token 一律拒绝；浏览器 Cookie 会话不受此映射限制。
// 各业务域的路径前缀互不重叠，因此按域依次匹配与单一 switch 的先后顺序等价。
func requiredAPIKeyScope(method, path string) (string, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	// resolvers 是按业务域拆分的权限解析器，任一返回放行即采用其权限。
	resolvers := []func(method, path string) (string, bool){
		accountScope, itemScope, orderScope, chatScope, replyScope, automationScope,
	}
	// resolve 是当前业务域的权限解析器。
	for _, resolve := range resolvers {
		// scope、allowed 是该业务域对当前方法与路径的权限判定。
		if scope, allowed := resolve(method, path); allowed {
			return scope, true
		}
	}
	return "", false
}

// accountScope 解析账号只读接口：单账号详情不包含 Cookie 或登录密码；账号设置、凭据、状态写接口不对外部令牌开放。
func accountScope(method, path string) (string, bool) {
	if method == http.MethodGet && (path == "/api/v1/accounts" || path == "/api/v1/accounts/details" || path == "/api/v1/accounts/runtime-status" || exactPathSegments(path, "/api/v1/accounts/", 1)) {
		return apikey.ScopeAccountsRead, true
	}
	return "", false
}

// itemScope 解析商品读取、发布、平台下架与批次生命周期接口；本地 DELETE 不等于平台下架，保持拒绝。
func itemScope(method, path string) (string, bool) {
	// exactSegments 复用严格路径分段校验，拒绝空段、重复斜杠和尾随斜杠。
	exactSegments := func(prefix string, count int) bool { return exactPathSegments(path, prefix, count) }
	switch {
	case (method == http.MethodGet || method == http.MethodPut) && exactSegments("/api/v1/items/", 3) && strings.HasSuffix(path, "/ai-settings"):
		return apikey.ScopeRepliesManage, true
	case method == http.MethodGet && (path == "/api/v1/items" || path == "/api/v1/items/publish-batches" || exactSegments("/api/v1/items/publish-batches/", 1) || (exactSegments("/api/v1/items/publish-batches/", 2) && strings.HasSuffix(path, "/result.csv")) || exactSegments("/api/v1/items/", 2)):
		return apikey.ScopeItemsRead, true
	case method == http.MethodPost && (path == "/api/v1/items/get-all-from-account" || path == "/api/v1/items/get-by-page" || path == "/api/v1/items/publish-categories/recommend"):
		return apikey.ScopeItemsRead, true
	case method == http.MethodPost && exactSegments("/api/v1/items/", 3) && strings.HasSuffix(path, "/unpublish"):
		return apikey.ScopeItemsUnpublish, true
	case method == http.MethodPost && (path == "/api/v1/items/publish" || path == "/api/v1/items/publish-batches" || path == "/api/v1/items/publish-batches/preview"):
		return apikey.ScopeItemsPublish, true
	case method == http.MethodPost && exactSegments("/api/v1/items/publish-batches/", 2) && (strings.HasSuffix(path, "/cancel") || strings.HasSuffix(path, "/retry-failed")):
		return apikey.ScopeItemsPublish, true
	}
	return "", false
}

// orderScope 解析订单只读接口：只暴露订单集合和单条详情，不暴露发货卡密等其他方法。
func orderScope(method, path string) (string, bool) {
	if method == http.MethodGet && (path == "/api/v1/orders" || exactPathSegments(path, "/api/v1/orders/", 1)) {
		return apikey.ScopeOrdersRead, true
	}
	return "", false
}

// chatScope 解析聊天读取、发送与人工介入配置接口，三类能力分开授权；不开放 WebSocket 与图片发送。
func chatScope(method, path string) (string, bool) {
	switch {
	case method == http.MethodGet && (path == "/api/v1/chat/sessions" || path == "/api/v1/chat/messages" || path == "/api/v1/chat/items" || path == "/api/v1/chat/quick-replies" || exactPathSegments(path, "/api/v1/chat/buyer-notes/", 1)):
		return apikey.ScopeChatRead, true
	case method == http.MethodPost && path == "/api/v1/chat/messages":
		return apikey.ScopeChatSend, true
	case method == http.MethodPut && exactPathSegments(path, "/api/v1/chat/manual-takeover/", 1):
		return apikey.ScopeChatTakeover, true
	}
	return "", false
}

// replyScope 解析回复管理接口；只允许现有回复资源实际注册的方法，不扩展到账号凭据或全局系统设置。
func replyScope(method, path string) (string, bool) {
	if isReplyManagementRoute(method, path) {
		return apikey.ScopeRepliesManage, true
	}
	return "", false
}

// automationScope 解析自动化读取、规则管理和运行处理接口，按 HTTP 方法分别授权。
func automationScope(method, path string) (string, bool) {
	// exactSegments 复用严格路径分段校验，拒绝空段、重复斜杠和尾随斜杠。
	exactSegments := func(prefix string, count int) bool { return exactPathSegments(path, prefix, count) }
	switch {
	case method == http.MethodGet && (path == "/api/v1/automation-issues" || path == "/api/v1/automation-audit-events"):
		return apikey.ScopeAutomationRead, true
	case method == http.MethodGet && (path == "/api/v1/automation-rules" || path == "/api/v1/delivery-templates" || exactSegments("/api/v1/delivery-templates/", 1) || exactSegments("/api/v1/account-tasks/", 1) || (exactSegments("/api/v1/account-tasks/", 2) && strings.HasSuffix(path, "/runs"))):
		return apikey.ScopeAutomationRead, true
	case method == http.MethodPost && (path == "/api/v1/automation-rules" || path == "/api/v1/delivery-templates" || exactSegments("/api/v1/account-tasks/", 2) && strings.HasSuffix(path, "/run")):
		return apikey.ScopeAutomationManage, true
	case (method == http.MethodPut || method == http.MethodDelete) && (exactSegments("/api/v1/automation-rules/", 1) || exactSegments("/api/v1/delivery-templates/", 1)):
		return apikey.ScopeAutomationManage, true
	case method == http.MethodPut && exactSegments("/api/v1/account-tasks/", 1):
		return apikey.ScopeAutomationManage, true
	case method == http.MethodPost && (exactSegments("/api/v1/automation-runs/", 2) || exactSegments("/api/v1/automation-pending-tasks/", 2)) && strings.HasSuffix(path, "/resolve"):
		return apikey.ScopeAutomationManage, true
	}
	return "", false
}

// isReplyManagementRoute 仅匹配当前已注册的回复配置读写方法；按资源族拆分以控制复杂度。
func isReplyManagementRoute(method, path string) bool {
	return isReplyRuleRoute(method, path) || isDefaultReplyRoute(method, path) || isAgentOrAIReplyRoute(method, path)
}

// isReplyRuleRoute 匹配关键词、商品级与问候语回复规则路由的方法白名单。
func isReplyRuleRoute(method, path string) bool {
	switch {
	case path == "/api/v1/reply-rules/items" && method == http.MethodGet:
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/items/", 2) && (method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete):
		return true
	case path == "/api/v1/reply-rules/greetings" && (method == http.MethodGet || method == http.MethodPost):
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/greetings/", 1) && (method == http.MethodPut || method == http.MethodDelete):
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/greetings/clear-records/", 1) && method == http.MethodPost:
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/", 1) && (method == http.MethodGet || method == http.MethodPost):
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/", 2) && replyRuleTailMatches(path, "items", 2) && (method == http.MethodGet || method == http.MethodPost):
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/", 2) && replyRuleTailMatches(path, "typed", 2) && method == http.MethodGet:
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/", 3) && replyRuleTailMatches(path, "typed", 3) && (method == http.MethodPut || method == http.MethodDelete):
		return true
	case exactPathSegments(path, "/api/v1/reply-rules/", 3) && replyRuleTailMatches(path, "index", 3) && method == http.MethodDelete:
		return true
	}
	return false
}

// isDefaultReplyRoute 匹配默认回复路由的方法白名单。
func isDefaultReplyRoute(method, path string) bool {
	switch {
	case path == "/api/v1/default-replies" && method == http.MethodGet:
		return true
	case path == "/api/v1/default-replies/list" && method == http.MethodGet:
		return true
	case exactPathSegments(path, "/api/v1/default-replies/", 1) && (method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete):
		return true
	case exactPathSegments(path, "/api/v1/default-replies/", 2) && method == http.MethodPost && strings.HasSuffix(path, "/clear-records"):
		return true
	}
	return false
}

// isAgentOrAIReplyRoute 匹配外部 Agent 回复与 AI 回复设置路由的方法白名单。
func isAgentOrAIReplyRoute(method, path string) bool {
	switch {
	case exactPathSegments(path, "/api/v1/agent-replies/", 1) && (method == http.MethodGet || method == http.MethodPut):
		return true
	case exactPathSegments(path, "/api/v1/agent-replies/", 2) && method == http.MethodPost && strings.HasSuffix(path, "/test"):
		return true
	case path == "/api/v1/settings/ai-reply" && method == http.MethodGet:
		return true
	case exactPathSegments(path, "/api/v1/settings/ai-reply/", 1) && (method == http.MethodGet || method == http.MethodPut):
		return true
	}
	return false
}

// replyRuleTailMatches 判断账号级关键词路由的第二个资源段是否为指定动作名，并校验精确路径深度。
func replyRuleTailMatches(path, segment string, count int) bool {
	if !exactPathSegments(path, "/api/v1/reply-rules/", count) {
		return false
	}
	// parts 是账号级回复规则路径中去除固定前缀后的非空资源段集合。
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/reply-rules/"), "/")
	return len(parts) > 1 && parts[1] == segment
}

// exactPathSegments 判断路径在给定前缀后是否恰好有 count 个非空路径段。
func exactPathSegments(path, prefix string, count int) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// rest 是去除固定资源前缀后仍需进行精确分段校验的路径后缀。
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" || strings.Contains(rest, "//") {
		return false
	}
	// parts 是待验证的非空路径段集合。
	parts := strings.Split(rest, "/")
	if len(parts) != count {
		return false
	}
	// part 是当前需要确认不为空的资源路径段。
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}

// hasAPIKeyScope 判断认证令牌是否包含请求所需的精确权限值。
func hasAPIKeyScope(scopes []string, required string) bool {
	// scope 是当前令牌拥有的一项精确权限标识。
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}
