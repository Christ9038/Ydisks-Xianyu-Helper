package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// seedSaleCard 创建一个带 3 行库存的 data 卡密组并返回用户、账号和卡密组标识。
func seedSaleCard(t *testing.T, s *Store) (userID int64, cookieID string, cardID int64) {
	t.Helper()
	// uid、cid 是测试账号所属用户和账号标识。
	uid, cid := seedAccount(t, s)
	// id、createErr 保存新建卡密组的标识及错误。
	id, createErr := s.Cards.Create(context.Background(), &CardFull{Name: "组A", Type: "data", UserID: uid, Enabled: true, DataContent: "k1\nk2\nk3"})
	if createErr != nil {
		t.Fatal(createErr)
	}
	return uid, cid, id
}

// stockOf 读取卡密组当前库存正文。
func stockOf(t *testing.T, s *Store, cardID int64) string {
	t.Helper()
	// stock 保存库存正文。
	var stock string
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.DB.QueryRow(`SELECT COALESCE(data_content,'') FROM cards WHERE id=?`, cardID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	return stock
}

// TestCards_ReserveBatchDataWritesReservedRecord 验证扣库存和 reserved 台账同事务写入，并带订单上下文。
func TestCards_ReserveBatchDataWritesReservedRecord(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；s 是本用例使用的测试服务或隔离数据库。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// cardID 是被测卡密组标识；uid 是测试卡密组所有者的用户标识。
	uid, _, cardID := seedSaleCard(t, s)

	// content、recordID、err 是第一次预留结果。
	content, recordID, err := s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{RunID: 7, CookieID: "c1", OrderID: "o1", ItemID: "i1", BuyerID: "b1"})
	if err != nil || content != "k1" || recordID == 0 {
		t.Fatalf("content=%q id=%d err=%v", content, recordID, err)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := stockOf(t, s, cardID); got != "k2\nk3" {
		t.Fatalf("库存应扣掉首行, got %q", got)
	}
	// records 是按订单读取的台账。
	records, err := s.Cards.ListSaleRecordsByOrder(ctx, uid, "c1", "o1")
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%v err=%v", records, err)
	}
	// r 是模拟的 HTTP 请求。
	if r := records[0]; r.Status != SaleStatusReserved || r.Content != "k1" || r.CardName != "组A" || r.CardType != "data" ||
		r.RunID != 7 || r.ItemID != "i1" || r.BuyerID != "b1" || r.SoldAt != 0 || r.CreatedAt == 0 {
		t.Fatalf("台账字段错误: %#v", r)
	}
	// 其他用户读取同一订单必须为空。
	if other, err := s.Cards.ListSaleRecordsByOrder(ctx, uid+99, "c1", "o1"); err != nil || len(other) != 0 {
		t.Fatalf("跨用户不得读到台账: %v err=%v", other, err)
	}
}

