package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// itemEditTestCookies 是测试用的伪造 Cookie，不对应任何真实账号。
const itemEditTestCookies = "unb=1; _m_h5_tk=token_abc; cookie2=c2;"

// itemEditSingleDetail 是单规格商品的 editDetail 响应 data，字段值沿用平台的字符串形态。
const itemEditSingleDetail = `{
 "itemId":"item-1","itemStatus":"1","simpleItem":"true","quantity":"242","canBargain":"true","redirectUrl":"x",
 "itemTextDTO":{"title":"测试商品","desc":"描述"},
 "itemPriceDTO":{"priceInCent":"12000","origPriceInCent":"0"},
 "itemPostFeeDTO":{"canFreeShipping":"true","supportFreight":"false","postPriceInCent":"0"},
 "itemCatDTO":{"catId":"1","catName":"分类","channelCatId":"2"},
 "itemAddrDTO":{"prov":"省","city":"市"},
 "imageInfoDOList":[{"url":"https://img/1.jpg","major":"true"}],
 "userRightsProtocols":[{"serviceCode":"A","enable":"true"}]
}`

// itemEditMultiDetail 是多规格商品的 editDetail 响应 data，故意不带 itemProperties 以覆盖还原逻辑。
const itemEditMultiDetail = `{
 "itemId":"item-2","itemStatus":"1","quantity":"1",
 "itemTextDTO":{"title":"多规格","desc":"描述"},
 "itemPriceDTO":{"origPriceInCent":"0"},
 "itemSkuList":[
  {"skuId":"s1","priceInCent":"100","quantity":"5","propertyList":[{"propertyText":"颜色","valueText":"红"},{"propertyText":"尺码","valueText":"S"}]},
  {"priceInCent":"200","quantity":"7","propertyList":[{"propertyText":"颜色","valueText":"蓝"},{"propertyText":"尺码","valueText":"S"}]}
 ]
}`

// itemEditRecorder 是记录每次请求的本地 MTOP 测试服务。
type itemEditRecorder struct {
	// mu 保护 apis 与 edits，测试服务的处理函数可能在不同 goroutine 中运行。
	mu sync.Mutex
	// apis 按顺序记录收到的 api 参数。
	apis []string
	// edits 记录 edit 请求体解码后的载荷。
	edits []map[string]any
	// detailBody 是 editDetail 返回的完整响应正文。
	detailBody string
	// editBody 是 edit 返回的完整响应正文。
	editBody string
}

// newItemEditServer 启动本地服务，并返回指向它的客户端和记录器。
func newItemEditServer(t *testing.T, detailBody, editBody string) (*ClientImpl, *itemEditRecorder) {
	t.Helper()
	// rec 保存测试服务收到的请求证据。
	rec := &itemEditRecorder{detailBody: detailBody, editBody: editBody}
	// server 是只服务 editDetail 与 edit 两个 api 的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// api 是本次请求的 MTOP 接口名。
		api := r.URL.Query().Get("api")
		// raw 是 MTOP 表单请求体。
		raw, _ := io.ReadAll(r.Body)
		// form 是解码后的表单字段。
		form, _ := url.ParseQuery(string(raw))
		rec.mu.Lock()
		rec.apis = append(rec.apis, api)
		if api == "mtop.idle.pc.idleitem.edit" {
			// payload 是 edit 载荷。
			var payload map[string]any
			_ = json.Unmarshal([]byte(form.Get("data")), &payload)
			rec.edits = append(rec.edits, payload)
		}
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if api == "mtop.idle.pc.idleitem.editDetail" {
			_, _ = w.Write([]byte(rec.detailBody))
			return
		}
		_, _ = w.Write([]byte(rec.editBody))
	}))
	t.Cleanup(server.Close)
	return &ClientImpl{HTTPClient: server.Client(), ItemEditDetailURL: server.URL, ItemEditURL: server.URL}, rec
}

