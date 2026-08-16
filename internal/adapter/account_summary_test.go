package adapter

import (
	"context"
	"errors"
	"testing"

	accountapp "xianyu-go/internal/application/account"
)

// TestAccountSummaryRepositoryRejectsMissingStore 验证账号摘要适配器缺少数据库时所有入口均快速失败。
func TestAccountSummaryRepositoryRejectsMissingStore(t *testing.T) {
	// repository 是未装配数据库的账号摘要适配器。
	repository := NewAccountSummaryRepository(nil)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// err 表示账号 ID 列表查询因缺少数据库依赖返回的装配错误。
	if _, err := repository.ListOwnedIDs(ctx, 1); err == nil {
		t.Fatal("缺少数据库时 ListOwnedIDs 不应成功")
	}
	// err 表示账号摘要列表查询因缺少数据库依赖返回的装配错误。
	if _, err := repository.ListSummaries(ctx, 1); err == nil {
		t.Fatal("缺少数据库时 ListSummaries 不应成功")
	}
	// err 表示单个账号摘要查询因缺少数据库依赖返回的装配错误。
	if _, err := repository.GetOwnedSummary(ctx, 1, "cid"); err == nil {
		t.Fatal("缺少数据库时 GetOwnedSummary 不应成功")
	}
	// err 表示账号所有权查询因缺少数据库依赖返回的装配错误。
	if _, err := repository.ExistsOwned(ctx, 1, "cid"); err == nil {
		t.Fatal("缺少数据库时 ExistsOwned 不应成功")
	}
	// err 表示账号所有者查询因缺少数据库依赖返回的装配错误。
	if _, err := repository.GetOwnerID(ctx, "cid"); err == nil {
		t.Fatal("缺少数据库时 GetOwnerID 不应成功")
	}
	// err 表示账号启用状态查询因缺少数据库依赖返回的装配错误。
	if _, err := repository.StatusOwned(ctx, 1, "cid"); err == nil {
		t.Fatal("缺少数据库时 StatusOwned 不应成功")
	}
	// err 表示管理员账号摘要查询因缺少数据库依赖返回的装配错误。
	if _, err := repository.ListAdminSummaries(ctx); err == nil {
		t.Fatal("缺少数据库时 ListAdminSummaries 不应成功")
	}
}

// TestAccountSummaryRepositoryMapsNonSensitiveQueries 验证真实 SQLite 下摘要、所有权、状态和管理员列表的映射。
func TestAccountSummaryRepositoryMapsNonSensitiveQueries(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的账号摘要适配器。
	repository := NewAccountSummaryRepository(store)
	// ctx 是本测试共用的非取消数据库上下文。
	ctx := context.Background()
	// ids、idsErr 保存账号 ID 列表及查询错误。
	ids, idsErr := repository.ListOwnedIDs(ctx, 1)
	if idsErr != nil || len(ids) != 1 || ids[0] != "cid" {
		t.Fatalf("账号 ID 列表异常 ids=%v err=%v", ids, idsErr)
	}
	// summaries、summariesErr 保存账号摘要列表及查询错误。
	summaries, summariesErr := repository.ListSummaries(ctx, 1)
	if summariesErr != nil || len(summaries) != 1 || summaries[0].ID != "cid" {
		t.Fatalf("账号摘要异常 summaries=%+v err=%v", summaries, summariesErr)
	}
	// summary、summaryErr 保存单个账号摘要及查询错误。
	summary, summaryErr := repository.GetOwnedSummary(ctx, 1, "cid")
	if summaryErr != nil || summary.ID != "cid" || summary.UserID != 1 {
		t.Fatalf("单个摘要异常 summary=%+v err=%v", summary, summaryErr)
	}
	// owned、ownedErr 保存本人账号的所有权结论。
	owned, ownedErr := repository.ExistsOwned(ctx, 1, "cid")
	if ownedErr != nil || !owned {
		t.Fatalf("本人所有权异常 owned=%v err=%v", owned, ownedErr)
	}
	// ownerID、ownerErr 保存账号所有者查询结果。
	ownerID, ownerErr := repository.GetOwnerID(ctx, "cid")
	if ownerErr != nil || ownerID != 1 {
		t.Fatalf("账号所有者异常 ownerID=%d err=%v", ownerID, ownerErr)
	}
	// enabled、statusErr 保存账号启用状态及查询错误。
	enabled, statusErr := repository.StatusOwned(ctx, 1, "cid")
	if statusErr != nil || !enabled {
		t.Fatalf("账号状态异常 enabled=%v err=%v", enabled, statusErr)
	}
	// adminSummaries、adminErr 保存管理员账号摘要及查询错误。
	adminSummaries, adminErr := repository.ListAdminSummaries(ctx)
	if adminErr != nil || len(adminSummaries) != 1 || adminSummaries[0].ID != "cid" || !adminSummaries[0].Enabled {
		t.Fatalf("管理员摘要异常 summaries=%+v err=%v", adminSummaries, adminErr)
	}
}

// TestAccountSummaryRepositoryMapsOwnershipAndInfrastructureErrors 验证越权、缺失账号和数据库故障不会返回伪成功。
func TestAccountSummaryRepositoryMapsOwnershipAndInfrastructureErrors(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的账号摘要适配器。
	repository := NewAccountSummaryRepository(store)
	// ctx 是本测试共用的非取消数据库上下文。
	ctx := context.Background()
	// _, otherErr 保存第二个用户创建错误；用户主键随后从摘要查询得到。
	_, otherErr := store.Users.Create(ctx, "other", "other@example.com", "pw")
	if otherErr != nil {
		t.Fatalf("创建第二用户失败: %v", otherErr)
	}
	// other、otherLookupErr 保存第二个用户及读取错误。
	other, otherLookupErr := store.Users.GetByUsername(ctx, "other")
	if otherLookupErr != nil {
		t.Fatalf("读取第二用户失败: %v", otherLookupErr)
	}
	// wrongOwned、wrongOwnedErr 保存跨用户所有权查询结果。
	wrongOwned, wrongOwnedErr := repository.ExistsOwned(ctx, other.ID, "cid")
	if wrongOwnedErr != nil || wrongOwned {
		t.Fatalf("跨用户所有权异常 owned=%v err=%v", wrongOwned, wrongOwnedErr)
	}
	// _, missingErr 验证缺失账号被转换为应用层稳定错误。
	if _, missingErr := repository.GetOwnedSummary(ctx, 1, "missing"); !errors.Is(missingErr, accountapp.ErrNotFound) {
		t.Fatalf("缺失账号错误=%v，期望=%v", missingErr, accountapp.ErrNotFound)
	}
	// closeErr 保存关闭数据库连接的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// _, queryErr 验证数据库关闭后适配器透传基础设施错误。
	if _, queryErr := repository.ListSummaries(ctx, 1); queryErr == nil {
		t.Fatal("数据库关闭后不应伪装摘要读取成功")
	}
}
