package db

import (
	"context"
	"errors"
	"testing"
)

// TestAccountTokensRejectsInvalidCandidatesAndCorruptedCiphertext 验证设备候选值和凭证密文损坏时的稳定错误。
func TestAccountTokensRejectsInvalidCandidatesAndCorruptedCiphertext(t *testing.T) {
	// store、cleanup 保存隔离的 SQLite 存储和关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// emptyErr 表示空设备候选值的输入校验错误。
	_, emptyErr := store.Tokens.GetOrCreateDeviceID(ctx, "cid", "")
	if emptyErr == nil {
		t.Fatal("空设备候选值应被拒绝")
	}
	// userCreated 表示测试账号创建是否成功。
	userCreated, userCreateErr := store.Users.Create(ctx, "admin", "a@e.com", "pw")
	if userCreateErr != nil || !userCreated {
		t.Fatalf("创建测试用户失败 created=%v err=%v", userCreated, userCreateErr)
	}
	// admin、adminErr 保存测试用户的非敏感身份及查询错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// cookieErr 表示测试账号 Cookie 的创建错误。
	if cookieErr := store.Cookies.Save(ctx, "cid", "sid=test", admin.ID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	// saveErr 表示写入可解密 Token 的结果。
	if saveErr := store.Tokens.Save(ctx, "cid", "device", "token", 10); saveErr != nil {
		t.Fatal(saveErr)
	}
	// corruptErr 表示把访问令牌替换为非法密文的数据库错误。
	if _, corruptErr := store.DB.ExecContext(ctx, "UPDATE account_tokens SET access_token=? WHERE cookie_id=?", "enc:v1:corrupted", "cid"); corruptErr != nil {
		t.Fatal(corruptErr)
	}
	// decryptErr 表示读取损坏密文时的解密错误。
	_, decryptErr := store.Tokens.Get(ctx, "cid")
	if decryptErr == nil {
		t.Fatal("损坏的访问令牌密文应返回解密错误")
	}
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// operations 保存关闭数据库后各 Token 写入端点的错误结果。
	operations := []error{
		store.Tokens.SaveBound(ctx, "cid", "device", "token", 1, "fingerprint"),
		store.Tokens.Clear(ctx, "cid"),
	}
	// operation 表示当前待验证的 Token 操作错误。
	for _, operation := range operations {
		if operation == nil {
			t.Fatal("关闭数据库后 Token 操作不应成功")
		}
	}
	// deviceErr 表示关闭数据库后设备标识创建的基础设施错误。
	_, deviceErr := store.Tokens.GetOrCreateDeviceID(ctx, "cid", "candidate")
	if deviceErr == nil || errors.Is(deviceErr, ErrNotFound) {
		t.Fatalf("关闭数据库后设备标识应返回基础设施错误，err=%v", deviceErr)
	}
}