// itemEditOK 把 data 包装为成功响应。
func itemEditOK(data string) string {
	return `{"ret":["SUCCESS::调用成功"],"data":` + data + `}`
}

// TestFetchItemEditDetailParsesSingleSpec 验证 editDetail 的请求元数据与单规格解析。
func TestFetchItemEditDetailParsesSingleSpec(t *testing.T) {
	// client、rec 是指向本地服务的客户端和请求记录器。
	client, rec := newItemEditServer(t, itemEditOK(itemEditSingleDetail), "")
	// detail、err 是解析结果与错误。
	detail, err := client.FetchItemEditDetail(context.Background(), itemEditTestCookies, " item-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ItemID != "item-1" || detail.MultiSpec || detail.Quantity != 242 || detail.Status != "1" || len(detail.SKUs) != 0 {
		t.Fatalf("单规格解析错误: %+v", detail)
	}
	if len(rec.apis) != 1 || rec.apis[0] != "mtop.idle.pc.idleitem.editDetail" {
		t.Fatalf("应只请求一次 editDetail: %v", rec.apis)
	}
}

// TestFetchItemEditDetailParsesMultiSpec 验证多规格 Key（skuId 优先，缺失时用规格拼接）、标签和库存。
func TestFetchItemEditDetailParsesMultiSpec(t *testing.T) {
	// client 是指向本地服务的客户端。
	client, _ := newItemEditServer(t, itemEditOK(itemEditMultiDetail), "")
	// detail、err 是解析结果与错误。
	detail, err := client.FetchItemEditDetail(context.Background(), itemEditTestCookies, "item-2")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.MultiSpec || len(detail.SKUs) != 2 || detail.Quantity != 12 {
		t.Fatalf("多规格解析错误: %+v", detail)
	}
	if detail.SKUs[0].Key != "sku:s1" || detail.SKUs[0].Label != "红 / S" || detail.SKUs[0].PriceCents != 100 || detail.SKUs[0].Quantity != 5 {
		t.Fatalf("第一行错误: %+v", detail.SKUs[0])
	}
	if detail.SKUs[1].Key != "spec:颜色=蓝|尺码=S" || detail.SKUs[1].Quantity != 7 {
		t.Fatalf("第二行错误: %+v", detail.SKUs[1])
	}
}

// TestFetchItemEditDetailRejectsBadInputAndResponses 验证空 ID 不发请求，以及风控、缺 SUCCESS、结构不可用的分类。
func TestFetchItemEditDetailRejectsBadInputAndResponses(t *testing.T) {
	// client、rec 用于空 ID 场景。
	client, rec := newItemEditServer(t, itemEditOK(itemEditSingleDetail), "")
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := client.FetchItemEditDetail(context.Background(), itemEditTestCookies, "  "); !errors.Is(err, ErrItemStockInvalid) || len(rec.apis) != 0 {
		t.Fatalf("空 ID 应直接拒绝且不发请求: err=%v apis=%v", err, rec.apis)
	}
	// cases 是各类不可用的响应。
	cases := []struct {
		// name 是子测试名称。
		name string
		// body 是 editDetail 响应正文。
		body string
		// check 判断返回的错误是否符合预期。
		check func(error) bool
	}{
		{"风控", `{"ret":["FAIL_SYS_USER_VALIDATE::RGV587_ERROR"],"data":{}}`, IsRiskVerificationErr},
		{"会话失效", `{"ret":["FAIL_SYS_SESSION_EXPIRED::Session过期"],"data":{}}`, IsSessionExpiredErr},
		{"空data", itemEditOK(`{}`), func(err error) bool { return errors.Is(err, ErrItemEditDetailUnusable) }},
		{"缺itemId", itemEditOK(`{"quantity":"1"}`), func(err error) bool { return errors.Is(err, ErrItemEditDetailUnusable) }},
		{"单规格缺quantity", itemEditOK(`{"itemId":"x"}`), func(err error) bool { return errors.Is(err, ErrItemEditDetailUnusable) }},
		{"规格行不是对象", itemEditOK(`{"itemId":"x","itemSkuList":["bad"]}`), func(err error) bool { return errors.Is(err, ErrItemEditDetailUnusable) }},
		{"规格行重复", itemEditOK(`{"itemId":"x","itemSkuList":[{"skuId":"a","quantity":"1"},{"skuId":"a","quantity":"2"}]}`), func(err error) bool { return errors.Is(err, ErrItemEditDetailUnusable) }},
		{"规格行无标识", itemEditOK(`{"itemId":"x","itemSkuList":[{"quantity":"1"}]}`), func(err error) bool { return errors.Is(err, ErrItemEditDetailUnusable) }},
	}
	// tc 是当前子测试用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// failing 是返回该响应的客户端。
			failing, _ := newItemEditServer(t, tc.body, "")
			// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
			if _, err := failing.FetchItemEditDetail(context.Background(), itemEditTestCookies, "x"); err == nil || !tc.check(err) {
				t.Fatalf("错误分类不符: %v", err)
			}
		})
	}
}

