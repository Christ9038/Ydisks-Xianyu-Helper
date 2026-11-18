// Package apikey 管理由本地用户签发、限权和撤销的外部 API 访问令牌。
// 明文令牌只在创建结果中返回一次；仓储只接收其 SHA-256 摘要。
package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	// TokenPrefix 是外部 API 访问令牌的稳定识别前缀。
	TokenPrefix = "ydk_live_"
	// DefaultExpiryDays 是未指定有效期时签发令牌的默认天数。
	DefaultExpiryDays = 90
	// MaxExpiryDays 限制单个访问令牌的最长有效期。
	MaxExpiryDays = 365
	// MaxNameRunes 限制访问令牌备注名称的字符数。
	MaxNameRunes = 64
)

const (
	// ScopeAccountsRead 允许读取当前用户的闲鱼账号摘要。
	ScopeAccountsRead = "accounts:read"
	// ScopeItemsRead 允许读取当前用户的商品及批量发布结果。
	ScopeItemsRead = "items:read"
	// ScopeItemsPublish 允许调用现有商品发布和批量发布用例。
	ScopeItemsPublish = "items:publish"
	// ScopeItemsUnpublish 允许对当前用户拥有的商品执行平台下架，不包含平台删除。
	ScopeItemsUnpublish = "items:unpublish"
	// ScopeOrdersRead 允许读取当前用户的订单信息。
	ScopeOrdersRead = "orders:read"
	// ScopeChatRead 允许读取聊天会话、消息和买家备注。
	ScopeChatRead = "chat:read"
	// ScopeChatSend 允许向买家发送普通文本消息。
	ScopeChatSend = "chat:send"
	// ScopeChatTakeover 允许单独调整人工介入暂停窗口，不允许修改账号凭据。
	ScopeChatTakeover = "chat:takeover"
	// ScopeRepliesManage 允许管理 AI、关键词、默认及 Agent 回复配置。
	ScopeRepliesManage = "replies:manage"
	// ScopeAutomationRead 允许读取自动化规则、问题和审计记录。
	ScopeAutomationRead = "automation:read"
	// ScopeAutomationManage 允许创建、修改、删除和处理自动化规则。
	ScopeAutomationManage = "automation:manage"
)

var (
	// ErrInvalidUser 表示调用方没有提供有效的本地用户身份。
	ErrInvalidUser = errors.New("API 访问令牌用户身份无效")
	// ErrInvalidInput 表示令牌名称、权限范围或有效期不符合约束。
	ErrInvalidInput = errors.New("API 访问令牌参数无效")
	// ErrNotFound 表示目标访问令牌不存在或不属于当前用户。
	ErrNotFound = errors.New("API 访问令牌不存在")
)

// supportedScopes 是当前已实现并允许签发的最小权限集合。
var supportedScopes = []string{
	ScopeAccountsRead,
	ScopeItemsRead,
	ScopeItemsPublish,
	ScopeItemsUnpublish,
	ScopeOrdersRead,
	ScopeChatRead,
	ScopeChatSend,
	ScopeChatTakeover,
	ScopeRepliesManage,
	ScopeAutomationRead,
	ScopeAutomationManage,
}

// Record 是访问令牌的非明文持久化记录；TokenHash 是摘要而不是可用令牌。
type Record struct {
	// ID 是本地访问令牌记录标识。
	ID string
	// UserID 是签发令牌的本地用户标识。
	UserID int64
	// Name 是用户为令牌设置的用途名称。
	Name string
	// TokenHash 是原始令牌的 SHA-256 十六进制摘要。
	TokenHash string
	// Prefix 是用于在管理界面识别令牌的非秘密前缀。
	Prefix string
	// Scopes 是该令牌被允许调用的权限集合。
	Scopes []string
	// CreatedAt 是签发时间的 Unix 秒数。
	CreatedAt int64
	// ExpiresAt 是失效时间的 Unix 秒数。
	ExpiresAt int64
	// LastUsedAt 是最近一次认证成功时间；0 表示尚未使用。
	LastUsedAt int64
	// RevokedAt 是撤销时间；0 表示尚未撤销。
	RevokedAt int64
}

