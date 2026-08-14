// dbseed 从本地 SQLite 抽取少量业务数据，脱敏后写入目标 MySQL/Postgres。
// 它只用于 Docker 功能测试：不会复制真实 Cookie、买家 ID 或卡密内容。
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// seedOptions 保存seedOptions，供当前处理流程使用
type seedOptions struct {
	Username      string
	Password      string
	AdminUsername string
	AdminPassword string
	Limit         int
}

// seedResult 保存seed结果，供当前处理流程使用
type seedResult struct {
	Items  int
	Orders int
	Cards  int
}

// main 负责main相关处理。
func main() {
	// sourcePath 保存source路径，供当前处理流程使用
	sourcePath := flag.String("source", "/seed/xianyu_data.db", "源 SQLite 文件")
	// targetURL 保存targetURL，供当前处理流程使用
	targetURL := flag.String("target", "", "目标数据库 URL")
	// username 保存username，供当前处理流程使用
	username := flag.String("username", "docker_fixture", "测试登录用户名")
	// password 保存密码，供当前处理流程使用
	password := flag.String("password", "docker_fixture_password", "测试登录密码")
	// adminUsername 保存adminUsername，供当前处理流程使用
	adminUsername := flag.String("admin-username", "docker_admin", "测试管理员用户名")
	// adminPassword 保存admin密码，供当前处理流程使用
	adminPassword := flag.String("admin-password", "docker_admin_password", "测试管理员密码")
	// limit 保存上限，供当前处理流程使用
	limit := flag.Int("limit", 20, "每类最多抽取条数")
	flag.Parse()
	if strings.TrimSpace(*targetURL) == "" {
		fmt.Fprintln(os.Stderr, "必须提供 -target")
		os.Exit(2)
	}

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// source、err 保存source、err，供当前处理流程使用
	source, err := sql.Open("sqlite", "file:"+*sourcePath+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		fatalf("打开源 SQLite: %v", err)
	}
	defer source.Close()
	if // err 保存err，供当前处理流程使用
	err := source.PingContext(ctx); err != nil {
		fatalf("连接源 SQLite: %v", err)
	}
	// targetDB、dialect、err 保存targetDB、dialect、err，供当前处理流程使用
	targetDB, dialect, err := db.Open(ctx, *targetURL)
	if err != nil {
		fatalf("打开目标数据库: %v", err)
	}
	defer targetDB.Close()

	// result、err 保存result、err，供当前处理流程使用
	result, err := seedFromSQLite(ctx, source, db.NewStore(targetDB, dialect), seedOptions{
		Username:      *username,
		Password:      *password,
		AdminUsername: *adminUsername,
		AdminPassword: *adminPassword,
		Limit:         *limit,
	})
	if err != nil {
		fatalf("导入测试数据: %v", err)
	}
	fmt.Printf("数据库种子完成：items=%d orders=%d cards=%d user=%s\n", result.Items, result.Orders, result.Cards, *username)
}

// seedFromSQLite 负责seedFromSQLite相关处理。
func seedFromSQLite(ctx context.Context, source *sql.DB, target *db.Store, options seedOptions) (seedResult, error) {
	if options.Limit <= 0 || options.Limit > 100 {
		options.Limit = 20
	}
	if options.Username == "" || options.Password == "" {
		return seedResult{}, fmt.Errorf("测试用户名和密码不能为空")
	}
	if options.AdminUsername == "" {
		options.AdminUsername = "docker_admin"
	}
	if options.AdminPassword == "" {
		options.AdminPassword = "docker_admin_password"
	}
	if // err 保存err，供当前处理流程使用
	err := ensureFixtureAdmin(ctx, target, options.AdminUsername, options.AdminPassword); err != nil {
		return seedResult{}, err
	}
	// user、err 保存user、err，供当前处理流程使用
	user, err := prepareFixtureUser(ctx, target, options)
	if err != nil {
		return seedResult{}, err
	}
	// accountID 保存账号ID，供当前处理流程使用
	const accountID = "docker-fixture-account"
	if // err 保存err，供当前处理流程使用
	err := target.Cookies.Save(ctx, accountID, "unb=docker-fixture; _m_h5_tk=fixture_1;", user.ID); err != nil {
		return seedResult{}, err
	}
	if // err 保存err，供当前处理流程使用
	err := target.Cookies.SetStatusWithReason(ctx, accountID, false, "Docker 脱敏测试账号"); err != nil {
		return seedResult{}, err
	}

	// result 保存结果，供当前处理流程使用
	result := seedResult{}
	// itemMap、count、err 保存商品Map、count、err，供当前处理流程使用
	itemMap, count, err := seedItems(ctx, source, target, accountID, options.Limit)
	if err != nil {
		return result, err
	}
	result.Items = count
	result.Orders, err = seedOrders(ctx, source, target, accountID, itemMap, options.Limit)
	if err != nil {
		return result, err
	}
	// 保留一条历史脏金额，用于验证三种数据库下统计接口都能安全跳过而不是 500。
	fixtureItemID := ""
	// id 表示当前遍历过程中的标识
	for _, id := range itemMap {
		fixtureItemID = id
		break
	}
	if fixtureItemID != "" {
		if // err 保存err，供当前处理流程使用
		err := target.Orders.Upsert(ctx, "docker-invalid-amount", db.OrderUpsertOpts{
			CookieID: accountID, ItemID: fixtureItemID, BuyerID: "docker-buyer-invalid",
			Quantity: "1", OrderStatus: "completed",
		}); err != nil {
			return result, err
		}
		// 直接写入一条模拟升级前遗留的脏数据；正常仓储写入口会拒绝该金额。
		if _, err := target.DB.ExecContext(ctx, `UPDATE orders SET amount='not-a-number' WHERE order_id='docker-invalid-amount'`); err != nil {
			return result, err
		}
	}
	result.Cards, err = seedCards(ctx, source, target, user.ID, options.Limit)
	return result, err
}

