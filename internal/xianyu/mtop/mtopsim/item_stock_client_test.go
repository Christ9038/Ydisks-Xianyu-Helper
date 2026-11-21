package mtopsim_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件用真实的 mtop.ClientImpl 对接平台模拟器的 editDetail / edit 接口，验证库存读取与修改的完整协议链路。

// redBlueSKUs 是“颜色”维度下红、蓝两行规格，库存分别为 3 和 4。
func redBlueSKUs() []mtopsim.ItemSKU {
	return []mtopsim.ItemSKU{
		{ID: "sku-red", Properties: []mtopsim.Spec{{Name: "颜色", Value: "红色"}}, PriceCents: 1000, Quantity: 3},
		{ID: "sku-blue", Properties: []mtopsim.Spec{{Name: "颜色", Value: "蓝色"}}, PriceCents: 1200, Quantity: 4},
	}
}

// TestItemEditDetailRoundTripsSingleAndMultiSpec 验证 editDetail 对单规格与多规格商品返回真实平台形态并被正确解析。
func TestItemEditDetailRoundTripsSingleAndMultiSpec(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// singleID、multiID 是单规格（库存 5）与两规格（库存 3+4）商品。
	singleID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)
	// multiID 是红 3 / 蓝 4 的两规格商品 ID。
	multiID := gateway.AddItemWithSKUs(sellerID, "多规格商品", redBlueSKUs())

	// single、singleErr 是单规格详情。
	single, singleErr := client.FetchItemEditDetail(ctx, cookie, singleID)
	if singleErr != nil || single.MultiSpec || single.Quantity != 5 || len(single.SKUs) != 0 || single.ItemID != singleID {
		t.Fatalf("单规格详情: %+v err=%v", single, singleErr)
	}
	// multi、multiErr 是多规格详情，顶层库存应为各行之和。
	multi, multiErr := client.FetchItemEditDetail(ctx, cookie, multiID)
	if multiErr != nil || !multi.MultiSpec || multi.Quantity != 7 || len(multi.SKUs) != 2 {
		t.Fatalf("多规格详情: %+v err=%v", multi, multiErr)
	}
	if multi.SKUs[0].Key != "sku:sku-red" || multi.SKUs[0].Label != "红色" || multi.SKUs[0].Quantity != 3 || multi.SKUs[1].PriceCents != 1200 {
		t.Fatalf("规格行解析错误: %+v", multi.SKUs)
	}
	if gateway.CallCount(mtopsim.APIItemEditDetail) != 2 || gateway.CallCount(mtopsim.APIItemEdit) != 0 {
		t.Fatalf("只读详情不应触发 edit: %v", gateway.CallSequence())
	}
}

// TestEditItemStockSingleSpec 验证单规格改库存：先读后写固定两次请求，保留标题/类目/图片，只改库存。
func TestEditItemStockSingleSpec(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// itemID 是库存为 5 的单规格商品。
	itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)
	// result、err 是库存修改结果。
	result, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: itemID, Quantity: 8, ExpectedQuantity: 5})
	if err != nil || result.ItemID != itemID {
		t.Fatalf("修改失败: %+v err=%v", result, err)
	}
	// item 是修改后的平台商品。
	item, _ := gateway.Item(itemID)
	if item.Quantity != 8 || item.EditCount != 1 {
		t.Fatalf("平台库存应为 8 且只编辑一次: %+v", item)
	}
	// got 是实际记录到的接口调用序列。
	if got := strings.Join(gateway.CallSequence(), ","); got != mtopsim.APIItemEditDetail+","+mtopsim.APIItemEdit {
		t.Fatalf("接口序列应为读 + 写: %s", got)
	}
	// text 是回填到 edit 载荷的标题节点，必须保持商品原标题。
	text, _ := item.EditData["itemTextDTO"].(map[string]any)
	if text["title"] != "单规格商品" || item.EditData["sourceId"] != itemID || item.EditData["bizcode"] != "pcMainPublish" {
		t.Fatalf("全量覆盖载荷回填异常: %+v", item.EditData)
	}
}

