package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// frontendDTOContractSpec 描述共享前端传输 DTO 与 Server 具名响应 DTO 的字段契约。
type frontendDTOContractSpec struct {
	// frontendPath 是 DTO 所在的前端源码相对路径；为空时使用共享 transport 文件。
	frontendPath string
	// frontendType 是 frontend/shared/api-contract/transport.ts 中的导出接口名称。
	frontendType string
	// backendType 是 internal/server 中实际写入 JSON 的具名响应类型名称；为空表示仅由 feature 归一的组合类型。
	backendType string
	// rationale 说明没有一一对应 Server DTO 的组合类型为何仍允许存在。
	rationale string
}

// frontendDTOContractSpecs 覆盖共享 transport 文件全部导出的对象 DTO。直接 HTTP DTO 必须指定 Server DTO；
// 只有 feature 对多个响应、动态设置或嵌套行 DTO 归一后产生的类型可以不指定后端类型。
var frontendDTOContractSpecs = []frontendDTOContractSpec{
	{frontendPath: "frontend/app/features/session/api.ts", frontendType: "SessionStatusResponse", backendType: "sessionVerificationResponse"},
	{frontendPath: "frontend/app/features/accounts/api.ts", frontendType: "AccountRuntimeStatus", rationale: "运行状态接口返回按账号 ID 索引的应用层状态映射，feature 只定义映射值形状。"},
	{frontendPath: "frontend/app/features/accounts/api.ts", frontendType: "LongLoginSettings", backendType: "longLoginResponse"},
	{frontendPath: "frontend/app/features/accounts/api.ts", frontendType: "PasswordLoginStartResponse", rationale: "密码登录兼容接口已永久禁用，实际响应使用统一错误信封；该类型仅保留历史调用编译兼容。"},
	{frontendPath: "frontend/app/features/accounts/api.ts", frontendType: "PasswordLoginStatusResponse", rationale: "密码登录兼容接口已永久禁用，实际响应使用统一错误信封；该类型仅保留历史调用编译兼容。"},
	{frontendPath: "frontend/app/features/chat/api.ts", frontendType: "ChatSessionPage", backendType: "chatSessionPageResponse"},
	{frontendPath: "frontend/app/features/chat/api.ts", frontendType: "ChatMessagePage", backendType: "chatMessagePageResponse"},
	{frontendPath: "frontend/app/features/dashboard/api.ts", frontendType: "ValidOrdersResult", rationale: "dashboard adapter 从 ValidOrdersResponse 归一订单状态并派生 orders/truncated。"},
	{frontendPath: "frontend/app/features/rules/api.ts", frontendType: "AutomationRunIssue", backendType: "automationRunIssueDTO"},
	{frontendPath: "frontend/app/features/rules/api.ts", frontendType: "DeferredAutomationIssue", backendType: "deferredAutomationIssueDTO"},
	{frontendPath: "frontend/app/features/settings/api.ts", frontendType: "SettingsSessionStatusResponse", backendType: "sessionVerificationResponse"},
	{frontendPath: "frontend/app/features/system/types.ts", frontendType: "BuildInfo", backendType: "healthResponse"},
	{frontendType: "PublishLocation", rationale: "发货地由 items feature 的高德地图适配器获取，不经过本服务 HTTP Server。"},
	{frontendType: "AccountDetail", rationale: "账号页面 UI 模型由 AccountSummaryResponse 和运行状态索引归一，包含前端派生状态而非单一 Server 响应。"},
	{frontendType: "AccountTaskSettings", backendType: "accountTaskSettingsResponse"},
	{frontendType: "AccountTaskSummary", backendType: "accountTaskSummaryResponse"},
	{frontendType: "ChatSession", backendType: "chatSessionDTO"},
	{frontendType: "ChatMessage", backendType: "chatMessageDTO"},
	{frontendType: "Order", rationale: "订单列表 feature 由 orderDTO 归一，补充本地 id 并将数量和状态转换为 UI 值。"},
	{frontendType: "Card", backendType: "cardResponse"},
	{frontendType: "Item", backendType: "itemListResponse"},
	{frontendType: "ShippingRule", rationale: "规则编辑器从 AutomationRuleResponse 解析动作配置并派生 card_group 与 variants。"},
	{frontendType: "AutomationAction", backendType: "automationActionResponse"},
	{frontendType: "ShippingVariant", rationale: "规格变体由自动化动作的 config_json 解码后产生，不是独立 HTTP DTO。"},
	{frontendType: "ReplyRule", rationale: "关键词接口的 KeywordTypedResponse 在 feature adapter 中转换为回复规则 UI 模型。"},
	{frontendType: "AdminStats", backendType: "adminStatsResponse"},
	{frontendType: "DashboardStats", backendType: "dashboardStatsResponse"},
	{frontendType: "OrderAnalytics", backendType: "orderAnalyticsResponse"},
	{frontendType: "SystemSettings", rationale: "设置接口保留历史动态键形状，且 adapter 将敏感值归一为已配置状态，不能静态映射到单一结构体。"},
	{frontendType: "AIReplySettings", backendType: "aiReplySettingsResponse"},
	{frontendType: "DefaultReply", backendType: "defaultReplyResponse"},
	{frontendType: "NotificationChannel", rationale: "通知编辑器 UI 模型不会接收渠道秘密配置；adapter 为新建/编辑表单构造空配置。"},
	{frontendType: "SessionResponse", backendType: "loginResponse"},
	{frontendType: "PaginatedResponse", rationale: "分页泛型由订单和自动化规则等不同 Server 响应复用，feature adapter 负责按端点归一。"},
	{frontendType: "ApiErrorResponse", rationale: "统一错误信封由 internal/httpapi 写入，不属于 internal/server 的成功响应 DTO。"},
	{frontendType: "AccountSummaryResponse", backendType: "cookieSummaryResponse"},
	{frontendType: "CookieSettingsResponse", backendType: "cookieSettingsResponse"},
	{frontendType: "CookieProfileResponse", backendType: "cookieProfileResponse"},
	{frontendType: "PauseDurationResponse", backendType: "pauseDurationResponse"},
	{frontendType: "ItemDetailResponse", backendType: "itemDetailResponse"},
	{frontendType: "ItemPublishResponse", backendType: "itemPublishResponse"},
	{frontendType: "ItemSyncResponse", backendType: "itemSyncResponse"},
	{frontendType: "ItemPageSyncResponse", backendType: "itemPageSyncResponse"},
	{frontendType: "OrderDTOResponse", backendType: "orderDTO"},
	{frontendType: "OrderDetailResponse", backendType: "orderDetailResponse"},
	{frontendType: "OrderRefreshDetailResponse", backendType: "orderRefreshDetailResponse"},
	{frontendType: "OrderSingleRefreshResponse", backendType: "orderSingleRefreshResponse"},
	{frontendType: "AutomationActionResponse", backendType: "automationActionResponse"},
	{frontendType: "AutomationRuleResponse", backendType: "automationRuleResponse"},
	{frontendType: "AutomationRulePageResponse", backendType: "automationRulePageResponse"},
	{frontendType: "AIReplySettingsResponse", backendType: "aiReplySettingsResponse"},
	{frontendType: "AIModelsResponse", backendType: "aiModelsResponse"},
	{frontendType: "UserSettingResponse", backendType: "userSettingResponse"},
	{frontendType: "CardBatchResponse", backendType: "cardBatchResponse"},
	{frontendType: "CardBatchResult", backendType: "cardBatchResultRow"},
	{frontendType: "CardAppendResponse", backendType: "cardAppendResponse"},
	{frontendType: "NotificationBinding", rationale: "消息通知接口以账号 ID 为外层动态键，adapter 展平列表时将该键补为 cookie_id。"},
	{frontendType: "AccountBindingsResponse", backendType: "accountBindingsResponse"},
	{frontendType: "CategoryRecommendationResponse", backendType: "categoryRecommendationResponse"},
	{frontendType: "ItemPublishBatchPreviewRow", backendType: "publishBatchPreviewRow"},
	{frontendType: "ItemPublishBatchPreviewResponse", backendType: "itemPublishBatchPreviewResponse"},
	{frontendType: "BatchIDResponse", backendType: "batchIDResponse"},
	{frontendType: "BatchCancelResponse", backendType: "batchCancelResponse"},
	{frontendType: "ItemPublishBatchRowResponse", backendType: "itemPublishBatchRowResponse"},
	{frontendType: "ItemPublishBatchResponse", backendType: "itemPublishBatchResponse"},
	{frontendType: "ItemPublishBatchListResponse", backendType: "itemPublishBatchListResponse"},
	{frontendType: "MutationIDResponse", backendType: "mutationIDResponse"},
	{frontendType: "OperationResponse", backendType: "operationResponse"},
	{frontendType: "NotificationChannelResponse", backendType: "notificationChannelResponse"},
	{frontendType: "KeywordBasicResponse", backendType: "keywordBasicResponse"},
	{frontendType: "KeywordItemResponse", backendType: "keywordItemResponse"},
	{frontendType: "KeywordTypedResponse", backendType: "keywordTypedResponse"},
	{frontendType: "ItemReplyResponse", backendType: "itemReplyResponse"},
	{frontendType: "DefaultReplyResponse", backendType: "defaultReplyResponse"},
	{frontendType: "AccountTaskSettingsResponse", backendType: "accountTaskSettingsResponse"},
	{frontendType: "AccountTaskRunResponse", backendType: "accountTaskRunResponse"},
	{frontendType: "AccountTaskRunsResponse", backendType: "accountTaskRunsResponse"},
	{frontendType: "AccountTaskSummaryResponse", backendType: "accountTaskSummaryResponse"},
	{frontendType: "AccountTaskRunResponseEnvelope", backendType: "accountTaskRunResponseEnvelope"},
	{frontendType: "AdminUserResponse", backendType: "adminUserResponse"},
	{frontendType: "AdminCookieResponse", backendType: "adminCookieResponse"},
	{frontendType: "AdminStatsResponse", backendType: "adminStatsResponse"},
	{frontendType: "DashboardStatsResponse", backendType: "dashboardStatsResponse"},
	{frontendType: "AnalyticsRevenueStatsResponse", backendType: "analyticsRevenueStatsResponse"},
	{frontendType: "AnalyticsDailyStatsResponse", backendType: "analyticsDailyStatsResponse"},
	{frontendType: "AnalyticsStatusStatsResponse", backendType: "analyticsStatusStatsResponse"},
	{frontendType: "AnalyticsCityStatsResponse", backendType: "analyticsCityStatsResponse"},
	{frontendType: "AnalyticsItemStatsResponse", backendType: "analyticsItemStatsResponse"},
	{frontendType: "OrderAnalyticsResponse", backendType: "orderAnalyticsResponse"},
	{frontendType: "ValidOrderResponse", backendType: "validOrderResponse"},
	{frontendType: "ValidOrdersResponse", backendType: "validOrdersResponse"},
	{frontendType: "QRLoginGenerateResponse", backendType: "qrLoginGenerateResponse"},
	{frontendType: "QRLoginStatusResponse", backendType: "qrLoginStatusResponse"},
	{frontendType: "QRLoginVerificationResponse", backendType: "qrLoginVerificationResponse"},
	{frontendType: "OrderRefreshResultResponse", backendType: "orderRefreshResultDTO"},
	{frontendType: "OrderRefreshSummaryResponse", backendType: "orderRefreshSummary"},
	{frontendType: "OrderRefreshResponse", backendType: "orderRefreshResponse"},
	{frontendType: "OrderRefreshJobStartResponse", backendType: "orderRefreshJobStartResponse"},
	{frontendType: "OrderRefreshJobStatusResponse", backendType: "orderRefreshJobStatusResponse"},
	{frontendType: "OrderRefreshJobCancelResponse", backendType: "orderRefreshJobCancelResponse"},
	{frontendType: "CardListResponse", rationale: "卡券接口直接返回 cardResponse 数组，前端仅用此类型表达历史包装。"},
	{frontendType: "OrderBatchResponse", rationale: "导入与发货端点使用不同 Server 响应 DTO，feature 将其归一为同一批量结果。"},
	{frontendType: "OrderBatchResult", rationale: "批量发货与导入的逐行 DTO 不同，feature 只保留共同展示字段。"},
	{frontendType: "ItemListEnvelope", rationale: "商品列表兼容端点同时支持数组和历史包装，feature adapter 负责归一。"},
	{frontendType: "AutomationIssuesEnvelope", rationale: "自动化问题响应由两个 Server 行 DTO 组合，feature adapter 不直接透传。"},
}

