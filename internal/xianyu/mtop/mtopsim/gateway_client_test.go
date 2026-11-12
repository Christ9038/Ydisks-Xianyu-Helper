package mtopsim_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
	"xianyu-go/internal/xianyu/protocol"
)

// 本文件用真实的 mtop.ClientImpl 对接平台模拟器，逐接口验证“签名 → 网关校验 → 业务状态机 → 响应解析”整条协议链路。

// sellerID、buyerID 是测试使用的卖家与买家账号标识，均为虚构的纯数字 ID。
const (
	sellerID = "2200000000001"
	buyerID  = "3300000000002"
)

// newSellerGateway 创建只有一个已登录卖家的平台模拟器，返回模拟器、真实客户端和卖家初始 Cookie。
func newSellerGateway(t *testing.T) (*mtopsim.Gateway, *mtop.ClientImpl, string) {
	t.Helper()
	// gateway 是每个用例独立的平台状态。
	gateway := mtopsim.New()
	// cookie 是卖家登录后的初始 Cookie。
	cookie := gateway.AddAccount(sellerID, "测试卖家")
	return gateway, gateway.Client(), cookie
}

// paidOrder 在模拟器中创建一件商品和一笔已付款订单，返回商品 ID 和订单号。
func paidOrder(gateway *mtopsim.Gateway, input mtopsim.OrderInput) (string, string) {
	// itemID 是卖家的在售商品。
	itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", len(input.Specs) > 0)
	input.ItemID, input.BuyerID, input.BuyerNick = itemID, buyerID, "测试买家"
	if input.Amount == "" {
		input.Amount = "9.90"
	}
	// orderID 是买家拍下后的订单号。
	orderID := gateway.PlaceOrder(input)
	gateway.PayOrder(orderID)
	return itemID, orderID
}

// TestReadAPIsRoundTripThroughStrictGateway 验证全部只读接口的真实请求都能通过模拟器的登录态、Token 和签名校验，并被正确解析。
func TestReadAPIsRoundTripThroughStrictGateway(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// itemID、orderID 是带两组规格、购买两件的已付款订单。
	itemID, orderID := paidOrder(gateway, mtopsim.OrderInput{
		Quantity: 2, Amount: "19.80", Specs: []mtopsim.Spec{{Name: "颜色", Value: "红色"}, {Name: "时长", Value: "月卡"}},
	})

	// profile、profileErr 是账号资料接口结果。
	profile, profileErr := client.FetchUserProfile(ctx, cookie)
	if profileErr != nil || profile.Nickname != "测试卖家" || profile.AvatarURL == "" {
		t.Fatalf("账号资料: %+v err=%v", profile, profileErr)
	}
	// login、loginErr 是登录态检查结果。
	login, loginErr := client.CheckLoginStatusContext(ctx, cookie)
	if loginErr != nil || login.Status != mtop.LoginStatusSuccess {
		t.Fatalf("登录态: %+v err=%v", login, loginErr)
	}
	// token、tokenErr 是 accessToken 换取结果。
	token, tokenErr := client.RefreshTokenContext(ctx, cookie)
	if tokenErr != nil || !strings.HasPrefix(token.AccessToken, "access-"+sellerID) || token.AccessTokenExpireAt <= time.Now().Unix() {
		t.Fatalf("accessToken: %+v err=%v", token, tokenErr)
	}
	// detail、detailErr 是订单详情结果；规格必须是完整组合，数量和金额来自平台。
	detail, detailErr := client.FetchOrderDetail(ctx, cookie, orderID)
	if detailErr != nil || detail.Quantity != "2" || detail.Amount != "19.80" || detail.OrderStatus != "2" ||
		detail.SpecName != "颜色；时长" || detail.SpecValue != "红色；月卡" {
		t.Fatalf("订单详情: %+v err=%v", detail, detailErr)
	}
	// sold、soldErr 是已售订单第一页；19 位订单号必须原样返回。
	sold, soldErr := client.FetchSoldOrdersPage(ctx, cookie, 1, 30)
	if soldErr != nil || len(sold.Items) != 1 || sold.NextPage || sold.Items[0].OrderID != orderID ||
		sold.Items[0].ItemID != itemID || sold.Items[0].BuyerID != buyerID || sold.Items[0].OrderStatus != "pending_ship" ||
		sold.Items[0].Quantity != "2" || sold.Items[0].Amount != "19.80" || sold.Items[0].IsBargain {
		t.Fatalf("已售订单: %+v err=%v", sold, soldErr)
	}
	// chatUser、chatUserErr 是会话买家资料结果。
	chatUser, chatUserErr := client.FetchChatUserInfo(ctx, cookie, gateway.Order(orderID).ChatID)
	if chatUserErr != nil || chatUser.Nickname != "测试买家" {
		t.Fatalf("会话买家: %+v err=%v", chatUser, chatUserErr)
	}
	// ownItems、ownItemsErr 是会话内查询本账号商品的结果；协议相对图片地址必须补全为 https。
	ownItems, ownItemsErr := client.SearchChatItems(ctx, cookie, gateway.Order(orderID).ChatID, "own", "会员", 1)
	if ownItemsErr != nil || len(ownItems.Items) != 1 || ownItems.Items[0].ItemID != itemID || !strings.HasPrefix(ownItems.Items[0].ImageURL, "https://") {
		t.Fatalf("会话商品: %+v err=%v", ownItems, ownItemsErr)
	}
	// candidates、candidatesErr 是发布类目候选；主推荐在首位且候选不丢失。
	candidates, _, candidatesErr := client.RecommendPublishCategoryCandidates(ctx, cookie, "视频会员")
	if candidatesErr != nil || len(candidates) != 2 || candidates[0].CatName != "虚拟物品" || candidates[1].CatName != "游戏软件平台" {
		t.Fatalf("类目候选: %+v err=%v", candidates, candidatesErr)
	}
	// 每个只读操作只对应一次平台请求，没有隐藏的额外调用。
	if total := gateway.CallCount(); total != 8 { // total 是平台实际收到的请求数。
		t.Fatalf("只读接口共应请求 8 次，实际 %d 次：%v", total, gateway.CallSequence())
	}
}