// TestEditItemStockMultiSpecKeepsOtherRows 验证多规格只改指定行：其他行库存不变、顶层库存重算、itemProperties 被还原提交。
func TestEditItemStockMultiSpecKeepsOtherRows(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// itemID 是红 3 / 蓝 4 的两规格商品。
	itemID := gateway.AddItemWithSKUs(sellerID, "多规格商品", redBlueSKUs())
	// result、err 是把红色改为 6、蓝色保持 4 的提交结果。
	result, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: itemID, SKUs: []mtop.ItemStockSKUEdit{
		{Key: "sku:sku-red", Quantity: 6, ExpectedQuantity: 3}, {Key: "sku:sku-blue", Quantity: 4, ExpectedQuantity: 4},
	}})
	if err != nil || result == nil {
		t.Fatalf("修改失败: %+v err=%v", result, err)
	}
	// item 是修改后的平台商品。
	item, _ := gateway.Item(itemID)
	if item.Quantity != 10 || item.SKUs[0].Quantity != 6 || item.SKUs[1].Quantity != 4 {
		t.Fatalf("多规格库存异常: %+v", item)
	}
	// properties 是提交时由 propertyList 还原的 itemProperties，平台要求多规格必须携带。
	properties, _ := item.EditData["itemProperties"].([]any)
	if len(properties) == 0 {
		t.Fatalf("多规格提交必须带 itemProperties: %+v", item.EditData)
	}
	// second 是把某行改为 0（售罄）但另一行仍有库存的提交，应被允许。
	if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: itemID, SKUs: []mtop.ItemStockSKUEdit{
		{Key: "sku:sku-red", Quantity: 0, ExpectedQuantity: 6}, {Key: "sku:sku-blue", Quantity: 4, ExpectedQuantity: 4},
	}}); err != nil {
		t.Fatalf("单行售罄应允许: %v", err)
	}
	if item, _ = gateway.Item(itemID); item.Quantity != 4 {
		t.Fatalf("重算库存应为 4: %+v", item)
	}
}

// TestEditItemStockOnPublishedItems 验证真实发布接口创建的商品（单规格与多规格）可被读取并修改，
// 即发布请求形态与编辑回填形态在模拟器里端到端一致。
func TestEditItemStockOnPublishedItems(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// images 是发布必需的图片。
	images := []mtop.PublishImage{{Filename: "a.png", ContentType: "image/png", Data: []byte("png-a")}}
	// single、singleErr 是单规格发布结果。
	single, singleErr := client.PublishItem(ctx, cookie, mtop.PublishItemRequest{Title: "发布单规格", Description: "d", PriceCents: 990, Quantity: 5, Virtual: true, Images: images})
	if singleErr != nil {
		t.Fatal(singleErr)
	}
	// multi、multiErr 是两规格发布结果，红 3 / 蓝 4。
	multi, multiErr := client.PublishItem(ctx, cookie, mtop.PublishItemRequest{
		Title: "发布多规格", Description: "d", Quantity: 7, Virtual: true, Images: images,
		Specs: []mtop.PublishSpec{{PropertyName: "颜色", Values: []mtop.PublishSpecValue{{Value: "红色", ImageIndex: -1}, {Value: "蓝色", ImageIndex: -1}}}},
		SKUs: []mtop.PublishSKU{
			{PriceCents: 990, Quantity: 3, PropertyList: []mtop.PublishSKUProperty{{PropertyText: "颜色", ValueText: "红色"}}},
			{PriceCents: 990, Quantity: 4, PropertyList: []mtop.PublishSKUProperty{{PropertyText: "颜色", ValueText: "蓝色"}}},
		},
	})
	if multiErr != nil {
		t.Fatal(multiErr)
	}
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: single.ItemID, Quantity: 9, ExpectedQuantity: 5}); err != nil {
		t.Fatalf("已发布单规格商品改库存: %v", err)
	}
	// detail、detailErr 是多规格商品的最新详情，用于取得发布时生成的规格行 Key。
	detail, detailErr := client.FetchItemEditDetail(ctx, cookie, multi.ItemID)
	if detailErr != nil || len(detail.SKUs) != 2 {
		t.Fatalf("已发布多规格详情: %+v err=%v", detail, detailErr)
	}
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: multi.ItemID, SKUs: []mtop.ItemStockSKUEdit{
		{Key: detail.SKUs[0].Key, Quantity: 8, ExpectedQuantity: 3}, {Key: detail.SKUs[1].Key, Quantity: 4, ExpectedQuantity: 4},
	}}); err != nil {
		t.Fatalf("已发布多规格商品改库存: %v", err)
	}
	// singleItem、multiItem 是修改后的平台商品。
	singleItem, _ := gateway.Item(single.ItemID)
	// multiItem 是修改后的多规格平台商品。
	multiItem, _ := gateway.Item(multi.ItemID)
	if singleItem.Quantity != 9 || multiItem.Quantity != 12 || multiItem.SKUs[0].Quantity != 8 {
		t.Fatalf("库存结果: single=%d multi=%d rows=%+v", singleItem.Quantity, multiItem.Quantity, multiItem.SKUs)
	}
}

