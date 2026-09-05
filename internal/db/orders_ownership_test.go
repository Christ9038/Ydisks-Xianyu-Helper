package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedOwnershipRepair 为 t 的 store 建立同用户买卖账号和软删除错绑单；返回管理用户、旧快照及卖家事实。
func seedOwnershipRepair(t *testing.T, store *Store) (int64, OrderOwnership, OrderUpsertOpts) {
	t.Helper()
	// ctx 用于本地夹具初始化，不执行任何平台请求。
	ctx := context.Background()
	// userID、buyerID 是种子账号的管理用户和虚构平台买家标识。
	userID, buyerID := seedAccount(t, store)
	// err 保存卖家账号创建错误。
	if err := store.Cookies.CreateOwned(ctx, "repair-seller", "fixture", userID); err != nil {
		t.Fatal(err)
	}
	// err 保存旧订单事实及账户上下文夹具写入错误。
	_, err := store.DB.ExecContext(ctx, `INSERT INTO orders(order_id,cookie_id,item_id,buyer_id,order_status,version,chat_id,paid_at,shipped_at,completed_at,buyer_reviewed_at,deleted_at)
		VALUES('repair-order',?,'repair-item',?,'completed',7,'buyer-chat','old-paid','old-shipped','old-completed','old-reviewed',CURRENT_TIMESTAMP)`, buyerID, buyerID)
	if err != nil {
		t.Fatal(err)
	}
	return userID, OrderOwnership{OrderID: "repair-order", CookieID: buyerID, ItemID: "repair-item", BuyerID: buyerID, Version: 7, Deleted: true, Owned: true},
		OrderUpsertOpts{CookieID: "repair-seller", ItemID: "repair-item", BuyerID: buyerID, OrderStatus: "pending_ship", Amount: "12.50"}
}

// TestMultiDB_OrderOwnershipRecovery 验证 t 中可用三方言的隐藏身份、审计、清理、事实合并和延迟买家写入保护。
func TestMultiDB_OrderOwnershipRecovery(t *testing.T) {
	// target 是独立数据库目标，其清理由对应子测试负责。
	for _, target := range allTestTargets(t) {
		// t 接收当前方言的断言，target 只在同步子测试生命周期内使用。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store、ctx 提供当前方言的仓储和操作生命周期。
			store, ctx := target.store, context.Background()
			// userID、expected、options 分别描述管理用户、旧归属证据和卖家事实。
			userID, expected, options := seedOwnershipRepair(t, store)
			// got、err 保存包含软删除状态的最小归属查询结果。
			got, err := store.Orders.FindOwnership(ctx, userID, expected.OrderID)
			if err != nil || got != expected {
				t.Fatalf("归属快照不一致: %v", err)
			}
			got, err = store.Orders.FindOwnership(ctx, userID+1000, expected.OrderID)
			if err != nil || got != (OrderOwnership{}) {
				t.Fatal("越权归属查询泄露旧身份")
			}
			_, err = store.Orders.FindOwnership(ctx, userID, "absent")
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("不存在订单错误=%v", err)
			}
			// err 保存正常迁移的结果，必须成功且提交审计。
			if err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); err != nil {
				t.Fatal(err)
			}
			// order、err 是恢复后的普通可见订单；其历史高级状态不得倒退。
			order, err := store.Orders.Get(ctx, expected.OrderID)
			if err != nil || order.CookieID != options.CookieID || order.Amount != "12.50" || order.OrderStatus != "completed" || order.Version <= expected.Version {
				t.Fatalf("恢复事实不正确: %v", err)
			}
			if order.ChatID != "" || order.PaidAt != "" || order.ShippedAt != "" || order.CompletedAt != "" || order.BuyerReviewedAt != "" {
				t.Fatal("错误买家上下文未清除")
			}
			// audit 仅保存迁移前非秘密上下文，不保存 Cookie 内容或任务原文。
			var audit string
			// err 保存非秘密旧上下文审计查询错误。
			if err := store.DB.QueryRowContext(ctx, `SELECT old_fields_json FROM order_ownership_repairs WHERE order_id=?`, expected.OrderID).Scan(&audit); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(audit, "buyer-chat") || !strings.Contains(audit, "old-reviewed") || strings.Contains(audit, "fixture") {
				t.Fatal("审计缺少旧上下文或包含不应持久化的信息")
			}
			// err 保存旧证据重放结果，必须拒绝重复归属修正。
			if err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); !errors.Is(err, ErrOrderConflict) {
				t.Fatalf("旧快照重复恢复必须冲突: %v", err)
			}
			// err 保存延迟买家普通写入结果，必须维持新的卖家归属。
			if err := store.Orders.Upsert(ctx, expected.OrderID, OrderUpsertOpts{CookieID: expected.CookieID}); !errors.Is(err, ErrForbidden) {
				t.Fatalf("延迟买家写入应被拒绝: %v", err)
			}
		})
	}
}

