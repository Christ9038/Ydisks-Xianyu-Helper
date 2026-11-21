package mtopsim

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"xianyu-go/internal/xianyu/mtop"
)

// handlerFunc 是单个 MTOP 接口的业务状态机：acct 是已通过登录态、Token 和签名校验的账号，
// data 是解码后的请求数据；返回平台标记和响应 data 字段。调用方持有 Gateway.mu。
type handlerFunc func(acct *account, data map[string]any) ([]string, any)

// handlers 返回接口名到业务状态机的映射；调用方必须持有 mu。
func (g *Gateway) handlers() map[string]handlerFunc {
	return map[string]handlerFunc{
		APIToken:             g.handleToken,
		APILoginUser:         g.handleLoginUser,
		APIUserNav:           g.handleUserNav,
		APIItemList:          g.handleItemList,
		APIOrderDetail:       g.handleOrderDetail,
		APISoldOrders:        g.handleSoldOrders,
		APIConsign:           g.handleConsign,
		APIFreeShipping:      g.handleFreeShipping,
		APIAdjustPrice:       g.handleAdjustPrice,
		APIRateList:          g.handleRateList,
		APIRateCreate:        g.handleRateCreate,
		APIPolish:            g.handlePolish,
		APIPolishBackup:      g.handlePolish,
		APIChatUser:          g.handleChatUser,
		APIChatItemSearch:    g.handleChatItemSearch,
		APICategoryRecommend: g.handleCategoryRecommend,
		APIPublish:           g.handlePublish,
		APIItemEditDetail:    g.handleItemEditDetail,
		APIItemEdit:          g.handleItemEdit,
	}
}

// success 返回业务成功的平台标记和 data 字段。
func success(payload any) ([]string, any) {
	return []string{RetSuccess}, payload
}

// failure 返回业务失败的平台标记和空 data。
func failure(ret string) ([]string, any) {
	return []string{ret}, map[string]any{}
}

