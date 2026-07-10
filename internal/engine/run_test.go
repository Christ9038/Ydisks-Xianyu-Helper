package engine

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/ws"
)

// fakeRunMtop 返回成功 token，不触网。
type fakeRunMtop struct{ token string }

func (f *fakeRunMtop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}
func (f *fakeRunMtop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return true, nil, "", nil
}
func (f *fakeRunMtop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeRunMtop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeRunMtop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}
func (f *fakeRunMtop) RefreshTokenWithDeviceIDContext(_ context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	return &mtop.RefreshResult{AccessToken: f.token}, nil
}

type fakeFailTokenMtop struct{ err error }

func (f *fakeFailTokenMtop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}
func (f *fakeFailTokenMtop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return true, nil, "", nil
}
func (f *fakeFailTokenMtop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeFailTokenMtop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeFailTokenMtop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}
func (f *fakeFailTokenMtop) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	return nil, f.err
}

// fakeWSConn 实现 WSConn，可控地投递消息并阻塞到 ctx 取消。
type fakeWSConn struct {
	mu            sync.Mutex
	closed        bool
	tokens        []string
	sentTexts     []string
	sentImages    []string
	heartbeatDone chan struct{}
	// onReceive 在 ReceiveLoop 启动时被调用，参数是 onMessage 回调，便于测试投递消息。
	onReceive func(onMessage func(map[string]any))
	// recvBlock 控制 ReceiveLoop 是否阻塞到 ctx 取消（默认 true）。
	recvBlock bool
}

func (f *fakeWSConn) HeartbeatLoop(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	if f.heartbeatDone != nil {
		close(f.heartbeatDone)
	}
	return ctx.Err()
}

func (f *fakeWSConn) ReceiveLoop(ctx context.Context, onMessage func(map[string]any)) error {
	if f.onReceive != nil {
		f.onReceive(onMessage)
	}
	if f.recvBlock {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (f *fakeWSConn) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeWSConn) SetAccessToken(token string) {
	f.mu.Lock()
	f.tokens = append(f.tokens, token)
	f.mu.Unlock()
}

func (f *fakeWSConn) SendText(_ context.Context, _, _, _, text string) error {
	f.mu.Lock()
	f.sentTexts = append(f.sentTexts, text)
	f.mu.Unlock()
	return nil
}

func (f *fakeWSConn) SendImage(_ context.Context, _, _, _, url string, _, _ int) error {
	f.mu.Lock()
	f.sentImages = append(f.sentImages, url)
	f.mu.Unlock()
	return nil
}

// fakeDialer 按预设序列返回连接或错误，第 N 次（1-based）调用返回 dialResults[N-1]。
type fakeDialer struct {
	mu      sync.Mutex
	results []dialResult // 每次.Dial 的结果
	calls   int
	conns   []*fakeWSConn
	lastCfg ws.Config // 记录最后一次 Dial 的配置（含 AccessToken）
}

type dialResult struct {
	conn *fakeWSConn
	err  error
}

func (d *fakeDialer) Dial(_ context.Context, cfg ws.Config, _ *slog.Logger) (WSConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastCfg = cfg
	idx := d.calls
	d.calls++
	if idx >= len(d.results) {
		// 超出预设：返回最后一个 conn，避免无限重连耗尽测试。
		if len(d.results) > 0 {
			last := d.results[len(d.results)-1]
			return last.conn, last.err
		}
		return nil, nil
	}
	r := d.results[idx]
	if r.conn != nil {
		d.conns = append(d.conns, r.conn)
	}
	return r.conn, r.err
}

// newRunAccount 构造一个用 fakeMtop + fakeDialer 的 Account，不触网。
func newRunAccount(t *testing.T, mtopClient mtop.Client) (*Account, *recordingHandler, *db.Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "run.db")
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "cid", true)

	h := &recordingHandler{}
	acc := New(Config{
		CookieID:  "cid",
		CookieStr: "unb=123; _m_h5_tk=tk_1;",
		Store:     store,
		Handler:   h,
		MTop:      mtopClient,
	})
	return acc, h, store, func() { d.Close() }
}

