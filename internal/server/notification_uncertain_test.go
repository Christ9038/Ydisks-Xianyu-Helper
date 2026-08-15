package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestUncertainNotificationEndpointsEnforceScope 验证用户与管理员 uncertain 查询的隔离和脱敏边界。
func TestUncertainNotificationEndpointsEnforceScope(t *testing.T) {
	// srv、store、cleanup 保存 HTTP 测试服务、数据库和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存数据库夹具使用的根上下文。
	ctx := context.Background()
	// userCreated、err 保存普通用户创建结果和数据库错误。
	userCreated, err := store.Users.Create(ctx, "uncertain-http-user", "uncertain-http@example.com", "pw")
	if err != nil || !userCreated {
		t.Fatalf("create user: created=%v err=%v", userCreated, err)
	}
	// user、err 保存普通用户实体和读取错误。
	user, err := store.Users.GetByUsername(ctx, "uncertain-http-user")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	// createChannel 创建指定用户拥有的通知渠道并返回渠道标识。
	createChannel := func(userID int64) int64 {
		// result、err 保存渠道插入结果和数据库错误。
		result, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,?,?)`, "ops", "webhook", `{}`, 1, userID)
		if err != nil {
			t.Fatalf("create channel: %v", err)
		}
		// channelID、err 保存渠道主键和读取主键错误。
		channelID, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("channel id: %v", err)
		}
		return channelID
	}
	// adminChannelID、userChannelID 保存管理员与普通用户各自拥有的渠道。
	adminChannelID := createChannel(1)
	// userChannelID 保存普通用户拥有的通知渠道标识。
	userChannelID := createChannel(user.ID)
	// enqueueUncertain 写入包含敏感正文的通知并推进到 uncertain 状态。
	enqueueUncertain := func(channelID int64, eventType string) {
		// body 保存仅供 worker 的测试正文，验证 HTTP 响应不会返回它。
		body := "绝不能从接口泄露的通知正文 token=super-secret"
		// err 保存 outbox 写入错误。
		if err := store.Notifications.EnqueueOutbox(ctx, []db.NotificationOutboxInput{{ChannelID: channelID, EventType: eventType, Body: body}}); err != nil {
			t.Fatalf("enqueue outbox: %v", err)
		}
		// workerToken 保存当前测试 worker 的租约标识。
		workerToken := "http-" + eventType
		// claimed、err 保存 worker 抢占结果和数据库错误。
		claimed, err := store.Notifications.ClaimOutbox(ctx, workerToken, time.Unix(100, 0), 10)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim outbox: messages=%+v err=%v", claimed, err)
		}
		// marked、err 保存 uncertain 状态转移结果和数据库错误。
		marked, err := store.Notifications.MarkOutboxUncertain(ctx, claimed[0].ID, workerToken, "credential=should-not-leak")
		if err != nil || !marked {
			t.Fatalf("mark uncertain: marked=%v err=%v", marked, err)
		}
	}
	enqueueUncertain(adminChannelID, "admin-event")
	enqueueUncertain(userChannelID, "user-event")

	// handler 保存当前服务的 HTTP 路由处理器。
	handler := srv.Router()
	// userCookie 保存普通用户登录后的会话 Cookie。
	userCookie := loginAsHelper(t, handler, "uncertain-http-user", "pw")
	// userReq、userRec 保存普通用户查询请求及响应。
	userReq := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/outbox/uncertain?limit=10", nil)
	userReq.AddCookie(userCookie)
	// userRec 保存普通用户查询响应。
	userRec := httptest.NewRecorder()
	handler.ServeHTTP(userRec, userReq)
	if userRec.Code != http.StatusOK {
		t.Fatalf("user uncertain status=%d body=%s", userRec.Code, userRec.Body.String())
	}
	// userResponse 保存普通用户可见的非敏感通知摘要。
	var userResponse notificationUncertainOutboxResponse
	// decodeErr 保存解析普通用户响应时的 JSON 错误。
	if decodeErr := json.Unmarshal(userRec.Body.Bytes(), &userResponse); decodeErr != nil {
		t.Fatalf("decode user response: %v", decodeErr)
	}
	if userResponse.Total != 1 || len(userResponse.Items) != 1 || userResponse.Items[0].EventType != "user-event" || userResponse.Items[0].OwnerUserID != 0 {
		t.Fatalf("user response violates scope: %+v", userResponse)
	}
	if strings.Contains(userRec.Body.String(), "super-secret") || strings.Contains(userRec.Body.String(), "should-not-leak") {
		t.Fatal("user uncertain response leaked notification body or error")
	}

	// adminCookie 保存管理员登录后的会话 Cookie。
	adminCookie := loginHelper(t, handler)
	// adminReq、adminRec 保存管理员查询请求及响应。
	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/notifications/outbox/uncertain?limit=10", nil)
	adminReq.AddCookie(adminCookie)
	// adminRec 保存管理员查询响应。
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin uncertain status=%d body=%s", adminRec.Code, adminRec.Body.String())
	}
	// adminResponse 保存管理员可见的全局摘要。
	var adminResponse notificationUncertainOutboxResponse
	// decodeErr 保存解析管理员响应时的 JSON 错误。
	if decodeErr := json.Unmarshal(adminRec.Body.Bytes(), &adminResponse); decodeErr != nil {
		t.Fatalf("decode admin response: %v", decodeErr)
	}
	if adminResponse.Total != 2 || len(adminResponse.Items) != 2 {
		t.Fatalf("admin response missing global items: %+v", adminResponse)
	}
	// item 表示当前遍历到的管理员通知摘要。
	for _, item := range adminResponse.Items {
		if item.OwnerUserID == 0 || !item.HasError {
			t.Fatalf("admin response missing safe metadata: %+v", item)
		}
	}
	if strings.Contains(adminRec.Body.String(), "super-secret") || strings.Contains(adminRec.Body.String(), "should-not-leak") {
		t.Fatal("admin uncertain response leaked notification body or error")
	}

	// forbiddenReq、forbiddenRec 保存普通用户访问管理员接口的请求及响应。
	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/notifications/outbox/uncertain", nil)
	forbiddenReq.AddCookie(userCookie)
	// forbiddenRec 保存普通用户访问管理员接口的响应。
	forbiddenRec := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("non-admin admin uncertain status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}
}
