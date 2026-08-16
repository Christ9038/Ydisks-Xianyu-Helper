package adapter

import (
	"context"
	"errors"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/db"
)

// TestAutomationRepositoryOwnershipMapsNotFound 验证资源缺失只表现为不归属，不泄露数据库模型错误。
func TestAutomationRepositoryOwnershipMapsNotFound(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// owned、ownedErr 保存不存在商品的归属判断结果。
	owned, ownedErr := repository.OwnsItem(ctx, 1, "cid", "missing-item")
	if owned || ownedErr != nil {
		t.Fatalf("缺失商品应返回 false,nil，owned=%v err=%v", owned, ownedErr)
	}
	// cardInfo、cardErr 保存不存在卡密组的最小应用摘要及错误。
	cardInfo, cardErr := repository.GetCard(ctx, 1, 999999)
	if cardInfo != (automationapp.CardInfo{}) || !errors.Is(cardErr, automationapp.ErrRuleNotFound) {
		t.Fatalf("缺失卡密组应映射为应用未找到，info=%+v err=%v", cardInfo, cardErr)
	}
}

// TestAutomationRepositoryOwnershipPropagatesDatabaseErrors 验证数据库不可用时不会伪装成归属失败。
func TestAutomationRepositoryOwnershipPropagatesDatabaseErrors(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// ctx 是数据库关闭后调用适配器的非取消上下文。
	ctx := context.Background()
	// closeErr 表示提前关闭测试数据库时的资源释放错误。
	closeErr := store.DB.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// _, itemErr 保存商品归属查询透传的数据库错误。
	_, itemErr := repository.OwnsItem(ctx, 1, "cid", "item-1")
	if itemErr == nil {
		t.Fatal("数据库关闭后商品归属查询应返回错误")
	}
	// _, cardErr 保存卡密组查询透传的数据库错误。
	_, cardErr := repository.GetCard(ctx, 1, 1)
	if cardErr == nil || errors.Is(cardErr, db.ErrNotFound) || errors.Is(cardErr, automationapp.ErrRuleNotFound) {
		t.Fatalf("数据库关闭后卡密查询应保留底层错误，err=%v", cardErr)
	}
}

// TestAutomationRepositoryEnsurePublishRuleIsIdempotent 验证发布自动化规则只创建一次。
func TestAutomationRepositoryEnsurePublishRuleIsIdempotent(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// input 是代表发布成功后自动发货的规则输入。
	input := automationapp.RuleInput{UserID: 1, CookieID: "cid", ItemID: "item-1", Name: "付款后自动发货 - 商品", TriggerType: automationapp.TriggerOrderPaid, Enabled: true, Priority: 100, ConfigJSON: "{}", Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionConfirmShipment, Enabled: true, SortOrder: 1}}}
	// firstErr 保存第一次幂等准备的错误。
	firstErr := repository.EnsurePublishRule(ctx, input)
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	// secondErr 保存第二次幂等准备的错误。
	secondErr := repository.EnsurePublishRule(ctx, input)
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	// rules、matchErr 保存按发布规则唯一条件查询到的规则。
	rules, matchErr := store.Automation.Match(ctx, input.CookieID, input.ItemID, input.TriggerType)
	if matchErr != nil || len(rules) != 1 {
		t.Fatalf("幂等准备应只保留一条规则，rules=%+v err=%v", rules, matchErr)
	}
}

// TestAutomationRepositoryEnsurePublishRulePropagatesDatabaseError 验证数据库故障不会被发布规则适配器吞掉。
func TestAutomationRepositoryEnsurePublishRulePropagatesDatabaseError(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// closeErr 表示提前关闭测试数据库时的资源释放错误。
	closeErr := store.DB.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// err 保存关闭数据库后执行幂等准备的错误。
	err := repository.EnsurePublishRule(context.Background(), automationapp.RuleInput{UserID: 1, CookieID: "account-1", ItemID: "item-1", Name: "规则", TriggerType: automationapp.TriggerOrderPaid})
	if err == nil {
		t.Fatal("数据库关闭后应返回发布规则持久化错误")
	}
}