// TestEditItemStockRejectsStaleExpectedQuantity 验证乐观并发：平台库存已被改变时不提交 edit。
func TestEditItemStockRejectsStaleExpectedQuantity(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// singleID、multiID 是单规格与多规格商品。
	singleID := gateway.AddItemWithStock(sellerID, "单规格", "9.90", 5)
	// multiID 是红 3 / 蓝 4 的两规格商品 ID。
	multiID := gateway.AddItemWithSKUs(sellerID, "多规格", redBlueSKUs())
	// 用户打开弹窗后，平台侧库存被买家拍下或 App 修改而变化。
	gateway.ChangeStock(singleID, 2)
	gateway.ChangeSKUStock(multiID, "sku-red", 1)
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: singleID, Quantity: 8, ExpectedQuantity: 5}); !errors.Is(err, mtop.ErrItemStockChanged) {
		t.Fatalf("单规格应报库存已变化: %v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: multiID, SKUs: []mtop.ItemStockSKUEdit{
		{Key: "sku:sku-red", Quantity: 6, ExpectedQuantity: 3}, {Key: "sku:sku-blue", Quantity: 4, ExpectedQuantity: 4},
	}}); !errors.Is(err, mtop.ErrItemStockChanged) {
		t.Fatalf("多规格应报库存已变化: %v", err)
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 0 {
		t.Fatalf("库存冲突时不得提交 edit: %v", gateway.CallSequence())
	}
	// item 是单规格商品，应保持平台侧新库存 2。
	if item, _ := gateway.Item(singleID); item.Quantity != 2 || item.EditCount != 0 {
		t.Fatalf("冲突时不得覆盖: %+v", item)
	}
}