// TestItemListPaginationAndOmittedCardList 验证商品分页同步，以及“没有在售商品时平台省略 cardList”必须按空列表成功处理。
func TestItemListPaginationAndOmittedCardList(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()

	// empty、emptyErr 是没有任何在售商品时的全量同步结果。
	empty, emptyErr := client.FetchAllItems(ctx, cookie, 20, 5)
	if emptyErr != nil || len(empty.Items) != 0 {
		t.Fatalf("省略 cardList 必须是成功的空列表: %+v err=%v", empty, emptyErr)
	}
	if gateway.CallCount(mtopsim.APIItemList) != 1 {
		t.Fatalf("空列表只应请求一次，实际 %v", gateway.CallSequence())
	}

	// index 是批量添加商品的序号，共 5 件，其中第 1 件为多规格。
	for index := 0; index < 5; index++ {
		gateway.AddItem(sellerID, "商品"+string(rune('A'+index)), "1.00", index == 0)
	}
	gateway.ResetCalls()
	// all、allErr 是每页 2 件的全量同步结果，应分 3 页取完。
	all, allErr := client.FetchAllItems(ctx, cookie, 2, 10)
	if allErr != nil || len(all.Items) != 5 || !all.Items[0].IsMultiSpec || all.Items[1].IsMultiSpec || all.Items[0].PriceText != "¥1.00" {
		t.Fatalf("分页同步: %+v err=%v", all, allErr)
	}
	if pages := gateway.CallCount(mtopsim.APIItemList); pages != 3 { // pages 是实际请求的页数。
		t.Fatalf("5 件商品每页 2 件应请求 3 页，实际 %d 页", pages)
	}
	// limited、limitedErr 是页数上限不足时的结果：必须报错而不是返回不完整列表。
	limited, limitedErr := client.FetchAllItems(ctx, cookie, 2, 2)
	if limitedErr == nil || limited != nil {
		t.Fatalf("达到页数上限必须失败: %+v err=%v", limited, limitedErr)
	}
}

// TestTokenExpiryRecoversWithoutDuplicateSideEffect 验证签名 Token 过期时客户端自动换用新 Token 重试，且确认发货只执行一次。
func TestTokenExpiryRecoversWithoutDuplicateSideEffect(t *testing.T) {
	t.Run("业务接口响应直接下发新 Token", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		// orderID 是待确认发货的已付款订单。
		_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
		gateway.ExpireToken(sellerID)
		// ok、updated、err 是确认发货结果和客户端合并后的 Cookie。
		ok, _, updated, err := client.ConsignContext(context.Background(), cookie, orderID)
		if err != nil || !ok {
			t.Fatalf("Token 过期后应自动恢复: ok=%v err=%v", ok, err)
		}
		if !mtop.MTopTokenCookieChanged(cookie, updated) || protocol.SignToken(updated) != protocol.SignToken(gateway.Cookie(sellerID)) {
			t.Fatalf("返回的 Cookie 必须带平台新签发的 Token")
		}
		if sequence := strings.Join(gateway.CallSequence(), ","); sequence != mtopsim.APIConsign+","+mtopsim.APIConsign { // sequence 是平台收到的接口序列。
			t.Fatalf("应为确认发货两次且不调用 Token 接口，实际 %s", sequence)
		}
		if order := gateway.Order(orderID); order.ConsignCount != 1 || order.Status != mtopsim.OrderShipped { // order 是平台侧订单事实。
			t.Fatalf("确认发货必须恰好生效一次: %+v", order)
		}
	})
	t.Run("业务接口不下发时主动调用 Token 接口", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		gateway.SetTokenRotationOnBusinessAPI(false)
		// orderID 是待确认发货的已付款订单。
		_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
		gateway.ExpireToken(sellerID)
		// ok、err 是确认发货结果。
		ok, _, _, err := client.ConsignContext(context.Background(), cookie, orderID)
		if err != nil || !ok {
			t.Fatalf("主动刷新 Token 后应成功: ok=%v err=%v", ok, err)
		}
		// want 是期望的接口序列：过期的确认发货 → Token 接口轮换 → Token 接口成功 → 重签的确认发货。
		want := strings.Join([]string{mtopsim.APIConsign, mtopsim.APIToken, mtopsim.APIToken, mtopsim.APIConsign}, ",")
		if sequence := strings.Join(gateway.CallSequence(), ","); sequence != want { // sequence 是平台收到的接口序列。
			t.Fatalf("接口序列应为 %s，实际 %s", want, sequence)
		}
		if gateway.Order(orderID).ConsignCount != 1 {
			t.Fatalf("确认发货必须恰好生效一次")
		}
	})
}

