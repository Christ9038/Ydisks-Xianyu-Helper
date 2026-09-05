package orders

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// discoveryRaceRepository 用独立内存锁模拟凭证提交临界区与订单存储，其他接口沿用未调用的基础替身。
type discoveryRaceRepository struct {
	// refreshRepositoryFake 仅补齐本测试不触达的仓储端口。
	*refreshRepositoryFake
	// mu 保护 orders 和 deletes；测试只在请求完成后读取统计。
	mu sync.Mutex
	// credentials 的账号锁在构造后不修改，互不阻塞不同账号。
	credentials map[string]*sync.Mutex
	// orders 保存当前可见订单及所属账号，软删除直接移除映射。
	orders map[string]*Order
	// deletes 统计缺失清理次数，以识别旧请求是否到达删除边界。
	deletes int
}

// LockCredentials 为 r 的 cookieID 返回独立锁的释放函数；调用方负责释放，不涉及平台 I/O。
func (r *discoveryRaceRepository) LockCredentials(cookieID string) func() {
	r.credentials[cookieID].Lock()
	return r.credentials[cookieID].Unlock
}

// LoadCookiePlatformDetail 返回 cookieID 的固定合成凭证；ctx 不访问外部状态，每次返回独立副本。
func (r *discoveryRaceRepository) LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*PlatformRuntimeData, error) {
	return &PlatformRuntimeData{ID: cookieID, UserID: 7, Value: "fixture"}, nil
}

// FindOrderOwnership 在 r.mu 内读取 orderID 的最小归属；ctx/userID 不访问真实鉴权服务。
func (r *discoveryRaceRepository) FindOrderOwnership(ctx context.Context, userID int64, orderID string) (RefreshOwnership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// order 是当前内存记录，缺失时走正常新增路径。
	order := r.orders[orderID]
	if order == nil {
		return RefreshOwnership{}, ErrNotFound
	}
	return RefreshOwnership{OrderID: orderID, CookieID: order.CookieID, Owned: true}, nil
}

// FindOrdersByIDs 在 r.mu 内复制 cookieID 对应的 orderIDs；ctx 不访问外部资源，返回值不共享可变订单。
func (r *discoveryRaceRepository) FindOrdersByIDs(ctx context.Context, cookieID string, orderIDs []string) (map[string]*Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// result 保存当前账号已有的订单副本。
	result := make(map[string]*Order)
	// orderID 是待查询订单；order 仅在锁内使用。
	for _, orderID := range orderIDs {
		// order 是当前账号可以读取的内存实体。
		if order := r.orders[orderID]; order != nil && order.CookieID == cookieID {
			// copied 防止调用者与仓储共享可变实体。
			copied := *order
			result[orderID] = &copied
		}
	}
	return result, nil
}

// BatchUpsertOrders 在 r.mu 内原子写入 rows；ctx 取消时不修改内存订单。
func (r *discoveryRaceRepository) BatchUpsertOrders(ctx context.Context, rows []RefreshOrderWrite) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// row 是本次已完成平台查询的订单写入事实。
	for _, row := range rows {
		r.orders[row.OrderID] = &Order{OrderID: row.OrderID, CookieID: row.Options.CookieID, OrderStatus: row.Options.OrderStatus}
	}
	return nil
}

// SoftDeleteMissingOrders 在 r.mu 内按 activeIDs 清理 cookieID 的缺失订单；ctx 取消时不计入清理次数。
func (r *discoveryRaceRepository) SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	r.deletes++
	// deleted 统计本次实际移除的可见订单。
	deleted := 0
	// orderID、order 是当前待判断是否仍在线上的本地订单。
	for orderID, order := range r.orders {
		// active 表示完整远端集合是否包含当前订单。
		if _, active := activeIDs[orderID]; !active && order.CookieID == cookieID {
			delete(r.orders, orderID)
			deleted++
		}
	}
	return deleted, nil
}

// discoveryRaceRuntime 通过 fetch 注入可控的平台响应，原子计数避免测试自身引入数据竞争。
type discoveryRaceRuntime struct {
	// refreshRuntimeFake 提供无外部副作用的能力与会话判定。
	*refreshRuntimeFake
	// fetch 使用 ctx 控制等待，detail 决定账号，返回已售快照及错误。
	fetch func(context.Context, *PlatformRuntimeData) (RefreshSoldFetchResult, error)
	// persists 统计平台会话提交，旧请求不得进入此边界。
	persists atomic.Int32
}