// TestEditItemStockRejectsInvalidShapesWithoutPlatformRequest 验证本地即可判定的非法请求不会触达平台，
// 以及与平台规格行不一致的请求在 edit 之前被拒绝。
func TestEditItemStockRejectsInvalidShapesWithoutPlatformRequest(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// itemID 是两规格商品。
	itemID := gateway.AddItemWithSKUs(sellerID, "多规格", redBlueSKUs())
	// local 是不需要平台即可判定非法的请求。
	for name, request := range map[string]mtop.ItemStockEditRequest{
		"空商品ID":  {Quantity: 1},
		"库存为0":   {ItemID: itemID, Quantity: 0},
		"库存超上限":  {ItemID: itemID, Quantity: 1000000},
		"规格全为0":  {ItemID: itemID, SKUs: []mtop.ItemStockSKUEdit{{Key: "a", Quantity: 0}, {Key: "b", Quantity: 0}}},
		"规格行重复":  {ItemID: itemID, SKUs: []mtop.ItemStockSKUEdit{{Key: "a", Quantity: 1}, {Key: "a", Quantity: 1}}},
		"规格库存为负": {ItemID: itemID, SKUs: []mtop.ItemStockSKUEdit{{Key: "a", Quantity: -1}}},
	} {
		// err 保存本次调用返回的失败原因，供后续断言使用。
		if _, err := client.EditItemStock(ctx, cookie, request); !errors.Is(err, mtop.ErrItemStockInvalid) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	if gateway.CallCount() != 0 {
		t.Fatalf("非法请求不应访问平台: %v", gateway.CallSequence())
	}
	// 与平台规格行不一致：未知 Key、缺行、对多规格商品提交单规格。
	for name, request := range map[string]mtop.ItemStockEditRequest{
		"未知规格":        {ItemID: itemID, SKUs: []mtop.ItemStockSKUEdit{{Key: "sku:none", Quantity: 1, ExpectedQuantity: 3}, {Key: "sku:sku-blue", Quantity: 4, ExpectedQuantity: 4}}},
		"缺少规格":        {ItemID: itemID, SKUs: []mtop.ItemStockSKUEdit{{Key: "sku:sku-red", Quantity: 5, ExpectedQuantity: 3}}},
		"单规格提交到多规格商品": {ItemID: itemID, Quantity: 3, ExpectedQuantity: 7},
	} {
		// err 保存本次调用返回的失败原因，供后续断言使用。
		if _, err := client.EditItemStock(ctx, cookie, request); err == nil {
			t.Fatalf("%s: 应被拒绝", name)
		}
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 0 {
		t.Fatalf("规格不一致不得提交 edit: %v", gateway.CallSequence())
	}
}

// TestEditItemStockUnsupportedCategory 验证类目不支持网页端编辑：读取与修改都返回专用哨兵错误，且不改任何数据。
func TestEditItemStockUnsupportedCategory(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// itemID 是被标记为类目不支持的商品。
	itemID := gateway.AddItemWithStock(sellerID, "不支持类目", "9.90", 5)
	gateway.MarkEditUnsupported(itemID)
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.FetchItemEditDetail(ctx, cookie, itemID); !errors.Is(err, mtop.ErrItemEditUnsupported) {
		t.Fatalf("读取应报不支持: %v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: itemID, Quantity: 8, ExpectedQuantity: 5}); !errors.Is(err, mtop.ErrItemEditUnsupported) {
		t.Fatalf("修改应报不支持: %v", err)
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 0 {
		t.Fatalf("读取阶段已不支持时不得提交 edit: %v", gateway.CallSequence())
	}
	// item 是故障注入期间被检查的单规格平台商品，用于确认数据未被修改。
	if item, _ := gateway.Item(itemID); item.Quantity != 5 {
		t.Fatalf("不得修改数据: %+v", item)
	}
	// 模拟读取通过但提交阶段平台返回不支持：直接注入 edit 故障。
	supported := gateway.AddItemWithStock(sellerID, "提交阶段不支持", "9.90", 5)
	gateway.InjectFault(mtopsim.APIItemEdit, mtopsim.Fault{Ret: []string{mtopsim.RetEditUnsupported}})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: supported, Quantity: 8, ExpectedQuantity: 5}); !errors.Is(err, mtop.ErrItemEditUnsupported) {
		t.Fatalf("提交阶段不支持应映射哨兵: %v", err)
	}
}

// TestItemStockRejectsForeignAndUnknownItems 验证不属于该卖家或不存在的商品无法读取或修改。
func TestItemStockRejectsForeignAndUnknownItems(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// foreignID 是其他卖家的商品。
	foreignID := gateway.AddItemWithStock("2200000000999", "别人的商品", "9.90", 5)
	// id 是当前检查的商品 ID（他人商品或不存在的商品）。
	for _, id := range []string{foreignID, "9999999999999"} {
		// err 保存本次调用返回的失败原因，供后续断言使用。
		if _, err := client.FetchItemEditDetail(ctx, cookie, id); err == nil {
			t.Fatalf("%s 读取应失败", id)
		}
		// err 保存本次调用返回的失败原因，供后续断言使用。
		if _, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: id, Quantity: 8, ExpectedQuantity: 5}); err == nil {
			t.Fatalf("%s 修改应失败", id)
		}
	}
	// item 是他人商品在平台中的当前状态，用于确认未被修改。
	if item, _ := gateway.Item(foreignID); item.Quantity != 5 || item.EditCount != 0 {
		t.Fatalf("不得修改他人商品: %+v", item)
	}
}

