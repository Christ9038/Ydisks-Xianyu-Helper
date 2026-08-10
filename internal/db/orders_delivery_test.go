package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOrdersByCookiePageScansBeyondLegacyLimit(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cookieID := seedAccount(t, s)
	for i := 0; i < 1001; i++ {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO orders (order_id,cookie_id,order_status,created_at) VALUES (?,?,?,?)`,
			fmt.Sprintf("page-order-%04d", i), cookieID, "2", fmt.Sprintf("2026-01-%02d 00:00:00", i%28+1)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.Orders.ByCookiePage(ctx, cookieID, 500, 0)
	if err != nil || len(first) != 500 {
		t.Fatalf("first len=%d err=%v", len(first), err)
	}
	third, err := s.Orders.ByCookiePage(ctx, cookieID, 500, 1000)
	if err != nil || len(third) != 1 {
		t.Fatalf("third len=%d err=%v", len(third), err)
	}
}

func TestOrdersSoftDeleteMissingForCookie(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	for _, orderID := range []string{"seller-order", "buyer-order"} {
		if err := s.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cid, OrderStatus: "pending_ship"}); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.Orders.SoftDeleteMissingForCookie(ctx, cid, map[string]struct{}{"seller-order": {}})
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	if _, err := s.Orders.Get(ctx, "buyer-order"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing order should be hidden, err=%v", err)
	}
	if err := s.Orders.Upsert(ctx, "buyer-order", OrderUpsertOpts{CookieID: cid, OrderStatus: "pending_ship"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orders.Get(ctx, "buyer-order"); err != nil {
		t.Fatalf("reappeared order should be restored, err=%v", err)
	}
}

// seedAccount 在临时库里建好 admin 用户 + 一个账号（cookie），返回 (userID, cookieID)。
// 多数订单/自动化/卡券测试都需要这两层外键先就位。
func seedAccount(t *testing.T, s *Store) (int64, string) {
	t.Helper()
	ctx := context.Background()
	if ok, err := s.Users.Create(ctx, "admin", "admin@e.com", "pw"); err != nil || !ok {
		t.Fatalf("create admin: ok=%v err=%v", ok, err)
	}
	if err := s.Users.SetAdmin(ctx, "admin"); err != nil {
		t.Fatalf("set admin: %v", err)
	}
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	if err := s.Cookies.Save(ctx, "acc1", "cv=admin", admin.ID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	return admin.ID, "acc1"
}

// --- orders.go / orders_ext.go ---

// TestOrders_UpsertEmptyOrderID 空 order_id 应被拒绝。
func TestOrders_UpsertEmptyOrderID(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if err := s.Orders.Upsert(ctx, "", OrderUpsertOpts{ItemID: "i1"}); err == nil {
		t.Fatal("空 order_id 应报错")
	}
}

// TestOrders_UpsertNoFields 二次 Upsert 不带任何更新字段应直接返回 nil（无 UPDATE）。
func TestOrders_UpsertNoFields(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{ItemID: "i1", BuyerID: "b1", CookieID: cid, Amount: "9.9"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// 不带任何更新字段 → set 为空 → 直接返回。
	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{}); err != nil {
		t.Fatalf("no-field upsert: %v", err)
	}
	got, _ := s.Orders.Get(ctx, "o1")
	if got.ItemID != "i1" || got.Amount != "9.9" {
		t.Fatalf("字段被清空: %#v", got)
	}
}

func TestOrdersUpsertNormalizesAndRejectsAmountsAtRepositoryBoundary(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	if err := s.Orders.Upsert(ctx, "normalized-amount", OrderUpsertOpts{CookieID: cid, Amount: "¥1,200.50"}); err != nil {
		t.Fatal(err)
	}
	order, _ := s.Orders.Get(ctx, "normalized-amount")
	if order.Amount != "1200.50" {
		t.Fatalf("amount=%q", order.Amount)
	}
	if err := s.Orders.Upsert(ctx, "full-width-yen", OrderUpsertOpts{CookieID: cid, Amount: "￥12.50"}); err != nil {
		t.Fatal(err)
	}
	order, _ = s.Orders.Get(ctx, "full-width-yen")
	if order.Amount != "12.50" {
		t.Fatalf("full-width amount=%q", order.Amount)
	}
	if err := s.Orders.Upsert(ctx, "invalid-amount", OrderUpsertOpts{CookieID: cid, Amount: "1e3"}); err == nil {
		t.Fatal("scientific notation must be rejected")
	}
	if _, err := s.Orders.Get(ctx, "invalid-amount"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid amount left a partial order: %v", err)
	}
	for _, invalid := range []string{"1,2", "12,34", "1,,000", "¥¥1"} {
		if _, ok := NormalizeOrderAmount(invalid); ok {
			t.Fatalf("malformed grouped amount %q must be rejected", invalid)
		}
	}
}

func TestOrders_PatchCanExplicitlyClearFields(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	if err := s.Orders.Upsert(ctx, "patch-order", OrderUpsertOpts{
		CookieID: cid, ItemID: "item-1", BuyerID: "buyer-1", Amount: "19.9", ReceiverPhone: "13800000000",
	}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	shipped := true
	if err := s.Orders.Patch(ctx, "patch-order", OrderPatch{Amount: &empty, ReceiverPhone: &empty, SystemShipped: &shipped}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Orders.Get(ctx, "patch-order")
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != "" || got.ReceiverPhone != "" || !got.SystemShipped || got.ItemID != "item-1" {
		t.Fatalf("explicit clear/unmodified fields mismatch: %+v", got)
	}
}

func TestOrders_ListForUserSearchesAcrossJoinedFields(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	if err := s.Orders.Upsert(ctx, "search-order", OrderUpsertOpts{CookieID: cid, ItemID: "item-1", BuyerID: "Buyer-ABC", ReceiverName: "张三"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Items.Upsert(ctx, &ItemInfoRow{CookieID: cid, ItemID: "item-1", ItemTitle: "Special Product"}); err != nil {
		t.Fatal(err)
	}
	for _, search := range []string{"special", "buyer-abc", "张三", "search-order"} {
		rows, total, err := s.Orders.ListForUser(ctx, OrderListFilter{UserID: uid, Search: search, Limit: 20})
		if err != nil || total != 1 || len(rows) != 1 || rows[0].OrderID != "search-order" {
			t.Fatalf("search %q: rows=%+v total=%d err=%v", search, rows, total, err)
		}
	}
}

func TestOrders_UpsertRejectsCookieTakeover(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	userID, cid := seedAccount(t, s)
	if err := s.Cookies.Save(ctx, "acc2", "cv=other", userID); err != nil {
		t.Fatalf("save second cookie: %v", err)
	}

	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{ItemID: "i1", BuyerID: "b1", CookieID: cid}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{CookieID: "acc2", Amount: "99"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cookie takeover should be forbidden, got %v", err)
	}
	got, err := s.Orders.Get(ctx, "o1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CookieID != cid || got.Amount == "99" {
		t.Fatalf("order ownership/data changed after forbidden upsert: %#v", got)
	}
}

// TestOrders_UpsertAllFields 覆盖 Upsert 全部可更新字段（spec/qty/receiver 等）。
func TestOrders_UpsertAllFields(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{ItemID: "i1", BuyerID: "b1", CookieID: cid}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{
		SpecName: "红色", SpecValue: "L", Quantity: "2", Amount: "19.9",
		ReceiverName: "张三", ReceiverPhone: "13800000000", ReceiverAddr: "某地", ReceiverCity: "杭州",
		OrderStatus: "paid", ChatID: "chat1",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.Orders.Get(ctx, "o1")
	if got.SpecName != "红色" || got.SpecValue != "L" || got.Quantity != "2" || got.Amount != "19.9" ||
		got.ReceiverName != "张三" || got.ReceiverPhone != "13800000000" ||
		got.ReceiverAddr != "某地" || got.ReceiverCity != "杭州" ||
		got.OrderStatus != "paid" || got.ChatID != "chat1" {
		t.Fatalf("字段未更新: %#v", got)
	}

	// SystemShipped true/false 都覆盖。
	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{SystemShipped: boolPtr(true)}); err != nil {
		t.Fatalf("set shipped: %v", err)
	}
	got, _ = s.Orders.Get(ctx, "o1")
	if !got.SystemShipped {
		t.Fatal("system_shipped 应为 true")
	}
	if err := s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{SystemShipped: boolPtr(false)}); err != nil {
		t.Fatalf("clear shipped: %v", err)
	}
	got, _ = s.Orders.Get(ctx, "o1")
	if got.SystemShipped {
		t.Fatal("system_shipped 应为 false")
	}
}

func TestOrdersUpsertDoesNotRegressAdvancedStatus(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	if err := s.Orders.Upsert(ctx, "status-order", OrderUpsertOpts{CookieID: cid, OrderStatus: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Orders.Upsert(ctx, "status-order", OrderUpsertOpts{CookieID: cid, OrderStatus: "pending_ship", Amount: "10"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Orders.Get(ctx, "status-order")
	if err != nil {
		t.Fatal(err)
	}
	if got.OrderStatus != "completed" || got.Amount != "10" {
		t.Fatalf("stale event regressed status or lost other facts: %+v", got)
	}
	if !shouldUpdateOrderStatus("pending_ship", "shipped") || shouldUpdateOrderStatus("completed", "shipped") {
		t.Fatal("status transition guard mismatch")
	}
}

// TestOrders_GetNotFound 未找到返回 ErrNotFound。
func TestOrders_GetNotFound(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	if _, err := s.Orders.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// TestOrders_ByCookie + NormalizeOrderStatus + AllTitles。
func TestOrders_ByCookieAndTitles(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// 三条订单，按 created_at 倒序。
	for i, oid := range []string{"o1", "o2", "o3"} {
		if err := s.Orders.Upsert(ctx, oid, OrderUpsertOpts{
			ItemID: "item_" + oid, BuyerID: "b1", CookieID: cid, Amount: "1.0",
			OrderStatus: NormalizeOrderStatus("2"), // pending_ship
		}); err != nil {
			t.Fatalf("upsert %s: %v", oid, err)
		}
		// created_at 由 DB 写入；人为错开以保证排序稳定。
		_, _ = s.DB.ExecContext(ctx, `UPDATE orders SET created_at=? WHERE order_id=?`,
			time.Now().Add(time.Duration(i)*time.Second).Format("2006-01-02 15:04:05"), oid)
	}

	// limit<=0 走默认 1000。
	rows, err := s.Orders.ByCookie(ctx, cid, 0)
	if err != nil {
		t.Fatalf("ByCookie: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("ByCookie len=%d want 3", len(rows))
	}
	// 倒序：最新（o3，created_at 最大）应排第一。
	if rows[0].OrderID != "o3" {
		t.Fatalf("ByCookie 顺序错误: first=%s want o3", rows[0].OrderID)
	}
	// OrderRow 的字段应回填。
	if rows[0].ItemID != "item_o3" || rows[0].CookieID != cid || rows[0].Amount != "1.0" {
		t.Fatalf("ByCookie 行字段: %#v", rows[0])
	}
	// 默认 system_shipped=false（未 Upsert 设置过）。
	if rows[0].SystemShipped {
		t.Fatal("ByCookie 默认 system_shipped 应为 false")
	}

	// AllTitles：item_info 无数据时应返回空 map（不报错）。
	titles, err := s.Items.AllTitles(ctx)
	if err != nil {
		t.Fatalf("AllTitles: %v", err)
	}
	if len(titles) != 0 {
		t.Fatalf("空 titles = %v", titles)
	}
	// 写入 item_info 后应能取到。
	if err := s.Items.Upsert(ctx, &ItemInfoRow{CookieID: cid, ItemID: "item_o1", ItemTitle: "标题1"}); err != nil {
		t.Fatal(err)
	}
	titles, _ = s.Items.AllTitles(ctx)
	if titles["item_o1"] != "标题1" {
		t.Fatalf("titles = %v", titles)
	}
}

// TestNormalizeOrderStatus 数字码与未知值。
func TestNormalizeOrderStatus(t *testing.T) {
	cases := map[string]string{
		"1":    "processing",
		"4":    "completed",
		"":     "unknown",
		"99":   "99", // 未知数字码原样返回
		"paid": "pending_ship",
	}
	for in, want := range cases {
		if got := NormalizeOrderStatus(in); got != want {
			t.Errorf("NormalizeOrderStatus(%q)=%q want %q", in, got, want)
		}
	}
}

// TestCards_CRUD 卡券 Create/Update/Delete/AllForUser 全链路。
func TestCards_CRUD(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)

	cf := &CardFull{
		Name: "卡密A", Type: "text", TextContent: "AAA", Enabled: true,
		UserID: uid, DelaySeconds: 5, IsMultiSpec: true, SpecName: "颜色", SpecValue: "红",
		APIConfig: `{"k":"v"}`, ImageURL: "http://img", Description: "desc",
	}
	id, err := s.Cards.Create(ctx, cf)
	if err != nil || id == 0 {
		t.Fatalf("Create: id=%d err=%v", id, err)
	}

	// Get 验证全部字段。
	got, err := s.Cards.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "卡密A" || got.Type != "text" || got.TextContent != "AAA" || !got.Enabled ||
		got.DelaySeconds != 5 || !got.IsMultiSpec || got.SpecName != "颜色" || got.SpecValue != "红" ||
		got.APIConfig != `{"k":"v"}` || got.ImageURL != "http://img" || got.Description != "desc" || got.UserID != uid {
		t.Fatalf("card 字段不符: %#v", got)
	}

	// Update 改字段。
	got.Name = "卡密B"
	got.Enabled = false
	got.IsMultiSpec = false
	got.TextContent = "BBB"
	if err := s.Cards.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got2, _ := s.Cards.Get(ctx, id)
	if got2.Name != "卡密B" || got2.Enabled || got2.IsMultiSpec || got2.TextContent != "BBB" {
		t.Fatalf("Update 后字段不符: %#v", got2)
	}

	// AllForUser：先建第二张卡券。
	id2, _ := s.Cards.Create(ctx, &CardFull{Name: "卡密C", Type: "data", DataContent: "x\ny", Enabled: true, UserID: uid})
	all, err := s.Cards.AllForUser(ctx, uid)
	if err != nil {
		t.Fatalf("AllForUser: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("AllForUser len=%d want 2", len(all))
	}
	// ORDER BY id DESC → id2 排第一。
	if all[0].ID != id2 {
		t.Fatalf("AllForUser 顺序: first id=%d want %d", all[0].ID, id2)
	}

	// Delete。
	if err := s.Cards.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Cards.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete 后应 ErrNotFound, got %v", err)
	}
}

// TestCards_GetNotFound 不存在的卡券 ID 返回 ErrNotFound。
func TestCards_GetNotFound(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	if _, err := s.Cards.Get(context.Background(), 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// --- delivery.go ---

// TestItems_Get_IsMultiSpec_Items.Get / IsMultiSpec / MultiQuantityDelivery。
func TestItems_GetAndFlags(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// 不存在 → ErrNotFound。
	if _, err := s.Items.Get(ctx, cid, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// IsMultiSpec 不存在 → false（不报错）。
	if s.Items.IsMultiSpec(ctx, cid, "nope") {
		t.Fatal("不存在商品 IsMultiSpec 应 false")
	}
	if s.Items.MultiQuantityDelivery(ctx, cid, "nope") {
		t.Fatal("不存在商品 MultiQuantityDelivery 应 false")
	}

	if err := s.Items.Upsert(ctx, &ItemInfoRow{
		CookieID: cid, ItemID: "i1", ItemTitle: "T", IsMultiSpec: true, MultiQuantityDelivery: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Items.Get(ctx, cid, "i1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ItemTitle != "T" || !got.IsMultiSpec || !got.MultiQuantityDelivery {
		t.Fatalf("Get 字段: %#v", got)
	}
	if !s.Items.IsMultiSpec(ctx, cid, "i1") {
		t.Fatal("IsMultiSpec 应 true")
	}
	if !s.Items.MultiQuantityDelivery(ctx, cid, "i1") {
		t.Fatal("MultiQuantityDelivery 应 true")
	}
}

// TestCards_ConsumeBatchData data 类型卡券按行消费。
func TestCards_ConsumeBatchData(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)

	// 不存在的卡券 → ErrNotFound。
	if _, err := s.Cards.ConsumeBatchData(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// data_content 为空 → 报错“批量数据为空”。
	cf := &CardFull{Name: "data", Type: "data", UserID: uid, Enabled: true}
	id, _ := s.Cards.Create(ctx, cf)
	if _, err := s.Cards.ConsumeBatchData(ctx, id); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("空批量应报错, got %v", err)
	}

	// 写入多行（含 \r\n 与空行），逐行消费。
	cf.DataContent = "line1\r\nline2\n\nline3\r"
	// 直接 UPDATE 因为 Create 已把 data_content 设空。
	if _, err := s.DB.ExecContext(ctx, `UPDATE cards SET data_content=? WHERE id=?`, "line1\r\nline2\n\nline3\r", id); err != nil {
		t.Fatal(err)
	}

	// 第一次消费 → line1，剩余 line2/line3。
	first, err := s.Cards.ConsumeBatchData(ctx, id)
	if err != nil || first != "line1" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, _ := s.Cards.ConsumeBatchData(ctx, id)
	if second != "line2" {
		t.Fatalf("second=%q want line2", second)
	}
	third, _ := s.Cards.ConsumeBatchData(ctx, id)
	if third != "line3" {
		t.Fatalf("third=%q want line3", third)
	}
	// 全部消费完 → 报错“为空”。
	if _, err := s.Cards.ConsumeBatchData(ctx, id); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("消费完后应报错, got %v", err)
	}
}

func TestCards_RestoreBatchDataReturnsReservedValueToFront(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	id, err := s.Cards.Create(ctx, &CardFull{
		Name: "restore", Type: "data", DataContent: "first\nsecond", Enabled: true, UserID: uid,
	})
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := s.Cards.ConsumeBatchData(ctx, id)
	if err != nil || reserved != "first" {
		t.Fatalf("reserve=%q err=%v", reserved, err)
	}
	if err := s.Cards.RestoreBatchData(ctx, id, reserved); err != nil {
		t.Fatal(err)
	}
	again, err := s.Cards.ConsumeBatchData(ctx, id)
	if err != nil || again != "first" {
		t.Fatalf("restored value must be consumed first: got=%q err=%v", again, err)
	}
}

// TestCards_AppendBatchData 追加卡密行 + 有效行计数。
func TestCards_AppendBatchData(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)

	// 不存在 → ErrNotFound。
	if _, err := s.Cards.AppendBatchData(ctx, 99999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	cf := &CardFull{Name: "data", Type: "data", UserID: uid, Enabled: true, DataContent: ""}
	id, _ := s.Cards.Create(ctx, cf)

	// 全是空行 → 报错“无有效卡密行”。
	if _, err := s.Cards.AppendBatchData(ctx, id, "  \n\n\r\n"); err == nil || !strings.Contains(err.Error(), "无有效") {
		t.Fatalf("空行应报错, got %v", err)
	}

	// 首次追加（空 existing）。
	n, err := s.Cards.AppendBatchData(ctx, id, "a\nb")
	if err != nil || n != 2 {
		t.Fatalf("Append 首次: n=%d err=%v", n, err)
	}
	got, _ := s.Cards.Get(ctx, id)
	if got.DataContent != "a\nb" {
		t.Fatalf("data_content=%q want a\\nb", got.DataContent)
	}
	// 二次追加（existing 非空 → 换行拼接）。
	n2, err := s.Cards.AppendBatchData(ctx, id, "c")
	if err != nil || n2 != 1 {
		t.Fatalf("Append 二次: n=%d err=%v", n2, err)
	}
	got, _ = s.Cards.Get(ctx, id)
	if got.DataContent != "a\nb\nc" {
		t.Fatalf("data_content=%q want a\\nb\\nc", got.DataContent)
	}
}

// TestSplitJoinLines 覆盖 splitLines/joinLines 的 \r\n / 空行 / 末尾换行分支。
func TestSplitJoinLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\r\nb", []string{"a", "b"}}, // \r\n 不产生空行
		{"a\n\nb", []string{"a", "b"}}, // 空行被过滤
		{"a\r", []string{"a"}},         // 末尾 \r
		{"\n\n", nil},                  // 全空行
	}
	for _, c := range cases {
		got := splitLines(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitLines(%q)=%v want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitLines(%q)[%d]=%q want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
	// joinLines 是 splitLines 的逆运算（不含空行）。
	joined := joinLines([]string{"a", "b", "c"})
	if joined != "a\nb\nc" {
		t.Fatalf("joinLines=%q", joined)
	}
	if joinLines(nil) != "" {
		t.Fatal("joinLines(nil) 应为空串")
	}
}

// --- sessions.go ---

// TestSession_GetEmpty 过期/空 sessionID 的处理。
func TestSession_GetEmpty(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.Sessions.Get(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("空 sessionID 应 ErrNotFound, got %v", err)
	}
}

// TestSession_Expired 过期会话被 Get 视为不存在并清理。
func TestSession_Expired(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	user, _ := s.Users.GetByUsername(ctx, "u1")

	// 直接写入一条已过期的会话。
	sid := "expired-session-id"
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (session_id, user_id, username, is_admin, expires_at, created_at) VALUES (?,?,?,?,?,?)`,
		sid, user.ID, "u1", 0, time.Now().Unix()-1, time.Now().Unix())
	if err != nil {
		t.Fatalf("insert expired session: %v", err)
	}
	if _, err := s.Sessions.Get(ctx, sid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期会话应 ErrNotFound, got %v", err)
	}
	// Get 应已删除该过期记录。
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id=?`, sid).Scan(&n)
	if n != 0 {
		t.Fatalf("过期会话应被清理, 仍存在 %d 行", n)
	}
}

// TestSession_DeleteExpired 批量清理过期会话。
func TestSession_DeleteExpired(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	user, _ := s.Users.GetByUsername(ctx, "u1")

	now := time.Now().Unix()
	// 两条过期 + 一条有效。
	for i, sid := range []string{"exp1", "exp2", "valid1"} {
		exp := now - 1
		if i == 2 {
			exp = now + 3600
		}
		_, _ = s.DB.ExecContext(ctx,
			`INSERT INTO sessions (session_id, user_id, username, is_admin, expires_at, created_at) VALUES (?,?,?,?,?,?)`,
			sid, user.ID, "u1", 0, exp, now)
	}

	n, err := s.Sessions.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteExpired 删除=%d want 2", n)
	}
	// 有效会话仍应存在。
	if _, err := s.Sessions.Get(ctx, "valid1"); err != nil {
		t.Fatalf("有效会话被误删: %v", err)
	}
}

func TestSession_GetRejectsInactiveUser(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	user, _ := s.Users.GetByUsername(ctx, "u1")
	sid, err := s.Sessions.Create(ctx, user)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE users SET is_active=0 WHERE id=?`, user.ID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}
	if _, err := s.Sessions.Get(ctx, sid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive user session should be ErrNotFound, got %v", err)
	}
}

