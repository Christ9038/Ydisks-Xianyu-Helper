// Package mtop: 商品库存编辑（editDetail 读取 + edit 全量覆盖提交）。
//
// 协议依据：闲鱼 PC 编辑页 `/publish?itemId=` 先调用 `mtop.idle.pc.idleitem.editDetail` 读取可编辑详情，
// 保存时有 itemId 走 `mtop.idle.pc.idleitem.edit`，载荷与发布同构且为全量覆盖，因此提交前必须以
// 最新详情为底、只改库存字段。本文件只做平台协议，不写业务数据、不决定自动化规则。
package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ItemEditDetailAPI 是 PC 编辑页读取可编辑商品详情的端点。
// ItemEditAPI 是 PC 编辑页提交已有商品修改的端点。
const (
	ItemEditDetailAPI = "https://h5api.m.goofish.com/h5/mtop.idle.pc.idleitem.editDetail/1.0/"
	ItemEditAPI       = "https://h5api.m.goofish.com/h5/mtop.idle.pc.idleitem.edit/1.0/"
)

// itemStockMax 是单个库存数的上限，与发布页输入范围保持一致。
const itemStockMax = 999999

// ErrItemStockInvalid 表示库存修改请求本身不合法（范围、重复规格行、规格集合与商品不一致等），未发起编辑请求。
var ErrItemStockInvalid = errors.New("库存修改请求不合法")

// ErrItemStockChanged 表示提交前重新读取的平台库存与用户看到的库存不一致，已放弃提交以免覆盖他人修改。
var ErrItemStockChanged = errors.New("商品库存已在平台侧变化，请刷新后重试")

// ErrItemEditUnsupported 表示该商品的类目不支持网页端编辑（真机返回 FAIL_BIZ_PC_NOT_SUPPORT_PUBLISH_OR_EDIT），
// 属于确定的平台拒绝：没有任何数据被修改，调用方应提示用户改用闲鱼 App。
var ErrItemEditUnsupported = errors.New("该商品类目暂不支持网页端编辑，请在闲鱼 App 中修改库存")

// ErrItemEditDetailUnusable 表示平台返回的编辑详情缺少必要字段，无法安全地以它为底做全量覆盖。
var ErrItemEditDetailUnusable = errors.New("闲鱼未返回可用的商品编辑详情")

// ItemStockSKU 是多规格商品中一行规格组合的库存视图，不含任何凭证。
type ItemStockSKU struct {
	// Key 是该规格行在同一商品内的稳定标识：优先取平台 skuId，缺失时由规格名与规格值拼成。
	Key string
	// Label 是展示给用户的规格组合文案，例如“红色 / XL”。
	Label string
	// PriceCents 是该规格行的售价（分）；解析失败时为 0，仅用于展示。
	PriceCents int64
	// Quantity 是该规格行当前库存。
	Quantity int
}

// ItemEditDetail 是 editDetail 的解析结果。原始响应只保存在包内，供 EditItemStock 回填，不对外暴露。
type ItemEditDetail struct {
	// ItemID 是平台返回的商品 ID。
	ItemID string
	// Status 是平台原样返回的 itemStatus 文本，仅用于诊断。
	Status string
	// MultiSpec 表示商品带有 itemSkuList，库存由规格行提供。
	MultiSpec bool
	// Quantity 是单规格商品的顶层库存；多规格商品为各规格行库存之和（仅展示）。
	Quantity int
	// SKUs 是多规格商品的规格行；单规格商品为空。
	SKUs []ItemStockSKU
	// UpdatedCookies 是响应后更新的 Cookie，调用方需要按凭证规则写回；为空表示没有变化。
	UpdatedCookies string
	// raw 是 editDetail 的 data 原文，只在包内用于构造全量覆盖载荷。
	raw map[string]any
}