// TestSessionExpiryAndRiskControlStopImmediately 验证 Session 失效和风控都被正确分类，且客户端不做任何重试。
func TestSessionExpiryAndRiskControlStopImmediately(t *testing.T) {
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	t.Run("Session 失效", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		// orderID 是待确认发货的已付款订单。
		_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
		gateway.ExpireSession(sellerID)
		// ok、err 是使用旧 Cookie 确认发货的结果。
		ok, _, _, err := client.ConsignContext(ctx, cookie, orderID)
		if ok || !mtop.IsSessionExpiredErr(err) || mtop.IsRiskVerificationErr(err) {
			t.Fatalf("应分类为 Session 失效: ok=%v err=%v", ok, err)
		}
		if gateway.CallCount() != 1 || gateway.Order(orderID).ConsignCount != 0 {
			t.Fatalf("Session 失效不得重试也不得生效: %v", gateway.CallSequence())
		}
		// relogin 是重新登录后的 Cookie，用它重试必须成功。
		relogin := gateway.Relogin(sellerID)
		if ok, _, _, err = client.ConsignContext(ctx, relogin, orderID); err != nil || !ok {
			t.Fatalf("重新登录后应成功: ok=%v err=%v", ok, err)
		}
	})
	t.Run("风控", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		// orderID 是待确认发货的已付款订单。
		_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
		gateway.SetRiskControl(sellerID, true)
		// calls 是风控期间依次尝试的各类接口，每个都必须只请求一次并返回风控错误。
		calls := map[string]func() error{
			"确认发货": func() error { _, _, _, err := client.ConsignContext(ctx, cookie, orderID); return err },             // err 是风控期间该接口返回的错误。
			"订单详情": func() error { _, err := client.FetchOrderDetail(ctx, cookie, orderID); return err },                 // err 是风控期间该接口返回的错误。
			"商品列表": func() error { _, err := client.FetchAllItems(ctx, cookie, 20, 5); return err },                      // err 是风控期间该接口返回的错误。
			"已售订单": func() error { _, err := client.FetchSoldOrdersPage(ctx, cookie, 1, 30); return err },                // err 是风控期间该接口返回的错误。
			"评价买家": func() error { _, err := client.RateBuyer(ctx, cookie, orderID, "好评"); return err },                  // err 是风控期间该接口返回的错误。
			"擦亮商品": func() error { _, err := client.PolishItem(ctx, cookie, gateway.Order(orderID).ItemID); return err }, // err 是风控期间该接口返回的错误。
			"换取令牌": func() error { _, err := client.RefreshTokenContext(ctx, cookie); return err },                       // err 是风控期间该接口返回的错误。
		}
		// name、call 是接口名称和对应调用。
		for name, call := range calls {
			gateway.ResetCalls()
			if err := call(); !mtop.IsRiskVerificationErr(err) { // err 是风控期间的调用结果。
				t.Fatalf("%s 应分类为风控: %v", name, err)
			}
			if count := gateway.CallCount(); count != 1 { // count 是该操作实际发出的请求数。
				t.Fatalf("%s 遇风控必须只请求一次，实际 %d 次：%v", name, count, gateway.CallSequence())
			}
		}
		if gateway.Order(orderID).ConsignCount != 0 {
			t.Fatalf("风控期间不得产生业务副作用")
		}
	})
}

