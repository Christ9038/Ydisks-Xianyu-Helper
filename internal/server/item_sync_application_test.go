package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// itemSyncRepositoryStub 是隔离 HTTP 错误映射测试的商品同步 Port 桩。
type itemSyncRepositoryStub struct {
	// owned 表示桩返回的账号归属结果。
	owned bool
	// allErr 保存全量同步桩返回的错误。
	allErr error
}

// OwnsAccount 返回预设的账号归属结果。
func (s *itemSyncRepositoryStub) OwnsAccount(_ context.Context, _ int64, _ string) (bool, error) {
	return s.owned, nil
}

// SyncAll 返回预设的全量同步错误。
func (s *itemSyncRepositoryStub) SyncAll(_ context.Context, _ itemapp.SyncQuery) (itemapp.SyncAllResult, error) {
	return itemapp.SyncAllResult{}, s.allErr
}

// SyncPage 返回无错误的空分页结果，满足同步 Port 的完整契约。
func (s *itemSyncRepositoryStub) SyncPage(_ context.Context, _ itemapp.SyncQuery) (itemapp.SyncPageResult, error) {
	return itemapp.SyncPageResult{}, nil
}

// TestSyncItemsPersistenceFailureMapsToServerError 验证本地持久化失败不会伪装成平台错误。
func TestSyncItemsPersistenceFailureMapsToServerError(t *testing.T) {
	// srv、cleanup 保存本次 HTTP 测试使用的服务和清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// persistenceErr 表示应用 Port 报告的本地写入失败。
	persistenceErr := &itemapp.SyncError{Kind: itemapp.SyncErrorPersistence, Err: errors.New("写入失败")}
	// srv.applications.itemSync 替换为可控的应用服务，隔离数据库故障映射。
	srv.applications.itemSync = itemapp.NewSyncService(&itemSyncRepositoryStub{owned: true, allErr: persistenceErr})
	// h 保存当前服务路由。
	h := srv.Router()
	// cookie 保存认证成功后的会话 Cookie。
	cookie := loginHelper(t, h)
	// req 保存发起商品全集同步的请求。
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(`{"cookie_id":"acc1"}`))
	req.AddCookie(cookie)
	// rec 保存 HTTP 响应。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
