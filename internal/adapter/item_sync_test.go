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

// TestItemSyncRepositoryEnrichMultiSpecBoundsConcurrency 验证同步适配器限制探测并发并复用缓存。
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
	// cachedItems 保存第二次调用使用的商品列表，验证适配器缓存命中。
	cachedItems := make([]mtop.ItemListItem, len(items))
	// index 表示缓存测试商品下标。
	for index := range cachedItems {
		cachedItems[index].ID = fmt.Sprintf("probe-%d", index)
	}
	// err 保存第二次多规格探测的缓存校验错误。
	if err := repository.enrichMultiSpec(context.Background(), "unb=1; _m_h5_tk=t_1;", "cid", cachedItems); err != nil {
		t.Fatalf("缓存多规格探测失败：%v", err)
	}
	if probeCalls != len(items) {
		t.Fatalf("缓存命中后探测次数=%d，期望=%d", probeCalls, len(items))
	}
}

// TestItemSyncRepositorySpecCacheExpires 验证同步适配器过期缓存不会继续返回旧值。
func TestItemSyncRepositorySpecCacheExpires(t *testing.T) {
	// repository 是仅用于验证缓存生命周期的零基础设施适配器。
	repository := &ItemSyncRepository{cache: map[string]itemSpecCacheEntry{
		"cid\x00item-1": {isMultiSpec: true, expiresAt: time.Now().Add(-time.Second)},
	}}
	// value、ok 保存过期缓存的读取结果。
	value, ok := repository.cachedSpec("cid", "item-1")
	if ok || value {
		t.Fatalf("过期缓存不应命中：value=%v ok=%v", value, ok)
	}
	repository.cacheSpec("cid", "item-1", false)
	value, ok = repository.cachedSpec("cid", "item-1")
	if !ok || value {
		t.Fatalf("新缓存结果异常：value=%v ok=%v", value, ok)
	}
}
