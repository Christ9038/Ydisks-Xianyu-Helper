package apikey

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// failingRepository 是让每个持久化操作都返回固定错误的仓储替身。
type failingRepository struct {
	// err 是所有仓储方法返回的模拟持久化错误。
	err error
}

// Create 返回预设错误，模拟摘要写入失败。
func (repository failingRepository) Create(context.Context, Record) error { return repository.err }

// List 返回预设错误，模拟令牌列表读取失败。
func (repository failingRepository) List(context.Context, int64) ([]Record, error) {
	return nil, repository.err
}

// Revoke 返回预设错误，模拟撤销写入失败。
func (repository failingRepository) Revoke(context.Context, int64, string, int64) error {
	return repository.err
}

// TestNewServiceRejectsNilRepository 验证必需依赖缺失时构造失败。
func TestNewServiceRejectsNilRepository(t *testing.T) {
	// service、err 是以空仓储构造服务的结果；必须失败且不返回半成品服务。
	if service, err := NewService(nil); err == nil || service != nil {
		t.Fatalf("nil 仓储必须拒绝构造: service=%v err=%v", service, err)
	}
}

// TestServiceRejectsInvalidUserAndNilReceiver 验证无效用户与零值服务统一返回用户身份错误。
func TestServiceRejectsInvalidUserAndNilReceiver(t *testing.T) {
	// service 是使用正常仓储构造的服务。
	service, _ := NewService(&fakeRepository{})
	// nilService 是未构造的空服务，所有入口必须安全拒绝而不是 panic。
	var nilService *Service
	// input 是满足其余约束的合法创建输入。
	input := CreateInput{Name: "ok", Scopes: []string{ScopeItemsRead}}
	// userID 是当前待验证的无效用户标识。
	for _, userID := range []int64{0, -1} {
		// err 是无效用户标识签发令牌时返回的错误。
		if _, err := service.Create(context.Background(), userID, input); !errors.Is(err, ErrInvalidUser) {
			t.Errorf("Create(user=%d) err=%v", userID, err)
		}
		// err 是无效用户标识读取列表时返回的错误。
		if _, err := service.List(context.Background(), userID); !errors.Is(err, ErrInvalidUser) {
			t.Errorf("List(user=%d) err=%v", userID, err)
		}
		// err 是无效用户标识撤销令牌时返回的错误。
		if err := service.Revoke(context.Background(), userID, "k"); !errors.Is(err, ErrInvalidUser) {
			t.Errorf("Revoke(user=%d) err=%v", userID, err)
		}
	}
	// err 是空服务签发令牌时返回的错误，不得 panic。
	if _, err := nilService.Create(context.Background(), 1, input); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("nil Create err=%v", err)
	}
	// err 是空服务读取列表时返回的错误，不得 panic。
	if _, err := nilService.List(context.Background(), 1); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("nil List err=%v", err)
	}
	// err 是空服务撤销令牌时返回的错误，不得 panic。
	if err := nilService.Revoke(context.Background(), 1, "k"); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("nil Revoke err=%v", err)
	}
}

// TestCreateValidationBoundaries 验证名称、有效期与权限的边界值。
func TestCreateValidationBoundaries(t *testing.T) {
	// repository 记录成功写入，便于检查默认有效期。
	repository := &fakeRepository{}
	// service 是使用测试仓储构造的服务。
	service, _ := NewService(repository)
	// invalid 是必须以 ErrInvalidInput 拒绝的输入。
	invalid := []CreateInput{
		{Name: "   ", Scopes: []string{ScopeItemsRead}},
		{Name: "含\n控制字符", Scopes: []string{ScopeItemsRead}},
		{Name: "负有效期", Scopes: []string{ScopeItemsRead}, ExpiresInDays: -1},
		{Name: "空白权限", Scopes: []string{"  "}},
		{Name: "大小写不同的权限", Scopes: []string{"Items:Read"}},
	}
	// input 是当前非法创建输入。
	for _, input := range invalid {
		// err 是当前非法输入被拒绝时的业务错误，必须是 ErrInvalidInput。
		if _, err := service.Create(context.Background(), 1, input); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("input %+v err=%v, want ErrInvalidInput", input, err)
		}
	}
	// exact 恰好等于名称上限，应当被接受；同时覆盖多字节字符按字符而非字节计数。
	exact, err := service.Create(context.Background(), 1, CreateInput{Name: strings.Repeat("长", MaxNameRunes), Scopes: []string{" " + ScopeItemsRead + " "}, ExpiresInDays: MaxExpiryDays})
	if err != nil {
		t.Fatalf("上限名称与最大有效期应通过: %v", err)
	}
	// got 是上限有效期令牌的有效秒数（过期时间减签发时间），应等于最大天数对应的秒数。
	if got := exact.Summary.ExpiresAt - exact.Summary.CreatedAt; got != int64(MaxExpiryDays*24*60*60) {
		t.Errorf("有效期秒数=%d", got)
	}
	// defaulted 使用 0 有效期，应落到默认天数。
	defaulted, err := service.Create(context.Background(), 1, CreateInput{Name: "默认", Scopes: []string{ScopeItemsRead}})
	if err != nil {
		t.Fatal(err)
	}
	// got 是默认有效期令牌的有效秒数，应等于默认天数对应的秒数。
	if got := defaulted.Summary.ExpiresAt - defaulted.Summary.CreatedAt; got != int64(DefaultExpiryDays*24*60*60) {
		t.Errorf("默认有效期秒数=%d", got)
	}
	if exact.Token == defaulted.Token {
		t.Error("两次签发的令牌必须不同")
	}
}

