package main

import "regexp"

// tokenRule 描述“从源码按正则提取的每个令牌，都必须原样出现在指定文档中”的一致性规则。
// 它用来守护命令行参数、环境变量、模板变量、别名等“代码是事实来源、文档只能跟随”的枚举型事实。
type tokenRule struct {
	// name 是规则名称，出现在每条违规信息里，便于定位是哪一类事实漂移。
	name string
	// sources 是被提取的源码文件，路径相对仓库根目录，支持 filepath.Glob 通配符。
	sources []string
	// scope 是可选的范围正则：非空时只在其第一次匹配到的文本内提取令牌，用于把提取限定在某个函数体内。
	scope *regexp.Regexp
	// pattern 是提取令牌的正则，所有非空捕获组都作为令牌。
	pattern *regexp.Regexp
	// split 是可选的分隔符：非空时把每个令牌再按它拆成多个，用于 a|b|c 形式的正则分支。
	split string
	// docs 是必须包含每个令牌的文档文件名，相对 docs/wiki，中英文版本都应列出。
	docs []string
	// format 是把令牌转换为文档中应出现文本的格式串，必须含一个 %s；空串表示原样匹配。
	format string
	// prefixOnly 表示这些令牌只要求文档出现其“前缀（到第一个下划线为止，含下划线）”，用于 POSTGRES_* 这类文档中合并书写的变量。
	prefixOnly []string
	// exempt 是明确不要求出现在文档中的令牌，必须附带原因写在规则旁的注释里。
	exempt map[string]bool
}

// docKeyword 把一份文档与“数值所在行必须包含的关键词”绑定。
type docKeyword struct {
	// file 是文档文件名，相对 docs/wiki。
	file string
	// keyword 是数值所在行必须包含的关键词，比较时忽略大小写；用于避免数字在无关位置偶然出现。
	keyword string
}

// numberRule 描述“源码常量中的每个数值，都必须与关键词出现在指定文档的同一行”的规则。
type numberRule struct {
	// name 是规则名称，出现在违规信息里。
	name string
	// source 是被提取的源码文件，路径相对仓库根目录。
	source string
	// pattern 是提取数值的正则，其全部捕获组都按数值处理。
	pattern *regexp.Regexp
	// docs 是需要出现这些数值的文档及各自关键词。
	docs []docKeyword
}

// forbiddenRule 描述“某文档不得再出现已被证实错误的说法”的回归守卫。
type forbiddenRule struct {
	// file 是被守卫的文档文件名，相对 docs/wiki。
	file string
	// phrase 是不得出现的字面文本。
	phrase string
	// reason 说明该说法为何错误，输出在违规信息里帮助修正。
	reason string
}

// zhEnPair 是同一页面的中文与英文文档文件名。
type zhEnPair struct {
	// zh 是中文文档文件名，相对 docs/wiki。
	zh string
	// en 是英文文档文件名，相对 docs/wiki。
	en string
}

