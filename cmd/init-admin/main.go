// Package main 交互式初始化管理员账号。
// 沿用 Fork 安全基线：不创建默认口令，密码不回显、二次确认，bcrypt 哈希。
//
// 用法：go run ./cmd/init-admin -db data/xianyu_data.db
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
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

	ctx := context.Background()
	database, dialect, err := db.Open(ctx, resolved)
	if err != nil {
		fatalf("打开数据库失败: %v", err)
	}
	defer database.Close()
	store := db.NewStore(database, dialect)

	reader := bufio.NewReader(os.Stdin)

	existing, err := store.Users.GetAdmin(ctx)
	if err != nil && !isNotFound(err) {
		fatalf("查询 admin 失败: %v", err)
	}

	if existing != nil {
		fmt.Printf("管理员用户 %s 已存在\n", existing.Username)
		fmt.Print("是否重置管理员密码？(y/N): ")
		ans, _ := reader.ReadString('\n')
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
			fmt.Println("跳过初始化")
			return
		}
		pw := promptPasswordTwice(reader)
		if _, err := auth.InitAdmin(ctx, store, "", pw); err != nil {
			fatalf("重置 admin 密码失败: %v", err)
		}
		fmt.Printf("重置完成：已更新 %s 的密码\n", existing.Username)
		return
	}

	fmt.Println("=== 初始化管理员账号（CLI）===")
	fmt.Print("请输入管理员邮箱: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)
	if email == "" {
		fatalf("邮箱不能为空")
	}
	pw := promptPasswordTwice(reader)

	created, err := auth.InitAdmin(ctx, store, email, pw)
	if err != nil {
		fatalf("创建 admin 用户失败：%v", err)
	}
	if created {
		fmt.Println("初始化完成：已创建 admin 用户")
	}
}

// promptPasswordTwice 两次输入密码（不回显）并校验一致性。
func promptPasswordTwice(reader *bufio.Reader) string {
	for {
		p1 := readPasswordNoEcho(reader, "请输入管理员密码（不回显）: ")
		if p1 == "" {
			fmt.Println("密码不能为空")
			continue
		}
		p2 := readPasswordNoEcho(reader, "请再次输入管理员密码（不回显）: ")
		if p1 != p2 {
			fmt.Println("两次输入不一致，请重试")
			continue
		}
		return p1
	}
}

// readPasswordNoEcho 不回显读取密码。非 TTY 时回退到普通读取（如管道），
// 共用同一 bufio.Reader 避免缓冲吞掉后续输入。
func readPasswordNoEcho(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
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

func isNotFound(err error) bool { return err == db.ErrNotFound }

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
