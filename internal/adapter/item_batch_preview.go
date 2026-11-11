package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// ItemBatchPreviewPort 将批量预检所需的非敏感归属与本地路径校验适配到基础设施。
type ItemBatchPreviewPort struct {
	// store 提供账号和卡券归属查询能力。
	store *db.Store
}

// NewItemBatchPreviewPort 创建批量预检基础设施 Port。
func NewItemBatchPreviewPort(store *db.Store) *ItemBatchPreviewPort {
	return &ItemBatchPreviewPort{store: store}
}

// CookieOwned 判断账号是否属于用户，不读取或解密 Cookie 内容。
func (port *ItemBatchPreviewPort) CookieOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := port.validate(); err != nil {
		return false, err
	}
	return port.store.Cookies.ExistsOwned(ctx, userID, strings.TrimSpace(cookieID))
}

// CardOwned 判断卡券组是否属于用户，不向应用层暴露卡券库存内容。
func (port *ItemBatchPreviewPort) CardOwned(ctx context.Context, userID, cardID int64) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := port.validate(); err != nil {
		return false, err
	}
	return port.store.Cards.ExistsOwned(ctx, cardID, userID)
}

// DeliveryTemplateRequirements 返回发货模板的归属、启用状态与变量键，不向应用层暴露模板消息正文。
// 模板不存在或属于他人时返回 Usable=false 且无错误，与其他归属查询的“不可见即不存在”语义一致。
func (port *ItemBatchPreviewPort) DeliveryTemplateRequirements(ctx context.Context, userID, templateID int64) (itemapp.BatchTemplateRequirements, error) {
	// err 表示适配器依赖校验错误。
	if err := port.validate(); err != nil {
		return itemapp.BatchTemplateRequirements{}, err
	}
	if port.store.DeliveryTemplates == nil {
		return itemapp.BatchTemplateRequirements{}, errors.New("发货模板存储未初始化")
	}
	// template、err 保存模板摘要及读取错误；不属于用户时视为不可用。
	template, err := port.store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if errors.Is(err, db.ErrNotFound) {
		return itemapp.BatchTemplateRequirements{}, nil
	}
	if err != nil {
		return itemapp.BatchTemplateRequirements{}, err
	}
	return itemapp.BatchTemplateRequirements{Usable: template.Enabled, CardKeys: append([]string(nil), template.Keys...), CustomKeys: append([]string(nil), template.CustomKeys...)}, nil
}

// TemplateCardBindable 判断卡密组能否被发货模板绑定：属于用户、已启用，且为文本、批量数据或配置有效的 API 类型。
// 判断规则与自动化规则保存时的模板绑定校验一致；只读取卡密组摘要，不读取卡密内容或 API 密钥。
func (port *ItemBatchPreviewPort) TemplateCardBindable(ctx context.Context, userID, cardID int64) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := port.validate(); err != nil {
		return false, err
	}
	// card、err 保存卡密组摘要及读取错误；不存在或属于他人时视为不可绑定。
	card, err := port.store.Cards.GetSummary(ctx, cardID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && (card == nil || card.UserID != userID)) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// apiReady 表示非 API 卡密组或 API 配置已通过校验。
	apiReady := card.Type != "api" || card.APIConfigSummary != nil && card.APIConfigSummary.Ready
	return card.Enabled && apiReady && (card.Type == "text" || card.Type == "data" || card.Type == "api"), nil
}

// ValidateImageReference 校验本地图片路径位于指定上传目录内且对应普通文件。
func (port *ItemBatchPreviewPort) ValidateImageReference(uploadDir, reference string) error {
	_ = port
	if isPreviewHTTPURL(reference) {
		return nil
	}
	// relative 和 err 表示清理后的相对路径及安全校验错误。
	relative, err := safePreviewPath(reference)
	if err != nil {
		return err
	}
	// path 表示上传根目录下的目标文件路径。
	path := filepath.Join(uploadDir, relative)
	// info 和 err 表示文件属性及读取错误。
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("图片文件不存在: %s", reference)
	}
	return nil
}

// validate 检查账号和卡券子仓储是否已装配。
func (port *ItemBatchPreviewPort) validate() error {
	if port == nil || port.store == nil || port.store.Cookies == nil || port.store.Cards == nil {
		return errors.New("批量预检数据库适配器未初始化")
	}
	return nil
}

// isPreviewHTTPURL 判断图片引用是否为允许的远程 HTTP 地址。
func isPreviewHTTPURL(reference string) bool {
	// value 保存标准化后的远程地址文本。
	value := strings.ToLower(strings.TrimSpace(reference))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// safePreviewPath 清理本地图片路径并拒绝绝对路径和目录穿越。
func safePreviewPath(raw string) (string, error) {
	// value 保存统一路径分隔符后的原始路径。
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if value == "" || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("图片路径不安全: %s", value)
	}
	// clean 保存清理后的相对路径。
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("图片路径不安全: %s", value)
	}
	return clean, nil
}

// 确保适配器实现批量预检的所有基础设施 Port。
var _ itemapp.BatchPreviewOwnershipPort = (*ItemBatchPreviewPort)(nil)
var _ itemapp.BatchPreviewImagePort = (*ItemBatchPreviewPort)(nil)

// 确保适配器实现发货模板预检端口，否则 Excel 与资源库模板动作会被判为不可用。
var _ itemapp.BatchPreviewTemplatePort = (*ItemBatchPreviewPort)(nil)