// --- users.go ---

// TestUsers_GetAdmin_GetByEmail_GetByID 各 Get 路径。
func TestUsers_Gets(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	s.Users.Create(ctx, "admin", "admin@e.com", "pw")
	s.Users.SetAdmin(ctx, "admin")

	admin, err := s.Users.GetAdmin(ctx)
	if err != nil || admin == nil || admin.Username != "admin" || !admin.IsAdmin {
		t.Fatalf("GetAdmin: %#v err=%v", admin, err)
	}
	byEmail, err := s.Users.GetByEmail(ctx, "admin@e.com")
	if err != nil || byEmail.ID != admin.ID {
		t.Fatalf("GetByEmail: %#v err=%v", byEmail, err)
	}
	byID, err := s.Users.GetByID(ctx, admin.ID)
	if err != nil || byID.Username != "admin" {
		t.Fatalf("GetByID: %#v err=%v", byID, err)
	}
}

// TestUsers_GetMissing 各 Get 不存在返回 ErrNotFound。
func TestUsers_GetMissing(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.Users.GetAdmin(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("空库 GetAdmin 应 ErrNotFound, got %v", err)
	}
	if _, err := s.Users.GetByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByUsername 不存在应 ErrNotFound, got %v", err)
	}
	if _, err := s.Users.GetByEmail(ctx, "nobody@e.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByEmail 不存在应 ErrNotFound, got %v", err)
	}
	if _, err := s.Users.GetByID(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByID 不存在应 ErrNotFound, got %v", err)
	}
}

