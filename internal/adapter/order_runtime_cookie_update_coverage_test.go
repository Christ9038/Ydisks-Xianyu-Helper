package adapter

import (
	"context"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// TestOrderRuntimeCookieUpdateCoversFlatAndAuthoritativeSessions 覆盖订单运行时 Cookie 会话更新的平面与权威快照分支。
func TestOrderRuntimeCookieUpdateCoversFlatAndAuthoritativeSessions(t *testing.T) {
	// detail 是订单刷新使用的非敏感平台凭证视图。
	detail := &orderapp.PlatformRuntimeData{ID: "acc1", Value: "sid=old", MetadataJSON: `{"existing":true}`}
	// flatContext、flatSession 保存未声明完整 Jar 的兼容会话。
	flatContext, flatSession := mtop.WithFlatCookieSession(context.Background(), detail.Value)
	_ = flatContext
	// flatUpdate 保存平面会话未发生变化时的零更新结果。
	flatUpdate := orderCookieUpdate(detail, flatSession)
	if flatUpdate.Handled || flatUpdate.Changed || flatUpdate.Value != detail.Value {
		t.Fatalf("平面未变化会话异常=%+v", flatUpdate)
	}

	// authoritativeContext、authoritativeSession 保存已确认完整 Jar 的权威会话。
	authoritativeContext, authoritativeSession := mtop.WithCookieSnapshot(context.Background(), []cookierefresh.BrowserCookie{{Name: "sid", Value: "old", Domain: ".goofish.com", Path: "/"}})
	_ = authoritativeContext
	// unchangedUpdate 保存权威快照未变化时的应用层更新结果。
	unchangedUpdate := orderCookieUpdate(detail, authoritativeSession)
	if !unchangedUpdate.Handled || unchangedUpdate.Changed || unchangedUpdate.MetadataJSON == "" {
		t.Fatalf("权威未变化会话异常=%+v", unchangedUpdate)
	}
	authoritativeSession.ReplaceSnapshot([]cookierefresh.BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	// changedUpdate 保存权威快照变化后的应用层更新结果。
	changedUpdate := orderCookieUpdate(detail, authoritativeSession)
	if !changedUpdate.Handled || !changedUpdate.Changed || changedUpdate.Value == "" || changedUpdate.MetadataJSON == "" {
		t.Fatalf("权威变化会话异常=%+v", changedUpdate)
	}

	// emptyUpdate 保存缺失凭证视图或会话时的安全零值结果。
	emptyUpdate := orderCookieUpdate(nil, nil)
	if emptyUpdate.Handled || emptyUpdate.Changed || emptyUpdate.Value != "" {
		t.Fatalf("空会话更新异常=%+v", emptyUpdate)
	}
}
