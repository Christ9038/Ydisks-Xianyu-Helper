package db

import (
	"strings"
	"testing"
)

// TestDialectUpsert 表驱动断言三种方言的 UPSERT 子句生成。
// 关键不变量：SQLite/Postgres 用 ON CONFLICT...DO UPDATE SET，MySQL 用 ON DUPLICATE KEY UPDATE；
// 列名按字典序输出（保证幂等可重现）；MySQL 下 EXCLUDED.col 自动改写为 VALUES(col)，
// 大小写不敏感；CURRENT_TIMESTAMP 等非 EXCLUDED 表达式原样保留。
// TestDialectUpsert 负责TestDialectUpsert相关处理。
func TestDialectUpsert(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		name     string
		d        Dialect
		conflict []string
		updates  map[string]string
		want     string
	}{
		{
			name:     "sqlite basic",
			d:        DialectSQLite,
			conflict: []string{"id"},
			updates:  map[string]string{"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP"},
			want:     " ON CONFLICT (id) DO UPDATE SET updated_at=CURRENT_TIMESTAMP, value=EXCLUDED.value",
		},
		{
			name:     "postgres same syntax as sqlite",
			d:        DialectPostgres,
			conflict: []string{"order_id"},
			updates:  map[string]string{"status": "EXCLUDED.status"},
			want:     " ON CONFLICT (order_id) DO UPDATE SET status=EXCLUDED.status",
		},
		{
			name:     "mysql rewrites EXCLUDED to VALUES",
			d:        DialectMySQL,
			conflict: []string{"id"},
			updates:  map[string]string{"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP"},
			want:     " ON DUPLICATE KEY UPDATE updated_at=CURRENT_TIMESTAMP, value=VALUES(value)",
		},
		{
			name:     "mysql lowercase excluded also rewritten",
			d:        DialectMySQL,
			conflict: []string{"id"},
			updates:  map[string]string{"value": "excluded.value"},
			want:     " ON DUPLICATE KEY UPDATE value=VALUES(value)",
		},
		{
			name:     "multi-column conflict key",
			d:        DialectSQLite,
			conflict: []string{"cookie_id", "item_id"},
			updates:  map[string]string{"title": "EXCLUDED.title"},
			want:     " ON CONFLICT (cookie_id, item_id) DO UPDATE SET title=EXCLUDED.title",
		},
		{
			name:     "empty conflict cols returns empty",
			d:        DialectSQLite,
			conflict: nil,
			updates:  map[string]string{"v": "EXCLUDED.v"},
			want:     "",
		},
		{
			name:     "empty updates returns empty",
			d:        DialectSQLite,
			conflict: []string{"id"},
			updates:  nil,
			want:     "",
		},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got 保存got，供当前处理流程使用
			got := DialectUpsert(c.d, c.conflict, c.updates)
			if got != c.want {
				t.Errorf("DialectUpsert(%s) = %q; want %q", c.d, got, c.want)
			}
		})
	}
}

// TestDialectInsertIgnore 断言“冲突即忽略”子句：MySQL 恒空（前缀模式负责），
// SQLite/Postgres 生成 ON CONFLICT...DO NOTHING，空 conflictCols 时返回空。
// TestDialectInsertIgnore 负责TestDialectInsertIgnore相关处理。
func TestDialectInsertIgnore(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		d        Dialect
		conflict []string
		want     string
	}{
		{DialectSQLite, []string{"id"}, " ON CONFLICT (id) DO NOTHING"},
		{DialectPostgres, []string{"order_id"}, " ON CONFLICT (order_id) DO NOTHING"},
		{DialectMySQL, []string{"id"}, ""},
		{DialectMySQL, nil, ""},
		{DialectSQLite, nil, ""},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		// got 保存got，供当前处理流程使用
		got := DialectInsertIgnore(c.d, c.conflict)
		if got != c.want {
			t.Errorf("DialectInsertIgnore(%s, %v) = %q; want %q", c.d, c.conflict, got, c.want)
		}
	}
}

// TestDialectInsertIgnorePrefix MySQL 走 INSERT IGNORE，其余走 INSERT。
func TestDialectInsertIgnorePrefix(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := DialectInsertIgnorePrefix(DialectMySQL); got != "INSERT IGNORE" {
		t.Errorf("mysql prefix = %q; want INSERT IGNORE", got)
	}
	if // got 保存got，供当前处理流程使用
	got := DialectInsertIgnorePrefix(DialectSQLite); got != "INSERT" {
		t.Errorf("sqlite prefix = %q; want INSERT", got)
	}
	if // got 保存got，供当前处理流程使用
	got := DialectInsertIgnorePrefix(DialectPostgres); got != "INSERT" {
		t.Errorf("postgres prefix = %q; want INSERT", got)
	}
}

// TestDialectQuote Postgres 用双引号，SQLite/MySQL 用反引号。
func TestDialectQuote(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		d    Dialect
		name string
		want string
	}{
		{DialectSQLite, "users", "`users`"},
		{DialectMySQL, "users", "`users`"},
		{DialectPostgres, "users", `"users"`},
		{DialectPostgres, "order_id", `"order_id"`},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		if // got 保存got，供当前处理流程使用
		got := DialectQuote(c.d, c.name); got != c.want {
			t.Errorf("DialectQuote(%s, %q) = %q; want %q", c.d, c.name, got, c.want)
		}
	}
}

// TestSortStrings 确认 sortStrings 原地升序排序（被 DialectUpsert 用于稳定输出顺序）。
func TestSortStrings(t *testing.T) {
	// in 保存in，供当前处理流程使用
	in := []string{"banana", "apple", "cherry", "apple"}
	sortStrings(in)
	// want 保存want，供当前处理流程使用
	want := "apple,apple,banana,cherry"
	if // got 保存got，供当前处理流程使用
	got := strings.Join(in, ","); got != want {
		t.Errorf("sortStrings = %q; want %q", got, want)
	}
}