// TestConsignStateMachine 验证确认发货在各订单阶段的结果：未付款拒绝、敏感词拒绝且状态不变、付款后成功并携带发货凭证。
func TestConsignStateMachine(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// itemID 是卖家的在售商品；orderID 是尚未付款的订单。
	itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
	// orderID 是买家已拍下、尚未付款的平台订单号。
	orderID := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: buyerID, Amount: "9.90"})

	// ok、ret、err 是未付款订单确认发货的结果：平台明确拒绝，属于业务失败而不是错误。
	ok, ret, _, err := client.ConsignContext(ctx, cookie, orderID)
	if err != nil || ok || len(ret) != 1 || ret[0] != mtopsim.RetOrderStatusInvalid {
		t.Fatalf("未付款订单应被业务拒绝: ok=%v ret=%v err=%v", ok, ret, err)
	}
	gateway.PayOrder(orderID)
	if ok, ret, _, err = client.ConsignContextWithDelivery(ctx, cookie, orderID, "卡密含违禁内容", nil); err != nil || ok || ret[0] != mtopsim.RetSensitiveWord {
		t.Fatalf("敏感词应被明确拒绝: ok=%v ret=%v err=%v", ok, ret, err)
	}
	if gateway.Order(orderID).Status != mtopsim.OrderPendingShip {
		t.Fatalf("敏感词拒绝后订单必须仍为待发货")
	}
	if ok, _, _, err = client.ConsignContextWithDelivery(ctx, cookie, orderID, "卡号 A-1", []string{"https://img.example/a.png"}); err != nil || !ok {
		t.Fatalf("已付款订单应确认发货成功: ok=%v err=%v", ok, err)
	}
	// order 是平台侧订单事实，发货凭证必须与请求一致。
	order := gateway.Order(orderID)
	if order.Status != mtopsim.OrderShipped || order.ConsignText != "卡号 A-1" || len(order.ConsignPictures) != 1 {
		t.Fatalf("平台订单事实: %+v", order)
	}
	// 其他卖家不能操作该订单。
	gateway.AddAccount("2200000000009", "其他卖家")
	if ok, ret, _, err = client.ConsignContext(ctx, gateway.Cookie("2200000000009"), orderID); err != nil || ok || ret[0] != mtopsim.RetOrderNotFound {
		t.Fatalf("其他卖家应看不到该订单: ok=%v ret=%v err=%v", ok, ret, err)
	}
}

// TestBargainFreeShippingStateMachine 验证砍价订单：未刀成不能确认发货，免拼遇团购同步延迟自动重试，免拼只让订单刀成、不改变发货状态。
func TestBargainFreeShippingStateMachine(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// itemID、orderID 是已付款但尚未刀成的砍价订单。
	itemID, orderID := paidOrder(gateway, mtopsim.OrderInput{Bargain: true})

	// sold、soldErr 是已售订单列表；未刀成的砍价订单必须被识别为砍价订单。
	sold, soldErr := client.FetchSoldOrdersPage(ctx, cookie, 1, 30)
	if soldErr != nil || !sold.Items[0].IsBargain {
		t.Fatalf("未刀成订单应识别为砍价订单: %+v err=%v", sold, soldErr)
	}
	// ok、ret、err 是未刀成时确认发货的结果。
	ok, ret, _, err := client.ConsignContext(ctx, cookie, orderID)
	if err != nil || ok || ret[0] != mtopsim.RetGrouponNotFormed {
		t.Fatalf("未刀成不能确认发货: ok=%v ret=%v err=%v", ok, ret, err)
	}
	if ok, ret, _, err = client.FreeShippingContext(ctx, cookie, orderID, itemID, "3300000000999"); err != nil || ok || ret[0] != mtopsim.RetBadRequest {
		t.Fatalf("买家不匹配的免拼必须被拒绝: ok=%v ret=%v err=%v", ok, ret, err)
	}
	gateway.SetGrouponSyncLag(orderID, 2)
	gateway.ResetCalls()
	if ok, _, _, err = client.FreeShippingContext(ctx, cookie, orderID, itemID, buyerID); err != nil || !ok {
		t.Fatalf("团购同步延迟后免拼应成功: ok=%v err=%v", ok, err)
	}
	if attempts := gateway.CallCount(mtopsim.APIFreeShipping); attempts != 3 { // attempts 是免拼接口实际请求次数。
		t.Fatalf("两次同步延迟应共请求 3 次，实际 %d 次", attempts)
	}
	// order 是免拼后的平台订单事实：已刀成但仍待发货。
	order := gateway.Order(orderID)
	if !order.BargainFormed || !order.FreeShipped || order.Status != mtopsim.OrderPendingShip {
		t.Fatalf("免拼只应让订单刀成: %+v", order)
	}
	if ok, ret, _, err = client.FreeShippingContext(ctx, cookie, orderID, itemID, buyerID); err != nil || ok || ret[0] != mtopsim.RetGrouponNotPending {
		t.Fatalf("重复免拼应被业务拒绝: ok=%v ret=%v err=%v", ok, ret, err)
	}
	if ok, _, _, err = client.ConsignContext(ctx, cookie, orderID); err != nil || !ok {
		t.Fatalf("刀成后确认发货应成功: ok=%v err=%v", ok, err)
	}
}

