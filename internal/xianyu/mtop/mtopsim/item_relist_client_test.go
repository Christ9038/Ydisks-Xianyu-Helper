package mtopsim_test

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件用真实的 mtop.ClientImpl 对接平台模拟器，验证“售出后重新上架”的协议链路与失败分类。

// TestRelistItemSingleSpecSoldOut 验证单规格售罄商品：补回库存 1、状态回到在售，固定两次请求。
func TestRelistItemSingleSpecSoldOut(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是库存 1 的单规格商品，随后被卖出。
	itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 1)
	gateway.SellOut(itemID)

	// result、err 是重新上架结果。
	result, err := client.RelistItem(context.Background(), cookie, itemID)
	if err != nil || result.AlreadyOnSale || !result.RestoredQuantity {
		t.Fatalf("重新上架失败: %+v err=%v", result, err)
	}
	// item 是平台侧当前商品状态。
	item, _ := gateway.Item(itemID)
	if !item.OnSale || item.Quantity != 1 || item.EditCount != 1 {
		t.Fatalf("平台状态不符: %+v", item)
	}
	if gateway.CallCount(mtopsim.APIItemEditDetail) != 1 || gateway.CallCount(mtopsim.APIItemEdit) != 1 {
		t.Fatalf("应固定读一次写一次: %v", gateway.CallSequence())
	}
}

// TestRelistItemMultiSpecSoldOut 验证多规格全部售罄时每个规格行补 1，并还原 itemProperties。
func TestRelistItemMultiSpecSoldOut(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是两规格商品，随后全部售罄。
	itemID := gateway.AddItemWithSKUs(sellerID, "多规格商品", redBlueSKUs())
	gateway.SellOut(itemID)

	// err 是本次调用返回的错误，用于断言错误映射或失败分类；result 是重新上架调用的返回结果。
	if result, err := client.RelistItem(context.Background(), cookie, itemID); err != nil || !result.RestoredQuantity {
		t.Fatalf("多规格重新上架失败: %+v err=%v", result, err)
	}
	// item 是平台侧当前商品状态。
	item, _ := gateway.Item(itemID)
	if !item.OnSale || item.Quantity != 2 || item.SKUs[0].Quantity != 1 || item.SKUs[1].Quantity != 1 {
		t.Fatalf("多规格平台状态不符: %+v", item)
	}
}

// TestRelistItemKeepsRemainingStock 验证下架但仍有库存的商品只改状态，不改库存。
func TestRelistItemKeepsRemainingStock(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是库存 5 的商品，被下架但库存保留。
	itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)
	gateway.SetOnSale(itemID, false)

	// err 是本次调用返回的错误，用于断言错误映射或失败分类；result 是重新上架调用的返回结果。
	if result, err := client.RelistItem(context.Background(), cookie, itemID); err != nil || result.RestoredQuantity {
		t.Fatalf("不应补库存: %+v err=%v", result, err)
	}
	// item 是平台侧当前商品状态。
	if item, _ := gateway.Item(itemID); !item.OnSale || item.Quantity != 5 {
		t.Fatalf("应保留原库存: %+v", item)
	}
}

// TestRelistItemAlreadyOnSaleSkipsEdit 验证已在售且有库存时只读取、不提交 edit（重复事件幂等）。
func TestRelistItemAlreadyOnSaleSkipsEdit(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是在售商品。
	itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)

	// err 是本次调用返回的错误，用于断言错误映射或失败分类；result 是重新上架调用的返回结果。
	if result, err := client.RelistItem(context.Background(), cookie, itemID); err != nil || !result.AlreadyOnSale {
		t.Fatalf("应识别为已在售: %+v err=%v", result, err)
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 0 {
		t.Fatalf("已在售不应提交 edit: %v", gateway.CallSequence())
	}
}

// TestRelistItemFailures 验证不支持类目、空商品 ID、风控和编辑拒绝的分类，且失败不修改平台数据、不重试。
func TestRelistItemFailures(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := client.RelistItem(ctx, cookie, " "); !errors.Is(err, mtop.ErrItemStockInvalid) {
		t.Fatalf("空商品 ID 应报输入错误: %v", err)
	}
	if gateway.CallCount(mtopsim.APIItemEditDetail) != 0 {
		t.Fatal("空商品 ID 不应发请求")
	}
	// unsupported 是类目不支持网页端编辑的已售商品。
	unsupported := gateway.AddItemWithStock(sellerID, "不支持类目", "9.90", 1)
	gateway.SellOut(unsupported)
	gateway.MarkEditUnsupported(unsupported)
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := client.RelistItem(ctx, cookie, unsupported); !errors.Is(err, mtop.ErrItemEditUnsupported) {
		t.Fatalf("不支持类目应返回哨兵错误: %v", err)
	}
	// supported 是支持编辑但 edit 阶段被平台判定不支持的商品。
	supported := gateway.AddItemWithStock(sellerID, "编辑阶段拒绝", "9.90", 1)
	gateway.SellOut(supported)
	gateway.InjectFault(mtopsim.APIItemEdit, mtopsim.Fault{Ret: []string{mtopsim.RetEditUnsupported}})
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := client.RelistItem(ctx, cookie, supported); !errors.Is(err, mtop.ErrItemEditUnsupported) {
		t.Fatalf("edit 阶段不支持应返回哨兵错误: %v", err)
	}
	// item 是平台侧当前的商品状态副本。
	if item, _ := gateway.Item(supported); item.OnSale || item.EditCount != 0 {
		t.Fatalf("失败不应修改平台数据: %+v", item)
	}
	// missing 是不存在的商品，editDetail 即失败，不应继续提交。
	if _, err := client.RelistItem(ctx, cookie, "999"); err == nil {
		t.Fatal("不存在的商品应失败")
	}
}
