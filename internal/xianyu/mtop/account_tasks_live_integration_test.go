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
// TestLivePolishAccount 负责TestLivePolish账号相关处理。
func TestLivePolishAccount(t *testing.T) {
	if os.Getenv("TEST_XIANYU_LIVE") != "1" {
		t.Skip("set TEST_XIANYU_LIVE=1 to run against a real account")
	}
	// dbURL 保存dbURL，供当前处理流程使用
	dbURL := os.Getenv("TEST_XIANYU_DB_URL")
	// accountID 保存账号ID，供当前处理流程使用
	accountID := os.Getenv("TEST_XIANYU_ACCOUNT_ID")
	if dbURL == "" || accountID == "" {
		t.Fatal("TEST_XIANYU_DB_URL and TEST_XIANYU_ACCOUNT_ID are required")
	}
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// database、dialect、err 保存database、dialect、err，供当前处理流程使用
	database, dialect, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()
	// store 保存store，供当前处理流程使用
	store := db.NewStore(database, dialect)
	// cookies、err 保存cookies、err，供当前处理流程使用
	cookies, err := store.Cookies.GetValue(ctx, accountID)
	if err != nil {
		t.Fatalf("read account credentials: %v", err)
	}
	// client 保存client，供当前处理流程使用
	client := &ClientImpl{}
	// items、err 保存items、err，供当前处理流程使用
	items, err := client.FetchAllItems(ctx, cookies, 20, 20)
	if err != nil {
		t.Fatalf("fetch live items: %v", err)
	}
	// current 保存current，供当前处理流程使用
	current := cookies
	if items.UpdatedCookies != "" {
		current = items.UpdatedCookies
	}
	// item 表示当前遍历过程中的商品
	for _, item := range items.Items {
		// result、polishErr 保存result、polishErr，供当前处理流程使用
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