// TestAdjustPriceStateMachine 验证改价只对待付款订单生效，付款后被明确拒绝。
func TestAdjustPriceStateMachine(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// itemID 是卖家的在售商品；orderID 是待付款订单。
	itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
	// orderID 是买家已拍下、尚未付款的平台订单号。
	orderID := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: buyerID, Amount: "9.90"})

	// ok、err 是把价格改为 6.66 元的结果。
	ok, _, _, err := client.AdjustOrderPriceContext(ctx, cookie, orderID, 666)
	if err != nil || !ok || gateway.Order(orderID).Amount != "6.66" {
		t.Fatalf("待付款订单改价应成功: ok=%v err=%v amount=%s", ok, err, gateway.Order(orderID).Amount)
	}
	gateway.PayOrder(orderID)
	// kind、classified 是付款后改价失败的分类：平台明确的业务拒绝。
	ok, _, _, err = client.AdjustOrderPriceContext(ctx, cookie, orderID, 100)
	// kind、classified 是付款后改价错误的分类及其是否可识别。
	if kind, classified := mtop.MTopErrorKindOf(err); ok || !classified || kind != mtop.MTopErrorBusiness {
		t.Fatalf("付款后改价应为业务拒绝: ok=%v err=%v", ok, err)
	}
	if gateway.Order(orderID).Amount != "6.66" {
		t.Fatalf("被拒绝的改价不得修改金额")
	}
}

// TestRateAndPolishAccountTasks 验证评价买家和擦亮商品的平台状态机及重复操作的分类。
func TestRateAndPolishAccountTasks(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// itemID、orderID 是已付款订单及其商品。
	itemID, orderID := paidOrder(gateway, mtopsim.OrderInput{})

	// rateErr 是对未完成订单评价的结果：平台明确不允许。
	_, rateErr := client.RateBuyer(ctx, cookie, orderID, "好评")
	if !mtop.IsRateOrderNotRateableErr(rateErr) {
		t.Fatalf("未完成订单不能评价: %v", rateErr)
	}
	gateway.ConfirmReceipt(orderID)
	// pending、pendingErr 是待评价订单列表。
	pending, pendingErr := client.FetchPendingRateOrders(ctx, cookie, 1, 50)
	if pendingErr != nil || len(pending.Orders) != 1 || pending.Orders[0].TradeID != orderID || pending.Orders[0].ItemID != itemID {
		t.Fatalf("待评价列表: %+v err=%v", pending, pendingErr)
	}
	if _, rateErr = client.RateBuyer(ctx, cookie, orderID, "合作愉快"); rateErr != nil || gateway.Order(orderID).SellerFeedback != "合作愉快" {
		t.Fatalf("评价应成功: %v", rateErr)
	}
	if _, rateErr = client.RateBuyer(ctx, cookie, orderID, "再评一次"); !mtop.IsRateAlreadyRatedErr(rateErr) {
		t.Fatalf("重复评价应分类为已评价: %v", rateErr)
	}
	if pending, pendingErr = client.FetchPendingRateOrders(ctx, cookie, 1, 50); pendingErr != nil || len(pending.Orders) != 0 {
		t.Fatalf("评价后待评价列表应为空: %+v err=%v", pending, pendingErr)
	}

	// now 是模拟器时钟的当前时刻，用于不真实等待地跨天。
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	gateway.SetClock(func() time.Time { return now })
	// cookie 在更换时钟后重新读取，避免旧 Token 的过期时间早于模拟时钟。
	cookie = gateway.Relogin(sellerID)
	gateway.ResetCalls()
	// polished、polishErr 是当天第一次擦亮的结果。
	polished, polishErr := client.PolishItem(ctx, cookie, itemID)
	if polishErr != nil || !polished.Success {
		t.Fatalf("第一次擦亮应成功: %+v err=%v", polished, polishErr)
	}
	if polished, polishErr = client.PolishItem(ctx, cookie, itemID); polishErr != nil || !polished.Success || polished.Message != "商品今天已经擦亮" {
		t.Fatalf("当天重复擦亮应按已擦亮处理: %+v err=%v", polished, polishErr)
	}
	if gateway.CallCount(mtopsim.APIPolishBackup) != 0 {
		t.Fatalf("已擦亮不应再请求备用接口: %v", gateway.CallSequence())
	}
	now = now.Add(24 * time.Hour)
	if polished, polishErr = client.PolishItem(ctx, cookie, itemID); polishErr != nil || polished.Message == "商品今天已经擦亮" {
		t.Fatalf("次日应可再次擦亮: %+v err=%v", polished, polishErr)
	}
}

