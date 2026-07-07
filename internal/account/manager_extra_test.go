package account

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
)

// fakeMtop 是注入 engine.Account 的可控 mtop 客户端，避免真实网络。
// refreshErr 非空时 RefreshToken 返回该错误；block 非空时阻塞到该 chan
// 关闭或 ctx 取消（用于让 Run 挂起、模拟"账号在线运行中"）。
type fakeMtop struct {
	refreshErr error
	block      chan struct{}
	calls      int
	mu         sync.Mutex
}

func (f *fakeMtop) RefreshTokenWithDeviceIDContext(ctx context.Context, _, _ string) (*mtop.RefreshResult, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return &mtop.RefreshResult{UpdatedCookies: ""}, ctx.Err()
		}
	}
	if f.refreshErr != nil {
		return &mtop.RefreshResult{}, f.refreshErr
	}
	return &mtop.RefreshResult{AccessToken: "fake-token"}, nil
}

func (f *fakeMtop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}
func (f *fakeMtop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return false, nil, "", nil
}
func (f *fakeMtop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeMtop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeMtop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}

// newTestStore 构造临时 SQLite + 已初始化的 admin，返回 store 与 cleanup。
func newTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	return store, func() { d.Close() }
}

// newManagerWithAccount 构造 Manager 并向 DB 写入一个启用的账号。
func newManagerWithAccount(t *testing.T, cookieID, cookieValue string) (*Manager, *db.Store, func()) {
	t.Helper()
	store, cleanup := newTestStore(t)
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	if err := store.Cookies.Save(context.Background(), cookieID, cookieValue, admin.ID); err != nil {
		cleanup()
		t.Fatalf("Save cookie: %v", err)
	}
	if err := store.Cookies.SetStatus(context.Background(), cookieID, true); err != nil {
		cleanup()
		t.Fatalf("SetStatus: %v", err)
	}
	return NewManager(store, noopHandler{}, nil), store, cleanup
}

// startAccountWithMtop 用自定义 mtop 客户端启动账号，返回 runCtx 的 cancel。
// runCtx 在测试结束 / Stop 时取消以回收 Run goroutine。
func startAccountWithMtop(t *testing.T, mgr *Manager, cookieID, cookieValue string, mtopClient mtop.Client) context.CancelFunc {
	t.Helper()
	runCtx, cancel := context.WithCancel(context.Background())
	acc := engine.New(engine.Config{
		CookieID:  cookieID,
		CookieStr: cookieValue,
		Store:     mgr.store,
		Handler:   mgr.handler,
		Logger:    mgr.logger,
		MTop:      mtopClient,
	})
	accCtx, accCancel := context.WithCancel(runCtx)
	ma := &managedAccount{
		cookieID: cookieID,
		acc:      acc,
		cancel:   accCancel,
		done:     make(chan struct{}),
	}
	mgr.mu.Lock()
	mgr.runCtx = runCtx
	mgr.accounts[cookieID] = ma
	mgr.mu.Unlock()
	go func() {
		defer close(ma.done)
		ma.err = acc.Run(accCtx)
	}()
	return cancel
}

// TestSender 验证 Sender 在账号在线时返回 MessageSender，离线时返回 false。
func TestSender(t *testing.T) {
	mgr, _, cleanup := newManagerWithAccount(t, "acc-on", "unb=1; _m_h5_tk=t1_1;")
	defer cleanup()

	// 离线：未启动。
	if s, ok := mgr.Sender("acc-on"); ok || s != nil {
		t.Fatalf("未启动账号 Sender 应返回 (nil,false)，got (%v,%v)", s, ok)
	}
	if s, ok := mgr.Sender("acc-missing"); ok || s != nil {
		t.Fatalf("不存在账号 Sender 应返回 (nil,false)，got (%v,%v)", s, ok)
	}

	// 用阻塞型 mtop 启动，让 Run 一直挂起，账号保持"运行中"。
	mtopClient := &fakeMtop{block: make(chan struct{})}
	cancel := startAccountWithMtop(t, mgr, "acc-on", "unb=1; _m_h5_tk=t1_1;", mtopClient)
	defer cancel()
	// 给 Run 一点时间进入 refreshToken 阻塞。
	time.Sleep(50 * time.Millisecond)

	s, ok := mgr.Sender("acc-on")
	if !ok || s == nil {
		t.Fatalf("在线账号 Sender 应返回 (sender,true)")
	}

	// 停止后应回到 (nil,false)。Stop 会取消 accCtx，refreshToken 在阻塞 select
	// 上收到 ctx.Done() 返回，Run 退出，不会进入真实 WS dial。
	mgr.Stop("acc-on")
	if s, ok := mgr.Sender("acc-on"); ok || s != nil {
		t.Fatalf("停止后 Sender 应返回 (nil,false)，got (%v,%v)", s, ok)
	}
}

