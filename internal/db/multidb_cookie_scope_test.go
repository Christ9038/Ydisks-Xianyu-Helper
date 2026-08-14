package db

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// TestMultiDB_CookieCredentialScope 验证所有数据库方言都能使用敏感数据边界内的窄查询。
func TestMultiDB_CookieCredentialScope(t *testing.T) {
	// targets 是本次回归可用的 SQLite、MySQL 和 Postgres 测试目标集合。
	targets := allTestTargets(t)
	// target 是当前循环选中的独立数据库测试目标。
	for _, target := range targets {
		// target 是当前子测试使用的独立数据库目标。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 是当前数据库目标共享的请求上下文。
			ctx := context.Background()
			// suffix 是跨数据库测试使用的唯一后缀，避免并行或重试时发生主键冲突。
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// username 是当前测试创建的本地用户名称。
			username := "scope_" + target.name + "_" + suffix
			// created, createErr 表示创建测试用户的结果。
			created, createErr := target.store.Users.Create(ctx, username, username+"@example.com", "test-password")
			if createErr != nil || !created {
				t.Fatalf("create user: created=%v err=%v", created, createErr)
			}
			// user 是刚创建用户的数据库记录，用于建立 cookie 所有权。
			user, userErr := target.store.Users.GetByUsername(ctx, username)
			if userErr != nil {
				t.Fatalf("get user: %v", userErr)
			}
			// cookieID 是当前测试账号的稳定标识。
			cookieID := "scope_" + target.name + "_" + suffix
			// cookieValue 是用于验证窄查询解密结果的测试 Cookie。
			cookieValue := "unb=1; _m_h5_tk=" + suffix
			// saveErr 表示创建测试账号及其 Cookie 密文失败的原因。
			if saveErr := target.store.Cookies.Save(ctx, cookieID, cookieValue, user.ID); saveErr != nil {
				t.Fatalf("save cookie: %v", saveErr)
			}
			// metadata 是平台续期流程需要保留的 Cookie 快照元数据。
			metadata := `{"snapshot":"` + suffix + `"}`
			// refreshErr 表示写入 Cookie metadata 失败的原因。
			if refreshErr := target.store.Cookies.UpdateRenewalCookie(ctx, cookieID, cookieValue, metadata, time.Now().Unix()); refreshErr != nil {
				t.Fatalf("update renewal cookie: %v", refreshErr)
			}

			// runtimeData 是只读取 Cookie 与 metadata 的运行时窄视图。
			runtimeData, runtimeErr := target.store.Cookies.GetCookieRuntimeData(ctx, cookieID)
			if runtimeErr != nil {
				t.Fatalf("GetCookieRuntimeData: %v", runtimeErr)
			}
			if runtimeData.Value != cookieValue || runtimeData.MetadataJSON != metadata {
				t.Fatalf("runtime data=%+v", runtimeData)
			}
			// platformData 是平台调用需要的 Cookie、metadata、所有者和浏览器设置窄视图。
			platformData, platformErr := target.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
			if platformErr != nil {
				t.Fatalf("GetCookiePlatformRuntimeData: %v", platformErr)
			}
			if platformData.ID != cookieID || platformData.UserID != user.ID || platformData.Value != cookieValue || platformData.MetadataJSON != metadata {
				t.Fatalf("platform data=%+v", platformData)
			}
			// summary 是所有权校验和管理页面使用的非敏感账号摘要。
			summary, summaryErr := target.store.Cookies.GetSummaryOwned(ctx, user.ID, cookieID)
			if summaryErr != nil {
				t.Fatalf("GetSummaryOwned: %v", summaryErr)
			}
			if summary.ID != cookieID || summary.UserID != user.ID {
				t.Fatalf("summary=%+v", summary)
			}
		})
	}
}
