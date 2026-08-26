package adapter

import (
	"context"
	"errors"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// TestAccountLoginAuditRepositoryMapping 验证登录审计应用模型到 SQLite 数据模型的字段映射。
func TestAccountLoginAuditRepositoryMapping(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试存储的登录审计适配器。
	repository := NewAccountLoginAuditRepository(store)
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// markErr 保存登录方式写入结果。
	if markErr := repository.MarkLogin(ctx, "cid", accountapp.LoginMethodQRScan, 123); markErr != nil {
		t.Fatalf("写入登录方式失败: %v", markErr)
	}
	// statusErr 保存账号启用状态写入结果。
	if statusErr := repository.SetStatusWithReason(ctx, "cid", true, ""); statusErr != nil {
		t.Fatalf("写入账号状态失败: %v", statusErr)
	}
	// logErr 保存登录审计日志写入结果。
	if logErr := repository.AddLoginLog(ctx, accountapp.LoginAuditLog{
		AccountID: "cid", UserID: 1, Method: accountapp.LoginMethodQRScan,
		Status: accountapp.LoginStatusSuccess, Message: "扫码登录成功",
		TriggerReason: "扫码登录", OccurredAt: 123,
	}); logErr != nil {
		t.Fatalf("写入登录审计失败: %v", logErr)
	}
	// detail 保存数据库返回的非敏感登录方式字段。
	detail, detailErr := store.Cookies.GetSummaryOwned(ctx, 1, "cid")
	if detailErr != nil || detail.LoginMethod != accountapp.LoginMethodQRScan || detail.LastLoginAt != 123 {
		t.Fatalf("登录方式映射异常 detail=%+v err=%v", detail, detailErr)
	}
	// logs 保存数据库返回的登录审计记录。
	logs, logsErr := store.LoginLogs.ListByCookie(ctx, "cid", 1)
	if logsErr != nil || len(logs) != 1 || logs[0].OwnerID != 1 || logs[0].AccountIdentifier != "cid" || logs[0].TriggerReason != "扫码登录" || logs[0].ErrorMessage != "扫码登录成功" {
		t.Fatalf("登录审计映射异常 logs=%+v err=%v", logs, logsErr)
	}
}

// TestAccountLoginAuditRepositoryInfrastructureErrors 验证适配器缺少依赖或数据库关闭时不伪装成功。
func TestAccountLoginAuditRepositoryInfrastructureErrors(t *testing.T) {
	// missingErr 保存缺少 Store 时的装配错误。
	missingErr := NewAccountLoginAuditRepository(nil).MarkLogin(context.Background(), "cid", "manual", 1)
	if missingErr == nil {
		t.Fatal("缺少 Store 时应返回装配错误")
	}
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的登录审计适配器。
	repository := NewAccountLoginAuditRepository(store)
	// closeErr 保存主动关闭测试数据库的资源释放结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// markErr 保存数据库关闭后登录方式写入结果。
	markErr := repository.MarkLogin(context.Background(), "cid", "manual", 1)
	if markErr == nil {
		t.Fatal("数据库关闭后应返回登录方式写入错误")
	}
	// logErr 保存数据库关闭后审计日志写入结果。
	logErr := repository.AddLoginLog(context.Background(), accountapp.LoginAuditLog{AccountID: "cid", Method: "manual"})
	if logErr == nil || errors.Is(logErr, accountapp.ErrLoginAuditUnavailable) {
		t.Fatalf("数据库故障应透传而非伪装应用装配错误: %v", logErr)
	}
}

// TestAccountLoginAuditRepositoryOptionalAndNilGuards 覆盖 nil 接收者、缺失 Cookie 仓储和兼容空审计表分支。
func TestAccountLoginAuditRepositoryOptionalAndNilGuards(t *testing.T) {
	// nilRepository 表示未装配的审计仓储接收者。
	var nilRepository *AccountLoginAuditRepository
	if nilRepository.SetStatusWithReason(context.Background(), "cid", true, "") == nil || nilRepository.AddLoginLog(context.Background(), accountapp.LoginAuditLog{}) == nil {
		t.Fatal("nil 审计仓储不应伪装成功")
	}
	// store 保存 Cookie 仓储被显式移除的测试 Store。
	store := db.NewStore(nil, db.DialectSQLite)
	store.Cookies = nil
	// repository 保存缺失 Cookie 仓储的审计适配器。
	repository := NewAccountLoginAuditRepository(store)
	if repository.MarkLogin(context.Background(), "cid", "manual", 1) == nil || repository.SetStatusWithReason(context.Background(), "cid", true, "") == nil {
		t.Fatal("缺失 Cookie 仓储不应伪装成功")
	}
	// store.LoginLogs 为空时 AddLoginLog 保持历史兼容空操作。
	store.LoginLogs = nil
	// err 保存可选审计表缺失时的兼容写入结果。
	if err := repository.AddLoginLog(context.Background(), accountapp.LoginAuditLog{AccountID: "cid"}); err != nil {
		t.Fatalf("可选登录审计表缺失不应阻断主流程: %v", err)
	}
}

var _ accountapp.LoginAuditRepository = (*AccountLoginAuditRepository)(nil)