// TestRuntimeStatuses 验证多账号状态快照聚合，覆盖：
//   - 运行中账号（done 未关）：状态原样返回
//   - done 已关 + err=nil：覆盖为 RuntimeError + "账号服务已退出"
//   - done 已关 + err=非 context.Canceled：覆盖为 RuntimeError + err 文案
//   - done 已关 + err=context.Canceled：覆盖为 RuntimeError + "账号服务已退出"
//   - done 已关 + 状态=RuntimeAuthExpired：保持原状态（通过 session-expired 走 handleMaxFailures）
//   - done 已关 + 状态=RuntimeVerificationRequired：保持原状态（通过验证类错误 + 取消）
func TestRuntimeStatuses(t *testing.T) {
	mgr, store, cleanup := newManagerWithAccount(t, "seed", "unb=1; _m_h5_tk=t1_1;")
	defer cleanup()
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")

	// 1) 运行中账号：阻塞型 mtop 让 Run 挂起。
	runMtop := &fakeMtop{block: make(chan struct{})}
	runCancel := startAccountWithMtop(t, mgr, "running", "unb=10; _m_h5_tk=t_1;", runMtop)
	defer runCancel()
	time.Sleep(50 * time.Millisecond)

	// 2) done 已关 + err=nil（手动构造 managedAccount，不跑 Run）。
	nilDoneAcc := engine.New(engine.Config{
		CookieID: "nil-done", CookieStr: "unb=2; _m_h5_tk=t_1;",
		Store: store, Handler: noopHandler{}, MTop: &fakeMtop{},
	})
	nilDoneMA := &managedAccount{cookieID: "nil-done", acc: nilDoneAcc, done: make(chan struct{})}
	close(nilDoneMA.done) // err 为 nil
	mgr.mu.Lock()
	mgr.accounts["nil-done"] = nilDoneMA
	mgr.mu.Unlock()

	// 3) done 已关 + err=非 canceled。
	errDoneAcc := engine.New(engine.Config{
		CookieID: "err-done", CookieStr: "unb=3; _m_h5_tk=t_1;",
		Store: store, Handler: noopHandler{}, MTop: &fakeMtop{},
	})
	errDoneMA := &managedAccount{cookieID: "err-done", acc: errDoneAcc, done: make(chan struct{}), err: errors.New("boom-failure")}
	close(errDoneMA.done)
	mgr.mu.Lock()
	mgr.accounts["err-done"] = errDoneMA
	mgr.mu.Unlock()

	// 4) done 已关 + err=context.Canceled。
	canceledAcc := engine.New(engine.Config{
		CookieID: "canceled", CookieStr: "unb=4; _m_h5_tk=t_1;",
		Store: store, Handler: noopHandler{}, MTop: &fakeMtop{},
	})
	canceledMA := &managedAccount{cookieID: "canceled", acc: canceledAcc, done: make(chan struct{}), err: context.Canceled}
	close(canceledMA.done)
	mgr.mu.Lock()
	mgr.accounts["canceled"] = canceledMA
	mgr.mu.Unlock()

	// 5) 状态=RuntimeAuthExpired（session-expired 错误让 Run 走到 handleMaxFailures 慢重试）。
	//    新行为：账号不再硬退出，而是保持 goroutine 存活、慢重试，状态停在 RuntimeAuthExpired。
	store.Cookies.Save(context.Background(), "auth-exp", "unb=5; _m_h5_tk=t_1;", admin.ID)
	authMgr := mgr
	authCancel := startAccountWithMtop(t, authMgr, "auth-exp", "unb=5; _m_h5_tk=t_1;",
		&fakeMtop{refreshErr: errors.New("token API 登录凭证已失效: ret=[FAIL_SYS_SESSION_EXPIRED] status=403")})
	defer authCancel()
	// 等待 Run 进入 RuntimeAuthExpired 慢重试（不再硬退出，done 不关闭）。
	if !waitForState(mgr, "auth-exp", engine.RuntimeAuthExpired, 2*time.Second) {
		t.Fatal("auth-exp 账号未在超时内进入 RuntimeAuthExpired")
	}

	// 6) done 已关 + 状态=RuntimeVerificationRequired（验证类错误 + 立即取消 ctx）。
	store.Cookies.Save(context.Background(), "verify", "unb=6; _m_h5_tk=t_1;", admin.ID)
	verifyMtop := &fakeMtop{refreshErr: errors.New("FAIL_SYS_USER_VALIDATE: captcha required")}
	verifyCancel := startAccountWithMtop(t, mgr, "verify", "unb=6; _m_h5_tk=t_1;", verifyMtop)
	// 让 Run 进入一次 refreshToken → setRuntimeError(RuntimeVerificationRequired) → sleepCtx。
	time.Sleep(80 * time.Millisecond)
	verifyCancel() // 取消 runCtx，sleepCtx 返回 ctx.Err()，Run 退出，done 关闭。
	if !waitForDone(mgr, "verify", 2*time.Second) {
		t.Fatal("verify 账号未在超时内退出")
	}

	statuses := mgr.RuntimeStatuses()

	// 运行中：应有该 key，状态非空。
	if s, ok := statuses["running"]; !ok || s.State == "" {
		t.Fatalf("running 应有非空状态，got %+v ok=%v", s, ok)
	}

	// nil-done：覆盖为 RuntimeError + "账号服务已退出"。
	if s := statuses["nil-done"]; s.State != engine.RuntimeError || s.Message != "账号服务已退出" {
		t.Fatalf("nil-done 状态错误，got %+v", s)
	}

	// err-done：覆盖为 RuntimeError + err 文案。
	if s := statuses["err-done"]; s.State != engine.RuntimeError || s.Message != "boom-failure" {
		t.Fatalf("err-done 状态错误，got %+v", s)
	}

	// canceled：覆盖为 RuntimeError + "账号服务已退出"（err==context.Canceled 跳过覆盖文案）。
	if s := statuses["canceled"]; s.State != engine.RuntimeError || s.Message != "账号服务已退出" {
		t.Fatalf("canceled 状态错误，got %+v", s)
	}

	// auth-exp：保持 RuntimeAuthExpired，不被覆盖为 RuntimeError。
	if s := statuses["auth-exp"]; s.State != engine.RuntimeAuthExpired {
		t.Fatalf("auth-exp 应保持 %s，got %+v", engine.RuntimeAuthExpired, s)
	}

	// verify：保持 RuntimeVerificationRequired，不被覆盖。
	if s := statuses["verify"]; s.State != engine.RuntimeVerificationRequired {
		t.Fatalf("verify 应保持 %s，got %+v", engine.RuntimeVerificationRequired, s)
	}

	// 聚合数量应包含全部 6 个账号。
	if len(statuses) != 6 {
		t.Fatalf("状态数量应为 6，got %d (%+v)", len(statuses), statuses)
	}
}