// ItemStockSKUEdit 是多规格库存修改中的一行。
type ItemStockSKUEdit struct {
	// Key 对应 ItemStockSKU.Key。
	Key string
	// Quantity 是要写入的新库存，范围 0..itemStockMax。
	Quantity int
	// ExpectedQuantity 是用户打开弹窗时看到的库存，用于乐观并发校验。
	ExpectedQuantity int
}

// ItemStockEditRequest 描述一次库存修改。单规格填 Quantity/ExpectedQuantity，多规格填 SKUs（必须覆盖全部规格行）。
type ItemStockEditRequest struct {
	// ItemID 是要修改的商品 ID。
	ItemID string
	// Quantity 是单规格商品的新库存，范围 1..itemStockMax。
	Quantity int
	// ExpectedQuantity 是单规格商品用户看到的库存，用于乐观并发校验。
	ExpectedQuantity int
	// SKUs 是多规格商品的全部规格行修改；非空表示按多规格处理。
	SKUs []ItemStockSKUEdit
}

// ItemStockEditResult 是库存修改成功后的结果。
type ItemStockEditResult struct {
	// ItemID 是被修改的商品 ID。
	ItemID string
	// Message 是平台 ret 的首条文本，仅用于诊断。
	Message string
	// DataFields 是 edit 成功响应 data 中的顶层字段名（已排序，不含取值），用于确认平台成功响应结构。
	DataFields []string
	// UpdatedCookies 是两次请求累计更新后的 Cookie；为空表示没有变化。
	UpdatedCookies string
}

// FetchItemEditDetail 读取商品的 PC 编辑详情。一次调用只发一次业务请求（Token 重签重试沿用 callMTop）。
func (c *ClientImpl) FetchItemEditDetail(ctx context.Context, cookiesStr, itemID string) (*ItemEditDetail, error) {
	// id 是去掉首尾空白后的商品 ID。
	id := strings.TrimSpace(itemID)
	if id == "" {
		return nil, fmt.Errorf("%w: 商品 ID 不能为空", ErrItemStockInvalid)
	}
	// decoded、updated、err 保存平台响应、更新后的 Cookie 和调用错误。
	decoded, updated, err := c.callMTop(ctx, cookiesStr, firstNonEmptyURL(c.ItemEditDetailURL, ItemEditDetailAPI),
		"mtop.idle.pc.idleitem.editDetail", "1.0", "a21ybx.publish.0.0", "a21ybx.publish.0.0",
		"xianyu_item_edit_detail", map[string]any{"itemId": id})
	if err != nil {
		return nil, err
	}
	// ret 是平台业务返回标记；缺少 SUCCESS 必须按统一规则分类，风控与会话失效不能被吞掉。
	ret := retFromDecoded(decoded)
	if !hasMTopSuccess(ret) {
		if itemEditRetUnsupported(ret) {
			return nil, ErrItemEditUnsupported
		}
		return nil, c.mtopResponseFailure("mtop.idle.pc.idleitem.editDetail", 200, ret, "平台 ret 未包含 SUCCESS")
	}
	// detail 是解析后的编辑详情。
	detail, err := parseItemEditDetail(decoded)
	if err != nil {
		return nil, err
	}
	detail.UpdatedCookies = updated
	return detail, nil
}