// ensureFixtureAdmin 负责ensureFixtureAdmin相关处理。
func ensureFixtureAdmin(ctx context.Context, target *db.Store, username, password string) error {
	// user、err 保存user、err，供当前处理流程使用
	user, err := target.Users.GetByUsername(ctx, username)
	if errors.Is(err, db.ErrNotFound) {
		// created、createErr 保存created、createErr，供当前处理流程使用
		created, createErr := target.Users.Create(ctx, username, username+"@docker.test", password)
		if createErr != nil {
			return fmt.Errorf("创建测试管理员: %w", createErr)
		}
		if !created {
			return fmt.Errorf("创建测试管理员失败：用户名或邮箱已存在")
		}
	} else if err != nil {
		return fmt.Errorf("查询测试管理员: %w", err)
	} else {
		// updated、updateErr 保存updated、updateErr，供当前处理流程使用
		updated, updateErr := target.Users.UpdatePassword(ctx, username, password)
		if updateErr != nil || !updated {
			return fmt.Errorf("重置测试管理员密码: updated=%v err=%v", updated, updateErr)
		}
	}
	if // err 保存err，供当前处理流程使用
	err := target.Users.SetAdmin(ctx, username); err != nil {
		return fmt.Errorf("设置测试管理员: %w", err)
	}
	if user != nil {
		_, err = target.DB.ExecContext(ctx, `UPDATE users SET is_active=1 WHERE id=?`, user.ID)
	} else {
		_, err = target.DB.ExecContext(ctx, `UPDATE users SET is_active=1 WHERE username=?`, username)
	}
	if err != nil {
		return fmt.Errorf("启用测试管理员: %w", err)
	}
	return nil
}

