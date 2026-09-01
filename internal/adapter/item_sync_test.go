package adapter

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/mtop"
)

// itemSyncDetailClient 是商品详情探测测试使用的平台客户端替身。
type itemSyncDetailClient struct {
	// Client 提供商品同步未涉及的平台客户端默认行为。
	mtop.Client
	// detect 保存测试控制的多规格探测逻辑。
	detect func(context.Context, string, string) (bool, error)
}

// DetectItemMultiSpec 执行测试注入的商品多规格探测逻辑。
func (client *itemSyncDetailClient) DetectItemMultiSpec(ctx context.Context, cookies, itemID string) (bool, error) {
	return client.detect(ctx, cookies, itemID)
}

// TestItemSyncRepositoryEnrichMultiSpecBoundsConcurrency 验证每次同步重新探测且限制详情探测并发。
func TestItemSyncRepositoryEnrichMultiSpecBoundsConcurrency(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// stateMu 保护远端探测并发统计。
	var stateMu sync.Mutex
	// active、maxActive、probeCalls 保存当前并发数、峰值并发数和探测次数。
	active, maxActive, probeCalls := 0, 0, 0
	// client 是带并发统计的商品详情探测替身。
	client := &itemSyncDetailClient{detect: func(_ context.Context, cookies, itemID string) (bool, error) {
		if cookies == "" || itemID == "" {
			t.Fatalf("探测参数缺失：cookies=%q itemID=%q", cookies, itemID)
		}
		stateMu.Lock()
		probeCalls++
		active++
		if active > maxActive {
			maxActive = active
		}
		stateMu.Unlock()
		time.Sleep(20 * time.Millisecond)
		stateMu.Lock()
		active--
		stateMu.Unlock()
		return true, nil
	}}
	// repository 是使用测试数据库和平台替身的商品同步适配器。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, nil, nil, nil)
	// items 保存等待探测的商品列表。
	items := make([]mtop.ItemListItem, 8)
	// index 表示当前商品在测试列表中的下标。
	for index := range items {
		items[index].ID = fmt.Sprintf("probe-%d", index)
	}
	// err 保存首次批量多规格探测的错误。
	if err := repository.enrichMultiSpec(context.Background(), "unb=1; _m_h5_tk=t_1;", "cid", items); err != nil {
		t.Fatalf("首次多规格探测失败：%v", err)
	}
	if maxActive > 4 {
		t.Fatalf("探测并发=%d，超过上限 4", maxActive)
	}
	// index、item 分别表示商品下标和探测结果。
	for index, item := range items {
		if !item.IsMultiSpec {
			t.Fatalf("商品 %d 未标记为多规格", index)
		}
	}
	// secondItems 保存第二次调用使用的商品列表，验证同步不会复用上一次多规格结果。
	secondItems := make([]mtop.ItemListItem, len(items))
	// index 表示第二次探测商品的下标。
	for index := range secondItems {
		secondItems[index].ID = fmt.Sprintf("probe-%d", index)
	}
	// err 保存第二次多规格探测的校验错误。
	if err := repository.enrichMultiSpec(context.Background(), "unb=1; _m_h5_tk=t_1;", "cid", secondItems); err != nil {
		t.Fatalf("第二次多规格探测失败：%v", err)
	}
	if probeCalls != len(items)*2 {
		t.Fatalf("第二次同步未重新探测，探测次数=%d，期望=%d", probeCalls, len(items)*2)
	}
}

// TestItemSyncRepositoryEnrichMultiSpecFollowsRemoteBothDirections 验证远端规格变化可双向更新本次同步结果。
func TestItemSyncRepositoryEnrichMultiSpecFollowsRemoteBothDirections(t *testing.T) {
	// store、cleanup 保存详情探测适配器使用的隔离数据库和清理责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// remoteValue 表示本次模拟的远端商品规格状态，可在两次同步之间切换。
	remoteValue := true
	// client 是按当前远端状态返回详情探测结果的平台替身。
	client := &itemSyncDetailClient{detect: func(_ context.Context, _ string, _ string) (bool, error) {
		return remoteValue, nil
	}}
	// repository 是使用详情探测替身的商品同步适配器。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, nil, nil, nil)
	// items 保存第一次同步前仍带有旧多规格标记的商品。
	items := []mtop.ItemListItem{{ID: "changing-item", IsMultiSpec: true}}
	remoteValue = false
	// err 保存多规格转单规格的详情探测错误。
	if err := repository.enrichMultiSpec(context.Background(), "unb=1; _m_h5_tk=t_1;", "cid", items); err != nil {
		t.Fatalf("多规格转单规格探测失败：%v", err)
	}
	if items[0].IsMultiSpec {
		t.Fatal("远端已变为单规格，但同步结果仍保留旧多规格标记")
	}
	// secondItems 保存第二次同步前带有旧单规格标记的同一商品。
	secondItems := []mtop.ItemListItem{{ID: "changing-item", IsMultiSpec: false}}
	remoteValue = true
	// err 保存单规格转多规格的详情探测错误。
	if err := repository.enrichMultiSpec(context.Background(), "unb=1; _m_h5_tk=t_1;", "cid", secondItems); err != nil {
		t.Fatalf("单规格转多规格探测失败：%v", err)
	}
	if !secondItems[0].IsMultiSpec {
		t.Fatal("远端已变为多规格，但同步结果仍保留旧单规格标记")
	}
}
