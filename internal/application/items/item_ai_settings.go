package items

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	// ItemAIOverrideInherit 表示商品沿用账号级 AI 启停状态。
	ItemAIOverrideInherit = "inherit"
	// ItemAIOverrideEnabled 表示商品强制启用 AI 回复。
	ItemAIOverrideEnabled = "enabled"
	// ItemAIOverrideDisabled 表示商品强制停用 AI 回复。
	ItemAIOverrideDisabled = "disabled"
	// ItemAIContextMaxRunes 是商品专属资料允许保存的最大 Unicode 字符数。
	ItemAIContextMaxRunes = 12000
)

var (
	// ErrItemAISettingsForbidden 表示当前用户无权访问指定账号。
	ErrItemAISettingsForbidden = errors.New("无权限操作该账号")
	// ErrItemAISettingsItemNotFound 表示账号下不存在当前可用商品。
	ErrItemAISettingsItemNotFound = errors.New("商品不存在")
	// ErrItemAISettingsInvalidOverride 表示覆盖值不属于三态契约。
	ErrItemAISettingsInvalidOverride = errors.New("商品 AI 覆盖值无效")
	// ErrItemAISettingsContextTooLong 表示商品资料超过 12000 个 Unicode 字符。
	ErrItemAISettingsContextTooLong = errors.New("商品 AI 资料过长")
	// ErrItemAISettingsInvalidContext 表示商品资料不是有效 Unicode 文本。
	ErrItemAISettingsInvalidContext = errors.New("商品 AI 资料必须是有效 Unicode 文本")
)

// ItemAISettings 是应用层使用的商品级 AI 配置模型。
type ItemAISettings struct {
	// CookieID 是商品所属账号标识。
	CookieID string
	// ItemID 是账号范围内的平台商品标识。
	ItemID string
	// Override 是 inherit、enabled 或 disabled 三态覆盖值。
	Override string
	// Context 是注入 AI 提示词的商品专属资料。
	Context string
}

// ItemAISettingsRepository 定义商品级 AI 配置用例所需的最小归属与持久化能力。
type ItemAISettingsRepository interface {
	// AccountOwned 判断账号是否属于指定用户，不读取账号凭证。
	AccountOwned(context.Context, int64, string) (bool, error)
	// ItemExists 判断账号下当前未删除商品是否存在。
	ItemExists(context.Context, string, string) (bool, error)
	// Get 读取独立配置；无记录时返回默认继承配置。
	Get(context.Context, string, string) (ItemAISettings, error)
	// Upsert 创建或更新独立配置。
	Upsert(context.Context, ItemAISettings) error
	// Delete 删除独立配置并恢复继承语义。
	Delete(context.Context, string, string) error
}

// ItemAISettingsService 编排商品级 AI 配置的授权、校验和持久化。
type ItemAISettingsService struct {
	// repository 保存商品归属和 AI 配置持久化端口。
	repository ItemAISettingsRepository
}

// NewItemAISettingsService 创建商品级 AI 配置服务并校验必需端口。
func NewItemAISettingsService(repository ItemAISettingsRepository) (*ItemAISettingsService, error) {
	if repository == nil {
		return nil, errors.New("商品 AI 配置仓储端口不能为空")
	}
	return &ItemAISettingsService{repository: repository}, nil
}

// Get 校验 userID 对 cookieID 和 itemID 的访问权后返回商品级 AI 配置。
func (service *ItemAISettingsService) Get(ctx context.Context, userID int64, cookieID, itemID string) (ItemAISettings, error) {
	// normalizedCookieID 和 normalizedItemID 是去除边界空白后的稳定业务标识。
	normalizedCookieID, normalizedItemID := strings.TrimSpace(cookieID), strings.TrimSpace(itemID)
	if service == nil || service.repository == nil {
		return ItemAISettings{}, errors.New("商品 AI 配置服务未初始化")
	}
	if userID <= 0 || normalizedCookieID == "" {
		return ItemAISettings{}, ErrItemAISettingsForbidden
	}
	if normalizedItemID == "" {
		return ItemAISettings{}, ErrItemAISettingsItemNotFound
	}
	// accessErr 保存账号和商品归属校验结果。
	if accessErr := service.authorize(ctx, userID, normalizedCookieID, normalizedItemID); accessErr != nil {
		return ItemAISettings{}, accessErr
	}
	return service.repository.Get(ctx, normalizedCookieID, normalizedItemID)
}

// Save 校验授权和输入后保存配置；inherit 且资料为空时删除独立记录。
func (service *ItemAISettingsService) Save(ctx context.Context, userID int64, settings ItemAISettings) error {
	if service == nil || service.repository == nil {
		return errors.New("商品 AI 配置服务未初始化")
	}
	// normalized 保存规范化业务标识后的配置，资料正文保持用户输入不变。
	normalized := settings
	normalized.CookieID = strings.TrimSpace(settings.CookieID)
	normalized.ItemID = strings.TrimSpace(settings.ItemID)
	normalized.Override = strings.TrimSpace(settings.Override)
	if userID <= 0 || normalized.CookieID == "" {
		return ErrItemAISettingsForbidden
	}
	if normalized.ItemID == "" {
		return ErrItemAISettingsItemNotFound
	}
	if !validItemAISettingsOverride(normalized.Override) {
		return ErrItemAISettingsInvalidOverride
	}
	if !utf8.ValidString(normalized.Context) {
		return ErrItemAISettingsInvalidContext
	}
	if utf8.RuneCountInString(normalized.Context) > ItemAIContextMaxRunes {
		return ErrItemAISettingsContextTooLong
	}
	// accessErr 保存账号和商品归属校验结果。
	if accessErr := service.authorize(ctx, userID, normalized.CookieID, normalized.ItemID); accessErr != nil {
		return accessErr
	}
	if normalized.Override == ItemAIOverrideInherit && normalized.Context == "" {
		return service.repository.Delete(ctx, normalized.CookieID, normalized.ItemID)
	}
	return service.repository.Upsert(ctx, normalized)
}

// authorize 依次校验账号归属和当前商品存在性，基础设施错误保持原样返回。
func (service *ItemAISettingsService) authorize(ctx context.Context, userID int64, cookieID, itemID string) error {
	// accountOwned 和 accountErr 保存不读取凭证的账号归属检查结果。
	accountOwned, accountErr := service.repository.AccountOwned(ctx, userID, cookieID)
	if accountErr != nil {
		return accountErr
	}
	if !accountOwned {
		return ErrItemAISettingsForbidden
	}
	// itemExists 和 itemErr 保存当前未删除商品的存在性检查结果。
	itemExists, itemErr := service.repository.ItemExists(ctx, cookieID, itemID)
	if itemErr != nil {
		return itemErr
	}
	if !itemExists {
		return ErrItemAISettingsItemNotFound
	}
	return nil
}

// validItemAISettingsOverride 判断覆盖值是否属于应用契约允许的三态集合。
func validItemAISettingsOverride(override string) bool {
	return override == ItemAIOverrideInherit || override == ItemAIOverrideEnabled || override == ItemAIOverrideDisabled
}