// checkFrontendDTOFieldContracts 验证前端共享对象 DTO 已登记，且直接 HTTP DTO 的每个字段都由 Server 显式提供。
func checkFrontendDTOFieldContracts(root string) []violation {
	// frontendPath 是共享前端传输契约的唯一源文件。
	frontendPath := filepath.Join(root, "frontend", "shared", "api-contract", "transport.ts")
	// frontendRaw、frontendReadErr 分别是前端契约源码及其读取错误。
	frontendRaw, frontendReadErr := os.ReadFile(frontendPath)
	if frontendReadErr != nil {
		return []violation{{file: "frontend/shared/api-contract/transport.ts", line: 1, message: fmt.Sprintf("无法读取前端 DTO 契约: %v", frontendReadErr)}}
	}
	// frontendFields、frontendLines 分别保存 TypeScript 对象 DTO 字段和其声明行。
	frontendFields, frontendLines := parseFrontendResponseFields(string(frontendRaw))
	// frontendPaths 保存每个已解析 DTO 对应的实际源码路径。
	frontendPaths := make(map[string]string, len(frontendFields))
	// typeName 是共享契约中的当前 DTO 名称。
	for typeName := range frontendFields {
		frontendPaths[typeName] = "frontend/shared/api-contract/transport.ts"
	}
	// featureSources 保存全部 feature API DTO 源码及注册表中的额外 DTO 源码。
	featureSources := make(map[string]struct{})
	// featureAPIFiles 是所有 feature API 适配器路径；新增响应 DTO 必须自动进入门禁。
	featureAPIFiles, featureGlobErr := filepath.Glob(filepath.Join(root, "frontend", "app", "features", "*", "api.ts"))
	if featureGlobErr != nil {
		return []violation{{file: "frontend/app/features", line: 1, message: fmt.Sprintf("无法枚举 feature DTO 契约: %v", featureGlobErr)}}
	}
	// featureAPIFile 是当前需要纳入扫描的 feature API 文件绝对路径。
	for _, featureAPIFile := range featureAPIFiles {
		// relativePath 是当前 feature API 文件相对于仓库根的稳定路径。
		relativePath, relativeErr := filepath.Rel(root, featureAPIFile)
		if relativeErr != nil {
			return []violation{{file: "frontend/app/features", line: 1, message: fmt.Sprintf("无法规范 feature DTO 路径: %v", relativeErr)}}
		}
		featureSources[filepath.ToSlash(relativePath)] = struct{}{}
	}
	// spec 是当前可能额外声明 feature DTO 源码的契约登记项。
	for _, spec := range frontendDTOContractSpecs {
		if spec.frontendPath != "" {
			featureSources[spec.frontendPath] = struct{}{}
		}
	}
	// featureSource 是当前需要读取的局部 feature DTO 源码路径。
	for featureSource := range featureSources {
		// featureRaw、featureReadErr 分别是局部 DTO 源码和读取错误。
		featureRaw, featureReadErr := os.ReadFile(filepath.Join(root, featureSource))
		if featureReadErr != nil {
			return []violation{{file: featureSource, line: 1, message: fmt.Sprintf("无法读取 feature DTO 契约: %v", featureReadErr)}}
		}
		// featureFields、featureLines 分别保存局部 DTO 字段和声明行号。
		featureFields, featureLines := parseFrontendResponseFields(string(featureRaw))
		// typeName、fields 分别是当前局部 DTO 名称和其顶层字段集合。
		for typeName, fields := range featureFields {
			if !isFeatureResponseDTOName(typeName) && !isExplicitFrontendDTO(featureSource, typeName) {
				continue
			}
			frontendFields[typeName] = fields
			frontendLines[typeName] = featureLines[typeName]
			frontendPaths[typeName] = featureSource
		}
	}
	// backendFields、backendLines 分别保存 Server 具名结构体的 JSON 字段和其声明行。
	backendFields, backendLines, backendErr := parseServerResponseFields(root)
	if backendErr != nil {
		return []violation{{file: "internal/server", line: 1, message: fmt.Sprintf("无法解析 Server DTO: %v", backendErr)}}
	}
	// registered 保存已在注册表登记的前端响应类型，避免新增 DTO 漏检。
	registered := make(map[string]frontendDTOContractSpec, len(frontendDTOContractSpecs))
	// violations 保存字段遗漏、未知 DTO 和无效登记。
	var violations []violation
	// spec 是当前前后端 DTO 对照规则。
	for _, spec := range frontendDTOContractSpecs {
		// duplicated 表示相同前端 DTO 是否已经登记过。
		if _, duplicated := registered[spec.frontendType]; duplicated {
			violations = append(violations, violation{file: "tools/architecturecheck/dto_field_contract.go", line: 1, message: fmt.Sprintf("前端 DTO %s 在字段契约注册表中重复登记", spec.frontendType)})
			continue
		}
		registered[spec.frontendType] = spec
		// frontendTypeFields、exists 分别是当前前端 DTO 的字段集合和是否存在。
		frontendTypeFields, exists := frontendFields[spec.frontendType]
		if !exists {
			violations = append(violations, violation{file: "tools/architecturecheck/dto_field_contract.go", line: 1, message: fmt.Sprintf("字段契约登记的前端 DTO %s 不存在", spec.frontendType)})
			continue
		}
		if spec.backendType == "" {
			if strings.TrimSpace(spec.rationale) == "" {
				violations = append(violations, violation{file: "tools/architecturecheck/dto_field_contract.go", line: 1, message: fmt.Sprintf("组合 DTO %s 必须说明不直接对应 Server DTO 的原因", spec.frontendType)})
			}
			continue
		}
		// backendTypeFields、backendExists 分别是 Server DTO 的 JSON 字段集合和是否存在。
		backendTypeFields, backendExists := backendFields[spec.backendType]
		if !backendExists {
			violations = append(violations, violation{file: "tools/architecturecheck/dto_field_contract.go", line: 1, message: fmt.Sprintf("前端 DTO %s 对应的 Server DTO %s 不存在", spec.frontendType, spec.backendType)})
			continue
		}
		// field 是前端声明为可消费的当前字段。
		for field := range frontendTypeFields {
			// provided 表示后端具名 DTO 是否显式提供当前前端字段。
			if _, provided := backendTypeFields[field]; provided {
				continue
			}
			violations = append(violations, violation{file: frontendPaths[spec.frontendType], line: frontendLines[spec.frontendType], message: fmt.Sprintf("前端 DTO %s 需要字段 %q，但 Server DTO %s 未提供；请补齐具名响应字段或移除未使用契约", spec.frontendType, field, spec.backendType)})
		}
		_ = backendLines
	}
	// frontendType 是当前需要强制登记的共享或 feature 响应 DTO。
	for frontendType := range parseTransportDTOTypeNames(string(frontendRaw)) {
		// exists 表示共享 DTO 是否已有字段契约登记。
		if _, exists := registered[frontendType]; exists {
			continue
		}
		violations = append(violations, violation{file: "frontend/shared/api-contract/transport.ts", line: frontendLines[frontendType], message: fmt.Sprintf("前端对象 DTO %s 未登记到字段契约门禁", frontendType)})
	}
	// frontendType、frontendPath 分别是局部响应 DTO 名称和其声明文件。
	for frontendType, frontendPath := range frontendPaths {
		if frontendPath == "frontend/shared/api-contract/transport.ts" {
			continue
		}
		// exists 表示局部响应 DTO 是否已有字段契约登记。
		if _, exists := registered[frontendType]; exists {
			continue
		}
		violations = append(violations, violation{file: frontendPath, line: frontendLines[frontendType], message: fmt.Sprintf("前端对象 DTO %s 未登记到字段契约门禁", frontendType)})
	}
	return violations
}

