package main

import (
	"go/parser"
	"go/token"
	"testing"
)

// TestHTTPResponseContractBoundary 验证 HTTP 契约扫描会拒绝动态 map 和直接 map 响应。
func TestHTTPResponseContractBoundary(t *testing.T) {
	// fset 是模拟 Server 源码的文件位置集合。
	fset := token.NewFileSet()
	// syntax、err 是同时包含 map 响应类型和直接 map 写入的模拟 Server 文件。
	syntax, err := parser.ParseFile(fset, "response.go", []byte(`package server
type badResponse struct { Rows []map[string]any }
func handler(w any) { writeJSON(w, 200, map[string]any{"ok": true}) }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// violations 是动态响应契约的扫描结果。
	violations := checkHTTPResponseContracts("internal/server/response.go", syntax, fset)
	if len(violations) != 2 {
		t.Fatalf("violations=%+v", violations)
	}
	// cleanSyntax 是只使用具名字段的模拟 Server 文件。
	cleanSyntax, err := parser.ParseFile(fset, "clean.go", []byte(`package server
type goodResponse struct { Rows []goodRow }
type goodRow struct { ID string }
func handler(w any) { writeJSON(w, 200, goodResponse{}) }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// cleanViolations 是合规响应契约的扫描结果。
	cleanViolations := checkHTTPResponseContracts("internal/server/clean.go", cleanSyntax, fset)
	if len(cleanViolations) != 0 {
		t.Fatalf("clean violations=%+v", cleanViolations)
	}
}

// TestControlledDynamicResponseTypes 验证已登记的兼容动态键不会被阶段三门禁误判。
func TestControlledDynamicResponseTypes(t *testing.T) {
	// fset 是兼容响应模拟源码的文件位置集合。
	fset := token.NewFileSet()
	// syntax、err 是包含既有动态键响应的模拟 Server 文件。
	syntax, err := parser.ParseFile(fset, "compat.go", []byte(`package server
type settingsResponse map[string]string
type notificationBindingListResponse map[string][]bindingRow
type automationRulePageResponse struct { TriggerCounts map[string]int }
type bindingRow struct { ID string }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// violations 是兼容响应扫描结果；现有键形状必须继续保留。
	violations := checkHTTPResponseContracts("internal/server/compat.go", syntax, fset)
	if len(violations) != 0 {
		t.Fatalf("controlled response violations=%+v", violations)
	}
}

// TestHTTPResponseContractScope 验证架构扫描不会误伤测试代码和非 Server 包。
func TestHTTPResponseContractScope(t *testing.T) {
	// fset 是模拟源代码的文件位置集合。
	fset := token.NewFileSet()
	// syntax、err 是包含动态 map 的模拟文件。
	syntax, err := parser.ParseFile(fset, "response_test.go", []byte(`package server
type testResponse map[string]any
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// got 是测试文件被扫描出的架构违规列表。
	if got := checkHTTPResponseContracts("internal/server/response_test.go", syntax, fset); len(got) != 0 {
		t.Fatalf("test-file violations=%+v", got)
	}
	// got 是非 Server 文件被扫描出的架构违规列表。
	if got := checkHTTPResponseContracts("internal/application/response.go", syntax, fset); len(got) != 0 {
		t.Fatalf("non-server violations=%+v", got)
	}
}

// TestHTTPRequestContractBoundary 验证 handler 请求体必须使用具名 DTO，避免匿名结构绕过版本化契约。
func TestHTTPRequestContractBoundary(t *testing.T) {
	// fset 是模拟 Server handler 文件的源码位置集合。
	fset := token.NewFileSet()
	// syntax、err 分别是包含匿名请求结构的模拟 handler AST 及解析错误。
	syntax, err := parser.ParseFile(fset, "request_handlers.go", []byte(`package server
func handler() { var req struct { Value string }; _ = req }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// violations 是匿名请求 DTO 应产生的架构违规。
	violations := checkHTTPRequestContracts("internal/server/request_handlers.go", syntax, fset)
	if len(violations) != 1 {
		t.Fatalf("violations=%+v", violations)
	}
	// cleanSyntax、cleanErr 分别是使用具名请求 DTO 的模拟 handler AST 及解析错误。
	cleanSyntax, cleanErr := parser.ParseFile(fset, "clean_handlers.go", []byte(`package server
type requestDTO struct { Value string }
func handler() { var req requestDTO; _ = req }
`), parser.ParseComments)
	if cleanErr != nil {
		t.Fatal(cleanErr)
	}
	// cleanViolations 是具名 DTO 应保持为零的架构违规集合。
	cleanViolations := checkHTTPRequestContracts("internal/server/clean_handlers.go", cleanSyntax, fset)
	if len(cleanViolations) != 0 {
		t.Fatalf("clean violations=%+v", cleanViolations)
	}
}

// TestNormalizeImportPath 验证架构检查器能够识别当前模块的内部包路径。
func TestNormalizeImportPath(t *testing.T) {
	if got /* got 是规范化后的导入路径。 */ := normalizeImportPath("xianyu-go/internal/db"); got != "internal/db" {
		t.Fatalf("normalizeImportPath=%q", got)
	}
	if got /* got 是未带模块前缀的标准库导入路径。 */ := normalizeImportPath("database/sql"); got != "database/sql" {
		t.Fatalf("normalizeImportPath 标准库路径被错误修改为 %q", got)
	}
}

// TestApplicationImportBoundary 验证应用层禁止依赖数据库和 HTTP 层。
func TestApplicationImportBoundary(t *testing.T) {
	if !isForbiddenApplicationImport("internal/application/orders/read_model.go", "internal/db") {
		t.Fatal("应用层导入 internal/db 应被拒绝")
	}
	if !isForbiddenApplicationImport("internal/application/orders/read_model.go", "database/sql") {
		t.Fatal("应用层导入 database/sql 应被拒绝")
	}
	if isForbiddenApplicationImport("internal/application/orders/read_model.go", "context") {
		t.Fatal("应用层导入 context 不应被拒绝")
	}
	if isForbiddenApplicationImport("internal/server/order_service.go", "internal/db") {
		t.Fatal("Server 文件不应套用应用层导入规则")
	}
}

// TestServerLowLevelBoundary 验证 Server 低层依赖不得通过临时白名单保留。
func TestServerLowLevelBoundary(t *testing.T) {
	if !isForbiddenServerLowLevelImport("internal/server/cookie_handlers.go", "internal/db") {
		t.Fatal("Server 低层依赖应被门禁拒绝")
	}
	if !isForbiddenServerLowLevelImport("internal/server/new_service.go", "internal/db") {
		t.Fatal("新增 Server 低层依赖必须被门禁拒绝")
	}
	if !isForbiddenServerLowLevelImport("internal/server/unit_of_work.go", "database/sql") {
		t.Fatal("Server 不得暴露 database/sql 事务类型")
	}
	if isForbiddenServerLowLevelImport("internal/server/new_service_test.go", "internal/db") {
		t.Fatal("测试文件不应被生产依赖门禁阻断")
	}
}

// TestHiddenDependencyBoundary 验证生产应用与 Server 不得通过反射或插件隐藏必需依赖。
func TestHiddenDependencyBoundary(t *testing.T) {
	if !isForbiddenHiddenDependencyImport("internal/application/orders/service.go", "reflect") {
		t.Fatal("应用层 reflect 依赖应被拒绝")
	}
	if !isForbiddenHiddenDependencyImport("internal/server/server.go", "plugin") {
		t.Fatal("Server plugin 依赖应被拒绝")
	}
	if isForbiddenHiddenDependencyImport("internal/adapter/adapter_test.go", "reflect") {
		t.Fatal("测试或适配层 reflect 依赖不应被本规则阻断")
	}
}

// TestRuntimeSetterBoundary 验证生产调用不能通过 Adapter setter 延迟补齐必需依赖。
func TestRuntimeSetterBoundary(t *testing.T) {
	// fset 是模拟生产源码的文件位置集合。
	fset := token.NewFileSet()
	// syntax、err 是包含测试兼容 setter 调用的模拟生产文件。
	syntax, err := parser.ParseFile(fset, "runtime.go", []byte(`package main
func run(adapter any) { adapter.SetAutomation(nil) }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// violations 是生产 setter 调用的扫描结果。
	violations := checkRuntimeSetterCalls("cmd/server/runtime.go", syntax, fset)
	if len(violations) != 1 {
		t.Fatalf("violations=%+v", violations)
	}
	// testSyntax 是测试文件中的同一调用，测试替身可以继续使用兼容 setter。
	testSyntax, err := parser.ParseFile(fset, "runtime_test.go", []byte(`package main
func test(adapter any) { adapter.SetAutomation(nil) }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// got 是测试文件中的 setter 调用扫描结果。
	if got := checkRuntimeSetterCalls("cmd/server/runtime_test.go", testSyntax, fset); len(got) != 0 {
		t.Fatalf("测试 setter 不应被阻断: %+v", got)
	}
}

// TestServerCompositionBoundary 验证已迁出的应用 worker 不会回流到 Server transport 构造阶段。
func TestServerCompositionBoundary(t *testing.T) {
	// fset 是模拟 Server 源码的文件位置集合。
	fset := token.NewFileSet()
	// syntax、err 分别是包含禁止应用 worker 与健康端口构造的模拟 Server 文件及解析错误。
	syntax, err := parser.ParseFile(fset, "composition.go", []byte(`package server
func build() { orderapp.NewReconciliationRecoveryCoordinator(nil); adapter.NewDatabaseHealth(nil) }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// violations 是迁移回流应产生的架构违规。
	violations := checkServerCompositionCalls("internal/server/composition.go", syntax, fset)
	if len(violations) != 2 {
		t.Fatalf("violations=%+v", violations)
	}
	// cleanSyntax、cleanErr 分别是仅接收已构造服务的合规 Server 文件及解析错误。
	cleanSyntax, cleanErr := parser.ParseFile(fset, "composition_clean.go", []byte(`package server
func accept(service any) { _ = service }
`), parser.ParseComments)
	if cleanErr != nil {
		t.Fatal(cleanErr)
	}
	// cleanViolations 是合规 Server 文件应保持为空的违规集合。
	cleanViolations := checkServerCompositionCalls("internal/server/composition_clean.go", cleanSyntax, fset)
	if len(cleanViolations) != 0 {
		t.Fatalf("clean violations=%+v", cleanViolations)
	}
}

// TestServerLifecycleComponentBoundary 验证 Server 不能重新成为应用 worker 生命周期组件的反向提供者。
func TestServerLifecycleComponentBoundary(t *testing.T) {
	// fset 是组合根迁移源码片段的统一位置集合。
	fset := token.NewFileSet()
	// syntax、parseErr 分别是错误调用遗留生命周期方法的模拟 Server 源码及解析错误。
	syntax, parseErr := parser.ParseFile(fset, "lifecycle.go", []byte(`package server
func handler(server any) { server.ApplicationLifecycleComponents() }
`), parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是遗留生命周期反转必须触发的架构违规。
	violations := checkServerCompositionCalls("internal/server/lifecycle.go", syntax, fset)
	if len(violations) != 1 {
		t.Fatalf("violations=%+v", violations)
	}
}

// TestApplicationTypeLeakBoundary 验证应用 Port 类型扫描会拒绝基础设施和 Server 类型。
func TestApplicationTypeLeakBoundary(t *testing.T) {
	// fset 是测试源代码的文件位置集合。
	fset := token.NewFileSet()
	// syntax、err 是包含违规字段的模拟应用文件及解析错误。
	syntax, err := parser.ParseFile(fset, "port.go", []byte(`package orders
type Bad struct { Tx *sql.Tx; Row db.Order; Runtime *Server }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// violations 是应用 Port 类型扫描结果。
	violations := checkApplicationTypeLeaks("internal/application/orders/port.go", syntax, fset)
	if len(violations) != 3 {
		t.Fatalf("violations=%+v", violations)
	}
	// cleanSyntax 是不泄露基础设施类型的模拟应用文件。
	cleanSyntax, err := parser.ParseFile(fset, "clean.go", []byte(`package orders
type Good struct { ID string }
`), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// cleanViolations 是干净应用文件的类型扫描结果。
	cleanViolations := checkApplicationTypeLeaks("internal/application/orders/clean.go", cleanSyntax, fset)
	if len(cleanViolations) != 0 {
		t.Fatalf("clean violations=%+v", cleanViolations)
	}
}
