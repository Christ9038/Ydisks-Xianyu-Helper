package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrSoldRecoveryUnsafe 表示订单存在不可安全迁移的历史动作或身份冲突，调用方应保留原数据并展示失败项。
var ErrSoldRecoveryUnsafe = errors.New("订单归属冲突，暂不能安全恢复")

// RefreshOwnership 是同步专用的非敏感归属快照，包含软删除记录但不包含地址、凭证或消息正文。
type RefreshOwnership struct {
	// OrderID 是平台订单标识。
	OrderID string
	// CookieID 是旧卖家归属键，不保证等于平台用户标识。
	CookieID string
	// ItemID 是本地已记录的商品标识，用于和平台卖家列表交叉核验。
	ItemID string
	// BuyerID 是历史记录的买家身份，不能以消息发送者替代。
	BuyerID string
	// Version 是提交修复时必须匹配的并发版本。
	Version int
	// Deleted 表示订单已软删除，恢复后不能再计作新订单。
	Deleted bool
	// Owned 表示旧归属账号属于当前管理用户；为假时其余旧身份必须隐藏。
	Owned bool
}

// finishOrderRefresh 根据 summary 的真实结果生成提示；results 保留逐项原因，successMessage 仅在全部成功时使用。
// 返回值维持既有任务契约，同时明确区分恢复、纠错和新增，不产生持久化或外部动作。
func finishOrderRefresh(summary RefreshSummary, results []RefreshOrderResult, successMessage string) RefreshResult {
	if summary.Failed > 0 {
		successMessage = fmt.Sprintf("订单同步未全部完成，发现 %d 个新订单，%d 项失败；请查看失败明细", summary.Discovered, summary.Failed)
	}
	if summary.Restored > 0 || summary.Reassigned > 0 {
		successMessage += fmt.Sprintf("；恢复 %d 个已删除订单，修正 %d 个历史错误归属", summary.Restored, summary.Reassigned)
	}
	return RefreshResult{PartialFailure: summary.Failed > 0, Message: successMessage, Summary: summary, Results: results}
}

// soldRecoveryOptions 把 remote 的平台事实转换为 cookieID 的写入选项；不携带旧账号会话或发货状态。
// 返回值只包含可信已售列表字段；unknown 按未提供状态处理以保留旧状态，其他金额和状态继续由现有写入边界校验。
func soldRecoveryOptions(cookieID string, remote RefreshSoldOrder) UpsertOptions {
	// options 保存平台事实；空字段由数据库按既有规则合并，禁止复制其他账号的地址和聊天上下文。
	options := UpsertOptions{CookieID: cookieID, ItemID: remote.ItemID, BuyerID: remote.BuyerID,
		CreatedAt: remote.CreatedAt, OrderStatus: remote.OrderStatus, Quantity: remote.Quantity, Amount: remote.Amount,
		ReceiverName: remote.ReceiverName, ReceiverPhone: remote.ReceiverPhone, ReceiverAddress: remote.ReceiverAddr, ReceiverCity: remote.ReceiverCity}
	if options.OrderStatus == "unknown" {
		options.OrderStatus = ""
	}
	if remote.IsBargain {
		// bargain 保存平台确认的砍价标记，指针在本次调用结束后由 options 持有。
		bargain := true
		options.IsBargain = &bargain
	}
	return options
}

// canRecoverSoldOwnership 对同用户历史买家错绑执行保守交叉核验；sellerID 必须来自实际已售请求会话。
// cookieID 是目标本地账号键，old 是授权读取的旧归属，remote 是该会话返回的订单；未知或矛盾身份一律不迁移。
func canRecoverSoldOwnership(cookieID, sellerID string, old RefreshOwnership, remote RefreshSoldOrder) bool {
	return old.Owned && old.CookieID != "" && old.CookieID != cookieID &&
		sellerID != "" && sellerID != remote.BuyerID &&
		remote.BuyerID != "" && old.BuyerID == remote.BuyerID && old.CookieID == remote.BuyerID &&
		remote.ItemID != "" && old.ItemID == remote.ItemID && old.OrderID == remote.OrderID
}