// TestEditItemStockSingleSpecChangesOnlyQuantity 验证单规格：两次请求、只改 quantity、其余字段原样回填且布尔值已标准化。
func TestEditItemStockSingleSpecChangesOnlyQuantity(t *testing.T) {
	// client、rec 是指向本地服务的客户端和请求记录器。
	client, rec := newItemEditServer(t, itemEditOK(itemEditSingleDetail), itemEditOK(`{"success":true}`))
	// result、err 是修改结果。
	result, err := client.EditItemStock(context.Background(), itemEditTestCookies, ItemStockEditRequest{ItemID: "item-1", Quantity: 9, ExpectedQuantity: 242})
	if err != nil || result == nil || result.ItemID != "item-1" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Join(rec.apis, ",") != "mtop.idle.pc.idleitem.editDetail,mtop.idle.pc.idleitem.edit" {
		t.Fatalf("应固定为先读后写两次请求: %v", rec.apis)
	}
	// payload 是 edit 载荷。
	payload := rec.edits[0]
	if payload["quantity"] != "9" || payload["itemId"] != "item-1" || payload["sourceId"] != "item-1" || payload["publishScene"] != "pcMainPublish" || payload["bizcode"] != "pcMainPublish" {
		t.Fatalf("关键字段错误: %v", payload)
	}
	// leaked 表示是否有不应出现的字段泄漏到载荷中。
	if _, leaked := payload["redirectUrl"]; leaked {
		t.Fatal("白名单之外的字段不应提交")
	}
	// title 是载荷中的标题字段。
	if title := payload["itemTextDTO"].(map[string]any)["title"]; title != "测试商品" {
		t.Fatalf("标题被改动: %v", title)
	}
	if payload["itemPriceDTO"].(map[string]any)["priceInCent"] != "12000" || len(payload["imageInfoDOList"].([]any)) != 1 {
		t.Fatalf("价格或图片未原样回填: %v", payload)
	}
	// fee 是标准化后的运费对象。
	fee := payload["itemPostFeeDTO"].(map[string]any)
	if fee["canFreeShipping"] != true || fee["supportFreight"] != false {
		t.Fatalf("运费布尔值未标准化: %v", fee)
	}
	// enabled 是权益协议的开关值。
	if enabled := payload["userRightsProtocols"].([]any)[0].(map[string]any)["enable"]; enabled != true {
		t.Fatalf("服务承诺 enable 未标准化: %v", enabled)
	}
}