// TestUsers_CreateDuplicateEmail 重复邮箱应返回 false（不报错）。
func TestUsers_CreateDuplicateEmail(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := s.Users.Create(ctx, "u1", "dup@e.com", "pw"); err != nil || !ok {
		t.Fatalf("首次 Create: ok=%v err=%v", ok, err)
	}
	// 同邮箱不同用户名。
	if ok, err := s.Users.Create(ctx, "u2", "dup@e.com", "pw"); err != nil || ok {
		t.Fatalf("重复邮箱应 ok=false, got ok=%v err=%v", ok, err)
	}
}

func TestUsersCreatePropagatesDatabaseErrors(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	if err := s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Users.Create(context.Background(), "db-error", "db-error@example.com", "pw"); err == nil || ok {
		t.Fatalf("database error was hidden: ok=%v err=%v", ok, err)
	}
}

// TestUsers_UpdatePasswordNotFound 不存在用户返回 false。
func TestUsers_UpdatePasswordNotFound(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ok, err := s.Users.UpdatePassword(context.Background(), "ghost", "pw")
	if err != nil || ok {
		t.Fatalf("不存在用户 UpdatePassword 应 ok=false, got ok=%v err=%v", ok, err)
	}
}

// TestUsers_VerifyAndUpgrade_Inactive 未激活用户验证失败。
func TestUsers_VerifyAndUpgrade_Inactive(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	// 手动置为未激活。
	_, _ = s.DB.ExecContext(ctx, `UPDATE users SET is_active=0 WHERE username=?`, "u1")
	user, ok, err := s.Users.VerifyAndUpgrade(ctx, "u1", "pw")
	if err != nil || ok || user != nil {
		t.Fatalf("未激活用户应 ok=false, got ok=%v user=%v err=%v", ok, user, err)
	}
}

