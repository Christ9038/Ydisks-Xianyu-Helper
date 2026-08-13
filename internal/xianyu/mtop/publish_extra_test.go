package mtop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// publish.go 用常量 URL（UploadMediaAPI / RecommendItemAPI / DefaultLocationAPI / PublishItemAPI），
// 通过 dispatchTransport 按 api query 参数分发到不同本地 handler，覆盖各 mtop 调用路径。

type dispatchTransport struct {
	handlers map[string]http.HandlerFunc
}

func (d *dispatchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	// 选 handler：upload 走 stream-upload 域名（无 api query），其它用 api query 区分
	api := req.URL.Query().Get("api")
	h, ok := d.handlers[api]
	if !ok {
		// upload 的请求路径不含 api=mtop.* ；用 "_upload" key
		if strings.Contains(req.URL.Host, "stream-upload") {
			h, ok = d.handlers["_upload"]
		}
	}
	if !ok {
		h = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no handler for api="+api, http.StatusNotFound)
		}
	}
	h(rec, req)
	resp := &http.Response{
		StatusCode: rec.Code,
		Header:     rec.Header().Clone(),
		Body:       rec.Result().Body,
		Request:    req,
	}
	return resp, nil
}

// TestPublishItemValidationFailures: 各参数校验分支。
func TestPublishItemValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		req  PublishItemRequest
		want string
	}{
		{"empty title", PublishItemRequest{Title: "  ", PriceCents: 100, Quantity: 1, Images: []PublishImage{{}}}, "商品标题不能为空"},
		{"zero price", PublishItemRequest{Title: "T", PriceCents: 0, Quantity: 1, Images: []PublishImage{{}}}, "商品价格必须大于 0"},
		{"zero quantity", PublishItemRequest{Title: "T", PriceCents: 100, Quantity: 0, Images: []PublishImage{{}}}, "库存数量必须大于 0"},
		{"no images", PublishItemRequest{Title: "T", PriceCents: 100, Quantity: 1}, "至少上传 1 张商品图片"},
		{"too many images", PublishItemRequest{Title: "T", PriceCents: 100, Quantity: 1, Images: make([]PublishImage, 10)}, "商品图片最多 9 张"},
		{"incomplete preferred category", PublishItemRequest{Title: "T", PriceCents: 100, Quantity: 1, PreferredCategory: &PublishCategory{CatID: "5001"}, Images: []PublishImage{{}}}, "默认类目必须同时包含"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &ClientImpl{}
			_, err := client.PublishItem(context.Background(), consignCookies, c.req)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err=%v want %q", err, c.want)
			}
		})
	}
}

// TestPublishItemDescriptionDefaultsToTitle: description 为空时回退为 title。
func TestPublishItemDescriptionDefaultsToTitle(t *testing.T) {
	png1 := tinyPNG(t)
	var gotDesc string
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			body := readBody(r)
			var decoded struct {
				Data struct {
					Desc string `json:"description"`
				} `json:"data"`
			}
			_ = json.Unmarshal([]byte("{"+body+"}"), &decoded) // data= 已 url-encoded
			// 直接解析 url-encoded data
			if v, ok := parseDataURL(body); ok {
				gotDesc = v["description"].(string)
			}
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"c1","catName":"类目"}}}`)
		},
		"mtop.taobao.idle.local.poi.get": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":[{"area":"X","city":"Y","divisionId":"1","longitude":118.7,"latitude":31.9,"poiId":"p1","poi":"P","prov":"Z"}]}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"new-item-1"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	req := PublishItemRequest{
		Title:        "测试商品",
		PriceCents:   1000,
		Quantity:     1,
		PostageMode:  "fixed",
		PostageCents: 500,
		Location:     &PublishLocation{Area: "X", City: "Y", DivisionID: "1", Longitude: 118.7, Latitude: 31.9, POIID: "p1", POIName: "P", Province: "Z"},
		Images:       []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	}
	res, err := client.PublishItem(context.Background(), consignCookies, req)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.ItemID != "new-item-1" {
		t.Fatalf("ItemID=%q", res.ItemID)
	}
	if res.ItemURL != "https://www.goofish.com/item?id=new-item-1" {
		t.Fatalf("ItemURL=%q", res.ItemURL)
	}
	if gotDesc != "测试商品" {
		t.Fatalf("desc=%q want 测试商品 (title 回退)", gotDesc)
	}
	if res.ImageURL != "https://cdn/a.jpg" {
		t.Fatalf("ImageURL=%q", res.ImageURL)
	}
}

// TestPublishItemUploadImageFailure: 图片上传失败导致 PublishItem 失败。
func TestPublishItemUploadImageFailure(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{}}`) // 缺 url
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	req := PublishItemRequest{
		Title:      "T",
		PriceCents: 1000,
		Quantity:   1,
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	}
	_, err := client.PublishItem(context.Background(), consignCookies, req)
	if err == nil || !strings.Contains(err.Error(), "图片上传响应缺少 url") {
		t.Fatalf("err=%v", err)
	}
}

