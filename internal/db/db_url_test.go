package db

import (
	"strings"
	"testing"
)

// --- db.go: parseDBURL / mysqlDSN ---

// TestParseDBURL 覆盖各 scheme 与向后兼容路径。
func TestParseDBURL(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name     string
		url      string
		driver   driverName
		dialect  Dialect
		dsnHas   string // dsn 应包含的子串
		wantErr  bool
		errMatch string
	}{
		{name: "sqlite path (no scheme)", url: "/tmp/x.db", driver: driverSQLite, dialect: DialectSQLite, dsnHas: "file:/tmp/x.db"},
		{name: "sqlite scheme", url: "sqlite://rel/path.db", driver: driverSQLite, dialect: DialectSQLite, dsnHas: "file:rel/path.db"},
		{name: "sqlite3 scheme", url: "sqlite3://x.db", driver: driverSQLite, dialect: DialectSQLite, dsnHas: "file:x.db"},
		{name: "mysql scheme", url: "mysql://user:pass@tcp(h:3306)/db", driver: driverMySQL, dialect: DialectMySQL, dsnHas: "clientFoundRows=true"},
		{name: "postgres url scheme", url: "postgres://u:p@h:5432/db", driver: driverPgx, dialect: DialectPostgres, dsnHas: "postgres://u:p@h:5432/db"},
		{name: "postgres url without user", url: "postgres://localhost:5432/db?sslmode=disable", driver: driverPgx, dialect: DialectPostgres, dsnHas: "postgres://localhost:5432/db?sslmode=disable"},
		{name: "pgx alias", url: "pgx://u:p@h:5432/db", driver: driverPgx, dialect: DialectPostgres, dsnHas: "pgx://u:p@h:5432/db"},
		{name: "postgres kv dsn", url: "postgres://host=localhost port=5432", driver: driverPgx, dialect: DialectPostgres, dsnHas: "host=localhost"},
		{name: "postgresql alias", url: "postgresql://u:p@h:5432/db", driver: driverPgx, dialect: DialectPostgres, dsnHas: "postgresql://u:p@h:5432/db"},
		{name: "empty url", url: "", wantErr: true, errMatch: "为空"},
		{name: "whitespace url", url: "   ", wantErr: true, errMatch: "为空"},
		{name: "unknown scheme", url: "redis://h:6379", wantErr: true, errMatch: "不支持"},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// driver、dialect、dsn、err 用于本次流程后续判断的driver、dialect、dsn、err
			driver, dialect, dsn, err := parseDBURL(c.url)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错, got driver=%s dsn=%s", driver, dsn)
				}
				if c.errMatch != "" && !strings.Contains(err.Error(), c.errMatch) {
					t.Fatalf("错误信息 %q 不含 %q", err.Error(), c.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDBURL(%q): %v", c.url, err)
			}
			if driver != c.driver {
				t.Errorf("driver=%s want %s", driver, c.driver)
			}
			if dialect != c.dialect {
				t.Errorf("dialect=%s want %s", dialect, c.dialect)
			}
			if c.dsnHas != "" && !strings.Contains(dsn, c.dsnHas) {
				t.Errorf("dsn=%q 不含 %q", dsn, c.dsnHas)
			}
		})
	}
}

// TestMysqlDSN mysqlDSN 强制启用迁移和匹配行计数所需参数。
func TestMysqlDSN(t *testing.T) {
	// 无 query → 追加两个参数。
	got := mysqlDSN("u:p@tcp(h:3306)/db")
	if !strings.Contains(got, "multiStatements=true") || !strings.Contains(got, "clientFoundRows=true") || !strings.HasPrefix(got, "u:p@tcp(h:3306)/db?") {
		t.Fatalf("无 query: %q", got)
	}
	// 已有 query → 保留原参数。
	got = mysqlDSN("u:p@tcp(h:3306)/db?parseTime=true")
	if !strings.Contains(got, "parseTime=true") || !strings.Contains(got, "multiStatements=true") || !strings.Contains(got, "clientFoundRows=true") {
		t.Fatalf("已有 query: %q", got)
	}
	// 即使调用方显式关闭也要覆盖，否则 no-op UPDATE 会被误判为不存在。
	got = mysqlDSN("u:p@tcp(h:3306)/db?multiStatements=false&clientFoundRows=false&loc=UTC")
	if strings.Count(got, "multiStatements=") != 1 || strings.Count(got, "clientFoundRows=") != 1 ||
		strings.Contains(got, "multiStatements=false") || strings.Contains(got, "clientFoundRows=false") || !strings.Contains(got, "loc=UTC") {
		t.Fatalf("未正确强制参数: %q", got)
	}
}

// --- pgx_compat.go: rewriteQuestionPlaceholders ---

// TestRewriteQuestionPlaceholders ? → $N，跳过引号内字面量。
func TestRewriteQuestionPlaceholders(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "no placeholder", in: "SELECT 1", want: "SELECT 1"},
		{name: "single", in: "SELECT * FROM t WHERE id=?", want: "SELECT * FROM t WHERE id=$1"},
		{name: "multi", in: "INSERT INTO t (a,b,c) VALUES (?,?,?)", want: "INSERT INTO t (a,b,c) VALUES ($1,$2,$3)"},
		{name: "single-quoted string with ?", in: "SELECT '?' AS q WHERE id=?", want: "SELECT '?' AS q WHERE id=$1"},
		{name: "escaped single quote", in: "SELECT 'it''s a ?' WHERE id=?", want: "SELECT 'it''s a ?' WHERE id=$1"},
		{name: "double-quoted identifier with ?", in: `SELECT "col?" FROM t WHERE id=?`, want: `SELECT "col?" FROM t WHERE id=$1`},
		{name: "empty", in: "", want: ""},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got 用于本次流程后续判断的got
			got := rewriteQuestionPlaceholders(c.in)
			if got != c.want {
				t.Errorf("rewriteQuestionPlaceholders(%q)=%q want %q", c.in, got, c.want)
			}
		})
	}
}