// TestUsers_VerifyAndUpgrade_LegacyUpgrade 命中老 SHA-256 哈希应静默升级到 bcrypt。
func TestUsers_VerifyAndUpgrade_LegacyUpgrade(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	user, _ := s.Users.GetByUsername(ctx, "u1")
	// 篡改密码为老 SHA-256 哈希。
	legacy := legacySHA256("oldpw")
	_, _ = s.DB.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, legacy, user.ID)

	got, ok, err := s.Users.VerifyAndUpgrade(ctx, "u1", "oldpw")
	if err != nil || !ok || got == nil {
		t.Fatalf("老哈希验证应成功: ok=%v err=%v", ok, err)
	}
	// 升级后 password_hash 应变成 bcrypt（$2 开头）。
	after, _ := s.Users.GetByUsername(ctx, "u1")
	if !strings.HasPrefix(after.PasswordHash, "$2") {
		t.Fatalf("未升级到 bcrypt: %q", after.PasswordHash)
	}
}

// TestUsers_UpdateCredentials 改用户名 + 撤销会话 + 用户名冲突。
func TestUsers_UpdateCredentials(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	s.Users.Create(ctx, "u2", "u2@e.com", "pw")
	u1, _ := s.Users.GetByUsername(ctx, "u1")

	// 给 u1 建一个会话。
	sid, _ := s.Sessions.Create(ctx, u1)
	if _, err := s.Sessions.Get(ctx, sid); err != nil {
		t.Fatalf("会话应存在: %v", err)
	}

	// 改用户名（不带密码）→ 应撤销会话。
	if err := s.Users.UpdateCredentials(ctx, u1.ID, "u1renamed", ""); err != nil {
		t.Fatalf("UpdateCredentials: %v", err)
	}
	if _, err := s.Sessions.Get(ctx, sid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("改凭据后会话应被撤销, got %v", err)
	}
	// 用户名已变更。
	if _, err := s.Users.GetByUsername(ctx, "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("旧用户名应不存在: %v", err)
	}
	renamed, _ := s.Users.GetByUsername(ctx, "u1renamed")
	if renamed.ID != u1.ID {
		t.Fatalf("renamed id 不匹配: %d vs %d", renamed.ID, u1.ID)
	}

	// 改成已被占用的用户名 → ErrUsernameTaken。
	if err := s.Users.UpdateCredentials(ctx, u1.ID, "u2", ""); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("占用用户名应 ErrUsernameTaken, got %v", err)
	}

	// 改用户名 + 密码。
	if err := s.Users.UpdateCredentials(ctx, u1.ID, "u1renamed2", "newpw"); err != nil {
		t.Fatalf("UpdateCredentials with pw: %v", err)
	}
	_, ok2, _ := s.Users.VerifyAndUpgrade(ctx, "u1renamed2", "newpw")
	if !ok2 {
		t.Fatal("新密码应可用")
	}

	// 不存在的用户 ID → ErrNotFound。
	if err := s.Users.UpdateCredentials(ctx, 99999, "anyone", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在用户应 ErrNotFound, got %v", err)
	}
}

