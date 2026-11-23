package mtopsim_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件把“全部 MTOP 接口 × 全部异常”做成矩阵：每个生产客户端操作都必须在风控、Session 失效、签名 Token 耗尽时
// 立即止损，不重试风控、不扩散请求、不产生业务副作用；同时守卫生产代码新增的 MTOP 接口必须在模拟器登记。

// operation 描述一个生产客户端操作的测试夹具。
type operation struct {
	// name 是子用例名称。
	name string
	// api 是该操作首个请求的 MTOP 接口名，Token 耗尽故障注入在该接口上。
	api string
	// setup 在平台状态里准备该操作需要的商品与订单，返回调用闭包；调用闭包返回操作的错误。
	setup func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(ctx context.Context) error
	// tokenMatch 判断 Token 耗尽后的错误是否符合该操作所属领域的分类；为空使用 mtop.IsMTopTokenExpiredErr。
	// 发布域把 Token 失败包装为 PublishError(auth_expired)，因此需要自己的判定。
	tokenMatch func(error) bool
	// rejectedBeforeMTOP 为真表示 Session 失效时请求在 MTOP 之前就被非 MTOP 端点（图片上传）拒绝，因此 MTOP 请求数应为 0。
	rejectedBeforeMTOP bool
	// noSideEffect 校验平台状态没有被该操作修改；为空表示该操作本身没有业务副作用。
	noSideEffect func(t *testing.T, gateway *mtopsim.Gateway)
}

