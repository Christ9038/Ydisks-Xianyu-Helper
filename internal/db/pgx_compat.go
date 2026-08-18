package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
)

// pgxCompatDriverName 是包裹 pgx 的自定义 driver 名。
// 注册后 sql.Open(pgxCompatDriverName, dsn) 走 qPgDriver，把 ? 改写成 $N。
// pgxCompatDriverName 用于本次流程后续判断的pgxCompatDriver名称
const pgxCompatDriverName = "pgx_compat"

// init 封装init业务协调。
func init() {
	sql.Register(pgxCompatDriverName, &qPgDriver{base: stdlib.GetDefaultDriver()})
}

// qPgDriver 包裹 pgx stdlib driver，仅为了把 ? 占位符改写成 $N。
// 全仓库业务 SQL 都用 ? 写（SQLite/MySQL 原生支持），Postgres 的 pgx
// 只认 $1/$2/...，不重写会报 "syntax error at or near \",\""。
// qPgDriver 用于本次流程后续判断的qPgDriver
type qPgDriver struct{ base driver.Driver }

// Open 打开当前值。
func (d *qPgDriver) Open(name string) (driver.Conn, error) {
	// c、err 用于本次流程后续判断的c、err
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &qConn{Conn: c}, nil
}

// qConn 包裹 pgx 连接。Exec/Query/Prepare 重写 SQL，其余方法透传给底层 pgx conn。
// 嵌入 driver.Conn 接口会把 Close/Begin/Prepare（legacy）等方法自动透传；
// 显式实现的可选接口方法（带 Context、Pinger、Validator 等）覆盖/补齐 pgx 行为。
// qConn 用于本次流程后续判断的qConn
type qConn struct {
	driver.Conn
}

// ExecContext 封装Exec上下文业务协调。
func (c *qConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	// ex、ok 用于本次流程后续判断的ex、ok
	ex, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return ex.ExecContext(ctx, rewriteQuestionPlaceholders(query), args)
}

// QueryContext 封装查询上下文业务协调。
func (c *qConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	// qx、ok 用于本次流程后续判断的qx、ok
	qx, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return qx.QueryContext(ctx, rewriteQuestionPlaceholders(query), args)
}

// PrepareContext 封装Prepare上下文业务协调。
func (c *qConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	// px、ok 用于本次流程后续判断的px、ok
	px, ok := c.Conn.(driver.ConnPrepareContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return px.PrepareContext(ctx, rewriteQuestionPlaceholders(query))
}

// BeginTx 封装BeginTx业务协调。
func (c *qConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// bx、ok 用于本次流程后续判断的bx、ok
	bx, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	return bx.BeginTx(ctx, opts)
}

// Ping 封装Ping业务协调。
func (c *qConn) Ping(ctx context.Context) error {
	if // p、ok 用于本次流程后续判断的p、ok
	p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

// IsValid 封装Is有效业务协调。
func (c *qConn) IsValid() bool {
	if // v、ok 用于本次流程后续判断的v、ok
	v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

// CheckNamedValue 检查Named值。
func (c *qConn) CheckNamedValue(nv *driver.NamedValue) error {
	if // nc、ok 用于本次流程后续判断的nc、ok
	nc, ok := c.Conn.(driver.NamedValueChecker); ok {
		return nc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// ResetSession 封装Reset会话业务协调。
func (c *qConn) ResetSession(ctx context.Context) error {
	if // rs、ok 用于本次流程后续判断的rs、ok
	rs, ok := c.Conn.(driver.SessionResetter); ok {
		return rs.ResetSession(ctx)
	}
	return nil
}

// rewriteQuestionPlaceholders 把 SQL 里的 ? 改写成 $1, $2, ...（Postgres 占位符）。
// 跳过单引号字符串字面量（'...'）和双引号标识符（"..."）内的 ?；
// ” 转义单引号也正确处理（相邻两个 ' 不改变字面量状态）。
// rewriteQuestionPlaceholders 封装rewriteQuestionPlaceholders业务协调。
func rewriteQuestionPlaceholders(query string) string {
	// b 用于本次流程后续判断的b
	var b strings.Builder
	b.Grow(len(query) + 16)
	// n 用于本次流程后续判断的n
	n := 0
	// inSingle、inDouble 用于本次流程后续判断的inSingle、inDouble
	inSingle, inDouble := false, false
	for // i 用于本次流程后续判断的i
	i := 0; i < len(query); i++ {
		// ch 用于本次流程后续判断的ch
		ch := query[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			b.WriteByte(ch)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			b.WriteByte(ch)
		case '?':
			if inSingle || inDouble {
				b.WriteByte(ch)
			} else {
				n++
				b.WriteByte('$')
				b.WriteString(strconv.Itoa(n))
			}
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}