// isFeatureResponseDTOName 判断 feature 局部接口是否表达 HTTP 响应或其响应行 DTO。
func isFeatureResponseDTOName(typeName string) bool {
	// suffix 是当前接口名称可证明为响应 DTO 的后缀。
	for _, suffix := range []string{"Response", "Page", "Status", "Settings", "Result", "Issue", "RuntimeStatus"} {
		if strings.HasSuffix(typeName, suffix) {
			return true
		}
	}
	return false
}

// isExplicitFrontendDTO 判断当前路径和类型是否由登记表显式纳入，例如健康检查 BuildInfo。
func isExplicitFrontendDTO(frontendPath, typeName string) bool {
	// spec 是当前字段契约登记项。
	for _, spec := range frontendDTOContractSpecs {
		if spec.frontendPath == frontendPath && spec.frontendType == typeName {
			return true
		}
	}
	return false
}

// parseTransportDTOTypeNames 返回共享 transport 中需要强制登记的全部导出对象 DTO 名称。
func parseTransportDTOTypeNames(source string) map[string]struct{} {
	// fields 是 transport DTO 的解析结果；只需其类型名称。
	fields, _ := parseFrontendResponseFields(source)
	// names 保存所有待检查的共享 DTO 名称。
	names := make(map[string]struct{}, len(fields))
	// typeName 是当前共享 DTO 名称。
	for typeName := range fields {
		names[typeName] = struct{}{}
	}
	return names
}