// TestEditItemStockMultiSpecChangesMatchedRows 验证多规格：只改匹配行、顶层 quantity 保持、缺失的 itemProperties 被还原。
func TestEditItemStockMultiSpecChangesMatchedRows(t *testing.T) {
	// client、rec 是指向本地服务的客户端和请求记录器。
	client, rec := newItemEditServer(t, itemEditOK(itemEditMultiDetail), itemEditOK(`{}`))
	// err 是修改错误。
	_, err := client.EditItemStock(context.Background(), itemEditTestCookies, ItemStockEditRequest{ItemID: "item-2", SKUs: []ItemStockSKUEdit{
		{Key: "sku:s1", Quantity: 0, ExpectedQuantity: 5},
		{Key: "spec:颜色=蓝|尺码=S", Quantity: 30, ExpectedQuantity: 7},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// payload 是 edit 载荷。
	payload := rec.edits[0]
	// rows 是载荷里的规格行。
	rows := payload["itemSkuList"].([]any)
	if rows[0].(map[string]any)["quantity"] != "0" || rows[1].(map[string]any)["quantity"] != "30" {
		t.Fatalf("规格库存未按行写入: %v", rows)
	}
	if rows[0].(map[string]any)["priceInCent"] != "100" || rows[1].(map[string]any)["skuId"] != nil {
		t.Fatalf("规格价格或标识被改动: %v", rows)
	}
	if payload["quantity"] != "1" {
		t.Fatalf("多规格顶层 quantity 应保持原值: %v", payload["quantity"])
	}
	// properties 是还原后的规格维度。
	properties := payload["itemProperties"].([]any)
	if len(properties) != 2 || properties[0].(map[string]any)["propertyName"] != "颜色" || len(properties[0].(map[string]any)["propertyValues"].([]any)) != 2 {
		t.Fatalf("itemProperties 还原错误: %v", properties)
	}
}

// TestEditItemStockMultiSpecKeepsExistingProperties 验证详情自带 itemProperties 与整数库存类型时原样保留。
func TestEditItemStockMultiSpecKeepsExistingProperties(t *testing.T) {
	// detail 是自带 itemProperties 且库存为数字的多规格详情。
	detail := `{"itemId":"item-3","itemProperties":[{"propertyName":"颜色","propertyValues":[{"propertyValue":"红"}]}],
	"itemSkuList":[{"skuId":"a","quantity":3,"propertyList":[{"propertyText":"颜色","valueText":"红"}]}]}`
	// client、rec 是指向本地服务的客户端和请求记录器。
	client, rec := newItemEditServer(t, itemEditOK(detail), itemEditOK(`{}`))
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := client.EditItemStock(context.Background(), itemEditTestCookies, ItemStockEditRequest{ItemID: "item-3", SKUs: []ItemStockSKUEdit{{Key: "sku:a", Quantity: 8, ExpectedQuantity: 3}}}); err != nil {
		t.Fatal(err)
	}
	// payload 是 edit 载荷。
	payload := rec.edits[0]
	if payload["itemSkuList"].([]any)[0].(map[string]any)["quantity"] != float64(8) {
		t.Fatalf("数字类型库存应保持数字: %v", payload["itemSkuList"])
	}
	if len(payload["itemProperties"].([]any)) != 1 || payload["quantity"] != "1" {
		t.Fatalf("已有 itemProperties 应保留: %v", payload)
	}
}

// TestEditItemStockRejectsStaleExpectedQuantity 验证库存已变化时只读不写。
func TestEditItemStockRejectsStaleExpectedQuantity(t *testing.T) {
	// client、rec 用于单规格库存已变化的场景。
	client, rec := newItemEditServer(t, itemEditOK(itemEditSingleDetail), itemEditOK(`{}`))
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := client.EditItemStock(context.Background(), itemEditTestCookies, ItemStockEditRequest{ItemID: "item-1", Quantity: 9, ExpectedQuantity: 100}); !errors.Is(err, ErrItemStockChanged) {
		t.Fatalf("err=%v", err)
	}
	if len(rec.edits) != 0 || len(rec.apis) != 1 {
		t.Fatalf("库存变化时不得提交 edit: %v", rec.apis)
	}
	// multiClient、multiRec 用于多规格某行库存已变化的场景。
	multiClient, multiRec := newItemEditServer(t, itemEditOK(itemEditMultiDetail), itemEditOK(`{}`))
	// err 是多规格修改错误。
	_, err := multiClient.EditItemStock(context.Background(), itemEditTestCookies, ItemStockEditRequest{ItemID: "item-2", SKUs: []ItemStockSKUEdit{
		{Key: "sku:s1", Quantity: 1, ExpectedQuantity: 5},
		{Key: "spec:颜色=蓝|尺码=S", Quantity: 1, ExpectedQuantity: 99},
	}})
	if !errors.Is(err, ErrItemStockChanged) || len(multiRec.edits) != 0 {
		t.Fatalf("err=%v edits=%d", err, len(multiRec.edits))
	}
}

// TestEditItemStockValidationNeverCallsPlatform 验证请求自身不合法时不发任何平台请求。
func TestEditItemStockValidationNeverCallsPlatform(t *testing.T) {
	// client、rec 用于统计平台请求数。
	client, rec := newItemEditServer(t, itemEditOK(itemEditSingleDetail), itemEditOK(`{}`))
	// cases 是各类本地即可判定的非法请求。
	cases := map[string]ItemStockEditRequest{
		"空商品ID":    {ItemID: " ", Quantity: 1},
		"单规格库存为0":  {ItemID: "a", Quantity: 0},
		"单规格超上限":   {ItemID: "a", Quantity: itemStockMax + 1},
		"单规格缺原库存":  {ItemID: "a", Quantity: 1, ExpectedQuantity: -1},
		"规格行无标识":   {ItemID: "a", SKUs: []ItemStockSKUEdit{{Key: " ", Quantity: 1}}},
		"规格行重复":    {ItemID: "a", SKUs: []ItemStockSKUEdit{{Key: "k", Quantity: 1}, {Key: "k", Quantity: 1}}},
		"规格库存为负":   {ItemID: "a", SKUs: []ItemStockSKUEdit{{Key: "k", Quantity: -1}}},
		"规格超上限":    {ItemID: "a", SKUs: []ItemStockSKUEdit{{Key: "k", Quantity: itemStockMax + 1}}},
		"规格缺原库存":   {ItemID: "a", SKUs: []ItemStockSKUEdit{{Key: "k", Quantity: 1, ExpectedQuantity: -1}}},
		"全部规格库存为0": {ItemID: "a", SKUs: []ItemStockSKUEdit{{Key: "k", Quantity: 0}, {Key: "j", Quantity: 0}}},
	}
	// name、req 是当前非法请求及说明。
	for name, req := range cases {
		// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
		if _, err := client.EditItemStock(context.Background(), itemEditTestCookies, req); !errors.Is(err, ErrItemStockInvalid) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	if len(rec.apis) != 0 {
		t.Fatalf("非法请求不应触达平台: %v", rec.apis)
	}
}

// TestEditItemStockRejectsMismatchedSpecShape 验证单/多规格不一致、规格行数量或标识不一致时拒绝提交。
func TestEditItemStockRejectsMismatchedSpecShape(t *testing.T) {
	// cases 是商品详情与请求形态不一致的场景。
	cases := []struct {
		// name 是子测试名称。
		name string
		// detail 是 editDetail 的 data。
		detail string
		// req 是库存修改请求。
		req ItemStockEditRequest
	}{
		{"单规格商品收到规格行", itemEditSingleDetail, ItemStockEditRequest{ItemID: "item-1", SKUs: []ItemStockSKUEdit{{Key: "k", Quantity: 1, ExpectedQuantity: 1}}}},
		{"多规格商品收到单库存", itemEditMultiDetail, ItemStockEditRequest{ItemID: "item-2", Quantity: 1, ExpectedQuantity: 12}},
		{"规格行缺失", itemEditMultiDetail, ItemStockEditRequest{ItemID: "item-2", SKUs: []ItemStockSKUEdit{{Key: "sku:s1", Quantity: 1, ExpectedQuantity: 5}}}},
		{"未知规格行", itemEditMultiDetail, ItemStockEditRequest{ItemID: "item-2", SKUs: []ItemStockSKUEdit{{Key: "sku:s1", Quantity: 1, ExpectedQuantity: 5}, {Key: "sku:none", Quantity: 1, ExpectedQuantity: 7}}}},
		{"详情商品ID不一致", itemEditSingleDetail, ItemStockEditRequest{ItemID: "other", Quantity: 1, ExpectedQuantity: 242}},
	}
	// tc 是当前子测试用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// client、rec 是指向本地服务的客户端和请求记录器。
			client, rec := newItemEditServer(t, itemEditOK(tc.detail), itemEditOK(`{}`))
			// err 是修改错误。
			_, err := client.EditItemStock(context.Background(), itemEditTestCookies, tc.req)
			if err == nil || len(rec.edits) != 0 {
				t.Fatalf("应拒绝且不提交: err=%v edits=%d", err, len(rec.edits))
			}
		})
	}
}

// TestEditItemStockClassifiesPlatformFailures 验证读阶段风控不会继续提交，写阶段失败按统一分类返回且不重试。
func TestEditItemStockClassifiesPlatformFailures(t *testing.T) {
	// risk 是风控响应。
	risk := `{"ret":["FAIL_SYS_USER_VALIDATE::RGV587_ERROR"],"data":{}}`
	// client、rec 用于读阶段风控场景。
	client, rec := newItemEditServer(t, risk, itemEditOK(`{}`))
	// req 是合法的单规格修改请求。
	req := ItemStockEditRequest{ItemID: "item-1", Quantity: 9, ExpectedQuantity: 242}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := client.EditItemStock(context.Background(), itemEditTestCookies, req); !IsRiskVerificationErr(err) || len(rec.apis) != 1 {
		t.Fatalf("读阶段风控应立即终止: err=%v apis=%v", err, rec.apis)
	}
	// writeCases 是写阶段的各类失败响应。
	writeCases := map[string]struct {
		// body 是 edit 响应正文。
		body string
		// check 判断错误是否符合预期。
		check func(error) bool
	}{
		"写阶段风控":       {risk, IsRiskVerificationErr},
		"ret缺SUCCESS": {`{"ret":["FAIL_BIZ_ITEM::商品不可编辑"],"data":{}}`, func(err error) bool { return err != nil && !IsRiskVerificationErr(err) }},
		"data失败标志":    {itemEditOK(`{"success":false,"msg":"类目不支持"}`), func(err error) bool { return err != nil && strings.Contains(err.Error(), "类目不支持") }},
	}
	// name、tc 是当前失败场景。
	for name, tc := range writeCases {
		// writeClient 是执行写入的测试客户端；writeRec 记录写入客户端收到的请求。
		writeClient, writeRec := newItemEditServer(t, itemEditOK(itemEditSingleDetail), tc.body)
		// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
		if _, err := writeClient.EditItemStock(context.Background(), itemEditTestCookies, req); !tc.check(err) {
			t.Fatalf("%s: err=%v", name, err)
		}
		if len(writeRec.edits) != 1 {
			t.Fatalf("%s: 失败后不得重试 edit: %d", name, len(writeRec.edits))
		}
	}
}

// TestItemEditUnsupportedCategory 验证类目不支持网页端编辑时返回专用哨兵错误，且 edit 阶段不重试。
func TestItemEditUnsupportedCategory(t *testing.T) {
	// unsupported 是真机观察到的“类目不支持网页编辑”响应。
	unsupported := `{"ret":["FAIL_BIZ_PC_NOT_SUPPORT_PUBLISH_OR_EDIT::该商品暂不支持网页发布，请到闲鱼APP发布"],"data":{}}`
	// readClient 覆盖读取阶段被拒绝的情况。
	readClient, _ := newItemEditServer(t, unsupported, "")
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := readClient.FetchItemEditDetail(context.Background(), itemEditTestCookies, "x"); !errors.Is(err, ErrItemEditUnsupported) {
		t.Fatalf("读取阶段 err=%v", err)
	}
	// writeClient、rec 覆盖提交阶段被拒绝的情况。
	writeClient, rec := newItemEditServer(t, itemEditOK(itemEditSingleDetail), unsupported)
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := writeClient.EditItemStock(context.Background(), itemEditTestCookies, ItemStockEditRequest{ItemID: "item-1", Quantity: 9, ExpectedQuantity: 242}); !errors.Is(err, ErrItemEditUnsupported) || len(rec.edits) != 1 {
		t.Fatalf("提交阶段 err=%v edits=%d", err, len(rec.edits))
	}
}

// TestItemEditBusinessFailure 验证“ret 成功但业务失败”的判定口径。
func TestItemEditBusinessFailure(t *testing.T) {
	// cases 是 data 到期望失败文本（空表示成功）的映射。
	cases := []struct {
		// name 是用例名称。
		name string
		// data 是 edit 响应 data。
		data map[string]any
		// want 是期望的失败文本；空串表示判定成功。
		want string
	}{
		{"空data视为成功", nil, ""},
		{"success为true", map[string]any{"success": true}, ""},
		{"success为false带说明", map[string]any{"success": false, "message": "被拒"}, "被拒"},
		{"success字符串false", map[string]any{"isSuccess": "FALSE"}, "平台返回编辑失败"},
		{"非成功code", map[string]any{"code": "E1", "tips": "超限"}, "超限"},
		{"errorCode兜底", map[string]any{"errorCode": "E2"}, "平台返回编辑失败（code=E2）"},
		{"成功code", map[string]any{"code": "200"}, ""},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		// got 是实际得到的结果，用于与期望比较。
		if got := itemEditBusinessFailure(tc.data); got != tc.want {
			t.Fatalf("%s: got=%q want=%q", tc.name, got, tc.want)
		}
	}
}

// TestItemEditHelpers 验证类型保持、布尔标准化、深拷贝失败与属性还原的边界。
func TestItemEditHelpers(t *testing.T) {
	if typedLike("5", 7) != "7" || typedLike(nil, 7) != "7" || typedLike(float64(5), 7) != 7 {
		t.Fatal("typedLike 应按原值类型写入")
	}
	if boolLike(true) != true || boolLike(" True ") != true || boolLike("no") != false || boolLike(3) != 3 {
		t.Fatal("boolLike 规则错误")
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := cloneJSONValue(make(chan int)); err == nil {
		t.Fatal("不可序列化的值应返回错误")
	}
	// got 是实际得到的结果，用于与期望比较。
	if got := rebuildItemProperties([]any{map[string]any{"propertyList": []any{map[string]any{"propertyText": "", "valueText": "x"}}}}); len(got) != 0 {
		t.Fatalf("缺规格名的项应被忽略: %v", got)
	}
	// noDetail 是缺少规格项的规格行，用于覆盖 Key 回退为空。
	noDetail := parseItemStockSKU(map[string]any{"quantity": "1"})
	if noDetail.Key != "" || noDetail.Label != "" {
		t.Fatalf("无标识规格行应返回空 Key: %+v", noDetail)
	}
	// fallback 的 actualValueText 回退。
	if got := parseItemStockSKU(map[string]any{"propertyList": []any{map[string]any{"propertyText": "色", "actualValueText": "绿"}}}); got.Key != "spec:色=绿" {
		t.Fatalf("应回退到 actualValueText: %+v", got)
	}
	if fallbackText("a", "b") != "a" || fallbackText("", "b") != "b" {
		t.Fatal("fallbackText 规则错误")
	}
}