// TestPublishItemRecommendCategoryFailure: 类目推荐失败。
func TestPublishItemRecommendCategoryFailure(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXPIRED::令牌过期"]}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	req := PublishItemRequest{
		Title:      "T",
		PriceCents: 1000,
		Quantity:   1,
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	}
	_, err := client.PublishItem(context.Background(), consignCookies, req)
	if err == nil {
		t.Fatalf("expected err")
	}
	var pe *PublishError
	if !errors.As(err, &pe) || pe.Code != PublishErrorTokenExpired {
		t.Fatalf("err=%v want PublishErrorTokenExpired", err)
	}
}

// TestPublishItemRecommendCategoryMissingData: ret 成功但 data 缺 categoryPredictResult。
func TestPublishItemRecommendCategoryMissingDataUsesElectronicMaterials(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"fallback-item"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	req := PublishItemRequest{
		Title:      "T",
		PriceCents: 1000,
		Quantity:   1,
		Virtual:    true,
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	}
	result, err := client.PublishItem(context.Background(), consignCookies, req)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result.CategoryID != "50023914" || result.CategoryName != "电子资料" {
		t.Fatalf("result=%+v", result)
	}
}

// TestPublishItemLocationFailure: 实物发布没有选择发货地。
func TestPublishItemLocationFailure(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"c1","catName":"类目"}}}`)
		},
		"mtop.taobao.idle.local.poi.get": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":[]}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	req := PublishItemRequest{
		Title:      "T",
		PriceCents: 1000,
		Quantity:   1,
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	}
	_, err := client.PublishItem(context.Background(), consignCookies, req)
	if err == nil || !strings.Contains(err.Error(), "必须选择发货地") {
		t.Fatalf("err=%v", err)
	}
}

// TestPublishVirtualItemSkipsLocation 虚拟商品不查询默认地址，也不发送实物地址字段。
func TestPublishVirtualItemSkipsLocation(t *testing.T) {
	png1 := tinyPNG(t)
	var publishedData map[string]any
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"c1","catName":"类目"}}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, r *http.Request) {
			publishedData, _ = parseDataURL(readBody(r))
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"virtual-item-1"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	res, err := client.PublishItem(context.Background(), consignCookies, PublishItemRequest{
		Title:      "虚拟商品",
		PriceCents: 1000,
		Quantity:   1,
		Virtual:    true,
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.ItemID != "virtual-item-1" {
		t.Fatalf("ItemID=%q", res.ItemID)
	}
	if publishedData == nil {
		t.Fatal("未解析到发布请求 data")
	}
	if _, exists := publishedData["itemAddrDTO"]; exists {
		t.Fatalf("虚拟商品不应发送 itemAddrDTO: %+v", publishedData["itemAddrDTO"])
	}
}

func TestPublishVirtualItemUsesPreferredCategoryWithoutRecommendation(t *testing.T) {
	png1 := tinyPNG(t)
	var publishedData map[string]any
	recommendCalled := false
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			recommendCalled = true
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, r *http.Request) {
			publishedData, _ = parseDataURL(readBody(r))
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"fallback-item-1"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	res, err := client.PublishItem(context.Background(), consignCookies, PublishItemRequest{
		Title:             "虚拟商品",
		PriceCents:        1000,
		Quantity:          1,
		Virtual:           true,
		PreferredCategory: &PublishCategory{CatID: "5001", CatName: "虚拟服务", ChannelCatID: "6001", TBCatID: "7001"},
		Images:            []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if recommendCalled {
		t.Fatal("配置默认类目后不应调用自动推荐接口")
	}
	if res.CategoryID != "5001" || res.CategoryName != "虚拟服务" {
		t.Fatalf("category result=%+v", res)
	}
	category, _ := publishedData["itemCatDTO"].(map[string]any)
	if category["catId"] != "5001" || category["catName"] != "虚拟服务" || category["channelCatId"] != "6001" || category["tbCatId"] != "7001" {
		t.Fatalf("published category=%+v", category)
	}
	if _, exists := publishedData["itemAddrDTO"]; exists {
		t.Fatalf("虚拟商品不应发送 itemAddrDTO: %+v", publishedData["itemAddrDTO"])
	}
}

func TestPublishVirtualItemUsesElectronicMaterialsWhenRecommendationIsEmpty(t *testing.T) {
	png1 := tinyPNG(t)
	var publishedData map[string]any
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, r *http.Request) {
			publishedData, _ = parseDataURL(readBody(r))
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"electronic-item-1"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	res, err := client.PublishItem(context.Background(), consignCookies, PublishItemRequest{
		Title:      "无法识别的虚拟商品",
		PriceCents: 1000,
		Quantity:   1,
		Virtual:    true,
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.CategoryID != "50023914" || res.CategoryName != "电子资料" {
		t.Fatalf("category result=%+v", res)
	}
	category, _ := publishedData["itemCatDTO"].(map[string]any)
	if category["channelCatId"] != "202036301" {
		t.Fatalf("published category=%+v", category)
	}
}

// TestPublishItemFinalPublishFailure: 发布接口返回 token 过期错误。
func TestPublishItemFinalPublishFailure(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"c1","catName":"类目"}}}`)
		},
		"mtop.taobao.idle.local.poi.get": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":[{"area":"X","city":"Y","divisionId":"1","longitude":118.7,"latitude":31.9,"poiId":"p1","poi":"P","prov":"Z"}]}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXPIRED::令牌过期"]}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	req := PublishItemRequest{
		Title:        "T",
		PriceCents:   1000,
		Quantity:     1,
		PostageMode:  "fixed",
		PostageCents: 500,
		Location:     &PublishLocation{Area: "X", City: "Y", DivisionID: "1", Longitude: 118.7, Latitude: 31.9, POIID: "p1", POIName: "P", Province: "Z"},
		Images:       []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	}
	_, err := client.PublishItem(context.Background(), consignCookies, req)
	if err == nil {
		t.Fatalf("expected err")
	}
	var pe *PublishError
	if !errors.As(err, &pe) || pe.Code != PublishErrorTokenExpired {
		t.Fatalf("err=%v want PublishErrorTokenExpired", err)
	}
}