// TestRun_ConnectsAndDispatchesMessage 验证 Run 主循环：
// 刷新 token → 拨号 WS → ReceiveLoop 投递消息 → dispatch 进 handler 防抖链 → ctx 取消后优雅退出。
func TestRun_ConnectsAndDispatchesMessage(t *testing.T) {
	acc, h, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()

	conn := &fakeWSConn{
		recvBlock: true,
		onReceive: func(onMessage func(map[string]any)) {
			// 投递一条普通聊天消息。
			onMessage(map[string]any{
				"1": map[string]any{
					"2": "chat-1@goofish",
					"10": map[string]any{
						"reminderContent": "你好",
						"senderUserId":    "buyer-1",
						"senderNick":      "买家",
						"reminderUrl":     "fleamarket://message_chat?itemId=item-1&peerUserId=buyer-1",
					},
				},
			})
		},
	}
	acc.wsDialer = &fakeDialer{results: []dialResult{{conn: conn}}}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- acc.Run(ctx) }()

	// 等待防抖延迟后消息进入 handler。
	deadline := time.After(3 * time.Second)
	for {
		h.mu.Lock()
		n := len(h.chats)
		h.mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("Run 未在 3s 内投递消息到 handler，chats=%d", n)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 初始 token 应通过 ws.Config.AccessToken 传给 Dial（非 SetAccessToken）。
	d := acc.wsDialer.(*fakeDialer)
	d.mu.Lock()
	gotToken := d.lastCfg.AccessToken == "tok-1"
	d.mu.Unlock()
	if !gotToken {
		t.Fatal("Dial 未收到 ws.Config.AccessToken=tok-1")
	}

	// 取消 ctx → Run 应退出。
	cancel()
	select {
	case err := <-runDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run 退出 err=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run 未在 ctx 取消后 3s 内退出")
	}
}

// TestRun_DialFailureIncrementsFailures 拨号失败时 connFailures 递增，
// ctx 在重试 sleep 期间取消 → Run 返回 ctx.Err。
func TestRun_DialFailureIncrementsFailures(t *testing.T) {
	acc, _, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()

	// 拨号始终失败。
	acc.wsDialer = &fakeDialer{results: []dialResult{{err: errFakeDial}}}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- acc.Run(ctx) }()

	// 等待首次拨号失败 + 进入重试 sleep。
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case <-runDone:
		// Run 应已退出。
	case <-time.After(3 * time.Second):
		t.Fatal("拨号失败 + ctx 取消后 Run 未退出")
	}
	acc.mu.Lock()
	failures := acc.connFailures
	acc.mu.Unlock()
	if failures < 1 {
		t.Fatalf("拨号失败应使 connFailures>=1，got %d", failures)
	}
}

// TestRun_ReceiveLoopEndsTriggersReconnect ReceiveLoop 返回后 Run 应重连（再次拨号成功）。
// 断线后 retryDelay 默认 5s；token 冷却 1min 会阻塞第二次 refresh，测试期间后台重置冷却。
func TestRun_ReceiveLoopEndsTriggersReconnect(t *testing.T) {
	acc, _, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	acc.lastTokenRefresh = time.Time{}

	conn1 := &fakeWSConn{recvBlock: false} // 立即返回，模拟断线
	conn2 := &fakeWSConn{recvBlock: true}  // 第二次连上后阻塞
	d := &fakeDialer{results: []dialResult{{conn: conn1}, {conn: conn2}}}
	acc.wsDialer = d

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 后台重置 token 冷却，避免第二次 refreshToken sleep 1 分钟（仅测试用）。
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				acc.mu.Lock()
				acc.lastTokenRefresh = time.Time{}
				acc.mu.Unlock()
			}
		}
	}()

	runDone := make(chan error, 1)
	go func() { runDone <- acc.Run(ctx) }()

	// 等待第二次拨号发生（首次断线后 retryDelay 5s + 重连）。
	deadline := time.After(8 * time.Second)
	for {
		d.mu.Lock()
		calls := d.calls
		d.mu.Unlock()
		if calls >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Run 未在 8s 内重连，calls=%d", calls)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("重连后 ctx 取消 Run 未退出")
	}
}

// TestRun_DisabledAccountExits 账号被禁用时 Run 立即退出。
func TestRun_DisabledAccountExits(t *testing.T) {
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	// 禁用账号。
	store.Cookies.SetStatus(context.Background(), "cid", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("禁用账号 Run 应返回 nil，got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("禁用账号 Run 应立即退出")
	}
}

func TestRun_TokenFetchThresholdDisablesAccount(t *testing.T) {
	acc, _, store, cleanup := newRunAccount(t, &fakeFailTokenMtop{err: errFakeDial})
	defer cleanup()
	h := &failingRefreshHandler{}
	acc.handler = h
	acc.mu.Lock()
	acc.tokenFetchFailures = TokenFetchDisableThreshold - 1
	acc.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- acc.Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("threshold disable should exit nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("token failure threshold should disable without retry sleep")
	}
	if store.Cookies.GetStatus(context.Background(), "cid") {
		t.Fatal("token failure threshold should disable account")
	}
	if len(h.events) == 0 || h.events[len(h.events)-1] != EventAccountDisabled {
		t.Fatalf("disable event not emitted: events=%+v alerts=%+v", h.events, h.alerts)
	}
}

var errFakeDial = fakeDialErr{}

type fakeDialErr struct{}

func (fakeDialErr) Error() string { return "fake dial failure" }