// --- ai_reply.go ---

// TestAIReply_GetNotFound 不存在返回 ErrNotFound。
func TestAIReply_GetNotFound(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	if _, err := s.AIReply.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// TestAIReply_GetDefaults 缺省值兜底（model_name/base_url 为空时填默认）。
func TestAIReply_GetDefaults(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// 直接插入一条 model_name/base_url 均为 NULL 的记录。
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO ai_reply_settings (cookie_id, ai_enabled, model_name, base_url) VALUES (?, 0, NULL, NULL)`, cid)
	if err != nil {
		t.Fatalf("insert ai_reply: %v", err)
	}
	got, err := s.AIReply.Get(ctx, cid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ModelName != "qwen-plus" {
		t.Fatalf("默认 model_name=%q want qwen-plus", got.ModelName)
	}
	if got.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("默认 base_url=%q", got.BaseURL)
	}
	if got.AIEnabled {
		t.Fatal("ai_enabled 应 false")
	}
}

// --- ws_message.go ---

// TestWSMessages_Add 默认 direction/parse_status 兜底 + 写入成功。
func TestWSMessages_Add(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// 空字段走默认值。
	if err := s.WSMessages.Add(ctx, WSMessage{
		CookieID: cid, RawText: `{"k":1}`, ParsedJSON: `{"k":1}`, MessageKind: "msg",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// 验证默认值写入。
	var dir, status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT direction, parse_status FROM ws_messages WHERE cookie_id=? ORDER BY id DESC LIMIT 1`, cid).Scan(&dir, &status)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if dir != "in" || status != "raw" {
		t.Fatalf("默认值: dir=%q status=%q want in/raw", dir, status)
	}

	// 显式 direction/parse_status。
	if err := s.WSMessages.Add(ctx, WSMessage{
		CookieID: cid, Direction: "out", ParseStatus: "parsed", Error: "ok",
		RawText: "raw", ParsedJSON: "{}", MessageKind: "kind2",
	}); err != nil {
		t.Fatalf("Add explicit: %v", err)
	}
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ws_messages WHERE cookie_id=?`, cid).Scan(&n)
	if n != 2 {
		t.Fatalf("ws_messages 行数=%d want 2", n)
	}
}

func TestWSMessages_AddBatchAndDeleteBefore(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	if err := s.WSMessages.AddBatch(ctx, []WSMessage{
		{CookieID: cid, RawText: "first"},
		{CookieID: cid, Direction: "out", ParseStatus: "parsed", RawText: "second"},
	}); err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE ws_messages SET created_at=? WHERE cookie_id=? AND raw_text=?`,
		time.Now().Add(-8*24*time.Hour), cid, "first"); err != nil {
		t.Fatalf("设置历史时间: %v", err)
	}

	deleted, err := s.WSMessages.DeleteBefore(ctx, cid, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("删除行数=%d want 1", deleted)
	}
	var rawText, direction, parseStatus string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT raw_text, direction, parse_status FROM ws_messages WHERE cookie_id=?`, cid,
	).Scan(&rawText, &direction, &parseStatus); err != nil {
		t.Fatalf("查询保留记录: %v", err)
	}
	if rawText != "second" || direction != "out" || parseStatus != "parsed" {
		t.Fatalf("保留记录=%q/%q/%q", rawText, direction, parseStatus)
	}
}
