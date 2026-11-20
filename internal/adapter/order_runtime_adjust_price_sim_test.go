package adapter

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// TestOrderRuntimeAdjustPriceAgainstPlatformSimulator 用真实 mtop 客户端和平台模拟器验证订单运行时的改价全链路：
// 待付款改价成功并更新平台金额，付款后被平台业务拒绝且金额不变，且不触发任何其他平台接口。
func TestOrderRuntimeAdjustPriceAgainstPlatformSimulator(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是带 admin 与 cid 账号夹具的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin 是账号 cid 的所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// gateway 是进程内平台模拟器，不访问网络。
	gateway := mtopsim.New()
	// cookie 是模拟器签发的卖家登录 Cookie（伪造值），写入 cid 账号作为平台凭证。
	cookie := gateway.AddAccount("2200000000001", "测试卖家")
	if err = store.Cookies.UpdateValueExisting(ctx, "cid", cookie); err != nil {
		t.Fatal(err)
	}
	// client 是真实 ClientImpl，仅传输层被模拟器替换。
	client := gateway.Client()
	// runtime 绑定存储与模拟器客户端。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }}, nil, nil)
	// itemID 是模拟器中卖家的在售商品。
	itemID := gateway.AddItem("2200000000001", "视频会员月卡", "9.90", false)
	// orderID 是买家已拍下、尚未付款的订单号。
	orderID := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: "3300000000002", Amount: "9.90"})

	// ok 是待付款订单改价为 6.66 元的结果。
	ok := runtime.AdjustPrice(ctx, "cid", orderID, admin.ID, 666)
	if !ok.Success || ok.Err != nil || gateway.Order(orderID).Amount != "6.66" {
		t.Fatalf("待付款改价应成功: %+v amount=%s", ok, gateway.Order(orderID).Amount)
	}
	if calls := gateway.CallCount(mtopsim.APIAdjustPrice); calls != 1 { // calls 是改价接口实际请求次数，单次操作只能一次。
		t.Fatalf("改价接口应只请求 1 次: %d", calls)
	}

	gateway.PayOrder(orderID)
	// rejected 是付款后改价的结果：平台业务拒绝，金额保持 6.66。
	rejected := runtime.AdjustPrice(ctx, "cid", orderID, admin.ID, 100)
	// kind、classified 是错误分类及其是否可识别。
	kind, classified := mtop.MTopErrorKindOf(rejected.Err)
	if rejected.Success || !classified || kind != mtop.MTopErrorBusiness || errors.Is(rejected.Err, context.Canceled) {
		t.Fatalf("付款后改价应为业务拒绝: %+v", rejected)
	}
	if gateway.Order(orderID).Amount != "6.66" {
		t.Fatalf("被拒绝的改价不得修改金额: %s", gateway.Order(orderID).Amount)
	}
	if calls := gateway.CallCount(mtopsim.APIAdjustPrice); calls != 2 { // 拒绝是业务错误，不得被重放。
		t.Fatalf("业务拒绝不得重试，改价接口共 2 次: %d", calls)
	}
}