// tokenRules 是全部令牌一致性规则；新增“代码里的枚举必须写进文档”的约束时在这里追加。
var tokenRules = []tokenRule{
	{
		name:    "命令行参数必须出现在配置参考",
		sources: []string{"cmd/server/main.go"},
		pattern: regexp.MustCompile(`flag\.\w+(?:Var)?\((?:&opts\.\w+, )?"([a-z][a-z-]*)"`),
		docs:    []string{"配置参考.zh-CN.md", "Configuration-Reference.md"},
		format:  "`-%s`",
	},
	{
		name:       "Compose 变量必须出现在配置参考",
		sources:    []string{"compose.yml"},
		pattern:    regexp.MustCompile(`(?m)^\s+([A-Z][A-Z0-9_]+): \$\{`),
		docs:       []string{"配置参考.zh-CN.md", "Configuration-Reference.md"},
		prefixOnly: []string{"POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_IMAGE"},
	},
	{
		name:       ".env.example 变量必须出现在配置参考",
		sources:    []string{".env.example"},
		pattern:    regexp.MustCompile(`(?m)^#?([A-Z][A-Z0-9_]+)=`),
		docs:       []string{"配置参考.zh-CN.md", "Configuration-Reference.md"},
		prefixOnly: []string{"POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_IMAGE"},
	},
	{
		name:    "进程读取的 XIANYU_* 等环境变量必须出现在配置参考",
		sources: []string{"cmd/server/*.go", "internal/automation/*.go", "internal/server/*.go", "internal/composition/*.go", "internal/composition/*/*.go", "internal/adapter/*.go"},
		pattern: regexp.MustCompile(`(?:Getenv|LookupEnv)\("((?:XIANYU_|LOG_|BROWSER_HEADLESS|DATABASE_URL|TZ)[A-Z_]*)"\)|Env\s*=\s*"((?:XIANYU_)[A-Z_]+)"`),
		docs:    []string{"配置参考.zh-CN.md", "Configuration-Reference.md"},
	},
	{
		name:    "批量铺货邮费模式及别名必须出现在批量铺货页",
		sources: []string{"internal/application/items/batch_preview.go"},
		scope:   regexp.MustCompile(`(?s)func normalizePostageMode\(.*?\n\}`),
		pattern: regexp.MustCompile(`"([^"]+)"`),
		docs:    []string{"批量铺货.zh-CN.md", "Bulk-Listing.md"},
	},
	{
		name:    "发货模板内置变量必须出现在发货模板页",
		sources: []string{"internal/deliverytemplate/template.go"},
		pattern: regexp.MustCompile(`builtinVariablePattern = regexp\.MustCompile\(.+\(((?:\w+\|)+\w+)\)`),
		split:   "|",
		docs:    []string{"发货模板.zh-CN.md", "Delivery-Templates.md"},
		format:  "{{%s}}",
	},
	{
		name:    "API 卡请求变量必须出现在卡密库存页与上线总览",
		sources: []string{"internal/adapter/api_delivery_client.go"},
		pattern: regexp.MustCompile(`"(\{[a-z_]+\})":\s`),
		docs:    []string{"卡密库存.zh-CN.md", "Card-Inventory.md", "账号、库存与商品.zh-CN.md", "Accounts-Inventory-and-Products.md"},
		format:  "`%s`",
	},
	{
		name:    "通知事件的界面名称必须出现在 AI、通知与运维页",
		sources: []string{"frontend/app/features/notifications/state.ts"},
		pattern: regexp.MustCompile(`value: '[a-z_]+', label: '([^']+)'`),
		docs:    []string{"AI、通知与运维.zh-CN.md", "AI-Notifications-and-Operations.md"},
	},
	{
		name:    "桌面安装包服务标识必须出现在部署页",
		sources: []string{"packaging/macos/*.plist.template"},
		pattern: regexp.MustCompile(`<key>Label</key><string>([^<]+)</string>`),
		docs:    []string{"部署与数据库.zh-CN.md", "Deployment-and-Database.md"},
	},
	{
		name:    "Linux 服务单元名称必须出现在部署页",
		sources: []string{"packaging/linux/install.sh"},
		pattern: regexp.MustCompile(`(?m)^APP_NAME="([^"]+)"`),
		docs:    []string{"部署与数据库.zh-CN.md", "Deployment-and-Database.md"},
		format:  "%s.service",
	},
}

