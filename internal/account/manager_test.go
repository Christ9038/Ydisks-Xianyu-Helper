package account

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// noopHandler 用于本次流程后续判断的noopHandler
type noopHandler struct{}

// HandleChatMessage 处理聊天消息。
func (noopHandler) HandleChatMessage(context.Context, engine.ChatMessage) error { return nil }

// HandleSystemEvent 处理系统Event。
func (noopHandler) HandleSystemEvent(context.Context, automation.Task) error { return nil }

// OnPasswordLoginRefresh 封装On密码登录Refresh业务协调。
func (noopHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 封装On账号Alert业务协调。
func (noopHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// TestManagerStartStop 验证从 DB 加载账号、启停和 GetInstance。
// 用无效 cookie 让账号快速进入重连等待（不会真正连上），验证管理逻辑而非网络。
// TestManagerStartStop 封装TestManager开始Stop业务协调。
func TestManagerStartStop(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	// store 用于本次流程后续判断的store
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")

	// 两个启用 + 一个禁用的账号。
	store.Cookies.Save(context.Background(), "acc1", "unb=1; _m_h5_tk=t1_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc1", true)
	store.Cookies.Save(context.Background(), "acc2", "unb=2; _m_h5_tk=t2_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc2", true)
	store.Cookies.Save(context.Background(), "acc3", "unb=3; _m_h5_tk=t3_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc3", false)

	// mgr 用于本次流程后续判断的mgr
	mgr := NewManager(store, noopHandler{}, nil)
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if // err 用于本次流程后续判断的err
	err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// acc1/acc2 应有运行实例，acc3 不应。
	for _, id := range []string{"acc1", "acc2"} {
		if // acc、ok 用于本次流程后续判断的acc、ok
		acc, ok := mgr.GetInstance(id); !ok || acc == nil {
			t.Fatalf("GetInstance(%s) 失败", id)
		}
	}

	// GetInstance 可取到。
	if acc, ok := mgr.GetInstance("acc1"); !ok || acc == nil {
		t.Fatal("GetInstance(acc1) 失败")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := mgr.GetInstance("acc3"); ok {
		t.Fatal("acc3 不应有实例")
	}

	// Stop 应能干净停止。
	mgr.Stop("acc1")
	mgr.Stop("acc2")
	if // ok 用于本次流程后续判断的ok
	_, ok := mgr.GetInstance("acc1"); ok {
		t.Fatal("Stop 后 acc1 仍存在")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := mgr.GetInstance("acc2"); ok {
		t.Fatal("Stop 后 acc2 仍存在")
	}
}

// TestManagerStoppingFence 验证账号删除期间禁止并发启动，并支持失败后的 fencing 释放。
func TestManagerStoppingFence(t *testing.T) {
	// mgr 是仅用于验证停止 fencing 状态机的账号管理器。
	mgr := NewManager(nil, noopHandler{}, nil)
	if !mgr.BeginStopping("fenced") {
		t.Fatal("首次建立停止 fencing 应成功")
	}
	if mgr.BeginStopping("fenced") {
		t.Fatal("重复建立停止 fencing 应被拒绝")
	}
	// err 表示 fencing 期间尝试启动账号的结果。
	err := mgr.Start(context.Background(), "fenced", "cookie")
	if err == nil {
		t.Fatal("停止 fencing 期间不应允许启动账号")
	}
	mgr.EndStopping("fenced")
	if !mgr.BeginStopping("fenced") {
		t.Fatal("释放 fencing 后应允许重新建立停止 fencing")
	}
	mgr.EndStopping("fenced")
}

// TestManagerGlobalStoppingFence 验证全量关闭期间不允许新的账号运行实例进入管理器。
func TestManagerGlobalStoppingFence(t *testing.T) {
	// mgr 是只验证生命周期 fencing 的管理器，不需要数据库或平台处理器。
	mgr := NewManager(nil, noopHandler{}, nil)
	mgr.mu.Lock()
	// stoppingAll 模拟 StopAllContext 已经建立的全局关闭屏障。
	mgr.stoppingAll = true
	mgr.mu.Unlock()

	// err 表示全局关闭期间尝试启动账号得到的拒绝原因。
	err := mgr.Start(context.Background(), "during-stop", "cookie")
	if err == nil {
		t.Fatal("全量停止期间不应允许启动账号")
	}
	if !strings.Contains(err.Error(), "正在停止") {
		t.Fatalf("全量停止错误=%v，未说明停止 fencing", err)
	}
}

// TestManagerConcurrentStartCreatesSingleManagedInstance 封装TestManagerConcurrent开始CreatesSingleManagedInstance业务协调。
func TestManagerConcurrentStartCreatesSingleManagedInstance(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// database、err 用于本次流程后续判断的database、err
	database, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// store 用于本次流程后续判断的store
	store := db.NewStore(database, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "same", "unb=1; _m_h5_tk=t_1;", admin.ID)

	// mgr 用于本次流程后续判断的mgr
	mgr := NewManager(store, noopHandler{}, nil)
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// wg 用于本次流程后续判断的wg
	var wg sync.WaitGroup
	for // i 用于本次流程后续判断的i
	i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if // err 用于本次流程后续判断的err
			err := mgr.Start(ctx, "same", "unb=1; _m_h5_tk=t_1;"); err != nil {
				t.Errorf("Start: %v", err)
			}
		}()
	}
	wg.Wait()
	mgr.mu.Lock()
	// count 用于本次流程后续判断的数量
	count := len(mgr.accounts)
	mgr.mu.Unlock()
	if count != 1 {
		t.Fatalf("managed instances=%d want 1", count)
	}
	mgr.Stop("same")
}

// TestManagerStopAll 验证 StopAll 停止所有运行中的账号，用于进程优雅退出。
func TestManagerStopAll(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	// store 用于本次流程后续判断的store
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// 三个启用账号。
	for _, id := range []string{"a1", "a2", "a3"} {
		store.Cookies.Save(context.Background(), id, "unb=1; _m_h5_tk=t_1;", admin.ID)
		store.Cookies.SetStatus(context.Background(), id, true)
	}

	// mgr 用于本次流程后续判断的mgr
	mgr := NewManager(store, noopHandler{}, nil)
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if // err 用于本次流程后续判断的err
	err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	// id 表示当前遍历过程中的标识
	for _, id := range []string{"a1", "a2", "a3"} {
		if // ok 用于本次流程后续判断的ok
		_, ok := mgr.GetInstance(id); !ok {
			t.Fatalf("GetInstance(%s) 失败", id)
		}
	}

	// StopAll 应清空全部实例。
	mgr.StopAll()
	// id 表示当前遍历过程中的标识
	for _, id := range []string{"a1", "a2", "a3"} {
		if // ok 用于本次流程后续判断的ok
		_, ok := mgr.GetInstance(id); ok {
			t.Fatalf("StopAll 后 %s 仍存在", id)
		}
	}

	// StopAll 在空状态下不应 panic / 死锁。
	mgr.StopAll()
}
