package db

import (
	"context"
	"database/sql"
	"strings"
)

// DialectUpsert 生成 UPSERT 子句，处理三种数据库的语法差异。
//
// SQLite/Postgres: ON CONFLICT (conflictCols) DO UPDATE SET col=expr, ...
//
//	MySQL:           ON DUPLICATE KEY UPDATE col=expr, ...
//
// updateAssignments 是 列名 -> SQL表达式 的映射。表达式里用 "EXCLUDED.col"
// 表示插入值（SQLite/Postgres 语法），MySQL 下自动转成 "VALUES(col)"。
// 其他表达式（如 "CURRENT_TIMESTAMP"）原样保留。
// DialectUpsert 负责DialectUpsert相关处理。
func DialectUpsert(d Dialect, conflictCols []string, updateAssignments map[string]string) string {
	if len(conflictCols) == 0 || len(updateAssignments) == 0 {
		return ""
	}
	// keys 保存keys，供当前处理流程使用
	keys := make([]string, 0, len(updateAssignments))
	// k 表示当前遍历过程中的k
	for k := range updateAssignments {
		keys = append(keys, k)
	}
	sortStrings(keys)
	// sets 保存sets，供当前处理流程使用
	sets := make([]string, 0, len(keys))
	// col 表示当前遍历过程中的col
	for _, col := range keys {
		// expr 保存expr，供当前处理流程使用
		expr := updateAssignments[col]
		if d == DialectMySQL {
			// EXCLUDED.col / excluded.col → VALUES(col)（大小写不敏感）
			upper := strings.ToUpper(expr)
			if strings.HasPrefix(upper, "EXCLUDED.") {
				field := expr[len("EXCLUDED."):] // 保留原大小写
				expr = "VALUES(" + field + ")"
			}
		}
		sets = append(sets, col+"="+expr)
	}
	switch d {
	case DialectMySQL:
		return " ON DUPLICATE KEY UPDATE " + strings.Join(sets, ", ")
	default:
		return " ON CONFLICT (" + strings.Join(conflictCols, ", ") + ") DO UPDATE SET " + strings.Join(sets, ", ")
	}
}

// dialectUpsert 包内别名（db 包内部用小写）。
func dialectUpsert(d Dialect, cols []string, m map[string]string) string {
	return DialectUpsert(d, cols, m)
}

// DialectInsertIgnore 返回"冲突即忽略"子句（VALUES 之后拼接）。
//
//	SQLite/Postgres: ON CONFLICT (cols) DO NOTHING
//	MySQL: 空（调用方应用 DialectInsertIgnorePrefix）
//
// DialectInsertIgnore 负责DialectInsertIgnore相关处理。
func DialectInsertIgnore(d Dialect, conflictCols []string) string {
	if d == DialectMySQL {
		return ""
	}
	if len(conflictCols) == 0 {
		return ""
	}
	return " ON CONFLICT (" + strings.Join(conflictCols, ", ") + ") DO NOTHING"
}

// dialectInsertIgnore 负责dialectInsertIgnore相关处理。
func dialectInsertIgnore(d Dialect, cols []string) string { return DialectInsertIgnore(d, cols) }

// DialectInsertIgnorePrefix 返回 INSERT 前缀（MySQL 用 INSERT IGNORE）。
func DialectInsertIgnorePrefix(d Dialect) string {
	if d == DialectMySQL {
		return "INSERT IGNORE"
	}
	return "INSERT"
}

// dialectInsertIgnorePrefix 负责dialectInsertIgnorePrefix相关处理。
func dialectInsertIgnorePrefix(d Dialect) string { return DialectInsertIgnorePrefix(d) }

// DialectQuote 返回方言下的标识符引用。
//
//	SQLite/MySQL: `name`（反引号）
//	Postgres:     "name"（双引号）
//
// DialectQuote 负责DialectQuote相关处理。
func DialectQuote(d Dialect, name string) string {
	if d == DialectPostgres {
		return "\"" + name + "\""
	}
	return "`" + name + "`"
}

// dialectQuote 负责dialectQuote相关处理。
func dialectQuote(d Dialect, name string) string { return DialectQuote(d, name) }

// sortStrings 原地排序字符串切片。
func sortStrings(s []string) {
	for // i 保存i，供当前处理流程使用
	i := 1; i < len(s); i++ {
		for // j 保存j，供当前处理流程使用
		j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// DBTX 是 *sql.DB 与 *sql.Tx 共有的最小执行接口，供 insertReturningID 复用。
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// insertReturningID 执行 INSERT 并返回自增主键 id。
// Postgres 的 pgx driver 不支持 Result.LastInsertId（Postgres 无该概念），
// 改用 `INSERT ... RETURNING id` + QueryRow.Scan；SQLite/MySQL 走 res.LastInsertId()。
// query 必须是可安全追加 ` RETURNING id` 的单条 INSERT（无尾随分号/ON CONFLICT 亦可）。
// insertReturningID 负责insertReturningID相关处理。
func insertReturningID(ctx context.Context, exec DBTX, dialect Dialect, query string, args ...any) (int64, error) {
	if dialect == DialectPostgres {
		// id 保存标识，供当前处理流程使用
		var id int64
		if // err 保存err，供当前处理流程使用
		err := exec.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