// numberRules 是全部数值一致性规则；关键词取自文档里该数值所在句子的稳定词汇。
var numberRules = []numberRule{
	{
		name: "批量铺货单次最多行数", source: "internal/server/item_publish_batch_handlers.go",
		pattern: regexp.MustCompile(`maxPublishBatchRows\s*=\s*(\d+)`),
		docs:    []docKeyword{{"批量铺货.zh-CN.md", "行"}, {"Bulk-Listing.md", "products per batch"}},
	},
	{
		name: "批量铺货表格大小上限（MiB）", source: "internal/server/item_publish_batch_handlers.go",
		pattern: regexp.MustCompile(`readLimitedBytes\(source, (\d+)<<20\)`),
		docs:    []docKeyword{{"批量铺货.zh-CN.md", "MiB"}, {"Bulk-Listing.md", "MiB"}},
	},
	{
		name: "批量铺货图片 ZIP 大小上限（MiB）", source: "internal/server/item_publish_batch_handlers.go",
		pattern: regexp.MustCompile(`readLimitedBytes\(zipFile, (\d+)<<20\)`),
		docs:    []docKeyword{{"批量铺货.zh-CN.md", "ZIP"}, {"Bulk-Listing.md", "ZIP"}},
	},
	{
		name: "快捷回复条数上限", source: "internal/application/chat/metadata.go",
		pattern: regexp.MustCompile(`quickReplyLimit\s*=\s*(\d+)`),
		docs:    []docKeyword{{"在线聊天与账号自动任务.zh-CN.md", "快捷回复"}, {"Chat-and-Account-Automation.md", "quick repl"}},
	},
	{
		name: "商品专属资料字数上限", source: "internal/application/items/item_ai_settings.go",
		pattern: regexp.MustCompile(`ItemAIContextMaxRunes\s*=\s*(\d+)`),
		docs:    []docKeyword{{"AI、通知与运维.zh-CN.md", "商品专属资料"}, {"AI-Notifications-and-Operations.md", "product-specific"}},
	},
	{
		name: "运行日志内存缓冲默认条数", source: "internal/logging/runtime_buffer.go",
		pattern: regexp.MustCompile(`defaultRuntimeBufferCapacity\s*=\s*(\d+)`),
		docs:    []docKeyword{{"运行日志.zh-CN.md", "条"}, {"Runtime-Logs.md", "entries"}},
	},
	{
		name: "自动化审计事件保留天数", source: "internal/automation/audit_recorder.go",
		pattern: regexp.MustCompile(`auditRetention\s*=\s*(\d+) \* 24`),
		docs:    []docKeyword{{"自动化发货与回复.zh-CN.md", "90"}, {"Automated-Delivery-and-Replies.md", "90"}},
	},
	{
		name: "API 卡幂等重试最大请求次数", source: "internal/adapter/api_delivery_client.go",
		pattern: regexp.MustCompile(`apiDeliveryRetryLimit\s*=\s*(\d+)`),
		docs:    []docKeyword{{"卡密库存.zh-CN.md", "重试"}, {"Card-Inventory.md", "retry"}},
	},
	{
		name: "登录失败限流阈值与窗口", source: "internal/server/login_limiter.go",
		pattern: regexp.MustCompile(`loginFailureWindow\s*=\s*(\d+) \* time\.Minute\s+loginFailuresPerIP\s*=\s*(\d+)\s+loginFailuresPerPrincipal\s*=\s*(\d+)`),
		docs:    []docKeyword{{"系统设置.zh-CN.md", "登录"}, {"System-and-AI.md", "login"}},
	},
	{
		name: "每日擦亮每页条数与最大页数", source: "internal/automation/account_tasks.go",
		pattern: regexp.MustCompile(`polishItemPageSize\s*=\s*(\d+)\s+polishItemMaxPages\s*=\s*(\d+)`),
		docs:    []docKeyword{{"在线聊天与账号自动任务.zh-CN.md", "擦亮"}, {"Chat-and-Account-Automation.md", "products per page"}},
	},
	{
		name: "待发货兜底冷却与观察窗口", source: "internal/automation/scheduler.go",
		pattern: regexp.MustCompile(`defaultPendingShipCooldown = (\d+) \* time\.Minute`),
		docs:    []docKeyword{{"自动化发货与回复.zh-CN.md", "兜底"}, {"Automated-Delivery-and-Replies.md", "fallback"}},
	},
	{
		name: "待发货兜底结算观察窗口", source: "internal/automation/scheduler.go",
		pattern: regexp.MustCompile(`defaultPendingShipSettleWindow = (\d+) \* time\.Minute`),
		docs:    []docKeyword{{"自动化发货与回复.zh-CN.md", "兜底"}, {"Automated-Delivery-and-Replies.md", "fallback"}},
	},
	{
		name: "MTOP 账号级节流间隔（秒）", source: "internal/xianyu/mtop/pacer.go",
		pattern: regexp.MustCompile(`DefaultPacerGap = (\d+) \* time\.Second[\s\S]*?BackgroundPacerGap = (\d+) \* time\.Second`),
		docs:    []docKeyword{{"请求频率与风控设计.zh-CN.md", "间隔"}, {"Request-Pacing-and-Risk-Control.md", "apart"}},
	},
	{
		name: "AI 报价有效期（分钟）", source: "internal/engine/reply.go",
		pattern: regexp.MustCompile(`aiQuoteValidity = (\d+) \* time\.Minute`),
		docs:    []docKeyword{{"账号管理.zh-CN.md", "分钟"}, {"Account-Management.md", "minutes"}},
	},
	{
		name: "发布间隔默认秒数", source: "internal/application/library/publish.go",
		pattern: regexp.MustCompile(`defaultPublishIntervalSeconds = (\d+)`),
		docs:    []docKeyword{{"资源库.zh-CN.md", "间隔"}, {"Resource-Library.md", "interval"}},
	},
	{
		name: "卡密延时发货秒数上限", source: "internal/application/cards/service.go",
		pattern: regexp.MustCompile(`draft\.DelaySeconds > (\d+)`),
		docs:    []docKeyword{{"卡密库存.zh-CN.md", "延时"}, {"Card-Inventory.md", "delay"}},
	},
	{
		name: "多规格组合数量上限", source: "internal/application/items/batch_specs.go",
		pattern: regexp.MustCompile(`maxBatchSpecCombinations = (\d+)`),
		docs:    []docKeyword{{"商品管理.zh-CN.md", "规格"}, {"Product-Management.md", "combination"}},
	},
	{
		name: "砍价最大轮次上限", source: "internal/application/settings/service.go",
		pattern: regexp.MustCompile(`settings\.MaxBargainRounds > (\d+)`),
		docs:    []docKeyword{{"账号管理.zh-CN.md", "轮次"}, {"Account-Management.md", "round"}},
	},
	{
		name: "账号暂停处理时长上限（分钟）", source: "frontend/app/features/accounts/components/AccountEditModal.tsx",
		pattern: regexp.MustCompile(`max="(\d+)"`),
		docs:    []docKeyword{{"账号管理.zh-CN.md", "暂停"}, {"Account-Management.md", "pause"}},
	},
	{
		name: "求评价规则默认等待、间隔与次数", source: "internal/automation/scheduler.go",
		pattern: regexp.MustCompile(`AfterShippedHours: (\d+), RepeatIntervalHours: (\d+), MaxAttempts: (\d+)`),
		docs:    []docKeyword{{"自动化发货与回复.zh-CN.md", "默认"}, {"Automated-Delivery-and-Replies.md", "default"}},
	},
}

