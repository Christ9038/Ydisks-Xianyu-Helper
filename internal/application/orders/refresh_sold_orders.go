package orders

import (
	"context"
	"fmt"
	"strings"
)

// chatConversationRefresher 定义订单同步按需刷新聊天联系人的可选运行时能力。
// 刷新失败只影响会话补关联，不应阻断订单自身落库；调用方据此决定是否重查本地候选。
type chatConversationRefresher interface {
	// RefreshChatConversations 刷新指定账号的联系人缓存。
	RefreshChatConversations(context.Context, string) error
}

// persistSoldOrders 将平台订单列表写入数据库并统计变化；只查询本地会话缓存，不在提交临界区发起平台请求。
func (s *RefreshService) persistSoldOrders(ctx context.Context, cookieID string, remoteOrders []RefreshSoldOrder) (int, int, map[string]struct{}, map[string]struct{}, error) {
	// discovered、updated 保存新增和变化订单数量。
	discovered, updated := 0, 0
	// newOrderIDs、remoteOrderIDs 保存新增和远端订单标识集合。
	newOrderIDs := make(map[string]struct{})
	// remoteOrderIDs 保存远端订单标识集合。
	remoteOrderIDs := make(map[string]struct{})
	// normalizedRemoteOrders 保存去重并完成金额归一化的平台订单。
	normalizedRemoteOrders := make([]RefreshSoldOrder, 0, len(remoteOrders))
	// seenRemoteIDs 保存已经处理的平台订单标识。
	seenRemoteIDs := make(map[string]struct{}, len(remoteOrders))
	// remote 是当前平台订单列表项。
	for _, remote := range remoteOrders {
		remote.OrderID = strings.TrimSpace(remote.OrderID)
		if remote.OrderID == "" {
			continue
		}
		// exists 表示当前远端订单是否已经在本批次出现。
		if _, exists := seenRemoteIDs[remote.OrderID]; exists {
			continue
		}
		seenRemoteIDs[remote.OrderID] = struct{}{}
		remoteOrderIDs[remote.OrderID] = struct{}{}
		// normalizedAmount、ok 保存金额归一化结果。
		normalizedAmount, ok := NormalizeOrderAmount(remote.Amount)
		if ok {
			remote.Amount = normalizedAmount
		}
		normalizedRemoteOrders = append(normalizedRemoteOrders, remote)
	}
	if len(normalizedRemoteOrders) == 0 {
		return discovered, updated, newOrderIDs, remoteOrderIDs, nil
	}
	// remoteIDs 保存批量读取本地订单的标识集合。
	remoteIDs := make([]string, 0, len(normalizedRemoteOrders))
	// remote 是当前已归一化的平台订单。
	for _, remote := range normalizedRemoteOrders {
		remoteIDs = append(remoteIDs, remote.OrderID)
	}
	// existingOrders、findErr 保存批量读取的本地订单及错误。
	existingOrders, findErr := s.repository.FindOrdersByIDs(ctx, cookieID, remoteIDs)
	if findErr != nil {
		return discovered, updated, newOrderIDs, remoteOrderIDs, fmt.Errorf("批量读取订单失败: %w", findErr)
	}
	// batchRows 保存订单发现阶段待一次性写入的订单。
	batchRows := make([]RefreshOrderWrite, 0, len(normalizedRemoteOrders))
	// remote 是当前待比较并写入的平台订单。
	for _, remote := range normalizedRemoteOrders {
		// existing、exists 保存当前订单的本地实体及存在标记。
		existing, exists := existingOrders[remote.OrderID]
		if exists && remote.CreatedAt == "" {
			// 缺少平台时间时沿用已有订单创建时间，避免详情补全把它改成同步时间。
			remote.CreatedAt = existing.CreatedAt
		}
		// changed 表示远端订单字段是否发生变化。
		changed := !exists || refreshSoldOrderChanged(existing, remote)
		// status 保存待写入的订单状态。
		status := remote.OrderStatus
		if exists && status == "unknown" {
			status = existing.OrderStatus
		}
		// bargain 保存砍价订单标记指针。
		var bargain *bool
		if remote.IsBargain {
			// value 保存砍价订单标记值。
			value := true
			bargain = &value
		}
		// chatID 保存按账号、买家和商品唯一匹配出的会话；无法唯一确认时保持空值，避免串发给错误买家。
		chatID := ""
		if strings.TrimSpace(remote.BuyerID) != "" && strings.TrimSpace(remote.ItemID) != "" {
			// chatIDs、chatErr 保存当前订单候选会话及查询错误；查询失败必须阻止本批写入。
			chatIDs, chatErr := s.repository.FindChatIDsByBuyerAndItem(ctx, cookieID, remote.BuyerID, remote.ItemID)
			if chatErr != nil {
				return discovered, updated, newOrderIDs, remoteOrderIDs, fmt.Errorf("匹配订单聊天会话失败: %w", chatErr)
			}

			if len(chatIDs) == 1 {
				chatID = chatIDs[0]
			}
		}
		batchRows = append(batchRows, RefreshOrderWrite{OrderID: remote.OrderID, Options: UpsertOptions{ItemID: remote.ItemID, BuyerID: remote.BuyerID, CookieID: cookieID, CreatedAt: remote.CreatedAt, OrderStatus: status, Quantity: remote.Quantity, Amount: remote.Amount, ReceiverName: remote.ReceiverName, ReceiverPhone: remote.ReceiverPhone, ReceiverAddress: remote.ReceiverAddr, ReceiverCity: remote.ReceiverCity, ChatID: chatID, IsBargain: bargain}})
		if !exists {
			discovered++
			newOrderIDs[remote.OrderID] = struct{}{}
		} else if changed {
			updated++
		}
	}
	// err 保存订单发现批量写入错误。
	if err := s.repository.BatchUpsertOrders(ctx, batchRows); err != nil {
		return 0, 0, make(map[string]struct{}), remoteOrderIDs, fmt.Errorf("批量保存订单失败: %w", err)
	}
	return discovered, updated, newOrderIDs, remoteOrderIDs, nil
}

// prepareSoldChats 为 cookieID 的 remoteOrders 按需补齐联系人缓存，ctx 控制平台请求取消。
// 调用方必须在凭证锁外执行；最多刷新一次，平台失败只影响关联，数据库查询或取消错误阻止本轮提交。
func (s *RefreshService) prepareSoldChats(ctx context.Context, cookieID string, remoteOrders []RefreshSoldOrder) error {
	// refresher、supported 判断可选平台联系人能力，未装配时仅在提交阶段查询本地缓存。
	refresher, supported := s.runtime.(chatConversationRefresher)
	if !supported {
		return nil
	}
	// remote 是待关联的卖家订单；身份不足时不猜测聊天会话。
	for _, remote := range remoteOrders {
		if strings.TrimSpace(remote.BuyerID) == "" || strings.TrimSpace(remote.ItemID) == "" {
			continue
		}
		// chatIDs、err 是本地已有候选会话及查询错误，查询失败不可被误当作空缓存。
		chatIDs, err := s.repository.FindChatIDsByBuyerAndItem(ctx, cookieID, remote.BuyerID, remote.ItemID)
		if err != nil {
			return fmt.Errorf("匹配订单聊天会话失败: %w", err)
		}
		if len(chatIDs) == 0 {
			// 可选平台刷新失败不会阻止订单落库；即使部分分页失败，提交阶段仍重新查询已落库的可信会话。
			_ = refresher.RefreshChatConversations(ctx, cookieID)
			return ctx.Err()
		}
	}
	return ctx.Err()
}