// EditItemStock 修改商品库存：先重新读取最新详情做快照，校验用户看到的库存未变化，
// 再以快照为底只改库存字段并全量提交。每次调用固定最多两次平台请求（读 + 写），遇到任何失败立即返回，不重试。
func (c *ClientImpl) EditItemStock(ctx context.Context, cookiesStr string, req ItemStockEditRequest) (*ItemStockEditResult, error) {
	// itemID 是规范化后的商品 ID。
	itemID := strings.TrimSpace(req.ItemID)
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := validateItemStockEditShape(itemID, req); err != nil {
		return nil, err
	}
	// detail、err 保存提交前重新读取的最新快照。
	detail, err := c.FetchItemEditDetail(ctx, cookiesStr, itemID)
	if err != nil {
		return nil, err
	}
	// payload 是以快照为底、只改库存后的全量覆盖载荷。
	payload, err := buildItemStockEditPayload(detail, itemID, req)
	if err != nil {
		return nil, err
	}
	// current 是写请求使用的 Cookie，优先采用读请求返回的最新值。
	current := cookiesStr
	if detail.UpdatedCookies != "" {
		current = detail.UpdatedCookies
	}
	// decoded、updated、err 保存提交响应、更新后的 Cookie 和调用错误。
	decoded, updated, err := c.callMTop(ctx, current, firstNonEmptyURL(c.ItemEditURL, ItemEditAPI),
		"mtop.idle.pc.idleitem.edit", "1.0", "a21ybx.publish.0.0", "a21ybx.publish.0.0",
		"xianyu_item_edit", payload)
	// finalCookies 是两次请求累计后应写回的 Cookie；写请求失败时也要带出读请求拿到的更新。
	finalCookies := updated
	if finalCookies == "" {
		finalCookies = detail.UpdatedCookies
	}
	if err != nil {
		return nil, err
	}
	// ret 是提交响应的业务返回标记。
	ret := retFromDecoded(decoded)
	if !hasMTopSuccess(ret) {
		if itemEditRetUnsupported(ret) {
			return nil, ErrItemEditUnsupported
		}
		return nil, c.mtopResponseFailure("mtop.idle.pc.idleitem.edit", 200, ret, "平台 ret 未包含 SUCCESS")
	}
	// reason 是平台业务失败的原因摘要，不含凭证。
	if reason := itemEditBusinessFailure(mapFromAny(decoded["data"])); reason != "" {
		return nil, fmt.Errorf("闲鱼商品编辑被拒绝: %s", reason)
	}
	return &ItemStockEditResult{ItemID: itemID, Message: firstRet(ret), DataFields: sortedFieldNames(mapFromAny(decoded["data"])), UpdatedCookies: finalCookies}, nil
}

