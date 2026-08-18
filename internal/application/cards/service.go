// Package cards 定义卡券 CRUD 用例、业务模型和消费者侧持久化 Port。
// 本包不依赖 HTTP、数据库模型或其他基础设施实现。
package cards

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// ErrInvalidUser 表示调用方未提供有效的用户身份。
var ErrInvalidUser = errors.New("卡券用户身份无效")

// ErrInvalidCardID 表示调用方未提供有效的卡券组标识。
var ErrInvalidCardID = errors.New("卡券ID无效")

// ErrNotFound 表示卡券组不存在。
var ErrNotFound = errors.New("卡券不存在")

// ErrForbidden 表示卡券组存在但不属于当前用户。
var ErrForbidden = errors.New("无权操作该卡密组")

// ErrUnsupportedAPIType 表示当前自动发货链路不允许新建或转换为 API 卡券。
var ErrUnsupportedAPIType = errors.New("API 卡密暂不支持自动发货")

// ErrNotDataType 表示只有 data 类型卡券组允许追加逐行卡密。
var ErrNotDataType = errors.New("只有 data（批量卡密）类型支持追加卡密")

// ValidationError 表示卡券输入不满足稳定的业务约束。
type ValidationError struct {
	// Message 是可由 HTTP 边界直接展示的稳定校验提示，不包含基础设施细节。
	Message string
}

// Error 返回卡券业务校验提示。
func (e *ValidationError) Error() string {
	if e == nil {
		return "卡券输入校验失败"
	}
	return e.Message
}

// Card 是应用层使用的完整卡券组模型，与数据库行和 HTTP DTO 解耦。
type Card struct {
	// ID 是卡券组的持久化标识。
	ID int64
	// Name 是用户可见的卡券组名称。
	Name string
	// Type 是 text、data、image 或历史兼容的 api 类型。
	Type string
	// APIConfig 是历史 API 卡券配置；当前仅允许读取或更新既有 API 卡券。
	APIConfig string
	// TextContent 是 text 类型自动发货时发送的文本内容。
	TextContent string
	// DataContent 是 data 类型尚未消费的逐行卡密库存。
	DataContent string
	// ImageURL 是 image 类型自动发货时发送的图片地址。
	ImageURL string
	// Description 是用户维护的卡券组说明。
	Description string
	// Enabled 表示自动化规则是否可以使用该卡券组。
	Enabled bool
	// DelaySeconds 是自动发货前的延迟秒数，范围为 0 到 3600。
	DelaySeconds int
	// IsMultiSpec 表示卡券组是否只匹配指定商品规格。
	IsMultiSpec bool
	// SpecName 是多规格匹配使用的规格名称。
	SpecName string
	// SpecValue 是多规格匹配使用的规格值。
	SpecValue string
	// UserID 是拥有该卡券组的本地用户标识。
	UserID int64
}

// Draft 是创建或更新卡券组时允许提交的业务字段。
type Draft struct {
	// Name 是用户可见的卡券组名称。
	Name string
	// Type 是 text、data、image 或历史兼容的 api 类型。
	Type string
	// APIConfig 是历史 API 卡券配置；新建 API 卡券仍被业务规则禁止。
	APIConfig string
	// TextContent 是 text 类型必须提供的非空发货内容。
	TextContent string
	// DataContent 是 data 类型必须提供的非空逐行库存。
	DataContent string
	// ImageURL 是 image 类型必须提供的非空图片地址。
	ImageURL string
	// Description 是用户维护的卡券组说明。
	Description string
	// Enabled 表示保存后是否允许自动化规则使用该卡券组。
	Enabled bool
	// DelaySeconds 是自动发货前的延迟秒数，范围为 0 到 3600。
	DelaySeconds int
	// IsMultiSpec 表示卡券组是否只匹配指定商品规格。
	IsMultiSpec bool
	// SpecName 是多规格匹配使用的规格名称。
	SpecName string
	// SpecValue 是多规格匹配使用的规格值。
	SpecValue string
}