// waitForDone 轮询等待某账号 done 关闭。
func waitForDone(mgr *Manager, cookieID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		ma, ok := mgr.accounts[cookieID]
		mgr.mu.Unlock()
		if !ok {
			return false
		}
		select {
		case <-ma.done:
			return true
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// waitForState 轮询等待某账号进入指定运行时状态。
func waitForState(mgr *Manager, cookieID, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		ma, ok := mgr.accounts[cookieID]
		mgr.mu.Unlock()
		if ok && ma.acc != nil && ma.acc.RuntimeStatus().State == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestRestart 验证重启账号：停止旧实例→读取最新 DB cookie→启动新实例。
func TestRestart(t *testing.T) {
	mgr, store, cleanup := newManagerWithAccount(t, "restart-acc", "unb=1; _m_h5_tk=old;")
	defer cleanup()
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")

	// 用阻塞 mtop 启动旧实例，使其保持运行。
	oldMtop := &fakeMtop{block: make(chan struct{})}
	cancel := startAccountWithMtop(t, mgr, "restart-acc", "unb=1; _m_h5_tk=old;", oldMtop)
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	oldMA, _ := mgr.getInstanceInternal("restart-acc")
	if oldMA == nil {
		t.Fatal("旧实例应存在")
	}

	// 更新 DB cookie 为新值，模拟外部刷新。
	store.Cookies.Save(context.Background(), "restart-acc", "unb=1; _m_h5_tk=new-refreshed;", admin.ID)

	ctx, ctxCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ctxCancel()
	if err := mgr.Restart(ctx, "restart-acc"); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	// 新实例应存在且 != 旧实例。
	newMA, ok := mgr.getInstanceInternal("restart-acc")
	if !ok || newMA == nil {
		t.Fatal("Restart 后应存在新实例")
	}
	if newMA == oldMA {
		t.Fatal("Restart 后应是新实例，不是旧实例")
	}
	// 新实例的 CookieStr 应反映最新 DB 值。
	if newMA.acc.CookieStr != "unb=1; _m_h5_tk=new-refreshed;" {
		t.Fatalf("新实例 CookieStr=%q want new-refreshed", newMA.acc.CookieStr)
	}

	// 旧实例的 done 应已关闭（被 Stop）。
	select {
	case <-oldMA.done:
	default:
		t.Fatal("旧实例应已被停止（done 关闭）")
	}

	// 清理新实例。
	mgr.Stop("restart-acc")
}

// TestRestart_GetDetailsError 验证 Restart 读不存在的账号详情应返回包装错误。
func TestRestart_GetDetailsError(t *testing.T) {
	mgr, _, cleanup := newManagerWithAccount(t, "seed", "unb=1; _m_h5_tk=t;")
	defer cleanup()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ctxCancel()
	err := mgr.Restart(ctx, "no-such-account")
	if err == nil {
		t.Fatal("Restart 不存在账号应返回错误")
	}
	if !strings.Contains(err.Error(), "读取账号详情失败") {
		t.Fatalf("错误应包含'读取账号详情失败'，got %v", err)
	}
}

// TestStart_SkipsRunning 验证对运行中账号调用 Start 跳过启动。
func TestStart_SkipsRunning(t *testing.T) {
	mgr, _, cleanup := newManagerWithAccount(t, "running-acc", "unb=1; _m_h5_tk=t;")
	defer cleanup()

	runMtop := &fakeMtop{block: make(chan struct{})}
	cancel := startAccountWithMtop(t, mgr, "running-acc", "unb=1; _m_h5_tk=t;", runMtop)
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	original, _ := mgr.getInstanceInternal("running-acc")

	ctx, ctxCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ctxCancel()
	if err := mgr.Start(ctx, "running-acc", "unb=1; _m_h5_tk=different;"); err != nil {
		t.Fatalf("Start 运行中账号应返回 nil，got %v", err)
	}

	// 应仍是同一实例（未被替换）。
	after, _ := mgr.getInstanceInternal("running-acc")
	if after != original {
		t.Fatal("Start 运行中账号不应替换实例")
	}
	if after.acc.CookieStr != "unb=1; _m_h5_tk=t;" {
		t.Fatalf("实例 CookieStr 不应变，got %q", after.acc.CookieStr)
	}

	mgr.Stop("running-acc")
}

// TestStart_RestartsExited 验证 Manager.Start 在"账号已存在且 done 已关"路径上
// 清理旧实例并重启新实例。该路径曾是双重解锁 bug（分支内 Unlock 后函数末尾再次
// Unlock），已修复：现在持锁 delete 后由函数末尾单次 Unlock。
func TestStart_RestartsExited(t *testing.T) {
	mgr, _, cleanup := newManagerWithAccount(t, "exited-acc", "unb=1; _m_h5_tk=t;")
	defer cleanup()

	// 注入一个已退出的 managedAccount（done 已关），无 Run goroutine。
	exitedAcc := engine.New(engine.Config{
		CookieID: "exited-acc", CookieStr: "unb=1; _m_h5_tk=old;",
		Store: mgr.store, Handler: noopHandler{}, MTop: &fakeMtop{},
	})
	exitedMA := &managedAccount{cookieID: "exited-acc", acc: exitedAcc, done: make(chan struct{}), err: context.Canceled}
	close(exitedMA.done)
	mgr.mu.Lock()
	mgr.accounts["exited-acc"] = exitedMA
	mgr.mu.Unlock()

	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()
	mgr.mu.Lock()
	mgr.runCtx = ctx
	mgr.mu.Unlock()

	if err := mgr.Start(ctx, "exited-acc", "unb=1; _m_h5_tk=brand-new;"); err != nil {
		t.Fatalf("Start 已退出账号应成功，got %v", err)
	}

	newMA, ok := mgr.getInstanceInternal("exited-acc")
	if !ok {
		t.Fatal("Start 后应存在新实例")
	}
	if newMA == exitedMA {
		t.Fatal("应替换为全新实例")
	}
	if newMA.acc.CookieStr != "unb=1; _m_h5_tk=brand-new;" {
		t.Fatalf("新实例 CookieStr=%q want brand-new", newMA.acc.CookieStr)
	}
	// done 不应已关闭（新实例在运行）。
	select {
	case <-newMA.done:
		t.Fatal("新实例 done 不应已关闭")
	default:
	}

	mgr.Stop("exited-acc")
}

// TestStartAll_LoadError 验证 StartAll 在 DB 加载失败时返回包装错误。
// 通过提前关闭底层 sql.DB 触发 AllForUser 查询错误。
func TestStartAll_LoadError(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	mgr := NewManager(store, noopHandler{}, nil)

	// 提前关闭 DB 让 AllForUser 失败。
	if err := store.DB.Close(); err != nil {
		t.Fatalf("关闭 DB: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := mgr.StartAll(ctx)
	if err == nil {
		t.Fatal("StartAll 在 DB 加载失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "加载账号失败") {
		t.Fatalf("错误应包含'加载账号失败'，got %v", err)
	}
}

// TestStartAll_DisabledNotStarted 验证 DB 中禁用的账号不被启动。
func TestStartAll_DisabledNotStarted(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// 启用 + 禁用 + 启用。
	store.Cookies.Save(context.Background(), "on1", "unb=1; _m_h5_tk=t;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "on1", true)
	store.Cookies.Save(context.Background(), "off1", "unb=2; _m_h5_tk=t;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "off1", false)
	store.Cookies.Save(context.Background(), "on2", "unb=3; _m_h5_tk=t;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "on2", true)

	// 用阻塞 mtop 的 Manager：直接构造，使所有启动账号都挂起、不触发真实网络。
	mgr := NewManager(store, noopHandler{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// 启用的应存在，禁用的不应存在。
	if _, ok := mgr.GetInstance("on1"); !ok {
		t.Fatal("on1 应被启动")
	}
	if _, ok := mgr.GetInstance("on2"); !ok {
		t.Fatal("on2 应被启动")
	}
	if _, ok := mgr.GetInstance("off1"); ok {
		t.Fatal("off1 已禁用不应被启动")
	}

	// StopAll 应清空全部。
	mgr.StopAll()
	for _, id := range []string{"on1", "on2", "off1"} {
		if _, ok := mgr.GetInstance(id); ok {
			t.Fatalf("StopAll 后 %s 仍存在", id)
		}
	}
}

// TestStop_Nonexistent 验证停止不存在的账号是 no-op，不 panic。
func TestStop_Nonexistent(t *testing.T) {
	mgr, _, cleanup := newManagerWithAccount(t, "seed", "unb=1; _m_h5_tk=t;")
	defer cleanup()
	// 不应 panic。
	mgr.Stop("does-not-exist")
}

// TestConcurrency 用 -race 验证多 goroutine 并发访问 GetInstance/Sender/RuntimeStatuses 不 race。
func TestConcurrency(t *testing.T) {
	mgr, store, cleanup := newManagerWithAccount(t, "conc-acc", "unb=1; _m_h5_tk=t;")
	defer cleanup()
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// 多个启用账号。
	for _, id := range []string{"conc-2", "conc-3"} {
		store.Cookies.Save(context.Background(), id, "unb=2; _m_h5_tk=t;", admin.ID)
		store.Cookies.SetStatus(context.Background(), id, true)
	}

	// 用阻塞 mtop 让账号保持运行，避免 Run 退出引入噪声。
	for _, id := range []string{"conc-acc", "conc-2", "conc-3"} {
		mtopClient := &fakeMtop{block: make(chan struct{})}
		c := startAccountWithMtop(t, mgr, id, "unb=1; _m_h5_tk=t;", mtopClient)
		defer c()
	}
	time.Sleep(60 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = mgr.GetInstance("conc-acc")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = mgr.Sender("conc-2")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.RuntimeStatuses()
			}
		}()
	}
	wg.Wait()
	mgr.StopAll()
}

// getInstanceInternal 返回内部 managedAccount（测试用）。
func (m *Manager) getInstanceInternal(cookieID string) (*managedAccount, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ma, ok := m.accounts[cookieID]
	return ma, ok
}
