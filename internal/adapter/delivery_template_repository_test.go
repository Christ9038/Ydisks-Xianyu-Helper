package adapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	"xianyu-go/internal/db"
)

// newDeliveryTemplateAdapterStore 创建适配器测试使用的独立 SQLite 仓储。
func newDeliveryTemplateAdapterStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// dbPath 保存本次测试数据库文件位置。
	dbPath := filepath.Join(t.TempDir(), "adapter.db")
	// rawDB、dialect、openErr 保存数据库连接、方言和打开错误。
	rawDB, dialect, openErr := db.Open(context.Background(), dbPath)
	if openErr != nil {
		t.Fatalf("open test db: %v", openErr)
	}
	// store 聚合适配器所需的数据库仓储。
	store := db.NewStore(rawDB, dialect)
	// cleanup 关闭本测试创建的数据库连接。
	cleanup := func() { _ = rawDB.Close() }
	return store, cleanup
}

// TestDeliveryTemplateRepositoryValidation 验证适配器在依赖缺失时返回初始化错误。
func TestDeliveryTemplateRepositoryValidation(t *testing.T) {
	// ctx 是本测试所有调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是故意未初始化的适配器。
	var repository *DeliveryTemplateRepository
	// list, listErr 保存列表初始化错误。
	list, listErr := repository.ListForUser(ctx, 1)
	// item, getErr 保存单模板初始化错误。
	item, getErr := repository.GetForUser(ctx, 1, 1)
	// createID, createErr 保存创建初始化错误。
	createID, createErr := repository.Create(ctx, 1, deliveryapp.Draft{Name: "模板", Messages: []string{"正文"}})
	// updateErr 保存更新初始化错误。
	updateErr := repository.Update(ctx, 1, 1, deliveryapp.Draft{Name: "模板", Messages: []string{"正文"}})
	// deleteErr 保存删除初始化错误。
	deleteErr := repository.Delete(ctx, 1, 1)
	if list != nil || item.ID != 0 || createID != 0 || listErr == nil || getErr == nil || createErr == nil || updateErr == nil || deleteErr == nil {
		t.Fatalf("invalid repository results: list=%v item=%+v id=%d errors=%v/%v/%v/%v/%v", list, item, createID, listErr, getErr, createErr, updateErr, deleteErr)
	}
}