// Summary 是可安全返回给管理界面的令牌摘要，不包含 TokenHash 或明文令牌。
type Summary struct {
	// ID 是本地访问令牌记录标识。
	ID string `json:"id"`
	// Name 是用户为令牌设置的用途名称。
	Name string `json:"name"`
	// Prefix 是用于识别令牌的非秘密前缀。
	Prefix string `json:"prefix"`
	// Scopes 是该令牌被允许调用的权限集合。
	Scopes []string `json:"scopes"`
	// CreatedAt 是签发时间的 Unix 秒数。
	CreatedAt int64 `json:"created_at"`
	// ExpiresAt 是失效时间的 Unix 秒数。
	ExpiresAt int64 `json:"expires_at"`
	// LastUsedAt 是最近一次认证成功时间；0 表示尚未使用。
	LastUsedAt int64 `json:"last_used_at"`
	// RevokedAt 是撤销时间；0 表示尚未撤销。
	RevokedAt int64 `json:"revoked_at"`
}

// CreateInput 是创建访问令牌时由管理界面提交的非敏感字段。
type CreateInput struct {
	// Name 是令牌用途名称。
	Name string
	// Scopes 是本次签发需要授予的最小权限集合。
	Scopes []string
	// ExpiresInDays 是有效期天数；0 使用 90 天默认值。
	ExpiresInDays int
}

// Created 是签发成功的结果；Token 只允许在本次创建响应中展示一次。
type Created struct {
	// Summary 是不含明文和摘要的令牌管理信息。
	Summary Summary
	// Token 是新生成的明文令牌；服务端不会再次保存或返回它。
	Token string
}

// Repository 定义令牌创建、列表和撤销所需的最小持久化能力。
type Repository interface {
	// Create 保存令牌摘要与非敏感元数据。
	Create(context.Context, Record) error
	// List 仅列出指定用户拥有的令牌摘要。
	List(context.Context, int64) ([]Record, error)
	// Revoke 撤销指定用户拥有的令牌；不存在或不属于该用户时返回 ErrNotFound。
	Revoke(context.Context, int64, string, int64) error
}

// Service 编排访问令牌的输入校验、随机签发和归属安全。
type Service struct {
	// repository 是构造时注入的访问令牌持久化端口。
	repository Repository
}

// NewService 创建访问令牌应用服务；repository 为空时返回构造错误。
func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("API 访问令牌仓储不能为空")
	}
	return &Service{repository: repository}, nil
}

// Create 签发高熵访问令牌并仅持久化摘要；返回值中的明文只供本次创建响应展示。
func (service *Service) Create(ctx context.Context, userID int64, input CreateInput) (Created, error) {
	if service == nil || service.repository == nil || userID <= 0 {
		return Created{}, ErrInvalidUser
	}
	// name 是去除首尾空白后的用途名称，后续还会校验长度和控制字符。
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > MaxNameRunes || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return Created{}, fmt.Errorf("%w：名称不能为空且不能超过 %d 个字符", ErrInvalidInput, MaxNameRunes)
	}
	// expiryDays 是本次令牌的有效天数；0 表示采用服务端默认值。
	expiryDays := input.ExpiresInDays
	if expiryDays == 0 {
		expiryDays = DefaultExpiryDays
	}
	if expiryDays < 1 || expiryDays > MaxExpiryDays {
		return Created{}, fmt.Errorf("%w：有效期必须为 1–%d 天", ErrInvalidInput, MaxExpiryDays)
	}
	// scopes、err 保存去重排序后的权限集合及未知或空权限输入错误。
	scopes, err := normalizeScopes(input.Scopes)
	if err != nil {
		return Created{}, err
	}
	// randomBytes 保存 256 位随机令牌主体；原始随机值不会写入日志或数据库。
	randomBytes := make([]byte, 32)
	// err 是读取密码学安全随机源生成 256 位令牌主体时的系统错误。
	if _, err := rand.Read(randomBytes); err != nil {
		return Created{}, fmt.Errorf("生成 API 访问令牌失败")
	}
	// token 是仅返回给本次创建调用方的明文 Bearer 凭证。
	token := TokenPrefix + base64.RawURLEncoding.EncodeToString(randomBytes)
	// now 保存签发时间；expiresAt 按固定 24 小时天数计算有效期终点。
	now := time.Now().Unix()
	// expiresAt 是按 24 小时天数计算的令牌过期 Unix 时间。
	expiresAt := now + int64(expiryDays*24*60*60)
	// prefix 只保留令牌开头用于管理界面识别，不足以还原或验证令牌。
	prefix := token[:len(TokenPrefix)+8]
	// record 是仅含摘要与非敏感元数据的持久化记录。
	record := Record{
		ID: uuid.NewString(), UserID: userID, Name: name, TokenHash: HashToken(token),
		Prefix: prefix, Scopes: scopes, CreatedAt: now, ExpiresAt: expiresAt,
	}
	// err 是将摘要和权限元数据写入令牌仓储时的持久化错误。
	if err := service.repository.Create(ctx, record); err != nil {
		return Created{}, err
	}
	return Created{Summary: toSummary(record), Token: token}, nil
}