// forbiddenRules 是已被证实错误的说法回归守卫；每条都对应一次真实的文档与代码漂移。
var forbiddenRules = []forbiddenRule{
	{"发货模板.zh-CN.md", "{{buyer_nick}}", "模板变量实际为 buyer_nickname"},
	{"发货模板.zh-CN.md", "{{item_title}}", "模板不支持 item_title 变量"},
	{"发货模板.zh-CN.md", "{{item_id}}", "模板不支持 item_id 变量"},
	{"Delivery-Templates.md", "{{buyer_nick}}", "模板变量实际为 buyer_nickname"},
	{"Delivery-Templates.md", "{{item_title}}", "模板不支持 item_title 变量"},
	{"Delivery-Templates.md", "{{item_id}}", "模板不支持 item_id 变量"},
	{"销售统计.zh-CN.md", "仅管理员", "销售统计对所有登录用户开放，只统计本人账号"},
	{"Sales-Statistics.md", "admin only", "销售统计对所有登录用户开放，只统计本人账号"},
	{"Sales-Statistics.md", "administrators only", "销售统计对所有登录用户开放，只统计本人账号"},
	{"系统设置.zh-CN.md", "能传递真实客户端 IP", "登录限流只读连接远端地址，不读 X-Forwarded-For"},
	{"配置参考.zh-CN.md", "命令行参数优先。", "数据库连接例外：DATABASE_URL 优先于 -db-url"},
}

// pagePairs 是需要中英文事实对齐检查的页面对；与 manual/scripts/pages.mjs 保持一致由站点构建兜底。
var pagePairs = []zhEnPair{
	{"账号管理.zh-CN.md", "Account-Management.md"},
	{"在线聊天与账号自动任务.zh-CN.md", "Chat-and-Account-Automation.md"},
	{"自动化发货与回复.zh-CN.md", "Automated-Delivery-and-Replies.md"},
	{"砍价订单与自动发货流程.zh-CN.md", "Bargain-Orders-and-Delivery-Flow.md"},
	{"卡密库存.zh-CN.md", "Card-Inventory.md"},
	{"发货模板.zh-CN.md", "Delivery-Templates.md"},
	{"资源库.zh-CN.md", "Resource-Library.md"},
	{"商品管理.zh-CN.md", "Product-Management.md"},
	{"批量铺货.zh-CN.md", "Bulk-Listing.md"},
	{"订单管理.zh-CN.md", "Orders.md"},
	{"部署与数据库.zh-CN.md", "Deployment-and-Database.md"},
	{"配置参考.zh-CN.md", "Configuration-Reference.md"},
	{"运行日志.zh-CN.md", "Runtime-Logs.md"},
	{"系统设置.zh-CN.md", "System-and-AI.md"},
	{"AI、通知与运维.zh-CN.md", "AI-Notifications-and-Operations.md"},
	{"通知事件参考.zh-CN.md", "Notification-Events.md"},
	{"管理API.zh-CN.md", "Admin-API.md"},
	{"仪表盘.zh-CN.md", "Dashboard.md"},
	{"销售统计.zh-CN.md", "Sales-Statistics.md"},
	{"风控处理.zh-CN.md", "Risk-Control-Troubleshooting.md"},
	{"请求频率与风控设计.zh-CN.md", "Request-Pacing-and-Risk-Control.md"},
	{"常见问题.zh-CN.md", "FAQ.md"},
}
