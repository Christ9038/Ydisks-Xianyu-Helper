package orders

import "testing"

// TestRefreshModelPureBranches 验证订单变化判断、稳定状态识别和目标分片的边界分支。
func TestRefreshModelPureBranches(t *testing.T) {
	// remote 是用于逐字段比较的平台订单快照。
	remote := RefreshSoldOrder{OrderStatus: "shipped", CreatedAt: "2026-08-01", ItemID: "item", BuyerID: "buyer", Quantity: "2", Amount: "10", ReceiverName: "买家", ReceiverPhone: "138", ReceiverAddr: "地址", ReceiverCity: "城市", IsBargain: true}
	// existing 是与平台快照完全一致的本地订单。
	existing := &Order{OrderStatus: "shipped", CreatedAt: remote.CreatedAt, ItemID: remote.ItemID, BuyerID: remote.BuyerID, Quantity: remote.Quantity, Amount: remote.Amount, ReceiverName: remote.ReceiverName, ReceiverPhone: remote.ReceiverPhone, ReceiverAddress: remote.ReceiverAddr, ReceiverCity: remote.ReceiverCity, IsBargain: 1}
	if refreshSoldOrderChanged(existing, remote) {
		t.Fatal("相同订单不应判定为变化")
	}
	// changedFields 保存每个单字段变化分支的远端快照。
	changedFields := []RefreshSoldOrder{
		{OrderStatus: "paid"}, {CreatedAt: "new"}, {ItemID: "new"}, {BuyerID: "new"}, {Quantity: "3"}, {Amount: "11"},
		{ReceiverName: "新买家"}, {ReceiverPhone: "139"}, {ReceiverAddr: "新地址"}, {ReceiverCity: "新城市"},
	}
	// changedRemote 表示当前字段变化样例。
	for _, changedRemote := range changedFields {
		if !refreshSoldOrderChanged(existing, changedRemote) {
			t.Fatalf("字段变化未识别: %+v", changedRemote)
		}
	}
	// unknownStatus 是平台明确表示未知状态的快照，不应单独触发状态变化。
	unknownStatus := RefreshSoldOrder{OrderStatus: "unknown"}
	if refreshSoldOrderChanged(existing, unknownStatus) {
		t.Fatal("unknown 状态不应单独判定变化")
	}
	if !refreshSoldOrderChanged(nil, RefreshSoldOrder{}) {
		t.Fatal("缺失本地订单应判定为变化")
	}
	// bargainExisting 保存尚未标记议价的本地订单。
	bargainExisting := *existing
	bargainExisting.IsBargain = 0
	if !refreshSoldOrderChanged(&bargainExisting, RefreshSoldOrder{IsBargain: true}) {
		t.Fatal("议价标记变化未识别")
	}
	// stableStatuses 保存无需重复详情抓取的稳定状态。
	stableStatuses := []string{"shipped", "completed", "cancelled"}
	// stableStatus 表示当前稳定状态。
	for _, stableStatus := range stableStatuses {
		if !isStableRefreshStatus(stableStatus) {
			t.Fatalf("稳定状态未识别: %q", stableStatus)
		}
	}
	if isStableRefreshStatus("pending") {
		t.Fatal("pending 不应视为稳定状态")
	}
	// targets 保存需要按默认大小切分的详情目标。
	targets := []refreshTarget{{OrderID: "1"}, {OrderID: "2"}, {OrderID: "3"}}
	// chunks 保存默认大小切分结果。
	chunks := splitRefreshTargets(targets, 0)
	if len(chunks) != 1 || len(chunks[0]) != 3 {
		t.Fatalf("默认分片错误: %+v", chunks)
	}
	// smallChunks 保存指定小分片大小的结果。
	smallChunks := splitRefreshTargets(targets, 2)
	if len(smallChunks) != 2 || len(smallChunks[0]) != 2 || len(smallChunks[1]) != 1 {
		t.Fatalf("指定分片错误: %+v", smallChunks)
	}
}

// TestSameRefreshOrderTimeNormalizesDatabaseAndPlatformFormats 验证数据库无时区文本与平台 RFC3339 时间不会造成虚假更新。
func TestSameRefreshOrderTimeNormalizesDatabaseAndPlatformFormats(t *testing.T) {
	// cases 覆盖等价时间、不同时间和非法文本，避免统计逻辑依赖字符串外观。
	cases := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "utc database text", left: "2026-08-01 12:00:00", right: "2026-08-01T12:00:00Z", want: true},
		{name: "offset text", left: "2026-08-01 20:00:00+08:00", right: "2026-08-01T12:00:00Z", want: true},
		{name: "date text", left: "2026-08-01", right: "2026-08-01T00:00:00Z", want: true},
		{name: "different", left: "2026-08-01 12:00:00", right: "2026-08-01T12:00:01Z", want: false},
		{name: "invalid", left: "not-a-time", right: "2026-08-01T12:00:00Z", want: false},
	}
	for /* tc 表示当前时间格式归一化测试场景。 */ _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 表示当前两种时间文本是否代表同一时刻。
			if got := sameRefreshOrderTime(tc.left, tc.right); got != tc.want {
				t.Fatalf("sameRefreshOrderTime(%q,%q)=%v want %v", tc.left, tc.right, got, tc.want)
			}
		})
	}
}
