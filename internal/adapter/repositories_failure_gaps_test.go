package adapter

import (
	"context"
	"errors"
	"testing"

	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	keywordsapp "xianyu-go/internal/application/keywords"
	notificationsapp "xianyu-go/internal/application/notifications"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// requireAllFail 断言一组调用结果全部为错误，用于验证数据库故障被向上传播。
func requireAllFail(t *testing.T, errs map[string]error) {
	t.Helper()
	// name、err 是调用名称与其返回的错误。
	for name, err := range errs {
		if err == nil {
			t.Errorf("%s 应传播底层故障", name)
		}
	}
}

// TestOrderRepositoryPropagatesDatabaseFailures 验证订单仓储在数据库不可用时的全部入口都返回错误。
func TestOrderRepositoryPropagatesDatabaseFailures(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是随后关闭的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	cleanup()
	// repository 绑定已关闭数据库。
	repository := OrderRepository{store: store}
	// errs 按名称收集调用错误。
	errs := map[string]error{}
	_, errs["GetOwnerID"] = repository.GetOwnerID(ctx, "cid")
	_, errs["ExistsOwned"] = repository.ExistsOwned(ctx, 1, "cid")
	_, errs["ListOwnedIDs"] = repository.ListOwnedIDs(ctx, 1)
	_, _, errs["ListOrdersForUser"] = repository.ListOrdersForUser(ctx, orderapp.ListFilter{UserID: 1})
	_, errs["GetOrder"] = repository.GetOrder(ctx, "o")
	_, _, errs["FindOrder"] = repository.FindOrder(ctx, "o")
	_, errs["FindOrdersByIDs"] = repository.FindOrdersByIDs(ctx, "cid", []string{"o"})
	_, errs["FindChatIDsByBuyerAndItem"] = repository.FindChatIDsByBuyerAndItem(ctx, "cid", "b", "i")
	_, errs["GetItem"] = repository.GetItem(ctx, "cid", "i")
	_, errs["SoftDeleteOrder"] = repository.SoftDeleteOrder(ctx, "o")
	errs["WithTransaction"] = repository.WithTransaction(ctx, func(orderapp.Writer) error { return nil })
	errs["UpsertOrder"] = repository.UpsertOrder(ctx, "o", orderapp.UpsertOptions{CookieID: "cid"})
	errs["BatchUpsertOrders"] = repository.BatchUpsertOrders(ctx, []orderapp.RefreshOrderWrite{{OrderID: "o"}})
	_, errs["LoadCookiePlatformDetail"] = repository.LoadCookiePlatformDetail(ctx, "cid")
	errs["UpdateRenewalCookie"] = repository.UpdateRenewalCookie(ctx, "cid", "v", "{}", 1)
	_, errs["SoftDeleteMissingOrders"] = repository.SoftDeleteMissingOrders(ctx, "cid", map[string]struct{}{"o": {}})
	_, errs["ListOrdersByCookieCursor"] = repository.ListOrdersByCookieCursor(ctx, "cid", 10, "", "")
	errs["Create"] = repository.Create(ctx, &orderapp.RefreshJob{ID: "j", UserID: 1, CookieID: "cid"})
	_, errs["Get"] = repository.Get(ctx, 1, "j")
	_, errs["Claim"] = repository.Claim(ctx, "j", "t", 1)
	_, errs["Cancel"] = repository.Cancel(ctx, 1, "j")
	_, errs["Complete"] = repository.Complete(ctx, "j", "t", "done", "{}", "")
	_, errs["Recoverable"] = repository.Recoverable(ctx, 1, 10)
	_, errs["RequeueExpired"] = repository.RequeueExpired(ctx, "j", 1)
	requireAllFail(t, errs)
}

// TestOrderRepositoryGuardsAndConstructors 验证订单仓储的构造保护、事务保护与空批次快速返回。
func TestOrderRepositoryGuardsAndConstructors(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	if NewOrderRepository(nil) != nil || NewOrderRepository(&db.Store{}) != nil {
		t.Fatal("缺依赖应返回 nil 仓储")
	}
	if NewOrderRefreshJobRepository(nil) != nil || NewOrderRefreshJobRepository(&db.Store{}) != nil {
		t.Fatal("缺依赖应返回 nil 任务仓储")
	}
	// bare 是没有写入 Unit of Work 的仓储。
	bare := OrderRepository{store: &db.Store{}}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := bare.WithTransaction(ctx, func(orderapp.Writer) error { return nil }); err == nil {
		t.Fatal("缺 Unit of Work 应失败")
	}
	// store、cleanup 是完整隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是完整装配的仓储。
	repository := OrderRepository{store: store}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := repository.WithTransaction(ctx, nil); err == nil {
		t.Fatal("空工作函数应失败")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := repository.BatchUpsertOrders(ctx, nil); err != nil {
		t.Fatalf("空批次应直接成功: %v", err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := bare.BatchUpsertOrders(ctx, []orderapp.RefreshOrderWrite{{OrderID: "o"}}); err == nil {
		t.Fatal("缺 Unit of Work 的批量写入应失败")
	}
	if refreshJobFromDB(nil) != nil {
		t.Fatal("空任务应转换为 nil")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := repository.Get(ctx, 1, "missing"); !errors.Is(err, orderapp.ErrRefreshJobNotFound) {
		t.Fatalf("缺失任务: %v", err)
	}
}

// TestAutomationRepositoryPropagatesDatabaseFailures 验证自动化规则仓储在数据库不可用时返回错误。
func TestAutomationRepositoryPropagatesDatabaseFailures(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是随后关闭的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	cleanup()
	// repository 绑定已关闭数据库。
	repository := NewAutomationRepository(store)
	// errs 按名称收集调用错误。
	errs := map[string]error{}
	_, _, errs["ListIssues"] = repository.ListIssues(ctx, 1)
	errs["ResolveRunIssue"] = repository.ResolveRunIssue(ctx, 1, 1, "x")
	errs["ResolveDeferredIssue"] = repository.ResolveDeferredIssue(ctx, 1, 1, true)
	_, errs["ListForUser"] = repository.ListForUser(ctx, 1)
	_, errs["GetForUser"] = repository.GetForUser(ctx, 1, 1)
	_, _, errs["ListPageForUser"] = repository.ListPageForUser(ctx, automationapp.RuleFilter{UserID: 1})
	_, errs["CountByTriggerForUser"] = repository.CountByTriggerForUser(ctx, automationapp.RuleFilter{UserID: 1})
	_, errs["Create"] = repository.Create(ctx, automationapp.RuleInput{CookieID: "cid"})
	errs["EnsurePublishRule"] = repository.EnsurePublishRule(ctx, automationapp.RuleInput{CookieID: "cid"})
	errs["Update"] = repository.Update(ctx, 1, 1, automationapp.RuleInput{CookieID: "cid"})
	errs["Delete"] = repository.Delete(ctx, 1, 1)
	_, errs["OwnsAccount"] = repository.OwnsAccount(ctx, 1, "cid")
	_, errs["OwnsItem"] = repository.OwnsItem(ctx, 1, "cid", "i")
	_, errs["GetItemDeliveryProfile"] = repository.GetItemDeliveryProfile(ctx, 1, "cid", "i")
	_, errs["GetCard"] = repository.GetCard(ctx, 1, 1)
	_, errs["AIReplyEnabled"] = repository.AIReplyEnabled(ctx, "cid")
	_, errs["GetDeliveryTemplate"] = repository.GetDeliveryTemplate(ctx, 1, 1)
	requireAllFail(t, errs)
}

// TestAnalyticsRepositoryPropagatesDatabaseFailures 验证统计仓储在数据库不可用时返回错误，并覆盖时间范围过滤分支。
func TestAnalyticsRepositoryPropagatesDatabaseFailures(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是随后关闭的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 绑定当前数据库。
	repository := NewAnalyticsRepository(store)
	// filter 带起止时间，覆盖时间范围条件拼接。
	filter := analyticsapp.Filter{UserID: 1, StartAt: "2000-01-01 00:00:00", EndBefore: "2100-01-01 00:00:00"}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := repository.QueryDaily(ctx, filter); err != nil {
		t.Fatalf("时间范围查询: %v", err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AnalyticsRepository{}).AvailableCardStock(ctx, 1); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("缺卡券存储: %v", err)
	}
	cleanup()
	// errs 按名称收集调用错误。
	errs := map[string]error{}
	_, errs["DashboardStats"] = repository.DashboardStats(ctx, 1)
	_, errs["AvailableCardStock"] = repository.AvailableCardStock(ctx, 1)
	_, errs["QueryRevenue"] = repository.QueryRevenue(ctx, filter)
	_, errs["QueryDaily"] = repository.QueryDaily(ctx, filter)
	_, errs["QueryStatus"] = repository.QueryStatus(ctx, filter)
	_, errs["QueryCity"] = repository.QueryCity(ctx, filter)
	_, errs["QueryItem"] = repository.QueryItem(ctx, filter)
	_, errs["CountValidOrders"] = repository.CountValidOrders(ctx, filter)
	_, errs["ListValidOrders"] = repository.ListValidOrders(ctx, filter, 10, 0)
	requireAllFail(t, errs)
}

// TestCardsRepositoryRejectsUnconfiguredAndFailsClosed 验证卡券仓储未装配与数据库关闭时的错误传播。
func TestCardsRepositoryRejectsUnconfiguredAndFailsClosed(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// calls 调用卡券仓储全部入口并收集错误。
	calls := func(repository *CardsRepository) map[string]error {
		// errs 按名称收集调用错误。
		errs := map[string]error{}
		_, errs["ListForUser"] = repository.ListForUser(ctx, 1)
		_, errs["Get"] = repository.Get(ctx, 1)
		_, errs["GetFull"] = repository.GetFull(ctx, 1)
		_, errs["Create"] = repository.Create(ctx, cardsapp.Card{})
		errs["Update"] = repository.Update(ctx, cardsapp.Card{ID: 1})
		_, errs["UpdateIfDataUnchanged"] = repository.UpdateIfDataUnchanged(ctx, cardsapp.Card{ID: 1}, "")
		errs["UpdateDataMetadata"] = repository.UpdateDataMetadata(ctx, cardsapp.Card{ID: 1})
		errs["Delete"] = repository.Delete(ctx, 1)
		_, errs["AppendData"] = repository.AppendData(ctx, 1, "x")
		_, errs["SaleCounts"] = repository.SaleCounts(ctx, 1)
		_, errs["ListSaleRecords"] = repository.ListSaleRecords(ctx, 1, cardsapp.SaleListQuery{CardID: 1, Limit: 10})
		_, errs["AllSaleRecords"] = repository.AllSaleRecords(ctx, 1, 1)
		_, errs["SaleRecordsByOrder"] = repository.SaleRecordsByOrder(ctx, 1, "c", "o")
		return errs
	}
	requireAllFail(t, calls(NewCardsRepository(nil)))
	// store、cleanup 是随后关闭的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	cleanup()
	requireAllFail(t, calls(NewCardsRepository(store)))
}

// TestNotificationRepositoriesPropagateDatabaseFailures 验证通知渠道与不确定通知仓储在数据库不可用时返回错误。
func TestNotificationRepositoriesPropagateDatabaseFailures(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是随后关闭的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	cleanup()
	// channels 是渠道仓储。
	channels := NewNotificationChannelRepository(store)
	// uncertain 是不确定通知仓储。
	uncertain := NewNotificationUncertainRepository(store)
	// errs 按名称收集调用错误。
	errs := map[string]error{}
	_, errs["ListChannels"] = channels.ListChannels(ctx, 1)
	_, errs["GetChannelForUpdate"] = channels.GetChannelForUpdate(ctx, 1, 1)
	_, errs["CreateChannel"] = channels.CreateChannel(ctx, 1, notificationsapp.ChannelInput{})
	errs["UpdateChannel"] = channels.UpdateChannel(ctx, 1, notificationsapp.ChannelRecord{})
	errs["DeleteChannel"] = channels.DeleteChannel(ctx, 1, 1)
	_, errs["OwnsChannel"] = channels.OwnsChannel(ctx, 1, 1)
	_, errs["OwnsAccount"] = channels.OwnsAccount(ctx, 1, "cid")
	_, errs["ListBindings"] = channels.ListBindings(ctx, 1)
	_, errs["GetBindingIDs"] = channels.GetBindingIDs(ctx, "cid")
	errs["SetBindings"] = channels.SetBindings(ctx, "cid", []int64{1})
	errs["SetSingleBinding"] = channels.SetSingleBinding(ctx, "cid", 1, true)
	errs["DeleteBinding"] = channels.DeleteBinding(ctx, 1, 1)
	errs["DeleteAccountBindings"] = channels.DeleteAccountBindings(ctx, 1, "cid")
	_, errs["ListUncertainForUser"] = uncertain.ListUncertainForUser(ctx, 1, 10)
	_, errs["CountUncertainForUser"] = uncertain.CountUncertainForUser(ctx, 1)
	_, errs["ListUncertainForAdmin"] = uncertain.ListUncertainForAdmin(ctx, 10)
	_, errs["CountUncertainForAdmin"] = uncertain.CountUncertainForAdmin(ctx)
	requireAllFail(t, errs)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := normalizeNotificationRepositoryError(db.ErrForbidden); !errors.Is(err, notificationsapp.ErrChannelForbidden) {
		t.Fatalf("Forbidden 映射: %v", err)
	}
}

// TestKeywordRepositoryDatabaseFailuresAfterAuthorization 验证账号归属通过后，关键词与商品回复表故障被向上传播。
func TestKeywordRepositoryDatabaseFailuresAfterAuthorization(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin 是账号 cid 的所有者，归属校验会通过。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// repository 是被测适配器。
	repository := NewKeywordRepository(store)
	// table 是需要删除以制造查询故障的业务表。
	for _, table := range []string{"keywords", "item_replay"} {
		// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
		if _, err := store.DB.ExecContext(ctx, "DROP TABLE "+table); err != nil {
			t.Fatal(err)
		}
	}
	// errs 按名称收集调用错误。
	errs := map[string]error{}
	_, errs["List"] = repository.List(ctx, admin.ID, "cid")
	_, errs["Add"] = repository.Add(ctx, admin.ID, "cid", keywordsapp.Draft{Keyword: "k", Reply: "r"})
	errs["DeleteByID"] = repository.DeleteByID(ctx, admin.ID, "cid", 1)
	errs["DeleteByIndex"] = repository.DeleteByIndex(ctx, admin.ID, "cid", 0)
	_, errs["ListItemReplies"] = repository.ListItemReplies(ctx, admin.ID)
	_, errs["GetItemReply"] = repository.GetItemReply(ctx, admin.ID, "cid", "i")
	errs["SetItemReply"] = repository.SetItemReply(ctx, admin.ID, "cid", "i", "c")
	errs["DeleteItemReply"] = repository.DeleteItemReply(ctx, admin.ID, "cid", "i")
	requireAllFail(t, errs)
	// 归属查询自身失败：他人用户访问不存在账号与数据库关闭均返回错误。
	if _, err := repository.List(ctx, admin.ID, "no-such"); !errors.Is(err, keywordsapp.ErrNotFound) {
		t.Fatalf("不存在账号: %v", err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := repository.List(ctx, admin.ID+100, "cid"); !errors.Is(err, keywordsapp.ErrForbidden) {
		t.Fatalf("他人账号: %v", err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := repository.List(ctx, admin.ID, ""); !errors.Is(err, keywordsapp.ErrInvalidInput) {
		t.Fatalf("空账号: %v", err)
	}
	cleanup()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := repository.List(ctx, admin.ID, "cid"); err == nil {
		t.Fatal("数据库关闭后归属查询应失败")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := repository.ListItemReplies(ctx, admin.ID); err == nil {
		t.Fatal("数据库关闭后账号列表应失败")
	}
}

// TestDefaultReplyAndDeliveryTemplateRepositoriesFailClosed 验证默认回复与发货模板仓储在未装配和数据库故障时返回错误。
func TestDefaultReplyAndDeliveryTemplateRepositoriesFailClosed(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// defaultCalls 调用默认回复仓储全部入口。
	defaultCalls := func(repository *DefaultReplyRepository) map[string]error {
		// errs 按名称收集调用错误。
		errs := map[string]error{}
		_, errs["CheckOwnership"] = repository.CheckOwnership(ctx, 1, "cid")
		_, errs["Get"] = repository.Get(ctx, "cid")
		errs["Upsert"] = repository.Upsert(ctx, "cid", defaultreplyapp.Reply{})
		_, errs["ListForUser"] = repository.ListForUser(ctx, 1)
		errs["Delete"] = repository.Delete(ctx, "cid")
		errs["ClearRecords"] = repository.ClearRecords(ctx, "cid")
		return errs
	}
	// templateCalls 调用发货模板仓储全部入口。
	templateCalls := func(repository *DeliveryTemplateRepository) map[string]error {
		// errs 按名称收集调用错误。
		errs := map[string]error{}
		_, errs["ListForUser"] = repository.ListForUser(ctx, 1)
		_, errs["GetForUser"] = repository.GetForUser(ctx, 1, 1)
		_, errs["Create"] = repository.Create(ctx, 1, deliveryapp.Draft{})
		errs["Update"] = repository.Update(ctx, 1, 1, deliveryapp.Draft{})
		errs["Delete"] = repository.Delete(ctx, 1, 1)
		return errs
	}
	requireAllFail(t, defaultCalls(NewDefaultReplyRepository(nil)))
	requireAllFail(t, templateCalls(NewDeliveryTemplateRepository(nil)))
	// store、cleanup 是随后关闭的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	cleanup()
	requireAllFail(t, defaultCalls(NewDefaultReplyRepository(store)))
	requireAllFail(t, templateCalls(NewDeliveryTemplateRepository(store)))
}