// sortedFieldNames 返回 map 的顶层键名并排序，只暴露结构、不含任何取值。
func sortedFieldNames(fields map[string]any) []string {
	// names 保存排序前的键名。
	names := make([]string, 0, len(fields))
	// name 是当前键名。
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// itemEditRetUnsupported 判断平台 ret 是否表示“该商品不支持网页端发布或编辑”。
func itemEditRetUnsupported(ret []string) bool {
	// line 是当前 ret 文本。
	for _, line := range ret {
		if strings.Contains(strings.ToUpper(line), "PC_NOT_SUPPORT_PUBLISH_OR_EDIT") {
			return true
		}
	}
	return false
}

// itemEditBusinessFailure 识别“ret 成功但业务失败”的响应：data 中显式 success=false 或非成功 code。
// 平台尚无成功响应样本，只按通用标志位判断，成功结构确认后再收紧。
func itemEditBusinessFailure(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	// message 是平台给出的失败说明，按常见字段名依次取第一个非空值。
	message := ""
	// key 是当前尝试读取的说明字段名。
	for _, key := range []string{"msg", "message", "errorMsg", "errMsg", "resultMsg", "tips"} {
		if message = mtopString(data[key]); message != "" {
			break
		}
	}
	// key 是当前检查的成功标志字段名。
	for _, key := range []string{"success", "isSuccess"} {
		// flag 是该标志字段的原值。
		flag, exists := data[key]
		if !exists {
			continue
		}
		// isBool 表示该值是否为布尔类型；value 是当前被检查或转换的字段值。
		if value, isBool := flag.(bool); isBool && !value {
			return fallbackText(message, "平台返回编辑失败")
		}
		// isText 表示该值是否为字符串类型；text 是字符串形式的字段值。
		if text, isText := flag.(string); isText && strings.EqualFold(strings.TrimSpace(text), "false") {
			return fallbackText(message, "平台返回编辑失败")
		}
	}
	// code 是业务失败码；空、0、200、success 视为成功。
	code := mtopString(data["code"])
	if code == "" {
		code = mtopString(data["errorCode"])
	}
	switch strings.ToLower(code) {
	case "", "0", "200", "success":
		return ""
	}
	return fallbackText(message, "平台返回编辑失败（code="+code+"）")
}

// 编译期保证 ClientImpl 满足可选库存接口。
var _ ItemStockClient = (*ClientImpl)(nil)

// fallbackText 在 primary 为空时返回 fallback。
func fallbackText(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

// parseItemEditDetail 把 editDetail 响应解析为库存视图，并保留原始 data 供回填。
func parseItemEditDetail(decoded map[string]any) (*ItemEditDetail, error) {
	// data 是 editDetail 的业务数据；平台字段值大多是字符串。
	data := mapFromAny(decoded["data"])
	if len(data) == 0 {
		return nil, ErrItemEditDetailUnusable
	}
	// itemID 是平台返回的商品 ID，缺失说明响应不是商品详情。
	itemID := mtopString(data["itemId"])
	if itemID == "" {
		return nil, ErrItemEditDetailUnusable
	}
	// detail 是解析结果，raw 直接引用响应 data，构造载荷时会先深拷贝。
	detail := &ItemEditDetail{ItemID: itemID, Status: mtopString(data["itemStatus"]), raw: data}
	// rows 是平台返回的规格行原文。
	rows, _ := data["itemSkuList"].([]any)
	// seen 用于检测规格行 Key 是否重复；重复时无法按 Key 精确修改，必须拒绝。
	seen := map[string]struct{}{}
	// row 是当前规格行原文。
	for _, row := range rows {
		// sku 是当前规格行的对象形式。
		sku := mapFromAny(row)
		if sku == nil {
			return nil, ErrItemEditDetailUnusable
		}
		// view 是当前规格行解析后的库存视图。
		view := parseItemStockSKU(sku)
		// duplicated 表示该规格行标识此前是否已出现，用于拒绝重复行。
		if _, duplicated := seen[view.Key]; duplicated || view.Key == "" {
			return nil, ErrItemEditDetailUnusable
		}
		seen[view.Key] = struct{}{}
		detail.SKUs = append(detail.SKUs, view)
		detail.Quantity += view.Quantity
	}
	if len(detail.SKUs) > 0 {
		detail.MultiSpec = true
		return detail, nil
	}
	// 单规格商品必须带顶层 quantity，否则无法确认修改的是哪个字段。
	if _, exists := data["quantity"]; !exists {
		return nil, ErrItemEditDetailUnusable
	}
	detail.Quantity = mtopInt(data["quantity"])
	return detail, nil
}

// parseItemStockSKU 解析一行规格：Key 优先取 skuId，缺失时用“规格名=规格值”按顺序拼接。
func parseItemStockSKU(sku map[string]any) ItemStockSKU {
	// labels、keyParts 分别保存展示文案片段和拼接 Key 的片段。
	var labels, keyParts []string
	// properties 是该规格行的规格名与规格值列表。
	properties, _ := sku["propertyList"].([]any)
	// property 是当前规格名与规格值。
	for _, property := range properties {
		// fields 是当前规格项的对象形式。
		fields := mapFromAny(property)
		// name、value 是规格名与规格值文本，规格值缺失时退回 actualValueText。
		name := strings.TrimSpace(mtopString(fields["propertyText"]))
		// value 是当前被检查或转换的字段值。
		value := strings.TrimSpace(mtopString(fields["valueText"]))
		if value == "" {
			value = strings.TrimSpace(mtopString(fields["actualValueText"]))
		}
		if value == "" {
			continue
		}
		labels = append(labels, value)
		keyParts = append(keyParts, name+"="+value)
	}
	// key 是规格行标识。
	key := strings.TrimSpace(mtopString(sku["skuId"]))
	if key != "" {
		key = "sku:" + key
	} else if len(keyParts) > 0 {
		key = "spec:" + strings.Join(keyParts, "|")
	}
	// price 是售价（分）；解析失败按 0 处理，仅用于展示。
	price, _ := strconv.ParseInt(strings.TrimSpace(mtopString(sku["priceInCent"])), 10, 64)
	return ItemStockSKU{Key: key, Label: strings.Join(labels, " / "), PriceCents: price, Quantity: mtopInt(sku["quantity"])}
}

// validateItemStockEditShape 在发起任何平台请求前校验请求自身：范围、规格行重复。
func validateItemStockEditShape(itemID string, req ItemStockEditRequest) error {
	if itemID == "" {
		return fmt.Errorf("%w: 商品 ID 不能为空", ErrItemStockInvalid)
	}
	if len(req.SKUs) == 0 {
		if req.Quantity < 1 || req.Quantity > itemStockMax {
			return fmt.Errorf("%w: 单规格库存必须在 1 到 %d 之间", ErrItemStockInvalid, itemStockMax)
		}
		if req.ExpectedQuantity < 0 {
			return fmt.Errorf("%w: 缺少原库存", ErrItemStockInvalid)
		}
		return nil
	}
	// seen 保存已出现的规格行 Key，用于拒绝重复行。
	seen := map[string]struct{}{}
	// positive 统计新库存大于 0 的规格行数；全部为 0 等同售罄，应走下架。
	positive := 0
	// row 是当前待校验的规格行修改。
	for _, row := range req.SKUs {
		if strings.TrimSpace(row.Key) == "" {
			return fmt.Errorf("%w: 规格行标识不能为空", ErrItemStockInvalid)
		}
		// duplicated 表示该规格行标识此前是否已出现，用于拒绝重复行。
		if _, duplicated := seen[row.Key]; duplicated {
			return fmt.Errorf("%w: 规格行重复", ErrItemStockInvalid)
		}
		seen[row.Key] = struct{}{}
		if row.Quantity < 0 || row.Quantity > itemStockMax {
			return fmt.Errorf("%w: 规格库存必须在 0 到 %d 之间", ErrItemStockInvalid, itemStockMax)
		}
		if row.ExpectedQuantity < 0 {
			return fmt.Errorf("%w: 缺少原库存", ErrItemStockInvalid)
		}
		if row.Quantity > 0 {
			positive++
		}
	}
	if positive == 0 {
		return fmt.Errorf("%w: 至少一个规格库存需大于 0，全部售罄请使用下架", ErrItemStockInvalid)
	}
	return nil
}

// itemEditScalarKeys、itemEditObjectKeys、itemEditListKeys 是回填到 edit 载荷的 editDetail 字段白名单，
// 取自 PC 编辑页提交时的字段集合；不在名单内的运行时字段（redirectUrl、jumpUrl 等）不提交。
var (
	itemEditScalarKeys = []string{"attribute_biz_line", "bizcode", "bucketId", "canBargain", "defaultPrice", "errorTipsMsg",
		"freebies", "itemStatus", "itemTypeStr", "quantity", "scene", "simpleItem", "supportBargainPrice", "topics"}
	itemEditObjectKeys = []string{"asyncSecurityInfo", "baseParams", "itemAddrDTO", "itemCatDTO", "itemGroupDTO",
		"itemPostFeeDTO", "itemPriceDTO", "itemTextDTO", "itemTopicParams", "yhbItemInfoDTO"}
	itemEditListKeys = []string{"imageInfoDOList", "itemLabelExtList", "itemProperties", "itemSkuList",
		"propertyImageList", "userRightsProtocols"}
)

// buildItemStockEditPayload 校验请求与最新快照一致后，生成只改库存字段的全量覆盖载荷。
func buildItemStockEditPayload(detail *ItemEditDetail, itemID string, req ItemStockEditRequest) (map[string]any, error) {
	if detail.ItemID != itemID {
		return nil, ErrItemEditDetailUnusable
	}
	// rowQuantity 保存多规格时 Key 到新库存的映射。
	rowQuantity, err := validateItemStockAgainstDetail(detail, req)
	if err != nil {
		return nil, err
	}
	// payload 是回填后的载荷；先深拷贝避免修改快照原文。
	payload := map[string]any{}
	// key 是当前回填的字段名。
	for _, key := range append(append(append([]string{}, itemEditScalarKeys...), itemEditObjectKeys...), itemEditListKeys...) {
		// value 是快照中该字段的原值。
		value, exists := detail.raw[key]
		if !exists {
			continue
		}
		// cloned 是深拷贝后的字段值。
		cloned, cloneErr := cloneJSONValue(value)
		if cloneErr != nil {
			return nil, ErrItemEditDetailUnusable
		}
		payload[key] = cloned
	}
	payload["itemId"] = itemID
	payload["sourceId"] = itemID
	payload["uniqueCode"] = strconv.FormatInt(time.Now().UnixMicro(), 10)
	payload["bizcode"] = "pcMainPublish"
	payload["publishScene"] = "pcMainPublish"
	// exists 表示映射中是否存在该键。
	if _, exists := payload["itemTypeStr"]; !exists {
		payload["itemTypeStr"] = "b"
	}
	normalizeItemEditBooleans(payload)
	if !detail.MultiSpec {
		payload["quantity"] = typedLike(detail.raw["quantity"], req.Quantity)
		return payload, nil
	}
	// rows 是载荷里的规格行，逐行只改 quantity。
	rows, _ := payload["itemSkuList"].([]any)
	// row 是载荷里的当前规格行。
	for _, row := range rows {
		// fields 是规格行的对象形式。
		fields := mapFromAny(row)
		// view 是该规格行按同一规则算出的 Key。
		view := parseItemStockSKU(fields)
		fields["quantity"] = typedLike(fields["quantity"], rowQuantity[view.Key])
	}
	// exists 表示映射中是否存在该键。
	if _, exists := payload["quantity"]; !exists {
		payload["quantity"] = "1"
	}
	// exists 表示映射中是否存在该键。
	if _, exists := payload["itemProperties"]; !exists {
		payload["itemProperties"] = rebuildItemProperties(rows)
	}
	return payload, nil
}

// validateItemStockAgainstDetail 校验请求与最新快照：单/多规格一致、规格行集合完全一致、用户看到的库存未变化。
// 返回多规格时 Key 到新库存的映射；单规格返回 nil。
func validateItemStockAgainstDetail(detail *ItemEditDetail, req ItemStockEditRequest) (map[string]int, error) {
	if !detail.MultiSpec {
		if len(req.SKUs) > 0 {
			return nil, fmt.Errorf("%w: 该商品是单规格商品", ErrItemStockInvalid)
		}
		if req.ExpectedQuantity != detail.Quantity {
			return nil, ErrItemStockChanged
		}
		return nil, nil
	}
	if len(req.SKUs) == 0 {
		return nil, fmt.Errorf("%w: 该商品是多规格商品，需要提供全部规格行", ErrItemStockInvalid)
	}
	if len(req.SKUs) != len(detail.SKUs) {
		return nil, fmt.Errorf("%w: 规格行数量与商品不一致", ErrItemStockInvalid)
	}
	// current 保存快照中 Key 到当前库存的映射。
	current := make(map[string]int, len(detail.SKUs))
	// sku 是快照中的规格行。
	for _, sku := range detail.SKUs {
		current[sku.Key] = sku.Quantity
	}
	// result 保存 Key 到新库存的映射。
	result := make(map[string]int, len(req.SKUs))
	// row 是请求中的规格行。
	for _, row := range req.SKUs {
		// quantity、exists 是快照中该规格行的当前库存及其是否存在。
		quantity, exists := current[row.Key]
		if !exists {
			return nil, fmt.Errorf("%w: 未知规格行", ErrItemStockInvalid)
		}
		if row.ExpectedQuantity != quantity {
			return nil, ErrItemStockChanged
		}
		result[row.Key] = row.Quantity
	}
	return result, nil
}

// typedLike 按原值的 JSON 类型写入数字：原值是字符串（editDetail 的常见形态）就写字符串，否则写整数。
func typedLike(original any, value int) any {
	// isText 表示该值是否为字符串类型。
	if _, isText := original.(string); isText {
		return strconv.Itoa(value)
	}
	if original == nil {
		return strconv.Itoa(value)
	}
	return value
}

// normalizeItemEditBooleans 把 editDetail 中以字符串表示的布尔字段转为真正的布尔值，与 PC 页提交一致。
func normalizeItemEditBooleans(payload map[string]any) {
	// fee 是运费相关的字段值。
	if fee := mapFromAny(payload["itemPostFeeDTO"]); fee != nil {
		// key 是需要标准化的运费布尔字段名。
		for _, key := range []string{"canFreeShipping", "supportFreight", "onlyTakeSelf"} {
			// exists 表示映射中是否存在该键；value 是当前被检查或转换的字段值。
			if value, exists := fee[key]; exists {
				fee[key] = boolLike(value)
			}
		}
	}
	// protocols 是服务承诺列表，每项的 enable 需要是布尔值。
	protocols, _ := payload["userRightsProtocols"].([]any)
	// protocol 是当前服务承诺项。
	for _, protocol := range protocols {
		// fields 是对象的字段映射。
		if fields := mapFromAny(protocol); fields != nil {
			// exists 表示映射中是否存在该键；value 是当前被检查或转换的字段值。
			if value, exists := fields["enable"]; exists {
				fields["enable"] = boolLike(value)
			}
		}
	}
}

// boolLike 把布尔或字符串形式的布尔值规范为 bool，其他类型原样返回。
func boolLike(value any) any {
	// typed 是类型归一后的字段值。
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return value
	}
}

// rebuildItemProperties 在 editDetail 没有返回 itemProperties 时，按规格行的 propertyList 还原规格维度与规格值。
// 维度与规格值保持首次出现的顺序；规格图沿用 propertyImageList，这里不声明 supportImage。
func rebuildItemProperties(rows []any) []any {
	// names 保存规格维度名的首次出现顺序，values 保存每个维度下规格值的首次出现顺序。
	var names []string
	// values 是维度名到规格值列表的映射，seen 用于去重。
	values := map[string][]string{}
	// seen 保存已出现的标识，用于去重。
	seen := map[string]map[string]struct{}{}
	// row 是当前规格行。
	for _, row := range rows {
		// properties 是该规格行的规格项列表。
		properties, _ := mapFromAny(row)["propertyList"].([]any)
		// property 是当前规格项。
		for _, property := range properties {
			// fields 是规格项的对象形式。
			fields := mapFromAny(property)
			// name、value 是规格名与规格值。
			name := strings.TrimSpace(mtopString(fields["propertyText"]))
			// value 是当前被检查或转换的字段值。
			value := strings.TrimSpace(mtopString(fields["valueText"]))
			if name == "" || value == "" {
				continue
			}
			if seen[name] == nil {
				seen[name] = map[string]struct{}{}
				names = append(names, name)
			}
			// exists 表示映射中是否存在该键。
			if _, exists := seen[name][value]; !exists {
				seen[name][value] = struct{}{}
				values[name] = append(values[name], value)
			}
		}
	}
	// out 是还原后的 itemProperties。
	out := make([]any, 0, len(names))
	// name 是当前规格维度名。
	for _, name := range names {
		// propertyValues 是该维度下的规格值对象列表。
		propertyValues := make([]any, 0, len(values[name]))
		// value 是该维度下的规格值。
		for _, value := range values[name] {
			propertyValues = append(propertyValues, map[string]any{"propertyValue": value})
		}
		out = append(out, map[string]any{"propertyName": name, "supportImage": false, "propertyValues": propertyValues})
	}
	return out
}

// cloneJSONValue 通过 JSON 往返深拷贝一个值，避免回填时改动快照原文。
func cloneJSONValue(value any) (any, error) {
	// raw 是序列化后的字节。
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	// out 是反序列化得到的独立副本。
	var out any
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