// Repository 定义卡券 CRUD 用例所需的最小持久化能力。
type Repository interface {
	// ListForUser 按稳定顺序返回指定用户拥有的全部卡券组。
	ListForUser(ctx context.Context, userID int64) ([]Card, error)
	// Get 按标识读取卡券组；资源缺失时返回 ErrNotFound。
	Get(ctx context.Context, cardID int64) (Card, error)
	// Create 持久化已校验且带所有者的卡券组，并返回新标识。
	Create(ctx context.Context, card Card) (int64, error)
	// Update 覆盖指定卡券组的可编辑字段，但不得改变所有者。
	Update(ctx context.Context, card Card) error
	// Delete 删除指定卡券组及数据库约束允许级联清理的关联数据。
	Delete(ctx context.Context, cardID int64) error
	// AppendData 向 data 类型卡券组追加逐行卡密，并返回新增行数。
	AppendData(ctx context.Context, cardID int64, content string) (int, error)
}

// Service 编排卡券输入校验、用户归属和 CRUD 持久化。
type Service struct {
	// repository 保存卡券用例依赖的窄持久化 Port。
	repository Repository
}

// NewService 创建卡券应用服务；空仓储会在调用时返回明确装配错误。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// List 查询 userID 拥有的全部卡券组；仓储错误原样返回。
func (s *Service) List(ctx context.Context, userID int64) ([]Card, error) {
	// err 表示用户身份或应用仓储未满足执行条件的错误。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	return s.repository.ListForUser(ctx, userID)
}

// Get 查询 cardID 并校验其属于 userID；不存在、越权和仓储故障使用不同错误语义。
func (s *Service) Get(ctx context.Context, userID, cardID int64) (Card, error) {
	// err 表示用户身份或应用仓储未满足执行条件的错误。
	if err := s.validateUser(userID); err != nil {
		return Card{}, err
	}
	return s.ownedCard(ctx, userID, cardID)
}

// ExistsOwned 判断卡券组是否属于指定用户；基础设施错误与越权错误仍返回给调用方区分处理。
func (s *Service) ExistsOwned(ctx context.Context, userID, cardID int64) (bool, error) {
	// err 表示用户身份、卡券标识或归属查询错误。
	if err := s.validateUser(userID); err != nil {
		return false, err
	}
	if cardID <= 0 {
		return false, ErrInvalidCardID
	}
	// _, err 丢弃非敏感卡券详情，只保留归属判断结果。
	if _, err := s.ownedCard(ctx, userID, cardID); err != nil {
		return false, err
	}
	return true, nil
}

// Create 校验 draft 并以 userID 为所有者创建卡券组，返回新标识。
func (s *Service) Create(ctx context.Context, userID int64, draft Draft) (int64, error) {
	// err 表示用户身份或应用仓储未满足执行条件的错误。
	if err := s.validateUser(userID); err != nil {
		return 0, err
	}
	// err 表示卡券草稿未满足类型、内容或延迟范围约束的校验错误。
	if err := validateDraft(draft); err != nil {
		return 0, err
	}
	if draft.Type == "api" {
		return 0, ErrUnsupportedAPIType
	}
	return s.repository.Create(ctx, cardFromDraft(0, userID, draft))
}

// Update 校验 draft 和所有权后更新 cardID；既有 API 卡券可继续编辑，但其他类型不能转换为 API。
func (s *Service) Update(ctx context.Context, userID, cardID int64, draft Draft) error {
	// err 表示用户身份或应用仓储未满足执行条件的错误。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	// err 表示卡券草稿未满足类型、内容或延迟范围约束的校验错误。
	if err := validateDraft(draft); err != nil {
		return err
	}
	// existing 是已经通过当前用户归属校验的卡券组。
	existing, err := s.ownedCard(ctx, userID, cardID)
	if err != nil {
		return err
	}
	if draft.Type == "api" && existing.Type != "api" {
		return ErrUnsupportedAPIType
	}
	return s.repository.Update(ctx, cardFromDraft(existing.ID, existing.UserID, draft))
}

