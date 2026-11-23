package mtopsim_test

import (
	"context"
	"testing"

	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件用真实 mtop.ClientImpl 对接模拟器的 downshelf 接口，验证商品下架的协议链路与失败分类。

// TestDownShelfItemRemovesOnlyOwnedOnSaleItems 验证下架只改变在售状态：成功后商品离开在售列表但仍存在，
// 重复下架、他人商品和不存在的商品都不得被判定为成功。
func TestDownShelfItemRemovesOnlyOwnedOnSaleItems(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// itemID 是卖家的在售商品；otherID 是另一位卖家的在售商品。
	itemID := gateway.AddItemWithStock(sellerID, "待下架商品", "9.90", 3)
	// otherSeller 是另一位卖家账号，用来验证归属校验。
	otherSeller := "2200000000009"
	gateway.AddAccount(otherSeller, "其他卖家")
	// otherID 是其他卖家的在售商品。
	otherID := gateway.AddItem(otherSeller, "他人商品", "1.00", false)

	// result、err 是下架结果。
	result, err := client.DownShelfItem(ctx, cookie, itemID)
	if err != nil || result == nil || !result.Success {
		t.Fatalf("下架应成功: %+v err=%v", result, err)
	}
	// item、exists 是下架后的平台商品：仍存在，库存不变，只是不在售。
	item, exists := gateway.Item(itemID)
	if !exists || item.OnSale || item.Quantity != 3 {
		t.Fatalf("下架只应改变在售状态: %+v exists=%v", item, exists)
	}
	// listed、listErr 是下架后的在售商品列表，不得再包含该商品。
	listed, listErr := client.FetchAllItems(ctx, cookie, 20, 5)
	if listErr != nil || len(listed.Items) != 0 {
		t.Fatalf("下架后不应出现在在售列表: %+v err=%v", listed, listErr)
	}

	// 失败场景：每个都必须返回错误且不改变他人商品。
	for name, target := range map[string]string{"重复下架": itemID, "他人商品": otherID, "不存在的商品": "9999999999"} { // name、target 是场景名与商品 ID。
		if result, err = client.DownShelfItem(ctx, cookie, target); err == nil || (result != nil && result.Success) {
			t.Fatalf("%s 不应被判定为成功: %+v err=%v", name, result, err)
		}
	}
	// other 是被越权下架请求命中的他人商品，用来确认其状态未变。
	if other, _ := gateway.Item(otherID); !other.OnSale {
		t.Fatalf("越权下架不得改变他人商品: %+v", other)
	}
}

// TestDownShelfItemSendsNoRequestForEmptyID 验证空商品 ID 在本地拒绝，不向平台发请求。
func TestDownShelfItemSendsNoRequestForEmptyID(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	if _, err := client.DownShelfItem(context.Background(), cookie, "  "); err == nil || gateway.CallCount() != 0 { // err 是空 ID 的本地校验错误。
		t.Fatalf("空商品 ID 应本地拒绝且零请求: err=%v calls=%v", err, gateway.CallSequence())
	}
}

// TestDownShelfItemRefusesUnconfirmedSuccess 验证平台 ret 成功但 data.success 不是 true 时不得判定下架成功。
func TestDownShelfItemRefusesUnconfirmedSuccess(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是卖家的在售商品。
	itemID := gateway.AddItem(sellerID, "待下架商品", "9.90", false)
	gateway.InjectFault(mtopsim.APIDownShelf, mtopsim.Fault{Ret: []string{mtopsim.RetSuccess}, Data: map[string]any{"success": false}})
	if result, err := client.DownShelfItem(context.Background(), cookie, itemID); err == nil || (result != nil && result.Success) { // result、err 是未确认成功时的结果。
		t.Fatalf("data.success=false 不得判定成功: %+v err=%v", result, err)
	}
	// 响应缺少 data.success 时同样不得判定成功。
	gateway.InjectFault(mtopsim.APIDownShelf, mtopsim.Fault{Ret: []string{mtopsim.RetSuccess}, Data: map[string]any{}})
	if result, err := client.DownShelfItem(context.Background(), cookie, itemID); err == nil || (result != nil && result.Success) { // result、err 是缺少确认字段时的结果。
		t.Fatalf("缺少 data.success 不得判定成功: %+v err=%v", result, err)
	}
}
