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
// TestLivePolishAccount 封装TestLivePolish账号业务协调。
func TestLivePolishAccount(t *testing.T) {
	if os.Getenv("TEST_XIANYU_LIVE") != "1" {
		t.Skip("set TEST_XIANYU_LIVE=1 to run against a real account")
	}
	// dbURL 用于本次流程后续判断的dbURL
	dbURL := os.Getenv("TEST_XIANYU_DB_URL")
	// accountID 用于本次流程后续判断的账号ID
	accountID := os.Getenv("TEST_XIANYU_ACCOUNT_ID")
	if dbURL == "" || accountID == "" {
		t.Fatal("TEST_XIANYU_DB_URL and TEST_XIANYU_ACCOUNT_ID are required")
	}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// database、dialect、err 用于本次流程后续判断的database、dialect、err
	database, dialect, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()
	// store 用于本次流程后续判断的store
	store := db.NewStore(database, dialect)
	// cookies、err 用于本次流程后续判断的cookies、err
	cookies, err := store.Cookies.GetValue(ctx, accountID)
	if err != nil {
		t.Fatalf("read account credentials: %v", err)
	}
	// client 用于本次流程后续判断的client
	client := &ClientImpl{}
	// items、err 用于本次流程后续判断的items、err
	items, err := client.FetchAllItems(ctx, cookies, 20, 20)
	if err != nil {
		t.Fatalf("fetch live items: %v", err)
	}
	// current 用于本次流程后续判断的current
	current := cookies
	if items.UpdatedCookies != "" {
		current = items.UpdatedCookies
	}
	// item 表示当前遍历过程中的商品
	for _, item := range items.Items {
		// result、polishErr 用于本次流程后续判断的result、polishErr
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
