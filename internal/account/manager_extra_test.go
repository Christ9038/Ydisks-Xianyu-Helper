package account

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/ws"
)

// fakeWSDialer 用于本次流程后续判断的fakeWSDialer
type fakeWSDialer struct{}

// refreshHandler 覆盖默认处理器的凭证刷新结果，验证 Manager 委托边界。
type refreshHandler struct {
	noopHandler
}

// OnPasswordLoginRefresh 返回成功，模拟上层完成密码登录刷新。
func (refreshHandler) OnPasswordLoginRefresh(context.Context, string) bool {
	return true
}

// stagedContext 在指定 Err 调用次数后返回取消错误，用于覆盖重启阶段检查的时间窗口。
type stagedContext struct {
	// calls 记录调用方读取 Err 的次数。
	calls int
	// cancelAfter 表示从第几次 Err 调用开始返回取消错误。
	cancelAfter int
	// done 保存可选的已关闭通知信号，用于模拟关闭 Context。
	done chan struct{}
}

// Deadline 返回无截止时间，保持测试上下文只由 Err 控制阶段转换。
func (c *stagedContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

// Done 返回可选的关闭通知信号。
func (c *stagedContext) Done() <-chan struct{} {
	return c.done
}

// Err 在达到预设调用次数后返回 context.Canceled。
func (c *stagedContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAfter {
		return context.Canceled
	}
	return nil
}

// Value 不提供测试上下文值。
func (c *stagedContext) Value(any) any {
	return nil
}

// Dial 封装Dial业务协调。
func (fakeWSDialer) Dial(context.Context, ws.Config, *slog.Logger) (engine.WSConn, error) {
	return fakeWSConn{}, nil
}

// fakeWSConn 用于本次流程后续判断的fakeWSConn
type fakeWSConn struct{}

// Register 封装Register业务协调。
func (fakeWSConn) Register(context.Context, string, string) error { return nil }

// HeartbeatLoop 封装HeartbeatLoop业务协调。
func (fakeWSConn) HeartbeatLoop(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

// ReceiveLoop 封装ReceiveLoop业务协调。
func (fakeWSConn) ReceiveLoop(ctx context.Context, _ func(map[string]any)) error {
	<-ctx.Done()
	return ctx.Err()
}

// Close 关闭当前值。
func (fakeWSConn) Close() error { return nil }

// SendText 封装Send文本业务协调。
func (fakeWSConn) SendText(context.Context, string, string, string, string) error {
	return nil
}

// SendImage 封装Send图片业务协调。
func (fakeWSConn) SendImage(context.Context, string, string, string, string, int, int) error {
	return nil
}

// fakeMtop 是注入 engine.Account 的可控 mtop 客户端，避免真实网络。
// refreshErr 非空时 RefreshToken 返回该错误；block 非空时阻塞到该 chan
// 关闭或 ctx 取消（用于让 Run 挂起、模拟"账号在线运行中"）。
// fakeMtop 用于本次流程后续判断的fakeMtop
type fakeMtop struct {
	refreshErr error
	block      chan struct{}
	calls      int
	mu         sync.Mutex
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
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

// FetchUserProfile 封装Fetch用户Profile业务协调。
func (f *fakeMtop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}

// AdjustOrderPriceContext 满足 MTOP 客户端接口，账号管理测试不关心订单改价。
func (f *fakeMtop) AdjustOrderPriceContext(context.Context, string, string, int64) (bool, []string, string, error) {
	return true, nil, "", nil
}

// ConsignContext 封装Consign上下文业务协调。
func (f *fakeMtop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return false, nil, "", nil
}

// FetchItemsPage 封装Fetch商品列表页码业务协调。
func (f *fakeMtop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// FetchAllItems 封装FetchAll商品列表业务协调。
func (f *fakeMtop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// PublishItem 封装发布商品业务协调。
func (f *fakeMtop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}

// newTestStore 构造临时 SQLite + 已初始化的 admin，返回 store 与 cleanup。
func newTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// store 用于本次流程后续判断的store
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	return store, func() { d.Close() }
}

// newManagerWithAccount 构造 Manager 并向 DB 写入一个启用的账号。
func newManagerWithAccount(t *testing.T, cookieID, cookieValue string) (*Manager, *db.Store, func()) {
	t.Helper()
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestStore(t)
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(context.Background(), cookieID, cookieValue, admin.ID); err != nil {
		cleanup()
		t.Fatalf("Save cookie: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.SetStatus(context.Background(), cookieID, true); err != nil {
		cleanup()
		t.Fatalf("SetStatus: %v", err)
	}
	return NewManager(store, noopHandler{}, nil), store, cleanup
}

// startAccountWithMtop 用自定义 mtop 客户端启动账号，返回 runCtx 的 cancel。
// runCtx 在测试结束 / Stop 时取消以回收 Run goroutine。
// startAccountWithMtop 封装开始账号WithMtop业务协调。
func startAccountWithMtop(t *testing.T, mgr *Manager, cookieID, cookieValue string, mtopClient mtop.Client) context.CancelFunc {
	t.Helper()
	// runCtx、cancel 用于本次流程后续判断的运行Ctx、cancel
	runCtx, cancel := context.WithCancel(context.Background())
	// acc 用于本次流程后续判断的acc
	acc := engine.New(engine.Config{
		CookieID:  cookieID,
		CookieStr: cookieValue,
		Store:     mgr.store,
		Handler:   mgr.handler,
		Logger:    mgr.logger,
		MTop:      mtopClient,
		WSDialer:  fakeWSDialer{},
	})
	// accCtx、accCancel 用于本次流程后续判断的accCtx、acc取消
	accCtx, accCancel := context.WithCancel(runCtx)
	// ma 用于本次流程后续判断的ma
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
	// mgr、cleanup 用于本次流程后续判断的mgr、cleanup
	mgr, _, cleanup := newManagerWithAccount(t, "acc-on", "unb=1; _m_h5_tk=t1_1;")
	defer cleanup()

	// 离线：未启动。
	if s, ok := mgr.Sender("acc-on"); ok || s != nil {
		t.Fatalf("未启动账号 Sender 应返回 (nil,false)，got (%v,%v)", s, ok)
	}
	if // s、ok 用于本次流程后续判断的s、ok
	s, ok := mgr.Sender("acc-missing"); ok || s != nil {
		t.Fatalf("不存在账号 Sender 应返回 (nil,false)，got (%v,%v)", s, ok)
	}

	// 用阻塞型 mtop 启动，让 Run 一直挂起，账号保持"运行中"。
	mtopClient := &fakeMtop{block: make(chan struct{})}
	// cancel 用于本次流程后续判断的取消
	cancel := startAccountWithMtop(t, mgr, "acc-on", "unb=1; _m_h5_tk=t1_1;", mtopClient)
	defer cancel()
	// 给 Run 一点时间进入 refreshToken 阻塞。
	time.Sleep(50 * time.Millisecond)

	// s、ok 用于本次流程后续判断的s、ok
	s, ok := mgr.Sender("acc-on")
	if !ok || s == nil {
		t.Fatalf("在线账号 Sender 应返回 (sender,true)")
	}

	// 停止后应回到 (nil,false)。Stop 会取消 accCtx，refreshToken 在阻塞 select
	// 上收到 ctx.Done() 返回，Run 退出，不会进入真实 WS dial。
	mgr.Stop("acc-on")
	if // s、ok 用于本次流程后续判断的s、ok
	s, ok := mgr.Sender("acc-on"); ok || s != nil {
		t.Fatalf("停止后 Sender 应返回 (nil,false)，got (%v,%v)", s, ok)
	}
}

// TestManagerInputAndCredentialBoundaries 验证管理器空 Context、停止 fencing 和凭证刷新边界。
func TestManagerInputAndCredentialBoundaries(t *testing.T) {
	// manager 是不依赖数据库启动的输入校验管理器。
	manager := NewManager(nil, noopHandler{}, nil)
	// nilContext 是专门验证管理器 nil Context 防护的空接口值。
	var nilContext context.Context
	if // err 是空启动 Context 的参数错误。
	err := manager.StartAll(nilContext); err == nil {
		t.Fatal("StartAll 应拒绝 nil Context")
	}
	if // err 是单账号空启动 Context 的参数错误。
	err := manager.Start(nilContext, "account", "cookie"); err == nil {
		t.Fatal("Start 应拒绝 nil Context")
	}
	if // err 是单账号空停止 Context 的参数错误。
	err := manager.StopContext(nilContext, "account"); err == nil {
		t.Fatal("StopContext 应拒绝 nil Context")
	}
	if // err 是全量空停止 Context 的参数错误。
	err := manager.StopAllContext(nilContext); err == nil {
		t.Fatal("StopAllContext 应拒绝 nil Context")
	}
	// manager.accounts 保存正在停止的账号实例占位，验证 Start 的实例 fencing。
	manager.accounts["stopping-account"] = &managedAccount{stopping: true}
	if // err 是实例停止 fencing 期间的启动拒绝错误。
	err := manager.Start(context.Background(), "stopping-account", "cookie"); err == nil {
		t.Fatal("正在停止的账号不应再次启动")
	}
	if // refreshed 是未装配处理器时的刷新结果。
	refreshed := manager.RecoverExpiredCredential(context.Background(), "account"); refreshed {
		t.Fatal("未装配处理器不应报告刷新成功")
	}
	// refreshManager 是注入成功刷新处理器的管理器。
	refreshManager := NewManager(nil, refreshHandler{}, nil)
	if // refreshed 是处理器委托返回的刷新结果。
	refreshed := refreshManager.RecoverExpiredCredential(context.Background(), "account"); !refreshed {
		t.Fatal("处理器刷新成功结果未透传")
	}
	// nilManager 保存 nil 管理器指针，用于验证刷新委托的空值保护。
	var nilManager *Manager
	if // refreshed 是 nil 管理器的刷新结果。
	refreshed := nilManager.RecoverExpiredCredential(context.Background(), "account"); refreshed {
		t.Fatal("nil 管理器不应报告刷新成功")
	}
}

// TestManagerStopAndRestartErrorBoundaries 验证停止等待取消、全量停止传播和重启启动失败边界。
func TestManagerStopAndRestartErrorBoundaries(t *testing.T) {
	// manager 是包含未关闭运行完成信号的停止测试管理器。
	manager := NewManager(nil, noopHandler{}, nil)
	// account 是未启动但可执行生命周期 Stop 的账号运行时。
	account := engine.New(engine.Config{CookieID: "cancel-account", CookieStr: "unb=1"})
	// managed 是等待信号保持打开的管理实例。
	managed := &managedAccount{cookieID: "cancel-account", acc: account, cancel: func() {}, done: make(chan struct{})}
	manager.accounts[managed.cookieID] = managed
	// canceledContext 是已经取消的停止请求上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if // err 是停止等待运行协程时保留的上下文取消错误。
	err := manager.StopContext(canceledContext, managed.cookieID); !errors.Is(err, context.Canceled) {
		t.Fatalf("停止等待应返回取消错误: %v", err)
	}
	if // err 是全量停止传播单账号取消错误的结果。
	err := manager.StopAllContext(canceledContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("全量停止应传播取消错误: %v", err)
	}
	// nilRestartErr 是 nil 重启 Context 的分类错误。
	// nilRestartContext 是专门验证重启 nil Context 防护的空接口值。
	var nilRestartContext context.Context
	// nilRestartErr 是 nil 重启 Context 返回的分类错误。
	nilRestartErr := manager.Restart(nilRestartContext, "cancel-account")
	if !errors.Is(nilRestartErr, ErrRestartIncomplete) {
		t.Fatalf("nil 重启 Context 错误异常: %v", nilRestartErr)
	}
	// postStopContext 在停止旧实例后才报告取消，覆盖重启读取 Cookie 前的检查。
	postStopContext := &stagedContext{cancelAfter: 2}
	// postStopManager 是没有旧实例的管理器，确保重启阶段直接进入停止后检查。
	postStopManager := NewManager(nil, noopHandler{}, nil)
	// postStopErr 是停止阶段后取消重启返回的分类错误。
	postStopErr := postStopManager.Restart(postStopContext, "cancel-account")
	if !errors.Is(postStopErr, ErrRestartIncomplete) || !errors.Is(postStopErr, context.Canceled) {
		t.Fatalf("停止后取消错误异常: %v", postStopErr)
	}
	// closedDone 是模拟关闭 Context 的已关闭信号。
	closedDone := make(chan struct{})
	close(closedDone)
	// stopErrorManager 是用于覆盖停止阶段错误包装的管理器。
	stopErrorManager := NewManager(nil, noopHandler{}, nil)
	// stopErrorAccount 是停止时可立即收束的账号运行时。
	stopErrorAccount := engine.New(engine.Config{CookieID: "restart-stop-error", CookieStr: "unb=1"})
	// stopErrorManaged 保留打开的管理完成信号，使管理器在 Context 关闭时返回错误。
	stopErrorManaged := &managedAccount{cookieID: "restart-stop-error", acc: stopErrorAccount, cancel: func() {}, done: make(chan struct{})}
	stopErrorManager.accounts[stopErrorManaged.cookieID] = stopErrorManaged
	// stopErrorContext 在首次检查后通过已关闭 Done 信号报告取消。
	stopErrorContext := &stagedContext{cancelAfter: 2, done: closedDone}
	// stopError 是旧实例停止阶段失败后的重启分类错误。
	stopError := stopErrorManager.Restart(stopErrorContext, stopErrorManaged.cookieID)
	if !errors.Is(stopError, ErrRestartIncomplete) || !errors.Is(stopError, context.Canceled) {
		t.Fatalf("停止阶段错误异常: %v", stopError)
	}

	// startAllManager 是在全局停止 fencing 下验证 StartAll 忽略单账号启动错误的管理器。
	startAllManager, _, cleanup := newManagerWithAccount(t, "start-all-error", "unb=1; _m_h5_tk=t_1;")
	defer cleanup()
	startAllManager.mu.Lock()
	startAllManager.stoppingAll = true
	startAllManager.mu.Unlock()
	if // err 是 StartAll 继续处理单账号启动失败后的整体结果。
	err := startAllManager.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll 不应因单账号启动失败中断: %v", err)
	}

	// restartManager 是在全局停止 fencing 下验证新实例启动失败的管理器。
	restartManager, _, restartCleanup := newManagerWithAccount(t, "restart-start-error", "unb=1; _m_h5_tk=t_1;")
	defer restartCleanup()
	restartManager.mu.Lock()
	restartManager.stoppingAll = true
	restartManager.mu.Unlock()
	// restartErr 是新实例启动被全局 fencing 拒绝后的分类错误。
	restartErr := restartManager.Restart(context.Background(), "restart-start-error")
	if !errors.Is(restartErr, ErrRestartIncomplete) || !strings.Contains(restartErr.Error(), "启动新账号实例失败") {
		t.Fatalf("重启启动错误异常: %v", restartErr)
	}
	// beforeStartContext 在读取最新 Cookie 后才报告取消，覆盖启动新实例前的检查。
	beforeStartContext := &stagedContext{cancelAfter: 3}
	// beforeStartErr 是启动新实例前取消重启返回的分类错误。
	beforeStartErr := restartManager.Restart(beforeStartContext, "restart-start-error")
	if !errors.Is(beforeStartErr, ErrRestartIncomplete) || !errors.Is(beforeStartErr, context.Canceled) {
		t.Fatalf("启动前取消错误异常: %v", beforeStartErr)
	}
}

// TestRuntimeStatuses 验证多账号状态快照聚合，覆盖：
//   - 运行中账号（done 未关）：状态原样返回
//   - done 已关 + err=nil：覆盖为 RuntimeError + "账号服务已退出"
//   - done 已关 + err=非 context.Canceled：覆盖为 RuntimeError + err 文案
//   - done 已关 + err=context.Canceled：覆盖为 RuntimeError + "账号服务已退出"
//   - done 已关 + 状态=RuntimeAuthExpired：保持原状态（通过 session-expired 走 handleMaxFailures）
//   - done 已关 + 状态=RuntimeVerificationRequired：保持原状态（通过验证类错误 + 取消）
//
// TestRuntimeStatuses 封装TestRuntimeStatuses业务协调。
func TestRuntimeStatuses(t *testing.T) {
	// mgr、store、cleanup 用于本次流程后续判断的mgr、store、cleanup
	mgr, store, cleanup := newManagerWithAccount(t, "seed", "unb=1; _m_h5_tk=t1_1;")
	defer cleanup()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")

	// 1) 运行中账号：阻塞型 mtop 让 Run 挂起。
	runMtop := &fakeMtop{block: make(chan struct{})}
	// runCancel 用于本次流程后续判断的运行取消
	runCancel := startAccountWithMtop(t, mgr, "running", "unb=10; _m_h5_tk=t_1;", runMtop)
	defer runCancel()
	time.Sleep(50 * time.Millisecond)

	// 2) done 已关 + err=nil（手动构造 managedAccount，不跑 Run）。
	nilDoneAcc := engine.New(engine.Config{
		CookieID: "nil-done", CookieStr: "unb=2; _m_h5_tk=t_1;",
		Store: store, Handler: noopHandler{}, MTop: &fakeMtop{},
	})
	// nilDoneMA 用于本次流程后续判断的nilDoneMA
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
	// errDoneMA 用于本次流程后续判断的errDoneMA
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
	// canceledMA 用于本次流程后续判断的canceledMA
	canceledMA := &managedAccount{cookieID: "canceled", acc: canceledAcc, done: make(chan struct{}), err: context.Canceled}
	close(canceledMA.done)
	mgr.mu.Lock()
	mgr.accounts["canceled"] = canceledMA
	mgr.mu.Unlock()

	// 5) 状态=RuntimeAuthExpired（session-expired 错误让 Run 走到 handleMaxFailures 慢重试）。
	//    新行为：账号不再硬退出，而是保持 goroutine 存活、慢重试，状态停在 RuntimeAuthExpired。
	store.Cookies.Save(context.Background(), "auth-exp", "unb=5; _m_h5_tk=t_1;", admin.ID)
	// authMgr 用于本次流程后续判断的authMgr
	authMgr := mgr
	// authCancel 用于本次流程后续判断的auth取消
	authCancel := startAccountWithMtop(t, authMgr, "auth-exp", "unb=5; _m_h5_tk=t_1;",
		&fakeMtop{refreshErr: errors.New("token API 登录凭证已失效: ret=[FAIL_SYS_SESSION_EXPIRED] status=403")})
	defer authCancel()
	// 等待 Run 进入 RuntimeAuthExpired 慢重试（不再硬退出，done 不关闭）。
	if !waitForState(mgr, "auth-exp", engine.RuntimeAuthExpired, 2*time.Second) {
		t.Fatal("auth-exp 账号未在超时内进入 RuntimeAuthExpired")
	}

	// 6) done 已关 + 状态=RuntimeVerificationRequired（验证类错误 + 立即取消 ctx）。
	store.Cookies.Save(context.Background(), "verify", "unb=6; _m_h5_tk=t_1;", admin.ID)
	// verifyMtop 用于本次流程后续判断的verifyMtop
	verifyMtop := &fakeMtop{refreshErr: errors.New("FAIL_SYS_USER_VALIDATE: captcha required")}
	// verifyCancel 用于本次流程后续判断的verify取消
	verifyCancel := startAccountWithMtop(t, mgr, "verify", "unb=6; _m_h5_tk=t_1;", verifyMtop)
	// 让 Run 进入一次 refreshToken → setRuntimeError(RuntimeVerificationRequired) → sleepCtx。
	time.Sleep(80 * time.Millisecond)
	verifyCancel() // 取消 runCtx，sleepCtx 返回 ctx.Err()，Run 退出，done 关闭。
	if !waitForDone(mgr, "verify", 2*time.Second) {
		t.Fatal("verify 账号未在超时内退出")
	}

	// statuses 用于本次流程后续判断的statuses
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
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		// ma、ok 用于本次流程后续判断的ma、ok
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
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		// ma、ok 用于本次流程后续判断的ma、ok
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
	// mgr、store、cleanup 用于本次流程后续判断的mgr、store、cleanup
	mgr, store, cleanup := newManagerWithAccount(t, "restart-acc", "unb=1; _m_h5_tk=old;")
	defer cleanup()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")

	// 用阻塞 mtop 启动旧实例，使其保持运行。
	oldMtop := &fakeMtop{block: make(chan struct{})}
	// cancel 用于本次流程后续判断的取消
	cancel := startAccountWithMtop(t, mgr, "restart-acc", "unb=1; _m_h5_tk=old;", oldMtop)
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	// oldMA 用于本次流程后续判断的oldMA
	oldMA, _ := mgr.getInstanceInternal("restart-acc")
	if oldMA == nil {
		t.Fatal("旧实例应存在")
	}

	// 更新 DB cookie 为新值，模拟外部刷新。
	store.Cookies.Save(context.Background(), "restart-acc", "unb=1; _m_h5_tk=new-refreshed;", admin.ID)

	// ctx、ctxCancel 用于本次流程后续判断的ctx、ctx取消
	ctx, ctxCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ctxCancel()
	if // err 用于本次流程后续判断的err
	err := mgr.Restart(ctx, "restart-acc"); err != nil {
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
	if got := newMA.acc.CurrentCookieStr(); got != "unb=1; _m_h5_tk=new-refreshed;" {
		t.Fatalf("新实例 CookieStr=%q want new-refreshed", got)
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
	// mgr、cleanup 用于本次流程后续判断的mgr、cleanup
	mgr, _, cleanup := newManagerWithAccount(t, "seed", "unb=1; _m_h5_tk=t;")
	defer cleanup()

	// ctx、ctxCancel 用于本次流程后续判断的ctx、ctx取消
	ctx, ctxCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ctxCancel()
	// err 用于本次流程后续判断的err
	err := mgr.Restart(ctx, "no-such-account")
	if err == nil {
		t.Fatal("Restart 不存在账号应返回错误")
	}
	if !strings.Contains(err.Error(), "读取账号详情失败") {
		t.Fatalf("错误应包含'读取账号详情失败'，got %v", err)
	}
}

// TestRestartCanceledBeforeStopKeepsRunningAccount 验证已取消的重启请求不会停止仍在线的旧实例。
func TestRestartCanceledBeforeStopKeepsRunningAccount(t *testing.T) {
	// mgr、cleanup 分别是带运行中账号的测试管理器及其资源清理函数。
	mgr, _, cleanup := newManagerWithAccount(t, "restart-canceled", "unb=1; _m_h5_tk=old;")
	defer cleanup()
	// oldMtop 让旧实例保持运行，便于验证取消重启没有触发停止。
	oldMtop := &fakeMtop{block: make(chan struct{})}
	// runCancel 在测试结束时释放旧实例使用的进程生命周期 Context。
	runCancel := startAccountWithMtop(t, mgr, "restart-canceled", "unb=1; _m_h5_tk=old;", oldMtop)
	defer runCancel()
	// oldManaged 是取消重启前的权威运行实例。
	oldManaged, ok := mgr.getInstanceInternal("restart-canceled")
	if !ok || oldManaged == nil {
		t.Fatal("取消前应存在运行中的账号实例")
	}
	// restartCtx、cancel 分别是已取消的重启请求 Context 及其释放函数。
	restartCtx, cancel := context.WithCancel(context.Background())
	cancel()
	// restartErr 是取消重启返回的可分类错误。
	restartErr := mgr.Restart(restartCtx, "restart-canceled")
	if !errors.Is(restartErr, ErrRestartIncomplete) || !errors.Is(restartErr, context.Canceled) {
		t.Fatalf("Restart 取消错误=%v，期望同时包含 ErrRestartIncomplete 和 context.Canceled", restartErr)
	}
	// current、stillRunning 分别是取消后的运行实例及其存在性标记。
	current, stillRunning := mgr.getInstanceInternal("restart-canceled")
	if !stillRunning || current != oldManaged {
		t.Fatal("已取消的重启不应替换或删除旧账号实例")
	}
	select {
	case <-oldManaged.done:
		t.Fatal("已取消的重启不应停止旧账号实例")
	default:
	}
	// stopErr 是测试结束前停止旧实例的结果。
	if stopErr := mgr.StopContext(context.Background(), "restart-canceled"); stopErr != nil {
		t.Fatalf("停止测试账号: %v", stopErr)
	}
}

// TestStart_SkipsRunning 验证对运行中账号调用 Start 跳过启动。
func TestStart_SkipsRunning(t *testing.T) {
	// mgr、cleanup 用于本次流程后续判断的mgr、cleanup
	mgr, _, cleanup := newManagerWithAccount(t, "running-acc", "unb=1; _m_h5_tk=t;")
	defer cleanup()

	// runMtop 用于本次流程后续判断的运行Mtop
	runMtop := &fakeMtop{block: make(chan struct{})}
	// cancel 用于本次流程后续判断的取消
	cancel := startAccountWithMtop(t, mgr, "running-acc", "unb=1; _m_h5_tk=t;", runMtop)
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	// original 用于本次流程后续判断的original
	original, _ := mgr.getInstanceInternal("running-acc")

	// ctx、ctxCancel 用于本次流程后续判断的ctx、ctx取消
	ctx, ctxCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ctxCancel()
	if // err 用于本次流程后续判断的err
	err := mgr.Start(ctx, "running-acc", "unb=1; _m_h5_tk=different;"); err != nil {
		t.Fatalf("Start 运行中账号应返回 nil，got %v", err)
	}

	// 应仍是同一实例（未被替换）。
	after, _ := mgr.getInstanceInternal("running-acc")
	if after != original {
		t.Fatal("Start 运行中账号不应替换实例")
	}
	if // got 用于本次流程后续判断的got
	got := after.acc.CurrentCookieStr(); got != "unb=1; _m_h5_tk=t;" {
		t.Fatalf("实例 CookieStr 不应变，got %q", got)
	}

	mgr.Stop("running-acc")
}

// TestStart_RestartsExited 验证 Manager.Start 在"账号已存在且 done 已关"路径上
// 清理旧实例并重启新实例。该路径曾是双重解锁 bug（分支内 Unlock 后函数末尾再次
// Unlock），已修复：现在持锁 delete 后由函数末尾单次 Unlock。
// TestStart_RestartsExited 封装Test开始RestartsExited业务协调。
func TestStart_RestartsExited(t *testing.T) {
	// mgr、cleanup 用于本次流程后续判断的mgr、cleanup
	mgr, _, cleanup := newManagerWithAccount(t, "exited-acc", "unb=1; _m_h5_tk=t;")
	defer cleanup()

	// 注入一个已退出的 managedAccount（done 已关），无 Run goroutine。
	exitedAcc := engine.New(engine.Config{
		CookieID: "exited-acc", CookieStr: "unb=1; _m_h5_tk=old;",
		Store: mgr.store, Handler: noopHandler{}, MTop: &fakeMtop{},
	})
	// exitedMA 用于本次流程后续判断的exitedMA
	exitedMA := &managedAccount{cookieID: "exited-acc", acc: exitedAcc, done: make(chan struct{}), err: context.Canceled}
	close(exitedMA.done)
	mgr.mu.Lock()
	mgr.accounts["exited-acc"] = exitedMA
	mgr.mu.Unlock()

	// ctx、ctxCancel 用于本次流程后续判断的ctx、ctx取消
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()
	mgr.mu.Lock()
	mgr.runCtx = ctx
	mgr.mu.Unlock()
	if // err 用于本次流程后续判断的err
	err := mgr.store.Cookies.Save(ctx, "exited-acc", "unb=1; _m_h5_tk=brand-new;", 0); err != nil {
		t.Fatal(err)
	}

	if // err 用于本次流程后续判断的err
	err := mgr.Start(ctx, "exited-acc", "unb=1; _m_h5_tk=brand-new;"); err != nil {
		t.Fatalf("Start 已退出账号应成功，got %v", err)
	}

	// newMA、ok 用于本次流程后续判断的newMA、ok
	newMA, ok := mgr.getInstanceInternal("exited-acc")
	if !ok {
		t.Fatal("Start 后应存在新实例")
	}
	if newMA == exitedMA {
		t.Fatal("应替换为全新实例")
	}
	if // got 用于本次流程后续判断的got
	got := newMA.acc.CurrentCookieStr(); got != "unb=1; _m_h5_tk=brand-new;" {
		t.Fatalf("新实例 CookieStr=%q want brand-new", got)
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
// 通过提前关闭底层 sql.DB 触发运行时凭证查询错误。
// TestStartAll_LoadError 封装Test开始AllLoad错误业务协调。
func TestStartAll_LoadError(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestStore(t)
	defer cleanup()
	// mgr 用于本次流程后续判断的mgr
	mgr := NewManager(store, noopHandler{}, nil)

	// 提前关闭 DB 让 ListEnabledRuntimeCredentials 失败。
	if err := store.DB.Close(); err != nil {
		t.Fatalf("关闭 DB: %v", err)
	}

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// err 用于本次流程后续判断的err
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
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestStore(t)
	defer cleanup()
	// admin 用于本次流程后续判断的admin
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
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if // err 用于本次流程后续判断的err
	err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// 启用的应存在，禁用的不应存在。
	if _, ok := mgr.GetInstance("on1"); !ok {
		t.Fatal("on1 应被启动")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := mgr.GetInstance("on2"); !ok {
		t.Fatal("on2 应被启动")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := mgr.GetInstance("off1"); ok {
		t.Fatal("off1 已禁用不应被启动")
	}

	// StopAll 应清空全部。
	mgr.StopAll()
	// id 表示当前遍历过程中的标识
	for _, id := range []string{"on1", "on2", "off1"} {
		if // ok 用于本次流程后续判断的ok
		_, ok := mgr.GetInstance(id); ok {
			t.Fatalf("StopAll 后 %s 仍存在", id)
		}
	}
}

// TestStop_Nonexistent 验证停止不存在的账号是 no-op，不 panic。
func TestStop_Nonexistent(t *testing.T) {
	// mgr、cleanup 用于本次流程后续判断的mgr、cleanup
	mgr, _, cleanup := newManagerWithAccount(t, "seed", "unb=1; _m_h5_tk=t;")
	defer cleanup()
	// 不应 panic。
	mgr.Stop("does-not-exist")
}

// TestConcurrency 用 -race 验证多 goroutine 并发访问 GetInstance/Sender/RuntimeStatuses 不 race。
func TestConcurrency(t *testing.T) {
	// mgr、store、cleanup 用于本次流程后续判断的mgr、store、cleanup
	mgr, store, cleanup := newManagerWithAccount(t, "conc-acc", "unb=1; _m_h5_tk=t;")
	defer cleanup()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// 多个启用账号。
	for _, id := range []string{"conc-2", "conc-3"} {
		store.Cookies.Save(context.Background(), id, "unb=2; _m_h5_tk=t;", admin.ID)
		store.Cookies.SetStatus(context.Background(), id, true)
	}

	// 用阻塞 mtop 让账号保持运行，避免 Run 退出引入噪声。
	for _, id := range []string{"conc-acc", "conc-2", "conc-3"} {
		// mtopClient 用于本次流程后续判断的mtopClient
		mtopClient := &fakeMtop{block: make(chan struct{})}
		// c 用于本次流程后续判断的c
		c := startAccountWithMtop(t, mgr, id, "unb=1; _m_h5_tk=t;", mtopClient)
		defer c()
	}
	time.Sleep(60 * time.Millisecond)

	// wg 用于本次流程后续判断的wg
	var wg sync.WaitGroup
	for // i 用于本次流程后续判断的i
	i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for // j 用于本次流程后续判断的j
			j := 0; j < 100; j++ {
				_, _ = mgr.GetInstance("conc-acc")
			}
		}()
		go func() {
			defer wg.Done()
			for // j 用于本次流程后续判断的j
			j := 0; j < 100; j++ {
				_, _ = mgr.Sender("conc-2")
			}
		}()
		go func() {
			defer wg.Done()
			for // j 用于本次流程后续判断的j
			j := 0; j < 100; j++ {
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
	// ma、ok 用于本次流程后续判断的ma、ok
	ma, ok := m.accounts[cookieID]
	return ma, ok
}