// TestItemStockFaultsStopImmediately 验证风控、会话失效、网络与截断响应都立即终止：读阶段失败不提交 edit，
// 写阶段失败不重试，且错误分类正确。
func TestItemStockFaultsStopImmediately(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// itemID 是库存为 5 的单规格商品。
	itemID := gateway.AddItemWithStock(sellerID, "单规格", "9.90", 5)
	// request 是每个用例使用的合法修改请求。
	request := mtop.ItemStockEditRequest{ItemID: itemID, Quantity: 8, ExpectedQuantity: 5}

	gateway.InjectFault(mtopsim.APIItemEditDetail, mtopsim.Fault{Ret: []string{mtopsim.RetRiskControl}})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, request); !mtop.IsRiskVerificationErr(err) {
		t.Fatalf("读阶段风控应被识别: %v", err)
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 0 || gateway.CallCount(mtopsim.APIItemEditDetail) != 1 {
		t.Fatalf("读阶段风控后不得继续请求: %v", gateway.CallSequence())
	}

	gateway.InjectFault(mtopsim.APIItemEdit, mtopsim.Fault{Ret: []string{mtopsim.RetRiskControl}})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(ctx, cookie, request); !mtop.IsRiskVerificationErr(err) {
		t.Fatalf("写阶段风控应被识别: %v", err)
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 1 {
		t.Fatalf("写阶段风控后不得重试 edit: %v", gateway.CallSequence())
	}

	gateway.InjectFault(mtopsim.APIItemEditDetail, mtopsim.Fault{Ret: []string{mtopsim.RetSessionExpired}})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.FetchItemEditDetail(ctx, cookie, itemID); !mtop.IsSessionExpiredErr(err) {
		t.Fatalf("会话失效应被识别: %v", err)
	}

	gateway.InjectFault(mtopsim.APIItemEditDetail, mtopsim.Fault{Err: errors.New("连接被重置")})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.FetchItemEditDetail(ctx, cookie, itemID); err == nil {
		t.Fatal("网络错误应返回错误")
	}
	gateway.InjectFault(mtopsim.APIItemEditDetail, mtopsim.Fault{Body: `{"ret":["SUCCESS::调用成功"],"data":`})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.FetchItemEditDetail(ctx, cookie, itemID); err == nil {
		t.Fatal("截断响应应返回错误")
	}
	gateway.InjectFault(mtopsim.APIItemEditDetail, mtopsim.Fault{Ret: []string{mtopsim.RetSuccess}, Data: map[string]any{}})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.FetchItemEditDetail(ctx, cookie, itemID); !errors.Is(err, mtop.ErrItemEditDetailUnusable) {
		t.Fatalf("空详情应判定为不可用: %v", err)
	}
	// item 是故障注入期间被检查的单规格平台商品，用于确认数据未被修改。
	if item, _ := gateway.Item(itemID); item.Quantity != 5 || item.EditCount != 0 {
		t.Fatalf("故障期间不得修改数据: %+v", item)
	}
}

// TestEditItemStockSurvivesOneTokenRefresh 验证签名 Token 过期时沿用 callMTop 既有的单次续期重试，之后正常完成，
// 且不会因此重复提交 edit 之外的修改。
func TestEditItemStockSurvivesOneTokenRefresh(t *testing.T) {
	// client 是对接模拟器的真实 MTOP 客户端；cookie 是卖家登录后的初始 Cookie；gateway 是本用例独立的平台模拟器状态。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是库存为 5 的单规格商品，用于验证 Token 过期后的单次续期。
	itemID := gateway.AddItemWithStock(sellerID, "单规格", "9.90", 5)
	gateway.InjectFault(mtopsim.APIItemEdit, mtopsim.Fault{Ret: []string{mtopsim.RetTokenExpired}})
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if _, err := client.EditItemStock(context.Background(), cookie, mtop.ItemStockEditRequest{ItemID: itemID, Quantity: 8, ExpectedQuantity: 5}); err != nil {
		t.Fatalf("一次 Token 过期后应自动续期并成功: %v", err)
	}
	// item 是续期重试后的平台商品，应只被成功编辑一次。
	if item, _ := gateway.Item(itemID); item.Quantity != 8 || item.EditCount != 1 {
		t.Fatalf("应只成功编辑一次: %+v", item)
	}
}