// TestPublishItemStockPermissionError: 库存权限错误分类。
func TestPublishItemStockPermissionError(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/a.jpg","pix":"800x600"}}`)
		},
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"c1","catName":"类目"}}}`)
		},
		"mtop.taobao.idle.local.poi.get": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":[{"area":"X","city":"Y","divisionId":"1","longitude":118.7,"latitude":31.9,"poiId":"p1","poi":"P","prov":"Z"}]}}`)
		},
		"mtop.idle.pc.idleitem.publish": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["账号没有库存发布权限"]}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	req := PublishItemRequest{
		Title:      "T",
		PriceCents: 1000,
		Quantity:   1,
		Location:   &PublishLocation{Area: "X", City: "Y", DivisionID: "1", Longitude: 118.7, Latitude: 31.9, POIID: "p1", POIName: "P", Province: "Z"},
		Images:     []PublishImage{{Filename: "a.png", ContentType: "image/png", Data: png1}},
	}
	_, err := client.PublishItem(context.Background(), consignCookies, req)
	if err == nil {
		t.Fatalf("expected err")
	}
	var pe *PublishError
	if !errors.As(err, &pe) || pe.Code != PublishErrorStockPermissionMissing {
		t.Fatalf("err=%v want PublishErrorStockPermissionMissing", err)
	}
}

