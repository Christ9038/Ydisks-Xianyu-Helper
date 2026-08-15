package main

import "testing"

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

// TestServerLowLevelTemporaryAllowlist 验证旧 Server 低层依赖必须显式登记。
func TestServerLowLevelTemporaryAllowlist(t *testing.T) {
	if isForbiddenServerLowLevelImport("internal/server/order_service.go", "internal/db") {
		t.Fatal("现有白名单文件不应被当前门禁阻断")
	}
	if !isForbiddenServerLowLevelImport("internal/server/new_service.go", "internal/db") {
		t.Fatal("新增 Server 低层依赖必须被门禁拒绝")
	}
	if isForbiddenServerLowLevelImport("internal/server/new_service_test.go", "internal/db") {
		t.Fatal("测试文件不应被生产依赖门禁阻断")
	}
}