// TestOrderOwnershipRecoveryUnsafe 验证 t 中每种外部动作痕迹均使整个迁移回滚。
func TestOrderOwnershipRecoveryUnsafe(t *testing.T) {
	// fixtures 将独立副作用名称映射到最小 SQL 夹具，不包含真实账号或秘密。
	fixtures := map[string]string{
		"系统发货":   `UPDATE orders SET system_shipped=1 WHERE order_id='repair-order'`,
		"催评次数":   `UPDATE orders SET review_request_count=1 WHERE order_id='repair-order'`,
		"催评时间":   `UPDATE orders SET last_review_request_at='sent' WHERE order_id='repair-order'`,
		"未完成补偿":  `INSERT INTO order_reconciliations(id,order_id,cookie_id,kind,status) VALUES('r','repair-order','repair-seller','ship','pending')`,
		"待执行":    `INSERT INTO automation_pending_tasks(task_key,cookie_id,trigger_type,task_json) VALUES('task','repair-seller','paid','{"OrderID":"repair-order"}')`,
		"死信兼容字段": `INSERT INTO automation_pending_tasks(task_key,cookie_id,trigger_type,task_json,status) VALUES('task','repair-seller','paid','{"order_id":"repair-order"}','dead_letter')`,
		"坏任务":    `INSERT INTO automation_pending_tasks(task_key,cookie_id,trigger_type,task_json) VALUES('task','repair-seller','paid','broken')`,
		"缺少任务身份": `INSERT INTO automation_pending_tasks(task_key,cookie_id,trigger_type,task_json) VALUES('task','repair-seller','paid','{}')`,
	}
	// name、fixture 分别标识当前应阻止迁移的副作用和创建语句。
	for name, fixture := range fixtures {
		// t 管理每种副作用的隔离夹具和回滚断言。
		t.Run(name, func(t *testing.T) {
			// store、cleanup 提供独立数据库和释放函数。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// userID、expected、options 是满足其他恢复条件的有效证据。
			userID, expected, options := seedOwnershipRepair(t, store)
			// ctx 控制本用例数据库操作生命周期。
			ctx := context.Background()
			// err 保存副作用夹具写入错误，不把夹具故障误认为安全保护通过。
			if _, err := store.DB.ExecContext(ctx, fixture); err != nil {
				t.Fatal(err)
			}
			// err 保存带履约痕迹的迁移结果，必须返回专用阻断错误。
			if err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); !errors.Is(err, ErrOrderRecoveryUnsafe) {
				t.Fatalf("副作用必须阻止迁移: %v", err)
			}
			assertOwnershipRepairRolledBack(t, store, userID, expected)
		})
	}
}

