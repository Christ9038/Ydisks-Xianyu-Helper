package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

func TestSeedFromSQLiteCopiesMetadataAndSanitizesSecrets(t *testing.T) {
	ctx := context.Background()
	sourceDB, sourceDialect, err := db.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceDB.Close()
	source := db.NewStore(sourceDB, sourceDialect)
	source.Users.Create(ctx, "source", "source@example.com", "pw")
	user, _ := source.Users.GetByUsername(ctx, "source")
	source.Cookies.Save(ctx, "real-account", "unb=real-secret; _m_h5_tk=secret;", user.ID)
	source.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "real-account", ItemID: "real-item", ItemTitle: "真实商品", ItemPrice: "19.90"})
	source.Orders.Upsert(ctx, "real-order", db.OrderUpsertOpts{CookieID: "real-account", ItemID: "real-item", BuyerID: "real-buyer", Amount: "19.90", OrderStatus: "completed"})
	source.Cards.Create(ctx, &db.CardFull{Name: "真实卡密", Type: "data", DataContent: "SECRET-CODE", Enabled: true, UserID: user.ID})

	targetDB, targetDialect, err := db.Open(ctx, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer targetDB.Close()
	target := db.NewStore(targetDB, targetDialect)
	result, err := seedFromSQLite(ctx, sourceDB, target, seedOptions{Username: "fixture", Password: "fixture-password", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Items != 1 || result.Orders != 1 || result.Cards != 1 {
		t.Fatalf("result=%+v", result)
	}
	cookie, _ := target.Cookies.GetValue(ctx, "docker-fixture-account")
	if strings.Contains(cookie, "real-secret") || strings.Contains(cookie, "secret") {
		t.Fatalf("source cookie leaked: %q", cookie)
	}
	fixtureUser, _ := target.Users.GetByUsername(ctx, "fixture")
	cards, _ := target.Cards.AllForUser(ctx, fixtureUser.ID)
	if len(cards) != 1 || strings.Contains(cards[0].DataContent, "SECRET-CODE") {
		t.Fatalf("source card secret leaked: %+v", cards)
	}

	second, err := seedFromSQLite(ctx, sourceDB, target, seedOptions{Username: "fixture", Password: "fixture-password", Limit: 10})
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