// FetchSoldOrders 由 r 转发 ctx、detail 到同步测试回调，不访问真实平台。
func (r *discoveryRaceRuntime) FetchSoldOrders(ctx context.Context, detail *PlatformRuntimeData) (RefreshSoldFetchResult, error) {
	return r.fetch(ctx, detail)
}

// PersistCookieSession 记录 r 的会话提交次数；ctx、detail、update 只满足端口，无真实 Cookie 写入。
func (r *discoveryRaceRuntime) PersistCookieSession(ctx context.Context, detail *PlatformRuntimeData, update RefreshCookieUpdate) (string, bool, bool, error) {
	r.persists.Add(1)
	return "", false, true, nil
}

// waitDiscoverySignal 用 t 等待 signal，超时结束测试，避免并发回归因错误锁顺序无限挂起。
func waitDiscoverySignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("账号发现未按预期推进")
	}
}

// waitDiscoveryError 用 t 等待同步调用的 done 返回值，超时只用于检测死锁而非安排执行顺序。
func waitDiscoveryError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done: // err 是后台同步最终返回的错误，不包含合成凭证。
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("账号发现未结束")
		return nil
	}
}

// TestDiscoveryLatestAttemptFencesOlderSnapshot 用 t 验证新请求完成、失败或取消后，迟到旧快照均不能清理订单。
func TestDiscoveryLatestAttemptFencesOlderSnapshot(t *testing.T) {
	// outcome 指定新请求最终状态，旧请求始终在其结束后才返回。
	for _, outcome := range []string{"success", "failure", "cancel"} {
		// t 接收当前交错时序的断言。
		t.Run(outcome, func(t *testing.T) {
			// ctx、cancel 控制旧请求生命周期，异常退出时也释放后台等待。
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// entered、release 确保旧请求先开始但最后返回，只有测试主协程关闭 release。
			entered, release := make(chan struct{}), make(chan struct{})
			// calls 对并发平台请求分配稳定次序。
			var calls atomic.Int32
			// repository 保存用于检测误删的初始订单。
			repository := &discoveryRaceRepository{refreshRepositoryFake: &refreshRepositoryFake{}, credentials: map[string]*sync.Mutex{"account": {}}, orders: map[string]*Order{"new": {OrderID: "new", CookieID: "account"}}}
			// newerCtx、cancelNewer 控制第二次请求在平台返回时取消的场景。
			newerCtx, cancelNewer := context.WithCancel(ctx)
			defer cancelNewer()
			// runtime 的回调用 requestCtx 等待旧请求释放，忽略凭证内容。
			runtime := &discoveryRaceRuntime{refreshRuntimeFake: &refreshRuntimeFake{}, fetch: func(requestCtx context.Context, _ *PlatformRuntimeData) (RefreshSoldFetchResult, error) {
				if calls.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
						return RefreshSoldFetchResult{}, nil
					case <-requestCtx.Done():
						return RefreshSoldFetchResult{}, requestCtx.Err()
					}
				}
				if outcome == "failure" {
					return RefreshSoldFetchResult{}, errors.New("平台请求失败")
				}
				if outcome == "cancel" {
					cancelNewer()
					return RefreshSoldFetchResult{}, requestCtx.Err()
				}
				return RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "new"}}}, nil
			}}
			// service 刻意用结构体字面量，要求并发保护零值可用。
			service := &RefreshService{repository: repository, runtime: runtime}
			// done 由旧请求协程发送一次结果，缓冲避免测试失败后阻塞退出。
			done := make(chan error, 1)
			// 后台调用使用 ctx，主测试通过 entered/release 控制其平台响应时机。
			go func() {
				// err 是旧请求提交或失效保护的最终结果。
				_, _, _, err := service.discoverAccount(ctx, 7, "account")
				done <- err
			}()
			waitDiscoverySignal(t, entered)
			// newerErr 是第二次请求的完成状态；结束不能恢复旧请求的提交资格。
			_, _, _, newerErr := service.discoverAccount(newerCtx, 7, "account")
			close(release)
			// olderErr 是迟到快照的拒绝原因。
			olderErr := waitDiscoveryError(t, done)
			if olderErr == nil || !strings.Contains(olderErr.Error(), "旧") || (newerErr == nil) != (outcome == "success") {
				t.Fatal("新请求结束后，旧快照未被正确拒绝")
			}
			if repository.orders["new"] == nil {
				t.Fatal("旧快照误删了本地订单")
			}
			// wantCommits 只允许成功的新请求提交会话并清理缺失订单。
			wantCommits := 0
			if outcome == "success" {
				wantCommits = 1
			}
			// wantPersists 允许未取消的新请求收集失败响应 Cookie，但禁止旧请求提交。
			wantPersists := 1
			if outcome == "cancel" {
				wantPersists = 0
			}
			if repository.deletes != wantCommits || int(runtime.persists.Load()) != wantPersists {
				t.Fatal("被取消或过时的请求仍有提交副作用")
			}
			assertDiscoveryCleaned(t, service)
		})
	}
}