// allOperations 列出生产 Client 暴露的全部会请求 MTOP 的操作。新增操作必须在这里登记。
func allOperations() []operation {
	return []operation{
		{name: "账号资料", api: mtopsim.APIUserNav, setup: func(_ *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			return func(ctx context.Context) error { _, err := client.FetchUserProfile(ctx, cookie); return err } // err 是调用错误。
		}},
		{name: "商品列表单页", api: mtopsim.APIItemList, setup: func(_ *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			return func(ctx context.Context) error { _, err := client.FetchItemsPage(ctx, cookie, 1, 20); return err } // err 是调用错误。
		}},
		{name: "商品列表全量", api: mtopsim.APIItemList, setup: func(_ *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			return func(ctx context.Context) error { _, err := client.FetchAllItems(ctx, cookie, 20, 5); return err } // err 是调用错误。
		}},
		{name: "订单详情", api: mtopsim.APIOrderDetail, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// orderID 是夹具里该操作使用的订单号。
			_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
			return func(ctx context.Context) error { _, err := client.FetchOrderDetail(ctx, cookie, orderID); return err } // err 是调用错误。
		}},
		{name: "已售订单", api: mtopsim.APISoldOrders, setup: func(_ *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			return func(ctx context.Context) error { _, err := client.FetchSoldOrdersPage(ctx, cookie, 1, 30); return err } // err 是调用错误。
		}},
		{name: "会话买家", api: mtopsim.APIChatUser, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// orderID 是夹具里该操作使用的订单号。
			_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
			return func(ctx context.Context) error {
				_, err := client.FetchChatUserInfo(ctx, cookie, gateway.Order(orderID).ChatID) // err 是调用错误。
				return err
			}
		}},
		{name: "会话商品", api: mtopsim.APIChatItemSearch, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// orderID 是夹具里该操作使用的订单号。
			_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
			return func(ctx context.Context) error {
				_, err := client.SearchChatItems(ctx, cookie, gateway.Order(orderID).ChatID, "own", "会员", 1) // err 是调用错误。
				return err
			}
		}},
		{name: "类目推荐", api: mtopsim.APICategoryRecommend, tokenMatch: isPublishTokenExpired, setup: func(_ *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			return func(ctx context.Context) error {
				_, _, err := client.RecommendPublishCategoryCandidates(ctx, cookie, "视频会员") // err 是调用错误。
				return err
			}
		}},
		{name: "确认发货", api: mtopsim.APIConsign, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// orderID 是夹具里该操作使用的订单号。
			_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
			return func(ctx context.Context) error {
				_, _, _, err := client.ConsignContext(ctx, cookie, orderID) // err 是调用错误。
				return err
			}
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			for _, order := range allOrders(gateway) { // order 是平台上的订单。
				if order.ConsignCount != 0 {
					t.Fatalf("不得产生发货副作用: %+v", order)
				}
			}
		}},
		{name: "砍价免拼", api: mtopsim.APIFreeShipping, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// itemID 是夹具里该操作使用的商品标识；orderID 是夹具里该操作使用的订单号。
			itemID, orderID := paidOrder(gateway, mtopsim.OrderInput{Bargain: true})
			return func(ctx context.Context) error {
				_, _, _, err := client.FreeShippingContext(ctx, cookie, orderID, itemID, buyerID) // err 是调用错误。
				return err
			}
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			for _, order := range allOrders(gateway) { // order 是平台上的订单。
				if order.FreeShipped || order.BargainFormed {
					t.Fatalf("不得产生免拼副作用: %+v", order)
				}
			}
		}},
		{name: "订单改价", api: mtopsim.APIAdjustPrice, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// itemID 是夹具里该操作使用的商品标识。
			itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
			orderID := gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: buyerID, Amount: "9.90"}) // orderID 是待付款订单。
			return func(ctx context.Context) error {
				_, _, _, err := client.AdjustOrderPriceContext(ctx, cookie, orderID, 666) // err 是调用错误。
				return err
			}
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			for _, order := range allOrders(gateway) { // order 是平台上的订单。
				if order.Amount != "9.90" {
					t.Fatalf("不得产生改价副作用: %+v", order)
				}
			}
		}},
		{name: "待评价列表", api: mtopsim.APIRateList, setup: func(_ *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			return func(ctx context.Context) error {
				// err 是该操作调用的返回错误。
				_, err := client.FetchPendingRateOrders(ctx, cookie, 1, 20)
				return err
			} // err 是调用错误。
		}},
		{name: "评价买家", api: mtopsim.APIRateCreate, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// orderID 是夹具里该操作使用的订单号。
			_, orderID := paidOrder(gateway, mtopsim.OrderInput{})
			return func(ctx context.Context) error {
				// err 是该操作调用的返回错误。
				_, err := client.RateBuyer(ctx, cookie, orderID, "好评")
				return err
			} // err 是调用错误。
		}},
		{name: "擦亮商品", api: mtopsim.APIPolish, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// itemID 是夹具里该操作使用的商品标识。
			itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
			return func(ctx context.Context) error { _, err := client.PolishItem(ctx, cookie, itemID); return err } // err 是调用错误。
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			for _, item := range gateway.Items(sellerID) { // item 是平台上的商品。
				if item.PolishedOn != "" {
					t.Fatalf("不得产生擦亮副作用: %+v", item)
				}
			}
		}},
		{name: "下架商品", api: mtopsim.APIDownShelf, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// itemID 是夹具里该操作使用的商品标识。
			itemID := gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
			return func(ctx context.Context) error { _, err := client.DownShelfItem(ctx, cookie, itemID); return err } // err 是调用错误。
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			for _, item := range gateway.Items(sellerID) { // item 是平台上的商品。
				if !item.OnSale {
					t.Fatalf("不得产生下架副作用: %+v", item)
				}
			}
		}},
		{name: "商品编辑详情", api: mtopsim.APIItemEditDetail, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// itemID 是夹具里该操作使用的商品标识。
			itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)
			return func(ctx context.Context) error { _, err := client.FetchItemEditDetail(ctx, cookie, itemID); return err } // err 是调用错误。
		}},
		{name: "修改库存", api: mtopsim.APIItemEditDetail, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// itemID 是夹具里该操作使用的商品标识。
			itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)
			return func(ctx context.Context) error {
				_, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: itemID, Quantity: 8, ExpectedQuantity: 5}) // err 是调用错误。
				return err
			}
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			for _, item := range gateway.Items(sellerID) { // item 是平台上的商品。
				if item.EditCount != 0 || item.Quantity != 5 {
					t.Fatalf("不得产生库存修改副作用: %+v", item)
				}
			}
		}},
		{name: "重新上架", api: mtopsim.APIItemEditDetail, setup: func(gateway *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			// itemID 是夹具里该操作使用的商品标识。
			itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 1)
			gateway.SellOut(itemID)
			return func(ctx context.Context) error { _, err := client.RelistItem(ctx, cookie, itemID); return err } // err 是调用错误。
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			for _, item := range gateway.Items(sellerID) { // item 是平台上的商品。
				if item.OnSale || item.EditCount != 0 {
					t.Fatalf("不得产生重新上架副作用: %+v", item)
				}
			}
		}},
		{name: "发布商品", api: mtopsim.APICategoryRecommend, tokenMatch: isPublishTokenExpired, rejectedBeforeMTOP: true, setup: func(_ *mtopsim.Gateway, client *mtop.ClientImpl, cookie string) func(context.Context) error {
			return func(ctx context.Context) error {
				// err 是该操作调用的返回错误。
				_, err := client.PublishItem(ctx, cookie, mtop.PublishItemRequest{
					Title: "视频会员月卡", Description: "自动发货", PriceCents: 990, Quantity: 5, Virtual: true,
					Images: []mtop.PublishImage{{Filename: "a.png", ContentType: "image/png", Data: []byte("png-a")}},
				}) // err 是调用错误。
				return err
			}
		}, noSideEffect: func(t *testing.T, gateway *mtopsim.Gateway) {
			t.Helper()
			if len(gateway.Items(sellerID)) != 0 {
				t.Fatalf("不得产生发布副作用: %+v", gateway.Items(sellerID))
			}
		}},
	}
}