// persistSoldSnapshot 使用 userID 的权限对完整 remoteOrders 逐单核对归属，cookieID 为目标账号，sellerID 为请求身份。
// 返回已提交新增/更新统计、恢复明细、完整远端订单集合及错误；业务冲突隔离，任何失败阻止调用方执行缺失软删除。
func (s *RefreshService) persistSoldSnapshot(ctx context.Context, userID int64, cookieID, sellerID string, remoteOrders []RefreshSoldOrder) (int, int, refreshDiscoveryResult, map[string]struct{}, error) {
	// result 保存已提交恢复明细；activeIDs 只表示平台快照中的订单，不能把失败订单误判为平台缺失。
	result := refreshDiscoveryResult{NewOrderIDs: make(map[string]struct{})}
	// activeIDs 兼作去重集合，确保同单重复出现不会重复修复或计数。
	activeIDs := make(map[string]struct{}, len(remoteOrders))
	// normal 保存无需迁移的候选批次；归属核对通过后才允许读取其完整本地字段。
	normal := make([]RefreshSoldOrder, 0, len(remoteOrders))
	// blocked 表示至少一笔业务冲突无法自动恢复，其他正常订单仍可提交。
	blocked := false
	// remote 是当前平台订单事实，不读取其他账号凭证或请求旧买家重新登录。
	for _, remote := range remoteOrders {
		remote.OrderID = strings.TrimSpace(remote.OrderID)
		if remote.OrderID == "" {
			return 0, 0, result, activeIDs, errors.New("已售订单缺少订单号，停止缺失订单清理")
		}
		// exists 表示该订单已处理，避免同单重复恢复。
		if _, exists := activeIDs[remote.OrderID]; exists {
			continue
		}
		activeIDs[remote.OrderID] = struct{}{}
		// ownership、readErr 保存含软删除记录的最小归属快照；跨用户查询不返回旧身份。
		ownership, readErr := s.repository.FindOrderOwnership(ctx, userID, remote.OrderID)
		if errors.Is(readErr, ErrNotFound) {
			normal = append(normal, remote)
			continue
		}
		if readErr != nil {
			return 0, 0, result, activeIDs, fmt.Errorf("核对订单归属失败: %w", readErr)
		}
		if ownership.Owned && ownership.CookieID == cookieID && !ownership.Deleted {
			normal = append(normal, remote)
			continue
		}
		// row 保存本笔可安全公开的结果，仅携带当前目标账号和平台已返回的订单号。
		row := RefreshOrderResult{CookieID: cookieID, OrderID: remote.OrderID, Stage: "recover"}
		// writeErr 保存恢复结果；不能证明归属时保留旧行并隔离失败。
		writeErr := ErrSoldRecoveryUnsafe
		if ownership.Owned && ownership.CookieID == cookieID {
			writeErr = s.repository.UpsertOrder(ctx, remote.OrderID, soldRecoveryOptions(cookieID, remote))
			if writeErr == nil {
				result.Restored++
				row.Message = "已恢复本账号的软删除订单"
			}
		} else if canRecoverSoldOwnership(cookieID, sellerID, ownership, remote) {
			writeErr = s.repository.RecoverSoldOwnership(ctx, userID, cookieID, ownership, soldRecoveryOptions(cookieID, remote))
			if writeErr == nil {
				result.Reassigned++
				row.Message = "已核验并恢复历史错误归属订单"
			}
		}
		row.Success = writeErr == nil
		if writeErr != nil {
			// knownConflict 仅把已知业务拒绝隔离；数据库不可用或取消必须中断，不能伪装成普通归属冲突。
			knownConflict := errors.Is(writeErr, ErrSoldRecoveryUnsafe) || errors.Is(writeErr, ErrForbidden)
			if !knownConflict {
				return 0, 0, result, activeIDs, fmt.Errorf("恢复订单失败: %w", writeErr)
			}
			blocked = true
			row.Error = "订单归属待核验，或存在历史自动化动作，未自动迁移；其他订单继续同步"
		}
		result.Results = append(result.Results, row)
	}
	// discovered、updated、newIDs、writeErr 保存普通订单批次提交结果；忽略批次集合，使用完整 activeIDs，且只累计已提交新增。
	discovered, updated, newIDs, _, writeErr := s.persistSoldOrders(ctx, cookieID, normal)
	result.NewOrderIDs = newIDs
	if errors.Is(writeErr, ErrForbidden) {
		// batchErr 表示归属在预检后被并发占用；整批已回滚，改用有界逐单提交以隔离该竞争。
		var batchErr error
		discovered, updated, batchErr = s.persistSoldAfterConflict(ctx, cookieID, normal, &result)
		if batchErr != nil {
			return discovered, updated, result, activeIDs, batchErr
		}
		writeErr = nil
	}
	if writeErr != nil {
		return discovered, updated, result, activeIDs, writeErr
	}
	if blocked {
		return discovered, updated, result, activeIDs, ErrSoldRecoveryUnsafe
	}
	return discovered, updated, result, activeIDs, nil
}

// persistSoldAfterConflict 在已回滚的普通批次发生归属竞争时逐单重试一次，绝不绕过通用写入保护。
// ctx 控制取消，cookieID 限制账号，normal 是原批次，result 接收已提交订单和明确失败项；返回提交数量及最终错误。
func (s *RefreshService) persistSoldAfterConflict(ctx context.Context, cookieID string, normal []RefreshSoldOrder, result *refreshDiscoveryResult) (int, int, error) {
	// discovered、updated 只累计实际单笔事务提交的订单；blocked 表示仍有竞争订单需要下次重试。
	discovered, updated, blocked := 0, 0, false
	// remote 是本次仅重试一次的订单，持续冲突不会无限循环。
	for _, remote := range normal {
		// added、changed、newIDs、err 保存当前单笔事务结果。
		added, changed, newIDs, _, err := s.persistSoldOrders(ctx, cookieID, []RefreshSoldOrder{remote})
		if errors.Is(err, ErrForbidden) {
			blocked = true
			result.Results = append(result.Results, RefreshOrderResult{CookieID: cookieID, OrderID: remote.OrderID, Stage: "recover", Error: "订单归属发生并发变化，请重试同步"})
			continue
		}
		if err != nil {
			return discovered, updated, err
		}
		discovered += added
		updated += changed
		// orderID 是单笔事务已经提交的新订单标识。
		for orderID := range newIDs {
			result.NewOrderIDs[orderID] = struct{}{}
		}
	}
	if blocked {
		return discovered, updated, ErrSoldRecoveryUnsafe
	}
	return discovered, updated, nil
}
