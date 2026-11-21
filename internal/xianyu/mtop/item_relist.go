package mtop

import (
	"context"
	"strings"
)

// itemStatusOnSale 是 editDetail/edit 中 itemStatus 表示“在售”的取值；下架为 "-2"。
const itemStatusOnSale = "0"

// ItemRelistResult 是一次“售出后重新上架”调用的结果。
type ItemRelistResult struct {
	// ItemID 是被处理的商品 ID。
	ItemID string
	// AlreadyOnSale 表示读取详情时商品已在售且有库存，未提交 edit。
	AlreadyOnSale bool
	// RestoredQuantity 表示库存为 0 的商品被补回了最小库存（单规格为 1，多规格为各规格行 1）。
	RestoredQuantity bool
	// UpdatedCookies 是请求累计更新后的 Cookie；为空表示没有变化。
	UpdatedCookies string
}

// RelistItem 以最新 editDetail 为底，把商品状态改回在售并全量提交。固定最多两次平台请求（读取 + 提交），不重试。
// 商品已在售且有库存时只读取、不提交；类目不支持网页端编辑时返回 ErrItemEditUnsupported 且不修改任何数据。
// 售出后库存为 0 的商品会补回最小库存（单规格 1；多规格全部规格行为 0 时每行 1），否则平台会拒绝上架。
func (c *ClientImpl) RelistItem(ctx context.Context, cookiesStr, itemID string) (*ItemRelistResult, error) {
	// id 是规范化后的商品 ID。
	id := strings.TrimSpace(itemID)
	if id == "" {
		return nil, ErrItemStockInvalid
	}
	// detail、err 保存提交前读取的最新快照。
	detail, err := c.FetchItemEditDetail(ctx, cookiesStr, id)
	if err != nil {
		return nil, err
	}
	if detail.ItemID != id {
		return nil, ErrItemEditDetailUnusable
	}
	// result 是调用结果；读取阶段拿到的 Cookie 更新始终带回。
	result := &ItemRelistResult{ItemID: id, UpdatedCookies: detail.UpdatedCookies}
	if strings.TrimSpace(detail.Status) == itemStatusOnSale && detail.Quantity > 0 {
		result.AlreadyOnSale = true
		return result, nil
	}
	// payload、restored 是改回在售后的全量覆盖载荷及是否补回了库存。
	payload, restored, err := buildItemRelistPayload(detail, id)
	if err != nil {
		return nil, err
	}
	// _、_、cookies、err 忽略 ret 文本与字段名，只保留累计更新后的 Cookie。
	_, _, cookies, err := c.submitItemEdit(ctx, cookiesStr, detail, id, payload)
	if err != nil {
		return nil, err
	}
	result.RestoredQuantity = restored
	result.UpdatedCookies = cookies
	return result, nil
}

// buildItemRelistPayload 在快照全量载荷上把 itemStatus 改为在售，并在库存为 0 时补回最小库存。
func buildItemRelistPayload(detail *ItemEditDetail, itemID string) (map[string]any, bool, error) {
	if detail.ItemID != itemID {
		return nil, false, ErrItemEditDetailUnusable
	}
	// payload 是深拷贝后的全量载荷。
	payload, err := cloneItemEditPayload(detail, itemID)
	if err != nil {
		return nil, false, err
	}
	payload["itemStatus"] = itemStatusOnSale
	// restored 表示是否补回了库存。
	restored := false
	if !detail.MultiSpec {
		if detail.Quantity <= 0 {
			payload["quantity"] = typedLike(detail.raw["quantity"], 1)
			restored = true
		}
		return payload, restored, nil
	}
	// rows 是载荷里的规格行。
	rows, _ := payload["itemSkuList"].([]any)
	if detail.Quantity <= 0 {
		// row 是载荷里的当前规格行，全部为 0 时逐行补 1。
		for _, row := range rows {
			// fields 是规格行的对象形式，可直接修改其库存字段。
			fields := mapFromAny(row)
			fields["quantity"] = typedLike(fields["quantity"], 1)
		}
		restored = true
	}
	// exists 是载荷中是否已存在该字段。
	if _, exists := payload["quantity"]; !exists {
		payload["quantity"] = "1"
	}
	// exists 是载荷中是否已存在该字段。
	if _, exists := payload["itemProperties"]; !exists {
		payload["itemProperties"] = rebuildItemProperties(rows)
	}
	return payload, restored, nil
}

// ItemRelistClient 是商品重新上架的可选扩展接口，避免扩大所有 MTOP 客户端替身的基础契约。
type ItemRelistClient interface {
	// RelistItem 以最新详情为底把商品改回在售，固定最多两次平台请求。
	RelistItem(ctx context.Context, cookiesStr, itemID string) (*ItemRelistResult, error)
}

// 确保真实客户端实现重新上架扩展接口。
var _ ItemRelistClient = (*ClientImpl)(nil)