// isPublishTokenExpired 判断错误是否是发布域对签名 Token 耗尽的分类（PublishError auth_expired）。
func isPublishTokenExpired(err error) bool {
	// publishErr 是错误链里的发布域错误。
	var publishErr *mtop.PublishError
	return errors.As(err, &publishErr) && publishErr.Code == mtop.PublishErrorTokenExpired
}

// allOrders 返回模拟器当前全部订单；通过已售订单之外的内部事实读取，供副作用断言使用。
func allOrders(gateway *mtopsim.Gateway) []mtopsim.Order {
	return gateway.Orders()
}

// mtopCalls 返回模拟器收到的 MTOP 请求数，不含图片上传端点（上传不是 MTOP 接口）。
func mtopCalls(gateway *mtopsim.Gateway) int {
	return gateway.CallCount() - gateway.CallCount(mtopsim.APIUpload)
}

// TestEveryOperationStopsAtOnceOnRiskControlAndSessionExpiry 验证每个操作遇到风控或 Session 失效都恰好只发一次 MTOP 请求，
// 返回可分类的错误且没有业务副作用，不会继续后续步骤（风控一旦触发只能靠降低频率避免，不能靠重试）。
func TestEveryOperationStopsAtOnceOnRiskControlAndSessionExpiry(t *testing.T) {
	// scenarios 是两类必须立即止损的平台状态及其错误判定。
	scenarios := []struct {
		name    string
		trigger func(gateway *mtopsim.Gateway)
		matches func(error) bool
	}{
		{"风控", func(gateway *mtopsim.Gateway) { gateway.SetRiskControl(sellerID, true) }, mtop.IsRiskVerificationErr},
		{"Session 失效", func(gateway *mtopsim.Gateway) { gateway.ExpireSession(sellerID) }, mtop.IsSessionExpiredErr},
	}
	// scenario 是当前平台状态场景。
	for _, scenario := range scenarios {
		// op 是当前被验证的客户端操作。
		for _, op := range allOperations() {
			t.Run(scenario.name+"/"+op.name, func(t *testing.T) {
				t.Parallel()
				// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
				gateway, client, cookie := newSellerGateway(t)
				// call 是已准备好夹具的操作调用。
				call := op.setup(gateway, client, cookie)
				gateway.ResetCalls()
				scenario.trigger(gateway)
				// err 是平台异常状态下的调用结果。
				err := call(context.Background())
				if err == nil {
					t.Fatalf("遇%s必须返回错误", scenario.name)
				}
				if (scenario.name != "Session 失效" || !op.rejectedBeforeMTOP) && !scenario.matches(err) {
					t.Fatalf("应分类为%s，实际 %v", scenario.name, err)
				}
				// wantCalls 是该场景应发出的 MTOP 请求数；先过图片上传的操作在 Session 失效时被上传端点拒绝，没有 MTOP 请求。
				wantCalls := 1
				if scenario.name == "Session 失效" && op.rejectedBeforeMTOP {
					wantCalls = 0
				}
				if count := mtopCalls(gateway); count != wantCalls { // count 是该操作实际发出的 MTOP 请求数。
					t.Fatalf("遇%s应请求 %d 次，实际 %d 次：%v", scenario.name, wantCalls, count, gateway.CallSequence())
				}
				if op.noSideEffect != nil {
					op.noSideEffect(t, gateway)
				}
			})
		}
	}
}