// parseFrontendResponseFields 从共享 TypeScript transport 文件读取全部导出对象 DTO 的顶层字段和 extends 字段。
func parseFrontendResponseFields(source string) (map[string]map[string]struct{}, map[string]int) {
	// declarationPattern 匹配导出的对象接口声明；字符串和联合类型不是可逐字段校验的 DTO。
	declarationPattern := regexp.MustCompile(`(?m)^export interface ([A-Za-z][A-Za-z0-9]*)[^\{]*\{`)
	// extendsPattern 匹配接口继承的单个对象 DTO；当前契约不使用交叉继承。
	extendsPattern := regexp.MustCompile(`\bextends\s+([A-Za-z][A-Za-z0-9]*)`)
	// fieldPattern 匹配接口体顶层的普通属性；索引签名不表示固定 HTTP 字段。
	fieldPattern := regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\??\s*:`)
	// fields 保存每个响应接口声明的固定字段集合。
	fields := make(map[string]map[string]struct{})
	// lines 保存接口声明的 1-based 行号，供失败信息定位。
	lines := make(map[string]int)
	// parents 保存接口的直接父 DTO，供后续合并继承字段。
	parents := make(map[string]string)
	// matches 保存所有响应接口声明的位置和类型名称。
	matches := declarationPattern.FindAllStringSubmatchIndex(source, -1)
	// match 是当前响应接口声明的位置元组。
	for _, match := range matches {
		// typeName 是当前响应接口名称。
		typeName := source[match[2]:match[3]]
		// bodyStart 是声明中第一个左花括号的下标。
		bodyStart := strings.Index(source[match[0]:match[1]], "{") + match[0]
		// bodyEnd 是配对右花括号的下标；异常源码由 TypeScript 类型检查另行报告。
		bodyEnd := matchingBrace(source, bodyStart)
		if bodyEnd < 0 {
			continue
		}
		// body 是不含外层花括号的接口字段文本。
		body := source[bodyStart+1 : bodyEnd]
		// typeFields 保存当前接口的顶层字段，嵌套对象字段不属于当前 Server DTO 的直接字段。
		typeFields := topLevelFrontendFields(body, fieldPattern)
		fields[typeName] = typeFields
		lines[typeName] = strings.Count(source[:match[0]], "\n") + 1
		// parentMatch 是当前接口继承的父 DTO 名称。
		if parentMatch := extendsPattern.FindStringSubmatch(source[match[0]:match[1]]); len(parentMatch) == 2 {
			parents[typeName] = parentMatch[1]
		}
	}
	// resolved 保存已展开继承字段的 DTO 集合。
	resolved := make(map[string]map[string]struct{}, len(fields))
	// resolving 保存当前 DFS 路径，防止异常源码中的循环继承无限递归。
	resolving := make(map[string]bool, len(fields))
	// resolve 返回当前 DTO 自身及所有父 DTO 的字段集合。
	var resolve func(string) map[string]struct{}
	resolve = func(typeName string) map[string]struct{} {
		// resolvedFields、ok 分别是已缓存的继承字段和其命中标记。
		if resolvedFields, ok := resolved[typeName]; ok {
			return resolvedFields
		}
		if resolving[typeName] {
			return fields[typeName]
		}
		resolving[typeName] = true
		// merged 是当前 DTO 合并父级后的独立字段集合。
		merged := make(map[string]struct{}, len(fields[typeName]))
		// field 是当前 DTO 自身声明的字段。
		for field := range fields[typeName] {
			merged[field] = struct{}{}
		}
		// parentType、hasParent 分别是当前 DTO 的父接口名称和继承标记。
		if parentType, hasParent := parents[typeName]; hasParent {
			// field 是从父接口继承的字段。
			for field := range resolve(parentType) {
				merged[field] = struct{}{}
			}
		}
		delete(resolving, typeName)
		resolved[typeName] = merged
		return merged
	}
	// typeName 是当前需要展开继承字段的 DTO 名称。
	for typeName := range fields {
		fields[typeName] = resolve(typeName)
	}
	return fields, lines
}

// topLevelFrontendFields 只提取 TypeScript 接口第一层字段，跳过嵌套对象和数组元素的属性。
func topLevelFrontendFields(body string, fieldPattern *regexp.Regexp) map[string]struct{} {
	// fields 保存当前接口顶层可由 HTTP 响应直接提供的字段。
	fields := make(map[string]struct{})
	// depth 保存当前行开始时所处的嵌套花括号层级。
	depth := 0
	// line 是当前待检查的源码行。
	for _, line := range strings.Split(body, "\n") {
		if depth == 0 {
			// match 是当前顶层属性名的正则匹配位置。
			if match := fieldPattern.FindStringSubmatch(line); len(match) == 2 {
				fields[match[1]] = struct{}{}
			}
		}
		// character 是当前行的单个字符；嵌套字段将在下一行因 depth 非零被跳过。
		for _, character := range line {
			switch character {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
	}
	return fields
}

// matchingBrace 返回 source 中指定左花括号对应右花括号的位置，不把对象字面量嵌套误认为接口结束。
func matchingBrace(source string, start int) int {
	// depth 保存当前花括号嵌套层级。
	depth := 0
	// index 是当前扫描的字节位置。
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

// parseServerResponseFields 收集 internal/server 生产源码中全部具名结构体的 JSON 字段。
func parseServerResponseFields(root string) (map[string]map[string]struct{}, map[string]int, error) {
	// fields 保存每个 Server 结构体暴露的 JSON 字段集合。
	fields := make(map[string]map[string]struct{})
	// lines 保存结构体声明所在行，供后续诊断扩展使用。
	lines := make(map[string]int)
	// embedded 保存结构体匿名嵌入的 DTO 类型，编码时会提升这些字段。
	embedded := make(map[string][]string)
	// fset 为 Go AST 节点提供稳定行号。
	fset := token.NewFileSet()
	// serverRoot 是 HTTP transport 源码目录。
	serverRoot := filepath.Join(root, "internal", "server")
	// walkErr 表示遍历或解析 Server DTO 时发生的错误。
	walkErr := filepath.WalkDir(serverRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// syntax、parseErr 分别是当前 Server 源码的 AST 与解析错误。
		syntax, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		// declaration 是当前文件的顶层声明。
		for _, declaration := range syntax.Decls {
			// general、ok 分别是类型声明块及其断言结果。
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			// specification 是类型声明块中的单个类型。
			for _, specification := range general.Specs {
				// typeSpec、ok 分别是具名类型规格及其断言结果。
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				// structure、ok 分别是具名结构体及其断言结果。
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				// jsonFields 保存当前结构体可编码的固定 JSON 字段。
				jsonFields := make(map[string]struct{})
				// field 是当前结构体字段声明。
				for _, field := range structure.Fields.List {
					if len(field.Names) == 0 {
						// embeddedType 是匿名嵌入结构体的类型名；指针嵌入与值嵌入都会提升 JSON 字段。
						embeddedType := embeddedStructTypeName(field.Type)
						if embeddedType != "" {
							embedded[typeSpec.Name.Name] = append(embedded[typeSpec.Name.Name], embeddedType)
						}
						continue
					}
					if field.Tag == nil {
						continue
					}
					// tag 是去除反引号后的结构体标签文本。
					tag := strings.Trim(field.Tag.Value, "`")
					// jsonName 是 json 标签的字段名；空名和 - 不属于响应字段。
					jsonName := strings.Split(strings.TrimPrefix(tag, "json:\""), "\"")[0]
					jsonName = strings.Split(jsonName, ",")[0]
					if jsonName != "" && jsonName != "-" {
						jsonFields[jsonName] = struct{}{}
					}
				}
				fields[typeSpec.Name.Name] = jsonFields
				lines[typeSpec.Name.Name] = fset.Position(typeSpec.Pos()).Line
			}
		}
		return nil
	})
	if walkErr != nil {
		return fields, lines, walkErr
	}
	// resolved 保存已经展开匿名嵌入字段的 Server DTO 集合。
	resolved := make(map[string]map[string]struct{}, len(fields))
	// resolving 保存当前 DFS 路径，避免异常源码出现循环嵌入时无限递归。
	resolving := make(map[string]bool, len(fields))
	// resolve 返回当前 DTO 自身及所有匿名嵌入 DTO 的 JSON 字段。
	var resolve func(string) map[string]struct{}
	resolve = func(typeName string) map[string]struct{} {
		// resolvedFields、ok 分别是已缓存的匿名嵌入字段和其命中标记。
		if resolvedFields, ok := resolved[typeName]; ok {
			return resolvedFields
		}
		if resolving[typeName] {
			return fields[typeName]
		}
		resolving[typeName] = true
		// merged 是当前 DTO 合并匿名嵌入字段后的独立集合。
		merged := make(map[string]struct{}, len(fields[typeName]))
		// field 是当前 Server DTO 自身声明的 JSON 字段。
		for field := range fields[typeName] {
			merged[field] = struct{}{}
		}
		// embeddedType 是当前 Server DTO 匿名嵌入的字段来源类型。
		for _, embeddedType := range embedded[typeName] {
			// field 是匿名嵌入类型提升到当前 DTO 的 JSON 字段。
			for field := range resolve(embeddedType) {
				merged[field] = struct{}{}
			}
		}
		delete(resolving, typeName)
		resolved[typeName] = merged
		return merged
	}
	// typeName 是当前需要展开匿名嵌入字段的 Server DTO 名称。
	for typeName := range fields {
		fields[typeName] = resolve(typeName)
	}
	return fields, lines, walkErr
}

// embeddedStructTypeName 返回匿名嵌入字段引用的本包结构体类型名；匿名基本类型不会提升 JSON DTO 字段。
func embeddedStructTypeName(expression ast.Expr) string {
	// identifier、ok 分别是匿名值嵌入的类型名及其断言结果。
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	// pointer、ok 分别是匿名指针嵌入及其断言结果。
	pointer, ok := expression.(*ast.StarExpr)
	if !ok {
		return ""
	}
	// identifier、ok 分别是指针指向的本包类型名及其断言结果。
	identifier, ok := pointer.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return identifier.Name
}

// sortedFieldNames 返回稳定排序的字段名列表，供测试失败输出保持可复现。
func sortedFieldNames(fields map[string]struct{}) []string {
	// names 保存待排序字段名。
	names := make([]string, 0, len(fields))
	// field 是当前字段集合成员。
	for field := range fields {
		names = append(names, field)
	}
	sort.Strings(names)
	return names
}
