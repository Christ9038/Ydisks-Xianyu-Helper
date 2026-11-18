package apikey

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRepository 是隔离测试中记录令牌摘要写入、列表和撤销行为的仓储替身。
type fakeRepository struct {
	// records 保存测试中成功写入的访问令牌摘要。
	records []Record
	// revoked 保存最近一次撤销请求的用户和令牌标识。
	revokedUserID int64
	revokedKeyID  string
}

// Create 保存测试令牌记录，模拟数据库仓储成功写入。
func (repository *fakeRepository) Create(_ context.Context, record Record) error {
	repository.records = append(repository.records, record)
	return nil
}

// List 返回测试仓储中属于指定用户的令牌记录。
func (repository *fakeRepository) List(_ context.Context, userID int64) ([]Record, error) {
	// records 保存属于目标用户的记录副本。
	var records []Record
	// record 是测试仓储中当前检查用户归属的一条令牌记录。
	for _, record := range repository.records {
		if record.UserID == userID {
			records = append(records, record)
		}
	}
	return records, nil
}

// Revoke 记录测试中的撤销调用。
func (repository *fakeRepository) Revoke(_ context.Context, userID int64, keyID string, _ int64) error {
	repository.revokedUserID, repository.revokedKeyID = userID, keyID
	return nil
}

// TestCreateIssuesOneTimeToken 验证签发令牌的随机格式、摘要持久化和权限归一。
func TestCreateIssuesOneTimeToken(t *testing.T) {
	// repository 是记录应用服务写入结果的测试仓储。
	repository := &fakeRepository{}
	// service 是使用测试仓储构造的访问令牌应用服务。
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	// created 是本次签发得到的一次性明文令牌和非敏感摘要。
	created, err := service.Create(context.Background(), 7, CreateInput{Name: "客服 Agent", Scopes: []string{ScopeChatSend, ScopeChatRead, ScopeChatRead}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, TokenPrefix) || len(created.Token) <= len(TokenPrefix)+30 {
		t.Fatalf("token format is invalid: %q", created.Token[:len(TokenPrefix)])
	}
	if len(repository.records) != 1 || repository.records[0].TokenHash != HashToken(created.Token) || repository.records[0].TokenHash == created.Token {
		t.Fatalf("repository record does not contain only token hash: %+v", repository.records)
	}
	if len(created.Summary.Scopes) != 2 || created.Summary.Scopes[0] != ScopeChatRead || created.Summary.Scopes[1] != ScopeChatSend {
		t.Fatalf("scopes should be unique and sorted: %+v", created.Summary.Scopes)
	}
	if created.Summary.Prefix != created.Token[:len(TokenPrefix)+8] || created.Summary.ExpiresAt <= created.Summary.CreatedAt {
		t.Fatalf("summary metadata invalid: %+v", created.Summary)
	}
}

// TestCreateRejectsInvalidScopeAndInput 验证未知权限、空权限、超长名称和无效有效期均被拒绝。
func TestCreateRejectsInvalidScopeAndInput(t *testing.T) {
	// service 是使用测试仓储构造的访问令牌应用服务。
	service, err := NewService(&fakeRepository{})
	if err != nil {
		t.Fatal(err)
	}
	// cases 是需要拒绝的非法创建请求。
	cases := []CreateInput{
		{Name: "无权限", Scopes: nil},
		{Name: "未知权限", Scopes: []string{"admin:*"}},
		{Name: strings.Repeat("长", MaxNameRunes+1), Scopes: []string{ScopeItemsRead}},
		{Name: "有效期过长", Scopes: []string{ScopeItemsRead}, ExpiresInDays: MaxExpiryDays + 1},
	}
	// input 是当前需要验证会被应用服务拒绝的非法创建请求。
	for _, input := range cases {
		// err 是当前非法权限、名称或有效期被应用服务拒绝时的业务错误。
		if _, err := service.Create(context.Background(), 1, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("input %+v should be invalid, err=%v", input, err)
		}
	}
}

// TestListAndRevokeAreUserScoped 验证列表只返回摘要且撤销操作携带当前用户归属。
func TestListAndRevokeAreUserScoped(t *testing.T) {
	// repository 是记录令牌创建和撤销的测试仓储。
	repository := &fakeRepository{}
	// service 是使用测试仓储构造的访问令牌应用服务。
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	// created 是用户 9 签发的访问令牌。
	created, err := service.Create(context.Background(), 9, CreateInput{Name: "只读", Scopes: []string{ScopeItemsRead}})
	if err != nil {
		t.Fatal(err)
	}
	// summaries 是用户 9 的管理列表。
	summaries, err := service.List(context.Background(), 9)
	if err != nil || len(summaries) != 1 || summaries[0].ID != created.Summary.ID {
		t.Fatalf("summaries=%+v err=%v", summaries, err)
	}
	// err 是按用户归属撤销已签发令牌时的应用错误。
	if err := service.Revoke(context.Background(), 9, created.Summary.ID); err != nil {
		t.Fatal(err)
	}
	if repository.revokedUserID != 9 || repository.revokedKeyID != created.Summary.ID {
		t.Fatalf("revoke ownership mismatch: user=%d key=%s", repository.revokedUserID, repository.revokedKeyID)
	}
}