// prepareFixtureUser 负责prepareFixture用户相关处理。
func prepareFixtureUser(ctx context.Context, target *db.Store, options seedOptions) (*db.User, error) {
	// user、err 保存user、err，供当前处理流程使用
	user, err := target.Users.GetByUsername(ctx, options.Username)
	if errors.Is(err, db.ErrNotFound) {
		// created、createErr 保存created、createErr，供当前处理流程使用
		created, createErr := target.Users.Create(ctx, options.Username, options.Username+"@docker.test", options.Password)
		if createErr != nil {
			return nil, fmt.Errorf("创建测试用户: %w", createErr)
		}
		if !created {
			return nil, fmt.Errorf("创建测试用户失败：用户名或邮箱已存在")
		}
		return target.Users.GetByUsername(ctx, options.Username)
	}
	if err != nil {
		return nil, fmt.Errorf("查询测试用户: %w", err)
	}

	// 测试用户可能来自持久卷中的上一次执行。先移除其自动化规则，释放
	// card_id 的 RESTRICT 外键，再清理账号数据和卡密，保留用户主键并重置密码。
	// query 表示当前遍历过程中的查询
	for _, query := range []string{
		`DELETE FROM automation_rules WHERE user_id=?`,
		`DELETE FROM cookies WHERE user_id=?`,
		`DELETE FROM cards WHERE user_id=?`,
	} {
		if // err 保存err，供当前处理流程使用
		_, err := target.DB.ExecContext(ctx, query, user.ID); err != nil {
			return nil, fmt.Errorf("清理上次测试数据: %w", err)
		}
	}
	// updated、err 保存updated、err，供当前处理流程使用
	updated, err := target.Users.UpdatePassword(ctx, options.Username, options.Password)
	if err != nil || !updated {
		return nil, fmt.Errorf("重置测试用户密码: updated=%v err=%v", updated, err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := target.DB.ExecContext(ctx, `UPDATE users SET is_active=1,is_admin=0 WHERE id=?`, user.ID); err != nil {
		return nil, fmt.Errorf("重置测试用户状态: %w", err)
	}
	return target.Users.GetByUsername(ctx, options.Username)
}

// seedItems 负责seed商品列表相关处理。
func seedItems(ctx context.Context, source *sql.DB, target *db.Store, accountID string, limit int) (map[string]string, int, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := source.QueryContext(ctx, `SELECT item_id,COALESCE(item_title,''),COALESCE(item_description,''),
		COALESCE(item_category,''),COALESCE(item_price,'') FROM item_info ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	// mapping 保存mapping，供当前处理流程使用
	mapping := make(map[string]string)
	// count 保存数量，供当前处理流程使用
	count := 0
	for rows.Next() {
		// sourceID、title、description、category、price 保存sourceID、title、description、category、price，供当前处理流程使用
		var sourceID, title, description, category, price string
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&sourceID, &title, &description, &category, &price); err != nil {
			return nil, count, err
		}
		// fixtureID 保存fixtureID，供当前处理流程使用
		fixtureID := fmt.Sprintf("docker-item-%03d", count+1)
		if // err 保存err，供当前处理流程使用
		err := target.Items.Upsert(ctx, &db.ItemInfoRow{
			CookieID: accountID, ItemID: fixtureID, ItemTitle: fallback(title, "SQLite 测试商品"),
			ItemDescription: description, ItemCategory: category, ItemPrice: price,
		}); err != nil {
			return nil, count, err
		}
		mapping[sourceID] = fixtureID
		count++
	}
	return mapping, count, rows.Err()
}

// seedOrders 负责seed订单列表相关处理。
func seedOrders(ctx context.Context, source *sql.DB, target *db.Store, accountID string, items map[string]string, limit int) (int, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := source.QueryContext(ctx, `SELECT order_id,COALESCE(item_id,''),COALESCE(quantity,'1'),
		COALESCE(amount,'0'),COALESCE(order_status,'unknown'),COALESCE(spec_name,''),COALESCE(spec_value,'')
		FROM orders ORDER BY updated_at DESC LIMIT ?`, limit*4)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 保存数量，供当前处理流程使用
	count := 0
	for rows.Next() && count < limit {
		// sourceOrderID、sourceItemID、quantity、amount、status、specName、specValue 保存source订单ID、source商品ID、quantity、amount、status、specName、spec值，供当前处理流程使用
		var sourceOrderID, sourceItemID, quantity, amount, status, specName, specValue string
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&sourceOrderID, &sourceItemID, &quantity, &amount, &status, &specName, &specValue); err != nil {
			return count, err
		}
		// itemID、ok 保存商品ID、ok，供当前处理流程使用
		itemID, ok := items[sourceItemID]
		if !ok {
			continue
		}
		if // normalized、valid 保存normalized、valid，供当前处理流程使用
		normalized, valid := db.NormalizeOrderAmount(amount); valid {
			amount = normalized
		} else {
			amount = ""
		}
		if // err 保存err，供当前处理流程使用
		err := target.Orders.Upsert(ctx, "docker-order-"+shortHash(sourceOrderID), db.OrderUpsertOpts{
			CookieID: accountID, ItemID: itemID, BuyerID: "docker-buyer", Quantity: quantity,
			Amount: amount, OrderStatus: status, SpecName: specName, SpecValue: specValue,
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

// seedCards 负责seed卡密列表相关处理。
func seedCards(ctx context.Context, source *sql.DB, target *db.Store, userID int64, limit int) (int, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := source.QueryContext(ctx, `SELECT COALESCE(name,''),type,COALESCE(delay_seconds,0)
		FROM cards ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 保存数量，供当前处理流程使用
	count := 0
	for rows.Next() {
		// name、cardType 保存name、card类型，供当前处理流程使用
		var name, cardType string
		// delay 保存延迟，供当前处理流程使用
		var delay int
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&name, &cardType, &delay); err != nil {
			return count, err
		}
		// card 保存卡密，供当前处理流程使用
		card := &db.CardFull{
			Name: fmt.Sprintf("[Docker测试] %s %d", fallback(name, "SQLite 卡密"), count+1),
			Type: "text", TextContent: "Docker 脱敏测试发货内容", Enabled: true,
			Description: "从本地 SQLite 元数据生成，不包含真实卡密", DelaySeconds: delay, UserID: userID,
		}
		if cardType == "data" {
			card.Type = "data"
			card.DataContent = fmt.Sprintf("DOCKER-FIXTURE-%03d-A\nDOCKER-FIXTURE-%03d-B", count+1, count+1)
		}
		if // err 保存err，供当前处理流程使用
		_, err := target.Cards.Create(ctx, card); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

// shortHash 负责shortHash相关处理。
func shortHash(value string) string {
	// sum 保存sum，供当前处理流程使用
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}

// fallback 负责fallback相关处理。
func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

// fatalf 负责fatalf相关处理。
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
