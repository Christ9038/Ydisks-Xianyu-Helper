// Package main 交互式初始化管理员账号。
// 沿用 Fork 安全基线：不创建默认口令，密码不回显、二次确认，bcrypt 哈希。
//
// 用法：go run ./cmd/init-admin -db data/xianyu_data.db
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

func main() {
	dbPath := flag.String("db", "data/xianyu_data.db", "SQLite 数据库路径（兼容旧用法）")
	dbURL := flag.String("db-url", "", "数据库连接 URL（sqlite:// mysql:// postgres://），优先级高于 -db；也可用 DATABASE_URL 环境变量")
	flag.Parse()

	// 解析数据库连接：DATABASE_URL > -db-url > -db。
	resolved := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if resolved == "" {
		resolved = strings.TrimSpace(*dbURL)
	}
	if resolved == "" {
		resolved = *dbPath
	}

	if err := run(context.Background(), resolved, bufio.NewReader(os.Stdin), os.Stdout); err != nil {
		fatalf("初始化管理员失败: %v", err)
	}
}

// run 执行一次初始化流程。把数据库、输入和输出显式注入后，CLI 的核心行为
// 可以在临时数据库中回归测试，而不会调用 os.Exit 或污染用户数据。
func run(ctx context.Context, resolved string, reader *bufio.Reader, out io.Writer) error {
	database, dialect, err := db.Open(ctx, resolved)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer database.Close()
	store := db.NewStore(database, dialect)

	existing, err := store.Users.GetAdmin(ctx)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("查询 admin 失败: %w", err)
	}

	if existing != nil {
		fmt.Fprintf(out, "管理员用户 %s 已存在\n", existing.Username)
		fmt.Fprint(out, "是否重置管理员密码？(y/N): ")
		ans, _ := reader.ReadString('\n')
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
			fmt.Fprintln(out, "跳过初始化")
			return nil
		}
		pw := promptPasswordTwice(reader, out)
		if _, err := auth.InitAdmin(ctx, store, "", pw); err != nil {
			return fmt.Errorf("重置 admin 密码失败: %w", err)
		}
		fmt.Fprintf(out, "重置完成：已更新 %s 的密码\n", existing.Username)
		return nil
	}

	fmt.Fprintln(out, "=== 初始化管理员账号（CLI）===")
	fmt.Fprint(out, "请输入管理员邮箱: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("邮箱不能为空")
	}
	pw := promptPasswordTwice(reader, out)

	created, err := auth.InitAdmin(ctx, store, email, pw)
	if err != nil {
		return fmt.Errorf("创建 admin 用户失败：%w", err)
	}
	if created {
		fmt.Fprintln(out, "初始化完成：已创建 admin 用户")
	}
	return nil
}

// promptPasswordTwice 两次输入密码（不回显）并校验一致性。
func promptPasswordTwice(reader *bufio.Reader, out io.Writer) string {
	for {
		p1 := readPasswordNoEcho(reader, "请输入管理员密码（不回显）: ", out)
		if p1 == "" {
			fmt.Fprintln(out, "密码不能为空")
			continue
		}
		p2 := readPasswordNoEcho(reader, "请再次输入管理员密码（不回显）: ", out)
		if p1 != p2 {
			fmt.Fprintln(out, "两次输入不一致，请重试")
			continue
		}
		return p1
	}
}

// readPasswordNoEcho 不回显读取密码。非 TTY 时回退到普通读取（如管道），
// 共用同一 bufio.Reader 避免缓冲吞掉后续输入。
func readPasswordNoEcho(reader *bufio.Reader, prompt string, out io.Writer) string {
	fmt.Fprint(out, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			fatalf("读取密码失败: %v", err)
		}
		return strings.TrimSpace(string(b))
	}
	// 非 TTY 回退：共用传入的 reader。
	s, _ := reader.ReadString('\n')
	fmt.Println()
	return strings.TrimSpace(s)
}

func isNotFound(err error) bool { return errors.Is(err, db.ErrNotFound) }

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