// TestSoldOrdersPagination 验证已售订单分页：最新订单在前，nextPage 准确，各阶段状态正确归一。
func TestSoldOrdersPagination(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// itemID 是卖家的在售商品。
	itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
	// waitPay、shipped、completed、closed 是处于不同阶段的四笔订单，按创建先后排列。
	waitPay := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: buyerID, Amount: "9.90"})
	// shipped 是随后会被确认发货的订单号。
	shipped := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: buyerID, Amount: "9.90"})
	// completed 是随后会被买家确认收货的订单号。
	completed := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: buyerID, Amount: "9.90"})
	// closed 是随后会被关闭的订单号。
	closed := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: buyerID, Amount: "9.90"})
	gateway.PayOrder(shipped)
	if ok, _, _, err := client.ConsignContext(ctx, cookie, shipped); err != nil || !ok { // ok、err 是确认发货结果。
		t.Fatalf("确认发货: ok=%v err=%v", ok, err)
	}
	gateway.ConfirmReceipt(completed)
	gateway.CloseOrder(closed)

	// want 是按“最新在前”排列的期望订单号与归一化状态。
	want := [][2]string{{closed, "cancelled"}, {completed, "completed"}, {shipped, "shipped"}, {waitPay, "processing"}}
	// got 收集两页的订单号与状态。
	var got [][2]string
	// pageNumber 是请求的页码，每页 3 条，共两页。
	for pageNumber := 1; pageNumber <= 2; pageNumber++ {
		// page、pageErr 是当前页结果。
		page, pageErr := client.FetchSoldOrdersPage(ctx, cookie, pageNumber, 3)
		if pageErr != nil || page.NextPage != (pageNumber == 1) || page.TotalCount != 4 {
			t.Fatalf("第 %d 页: %+v err=%v", pageNumber, page, pageErr)
		}
		// item 是当前页中的一笔订单。
		for _, item := range page.Items {
			got = append(got, [2]string{item.OrderID, item.OrderStatus})
		}
	}
	if len(got) != len(want) {
		t.Fatalf("订单数量 got=%v want=%v", got, want)
	}
	// index 是订单在期望序列中的位置。
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("第 %d 笔订单 got=%v want=%v", index+1, got[index], want[index])
		}
	}
}

// TestPublishItemFlow 验证发布链路：上传图片 → 推荐类目 → 发布，发布后的商品出现在商品列表中。
func TestPublishItemFlow(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// request 是带两张图片的虚拟商品发布请求。
	request := mtop.PublishItemRequest{
		Title: "视频会员月卡", Description: "自动发货", PriceCents: 990, Quantity: 5, Virtual: true,
		Images: []mtop.PublishImage{{Filename: "a.png", ContentType: "image/png", Data: []byte("png-a")}, {Filename: "b.png", ContentType: "image/png", Data: []byte("png-b")}},
	}
	// published、publishErr 是发布结果。
	published, publishErr := client.PublishItem(ctx, cookie, request)
	if publishErr != nil || published.ItemID == "" || published.CategoryName != "虚拟物品" || !strings.Contains(published.ImageURL, "/sim/"+sellerID+"/") {
		t.Fatalf("发布: %+v err=%v", published, publishErr)
	}
	// want 是期望的接口序列：每张图片上传一次，然后推荐类目，最后发布。
	want := strings.Join([]string{mtopsim.APIUpload, mtopsim.APIUpload, mtopsim.APICategoryRecommend, mtopsim.APIPublish}, ",")
	if sequence := strings.Join(gateway.CallSequence(), ","); sequence != want { // sequence 是平台收到的接口序列。
		t.Fatalf("接口序列应为 %s，实际 %s", want, sequence)
	}
	// item、exists 是平台侧新商品及其是否存在。
	item, exists := gateway.Item(published.ItemID)
	if !exists || item.Title != "视频会员月卡" || item.Price != "9.90" || item.SellerID != sellerID {
		t.Fatalf("平台商品事实: %+v exists=%v", item, exists)
	}
	// listed、listErr 是发布后的商品列表，新商品必须可被同步到。
	listed, listErr := client.FetchAllItems(ctx, cookie, 20, 5)
	if listErr != nil || len(listed.Items) != 1 || listed.Items[0].ID != published.ItemID {
		t.Fatalf("发布后同步: %+v err=%v", listed, listErr)
	}

	// 标题无法识别类目时回退到默认虚拟类目，发布仍然成功。
	request.Title = "无法识别的标题"
	if published, publishErr = client.PublishItem(ctx, cookie, request); publishErr != nil || published.CategoryID != mtop.DefaultVirtualPublishCategory().CatID {
		t.Fatalf("类目回退: %+v err=%v", published, publishErr)
	}

	// 上传服务失败时不得继续请求类目或发布接口。
	gateway.ResetCalls()
	gateway.InjectFault(mtopsim.APIUpload, mtopsim.Fault{HTTPStatus: http.StatusBadGateway, Body: "bad gateway"})
	if _, publishErr = client.PublishItem(ctx, cookie, request); publishErr == nil || gateway.CallCount(mtopsim.APIPublish, mtopsim.APICategoryRecommend) != 0 {
		t.Fatalf("上传失败必须终止发布: err=%v calls=%v", publishErr, gateway.CallSequence())
	}
}

