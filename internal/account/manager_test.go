package account

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// noopHandler 保存noopHandler，供当前处理流程使用
type noopHandler struct{}

// HandleChatMessage 处理聊天消息。
func (noopHandler) HandleChatMessage(context.Context, engine.ChatMessage) error { return nil }

// HandleSystemEvent 处理系统Event。
func (noopHandler) HandleSystemEvent(context.Context, automation.Task) error { return nil }

// OnPasswordLoginRefresh 负责On密码登录Refresh相关处理。
func (noopHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 负责On账号Alert相关处理。
func (noopHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// TestManagerStartStop 验证从 DB 加载账号、启停和 GetInstance。
// 用无效 cookie 让账号快速进入重连等待（不会真正连上），验证管理逻辑而非网络。
// TestManagerStartStop 负责TestManager开始Stop相关处理。
func TestManagerStartStop(t *testing.T) {
	// dbPath 保存db路径，供当前处理流程使用
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d、err 保存d、err，供当前处理流程使用
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	// store 保存store，供当前处理流程使用
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")

	// 两个启用 + 一个禁用的账号。
	store.Cookies.Save(context.Background(), "acc1", "unb=1; _m_h5_tk=t1_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc1", true)
	store.Cookies.Save(context.Background(), "acc2", "unb=2; _m_h5_tk=t2_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc2", true)
	store.Cookies.Save(context.Background(), "acc3", "unb=3; _m_h5_tk=t3_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc3", false)

	// mgr 保存mgr，供当前处理流程使用
	mgr := NewManager(store, noopHandler{}, nil)
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if // err 保存err，供当前处理流程使用
	err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// acc1/acc2 应有运行实例，acc3 不应。
	for _, id := range []string{"acc1", "acc2"} {
		if // acc、ok 保存acc、ok，供当前处理流程使用
		acc, ok := mgr.GetInstance(id); !ok || acc == nil {
			t.Fatalf("GetInstance(%s) 失败", id)
		}
	}

	// GetInstance 可取到。
	if acc, ok := mgr.GetInstance("acc1"); !ok || acc == nil {
		t.Fatal("GetInstance(acc1) 失败")
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := mgr.GetInstance("acc3"); ok {
		t.Fatal("acc3 不应有实例")
	}

	// Stop 应能干净停止。
	mgr.Stop("acc1")
	mgr.Stop("acc2")
	if // ok 保存ok，供当前处理流程使用
	_, ok := mgr.GetInstance("acc1"); ok {
		t.Fatal("Stop 后 acc1 仍存在")
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := mgr.GetInstance("acc2"); ok {
		t.Fatal("Stop 后 acc2 仍存在")
	}
}

// TestManagerConcurrentStartCreatesSingleManagedInstance 负责TestManagerConcurrent开始CreatesSingleManagedInstance相关处理。
func TestManagerConcurrentStartCreatesSingleManagedInstance(t *testing.T) {
	// dbPath 保存db路径，供当前处理流程使用
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// database、err 保存database、err，供当前处理流程使用
	database, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// store 保存store，供当前处理流程使用
	store := db.NewStore(database, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "same", "unb=1; _m_h5_tk=t_1;", admin.ID)

	// mgr 保存mgr，供当前处理流程使用
	mgr := NewManager(store, noopHandler{}, nil)
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// wg 保存wg，供当前处理流程使用
	var wg sync.WaitGroup
	for // i 保存i，供当前处理流程使用
	i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if // err 保存err，供当前处理流程使用
			err := mgr.Start(ctx, "same", "unb=1; _m_h5_tk=t_1;"); err != nil {
				t.Errorf("Start: %v", err)
			}
		}()
	}
	wg.Wait()
	mgr.mu.Lock()
	// count 保存数量，供当前处理流程使用
	count := len(mgr.accounts)
	mgr.mu.Unlock()
	if count != 1 {
		t.Fatalf("managed instances=%d want 1", count)
	}
	mgr.Stop("same")
}

// TestManagerStopAll 验证 StopAll 停止所有运行中的账号，用于进程优雅退出。
func TestManagerStopAll(t *testing.T) {
	// dbPath 保存db路径，供当前处理流程使用
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d、err 保存d、err，供当前处理流程使用
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	// store 保存store，供当前处理流程使用
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// 三个启用账号。
	for _, id := range []string{"a1", "a2", "a3"} {
		store.Cookies.Save(context.Background(), id, "unb=1; _m_h5_tk=t_1;", admin.ID)
		store.Cookies.SetStatus(context.Background(), id, true)
	}

	// mgr 保存mgr，供当前处理流程使用
	mgr := NewManager(store, noopHandler{}, nil)
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if // err 保存err，供当前处理流程使用
	err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	// id 表示当前遍历过程中的标识
	for _, id := range []string{"a1", "a2", "a3"} {
		if // ok 保存ok，供当前处理流程使用
		_, ok := mgr.GetInstance(id); !ok {
			t.Fatalf("GetInstance(%s) 失败", id)
		}
	}

	// StopAll 应清空全部实例。
	mgr.StopAll()
	// id 表示当前遍历过程中的标识
	for _, id := range []string{"a1", "a2", "a3"} {
		if // ok 保存ok，供当前处理流程使用
		_, ok := mgr.GetInstance(id); ok {
			t.Fatalf("StopAll 后 %s 仍存在", id)
		}
	}

	// StopAll 在空状态下不应 panic / 死锁。
	mgr.StopAll()
}