// assertDiscoveryCleaned 用 t 在请求全部返回后检查 service 是否释放发现代次，遍历不持有业务锁。
func assertDiscoveryCleaned(t *testing.T, service *RefreshService) {
	t.Helper()
	// 回调只用于发现残留记录，忽略账号键及上下文指针，不输出内部身份。
	service.discoveries.Range(func(_, _ any) bool {
		t.Fatal("已结束请求的账号发现代次未清理")
		return false
	})
}

// TestDiscoveryAccountCancellationAndIsolation 用 t 验证账号间无网络等待串行化，取消后同账号可重新发现。
func TestDiscoveryAccountCancellationAndIsolation(t *testing.T) {
	// ctx、cancel 管理首个账号的阻塞平台请求，测试结束必定取消。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// entered 通知主测试首个账号已进入平台等待。
	entered := make(chan struct{})
	// accountCalls 区分首个账号的取消请求和后续成功重试。
	var accountCalls atomic.Int32
	// repository 为两个账号提供互相独立的凭证锁与订单存储。
	repository := &discoveryRaceRepository{refreshRepositoryFake: &refreshRepositoryFake{}, credentials: map[string]*sync.Mutex{"first": {}, "second": {}}, orders: make(map[string]*Order)}
	// runtime 的 requestCtx 控制取消；detail.ID 决定平台响应所属账号。
	runtime := &discoveryRaceRuntime{refreshRuntimeFake: &refreshRuntimeFake{}, fetch: func(requestCtx context.Context, detail *PlatformRuntimeData) (RefreshSoldFetchResult, error) {
		if detail.ID == "first" && accountCalls.Add(1) == 1 {
			close(entered)
			<-requestCtx.Done()
			// 模拟平台在取消后仍返回成功快照，应用仍须拦截提交。
			return RefreshSoldFetchResult{}, nil
		}
		return RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: detail.ID + "-order"}}}, nil
	}}
	// service 使用字面量验证发现状态无需额外初始化步骤。
	service := &RefreshService{repository: repository, runtime: runtime}
	// firstDone、secondDone 分别收集两个账号的调用结果，缓冲确保异常退出不阻塞发送。
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	// 首账号等待由 ctx 取消，不持有服务内存锁或凭证锁。
	go func() {
		// err 是首账号在平台返回后识别的取消错误。
		_, _, _, err := service.discoverAccount(ctx, 7, "first")
		firstDone <- err
	}()
	waitDiscoverySignal(t, entered)
	// 次账号应在首账号仍等待时独立完成整个提交过程。
	go func() {
		// err 是次账号独立同步的结果。
		_, _, _, err := service.discoverAccount(ctx, 7, "second")
		secondDone <- err
	}()
	if waitDiscoveryError(t, secondDone) != nil {
		t.Fatal("不同账号未独立完成同步")
	}
	cancel()
	if !errors.Is(waitDiscoveryError(t, firstDone), context.Canceled) {
		t.Fatal("取消的账号发现未返回上下文错误")
	}
	if runtime.persists.Load() != 1 || repository.deletes != 1 || repository.orders["second-order"] == nil {
		t.Fatal("取消请求仍提交数据或影响其他账号")
	}
	assertDiscoveryCleaned(t, service)
	// retryErr 验证取消清理后的同账号可以开始全新的发现窗口。
	_, _, _, retryErr := service.discoverAccount(context.Background(), 7, "first")
	if retryErr != nil || repository.orders["first-order"] == nil {
		t.Fatal("同账号取消后无法重新同步")
	}
	assertDiscoveryCleaned(t, service)
}