// TestCards_ReserveBatchDataErrors 验证库存缺失、空库存时不留下台账。
func TestCards_ReserveBatchDataErrors(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；s 是本用例使用的测试服务或隔离数据库。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// uid 是测试卡密组所有者的用户标识。
	uid, _ := seedAccount(t, s)

	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, _, err := s.Cards.ReserveBatchData(ctx, 99999, CardSaleMeta{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// emptyID 是没有库存的 data 卡密组。
	emptyID, _ := s.Cards.Create(ctx, &CardFull{Name: "空", Type: "data", UserID: uid, Enabled: true})
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, _, err := s.Cards.ReserveBatchData(ctx, emptyID, CardSaleMeta{}); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("空库存应报错, got %v", err)
	}
	// blankID 的库存只有换行，没有有效行。
	blankID, _ := s.Cards.Create(ctx, &CardFull{Name: "空行", Type: "data", UserID: uid, Enabled: true, DataContent: "\n\n"})
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, _, err := s.Cards.ReserveBatchData(ctx, blankID, CardSaleMeta{}); err == nil || !strings.Contains(err.Error(), "无有效行") {
		t.Fatalf("无有效行应报错, got %v", err)
	}
	// total 是台账总数。
	var total int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM card_sale_records`).Scan(&total)
	if total != 0 {
		t.Fatalf("失败预留不得留下台账, got %d", total)
	}
}

// TestCards_ReleaseBatchDataRestoresStockAndDeletesRecord 验证确定未发送时库存回到头部且台账被删除。
func TestCards_ReleaseBatchDataRestoresStockAndDeletesRecord(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；s 是本用例使用的测试服务或隔离数据库。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// cardID 是被测卡密组标识；uid 是测试卡密组所有者的用户标识。
	uid, _, cardID := seedSaleCard(t, s)
	// content 保存扣减出的卡密正文；err 保存被测调用返回的错误，用于断言成功或期望的失败；recordID 是 reserved 台账记录标识。
	content, recordID, err := s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{CookieID: "c1", OrderID: "o1"})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.Cards.ReleaseBatchData(ctx, cardID, recordID, content); err != nil {
		t.Fatal(err)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := stockOf(t, s, cardID); got != "k1\nk2\nk3" {
		t.Fatalf("库存应恢复, got %q", got)
	}
	// records 保存读取到的台账记录，用于断言数量、状态与内容。
	if records, _ := s.Cards.ListSaleRecordsByOrder(ctx, uid, "c1", "o1"); len(records) != 0 {
		t.Fatalf("台账应被删除, got %v", records)
	}
	// 空内容和不存在卡密组的错误分支。
	if err := s.Cards.ReleaseBatchData(ctx, cardID, 0, "  "); err == nil {
		t.Fatal("空内容应报错")
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.Cards.ReleaseBatchData(ctx, 99999, 0, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// TestCards_MarkSaleRecordsSoldAndCounts 验证确认售出、重复确认幂等以及已售计数只含 sold。
func TestCards_MarkSaleRecordsSoldAndCounts(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；s 是本用例使用的测试服务或隔离数据库。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// cardID 是被测卡密组标识；uid 是测试卡密组所有者的用户标识。
	uid, _, cardID := seedSaleCard(t, s)
	// first 保存第一次扣减出的卡密。
	_, first, _ := s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{CookieID: "c1", OrderID: "o1"})
	_, _, _ = s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{CookieID: "c1", OrderID: "o2"})

	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.Cards.MarkSaleRecordsSold(ctx, first, 0, -3); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.Cards.MarkSaleRecordsSold(ctx, first); err != nil {
		t.Fatalf("重复确认应幂等: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.Cards.MarkSaleRecordsSold(ctx); err != nil {
		t.Fatalf("空列表应直接返回: %v", err)
	}
	// counts 保存按卡密组统计的已售数量；err 保存被测调用返回的错误，用于断言成功或期望的失败。
	counts, err := s.Cards.SaleCounts(ctx, uid)
	if err != nil || counts[cardID] != 1 {
		t.Fatalf("已售数只含 sold: %v err=%v", counts, err)
	}
	// records 保存读取到的台账记录，用于断言数量、状态与内容。
	if records, _ := s.Cards.ListSaleRecordsByOrder(ctx, uid, "c1", "o1"); len(records) != 1 || records[0].Status != SaleStatusSold || records[0].SoldAt == 0 {
		t.Fatalf("o1 应为已售: %#v", records)
	}
}

// TestCards_RecordAPISale 验证 API 卡台账的状态校验与卡密组快照。
func TestCards_RecordAPISale(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；s 是本用例使用的测试服务或隔离数据库。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// uid 是测试卡密组所有者的用户标识。
	uid, _ := seedAccount(t, s)
	// apiID 是 API 类型卡密组的标识；err 保存被测调用返回的错误，用于断言成功或期望的失败。
	apiID, err := s.Cards.Create(ctx, &CardFull{Name: "接口卡", Type: "text", UserID: uid, Enabled: true, TextContent: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := s.Cards.RecordAPISale(ctx, apiID, CardSaleMeta{}, "secret", "bogus"); err == nil {
		t.Fatal("非法状态应报错")
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := s.Cards.RecordAPISale(ctx, 99999, CardSaleMeta{}, "secret", SaleStatusSold); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败；soldID 是已确认售出的台账记录标识。
	soldID, err := s.Cards.RecordAPISale(ctx, apiID, CardSaleMeta{CookieID: "c1", OrderID: "o9"}, "secret", SaleStatusSold)
	if err != nil || soldID == 0 {
		t.Fatalf("id=%d err=%v", soldID, err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := s.Cards.RecordAPISale(ctx, apiID, CardSaleMeta{CookieID: "c1", OrderID: "o9"}, "unknown", SaleStatusReserved); err != nil {
		t.Fatal(err)
	}
	// records 保存读取到的台账记录，用于断言数量、状态与内容。
	records, _ := s.Cards.ListSaleRecordsByOrder(ctx, uid, "c1", "o9")
	if len(records) != 2 || records[0].Status != SaleStatusSold || records[0].SoldAt == 0 || records[1].Status != SaleStatusReserved || records[1].SoldAt != 0 {
		t.Fatalf("API 台账状态错误: %#v", records)
	}
}

// TestCards_ListSaleRecordsFilters 验证分页、状态、订单号/内容搜索、通配符转义和用户隔离。
func TestCards_ListSaleRecordsFilters(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；s 是本用例使用的测试服务或隔离数据库。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// cardID 是被测卡密组标识；uid 是测试卡密组所有者的用户标识。
	uid, _, cardID := seedSaleCard(t, s)
	// a 是第一个测试对象。
	_, a, _ := s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{CookieID: "c1", OrderID: "order-A"})
	_, _, _ = s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{CookieID: "c1", OrderID: "order-B"})
	_ = s.Cards.MarkSaleRecordsSold(ctx, a)

	// all 保存卡密组的全部台账记录；err 保存被测调用返回的错误，用于断言成功或期望的失败。
	all, err := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, CardID: cardID})
	if err != nil || all.Total != 2 || len(all.Records) != 2 || all.Records[0].OrderID != "order-B" {
		t.Fatalf("默认按 id 倒序: %#v err=%v", all, err)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, Status: SaleStatusSold}); page.Total != 1 || page.Records[0].OrderID != "order-A" {
		t.Fatalf("状态过滤错误: %#v", page)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, Search: "order-B"}); page.Total != 1 {
		t.Fatalf("订单号搜索错误: %#v", page)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, Search: "k1"}); page.Total != 1 || page.Records[0].Content != "k1" {
		t.Fatalf("内容搜索错误: %#v", page)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, Search: "%"}); page.Total != 0 {
		t.Fatalf("通配符必须按字面量匹配: %#v", page)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, Limit: 1, Offset: 1}); len(page.Records) != 1 || page.Records[0].OrderID != "order-A" || page.Total != 2 {
		t.Fatalf("分页错误: %#v", page)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, Limit: 9999, Offset: -5}); len(page.Records) != 2 {
		t.Fatalf("越界分页应被规范化: %#v", page)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := s.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid + 1}); page.Total != 0 {
		t.Fatalf("跨用户必须隔离: %#v", page)
	}
}

// TestAutomationFinishRunSettlesReservedRecords 验证成功收口确认本次运行遗留的待核对卡密，失败收口保持待核对。
func TestAutomationFinishRunSettlesReservedRecords(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；s 是本用例使用的测试服务或隔离数据库。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// cardID 是被测卡密组标识；cid 是测试账号标识；uid 是测试卡密组所有者的用户标识。
	uid, cid, cardID := seedSaleCard(t, s)
	// ruleID 是启动运行所需的自动化规则。
	ruleID, _ := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100, AutomationActionInput{ActionType: "send_card", Enabled: true}))
	// runID、started、startErr 是已启动运行的标识、是否成功领取及错误；首次领取的租约代次为 1。
	runID, started, startErr := s.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cid, ItemID: "i1", OrderID: "o1", TriggerType: "paid", TriggerKey: "paid:o1"})
	if startErr != nil || !started {
		t.Fatalf("启动运行失败: started=%v err=%v", started, startErr)
	}
	// attempt 是当前重试序号。
	attempt := 1
	_, _, _ = s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{RunID: runID, CookieID: "c1", OrderID: "o1"})
	_, _, _ = s.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{RunID: runID + 100, CookieID: "c1", OrderID: "o2"})

	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.Automation.FinishRun(ctx, runID, attempt, "success", 1, ""); err != nil {
		t.Fatal(err)
	}
	// counts 保存按卡密组统计的已售数量。
	counts, _ := s.Cards.SaleCounts(ctx, uid)
	if counts[cardID] != 1 {
		t.Fatalf("只应确认本次运行的台账, got %v", counts)
	}
	// records 保存读取到的台账记录，用于断言数量、状态与内容。
	if records, _ := s.Cards.ListSaleRecordsByOrder(ctx, uid, "c1", "o2"); records[0].Status != SaleStatusReserved {
		t.Fatalf("其他运行的台账不得被确认: %#v", records)
	}
}

// TestMultiDB_CardSaleLedger 在 SQLite、MySQL、PostgreSQL 上验证售出台账的扣库存记账、搜索转义、确认售出、统计、恢复与运行收口。
func TestMultiDB_CardSaleLedger(t *testing.T) {
	// tg 是当前方言的隔离测试目标。
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 是测试使用的根上下文。
			ctx := context.Background()
			// uid、cid、cardID 是卡密组所有者、账号与卡密组标识。
			uid, cid, cardID := seedSaleCard(t, tg.store)
			// ruleID 是启动运行所需的自动化规则。
			ruleID, _ := tg.store.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100, AutomationActionInput{ActionType: "send_card", Enabled: true}))
			// runID、started、startErr 是已启动运行的标识、是否成功领取及错误。
			runID, started, startErr := tg.store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cid, ItemID: "i1", OrderID: "o1", TriggerType: "paid", TriggerKey: "paid:o1"})
			if startErr != nil || !started {
				t.Fatalf("启动运行失败: started=%v err=%v", started, startErr)
			}
			// content、recordID、reserveErr 是第一次扣减出的卡密、台账标识及错误。
			content, recordID, reserveErr := tg.store.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{RunID: runID, CookieID: cid, OrderID: "o1", ItemID: "i1", BuyerID: "b1"})
			if reserveErr != nil || content != "k1" || recordID == 0 {
				t.Fatalf("扣减失败: content=%q id=%d err=%v", content, recordID, reserveErr)
			}
			// secondContent、secondID 是第二次扣减出的卡密与台账标识，随后被恢复。
			secondContent, secondID, _ := tg.store.Cards.ReserveBatchData(ctx, cardID, CardSaleMeta{CookieID: cid, OrderID: "o2"})
			// err 保存释放卡密的错误，恢复必须成功。
			if err := tg.store.Cards.ReleaseBatchData(ctx, cardID, secondID, secondContent); err != nil {
				t.Fatal(err)
			}
			// stock 是恢复后的库存正文，被恢复的 k2 应回到头部。
			if stock := stockOf(t, tg.store, cardID); stock != "k2\nk3" {
				t.Fatalf("恢复后库存异常: %q", stock)
			}
			// apiID、apiErr 是 API 卡的售出台账标识及错误，内容含 LIKE 通配符。
			apiID, apiErr := tg.store.Cards.RecordAPISale(ctx, cardID, CardSaleMeta{CookieID: cid, OrderID: "o3"}, "100%_x", SaleStatusReserved)
			if apiErr != nil || apiID == 0 {
				t.Fatalf("API 台账失败: %v", apiErr)
			}
			// page 保存按通配符搜索的结果，转义后只应命中字面量内容。
			page, listErr := tg.store.Cards.ListSaleRecords(ctx, CardSaleQuery{UserID: uid, CardID: cardID, Search: "0%_"})
			if listErr != nil || page.Total != 1 || page.Records[0].ID != apiID {
				t.Fatalf("搜索转义错误: %+v err=%v", page, listErr)
			}
			// 运行成功收口后，本运行的 reserved 台账变为 sold，其他运行不受影响。
			if err := tg.store.Automation.FinishRun(ctx, runID, 1, "success", 1, ""); err != nil {
				t.Fatal(err)
			}
			// err 保存运行成功收口的错误。
			if err := tg.store.Cards.MarkSaleRecordsSold(ctx, apiID); err != nil {
				t.Fatal(err)
			}
			// counts 保存按卡密组统计的已售数量。
			counts, countErr := tg.store.Cards.SaleCounts(ctx, uid)
			if countErr != nil || counts[cardID] != 2 {
				t.Fatalf("已售统计错误: %v err=%v", counts, countErr)
			}
			// byOrder 保存按订单读取的台账。
			byOrder, orderErr := tg.store.Cards.ListSaleRecordsByOrder(ctx, uid, cid, "o1")
			if orderErr != nil || len(byOrder) != 1 || byOrder[0].Content != "k1" || byOrder[0].Status != SaleStatusSold || byOrder[0].BuyerID != "b1" {
				t.Fatalf("按订单读取错误: %+v err=%v", byOrder, orderErr)
			}
		})
	}
}
