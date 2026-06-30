package account

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/engine"
)

type noopHandler struct{}

func (noopHandler) HandleChatMessage(context.Context, engine.ChatMessage) error     { return nil }
func (noopHandler) HandleSystemEvent(context.Context, automation.Task) error        { return nil }
func (noopHandler) OnPasswordLoginRefresh(context.Context, string) bool             { return false }

// TestManagerStartStop 验证从 DB 加载账号、启停和 GetInstance。
// 用无效 cookie 让账号快速进入重连等待（不会真正连上），验证管理逻辑而非网络。
func TestManagerStartStop(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	store := db.NewStore(d)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")

	// 两个启用 + 一个禁用的账号。
	store.Cookies.Save(context.Background(), "acc1", "unb=1; _m_h5_tk=t1_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc1", true)
	store.Cookies.Save(context.Background(), "acc2", "unb=2; _m_h5_tk=t2_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc2", true)
	store.Cookies.Save(context.Background(), "acc3", "unb=3; _m_h5_tk=t3_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "acc3", false)

	mgr := NewManager(store, noopHandler{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// acc1/acc2 应有运行实例，acc3 不应。
	for _, id := range []string{"acc1", "acc2"} {
		if acc, ok := mgr.GetInstance(id); !ok || acc == nil {
			t.Fatalf("GetInstance(%s) 失败", id)
		}
	}

	// GetInstance 可取到。
	if acc, ok := mgr.GetInstance("acc1"); !ok || acc == nil {
		t.Fatal("GetInstance(acc1) 失败")
	}
	if _, ok := mgr.GetInstance("acc3"); ok {
		t.Fatal("acc3 不应有实例")
	}

	// Stop 应能干净停止。
	mgr.Stop("acc1")
	mgr.Stop("acc2")
	if _, ok := mgr.GetInstance("acc1"); ok {
		t.Fatal("Stop 后 acc1 仍存在")
	}
	if _, ok := mgr.GetInstance("acc2"); ok {
		t.Fatal("Stop 后 acc2 仍存在")
	}
}

// TestManagerStopAll 验证 StopAll 停止所有运行中的账号，用于进程优雅退出。
func TestManagerStopAll(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	store := db.NewStore(d)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// 三个启用账号。
	for _, id := range []string{"a1", "a2", "a3"} {
		store.Cookies.Save(context.Background(), id, "unb=1; _m_h5_tk=t_1;", admin.ID)
		store.Cookies.SetStatus(context.Background(), id, true)
	}

	mgr := NewManager(store, noopHandler{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	for _, id := range []string{"a1", "a2", "a3"} {
		if _, ok := mgr.GetInstance(id); !ok {
			t.Fatalf("GetInstance(%s) 失败", id)
		}
	}

	// StopAll 应清空全部实例。
	mgr.StopAll()
	for _, id := range []string{"a1", "a2", "a3"} {
		if _, ok := mgr.GetInstance(id); ok {
			t.Fatalf("StopAll 后 %s 仍存在", id)
		}
	}

	// StopAll 在空状态下不应 panic / 死锁。
	mgr.StopAll()
}