// TestDiscoveryAlreadyCanceledDoesNotRegister 用 t 验证预先取消的上下文不占用代次或调用平台。
func TestDiscoveryAlreadyCanceledDoesNotRegister(t *testing.T) {
	// ctx、cancel 构造进入发现前已经失效的调用上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// service 无需依赖即可拒绝取消请求，任何错误继续执行都会让测试失败。
	service := &RefreshService{}
	// err 应保留取消语义而非产生账号或平台失败。
	_, _, _, err := service.discoverAccount(ctx, 7, "account")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("预先取消的发现未立即返回")
	}
	assertDiscoveryCleaned(t, service)
}

// TestDiscoveryCanceledOlderKeepsNewerGeneration 用 t 验证旧请求取消清理时不能删除同账号仍在执行的新代次。
func TestDiscoveryCanceledOlderKeepsNewerGeneration(t *testing.T) {
	// ctx、cancel 控制旧请求；newerCtx、cancelNewer 控制独立的新请求，异常退出均可释放等待。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// newerCtx 不继承旧请求取消，新代次必须继续提交。
	newerCtx, cancelNewer := context.WithCancel(context.Background())
	defer cancelNewer()
	// entered、newerEntered、release 分别同步旧请求进入、新请求进入以及新响应放行。
	entered, newerEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// calls 确定两个平台请求的先后顺序，避免测试依赖调度时间。
	var calls atomic.Int32
	// repository 提供单账号真实互斥语义，订单初始为空。
	repository := &discoveryRaceRepository{refreshRepositoryFake: &refreshRepositoryFake{}, credentials: map[string]*sync.Mutex{"account": {}}, orders: make(map[string]*Order)}
	// runtime 的 requestCtx 支持各自取消；返回结果只含合成订单号。
	runtime := &discoveryRaceRuntime{refreshRuntimeFake: &refreshRuntimeFake{}, fetch: func(requestCtx context.Context, _ *PlatformRuntimeData) (RefreshSoldFetchResult, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-requestCtx.Done()
			return RefreshSoldFetchResult{}, requestCtx.Err()
		}
		close(newerEntered)
		select {
		case <-release:
			return RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "new"}}}, nil
		case <-requestCtx.Done():
			return RefreshSoldFetchResult{}, requestCtx.Err()
		}
	}}
	// service 的代次表由两个并发调用共享。
	service := &RefreshService{repository: repository, runtime: runtime}
	// olderDone、newerDone 各自保存一次请求完成结果。
	olderDone, newerDone := make(chan error, 1), make(chan error, 1)
	// 旧请求先进入平台等待，由主测试取消。
	go func() {
		// err 是旧请求取消结果。
		_, _, _, err := service.discoverAccount(ctx, 7, "account")
		olderDone <- err
	}()
	waitDiscoverySignal(t, entered)
	// 新请求登记后等待主测试放行响应，期间旧请求会完成清理。
	go func() {
		// err 是新请求是否仍具有提交资格的结果。
		_, _, _, err := service.discoverAccount(newerCtx, 7, "account")
		newerDone <- err
	}()
	waitDiscoverySignal(t, newerEntered)
	cancel()
	if !errors.Is(waitDiscoveryError(t, olderDone), context.Canceled) {
		t.Fatal("旧请求取消未正常完成")
	}
	close(release)
	if waitDiscoveryError(t, newerDone) != nil || repository.orders["new"] == nil || runtime.persists.Load() != 1 || repository.deletes != 1 {
		t.Fatal("旧请求清理误伤了同账号新代次")
	}
	assertDiscoveryCleaned(t, service)
}

// TestDiscoveryCredentialChangeDoesNotRecoverOldSession 用 t 验证旧会话失效与凭证变化同时发生时只返回凭证变化。
func TestDiscoveryCredentialChangeDoesNotRecoverOldSession(t *testing.T) {
	// repository 在请求后返回新的账号凭证，模拟用户完成重新登录。
	repository := &refreshRepositoryFake{loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "before"}, {UserID: 7, Value: "after"}}}
	// runtime 把旧请求标记为会话失效；该错误不能触发新会话恢复。
	runtime := &refreshRuntimeFake{fetchErr: errors.New("旧会话失效"), expired: true}
	// service 使用零值并发状态以覆盖既有结构体字面量构造方式。
	service := &RefreshService{repository: repository, runtime: runtime}
	// result、err 保存凭证复核结果和会话标记。
	_, _, result, err := service.discoverAccount(context.Background(), 7, "account")
	if !errors.Is(err, ErrRefreshCredentialChanged) || result.SessionExpired || runtime.recoverCalls != 0 || repository.batchUpsertCount != 0 {
		t.Fatal("旧会话错误仍影响新凭证或进入订单写入")
	}
}