// TestEveryOperationBoundsRequestsWhenTokenRetryExhausted 验证签名 Token 连续过期且平台不下发新 Token 时，
// 每个操作的请求总数有硬上限（业务请求 ≤4、令牌刷新 ≤3），返回 Token 过期错误且没有业务副作用。
func TestEveryOperationBoundsRequestsWhenTokenRetryExhausted(t *testing.T) {
	// op 是当前被验证的客户端操作。
	for _, op := range allOperations() {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()
			// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
			gateway, client, cookie := newSellerGateway(t)
			// call 是已准备好夹具的操作调用。
			call := op.setup(gateway, client, cookie)
			gateway.ResetCalls()
			// index 是注入的第几次 Token 过期响应；数量多于客户端的最大尝试次数。
			for index := 0; index < 10; index++ {
				gateway.InjectFault(op.api, mtopsim.Fault{Ret: []string{mtopsim.RetTokenExpired}})
			}
			// err 是重试耗尽后的调用结果。
			err := call(context.Background())
			// match 是该操作所属领域对 Token 耗尽的错误判定。
			match := op.tokenMatch
			if match == nil {
				match = mtop.IsMTopTokenExpiredErr
			}
			if err == nil || !match(err) {
				t.Fatalf("重试耗尽应返回 Token 过期错误，实际 %v", err)
			}
			if business, refresh := gateway.CallCount(op.api), gateway.CallCount(mtopsim.APIToken); business > 4 || refresh > 3 { // business、refresh 是业务接口与 Token 接口的请求数。
				t.Fatalf("请求数超过硬上限（业务 %d>4 或刷新 %d>3）：%v", business, refresh, gateway.CallSequence())
			}
			if total := mtopCalls(gateway); total > 8 { // total 是该操作的 MTOP 请求总数。
				t.Fatalf("MTOP 请求总数 %d 超过上限 8：%v", total, gateway.CallSequence())
			}
			if op.noSideEffect != nil {
				op.noSideEffect(t, gateway)
			}
		})
	}
}

// TestPaginationStopsAtOnceWhenRiskControlHitsMidway 验证分页类批量操作在中途遇到风控时立即终止，不再请求剩余页。
func TestPaginationStopsAtOnceWhenRiskControlHitsMidway(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// index 是创建的商品序号；创建 5 件商品、每页 2 件，共 3 页。
	for index := 0; index < 5; index++ {
		gateway.AddItem(sellerID, "视频会员月卡", "9.90", false)
	}
	gateway.ResetCalls()
	// 第 1 页正常通过，第 2 页请求返回风控，之后不得再有请求。
	gateway.InjectFault(mtopsim.APIItemList, mtopsim.Fault{Skip: 1, Ret: []string{mtopsim.RetRiskControl}, Data: map[string]any{"url": "https://www.goofish.com/punish?x5secdata=simulated"}})
	// err 是中途遇风控后的商品全量同步结果。
	_, err := client.FetchAllItems(context.Background(), cookie, 2, 5)
	if !mtop.IsRiskVerificationErr(err) {
		t.Fatalf("中途风控应终止并返回风控错误: %v", err)
	}
	if count := gateway.CallCount(mtopsim.APIItemList); count != 2 { // count 是商品列表接口实际请求次数。
		t.Fatalf("第 2 页遇风控后不得再请求，实际 %d 次：%v", count, gateway.CallSequence())
	}
}

// finalStepAPIs 是多步流程的最后一步接口：它们总是在读取/推荐步骤成功之后才被请求，
// 因此由 TestMultiStepOperationsStopWhenRiskHitsAtFinalStep 专门覆盖“最后一步遇风控”，不进入首请求矩阵。
var finalStepAPIs = map[string]bool{mtopsim.APIItemEdit: true, mtopsim.APIPublish: true}