// ---- uploadPublishImage 直接测试 ----

func TestUploadPublishImageSuccess(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			// 验证 multipart 包含文件
			ct := r.Header.Get("content-type")
			if !strings.HasPrefix(ct, "multipart/form-data") {
				t.Errorf("content-type=%q", ct)
			}
			fmt.Fprint(w, `{"object":{"url":"https://cdn/x.jpg","pix":"640x480"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	img := PublishImage{Filename: "x.png", ContentType: "image/png", Data: png1}
	res, updated, err := client.uploadPublishImage(context.Background(), consignCookies, img)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.URL != "https://cdn/x.jpg" || res.Width != 640 || res.Height != 480 {
		t.Fatalf("res=%+v", res)
	}
	if updated != consignCookies {
		t.Fatalf("updated=%q want orig", updated)
	}
}

func TestUploadPublishImageHTTPError(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	_, _, err := client.uploadPublishImage(context.Background(), consignCookies, PublishImage{Data: png1})
	if err == nil || !strings.Contains(err.Error(), "上传商品图片失败: http=500") {
		t.Fatalf("err=%v", err)
	}
}

func TestUploadPublishImageParseFailure(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `not-json{`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	_, _, err := client.uploadPublishImage(context.Background(), consignCookies, PublishImage{Data: png1})
	if err == nil || !strings.Contains(err.Error(), "解析图片上传响应失败") {
		t.Fatalf("err=%v", err)
	}
}

func TestUploadPublishImageMissingURL(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"pix":"800x600"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	_, _, err := client.uploadPublishImage(context.Background(), consignCookies, PublishImage{Data: png1})
	if err == nil || !strings.Contains(err.Error(), "图片上传响应缺少 url") {
		t.Fatalf("err=%v", err)
	}
}

func TestUploadPublishImageFallbackURL(t *testing.T) {
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			// object.url 为空，回退到 decoded["url"]
			fmt.Fprint(w, `{"object":{},"url":"https://cdn/fallback.jpg"}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	res, _, err := client.uploadPublishImage(context.Background(), consignCookies, PublishImage{Data: png1})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.URL != "https://cdn/fallback.jpg" {
		t.Fatalf("URL=%q", res.URL)
	}
}

func TestUploadPublishImageDecodeConfigFallback(t *testing.T) {
	// pix 字段空，但图片是合法 PNG，走 image.DecodeConfig 回退。
	png1 := tinyPNG(t)
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"_upload": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"object":{"url":"https://cdn/y.jpg"}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	res, _, err := client.uploadPublishImage(context.Background(), consignCookies, PublishImage{Filename: "y.png", ContentType: "image/png", Data: png1})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	// tinyPNG 是 1x1
	if res.Width != 1 || res.Height != 1 {
		t.Fatalf("Width=%d Height=%d want 1x1", res.Width, res.Height)
	}
}

// ---- recommendPublishCategory 直接测试 ----

func TestRecommendPublishCategorySuccess(t *testing.T) {
	png1 := tinyPNG(t)
	imgs := []uploadedImage{{URL: "https://cdn/a.jpg", Width: 800, Height: 600}}
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"c1","catName":"类目","channelCatId":"cc1","tbCatId":"tc1"}}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	cat, _, err := client.recommendPublishCategory(context.Background(), consignCookies, "T", "D", imgs)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	sub := cat["categoryPredictResult"].(map[string]any)
	if sub["catId"] != "c1" {
		t.Fatalf("cat=%+v", cat)
	}
	_ = png1
}

