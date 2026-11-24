package db

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// TestMultiDB_AgentRemoteSessions 在各方言上验证平台会话标识映射：保存读回、覆盖更新、按会话隔离、删除、
// 过期清理与账号删除级联，覆盖迁移 00069 的最终 schema。
func TestMultiDB_AgentRemoteSessions(t *testing.T) {
	// target 是当前执行回归的数据库方言目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的目标副本，避免循环变量复用。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 是当前方言内全部操作共用的上下文。
			ctx := context.Background()
			// suffix 保证外部数据库重复执行时业务标识唯一。
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// userName 是测试用户名；created、err 是创建结果。
			userName := target.name + "_ars_" + suffix
			// created 表示用户是否创建成功；err 是该步骤的错误，期望为空或为被测的特定错误。
			if created, err := target.store.Users.Create(ctx, userName, userName+"@e.com", "pw"); err != nil || !created {
				t.Fatalf("create user: created=%v err=%v", created, err)
			}
			// user、err 是读回的用户，用于取得主键。
			user, err := target.store.Users.GetByUsername(ctx, userName)
			if err != nil {
				t.Fatal(err)
			}
			// cookieID 是测试账号标识。
			cookieID := "ars_" + suffix
			if err = target.store.Cookies.Save(ctx, cookieID, "unb=1", user.ID); err != nil {
				t.Fatal(err)
			}
			// sessions 是被测仓储；clock 是可调的固定时钟，用于验证过期清理。
			sessions := target.store.AgentRemoteSessions
			// clock 是可调的固定时钟。
			clock := time.Now()
			sessions.now = func() time.Time { return clock }
			defer func() { sessions.now = nil }()

			// getErr 是读取会话标识的错误；got 是读取或解析得到的实际值，用于与期望值比对。
			if got, getErr := sessions.Get(ctx, cookieID, "chat-1"); getErr != nil || got != "" {
				t.Fatalf("未保存时应返回空串: %q err=%v", got, getErr)
			}
			// putErr 是保存会话映射的错误，期望为空。
			if putErr := sessions.Put(ctx, cookieID, "chat-1", "conv-a"); putErr != nil {
				t.Fatal(putErr)
			}
			// putErr 是保存会话映射的错误，期望为空。
			if putErr := sessions.Put(ctx, cookieID, "chat-2", "conv-b"); putErr != nil {
				t.Fatal(putErr)
			}
			// got 是读取或解析得到的实际值，用于与期望值比对。
			if got, _ := sessions.Get(ctx, cookieID, "chat-1"); got != "conv-a" {
				t.Fatalf("读回错误: %q", got)
			}
			// 覆盖更新同一会话。
			if putErr := sessions.Put(ctx, cookieID, "chat-1", "conv-a2"); putErr != nil {
				t.Fatal(putErr)
			}
			// got 是读取或解析得到的实际值，用于与期望值比对。
			if got, _ := sessions.Get(ctx, cookieID, "chat-1"); got != "conv-a2" {
				t.Fatalf("覆盖更新错误: %q", got)
			}
			// got 是读取或解析得到的实际值，用于与期望值比对。
			if got, _ := sessions.Get(ctx, cookieID, "chat-2"); got != "conv-b" {
				t.Fatalf("其他会话不应受影响: %q", got)
			}
			// 删除只影响指定会话。
			if delErr := sessions.Delete(ctx, cookieID, "chat-1"); delErr != nil {
				t.Fatal(delErr)
			}
			// got 是读取或解析得到的实际值，用于与期望值比对。
			if got, _ := sessions.Get(ctx, cookieID, "chat-1"); got != "" {
				t.Fatalf("删除后应为空: %q", got)
			}
			// 时钟前进超过保留期后再写入一条，旧映射被清理。
			clock = clock.Add(AgentRemoteSessionTTL + time.Hour)
			// putErr 是保存会话映射的错误，期望为空。
			if putErr := sessions.Put(ctx, cookieID, "chat-3", "conv-c"); putErr != nil {
				t.Fatal(putErr)
			}
			// got 是读取或解析得到的实际值，用于与期望值比对。
			if got, _ := sessions.Get(ctx, cookieID, "chat-2"); got != "" {
				t.Fatalf("过期映射应被清理: %q", got)
			}
			// got 是读取或解析得到的实际值，用于与期望值比对。
			if got, _ := sessions.Get(ctx, cookieID, "chat-3"); got != "conv-c" {
				t.Fatalf("新映射应保留: %q", got)
			}
			// 删除账号时映射随外键级联清除。
			if delErr := target.store.Cookies.Delete(ctx, cookieID); delErr != nil {
				t.Fatalf("删除账号失败: %v", delErr)
			}
			// got 是读取或解析得到的实际值，用于与期望值比对。
			if got, _ := sessions.Get(ctx, cookieID, "chat-3"); got != "" {
				t.Fatalf("账号删除后映射应级联清除: %q", got)
			}
		})
	}
}