// TestServicePropagatesRepositoryErrors 验证仓储错误原样向上传播，且 Create 失败不返回令牌。
func TestServicePropagatesRepositoryErrors(t *testing.T) {
	// boom 是模拟的持久化故障。
	boom := errors.New("db down")
	// service 使用恒失败仓储。
	service, _ := NewService(failingRepository{err: boom})
	// created、err 是仓储写入失败时的签发结果；err 必须携带仓储错误，且 created.Token 必须为空以免泄露未保存的令牌。
	if created, err := service.Create(context.Background(), 1, CreateInput{Name: "n", Scopes: []string{ScopeItemsRead}}); !errors.Is(err, boom) || created.Token != "" {
		t.Errorf("Create 失败时不得返回令牌: token=%q err=%v", created.Token, err)
	}
	// err 是仓储读取失败时 List 返回的错误。
	if _, err := service.List(context.Background(), 1); !errors.Is(err, boom) {
		t.Errorf("List err=%v", err)
	}
	// err 是仓储写入失败时 Revoke 返回的错误。
	if err := service.Revoke(context.Background(), 1, "k"); !errors.Is(err, boom) {
		t.Errorf("Revoke err=%v", err)
	}
}

// TestRevokeRejectsBlankKeyAndTrimsID 验证空令牌标识被拒绝，非空标识会去除首尾空白后下传。
func TestRevokeRejectsBlankKeyAndTrimsID(t *testing.T) {
	// repository 记录撤销调用。
	repository := &fakeRepository{}
	// service 是使用测试仓储构造的服务。
	service, _ := NewService(repository)
	// err 是空白令牌标识被拒绝时的业务错误。
	if err := service.Revoke(context.Background(), 1, "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空标识 err=%v", err)
	}
	// err 是带首尾空白的标识撤销结果；仓储应收到去除空白后的标识。
	if err := service.Revoke(context.Background(), 1, "  key-1 "); err != nil || repository.revokedKeyID != "key-1" {
		t.Fatalf("应去除空白: key=%q err=%v", repository.revokedKeyID, err)
	}
}

// TestSupportedScopesReturnsCopyAndHashIsStable 验证白名单副本不可被调用方篡改，摘要稳定且不等于明文。
func TestSupportedScopesReturnsCopyAndHashIsStable(t *testing.T) {
	// first 是第一次取得的权限副本。
	first := SupportedScopes()
	if len(first) != len(supportedScopes) {
		t.Fatalf("权限数量=%d", len(first))
	}
	first[0] = "tampered"
	if SupportedScopes()[0] == "tampered" {
		t.Fatal("修改返回值不得影响包级白名单")
	}
	// hash 是固定输入的 SHA-256 十六进制摘要，长度必须为 64 且可重复计算。
	hash := HashToken("ydk_live_x")
	if len(hash) != 64 || hash != HashToken("ydk_live_x") || hash == HashToken("ydk_live_y") || strings.Contains(hash, "ydk_live") {
		t.Fatalf("摘要异常: %q", hash)
	}
}

// TestListProjectsSummariesWithoutHash 验证列表投影复制权限切片且不暴露摘要字段。
func TestListProjectsSummariesWithoutHash(t *testing.T) {
	// repository 预置一条含摘要的记录。
	repository := &fakeRepository{records: []Record{{ID: "a", UserID: 5, Name: "n", TokenHash: "secret-hash", Scopes: []string{ScopeChatRead}}}}
	// service 是使用测试仓储构造的服务。
	service, _ := NewService(repository)
	// summaries 是列表投影结果。
	summaries, err := service.List(context.Background(), 5)
	if err != nil || len(summaries) != 1 {
		t.Fatalf("summaries=%+v err=%v", summaries, err)
	}
	summaries[0].Scopes[0] = "tampered"
	if repository.records[0].Scopes[0] != ScopeChatRead {
		t.Fatal("摘要必须持有权限切片副本")
	}
	// empty、err 是无记录用户的列表结果，应为非 nil 的空切片且无错误。
	if empty, err := service.List(context.Background(), 6); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("无记录应返回非 nil 空列表: %v %v", empty, err)
	}
}