// productionAPILiteral 匹配生产代码中出现的 MTOP 接口名字面量。
var productionAPILiteral = regexp.MustCompile(`"(mtop\.[A-Za-z0-9_.]+)"|/h5/(mtop\.[A-Za-z0-9_.]+)/`)

// TestEveryProductionMTOPAPIIsRegisteredInSimulator 守卫：生产代码引用的每个 MTOP 接口都必须在模拟器登记处理器，
// 模拟器登记的每个接口也必须被生产代码引用，避免新增接口绕过全矩阵测试或遗留失效处理器。
func TestEveryProductionMTOPAPIIsRegisteredInSimulator(t *testing.T) {
	// files 是 mtop 包下的全部源文件。
	files, globErr := filepath.Glob("../*.go")
	if globErr != nil {
		t.Fatal(globErr)
	}
	// referenced 是生产代码里出现的接口名集合。
	referenced := map[string]bool{}
	for _, file := range files { // file 是 mtop 包内的单个源文件路径。
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		// source、readErr 是该文件的内容及读取错误。
		source, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		// code 是去掉整行注释后的源码，避免注释里的示例路径被当成接口名。
		var code strings.Builder
		for _, line := range strings.Split(string(source), "\n") { // line 是源文件的单行。
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				code.WriteString(line + "\n")
			}
		}
		for _, match := range productionAPILiteral.FindAllStringSubmatch(code.String(), -1) { // match 是一次接口名字面量匹配。
			// 字面量形态取第 1 组，/h5/<接口>/<版本>/ 端点形态取第 2 组。
			if match[1] != "" {
				referenced[match[1]] = true
			} else {
				referenced[match[2]] = true
			}
		}
	}
	// registered 是模拟器已登记处理器的接口名集合。
	registered := map[string]bool{}
	for _, name := range mtopsim.New().SupportedAPIs() { // name 是单个已登记接口名。
		registered[name] = true
	}
	// missing、stale 分别是生产代码引用但未登记、已登记但无人引用的接口。
	var missing, stale []string
	for name := range referenced { // name 是生产代码引用的接口名。
		if !registered[name] {
			missing = append(missing, name)
		}
	}
	for name := range registered { // name 是模拟器登记的接口名。
		if !referenced[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 || len(stale) > 0 {
		t.Fatalf("生产代码引用但模拟器未登记: %v；模拟器登记但生产代码未引用: %v", missing, stale)
	}
	// covered 是全矩阵里出现过的接口名；每个登记接口至少要被一个矩阵操作作为首个请求接口或后续接口触达。
	covered := map[string]bool{mtopsim.APIToken: true, mtopsim.APIPolishBackup: true, mtopsim.APILoginUser: true}
	for _, op := range allOperations() { // op 是矩阵里的一个操作。
		covered[op.api] = true
	}
	for name := range registered { // name 是模拟器登记的接口名。
		if !covered[name] && !finalStepAPIs[name] {
			t.Errorf("接口 %s 没有对应的全矩阵操作", name)
		}
	}
}

// TestLoginStatusStopsAtOnceOnRiskSessionAndBoundsTokenRetry 验证登录态检查（返回状态而非错误）遇风控、Session 失效立即止损，
// 签名 Token 持续过期时请求数有上限，并且不会因此产生账号恢复或登录请求。
func TestLoginStatusStopsAtOnceOnRiskSessionAndBoundsTokenRetry(t *testing.T) {
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	t.Run("风控", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		gateway.SetRiskControl(sellerID, true)
		// result、err 是风控期间的登录态检查结果。
		result, err := client.CheckLoginStatusContext(ctx, cookie)
		if err != nil || result.Status != mtop.LoginStatusRiskRequired || gateway.CallCount() != 1 {
			t.Fatalf("风控应只请求一次并报告 risk_required: %+v err=%v calls=%v", result, err, gateway.CallSequence())
		}
	})
	t.Run("Session 失效", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		gateway.ExpireSession(sellerID)
		// result、err 是 Session 失效后的登录态检查结果。
		result, err := client.CheckLoginStatusContext(ctx, cookie)
		if err != nil || result.Status != mtop.LoginStatusSessionExpired || gateway.CallCount() != 1 {
			t.Fatalf("Session 失效应只请求一次并报告 session_expired: %+v err=%v calls=%v", result, err, gateway.CallSequence())
		}
	})
	t.Run("Token 持续过期", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		// index 是注入的第几次 Token 过期响应；数量多于客户端的最大尝试次数。
		for index := 0; index < 10; index++ {
			gateway.InjectFault(mtopsim.APILoginUser, mtopsim.Fault{Ret: []string{mtopsim.RetTokenExpired}})
		}
		// result、err 是 Token 持续过期时的检查结果，不论状态如何都不能无限请求。
		result, err := client.CheckLoginStatusContext(ctx, cookie)
		if total := gateway.CallCount(); total > 4 { // total 是该检查发出的平台请求总数。
			t.Fatalf("登录态检查请求数 %d 超过上限 4: result=%+v err=%v calls=%v", total, result, err, gateway.CallSequence())
		}
	})
}