// TestGatewayFaultInjection 验证注入的网关异常：HTTP 错误、截断正文、网络错误，以及“平台已执行但响应丢失”的结果未知场景。
func TestGatewayFaultInjection(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// orderID 是待确认发货的已付款订单。
	_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
	// networkErr 是注入的网络层错误。
	networkErr := errors.New("connection reset by peer")

	gateway.InjectFault(mtopsim.APIConsign,
		mtopsim.Fault{HTTPStatus: http.StatusBadGateway, Body: "<html>502</html>"},
		mtopsim.Fault{Body: `{"ret":["SUCCESS::调用成`},
		mtopsim.Fault{Err: networkErr},
	)
	// attempt 是依次消费三种注入异常的尝试序号；每次都必须返回错误且平台订单不变。
	for attempt := 1; attempt <= 3; attempt++ {
		if ok, _, _, err := client.ConsignContext(ctx, cookie, orderID); err == nil || ok { // ok、err 是本次确认发货结果。
			t.Fatalf("第 %d 种异常必须返回错误: ok=%v err=%v", attempt, ok, err)
		}
	}
	if gateway.Order(orderID).ConsignCount != 0 || gateway.CallCount(mtopsim.APIConsign) != 3 {
		t.Fatalf("请求未到达业务时不得生效，且每次异常只请求一次: %v", gateway.CallSequence())
	}

	// 平台已执行但响应丢失：调用方得到错误，而平台订单已经发货，重复请求会得到“已发货”。
	gateway.InjectFault(mtopsim.APIConsign, mtopsim.Fault{Err: networkErr, AfterEffect: true})
	// ok、err 是响应丢失时的确认发货结果。
	ok, _, _, err := client.ConsignContext(ctx, cookie, orderID)
	if err == nil || ok || !errors.Is(err, networkErr) {
		t.Fatalf("响应丢失必须返回原始网络错误: ok=%v err=%v", ok, err)
	}
	if gateway.Order(orderID).ConsignCount != 1 {
		t.Fatalf("响应丢失时平台应已发货")
	}

	// 已取消的上下文不会到达平台。
	gateway.ResetCalls()
	// canceled、cancel 是立即取消的上下文。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, detailErr := client.FetchOrderDetail(canceled, cookie, orderID); !errors.Is(detailErr, context.Canceled) || gateway.CallCount() != 0 { // detailErr 是取消后的调用结果。
		t.Fatalf("已取消的请求不得到达平台: err=%v calls=%v", detailErr, gateway.CallSequence())
	}
}

// TestGatewayRejectsForgedSignatureAndUnknownAPI 验证模拟器本身的严格性：签名与请求内容不一致、或请求未登记的接口都会被拒绝。
func TestGatewayRejectsForgedSignatureAndUnknownAPI(t *testing.T) {
	// gateway、cookie 是独立的平台状态和卖家 Cookie。
	gateway := mtopsim.New()
	// cookie 是卖家登录后的初始 Cookie。
	cookie := gateway.AddAccount(sellerID, "测试卖家")
	// send 用给定的签名数据和请求数据直接向模拟器发一次请求，返回响应正文。
	send := func(api, signedData, sentData string) string {
		// timestamp 是参与签名的毫秒时间戳。
		timestamp := "1700000000000"
		// query 是带签名的查询参数；签名按 signedData 计算。
		query := url.Values{"appKey": {protocol.SignAppKey}, "t": {timestamp}, "sign": {protocol.GenerateSign(timestamp, protocol.SignToken(cookie), signedData)}}
		// request、requestErr 是直接构造的 HTTP 请求。
		request, requestErr := http.NewRequest(http.MethodPost, "https://h5api.m.goofish.com/h5/"+api+"/1.0/?"+query.Encode(), strings.NewReader("data="+url.QueryEscape(sentData)))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Cookie", cookie)
		// response、responseErr 是模拟器的响应。
		response, responseErr := gateway.RoundTrip(request)
		if responseErr != nil {
			t.Fatal(responseErr)
		}
		defer response.Body.Close()
		// body 保存响应正文的前 512 字节，足以包含平台标记。
		body := make([]byte, 512)
		// read 是实际读取的字节数。
		read, _ := response.Body.Read(body)
		return string(body[:read])
	}
	if body := send(mtopsim.APILoginUser, `{}`, `{}`); !strings.Contains(body, "SUCCESS") { // body 是签名正确时的响应。
		t.Fatalf("签名正确应成功: %s", body)
	}
	if body := send(mtopsim.APIConsign, `{"orderId":"1"}`, `{"orderId":"2"}`); !strings.Contains(body, "FAIL_SYS_ILLEGAL_ACCESS") { // body 是请求内容被篡改时的响应。
		t.Fatalf("签名与内容不一致应被拒绝: %s", body)
	}
	if body := send("mtop.idle.unknown.api", `{}`, `{}`); !strings.Contains(body, "FAIL_SYS_API_NOT_FOUNDED") { // body 是请求未登记接口时的响应。
		t.Fatalf("未登记接口应被拒绝: %s", body)
	}
}

