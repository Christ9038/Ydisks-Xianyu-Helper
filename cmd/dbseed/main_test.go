package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestSeedFromSQLiteCopiesMetadataAndSanitizesSecrets 封装TestSeedFromSQLiteCopiesMetadataAndSanitizesSecrets业务协调。
func TestSeedFromSQLiteCopiesMetadataAndSanitizesSecrets(t *testing.T) {
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// sourceDB、sourceDialect、err 用于本次流程后续判断的sourceDB、sourceDialect、err
	sourceDB, sourceDialect, err := db.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceDB.Close()
	// source 用于本次流程后续判断的source
	source := db.NewStore(sourceDB, sourceDialect)
	source.Users.Create(ctx, "source", "source@example.com", "pw")
	// user 用于本次流程后续判断的用户
	user, _ := source.Users.GetByUsername(ctx, "source")
	source.Cookies.Save(ctx, "real-account", "unb=real-secret; _m_h5_tk=secret;", user.ID)
	source.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "real-account", ItemID: "real-item", ItemTitle: "真实商品", ItemPrice: "19.90"})
	source.Orders.Upsert(ctx, "real-order", db.OrderUpsertOpts{CookieID: "real-account", ItemID: "real-item", BuyerID: "real-buyer", Amount: "19.90", OrderStatus: "completed"})
	source.Cards.Create(ctx, &db.CardFull{Name: "真实卡密", Type: "data", DataContent: "SECRET-CODE", Enabled: true, UserID: user.ID})

	// targetDB、targetDialect、err 用于本次流程后续判断的targetDB、targetDialect、err
	targetDB, targetDialect, err := db.Open(ctx, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer targetDB.Close()
	// target 用于本次流程后续判断的target
	target := db.NewStore(targetDB, targetDialect)
	// options 用于本次流程后续判断的options
	options := seedOptions{Username: "fixture", Password: "fixture-password", AdminUsername: "fixture-admin", AdminPassword: "fixture-admin-password", Limit: 10}
	// result、err 用于本次流程后续判断的result、err
	result, err := seedFromSQLite(ctx, sourceDB, target, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Items != 1 || result.Orders != 1 || result.Cards != 1 {
		t.Fatalf("result=%+v", result)
	}
	// cookie 用于本次流程后续判断的登录凭证
	cookie, _ := target.Cookies.GetValue(ctx, "docker-fixture-account")
	if strings.Contains(cookie, "real-secret") || strings.Contains(cookie, "secret") {
		t.Fatalf("source cookie leaked: %q", cookie)
	}
	// fixtureUser 用于本次流程后续判断的fixture用户
	fixtureUser, _ := target.Users.GetByUsername(ctx, "fixture")
	// fixtureAdmin、adminErr 用于本次流程后续判断的fixtureAdmin、adminErr
	fixtureAdmin, adminErr := target.Users.GetByUsername(ctx, "fixture-admin")
	if adminErr != nil || !fixtureAdmin.IsAdmin || !fixtureAdmin.IsActive {
		t.Fatalf("fixture admin missing or inactive: user=%+v err=%v", fixtureAdmin, adminErr)
	}
	// cards 用于本次流程后续判断的卡密列表
	cards, _ := target.Cards.AllForUser(ctx, fixtureUser.ID)
	if len(cards) != 1 || strings.Contains(cards[0].DataContent, "SECRET-CODE") {
		t.Fatalf("source card secret leaked: %+v", cards)
	}
	// invalidOrder、err 用于本次流程后续判断的invalidOrder、err
	invalidOrder, err := target.Orders.Get(ctx, "docker-invalid-amount")
	if err != nil || invalidOrder.Amount != "not-a-number" {
		t.Fatalf("invalid amount fixture missing: order=%+v err=%v", invalidOrder, err)
	}

	// second、err 用于本次流程后续判断的second、err
	second, err := seedFromSQLite(ctx, sourceDB, target, options)
	if err != nil {
		t.Fatalf("second idempotent seed: %v", err)
	}
	if second != result {
		t.Fatalf("second seed result=%+v want %+v", second, result)
	}
	cards, _ = target.Cards.AllForUser(ctx, fixtureUser.ID)
	if len(cards) != 1 {
		t.Fatalf("second seed should replace fixture cards, got %d", len(cards))
	}
}