// dataString 读取请求数据中的字符串字段；数字字段按其十进制原文返回，不经过浮点数。
func dataString(data map[string]any, key string) string {
	// value 是按运行时类型断言后的字段值。
	switch value := data[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

// dataInt 读取请求数据中的整数字段；缺失或非法时返回 fallback。
func dataInt(data map[string]any, key string, fallback int) int {
	// parsed、parseErr 是字段按十进制整数解析的结果。
	parsed, parseErr := strconv.Atoi(dataString(data, key))
	if parseErr != nil {
		return fallback
	}
	return parsed
}

// sellerOrder 返回属于当前卖家的订单；订单不存在或属于他人时返回 nil。调用方必须持有 mu。
func (g *Gateway) sellerOrder(acct *account, orderID string) *Order {
	// order、exists 是订单及其是否存在。
	order, exists := g.orders[orderID]
	if !exists || order.SellerID != acct.UserID {
		return nil
	}
	return order
}

// handleToken 签发 WebSocket 注册用的 accessToken；请求必须携带注册 appKey 和设备标识。
func (g *Gateway) handleToken(acct *account, data map[string]any) ([]string, any) {
	if dataString(data, "appKey") != mtop.RegAppKey || dataString(data, "deviceId") == "" {
		return failure(RetBadRequest)
	}
	acct.accessTokens++
	return success(map[string]any{
		"accessToken":            fmt.Sprintf("access-%s-%d", acct.UserID, acct.accessTokens),
		"accessTokenExpiredTime": strconv.FormatInt(g.now().Add(g.tokenTTL).UnixMilli(), 10),
	})
}

// handleLoginUser 确认登录态有效；能到达这里说明 Session 与 Token 均已通过校验。
func (g *Gateway) handleLoginUser(acct *account, _ map[string]any) ([]string, any) {
	return success(map[string]any{"userId": acct.UserID})
}

// handleUserNav 返回当前账号的昵称和头像。
func (g *Gateway) handleUserNav(acct *account, _ map[string]any) ([]string, any) {
	return success(map[string]any{"module": map[string]any{"base": map[string]any{
		"displayName": acct.Nickname,
		"displayNick": acct.Nickname,
		"avatar":      "https://img.alicdn.example/avatar/" + acct.UserID + ".png",
	}}})
}

// handleItemList 分页返回当前账号的在售商品。
// 协议事实：账号没有在售商品时，成功响应省略 cardList 字段，客户端必须按空列表处理。
func (g *Gateway) handleItemList(acct *account, data map[string]any) ([]string, any) {
	if dataString(data, "userId") != acct.UserID {
		return failure(RetBadRequest)
	}
	// onSale 保存当前账号全部在售商品，顺序与发布顺序一致。
	var onSale []*Item
	// itemID 是按发布顺序遍历的商品 ID。
	for _, itemID := range g.itemOrder {
		// item 是当前遍历到的商品。
		if item := g.items[itemID]; item.SellerID == acct.UserID && item.OnSale {
			onSale = append(onSale, item)
		}
	}
	if len(onSale) == 0 {
		return success(map[string]any{"nextPage": false})
	}
	// pageNumber、pageSize 是请求的页码和每页条数。
	pageNumber, pageSize := dataInt(data, "pageNumber", 1), dataInt(data, "pageSize", 20)
	// start、end 是本页在 onSale 中的半开区间，越界时收敛为空页。
	start, end := clampPage(len(onSale), pageNumber, pageSize)
	// cards 是本页的商品卡片。
	cards := make([]any, 0, end-start)
	// item 是本页中的一件商品。
	for _, item := range onSale[start:end] {
		cards = append(cards, map[string]any{"cardType": 1003, "cardData": map[string]any{
			"id":          item.ID,
			"title":       item.Title,
			"categoryId":  "50023914",
			"auctionType": "b",
			"itemStatus":  0,
			"detailUrl":   "fleamarket://item?id=" + item.ID,
			"priceInfo":   map[string]any{"preText": "¥", "price": item.Price},
			"picInfo":     map[string]any{"picUrl": "https://img.alicdn.example/item/" + item.ID + ".png", "width": 800, "height": 800},
			"detailParams": map[string]any{
				"itemId": item.ID, "title": item.Title, "soldPrice": item.Price, "isSKU": strconv.FormatBool(item.MultiSpec),
			},
		}})
	}
	return success(map[string]any{"cardList": cards, "totalCount": len(onSale), "nextPage": end < len(onSale)})
}

// clampPage 把页码和每页条数换算成长度为 total 的列表上的半开区间。
func clampPage(total, pageNumber, pageSize int) (start, end int) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start = (pageNumber - 1) * pageSize
	if start > total {
		start = total
	}
	end = start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

// handleOrderDetail 返回订单详情：状态码、购买件数、完整规格组合和成交金额。买卖双方都可查看。
func (g *Gateway) handleOrderDetail(acct *account, data map[string]any) ([]string, any) {
	// order、exists 是请求的订单及其是否存在。
	order, exists := g.orders[dataString(data, "tid")]
	if !exists || (order.SellerID != acct.UserID && order.BuyerID != acct.UserID) {
		return failure(RetOrderNotFound)
	}
	// itemInfo 是详情中的商品信息节点。
	itemInfo := map[string]any{"itemId": order.ItemID, "title": g.items[order.ItemID].Title, "buyAmount": strconv.Itoa(order.Quantity)}
	if len(order.Specs) > 0 {
		// properties 是结构化的规格列表，每个维度一条。
		properties := make([]any, 0, len(order.Specs))
		// spec 是订单的一组规格维度。
		for _, spec := range order.Specs {
			properties = append(properties, map[string]any{"name": spec.Name, "value": spec.Value})
		}
		itemInfo["skuProperties"] = properties
	}
	return success(map[string]any{
		"utArgs": map[string]any{"orderStatus": order.Status.detailCode(), "orderId": order.ID},
		"components": []any{map[string]any{"render": "orderInfoVO", "data": map[string]any{
			"itemInfo":  itemInfo,
			"priceInfo": map[string]any{"amount": map[string]any{"value": order.Amount}},
		}}},
	})
}

// handleSoldOrders 分页返回当前卖家的已售订单，最新订单在前。
// 砍价且尚未刀成的待发货订单带有 SKIP_PIN 按钮，客户端据此识别砍价订单。
func (g *Gateway) handleSoldOrders(acct *account, data map[string]any) ([]string, any) {
	// sold 保存当前卖家的全部订单，按创建时间倒序。
	var sold []*Order
	// index 从最新订单向最早订单遍历。
	for index := len(g.orderSeq) - 1; index >= 0; index-- {
		// order 是当前遍历到的订单。
		if order := g.orders[g.orderSeq[index]]; order.SellerID == acct.UserID {
			sold = append(sold, order)
		}
	}
	// start、end 是本页在 sold 中的半开区间。
	start, end := clampPage(len(sold), dataInt(data, "pageNumber", 1), dataInt(data, "rowsPerPage", 30))
	// rows 是本页的订单行。
	rows := make([]any, 0, end-start)
	// order 是本页中的一笔订单。
	for _, order := range sold[start:end] {
		// buttons 是卖家可执行的操作按钮。
		buttons := []any{}
		if order.Bargain && !order.BargainFormed && order.Status == OrderPendingShip {
			buttons = append(buttons, map[string]any{"tradeAction": "SKIP_PIN", "name": "免拼发货"})
		}
		rows = append(rows, map[string]any{
			"commonData": map[string]any{
				"orderId": order.ID, "itemId": order.ItemID, "orderStatus": order.Status.soldLabel(),
				"inRefund": false, "orderCreateTime": strconv.FormatInt(order.CreatedAt.UnixMilli(), 10),
			},
			"buyerInfoVO": map[string]any{"buyerId": order.BuyerID, "userNick": order.BuyerNick},
			"priceVO":     map[string]any{"totalPrice": order.Amount, "buyNum": strconv.Itoa(order.Quantity)},
			"rightVO":     map[string]any{"btnList": buttons},
		})
	}
	return success(map[string]any{"module": map[string]any{
		"items": rows, "nextPage": end < len(sold), "totalCount": strconv.Itoa(len(sold)),
	}})
}

// handleConsign 执行虚拟商品确认发货状态机：只有本卖家、已付款、已刀成（如为砍价单）且文本未命中敏感词的订单才会进入已发货。
func (g *Gateway) handleConsign(acct *account, data map[string]any) ([]string, any) {
	// order 是请求确认发货的订单。
	order := g.sellerOrder(acct, dataString(data, "orderId"))
	if order == nil {
		return failure(RetOrderNotFound)
	}
	switch order.Status {
	case OrderShipped, OrderCompleted:
		return failure(RetAlreadyDelivered)
	case OrderPendingShip:
	default:
		return failure(RetOrderStatusInvalid)
	}
	if order.Bargain && !order.BargainFormed {
		return failure(RetGrouponNotFormed)
	}
	// tradeText 是提交的文本发货凭证。
	tradeText := dataString(data, "tradeText")
	// word 是敏感词表中的一个词。
	for _, word := range g.sensitiveWords {
		if word != "" && strings.Contains(tradeText, word) {
			return failure(RetSensitiveWord)
		}
	}
	order.Status = OrderShipped
	order.ConsignCount++
	order.ConsignText = tradeText
	order.ConsignPictures = nil
	// pictures、isList 是提交的图片凭证列表及其类型是否正确。
	if pictures, isList := data["picList"].([]any); isList {
		// picture 是单个图片凭证地址。
		for _, picture := range pictures {
			// address、isText 是图片地址及其是否为字符串。
			if address, isText := picture.(string); isText {
				order.ConsignPictures = append(order.ConsignPictures, address)
			}
		}
	}
	return success(map[string]any{})
}

// handleFreeShipping 执行砍价订单免拼：只有本卖家、已付款且尚未刀成的砍价订单可以免拼，
// 请求中的商品和买家必须与订单一致。免拼只让订单刀成，不改变发货状态。
func (g *Gateway) handleFreeShipping(acct *account, data map[string]any) ([]string, any) {
	// order 是请求免拼的订单。
	order := g.sellerOrder(acct, dataString(data, "bizOrderId"))
	if order == nil {
		return failure(RetOrderNotFound)
	}
	if dataString(data, "itemId") != order.ItemID || dataString(data, "buyerId") != order.BuyerID {
		return failure(RetBadRequest)
	}
	if !order.Bargain || order.BargainFormed || order.Status != OrderPendingShip {
		return failure(RetGrouponNotPending)
	}
	if order.GrouponSyncLag > 0 {
		order.GrouponSyncLag--
		return failure(RetGrouponSyncLag)
	}
	order.BargainFormed = true
	order.FreeShipped = true
	return success(map[string]any{})
}

// handleAdjustPrice 修改待付款订单的成交价格；modifyFee 以分为单位。
func (g *Gateway) handleAdjustPrice(acct *account, data map[string]any) ([]string, any) {
	// order 是请求改价的订单。
	order := g.sellerOrder(acct, dataString(data, "orderId"))
	if order == nil {
		return failure(RetOrderNotFound)
	}
	// cents 是目标价格，单位为分。
	cents := dataInt(data, "modifyFee", 0)
	if cents <= 0 {
		return failure(RetBadRequest)
	}
	if order.Status != OrderWaitPay {
		return failure(RetOrderStatusInvalid)
	}
	if order.AdjustBusy > 0 {
		order.AdjustBusy--
		return failure(RetCannotModifyFee)
	}
	order.Amount = fmt.Sprintf("%d.%02d", cents/100, cents%100)
	return success(map[string]any{"success": true})
}

// handleRateList 返回当前卖家交易成功且尚未评价买家的订单。
func (g *Gateway) handleRateList(acct *account, data map[string]any) ([]string, any) {
	// pending 保存待评价订单行，最新订单在前。
	var pending []any
	// index 从最新订单向最早订单遍历。
	for index := len(g.orderSeq) - 1; index >= 0; index-- {
		// order 是当前遍历到的订单。
		order := g.orders[g.orderSeq[index]]
		if order.SellerID == acct.UserID && order.Status == OrderCompleted && !order.SellerRated {
			pending = append(pending, map[string]any{"tradeId": order.ID, "itemId": order.ItemID})
		}
	}
	// start、end 是本页在 pending 中的半开区间。
	start, end := clampPage(len(pending), dataInt(data, "pageNumber", 1), dataInt(data, "rowsPerPage", 50))
	return success(map[string]any{"module": map[string]any{"items": pending[start:end], "nextPage": end < len(pending)}})
}

// handleRateCreate 评价买家：只有本卖家、交易成功且尚未评价的订单可以评价。
func (g *Gateway) handleRateCreate(acct *account, data map[string]any) ([]string, any) {
	// order 是请求评价的订单。
	order := g.sellerOrder(acct, dataString(data, "tradeId"))
	if order == nil {
		return failure(RetOrderNotFound)
	}
	if order.SellerRated {
		return failure(RetAlreadyRated)
	}
	if order.Status != OrderCompleted {
		return failure(RetRateNotAllowed)
	}
	order.SellerRated = true
	order.SellerFeedback = dataString(data, "feedback")
	return success(map[string]any{})
}

// handlePolish 擦亮商品：同一商品在模拟器时钟的同一天内只能擦亮一次。主接口与备用接口共用该状态。
func (g *Gateway) handlePolish(acct *account, data map[string]any) ([]string, any) {
	// item、exists 是请求擦亮的商品及其是否存在。
	item, exists := g.items[dataString(data, "itemId")]
	if !exists || item.SellerID != acct.UserID || !item.OnSale {
		return failure(RetBadRequest)
	}
	// today 是模拟器时钟下的当天日期。
	today := g.now().Format("2006-01-02")
	if item.PolishedOn == today {
		return failure(RetPolishAgain)
	}
	item.PolishedOn = today
	return success(map[string]any{})
}

// handleChatUser 返回会话中买家的昵称和头像；会话必须属于当前卖家的某笔订单。
func (g *Gateway) handleChatUser(acct *account, data map[string]any) ([]string, any) {
	// chatID 是请求查询的会话 ID。
	chatID := dataString(data, "sessionId")
	// orderID 按创建顺序遍历订单，寻找该会话对应的买家。
	for _, orderID := range g.orderSeq {
		// order 是当前遍历到的订单。
		if order := g.orders[orderID]; order.ChatID == chatID && order.SellerID == acct.UserID {
			return success(map[string]any{"userInfo": map[string]any{
				"fishNick": order.BuyerNick,
				"logo":     "https://img.alicdn.example/avatar/" + order.BuyerID + ".png",
			}})
		}
	}
	return failure(RetBadRequest)
}

// handleChatItemSearch 在会话中查询商品：own 返回当前账号的在售商品，other 返回对方的在售商品（买家通常没有）。
func (g *Gateway) handleChatItemSearch(acct *account, data map[string]any) ([]string, any) {
	// keyword 是标题关键词；为空时不过滤。
	keyword := dataString(data, "queryWord")
	// matched 保存命中的商品行。
	matched := []any{}
	if dataString(data, "searchItemRole") == "own" {
		// itemID 是按发布顺序遍历的商品 ID。
		for _, itemID := range g.itemOrder {
			// item 是当前遍历到的商品。
			item := g.items[itemID]
			if item.SellerID != acct.UserID || !item.OnSale || !strings.Contains(item.Title, keyword) {
				continue
			}
			matched = append(matched, map[string]any{
				"itemId": item.ID, "title": item.Title, "reservePrice": item.Price, "desc": item.Title,
				"absoluteMajorPicture": "//img.alicdn.example/item/" + item.ID + ".png",
			})
		}
	}
	// start、end 是本页在 matched 中的半开区间；该接口固定每页 20 条。
	start, end := clampPage(len(matched), dataInt(data, "pageNumber", 1), 20)
	return success(map[string]any{"items": matched[start:end], "nextPage": end < len(matched)})
}

// handleCategoryRecommend 按标题推荐发布类目：主推荐在 categoryPredictResult，全部候选在“分类”卡片中。
// 标题包含“无法识别”时不返回任何类目，用于覆盖客户端的默认类目回退。
func (g *Gateway) handleCategoryRecommend(_ *account, data map[string]any) ([]string, any) {
	if strings.Contains(dataString(data, "title"), "无法识别") {
		return success(map[string]any{"cardList": []any{}})
	}
	// primary、secondary 是主推荐类目和另一个候选类目。
	primary := map[string]any{"catId": "50023914", "catName": "虚拟物品", "channelCatId": "202036301", "tbCatId": "50023914", "isClicked": true}
	// secondary 是未被选中的另一个候选类目，用于验证候选列表不丢失。
	secondary := map[string]any{"catId": "50025461", "catName": "游戏软件平台", "channelCatId": "202036302", "tbCatId": "50025461", "isClicked": false}
	return success(map[string]any{
		"categoryPredictResult": map[string]any{"catId": "50023914", "catName": "虚拟物品", "channelCatId": "202036301", "tbCatId": "50023914"},
		"cardList": []any{map[string]any{"cardData": map[string]any{
			"propertyId": "-10000", "propertyName": "分类", "valuesList": []any{primary, secondary},
		}}},
	})
}

// handlePublish 发布商品：标题、类目和至少一张已上传图片齐全时创建在售商品并返回商品 ID。
func (g *Gateway) handlePublish(acct *account, data map[string]any) ([]string, any) {
	// text、category、price 是发布请求中的文案、类目和价格节点。
	text, _ := data["itemTextDTO"].(map[string]any)
	// category 是发布请求中的类目节点。
	category, _ := data["itemCatDTO"].(map[string]any)
	// price 是发布请求中的价格节点。
	price, _ := data["itemPriceDTO"].(map[string]any)
	// images、skus 是商品图片列表和多规格 SKU 列表。
	images, _ := data["imageInfoDOList"].([]any)
	// skus 是多规格商品的 SKU 列表；单规格商品为空。
	skus, _ := data["itemSkuList"].([]any)
	// title 是商品标题。
	title := dataString(text, "title")
	if title == "" || dataString(category, "catId") == "" || len(images) == 0 || dataInt(data, "quantity", 0) < 1 {
		return failure(RetBadRequest)
	}
	// cents 是单规格商品的价格，单位为分；多规格商品的价格在各 SKU 上。
	cents := dataInt(price, "priceInCent", 0)
	if len(skus) == 0 && cents <= 0 {
		return failure(RetBadRequest)
	}
	// itemID 是新商品的平台 ID。
	itemID := g.addItemLocked(&Item{
		SellerID: acct.UserID, Title: title, Price: fmt.Sprintf("%d.%02d", cents/100, cents%100),
		MultiSpec: len(skus) > 0, OnSale: true, PublishData: data,
	})
	g.items[itemID].Quantity = dataInt(data, "quantity", 0)
	if len(skus) > 0 {
		g.items[itemID].SKUs, g.items[itemID].Quantity = g.publishedSKUs(skus)
	}
	return success(map[string]any{"itemId": itemID})
}

// publishedSKUs 把发布请求中的规格行转换为平台 SKU（分配 skuId），并返回各行库存之和。
func (g *Gateway) publishedSKUs(rows []any) ([]ItemSKU, int) {
	// skus 保存转换后的规格行；total 是各行库存之和。
	skus, total := make([]ItemSKU, 0, len(rows)), 0
	// raw 是请求中的一行规格。
	for _, raw := range rows {
		// row 是该行的对象形式。
		row, _ := raw.(map[string]any)
		// sku 是转换后的规格行。
		sku := ItemSKU{ID: fmt.Sprintf("6%012d", g.nextSequence()), PriceCents: dataInt(row, "priceInCent", 0), Quantity: dataInt(row, "quantity", 0)}
		// properties 是该行的规格组合。
		properties, _ := row["propertyList"].([]any)
		// property 是一个规格项。
		for _, rawProperty := range properties {
			// fields 是规格项的对象形式。
			fields, _ := rawProperty.(map[string]any)
			sku.Properties = append(sku.Properties, Spec{Name: dataString(fields, "propertyText"), Value: dataString(fields, "valueText")})
		}
		total += sku.Quantity
		skus = append(skus, sku)
	}
	return skus, total
}

// ownedEditableItem 返回请求者拥有的商品；不存在或不属于该卖家时返回 nil。
func (g *Gateway) ownedEditableItem(acct *account, itemID string) *Item {
	// item、exists 是请求编辑的商品及其是否存在。
	item, exists := g.items[itemID]
	if !exists || item.SellerID != acct.UserID {
		return nil
	}
	return item
}

// handleItemEditDetail 返回商品编辑页详情。字段值与真实平台一致：数值大多是字符串，
// 多规格商品顶层 quantity 固定为 "1" 而库存在 itemSkuList，且不返回 itemProperties。
func (g *Gateway) handleItemEditDetail(acct *account, data map[string]any) ([]string, any) {
	// item 是请求读取的商品。
	item := g.ownedEditableItem(acct, dataString(data, "itemId"))
	if item == nil {
		return failure(RetBadRequest)
	}
	if item.EditUnsupported {
		return failure(RetEditUnsupported)
	}
	// detail 是返回给客户端的详情；有发布请求副本时以它为底，保证回填字段与发布时一致。
	detail := map[string]any{}
	if item.EditData != nil {
		detail = cloneMap(item.EditData)
	} else if item.PublishData != nil {
		detail = cloneMap(item.PublishData)
	} else {
		detail["itemTextDTO"] = map[string]any{"title": item.Title, "desc": "模拟商品"}
		detail["itemCatDTO"] = map[string]any{"catId": "50023914", "catName": "电子资料"}
		detail["itemPriceDTO"] = map[string]any{"priceInCent": item.Price}
		detail["imageInfoDOList"] = []any{map[string]any{"url": "https://img.alicdn.example/item/" + item.ID + ".png", "major": true}}
	}
	// 编辑页只会返回这些运行时字段，以下覆盖保证库存字段以平台当前状态为准。
	delete(detail, "itemProperties")
	delete(detail, "uniqueCode")
	delete(detail, "sourceId")
	detail["itemId"] = item.ID
	// 在售为 "0"，下架或售罄后为 "-2"，与真实平台一致。
	detail["itemStatus"] = "-2"
	if item.OnSale {
		detail["itemStatus"] = "0"
	}
	detail["bizcode"] = "pcMainPublish"
	if item.MultiSpec {
		detail["quantity"] = "1"
		// rows 是返回的规格行，数值字段统一为字符串。
		rows := make([]any, 0, len(item.SKUs))
		// sku 是平台内部的规格行。
		for _, sku := range item.SKUs {
			// properties 是该行规格组合，字段名与发布请求一致。
			properties := make([]any, 0, len(sku.Properties))
			// spec 是一个规格项。
			for _, spec := range sku.Properties {
				properties = append(properties, map[string]any{"propertyText": spec.Name, "valueText": spec.Value})
			}
			rows = append(rows, map[string]any{
				"skuId": sku.ID, "itemId": item.ID, "sellerId": item.SellerID,
				"quantity": strconv.Itoa(sku.Quantity), "priceInCent": strconv.Itoa(sku.PriceCents), "propertyList": properties,
			})
		}
		detail["itemSkuList"] = rows
	} else {
		detail["quantity"] = strconv.Itoa(item.Quantity)
		delete(detail, "itemSkuList")
	}
	return success(detail)
}

// handleItemEdit 以全量覆盖方式提交商品编辑。模拟器校验平台会拒绝的情形：缺少来源标识或唯一码、
// 缺少标题/类目/图片、多规格缺少 itemProperties 或规格行集合与平台当前不一致、库存超出范围。
func (g *Gateway) handleItemEdit(acct *account, data map[string]any) ([]string, any) {
	// item 是请求修改的商品。
	item := g.ownedEditableItem(acct, dataString(data, "itemId"))
	if item == nil {
		return failure(RetBadRequest)
	}
	if item.EditUnsupported {
		return failure(RetEditUnsupported)
	}
	// text、category、images 是必须随全量载荷回填的标题、类目和图片。
	text, _ := data["itemTextDTO"].(map[string]any)
	// category 是类目节点。
	category, _ := data["itemCatDTO"].(map[string]any)
	// images 是图片列表。
	images, _ := data["imageInfoDOList"].([]any)
	if dataString(data, "sourceId") != item.ID || dataString(data, "uniqueCode") == "" || dataString(data, "bizcode") != "pcMainPublish" ||
		dataString(text, "title") == "" || dataString(category, "catId") == "" || len(images) == 0 {
		return failure(RetBadRequest)
	}
	// rows 是请求中的规格行。
	rows, _ := data["itemSkuList"].([]any)
	if !item.MultiSpec {
		// quantity 是单规格新库存。
		quantity := dataInt(data, "quantity", 0)
		if len(rows) != 0 || quantity < 1 || quantity > 999999 {
			return failure(RetBadRequest)
		}
		item.Quantity = quantity
	} else {
		// properties 是该规格行的规格项列表。
		if properties, _ := data["itemProperties"].([]any); len(properties) == 0 || len(rows) != len(item.SKUs) {
			return failure(RetBadRequest)
		}
		// updated 保存按 skuId 匹配后的新库存；total、positive 用于重算与校验。
		updated, total, positive := map[string]int{}, 0, 0
		// raw 是请求中的一行规格。
		for _, raw := range rows {
			// row 是该行的对象形式。
			row, _ := raw.(map[string]any)
			// quantity 是该行新库存。
			quantity := dataInt(row, "quantity", -1)
			if quantity < 0 || quantity > 999999 {
				return failure(RetBadRequest)
			}
			updated[dataString(row, "skuId")] = quantity
			total += quantity
			if quantity > 0 {
				positive++
			}
		}
		// matched 统计请求行与平台规格行的匹配数，必须逐行一一对应。
		matched := 0
		// sku 是平台内部的规格行。
		for _, sku := range item.SKUs {
			// exists 表示商品是否存在。
			if _, exists := updated[sku.ID]; exists {
				matched++
			}
		}
		if matched != len(item.SKUs) || positive == 0 {
			return failure(RetBadRequest)
		}
		// i 是规格行下标；校验通过后才整体写入，避免半途修改。
		for i := range item.SKUs {
			item.SKUs[i].Quantity = updated[item.SKUs[i].ID]
		}
		item.Quantity = total
	}
	// 提交 itemStatus="0" 表示重新上架；未带该值的编辑不改变在售状态（与真实平台“原样重提不会上架”一致）。
	if dataString(data, "itemStatus") == "0" {
		item.OnSale = true
	}
	item.EditData = cloneMap(data)
	item.EditCount++
	return success(map[string]any{"itemId": item.ID})
}

// cloneMap 通过 JSON 往返深拷贝请求数据，使平台状态与调用方持有的载荷互不影响；数据来自 JSON 解码，往返必然成功。
func cloneMap(source map[string]any) map[string]any {
	// encoded、err 是序列化结果。
	encoded, err := json.Marshal(source)
	if err != nil {
		return map[string]any{}
	}
	// cloned 是深拷贝结果，数字保留为 json.Number 以与其他处理器的解码口径一致。
	cloned := map[string]any{}
	// decoder 按 json.Number 解码，保持与其他处理器一致的数字口径。
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	// err 保存本次调用返回的失败原因，供后续断言使用。
	if err := decoder.Decode(&cloned); err != nil {
		return map[string]any{}
	}
	return cloned
}
