package main

import (
	"go/parser"
	"go/token"
	"testing"
)

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
	if isForbiddenServerLowLevelImport("internal/server/new_service_test.go", "internal/db") {
		t.Fatal("测试文件不应被生产依赖门禁阻断")
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
