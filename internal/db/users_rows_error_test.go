package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
)

// errDeleteUserCookieRows 是测试驱动在 Cookie 行迭代过程中返回的原始错误。
var errDeleteUserCookieRows = errors.New("cookie rows iteration failed")

// deleteUserRowsDriverName 是只供本测试使用的 database/sql 驱动注册名称。
const deleteUserRowsDriverName = "ydisks-delete-user-rows-error"

// deleteUserRowsRegisterOnce 保证并发测试执行时仅注册一次测试驱动。
var deleteUserRowsRegisterOnce sync.Once

// deleteUserRowsState 记录删除用户事务执行的提交、回滚和 SQL 阶段。
type deleteUserRowsState struct {
	// rolledBack 表示 Delete 在行迭代失败后是否按事务约定回滚。
	rolledBack bool
	// committed 表示 Delete 是否错误地在不完整 Cookie 行流后提交事务。
	committed bool
	// executed 保存已执行语句，用于断言未继续运行关联删除。
	executed []string
}

// deleteUserRowsDriver 创建共享状态的测试数据库连接。
type deleteUserRowsDriver struct {
	// state 保存当前测试要观察的事务行为。
	state *deleteUserRowsState
}

// Open 返回使用同一状态记录器的测试连接。
func (driverInstance deleteUserRowsDriver) Open(string) (driver.Conn, error) {
	return &deleteUserRowsConn{state: driverInstance.state}, nil
}

// deleteUserRowsConn 实现 Delete 所需的事务、查询和执行最小数据库驱动接口。
type deleteUserRowsConn struct {
	// state 保存本连接观察到的事务和 SQL 执行记录。
	state *deleteUserRowsState
}

// Prepare 不支持非 Context 预处理路径；Delete 不会使用该接口。
func (connection *deleteUserRowsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}

// Close 释放测试连接；共享状态无需额外清理。
func (connection *deleteUserRowsConn) Close() error { return nil }

// Begin 返回能记录 Commit/Rollback 的测试事务。
func (connection *deleteUserRowsConn) Begin() (driver.Tx, error) {
	return &deleteUserRowsTx{state: connection.state}, nil
}

// BeginTx 与 Begin 保持同一事务记录语义。
func (connection *deleteUserRowsConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return connection.Begin()
}

// ExecContext 记录当前 SQL；测试若发现后续关联删除会在断言中失败。
func (connection *deleteUserRowsConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	connection.state.executed = append(connection.state.executed, query)
	return driver.RowsAffected(1), nil
}

// QueryContext 仅为 Cookie ID 查询返回一条记录后发生的行流迭代错误。
func (connection *deleteUserRowsConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "SELECT id FROM cookies") {
		return &deleteUserRows{position: 0}, nil
	}
	return nil, errors.New("unexpected query")
}

// deleteUserRowsTx 记录 database/sql 最终选择提交还是回滚。
type deleteUserRowsTx struct {
	// state 保存共享的测试事务观察值。
	state *deleteUserRowsState
}

// Commit 记录事务提交动作。
func (transaction *deleteUserRowsTx) Commit() error { transaction.state.committed = true; return nil }

// Rollback 记录事务回滚动作。
func (transaction *deleteUserRowsTx) Rollback() error {
	transaction.state.rolledBack = true
	return nil
}

// deleteUserRows 在返回一条 Cookie ID 后向 database/sql 报告迭代错误。
type deleteUserRows struct {
	// position 保存当前行流推进位置。
	position int
}

// Columns 返回 Cookie ID 查询的单列名称。
func (rows *deleteUserRows) Columns() []string { return []string{"id"} }

// Close 关闭测试行流；迭代错误通过 Next 返回。
func (rows *deleteUserRows) Close() error { return nil }

// Next 先提供一个 Cookie ID，再返回预设迭代错误。
func (rows *deleteUserRows) Next(destination []driver.Value) error {
	if rows.position == 0 {
		destination[0] = "cookie-1"
		rows.position++
		return nil
	}
	return errDeleteUserCookieRows
}

// TestUsersDeleteRollsBackWhenCookieRowsIterationFails 验证不完整 Cookie 行流不会触发关联删除或提交。
func TestUsersDeleteRollsBackWhenCookieRowsIterationFails(t *testing.T) {
	// state 保存本次删除操作的事务观察值。
	state := &deleteUserRowsState{}
	deleteUserRowsRegisterOnce.Do( /* 当前回调注册仅供本测试使用的最小 database/sql 驱动。 */ func() {
		sql.Register(deleteUserRowsDriverName, deleteUserRowsDriver{state: state})
	})
	// database 保存测试驱动提供的临时数据库连接。
	database, openErr := sql.Open(deleteUserRowsDriverName, "")
	if openErr != nil {
		t.Fatalf("打开测试数据库失败: %v", openErr)
	}
	defer database.Close()
	// users 保存待验证的用户仓储。
	users := &Users{DB: database}
	// deleteErr 保存用户删除返回的行流迭代错误。
	deleteErr := users.Delete(context.Background(), 7)
	if !errors.Is(deleteErr, errDeleteUserCookieRows) {
		t.Fatalf("删除错误=%v，期望原始迭代错误", deleteErr)
	}
	if !state.rolledBack || state.committed {
		t.Fatalf("事务状态 rolledBack=%t committed=%t", state.rolledBack, state.committed)
	}
	// query 保存当前已执行的 SQL，用于确认行流失败后没有进入关联数据删除阶段。
	for _, query := range state.executed {
		if strings.Contains(query, "automation_rules") || strings.Contains(query, "DELETE FROM users") {
			t.Fatalf("行迭代失败后仍执行了后续删除: %s", query)
		}
	}
}