// assertOwnershipRepairRolledBack 为 t 检查 store 中 userID 的 expected 归属及审计均未被失败事务改变。
func assertOwnershipRepairRolledBack(t *testing.T, store *Store, userID int64, expected OrderOwnership) {
	t.Helper()
	// got、err 保存失败事务后的归属快照。
	got, err := store.Orders.FindOwnership(context.Background(), userID, expected.OrderID)
	if err != nil || got != expected {
		t.Fatalf("失败事务改变了旧订单: %v", err)
	}
	// count 统计本订单已经提交的审计条数，失败恢复不得留下审计。
	var count int
	// err 保存失败事务后审计存在性检查结果。
	if err := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM order_ownership_repairs WHERE order_id=?`, expected.OrderID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("失败恢复留下审计: %v", err)
	}
}

// TestOrderFindByIDsForAccount 验证 t 中归属检查之后账号发生变化时，完整查询不会返回其他账号的收货字段。
func TestOrderFindByIDsForAccount(t *testing.T) {
	// store、cleanup 提供独立夹具及连接清理。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// userID、expected、options 描述待迁移订单及原本获准读取它的账号。
	userID, expected, options := seedOwnershipRepair(t, store)
	// ctx 控制恢复和后续列表查询。
	ctx := context.Background()
	// err 保存读取竞争夹具中的归属修正结果。
	if err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); err != nil {
		t.Fatal(err)
	}
	// found、err 代表旧账号在归属变化后发起的完整字段查询。
	found, err := store.Orders.FindByIDsForAccount(ctx, expected.CookieID, []string{expected.OrderID, expected.OrderID, " ", "absent"})
	if err != nil || len(found) != 0 {
		t.Fatalf("旧账号查询泄漏变更后的完整订单: %v", err)
	}
	found, err = store.Orders.FindByIDsForAccount(ctx, options.CookieID, []string{expected.OrderID})
	if err != nil || len(found) != 1 || found[expected.OrderID].CookieID != options.CookieID {
		t.Fatalf("正确账号查询失败: %v", err)
	}
	found, err = store.Orders.FindByIDsForAccount(ctx, "", []string{expected.OrderID})
	if err != nil || len(found) != 0 {
		t.Fatal("空账号不得退化为无账号过滤")
	}
}

// TestOrderOwnershipRecoveryAllRunStates 验证 t 中所有自动化状态均阻止恢复，包括带卡密发货凭据的终态。
func TestOrderOwnershipRecoveryAllRunStates(t *testing.T) {
	// status 遍历持久化运行的常见状态及未知状态，不能只拒绝运行中任务。
	for _, status := range []string{"running", "success", "failed", "canceled", "uncertain", "pending", "unknown"} {
		// t 承载当前状态的隔离断言，status 由同步子测试使用。
		t.Run(status, func(t *testing.T) {
			// store、cleanup 提供独立订单和运行夹具。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// userID、expected、options 是合法恢复证据，只有运行痕迹会阻止它。
			userID, expected, options := seedOwnershipRepair(t, store)
			// ctx 控制本用例的数据库操作。
			ctx := context.Background()
			// err 保存运行所属规则的初始化错误。
			if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rules(user_id,cookie_id,name,trigger_type) VALUES(?,'repair-seller','fixture','paid')`, userID); err != nil {
				t.Fatal(err)
			}
			// err 保存带虚构发货凭据的运行写入错误；从不输出凭据原文。
			if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,delivery_proof)
				SELECT id,'repair-seller','repair-order','paid','fixture',?,'opaque-proof' FROM automation_rules`, status); err != nil {
				t.Fatal(err)
			}
			// err 必须表明已有外部动作痕迹，而不是清空历史再恢复。
			if err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); !errors.Is(err, ErrOrderRecoveryUnsafe) {
				t.Fatalf("运行状态未阻止恢复: %v", err)
			}
			assertOwnershipRepairRolledBack(t, store, userID, expected)
		})
	}
}

// TestOrderOwnershipRecoveryRollback 验证 t 中持久化失败、取消及证据变化均不会留下部分归属或审计。
func TestOrderOwnershipRecoveryRollback(t *testing.T) {
	// scenario 是待注入的失败位置；所有场景共用完整旧状态回滚断言。
	for _, scenario := range []string{"audit", "ownership-write", "merge", "cancel", "version", "buyer", "item", "deleted", "new-owner", "old-owner", "alias", "same-account", "unowned", "incoming-chat", "incoming-shipped"} {
		// t 管理当前失败场景的夹具，避免故障注入污染其他用例。
		t.Run(scenario, func(t *testing.T) {
			// store、cleanup 提供独立 SQLite 数据库和连接清理。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// userID、expected、options 保存有效恢复输入，后续仅改变当前场景的一个条件。
			userID, expected, options := seedOwnershipRepair(t, store)
			// actual 是数据库仍保持的真实旧快照，用于验证事务回滚。
			actual := expected
			// ctx、cancel 由测试拥有，取消场景在调用前终止事务生命周期。
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// want 是可识别的失败种类；数据库故障只要求错误不为空。
			var want error
			switch scenario {
			case "audit":
				// err 保存审计插入故障触发器的创建错误。
				if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_repair BEFORE INSERT ON order_ownership_repairs BEGIN SELECT RAISE(ABORT,'audit rejected'); END`); err != nil {
					t.Fatal(err)
				}
			case "merge":
				options.Amount = "invalid"
			case "ownership-write":
				// err 保存归属 UPDATE 故障触发器的创建错误，用于证明成功写入的审计也会回滚。
				if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_owner BEFORE UPDATE OF cookie_id ON orders BEGIN SELECT RAISE(ABORT,'owner rejected'); END`); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
				want = context.Canceled
			case "version":
				expected.Version--
				want = ErrOrderConflict
			case "buyer":
				options.BuyerID = "different"
				want = ErrOrderConflict
			case "item":
				options.ItemID = "different"
				want = ErrOrderConflict
			case "deleted":
				expected.Deleted = false
				want = ErrOrderConflict
			case "new-owner", "old-owner":
				// id 选择在授权之后变更管理用户的账号，以复现过期用户所有权。
				id := options.CookieID
				if scenario == "old-owner" {
					id = expected.CookieID
				}
				// err 保存虚构其他管理用户的创建错误。
				if _, err := store.DB.ExecContext(ctx, `INSERT INTO users(id,username,email,password_hash) VALUES(9000,'other','other@example.invalid','fixture')`); err != nil {
					t.Fatal(err)
				}
				// err 保存授权后账号归属切换结果。
				if _, err := store.DB.ExecContext(ctx, `UPDATE cookies SET user_id=9000 WHERE id=?`, id); err != nil {
					t.Fatal(err)
				}
				want = ErrForbidden
			case "alias":
				expected.CookieID = "local-alias"
				want = ErrOrderConflict
			case "same-account":
				options.CookieID = expected.CookieID
				want = ErrOrderConflict
			case "unowned":
				expected.Owned = false
				want = ErrForbidden
			case "incoming-chat":
				options.ChatID = "buyer-context"
				want = ErrOrderRecoveryUnsafe
			case "incoming-shipped":
				// shipped 模拟调用方误将外部履约标记混入同步事实。
				shipped := true
				options.SystemShipped = &shipped
				want = ErrOrderRecoveryUnsafe
			}
			// err 保存恢复结果，数据库故障和冲突都必须完整传播。
			err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options)
			if err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("错误未正确传播: %v", err)
			}
			if scenario == "old-owner" {
				// err 将测试主动变更的账号归属还原后再检查订单事务回滚，不改变真实订单行。
				if _, err := store.DB.ExecContext(context.Background(), `UPDATE cookies SET user_id=? WHERE id=?`, userID, actual.CookieID); err != nil {
					t.Fatal(err)
				}
			}
			assertOwnershipRepairRolledBack(t, store, userID, actual)
		})
	}
}

// TestOrderOwnershipRecoveryPreservesNotificationHistory 验证 t 中通知和其他用户坏任务不会被读取、迁移或删除。
func TestOrderOwnershipRecoveryPreservesNotificationHistory(t *testing.T) {
	// store、cleanup 创建隔离数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// userID、expected、options 提供合法恢复证据。
	userID, expected, options := seedOwnershipRepair(t, store)
	// ctx 只用于本地 SQL，不调用通知渠道。
	ctx := context.Background()
	// statement 遍历通知、已完成补偿和无关账号任务夹具；正文只是虚构标识。
	for _, statement := range []string{
		`INSERT INTO notification_channels(name,type,config,user_id) VALUES('fixture','webhook','{}',1)`,
		`INSERT INTO notification_outbox(channel_id,event_type,body,status) SELECT id,'delivery','repair-order','uncertain' FROM notification_channels`,
		`INSERT INTO order_reconciliations(id,order_id,cookie_id,kind,status) VALUES('resolved','repair-order','repair-seller','ship','resolved')`,
		`INSERT INTO users(id,username,email,password_hash) VALUES(9000,'other','other@example.invalid','fixture')`,
		`INSERT INTO cookies(id,value,user_id) VALUES('unrelated','fixture',9000)`,
		`INSERT INTO automation_pending_tasks(task_key,cookie_id,trigger_type,task_json) VALUES('unrelated','unrelated','paid','broken')`,
	} {
		// err 保存当前夹具创建错误，测试不打印载荷。
		if _, err := store.DB.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	// err 验证非履约历史及其他用户坏载荷不阻止已授权恢复。
	if err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); err != nil {
		t.Fatal(err)
	}
	// count 只统计原通知记录，不读取通知正文或渠道配置。
	var count int
	// err 保存原始通知状态检查结果。
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox WHERE status='uncertain'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("通知历史被改变: %v", err)
	}
}

// TestOrdersBatch3000Timing 为 t 实测同事务内 3000 条插入和更新，并检查旧批量砍价、创建时间和状态语义。
func TestOrdersBatch3000Timing(t *testing.T) {
	// store、cleanup 提供临时数据库；初始化时间不计入批量耗时。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// cookieID 是本次性能夹具唯一所属账号。
	_, cookieID := seedAccount(t, store)
	// ctx 控制整批写入生命周期。
	ctx := context.Background()
	// bargain 使插入包含 true，随后 false 候选必须保持历史 true。
	bargain := true
	// rows 包含 3000 个虚构订单，创建时间在更新时省略。
	rows := make([]BatchOrderUpsert, 3000)
	// index 是虚构订单的稳定编号。
	for index := range rows {
		rows[index] = BatchOrderUpsert{OrderID: fmt.Sprintf("timing-%04d", index), Options: OrderUpsertOpts{
			CookieID: cookieID, CreatedAt: "2024-01-01 00:00:00", OrderStatus: "completed", IsBargain: &bargain,
		}}
	}
	// start 标记整批插入实际耗时的起点。
	start := time.Now()
	// err 保存批量插入错误，任何错误都使本次性能证据无效。
	if err := store.Orders.UpsertMany(ctx, rows); err != nil {
		t.Fatal(err)
	}
	t.Logf("3000 条 SQLite 插入: %s", time.Since(start))
	bargain = false
	// index 清除平台时间并提供 unknown，验证旧批量的空值及状态保留语义。
	for index := range rows {
		rows[index].Options.CreatedAt = ""
		rows[index].Options.OrderStatus = "unknown"
	}
	start = time.Now()
	// err 保存批量更新错误，不能用失败重试计作成功性能结果。
	if err := store.Orders.UpsertMany(ctx, rows); err != nil {
		t.Fatal(err)
	}
	t.Logf("3000 条 SQLite 更新: %s", time.Since(start))
	// count 在数据库侧验证全部行保持原有时间、砍价标记及高级状态。
	var count int
	// err 保存完整批量结果检查错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE cookie_id=? AND is_bargain=1 AND order_status='completed' AND created_at='2024-01-01 00:00:00'`, cookieID).Scan(&count); err != nil || count != 3000 {
		t.Fatalf("批量兼容行为发生退化: count=%d err=%v", count, err)
	}
}