func TestRecommendPublishCategoryUsesSelectedCategoryCard(t *testing.T) {
	imgs := []uploadedImage{{URL: "https://cdn/a.jpg", Width: 800, Height: 600}}
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[{"cardData":{"propertyId":"-10000","propertyName":"分类","valuesList":[{"catId":"50023914","catName":"电子资料","channelCatId":"202036301","isClicked":"1"}]}}]}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	category, _, err := client.recommendPublishCategory(context.Background(), consignCookies, "其他虚拟资料", "其他虚拟资料", imgs)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	selected := category["categoryPredictResult"].(map[string]any)
	if selected["catId"] != "50023914" || selected["catName"] != "电子资料" || selected["channelCatId"] != "202036301" {
		t.Fatalf("selected=%+v", selected)
	}
	if labels := publishLabels(category); len(labels) != 1 {
		t.Fatalf("labels=%+v", labels)
	}
}

func TestDefaultVirtualPublishCategoryIncludesSelectedCategoryLabel(t *testing.T) {
	category := DefaultVirtualPublishCategory()
	if category.CatID != "50023914" || category.CatName != "电子资料" || category.ChannelCatID != "202036301" || category.TBCatID != "" {
		t.Fatalf("default category=%+v", category)
	}
	fallback := fallbackPublishCategory(category)
	labels := publishLabels(fallback)
	if len(labels) != 1 {
		t.Fatalf("labels=%+v", labels)
	}
	label := labels[0].(map[string]any)
	if label["channelCateId"] != "202036301" || label["channelCateName"] != "电子资料" || label["propertyId"] != "-10000" {
		t.Fatalf("label=%+v", label)
	}
}

func TestRecommendPublishCategoryDataNil(t *testing.T) {
	imgs := []uploadedImage{{URL: "u", Width: 1, Height: 1}}
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	_, _, err := client.recommendPublishCategory(context.Background(), consignCookies, "T", "D", imgs)
	if err == nil || !strings.Contains(err.Error(), "类目推荐响应缺少 data") {
		t.Fatalf("err=%v", err)
	}
}

func TestRecommendPublishCategoryEmptyCatId(t *testing.T) {
	imgs := []uploadedImage{{URL: "u", Width: 1, Height: 1}}
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"mtop.taobao.idle.kgraph.property.recommend": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{}}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	_, _, err := client.recommendPublishCategory(context.Background(), consignCookies, "T", "D", imgs)
	if err == nil || !strings.Contains(err.Error(), "未能自动识别商品类目") {
		t.Fatalf("err=%v", err)
	}
}

// ---- PublishLocations 真实坐标查询测试 ----

func TestPublishLocationsUsesProvidedCoordinates(t *testing.T) {
	var requested map[string]any
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"mtop.taobao.idle.local.poi.get": func(w http.ResponseWriter, r *http.Request) {
			requested, _ = parseDataURL(readBody(r))
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":[{"area":"A","city":"C","divisionId":"1","longitude":118.7,"latitude":31.9,"poiId":"p","poi":"P","prov":"PR"},{"area":"A","city":"C","divisionId":"1","longitude":118.71,"latitude":31.91,"poiId":"p2","poi":"P2","prov":"PR"}]}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	locations, _, err := client.PublishLocations(context.Background(), consignCookies, 118.7, 31.9)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if requested["longitude"] != float64(118.7) || requested["latitude"] != float64(31.9) {
		t.Fatalf("查询坐标=%+v", requested)
	}
	if len(locations) != 2 || locations[0].Area != "A" || locations[1].POIID != "p2" {
		t.Fatalf("locations=%+v", locations)
	}
}

