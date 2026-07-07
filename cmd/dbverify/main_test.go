package main

import "testing"

// TestMaskURL 验证带凭证的数据库 URL 脱敏：保留 scheme + host，密码替换为 ***。
func TestMaskURL(t *testing.T) {
	cases := map[string]string{
		"mysql://user:secret@tcp(host:3306)/db?x=1":      "mysql://***@tcp(host:3306)/db?x=1",
		"postgres://user:pass@host:5432/db":              "postgres://***@host:5432/db",
		"postgresql://u:p@h:5432/d":                      "postgresql://***@h:5432/d",
		"sqlite://data/x.db":                             "sqlite://data/x.db", // 无密码，原样
		"/local/path.db":                                 "/local/path.db",     // 非 URL，原样
	}
	for in, want := range cases {
		if got := maskURL(in); got != want {
			t.Errorf("maskURL(%q) = %q; want %q", in, got, want)
		}
	}
}