// TestDeliveryTemplateRepositoryMapsModelsAndErrors 验证数据库模型转换、创建更新及稳定错误映射。
func TestDeliveryTemplateRepositoryMapsModelsAndErrors(t *testing.T) {
	// store, cleanup 保存适配器测试使用的数据库及清理函数。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	defer cleanup()
	// ctx 是本测试所有调用共用的非取消上下文。
	ctx := context.Background()
	// createOK、createErr 保存测试用户创建结果。
	createOK, createErr := store.Users.Create(ctx, "admin", "admin@example.com", "pw")
	if createErr != nil || !createOK {
		t.Fatalf("create user: ok=%v err=%v", createOK, createErr)
	}
	// user, userErr 保存测试用户记录。
	user, userErr := store.Users.GetByUsername(ctx, "admin")
	if userErr != nil {
		t.Fatal(userErr)
	}
	// cookieErr 保存自动化规则引用所需的账号凭证外键初始化结果。
	if cookieErr := store.Cookies.Save(ctx, "adapter-cookie", "cv=admin", user.ID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	// repository 是绑定数据库模板仓储的应用适配器。
	repository := NewDeliveryTemplateRepository(store)
	// templateID、templateErr 保存应用层创建结果。
	templateID, templateErr := repository.Create(ctx, user.ID, deliveryapp.Draft{Name: "  模板  ", Enabled: true, Messages: []string{"订单 {{order_id}}", "卡密 {{cards.main}}"}})
	if templateErr != nil || templateID <= 0 {
		t.Fatalf("Create id=%d err=%v", templateID, templateErr)
	}
	// templates、listErr 保存应用层列表结果。
	templates, listErr := repository.ListForUser(ctx, user.ID)
	if listErr != nil || len(templates) != 1 || templates[0].ID != templateID || templates[0].Name != "模板" || len(templates[0].Messages) != 2 || len(templates[0].Keys) != 1 || templates[0].Keys[0] != "main" {
		t.Fatalf("List templates=%+v err=%v", templates, listErr)
	}
	// item、getErr 保存应用层单模板结果。
	item, getErr := repository.GetForUser(ctx, user.ID, templateID)
	if getErr != nil || item.ID != templateID || len(item.Messages) != 2 {
		t.Fatalf("Get item=%+v err=%v", item, getErr)
	}
	// updateErr 保存应用层更新结果。
	updateErr := repository.Update(ctx, user.ID, templateID, deliveryapp.Draft{Name: "更新模板", Enabled: false, Messages: []string{"{{cards.main}}"}})
	if updateErr != nil {
		t.Fatalf("Update: %v", updateErr)
	}
	// missingGetErr 保存跨用户单模板读取的稳定错误。
	_, missingGetErr := repository.GetForUser(ctx, user.ID+1, templateID)
	if !errors.Is(missingGetErr, deliveryapp.ErrNotFound) {
		t.Fatalf("cross-user Get error=%v", missingGetErr)
	}
	// missingUpdateErr 保存跨用户更新的稳定错误。
	missingUpdateErr := repository.Update(ctx, user.ID+1, templateID, deliveryapp.Draft{Name: "越权", Messages: []string{"正文"}})
	if !errors.Is(missingUpdateErr, deliveryapp.ErrNotFound) {
		t.Fatalf("cross-user Update error=%v", missingUpdateErr)
	}

	// referencedTemplateID、referencedTemplateErr 保存用于验证删除保护的模板。
	referencedTemplateID, referencedTemplateErr := repository.Create(ctx, user.ID, deliveryapp.Draft{Name: "引用模板", Enabled: true, Messages: []string{"正文"}})
	if referencedTemplateErr != nil {
		t.Fatal(referencedTemplateErr)
	}
	// ruleID、ruleErr 保存引用模板的自动化规则。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: user.ID, CookieID: "adapter-cookie", ItemID: "item", Name: "引用", TriggerType: "paid", Enabled: true, Actions: []db.AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: referencedTemplateID, Enabled: true, SortOrder: 1}}})
	if ruleErr != nil || ruleID <= 0 {
		t.Fatalf("create reference rule id=%d err=%v", ruleID, ruleErr)
	}
	// referencedErr 保存删除被引用模板的稳定错误。
	referencedErr := repository.Delete(ctx, user.ID, referencedTemplateID)
	if !errors.Is(referencedErr, deliveryapp.ErrReferenced) {
		t.Fatalf("referenced Delete error=%v", referencedErr)
	}
	// deleteErr 保存删除未被引用模板的结果。
	deleteErr := repository.Delete(ctx, user.ID, templateID)
	if deleteErr != nil {
		t.Fatalf("Delete: %v", deleteErr)
	}
}

// TestDeliveryTemplateRepositoryPropagatesClosedDatabaseErrors 验证模板适配器不会吞掉底层数据库故障。
func TestDeliveryTemplateRepositoryPropagatesClosedDatabaseErrors(t *testing.T) {
	// store、cleanup 保存即将关闭的模板测试数据库。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	defer cleanup()
	// repository 保存绑定数据库的模板适配器。
	repository := NewDeliveryTemplateRepository(store)
	// ctx 保存数据库故障测试上下文。
	ctx := context.Background()
	// closeErr 保存关闭测试数据库连接的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// listErr 保存关闭数据库后的模板列表错误。
	if _, listErr := repository.ListForUser(ctx, 1); listErr == nil {
		t.Fatal("数据库关闭后模板列表不应成功")
	}
	// getErr 保存关闭数据库后的模板读取错误。
	if _, getErr := repository.GetForUser(ctx, 1, 1); getErr == nil {
		t.Fatal("数据库关闭后模板读取不应成功")
	}
	// createErr 保存关闭数据库后的模板创建错误。
	if _, createErr := repository.Create(ctx, 1, deliveryapp.Draft{Name: "模板", Messages: []string{"正文"}}); createErr == nil {
		t.Fatal("数据库关闭后模板创建不应成功")
	}
	// updateErr 保存关闭数据库后的模板更新错误。
	if updateErr := repository.Update(ctx, 1, 1, deliveryapp.Draft{Name: "模板", Messages: []string{"正文"}}); updateErr == nil {
		t.Fatal("数据库关闭后模板更新不应成功")
	}
	// deleteErr 保存关闭数据库后的模板删除错误。
	if deleteErr := repository.Delete(ctx, 1, 1); deleteErr == nil {
		t.Fatal("数据库关闭后模板删除不应成功")
	}
}
