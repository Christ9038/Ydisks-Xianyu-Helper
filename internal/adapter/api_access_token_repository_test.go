package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	apikeyapp "xianyu-go/internal/application/apikey"
	"xianyu-go/internal/db"
)

// TestAPIAccessTokenRepositoryRoundTrip 验证适配器在应用层与数据库模型之间完整转换，并保持用户归属。
func TestAPIAccessTokenRepositoryRoundTrip(t *testing.T) {
	// store、cleanup 是带管理员账号的隔离 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本次测试的数据库上下文。
	ctx := context.Background()
	// repository 是被测适配器。
	repository := NewAPIAccessTokenRepository(store.APIAccessTokens)
	if repository == nil {
		t.Fatal("非空数据库仓储应创建适配器")
	}
	// service 通过真实应用服务驱动适配器，覆盖创建、列表和撤销。
	service, err := apikeyapp.NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	// owner 是适配器测试存储里预置的管理员用户。
	owner, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// created 是签发结果，其中明文只在此处出现。
	created, err := service.Create(ctx, owner.ID, apikeyapp.CreateInput{Name: "适配器", Scopes: []string{apikeyapp.ScopeChatRead, apikeyapp.ScopeItemsRead}})
	if err != nil {
		t.Fatal(err)
	}
	// principal 证明库里保存的是摘要并且可被认证。
	principal, err := store.APIAccessTokens.Authenticate(ctx, apikeyapp.HashToken(created.Token), time.Now().Unix())
	if err != nil || principal.UserID != owner.ID || len(principal.Scopes) != 2 {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
	// summaries 是经适配器读回的列表。
	summaries, err := service.List(ctx, owner.ID)
	if err != nil || len(summaries) != 1 || summaries[0].Name != "适配器" || summaries[0].Prefix != created.Summary.Prefix {
		t.Fatalf("summaries=%+v err=%v", summaries, err)
	}
	// 他人不能撤销，本人撤销后令牌立即失效。
	if err := service.Revoke(ctx, owner.ID+100, created.Summary.ID); !errors.Is(err, apikeyapp.ErrNotFound) {
		t.Fatalf("跨用户撤销应返回 apikey.ErrNotFound: %v", err)
	}
	// err 是本人撤销已签发令牌时服务返回的错误，应为空。
	if err := service.Revoke(ctx, owner.ID, created.Summary.ID); err != nil {
		t.Fatal(err)
	}
	// err 是撤销后再次以摘要认证令牌得到的结果，必须是数据库未找到错误。
	if _, err := store.APIAccessTokens.Authenticate(ctx, apikeyapp.HashToken(created.Token), time.Now().Unix()); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("撤销后应失效: %v", err)
	}
}

// TestAPIAccessTokenRepositoryNilAndErrors 验证空仓储不创建适配器，数据库故障原样返回。
func TestAPIAccessTokenRepositoryNilAndErrors(t *testing.T) {
	if NewAPIAccessTokenRepository(nil) != nil {
		t.Fatal("nil 数据库仓储应返回 nil 适配器")
	}
	// store、cleanup 是隔离存储；随后关闭其连接模拟数据库故障。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定将被关闭连接的适配器。
	repository := NewAPIAccessTokenRepository(store.APIAccessTokens)
	store.DB.Close()
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 是数据库连接关闭后适配器创建令牌记录返回的错误，必须非空。
	if err := repository.Create(ctx, apikeyapp.Record{ID: "x", UserID: 1, Scopes: []string{"items:read"}}); err == nil {
		t.Error("Create 数据库故障必须返回错误")
	}
	// err 是数据库连接关闭后适配器读取令牌列表返回的错误，必须非空。
	if _, err := repository.List(ctx, 1); err == nil {
		t.Error("List 数据库故障必须返回错误")
	}
	// err 是数据库连接关闭后适配器撤销令牌返回的错误，必须非空。
	if err := repository.Revoke(ctx, 1, "x", 1); err == nil {
		t.Error("Revoke 数据库故障必须返回错误")
	}
}