// Delete 校验 cardID 归属后删除卡券组；删除仓储错误原样返回。
func (s *Service) Delete(ctx context.Context, userID, cardID int64) error {
	// err 表示用户身份或应用仓储未满足执行条件的错误。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	// err 表示卡券不存在、越权或读取卡券时的基础设施错误。
	if _, err := s.ownedCard(ctx, userID, cardID); err != nil {
		return err
	}
	return s.repository.Delete(ctx, cardID)
}

// AppendData 校验卡券归属和类型后追加逐行库存，拒绝空内容和越权操作。
func (s *Service) AppendData(ctx context.Context, userID, cardID int64, content string) (int, error) {
	// err 表示用户身份或应用仓储未满足执行条件的错误。
	if err := s.validateUser(userID); err != nil {
		return 0, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, &ValidationError{Message: "内容为空"}
	}
	// card、err 保存已通过当前用户归属校验的卡券组及读取错误。
	card, err := s.ownedCard(ctx, userID, cardID)
	if err != nil {
		return 0, err
	}
	if card.Type != "data" {
		return 0, ErrNotDataType
	}
	return s.repository.AppendData(ctx, cardID, content)
}

// validateUser 检查应用服务及用户身份是否具备执行卡券用例的条件。
func (s *Service) validateUser(userID int64) error {
	if s == nil || s.repository == nil {
		return errors.New("卡券 repository 未初始化")
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	return nil
}

// ownedCard 读取 cardID 并验证 userID 所有权，不将数据库故障降级为资源缺失。
func (s *Service) ownedCard(ctx context.Context, userID, cardID int64) (Card, error) {
	if cardID <= 0 {
		return Card{}, ErrInvalidCardID
	}
	// card 是持久化层返回的完整卡券组。
	card, err := s.repository.Get(ctx, cardID)
	if err != nil {
		return Card{}, err
	}
	if card.UserID != userID {
		return Card{}, ErrForbidden
	}
	return card, nil
}

// validateDraft 校验类型、必填内容和延迟范围，保持创建与更新使用同一套规则。
func validateDraft(draft Draft) error {
	if draft.Name == "" || draft.Type == "" {
		return &ValidationError{Message: "名称和类型不能为空"}
	}
	switch draft.Type {
	case "text", "data", "image", "api":
	default:
		return &ValidationError{Message: "类型必须为 text、data、image 或 api"}
	}
	if draft.DelaySeconds < 0 || draft.DelaySeconds > 3600 {
		return &ValidationError{Message: "延时发货必须在 0 到 3600 秒之间"}
	}
	switch draft.Type {
	case "text":
		if strings.TrimSpace(draft.TextContent) == "" {
			return &ValidationError{Message: "文本卡密内容不能为空"}
		}
	case "data":
		if strings.TrimSpace(draft.DataContent) == "" {
			return &ValidationError{Message: "数据卡密内容不能为空"}
		}
	case "image":
		// imageURL、err 分别保存规范化后的远程图片地址和 URL 解析错误。
		imageURL, err := url.Parse(strings.TrimSpace(draft.ImageURL))
		if strings.TrimSpace(draft.ImageURL) == "" {
			return &ValidationError{Message: "图片卡密 URL 不能为空"}
		}
		if err != nil || imageURL.Hostname() == "" || imageURL.User != nil || (imageURL.Scheme != "http" && imageURL.Scheme != "https") {
			return &ValidationError{Message: "图片卡密 URL 必须是 HTTP(S) 地址"}
		}
	}
	return nil
}

// cardFromDraft 把已校验输入与不可由请求覆盖的标识、所有者组合为持久化模型。
func cardFromDraft(cardID, userID int64, draft Draft) Card {
	return Card{
		ID: cardID, Name: draft.Name, Type: draft.Type, APIConfig: draft.APIConfig,
		TextContent: draft.TextContent, DataContent: draft.DataContent, ImageURL: draft.ImageURL,
		Description: draft.Description, Enabled: draft.Enabled, DelaySeconds: draft.DelaySeconds,
		IsMultiSpec: draft.IsMultiSpec, SpecName: draft.SpecName, SpecValue: draft.SpecValue, UserID: userID,
	}
}
