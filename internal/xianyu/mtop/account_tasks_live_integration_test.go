package mtop

import (
	"context"
	"os"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestLivePolishAccount is opt-in because it calls the real Xianyu APIs and
// changes the selected account's daily polish state. It never logs credentials.
func TestLivePolishAccount(t *testing.T) {
	if os.Getenv("TEST_XIANYU_LIVE") != "1" {
		t.Skip("set TEST_XIANYU_LIVE=1 to run against a real account")
	}
	dbURL := os.Getenv("TEST_XIANYU_DB_URL")
	accountID := os.Getenv("TEST_XIANYU_ACCOUNT_ID")
	if dbURL == "" || accountID == "" {
		t.Fatal("TEST_XIANYU_DB_URL and TEST_XIANYU_ACCOUNT_ID are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	database, dialect, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()
	store := db.NewStore(database, dialect)
	cookies, err := store.Cookies.GetValue(ctx, accountID)
	if err != nil {
		t.Fatalf("read account credentials: %v", err)
	}
	client := &ClientImpl{}
	items, err := client.FetchAllItems(ctx, cookies, 20, 20)
	if err != nil {
		t.Fatalf("fetch live items: %v", err)
	}
	current := cookies
	if items.UpdatedCookies != "" {
		current = items.UpdatedCookies
	}
	for _, item := range items.Items {
		result, polishErr := client.PolishItem(ctx, current, item.ID)
		if polishErr != nil || result == nil || !result.Success {
			t.Fatalf("polish item %s: result=%+v err=%v", item.ID, result, polishErr)
		}
		if result.UpdatedCookies != "" {
			current = result.UpdatedCookies
		}
	}
	t.Logf("live polish responses accepted for %d items", len(items.Items))
}
