package adapter

import (
	"context"
	"errors"
	"testing"

	defaultreplyapp "xianyu-go/internal/application/defaultreply"
)

// TestDefaultReplyRepositoryCRUDMapping 验证默认回复 CRUD 和应用模型之间的字段映射。
func TestDefaultReplyRepositoryCRUDMapping(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的默认回复数据库适配器。
	repository := NewDefaultReplyRepository(store)
	// ctx 是本测试所有数据库操作使用的非取消上下文。
	ctx := context.Background()
	// owner 是测试模板中创建的默认管理员用户。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// ownership、ownershipErr 保存账号所有权 Port 的返回结果。
	ownership, ownershipErr := repository.CheckOwnership(ctx, owner.ID, "cid")
	if ownershipErr != nil || ownership.OwnerID != owner.ID {
		t.Fatalf("账号归属映射异常 ownership=%+v err=%v", ownership, ownershipErr)
	}
	// input 是覆盖全部默认回复字段的应用模型。
	input := defaultreplyapp.Reply{Enabled: true, ReplyContent: "你好", ReplyImageURL: "https://example.invalid/reply.png", ReplyOnce: true}
	// upsertErr 表示保存默认回复的数据库错误。
	if upsertErr := repository.Upsert(ctx, "cid", input); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// got、getErr 保存适配器转换后的默认回复模型及查询错误。
	got, getErr := repository.Get(ctx, "cid")
	if getErr != nil || got != input {
		t.Fatalf("默认回复字段映射异常 got=%+v want=%+v err=%v", got, input, getErr)
	}
	// listed、listErr 保存按用户隔离的默认回复摘要列表。
	listed, listErr := repository.ListForUser(ctx, owner.ID)
	if listErr != nil || len(listed) != 1 || listed[0].CookieID != "cid" || listed[0].Reply != input {
		t.Fatalf("默认回复列表映射异常 listed=%+v err=%v", listed, listErr)
	}
	// updated 是第二次写入的应用模型，用于验证覆盖语义。
	updated := defaultreplyapp.Reply{Enabled: false, ReplyContent: "更新后"}
	// updateErr 表示覆盖默认回复的数据库错误。
	if updateErr := repository.Upsert(ctx, "cid", updated); updateErr != nil {
		t.Fatal(updateErr)
	}
	// afterUpdate、afterUpdateErr 保存覆盖后的默认回复模型及查询错误。
	afterUpdate, afterUpdateErr := repository.Get(ctx, "cid")
	if afterUpdateErr != nil || afterUpdate != updated {
		t.Fatalf("默认回复覆盖异常 got=%+v err=%v", afterUpdate, afterUpdateErr)
	}
	// clearErr 表示清理默认回复投递记录的数据库错误。
	if clearErr := repository.ClearRecords(ctx, "cid"); clearErr != nil {
		t.Fatal(clearErr)
	}
	// deleteErr 表示删除默认回复的数据库错误。
	if deleteErr := repository.Delete(ctx, "cid"); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// missingErr 保存删除后读取配置的应用层缺失错误。
	if _, missingErr := repository.Get(ctx, "cid"); !errors.Is(missingErr, defaultreplyapp.ErrConfigNotFound) {
		t.Fatalf("删除后应映射为配置缺失，err=%v", missingErr)
	}
}

// TestDefaultReplyRepositoryOwnershipErrors 验证不存在账号和跨用户账号的非敏感归属语义。
func TestDefaultReplyRepositoryOwnershipErrors(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的默认回复数据库适配器。
	repository := NewDefaultReplyRepository(store)
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// missingErr 保存不存在账号的归属查询错误。
	_, missingErr := repository.CheckOwnership(ctx, 1, "missing-account")
	if !errors.Is(missingErr, defaultreplyapp.ErrAccountNotFound) {
		t.Fatalf("不存在账号应返回应用缺失错误，err=%v", missingErr)
	}
	// otherErr 保存跨用户账号的归属查询错误；模板账号 cid 属于管理员。
	other, otherErr := repository.CheckOwnership(ctx, 999, "cid")
	if otherErr != nil || other.OwnerID != 1 {
		t.Fatalf("跨用户归属应返回真实所有者而非敏感数据 owner=%+v err=%v", other, otherErr)
	}
}

// TestDefaultReplyRepositoryPropagatesInfrastructureErrors 验证数据库不可用时不伪装为业务缺失。
func TestDefaultReplyRepositoryPropagatesInfrastructureErrors(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的默认回复适配器。
	repository := NewDefaultReplyRepository(store)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// err 保存数据库关闭后执行默认回复查询的底层错误。
	if _, err := repository.Get(context.Background(), "cid"); err == nil || errors.Is(err, defaultreplyapp.ErrConfigNotFound) {
		t.Fatalf("数据库故障应透传而非映射为配置缺失，err=%v", err)
	}
	// ownershipErr 保存数据库关闭后执行归属查询的底层错误。
	if _, ownershipErr := repository.CheckOwnership(context.Background(), 1, "cid"); ownershipErr == nil {
		t.Fatal("数据库故障时归属查询应返回错误")
	}
	// invalidErr 保存空适配器的装配错误。
	if _, invalidErr := NewDefaultReplyRepository(nil).ListForUser(context.Background(), 1); invalidErr == nil {
		t.Fatal("缺少 Store 时应返回适配器装配错误")
	}
}

var _ defaultreplyapp.Repository = (*DefaultReplyRepository)(nil)