// TestMultiStepOperationsStopWhenRiskHitsAtFinalStep 验证多步流程在最后一步（edit / publish）遇到风控时立即终止：
// 前序读取步骤已成功，最后一步只请求一次，不重试、不产生半完成的业务副作用。
func TestMultiStepOperationsStopWhenRiskHitsAtFinalStep(t *testing.T) {
	// risk 是最后一步返回的风控响应。
	risk := mtopsim.Fault{Ret: []string{mtopsim.RetRiskControl}, Data: map[string]any{"url": "https://www.goofish.com/punish?x5secdata=simulated"}}
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	t.Run("修改库存", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		// itemID 是库存为 5 的单规格商品。
		itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)
		gateway.InjectFault(mtopsim.APIItemEdit, risk)
		// err 是最后一步遇风控的结果。
		_, err := client.EditItemStock(ctx, cookie, mtop.ItemStockEditRequest{ItemID: itemID, Quantity: 8, ExpectedQuantity: 5})
		if !mtop.IsRiskVerificationErr(err) || gateway.CallCount(mtopsim.APIItemEditDetail) != 1 || gateway.CallCount(mtopsim.APIItemEdit) != 1 {
			t.Fatalf("应读 1 次、写 1 次后终止: err=%v calls=%v", err, gateway.CallSequence())
		}
		if item, _ := gateway.Item(itemID); item.Quantity != 5 || item.EditCount != 0 { // item 是风控后的平台商品。
			t.Fatalf("风控期间库存不得变化: %+v", item)
		}
	})
	t.Run("重新上架", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		// itemID 是已售罄下架的商品。
		itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 1)
		gateway.SellOut(itemID)
		gateway.InjectFault(mtopsim.APIItemEdit, risk)
		// err 是最后一步遇风控的结果。
		_, err := client.RelistItem(ctx, cookie, itemID)
		if !mtop.IsRiskVerificationErr(err) || gateway.CallCount(mtopsim.APIItemEdit) != 1 {
			t.Fatalf("应只提交一次 edit 后终止: err=%v calls=%v", err, gateway.CallSequence())
		}
		if item, _ := gateway.Item(itemID); item.OnSale || item.EditCount != 0 { // item 是风控后的平台商品。
			t.Fatalf("风控期间商品不得被重新上架: %+v", item)
		}
	})
	t.Run("发布商品", func(t *testing.T) {
		// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
		gateway, client, cookie := newSellerGateway(t)
		gateway.InjectFault(mtopsim.APIPublish, risk)
		// err 是最后一步遇风控的结果。
		_, err := client.PublishItem(ctx, cookie, mtop.PublishItemRequest{
			Title: "视频会员月卡", Description: "自动发货", PriceCents: 990, Quantity: 5, Virtual: true,
			Images: []mtop.PublishImage{{Filename: "a.png", ContentType: "image/png", Data: []byte("png-a")}},
		})
		if !mtop.IsRiskVerificationErr(err) || gateway.CallCount(mtopsim.APICategoryRecommend) != 1 || gateway.CallCount(mtopsim.APIPublish) != 1 {
			t.Fatalf("应推荐 1 次、发布 1 次后终止: err=%v calls=%v", err, gateway.CallSequence())
		}
		if len(gateway.Items(sellerID)) != 0 {
			t.Fatalf("风控期间不得创建商品: %+v", gateway.Items(sellerID))
		}
	})
}
