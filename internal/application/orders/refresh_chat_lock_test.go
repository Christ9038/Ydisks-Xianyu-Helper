package orders

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// chatLockRepository 以真实互斥保护凭证，其他持久化能力使用同步内存夹具。
type chatLockRepository struct {
	// refreshRepositoryFake 保存当前凭证和订单，测试只在同一调用栈访问。
	*refreshRepositoryFake
	// mu 模拟数据库账号凭证锁；外部聊天请求不得持有它。
	mu sync.Mutex
}

// LockCredentials 为 cookieID 的测试请求取得锁并返回释放回调。
func (r *chatLockRepository) LockCredentials(cookieID string) func() { r.mu.Lock(); return r.mu.Unlock }

// TestDiscoveryChatRefreshOutsideLock 验证锁外联系人请求、取消、凭证变化及新代次都不能绕过提交校验。
func TestDiscoveryChatRefreshOutsideLock(t *testing.T) {
	// outcome 枚举聊天请求完成时可能发生的生命周期变化。
	for _, outcome := range []string{"success", "cancel", "credentials", "generation"} {
		// t 管理当前同步场景的断言。
		t.Run(outcome, func(t *testing.T) {
			// ctx、cancel 由本测试控制同步生命周期，退出时释放取消资源。
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// repository 保存合法凭证和空会话缓存。
			repository := &chatLockRepository{refreshRepositoryFake: &refreshRepositoryFake{detail: &PlatformRuntimeData{UserID: 7, Value: "fixture"}}}
			// service 在回调执行前完成构造，回调仅在 generation 场景登记更新的请求。
			var service *RefreshService
			// runtime 使用本地已售快照，联系人回调检查锁并模拟生命周期变化。
			runtime := &refreshRuntimeFake{soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order", BuyerID: "buyer", ItemID: "item"}}}, chatRefresh: func(context.Context, string) error {
				if !repository.mu.TryLock() {
					t.Fatal("平台联系人请求持有凭证锁")
				}
				repository.mu.Unlock()
				switch outcome {
				case "cancel":
					cancel()
				case "credentials":
					repository.detail = &PlatformRuntimeData{UserID: 7, Value: "new-fixture"}
				case "generation":
					service.beginDiscovery(ctx, "account")
				}
				return nil
			}}
			service = NewRefreshService(repository, runtime, 10)
			// err 保存经过联系人准备及锁内复核后的同步结果。
			_, _, _, err := service.discoverAccount(ctx, 7, "account")
			if outcome == "success" {
				if err != nil || repository.batchUpsertCount != 1 {
					t.Fatalf("正常请求未提交: %v", err)
				}
			} else if err == nil || repository.batchUpsertCount != 0 {
				t.Fatalf("过期请求不得提交: %v", err)
			}
			if outcome == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("取消错误丢失")
			}
		})
	}
}

// TestPrepareSoldChatsLocalPaths 验证缓存命中、身份缺失、数据库失败和未装配运行时的准备行为。
func TestPrepareSoldChatsLocalPaths(t *testing.T) {
	// repository 保存可直接复用的唯一会话和后续可注入的数据库错误。
	repository := &refreshRepositoryFake{chatIDs: map[string][]string{"buyer\x00item": {"chat"}}}
	// runtime 仅统计不应发生的额外平台刷新。
	runtime := &refreshRuntimeFake{}
	// service 负责锁外联系人准备。
	service := NewRefreshService(repository, runtime, 1)
	// err 验证缓存命中与缺失身份均可在本地完成准备。
	if err := service.prepareSoldChats(context.Background(), "account", []RefreshSoldOrder{{}, {BuyerID: "buyer", ItemID: "item"}}); err != nil || runtime.chatRefreshCalls != 0 {
		t.Fatal("已有会话不应刷新")
	}
	repository.chatMatchErr = errors.New("local query failed")
	// err 应保留仓储查询失败，避免静默跳过匹配错误。
	if err := service.prepareSoldChats(context.Background(), "account", []RefreshSoldOrder{{BuyerID: "buyer", ItemID: "item"}}); !errors.Is(err, repository.chatMatchErr) {
		t.Fatal("查询错误未透传")
	}
	service.runtime = nil
	// err 验证没有运行时能力时准备步骤可安全结束。
	if err := service.prepareSoldChats(context.Background(), "account", nil); err != nil {
		t.Fatal(err)
	}
}