// TestTokenRetryExhaustionSendsNoTrailingRefresh 验证签名 Token 持续过期时的请求次数上限：
// 业务接口最多尝试 4 次，相邻两次之间各刷新一次 Token，共 3 次；最后一次失败后不得再多刷新一次无人使用的 Token。
// Token 重试间隔是生产常量（1 秒），各子用例并行执行以控制总耗时。
func TestTokenRetryExhaustionSendsNoTrailingRefresh(t *testing.T) {
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// expired 是连续 4 次“Token 已过期且未下发新 Token”的平台响应。
	expired := []mtopsim.Fault{{Ret: []string{mtopsim.RetTokenExpired}}, {Ret: []string{mtopsim.RetTokenExpired}}, {Ret: []string{mtopsim.RetTokenExpired}}, {Ret: []string{mtopsim.RetTokenExpired}}}
	// cases 列出三个共用同一重试结构的只读接口及其调用方式。
	cases := []struct {
		// name 是子用例名；api 是被注入故障的平台接口。
		name, api string
		// call 用真实客户端发起一次调用并返回其错误。
		call func(client *mtop.ClientImpl, cookie string) error
	}{
		{"商品列表", mtopsim.APIItemList, func(client *mtop.ClientImpl, cookie string) error {
			_, callErr := client.FetchAllItems(ctx, cookie, 20, 1) // callErr 是商品列表调用的错误。
			return callErr
		}},
		{"账号资料", mtopsim.APIUserNav, func(client *mtop.ClientImpl, cookie string) error {
			_, callErr := client.FetchUserProfile(ctx, cookie) // callErr 是账号资料调用的错误。
			return callErr
		}},
		{"会话商品", mtopsim.APIChatItemSearch, func(client *mtop.ClientImpl, cookie string) error {
			_, callErr := client.SearchChatItems(ctx, cookie, "chat-1", "own", "会员", 1) // callErr 是会话商品查询的错误。
			return callErr
		}},
	}
	// testCase 是当前被验证的接口。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
			gateway, client, cookie := newSellerGateway(t)
			gateway.InjectFault(testCase.api, expired...)
			// callErr 是重试耗尽后的错误，必须仍被识别为 Token 过期。
			callErr := testCase.call(client, cookie)
			if !mtop.IsMTopTokenExpiredErr(callErr) {
				t.Fatalf("重试耗尽应返回 Token 过期错误，实际 %v", callErr)
			}
			if business, refresh := gateway.CallCount(testCase.api), gateway.CallCount(mtopsim.APIToken); business != 4 || refresh != 3 { // business、refresh 是业务接口和 Token 接口的请求数。
				t.Fatalf("应请求业务接口 4 次、刷新 Token 3 次，实际 %d、%d：%v", business, refresh, gateway.CallSequence())
			}
		})
	}
}

// TestPolishSkipsBackupWhenTokenRetryExhausted 验证擦亮主接口因签名 Token 重试耗尽失败时不再请求备用接口：
// Token 问题与具体接口无关，备用接口必然同样失败。
func TestPolishSkipsBackupWhenTokenRetryExhausted(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是待擦亮的在售商品。
	itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
	// index 是注入的第几次 Token 过期响应；注入数量多于客户端的最大尝试次数。
	for index := 0; index < 8; index++ {
		gateway.InjectFault(mtopsim.APIPolish, mtopsim.Fault{Ret: []string{mtopsim.RetTokenExpired}})
	}
	// polishErr 是擦亮结果，必须是 Token 过期错误。
	_, polishErr := client.PolishItem(context.Background(), cookie, itemID)
	if !mtop.IsMTopTokenExpiredErr(polishErr) {
		t.Fatalf("应返回 Token 过期错误，实际 %v", polishErr)
	}
	if backup := gateway.CallCount(mtopsim.APIPolishBackup); backup != 0 { // backup 是备用擦亮接口的请求数。
		t.Fatalf("Token 重试耗尽后不得请求备用接口，实际 %d 次：%v", backup, gateway.CallSequence())
	}
}
