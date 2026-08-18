package orders

// refreshWrite 保存详情分片中等待事务写入的单条订单。
type refreshWrite struct {
	// OrderID 是待更新订单标识。
	OrderID string
	// CurrentStatus 是刷新前订单状态。
	CurrentStatus string
	// NewStatus 是刷新后订单状态。
	NewStatus string
	// Options 是订单写入字段。
	Options UpsertOptions
	// CookieUpdate 是本次详情请求观察到的 Cookie 更新。
	CookieUpdate RefreshCookieUpdate
}

// refreshTarget 保存待补全详情的订单目标。
type refreshTarget struct {
	// OrderID 是待刷新订单标识。
	OrderID string
	// CurrentStatus 是本地当前订单状态。
	CurrentStatus string
}

// refreshSoldOrderChanged 判断平台订单字段是否发生业务变化。
func refreshSoldOrderChanged(existing *Order, remote RefreshSoldOrder) bool {
	if existing == nil {
		return true
	}
	return (remote.OrderStatus != "" && remote.OrderStatus != "unknown" && NormalizeOrderStatus(existing.OrderStatus) != remote.OrderStatus) ||
		(remote.ItemID != "" && existing.ItemID != remote.ItemID) ||
		(remote.BuyerID != "" && existing.BuyerID != remote.BuyerID) ||
		(remote.Quantity != "" && existing.Quantity != remote.Quantity) ||
		(remote.Amount != "" && existing.Amount != remote.Amount) ||
		(remote.ReceiverName != "" && existing.ReceiverName != remote.ReceiverName) ||
		(remote.ReceiverPhone != "" && existing.ReceiverPhone != remote.ReceiverPhone) ||
		(remote.ReceiverAddr != "" && existing.ReceiverAddress != remote.ReceiverAddr) ||
		(remote.ReceiverCity != "" && existing.ReceiverCity != remote.ReceiverCity) ||
		(remote.IsBargain && existing.IsBargain == 0)
}

// isStableRefreshStatus 判断订单是否处于无需重复详情抓取的稳定状态。
func isStableRefreshStatus(status string) bool {
	switch status {
	case "shipped", "completed", "cancelled":
		return true
	default:
		return false
	}
}

// splitRefreshTargets 按固定大小切分订单刷新目标。
func splitRefreshTargets(targets []refreshTarget, size int) [][]refreshTarget {
	if size <= 0 {
		size = 100
	}
	// chunks 保存切分后的详情目标分片。
	chunks := make([][]refreshTarget, 0, (len(targets)+size-1)/size)
	// start 是当前分片起始下标。
	for start := 0; start < len(targets); start += size {
		// end 是当前分片结束下标。
		end := start + size
		if end > len(targets) {
			end = len(targets)
		}
		chunks = append(chunks, targets[start:end])
	}
	return chunks
}