// List 返回当前用户的令牌管理摘要；任何记录都不包含可用于认证的令牌材料。
func (service *Service) List(ctx context.Context, userID int64) ([]Summary, error) {
	if service == nil || service.repository == nil || userID <= 0 {
		return nil, ErrInvalidUser
	}
	// records 保存仓储返回的当前用户令牌元数据。
	records, err := service.repository.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	// summaries 是不含令牌摘要和明文的 HTTP 可用列表。
	summaries := make([]Summary, 0, len(records))
	// record 是当前用户的一条令牌持久化摘要，转换后不会暴露哈希。
	for _, record := range records {
		summaries = append(summaries, toSummary(record))
	}
	return summaries, nil
}

// Revoke 撤销当前用户拥有的访问令牌；撤销后新的 Bearer 请求立即无法通过认证。
func (service *Service) Revoke(ctx context.Context, userID int64, keyID string) error {
	if service == nil || service.repository == nil || userID <= 0 {
		return ErrInvalidUser
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return fmt.Errorf("%w：令牌标识不能为空", ErrInvalidInput)
	}
	return service.repository.Revoke(ctx, userID, keyID, time.Now().Unix())
}

// HashToken 计算令牌摘要；令牌具有 256 位随机熵，因此不需要密码式慢哈希。
func HashToken(token string) string {
	// digest 保存完整令牌的 SHA-256 摘要，持久化时只使用其十六进制表示。
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// SupportedScopes 返回当前可签发权限的副本，避免调用方修改包级白名单。
func SupportedScopes() []string {
	return append([]string(nil), supportedScopes...)
}

// normalizeScopes 去重并排序权限，拒绝未知值和空权限集合。
func normalizeScopes(scopes []string) ([]string, error) {
	// allowed 保存当前支持的精确权限值，防止调用方通过自造 scope 扩大访问范围。
	allowed := make(map[string]struct{}, len(supportedScopes))
	// scope 是当前支持的精确权限标识，用于建立白名单查找表。
	for _, scope := range supportedScopes {
		allowed[scope] = struct{}{}
	}
	// unique 保存已验证的权限，消除重复输入。
	unique := make(map[string]struct{}, len(scopes))
	// scope 是调用方提交、尚待校验和去重的一项权限。
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		// ok 表示当前权限是否存在于服务端支持的精确白名单中。
		if _, ok := allowed[scope]; !ok {
			return nil, fmt.Errorf("%w：不支持的权限 %q", ErrInvalidInput, scope)
		}
		unique[scope] = struct{}{}
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("%w：至少选择一项权限", ErrInvalidInput)
	}
	// normalized 是排序后的权限集合，保证存储与响应顺序稳定。
	normalized := make([]string, 0, len(unique))
	// scope 是去重集合中的一项已验证权限，随后按字典序稳定排序。
	for scope := range unique {
		normalized = append(normalized, scope)
	}
	sort.Strings(normalized)
	return normalized, nil
}

// toSummary 将持久化记录投影为不含任何认证秘密的管理摘要。
func toSummary(record Record) Summary {
	return Summary{
		ID: record.ID, Name: record.Name, Prefix: record.Prefix,
		Scopes: append([]string(nil), record.Scopes...), CreatedAt: record.CreatedAt,
		ExpiresAt: record.ExpiresAt, LastUsedAt: record.LastUsedAt, RevokedAt: record.RevokedAt,
	}
}