func TestPublishLocationsMalformedAddress(t *testing.T) {
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"mtop.taobao.idle.local.poi.get": func(w http.ResponseWriter, r *http.Request) {
			// commonAddresses[0] 不是 map
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":["not-a-map"]}}`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	_, _, err := client.PublishLocations(context.Background(), consignCookies, 118.7, 31.9)
	if err == nil || !strings.Contains(err.Error(), "没有可用") {
		t.Fatalf("err=%v", err)
	}
}

// ---- callMTop / buildMTopQuery ----

func TestCallMTopParseFailure(t *testing.T) {
	dt := &dispatchTransport{handlers: map[string]http.HandlerFunc{
		"some.api": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `bad{`)
		},
	}}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: dt}}
	_, _, err := client.callMTop(context.Background(), consignCookies, "http://x", "some.api", "1.0", "spm", "spmPre", "log", map[string]any{"k": "v"})
	if err == nil || !strings.Contains(err.Error(), "解析 some.api 响应失败") {
		t.Fatalf("err=%v", err)
	}
}

func TestCallMTopRequestError(t *testing.T) {
	// 指向不可达地址
	client := &ClientImpl{HTTPClient: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: "http://127.0.0.1:1"}, Timeout: 0}}
	_, _, err := client.callMTop(context.Background(), consignCookies, "http://127.0.0.1:1", "any.api", "1.0", "s", "p", "l", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "any.api 请求失败") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildMTopQuery(t *testing.T) {
	q := buildMTopQuery("myapi", "2.0", "T", "SIGN", "spmCnt", "spmPre", "logID")
	if !strings.Contains(q, "api=myapi") || !strings.Contains(q, "v=2.0") ||
		!strings.Contains(q, "t=T") || !strings.Contains(q, "sign=SIGN") ||
		!strings.Contains(q, "spm_cnt=spmCnt") || !strings.Contains(q, "spm_pre=spmPre") ||
		!strings.Contains(q, "log_id=logID") {
		t.Fatalf("query=%q 缺字段", q)
	}
}

// ---- 发布相关纯函数 ----

func TestPublishImagePayload(t *testing.T) {
	p := publishImagePayload(uploadedImage{URL: "u", Width: 100, Height: 200}, true)
	if p["url"] != "u" || p["widthSize"] != 100 || p["heightSize"] != 200 ||
		p["major"] != true || p["type"] != 0 || p["status"] != "done" {
		t.Fatalf("payload=%+v", p)
	}
}

func TestPublishLabelsExtractsClicked(t *testing.T) {
	category := map[string]any{
		"cardList": []any{
			map[string]any{
				"cardData": map[string]any{
					"propertyId":   "p1",
					"propertyName": "品牌",
					"valuesList": []any{
						map[string]any{"isClicked": false, "catName": "X"},
						map[string]any{"isClicked": true, "catName": "Nike", "channelCatId": "cc", "tbCatId": "tc"},
					},
				},
			},
		},
	}
	labels := publishLabels(category)
	if len(labels) != 1 {
		t.Fatalf("labels=%d want 1", len(labels))
	}
	l := labels[0].(map[string]any)
	if l["propertyId"] != "p1" || l["channelCateId"] != "cc" || l["tbCatId"] != "tc" {
		t.Fatalf("label=%+v", l)
	}
	if l["properties"] != "p1##品牌:cc##Nike" {
		t.Fatalf("properties=%v", l["properties"])
	}
}

func TestPublishLabelsEmpty(t *testing.T) {
	if got := publishLabels(map[string]any{}); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
	// cardData 为 nil 的卡片应跳过
	out := publishLabels(map[string]any{"cardList": []any{map[string]any{"cardData": nil}}})
	if len(out) != 0 {
		t.Fatalf("got=%v", out)
	}
}

func TestPublishErrorError(t *testing.T) {
	// 有 Ret
	e := &PublishError{Ret: []string{"FAIL::a", "FAIL::b"}}
	if got := e.Error(); got != "FAIL::a; FAIL::b" {
		t.Fatalf("Error()=%q", got)
	}
	// 无 Ret，有 Body
	e = &PublishError{Code: PublishErrorUnknown, Body: "some body"}
	if got := e.Error(); got != "some body" {
		t.Fatalf("Error()=%q", got)
	}
	// 无 Ret 无 Body，回落 Code
	e = &PublishError{Code: PublishErrorUnknown}
	if got := e.Error(); got != string(PublishErrorUnknown) {
		t.Fatalf("Error()=%q", got)
	}
	// 长 Body 截断
	longBody := strings.Repeat("x", 300)
	e = &PublishError{Code: PublishErrorUnknown, Body: longBody}
	got := e.Error()
	if !strings.HasPrefix(got, "xxxxx") || len(got) > 244 {
		t.Fatalf("Error() 截断异常 len=%d", len(got))
	}
}

