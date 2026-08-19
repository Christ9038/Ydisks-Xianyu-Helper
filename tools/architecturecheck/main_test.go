package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// TestServerLowLevelBoundary 验证 Server 低层依赖不得通过临时例外保留。
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

// TestServerInfrastructureFieldBoundary 验证生产 Server 不能重新保存业务运行时或具体平台客户端。
func TestServerInfrastructureFieldBoundary(t *testing.T) {
	// fset 是模拟 Server 源码的统一位置集合。
	fset := token.NewFileSet()
	// syntax、parseErr 分别是包含禁止字段的模拟 Server 文件及其解析错误。
	syntax, parseErr := parser.ParseFile(fset, "server.go", []byte(`package server
type Server struct { Manager *account.Manager; Client adapter.MTOPClient; Port PlatformPort }
`), parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是禁止字段必须触发的架构违规。
	violations := checkServerInfrastructureFields("internal/server/server.go", syntax, fset)
	if len(violations) != 2 {
		t.Fatalf("violations=%+v", violations)
	}
	// cleanSyntax、cleanErr 分别是只持有消费者定义 Port 的合规 Server 源码及其解析错误。
	cleanSyntax, cleanErr := parser.ParseFile(fset, "clean.go", []byte(`package server
type Server struct { Port PlatformPort }
`), parser.ParseComments)
	if cleanErr != nil {
		t.Fatal(cleanErr)
	}
	// cleanViolations 是合规 Server 应保持为空的扫描结果。
	cleanViolations := checkServerInfrastructureFields("internal/server/clean.go", cleanSyntax, fset)
	if len(cleanViolations) != 0 {
		t.Fatalf("clean violations=%+v", cleanViolations)
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

// TestStageTwoServerBoundary 验证阶段二会拒绝 Server 内残留的组合根、平台 Port 与基础设施导入。
func TestStageTwoServerBoundary(t *testing.T) {
	// fset 是模拟 Server 源码的统一位置集合。
	fset := token.NewFileSet()
	// syntax、parseErr 分别是包含阶段二禁止依赖和声明的模拟 Server 源码及解析失败原因。
	syntax, parseErr := parser.ParseFile(fset, "server.go", []byte(`package server
import "xianyu-go/internal/adapter"
type ApplicationServices struct{}
type PlatformPort interface{}
func (s *Server) ApplicationServices() *ApplicationServices { return nil }
func (s *Server) mtopClient() adapter.MTOPClient { return nil }
`), parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是组合根和平台旁路必须产生的违规集合。
	violations := checkStageTwoTransportBoundary("internal/server/server.go", syntax, fset)
	if len(violations) != 5 {
		t.Fatalf("violations=%+v", violations)
	}
}

// TestStageTwoConstructionBoundary 验证构造门禁只拒绝应用/adapter 构造与 factory 链，不误伤标准库调用。
func TestStageTwoConstructionBoundary(t *testing.T) {
	// fset 是模拟 Server 源码的统一位置集合。
	fset := token.NewFileSet()
	// syntax、parseErr 分别是包含标准库、应用构造和 factory 链的模拟 Server 源码及解析失败原因。
	syntax, parseErr := parser.ParseFile(fset, "composition.go", []byte(`package server
import json "encoding/json"
import orderapp "xianyu-go/internal/application/orders"
func build() { json.NewEncoder(nil); orderapp.NewRefreshJobRunner(nil, nil, orderapp.RefreshJobRunnerOptions{}); dependencies.ItemDependencies.NewItemBatchRepository() }
`), parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是应用构造与 factory 链必须产生的违规集合；json.NewEncoder 不应触发。
	violations := checkStageTwoServerConstruction("internal/server/composition.go", syntax, fset)
	if len(violations) != 2 {
		t.Fatalf("violations=%+v", violations)
	}
}

// TestStageTwoCmdBoundary 验证 cmd/server 不能通过 Server API 间接装配应用服务或反向取得生命周期组件。
func TestStageTwoCmdBoundary(t *testing.T) {
	// fset 是模拟 cmd/server 源码的统一位置集合。
	fset := token.NewFileSet()
	// syntax、parseErr 分别是包含旧组合根 API 的模拟入口源码及解析失败原因。
	syntax, parseErr := parser.ParseFile(fset, "main.go", []byte(`package main
func build() { server.NewApplicationServices(nil); applications.LifecycleComponents() }
`), parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是旧 Server 组合根 API 必须产生的违规集合。
	violations := checkStageTwoCmdCalls("cmd/server/main.go", syntax, fset)
	if len(violations) != 2 {
		t.Fatalf("violations=%+v", violations)
	}
}

// TestStageTwoCompositionRootBoundary 验证空目录和未被 cmd/server 调用的伪组合层不能通过阶段二门禁。
func TestStageTwoCompositionRootBoundary(t *testing.T) {
	// root 是独立临时仓库根目录，避免真实工作树状态影响结构性单元测试。
	root := t.TempDir()
	// missingViolations 是缺少 composition 目录时应产生的违规集合。
	missingViolations := checkStageTwoCompositionRoot(root)
	if len(missingViolations) != 1 {
		t.Fatalf("missing violations=%+v", missingViolations)
	}
	// compositionDir 是满足阶段二目录形态的最小独立组合层目录。
	compositionDir := filepath.Join(root, "internal", "composition")
	// mkdirErr 表示创建临时组合层目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(compositionDir, 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// compositionSource 是组合层最小生产文件；只有测试文件不能替代生产组合根。
	compositionSource := []byte("package composition\n")
	// writeErr 表示写入临时组合层生产文件失败的文件系统原因。
	if writeErr := os.WriteFile(filepath.Join(compositionDir, "composition.go"), compositionSource, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// commandDir 是最小 cmd/server 入口目录。
	commandDir := filepath.Join(root, "cmd", "server")
	// mkdirErr 表示创建临时入口目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(commandDir, 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// commandSource 是显式引用组合层的最小入口源码。
	commandSource := []byte("package main\nimport _ \"xianyu-go/internal/composition\"\n")
	// writeErr 表示写入临时入口源码失败的文件系统原因。
	if writeErr := os.WriteFile(filepath.Join(commandDir, "main.go"), commandSource, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// cleanViolations 是满足独立组合层和入口调用关系后的违规集合。
	cleanViolations := checkStageTwoCompositionRoot(root)
	if len(cleanViolations) != 0 {
		t.Fatalf("clean violations=%+v", cleanViolations)
	}
}

// TestStageTwoApplicationPortBoundary 验证 HTTP Port 容器不能以具体应用服务指针伪装最小依赖。
func TestStageTwoApplicationPortBoundary(t *testing.T) {
	// fset 是模拟 Server Port 源码的位置集合。
	fset := token.NewFileSet()
	// syntax、parseErr 分别是同时包含违规具体服务指针和合规消费者接口的模拟源码及解析错误。
	syntax, parseErr := parser.ParseFile(fset, "application_ports.go", []byte(`package server
import accountapp "xianyu-go/internal/application/account"
type ApplicationPorts struct { Bad *accountapp.RuntimeService; Good AccountRuntimePort }
type ApplicationPortsInput struct { Nested []*accountapp.ProfileService }
type AccountRuntimePort interface{}
`), parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是具体应用服务指针必须触发的违规集合。
	violations := checkStageTwoApplicationPortDeclarations("internal/server/application_ports.go", syntax, fset)
	if len(violations) != 2 {
		t.Fatalf("violations=%+v", violations)
	}
	// cleanSyntax、cleanErr 分别是仅声明消费者接口的合规源码及解析错误。
	cleanSyntax, cleanErr := parser.ParseFile(fset, "application_ports.go", []byte(`package server
type ApplicationPorts struct { Runtime AccountRuntimePort }
type ApplicationPortsInput struct { Runtime AccountRuntimePort }
type AccountRuntimePort interface{}
`), parser.ParseComments)
	if cleanErr != nil {
		t.Fatal(cleanErr)
	}
	// cleanViolations 是合规 Port 容器应保持为空的扫描结果。
	cleanViolations := checkStageTwoApplicationPortDeclarations("internal/server/application_ports.go", cleanSyntax, fset)
	if len(cleanViolations) != 0 {
		t.Fatalf("clean violations=%+v", cleanViolations)
	}
}

// TestStageTwoCompositionProjectionBoundary 验证 composition 核心不能反向导入 HTTP Server。
func TestStageTwoCompositionProjectionBoundary(t *testing.T) {
	// fset 是模拟 composition 源码的位置集合。
	fset := token.NewFileSet()
	// syntax、parseErr 分别是错误依赖 Server 的 composition 核心源码及解析错误。
	syntax, parseErr := parser.ParseFile(fset, "services.go", []byte(`package composition
import "xianyu-go/internal/server"
var _ server.Dependencies
`), parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是组合核心反向依赖必须触发的违规集合。
	violations := checkStageTwoCompositionProjection("internal/composition/services.go", syntax, fset)
	if len(violations) != 1 {
		t.Fatalf("violations=%+v", violations)
	}
	// cleanViolations 是 runtime 投影层的合法依赖应保持为空的扫描结果。
	if cleanViolations := checkStageTwoCompositionProjection("internal/composition/runtime/server_dependencies.go", syntax, fset); len(cleanViolations) != 0 {
		t.Fatalf("runtime projection violations=%+v", cleanViolations)
	}
}

// TestStageTwoRealSourceGate 验证完成迁移后的真实仓库源码必须通过阶段二门禁。
func TestStageTwoRealSourceGate(t *testing.T) {
	// root 是当前阶段二迁移后的真实仓库根目录。
	root := repositoryRootForContractTest(t)
	// violations、checkErr 分别是真实源码扫描结果和扫描失败原因。
	violations, checkErr := checkRepository(root)
	if checkErr != nil {
		t.Fatal(checkErr)
	}
	if len(violations) != 0 {
		// joined 是将全部违规位置拼接为便于定位的失败诊断文本。
		joined := ""
		// violation 是当前一条真实源码架构违规，按扫描顺序汇总。
		for _, violation := range violations {
			joined += violation.file + " " + violation.message + "\n"
		}
		t.Fatalf("完成迁移后的真实源码仍违反阶段二门禁:\n%s", joined)
	}
}
