package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"xianyu-go/internal/db"
)

var (
	// serverTestTemplateOnce 保证整个 server 测试进程只执行一次全量迁移。
	serverTestTemplateOnce sync.Once
	// serverTestTemplateDir 保存共享 SQLite 模板所在的临时目录。
	serverTestTemplateDir string
	// serverTestTemplatePath 保存已完成迁移的 SQLite 模板文件路径。
	serverTestTemplatePath string
	// serverTestTemplateErr 保存创建共享模板时遇到的错误。
	serverTestTemplateErr error
	// openAPISuccessOperations 收集当前 Go 测试进程中经真实 Router 成功响应校验的 operationId。
	openAPISuccessOperations sync.Map
)

// TestMain 在所有 server 测试结束后删除共享数据库模板目录。
func TestMain(m *testing.M) {
	// exitCode 保存测试套件的最终退出状态。
	exitCode := m.Run()
	if serverTestTemplateDir != "" {
		_ = os.RemoveAll(serverTestTemplateDir)
	}
	os.Exit(exitCode)
}

// serverTestDatabasePath 创建当前测试专属的 SQLite 副本并返回文件路径。
func serverTestDatabasePath(t *testing.T) string {
	t.Helper()
	serverTestTemplateOnce.Do(func() {
		// templateDir 是共享模板的临时目录，生命周期覆盖整个测试进程。
		templateDir, err := os.MkdirTemp("", "ydisks-server-test-db-")
		if err != nil {
			serverTestTemplateErr = err
			return
		}
		serverTestTemplateDir = templateDir
		serverTestTemplatePath = filepath.Join(templateDir, "template.db")
		// templateDB 是仅用于执行一次 goose 迁移的数据库连接。
		templateDB, _, err := db.Open(context.Background(), serverTestTemplatePath)
		if err != nil {
			serverTestTemplateErr = err
			return
		}
		// seedErr 表示向模板写入固定管理员和账号夹具时的错误。
		if seedErr := seedServerTestTemplate(templateDB); seedErr != nil {
			serverTestTemplateErr = seedErr
			_ = templateDB.Close()
			return
		}
		// closeErr 表示模板连接关闭失败；关闭后才能安全复制 SQLite 文件。
		if closeErr := templateDB.Close(); closeErr != nil {
			serverTestTemplateErr = closeErr
		}
	})
	if serverTestTemplateErr != nil {
		t.Fatalf("创建 server 测试数据库模板失败: %v", serverTestTemplateErr)
	}
	// testPath 是当前测试从只读模板复制出的独立数据库文件。
	testPath := filepath.Join(t.TempDir(), "test.db")
	// templateData 是模板数据库的完整字节内容，SQLite 连接尚未打开时可安全复制。
	templateData, err := os.ReadFile(serverTestTemplatePath)
	if err != nil {
		t.Fatalf("读取 server 测试数据库模板失败: %v", err)
	}
	// writeErr 表示将模板字节写入当前测试文件时的文件系统错误。
	if writeErr := os.WriteFile(testPath, templateData, 0o600); writeErr != nil {
		t.Fatalf("复制 server 测试数据库模板失败: %v", writeErr)
	}
	return testPath
}

// seedServerTestTemplate 向已迁移的模板写入所有普通 server 测试共同需要的固定数据。
func seedServerTestTemplate(database *sql.DB) error {
	// ctx 是模板初始化期间复用的无取消数据库上下文。
	ctx := context.Background()
	// store 聚合模板初始化所需的用户和账号 repository。
	store := db.NewStore(database, db.DialectSQLite)
	// createErr 表示创建固定管理员账户失败的原因。
	if _, createErr := store.Users.Create(ctx, "admin", "a@e.com", "pw"); createErr != nil {
		return fmt.Errorf("创建 server 测试管理员: %w", createErr)
	}
	// adminErr 表示把固定账户标记为管理员失败的原因。
	if adminErr := store.Users.SetAdmin(ctx, "admin"); adminErr != nil {
		return fmt.Errorf("设置 server 测试管理员: %w", adminErr)
	}
	// admin 是账号 cookie 所属的固定管理员用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		return fmt.Errorf("读取 server 测试管理员: %w", adminErr)
	}
	// cookieErr 表示创建测试账号 cookie 夹具失败的原因。
	if cookieErr := store.Cookies.Save(ctx, "acc1", "unb=123; _m_h5_tk=tk1_1;", admin.ID); cookieErr != nil {
		return fmt.Errorf("创建 server 测试账号: %w", cookieErr)
	}
	return nil
}

// openServerTestDatabase 直接打开已迁移的 SQLite 副本，避免每个测试再次执行 goose。
func openServerTestDatabase(path string) (*sql.DB, error) {
	// database 是当前测试专属的 SQLite 连接池。
	database, err := sql.Open("sqlite", sqliteTestDSN(path))
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 测试副本: %w", err)
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	// pingErr 表示测试副本无法建立有效数据库连接的原因。
	if pingErr := database.PingContext(context.Background()); pingErr != nil {
		_ = database.Close()
		return nil, fmt.Errorf("连接 SQLite 测试副本: %w", pingErr)
	}
	return database, nil
}

// sqliteTestDSN 为测试副本开启外键、WAL 和忙等待，保持与生产 SQLite 配置一致。
func sqliteTestDSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
}