func TestClassifyPublishErrorUnknown(t *testing.T) {
	// 非已知错误，回落 unknown
	err := classifyPublishError([]string{"FAIL_BIZ_RANDOM::随机错误"}, map[string]any{})
	var pe *PublishError
	if !errors.As(err, &pe) || pe.Code != PublishErrorUnknown {
		t.Fatalf("err=%v want unknown", err)
	}
}

func TestClassifyPublishErrorLoginInBody(t *testing.T) {
	// ret 非空，但 body 含 login
	err := classifyPublishError([]string{}, map[string]any{"msg": "need login again"})
	var pe *PublishError
	if !errors.As(err, &pe) || pe.Code != PublishErrorTokenExpired {
		t.Fatalf("err=%v want token expired", err)
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("has stock permission", []string{"stock", "permission"}) {
		t.Fatalf("want true")
	}
	if containsAny("nothing here", []string{"stock", "permission"}) {
		t.Fatalf("want false")
	}
	if containsAny("", []string{}) {
		t.Fatalf("empty want false")
	}
}

func TestRetFromDecoded(t *testing.T) {
	decoded := map[string]any{"ret": []any{"SUCCESS::a", "FAIL::b", 123}}
	got := retFromDecoded(decoded)
	if len(got) != 3 || got[0] != "SUCCESS::a" || got[1] != "FAIL::b" || got[2] != "123" {
		t.Fatalf("got=%v", got)
	}
	// 无 ret 字段
	if got := retFromDecoded(map[string]any{}); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestMapFromAny(t *testing.T) {
	if m := mapFromAny(map[string]any{"k": "v"}); m == nil || m["k"] != "v" {
		t.Fatalf("m=%v", m)
	}
	if m := mapFromAny("not-map"); m != nil {
		t.Fatalf("m=%v want nil", m)
	}
	if m := mapFromAny(nil); m != nil {
		t.Fatalf("m=%v want nil", m)
	}
}

func TestFindStringDeepNested(t *testing.T) {
	v := map[string]any{
		"a": []any{
			map[string]any{"b": map[string]any{"item_id": "deep-id"}},
		},
	}
	if got := findStringDeep(v, "itemId", "item_id", "id"); got != "deep-id" {
		t.Fatalf("got=%q", got)
	}
	// 多 key 命中第一个非空
	v = map[string]any{"id": "first"}
	if got := findStringDeep(v, "itemId", "item_id", "id"); got != "first" {
		t.Fatalf("got=%q", got)
	}
	// 找不到
	if got := findStringDeep(map[string]any{"a": "b"}, "missing"); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestEscapeMultipartFilename(t *testing.T) {
	if got := escapeMultipartFilename(`a"b\c`); got != `a\"b\\c` {
		t.Fatalf("got=%q", got)
	}
	if got := escapeMultipartFilename("normal"); got != "normal" {
		t.Fatalf("got=%q", got)
	}
}

// ---- helpers ----

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func readBody(r *http.Request) string {
	buf := make([]byte, r.ContentLength)
	r.Body.Read(buf)
	return string(buf)
}

// parseDataURL 解析 "data=%7B...%7D" 形式的 body 中的 data 字段为 map。
func parseDataURL(body string) (map[string]any, bool) {
	if !strings.HasPrefix(body, "data=") {
		return nil, false
	}
	enc := strings.TrimPrefix(body, "data=")
	dec, err := url.QueryUnescape(enc)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(dec), &m); err != nil {
		return nil, false
	}
	return m, true
}
