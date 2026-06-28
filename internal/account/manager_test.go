package account

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

type noopHandler struct{}

func (noopHandler) HandleChatMessage(context.Context, engine.ChatMessage) error     { return nil }
func (noopHandler) HandleSystemMessage(context.Context, engine.SystemMessage) error { return nil }
func (noopHandler) OnPasswordLoginRefresh(context.Context, string) bool             { return false }

// TestManager_StartStopAll 验证从 DB 加载账号、启停、GetInstance、RunningAccounts。
// 用无效 cookie 让账号快速进入重连等待（不会真正连上），验证管理逻辑而非网络。
func TestManager_StartStopAll(t *testing.T) {
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

	// acc1/acc2 应在运行，acc3 不应。
	running := map[string]bool{}
	for _, id := range mgr.RunningAccounts() {
		running[id] = true
	}
	if !running["acc1"] || !running["acc2"] {
		t.Fatalf("acc1/acc2 应运行，got %v", running)
	}
	if running["acc3"] {
		t.Fatal("acc3 已禁用不应启动")
	}

	// GetInstance 可取到。
	if acc, ok := mgr.GetInstance("acc1"); !ok || acc == nil {
		t.Fatal("GetInstance(acc1) 失败")
	}
	if _, ok := mgr.GetInstance("acc3"); ok {
		t.Fatal("acc3 不应有实例")
	}

	// StopAll 应能干净停止。
	mgr.StopAll()
	// 给 goroutine 退出时间。
	time.Sleep(100 * time.Millisecond)
	if len(mgr.RunningAccounts()) != 0 {
		t.Fatalf("StopAll 后应无运行账号，got %v", mgr.RunningAccounts())
	}
}
