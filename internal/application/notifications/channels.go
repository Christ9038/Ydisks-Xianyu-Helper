package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrChannelNotFound 表示通知渠道或账号绑定目标不存在。
	ErrChannelNotFound = errors.New("通知渠道不存在")
	// ErrChannelForbidden 表示当前用户无权操作通知渠道或账号绑定。
	ErrChannelForbidden = errors.New("无权操作通知渠道")
	// ErrChannelInvalidInput 表示渠道字段、用户归属或绑定参数无效。
	ErrChannelInvalidInput = errors.New("通知渠道参数无效")
	// ErrAccountForbidden 表示账号不存在或不属于当前用户。
	ErrAccountForbidden = errors.New("无权操作账号通知绑定")
	// ErrNotifierUnavailable 表示测试发送依赖尚未装配。
	ErrNotifierUnavailable = errors.New("通知器未启用")
)

// ChannelSummary 是通知渠道的非敏感展示模型；它刻意不包含配置 JSON。
type ChannelSummary struct {
	// ID 是渠道稳定标识。
	ID int64
	// Name 是用户可识别的渠道名称。
	Name string
	// Type 是通知渠道协议类型。
	Type string
	// EventTypes 是渠道订阅的事件类型编码。
	EventTypes string
	// Enabled 表示渠道是否参与通知投递。
	Enabled bool
	// UserID 是渠道所属用户，仅供应用层归属校验和管理员展示。
	UserID int64
}

// ChannelEditor 是通知渠道编辑器需要的非敏感配置视图；它不携带 SMTP 密码或其他渠道凭据。
type ChannelEditor struct {
	// ID 是渠道稳定标识。
	ID int64
	// Name 是用户可识别的渠道名称。
	Name string
	// Type 是通知渠道协议类型。
	Type string
	// EventTypes 是渠道订阅的事件类型编码。
	EventTypes string
	// Enabled 表示渠道是否参与通知投递。
	Enabled bool
	// ToEmail 是邮件渠道的收件地址；其他渠道为空。
	ToEmail string
	// UseCustomSMTP 表示邮件渠道是否使用独立 SMTP；不代表任何 SMTP 凭据内容。
	UseCustomSMTP bool
}

// ChannelRecord 是通知渠道更新时由持久化端口返回的内部记录，Config 仅允许留在应用编排与存储边界。
type ChannelRecord struct {
	// ID 是渠道稳定标识。
	ID int64
	// Name 是渠道名称。
	Name string
	// Type 是渠道协议类型。
	Type string
	// Config 是加密存储解密后的配置 JSON，禁止进入 HTTP 或应用响应。
	Config string
	// EventTypes 是渠道订阅事件类型编码。
	EventTypes string
	// Enabled 表示渠道是否参与通知投递。
	Enabled bool
	// UserID 是渠道所属用户。
	UserID int64
}

// ChannelInput 是创建通知渠道时的业务输入；Config 只用于写入敏感配置。
type ChannelInput struct {
	// Name 是渠道名称。
	Name string
	// Type 是渠道协议类型。
	Type string
	// Config 是待加密保存的渠道配置 JSON，禁止记录日志或返回响应。
	Config string
	// EventTypes 是渠道订阅事件类型编码。
	EventTypes string
	// Enabled 表示创建后是否启用渠道。
	Enabled bool
}

// ChannelPatch 是通知渠道的部分更新输入；nil 字段表示保留现值。
type ChannelPatch struct {
	// Name 是可选的新渠道名称。
	Name *string
	// Type 是可选的新渠道协议类型。
	Type *string
	// Config 是可选的新敏感配置 JSON，禁止记录日志或返回响应。
	Config *string
	// EmailRecipient 仅更新已有邮件渠道的收件地址，保留全部 SMTP 字段；与 Config 互斥。
	EmailRecipient *string
	// EventTypes 是可选的新订阅事件类型编码。
	EventTypes *string
	// Enabled 是可选的新启用状态。
	Enabled *bool
}

// BindingSummary 是账号与通知渠道绑定的非敏感展示模型。
type BindingSummary struct {
	// ID 是绑定记录稳定标识。
	ID int64
	// CookieID 是绑定账号标识。
	CookieID string
	// ChannelID 是绑定渠道标识。
	ChannelID int64
	// ChannelName 是绑定渠道名称。
	ChannelName string
	// Enabled 表示绑定是否启用。
	Enabled bool
}

// ChannelRepository 定义通知渠道和账号绑定用例所需的最小持久化端口。
type ChannelRepository interface {
	// ListChannels 查询指定用户的非敏感渠道摘要。
	ListChannels(ctx context.Context, userID int64) ([]ChannelSummary, error)
	// GetChannelForUpdate 查询指定用户拥有的渠道完整记录，仅供更新合并使用。
	GetChannelForUpdate(ctx context.Context, channelID, userID int64) (*ChannelRecord, error)
	// CreateChannel 保存一条渠道并返回稳定标识。
	CreateChannel(ctx context.Context, userID int64, input ChannelInput) (int64, error)
	// UpdateChannel 保存已通过归属校验的完整渠道记录。
	UpdateChannel(ctx context.Context, userID int64, record ChannelRecord) error
	// DeleteChannel 删除指定用户拥有的渠道。
	DeleteChannel(ctx context.Context, channelID, userID int64) error
	// OwnsChannel 判断渠道是否属于指定用户。
	OwnsChannel(ctx context.Context, channelID, userID int64) (bool, error)
	// OwnsAccount 判断账号是否属于指定用户，不读取账号凭证。
	OwnsAccount(ctx context.Context, userID int64, cookieID string) (bool, error)
	// ListBindings 查询指定用户的非敏感绑定摘要。
	ListBindings(ctx context.Context, userID int64) ([]BindingSummary, error)
	// GetBindingIDs 查询账号当前启用的渠道标识。
	GetBindingIDs(ctx context.Context, cookieID string) ([]int64, error)
	// SetBindings 覆盖保存账号的渠道绑定。
	SetBindings(ctx context.Context, cookieID string, channelIDs []int64) error
	// SetSingleBinding 更新单个账号渠道绑定状态。
	SetSingleBinding(ctx context.Context, cookieID string, channelID int64, enabled bool) error
	// DeleteBinding 删除指定用户的一条绑定。
	DeleteBinding(ctx context.Context, userID, bindingID int64) error
	// DeleteAccountBindings 删除指定用户账号的全部绑定。
	DeleteAccountBindings(ctx context.Context, userID int64, cookieID string) error
}

// ChannelSender 定义测试通知发送能力，避免应用层依赖具体通知器。
type ChannelSender interface {
	// SendToChannel 向指定渠道发送测试正文。
	SendToChannel(channelID int64, body string) error
}

// ChannelService 编排通知渠道 CRUD、账号绑定和测试发送，不依赖 HTTP 或数据库模型。
type ChannelService struct {
	// repository 保存渠道与绑定的最小持久化端口。
	repository ChannelRepository
	// sender 保存测试通知发送端口；普通 CRUD 不依赖该端口。
	sender ChannelSender
}

// NewChannelService 构造通知渠道应用服务。
func NewChannelService(repository ChannelRepository, sender ChannelSender) *ChannelService {
	return &ChannelService{repository: repository, sender: sender}
}

// ListChannels 查询用户拥有的非敏感通知渠道摘要。
func (s *ChannelService) ListChannels(ctx context.Context, userID int64) ([]ChannelSummary, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrChannelInvalidInput
	}
	return s.repository.ListChannels(ctx, userID)
}

// GetChannelEditor 按用户归属读取编辑器所需的非敏感渠道字段。
func (s *ChannelService) GetChannelEditor(ctx context.Context, userID, channelID int64) (ChannelEditor, error) {
	if s == nil || s.repository == nil || userID <= 0 || channelID <= 0 {
		return ChannelEditor{}, ErrChannelInvalidInput
	}
	// record 保存通过用户归属校验的完整内部渠道记录；其 Config 只在本方法内解析，禁止进入响应。
	record, lookupErr := s.repository.GetChannelForUpdate(ctx, channelID, userID)
	if lookupErr != nil {
		return ChannelEditor{}, lookupErr
	}
	if record == nil {
		return ChannelEditor{}, ErrChannelForbidden
	}
	// editor 保存剔除敏感配置后的编辑器视图。
	editor := ChannelEditor{ID: record.ID, Name: record.Name, Type: record.Type, EventTypes: record.EventTypes, Enabled: record.Enabled}
	if record.Type != "email" {
		return editor, nil
	}
	// config 保存仅为读取收件地址和 SMTP 模式而解析的渠道配置；不会被原样返回。
	var config map[string]any
	if json.Unmarshal([]byte(record.Config), &config) != nil {
		return editor, nil
	}
	// recipient 保存兼容 to_email/email 两种历史字段后的收件地址。
	recipient := strings.TrimSpace(stringValue(config["to_email"]))
	if recipient == "" {
		recipient = strings.TrimSpace(stringValue(config["email"]))
	}
	editor.ToEmail = recipient
	// modeValue 保存邮件渠道是否显式开启独立 SMTP 的布尔值。
	modeValue, modePresent := config["use_custom_smtp"]
	if modePresent {
		editor.UseCustomSMTP = parseConfigBool(modeValue)
	} else {
		// key 枚举旧版逐字段 SMTP 覆盖，缺少显式模式时仍应展示其自定义来源。
		for _, key := range []string{"smtp_server", "smtp_port", "smtp_user", "smtp_password", "smtp_from", "smtp_from_name", "smtp_from_address", "smtp_use_tls", "smtp_use_ssl"} {
			if config[key] != nil && strings.TrimSpace(fmt.Sprint(config[key])) != "" {
				editor.UseCustomSMTP = true
				break
			}
		}
	}
	return editor, nil
}

// CreateChannel 校验并创建通知渠道；敏感配置只向持久化端口传递。
func (s *ChannelService) CreateChannel(ctx context.Context, userID int64, input ChannelInput) (int64, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return 0, ErrChannelInvalidInput
	}
	// validationErr 表示创建输入的名称和协议类型校验结果。
	if validationErr := validateChannelFields(input.Name, input.Type); validationErr != nil {
		return 0, validationErr
	}
	return s.repository.CreateChannel(ctx, userID, input)
}

// UpdateChannel 合并部分更新、校验最终字段并保存用户拥有的渠道。
func (s *ChannelService) UpdateChannel(ctx context.Context, userID, channelID int64, patch ChannelPatch) error {
	if s == nil || s.repository == nil || userID <= 0 || channelID <= 0 {
		return ErrChannelInvalidInput
	}
	// record 保存已通过归属查询的渠道记录；lookupErr 保存读取失败原因。
	record, lookupErr := s.repository.GetChannelForUpdate(ctx, channelID, userID)
	if lookupErr != nil {
		return lookupErr
	}
	if record == nil {
		return ErrChannelForbidden
	}
	if patch.EmailRecipient != nil {
		if record.Type != "email" || patch.Config != nil || (patch.Type != nil && *patch.Type != "email") {
			return ErrChannelInvalidInput
		}
		// config、patchErr 只修改收件地址；配置无效时拒绝写入，不能重建空对象丢弃服务端秘密。
		config, patchErr := patchEmailRecipient(record.Config, *patch.EmailRecipient)
		if patchErr != nil {
			return patchErr
		}
		record.Config = config
	}
	if patch.Name != nil {
		record.Name = *patch.Name
	}
	if patch.Type != nil {
		record.Type = *patch.Type
	}
	if patch.Config != nil {
		record.Config = *patch.Config
	}
	if patch.EventTypes != nil {
		record.EventTypes = *patch.EventTypes
	}
	if patch.Enabled != nil {
		record.Enabled = *patch.Enabled
	}
	// validationErr 表示合并后的渠道名称和协议类型校验结果。
	if validationErr := validateChannelFields(record.Name, record.Type); validationErr != nil {
		return validationErr
	}
	record.UserID = userID
	return s.repository.UpdateChannel(ctx, userID, *record)
}

// patchEmailRecipient 将 recipient 写入原 config JSON，保留旧版逐字段覆盖、未知配置和秘密；返回值只能进入存储。
// 空收件地址或无法解析为对象的旧配置返回输入错误，不输出配置内容。
func patchEmailRecipient(config, recipient string) (string, error) {
	if strings.TrimSpace(recipient) == "" {
		return "", ErrChannelInvalidInput
	}
	// fields 保留所有旧字段的 JSON 表达；其值可能包含明文凭据，禁止日志和 HTTP 响应使用。
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(config), &fields) != nil || fields == nil {
		return "", ErrChannelInvalidInput
	}
	// value 是只替换 to_email 的 JSON 字符串，字符串序列化不会失败。
	value, _ := json.Marshal(strings.TrimSpace(recipient))
	fields["to_email"] = value
	// encoded、err 保存合并后的完整配置及序列化错误，内容仅用于持久化。
	encoded, err := json.Marshal(fields)
	return string(encoded), err
}

// DeleteChannel 删除用户拥有的通知渠道。
func (s *ChannelService) DeleteChannel(ctx context.Context, userID, channelID int64) error {
	if s == nil || s.repository == nil || userID <= 0 || channelID <= 0 {
		return ErrChannelInvalidInput
	}
	return s.repository.DeleteChannel(ctx, channelID, userID)
}

// OwnsChannel 查询渠道是否属于指定用户；只返回归属结论，不读取渠道配置。
func (s *ChannelService) OwnsChannel(ctx context.Context, userID, channelID int64) (bool, error) {
	if s == nil || s.repository == nil || userID <= 0 || channelID <= 0 {
		return false, ErrChannelInvalidInput
	}
	return s.repository.OwnsChannel(ctx, channelID, userID)
}

// TestChannel 校验渠道归属并发送固定格式测试通知；body 不包含渠道配置或凭证。
func (s *ChannelService) TestChannel(ctx context.Context, userID, channelID int64, now time.Time) error {
	if s == nil || s.repository == nil || userID <= 0 || channelID <= 0 {
		return ErrChannelInvalidInput
	}
	if s.sender == nil {
		return ErrNotifierUnavailable
	}
	// owned 表示渠道归属结果；ownershipErr 保存归属查询失败原因。
	owned, ownershipErr := s.repository.OwnsChannel(ctx, channelID, userID)
	if ownershipErr != nil {
		return ownershipErr
	}
	if !owned {
		return ErrChannelForbidden
	}
	if now.IsZero() {
		now = time.Now()
	}
	// body 是固定测试通知正文，不包含渠道敏感配置。
	body := fmt.Sprintf("🧪 通知渠道测试\n\n这是一条来自Ydisks闲鱼助手的测试通知，收到说明渠道配置正常。\n时间: %s", now.Format("2006-01-02 15:04:05"))
	return s.sender.SendToChannel(channelID, body)
}

// ListBindings 查询用户所有账号的非敏感通知绑定。
func (s *ChannelService) ListBindings(ctx context.Context, userID int64) ([]BindingSummary, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrChannelInvalidInput
	}
	return s.repository.ListBindings(ctx, userID)
}

// GetBindingIDs 校验账号归属后查询其启用渠道 ID。
func (s *ChannelService) GetBindingIDs(ctx context.Context, userID int64, cookieID string) ([]int64, error) {
	// validationErr 表示账号归属校验结果。
	if validationErr := s.validateAccount(ctx, userID, cookieID); validationErr != nil {
		return nil, validationErr
	}
	return s.repository.GetBindingIDs(ctx, cookieID)
}

// SetBindings 校验账号和全部渠道归属后覆盖保存绑定。
func (s *ChannelService) SetBindings(ctx context.Context, userID int64, cookieID string, channelIDs []int64) error {
	// validationErr 表示账号归属校验结果。
	if validationErr := s.validateAccount(ctx, userID, cookieID); validationErr != nil {
		return validationErr
	}
	// channelID 表示当前待校验归属的渠道。
	for _, channelID := range channelIDs {
		// owned 表示渠道归属结果；ownershipErr 保存归属查询失败原因。
		owned, ownershipErr := s.repository.OwnsChannel(ctx, channelID, userID)
		if ownershipErr != nil {
			return ownershipErr
		}
		if !owned {
			return ErrChannelForbidden
		}
	}
	return s.repository.SetBindings(ctx, cookieID, channelIDs)
}

// SetSingleBinding 校验账号和渠道归属后更新单条绑定状态。
func (s *ChannelService) SetSingleBinding(ctx context.Context, userID int64, cookieID string, channelID int64, enabled bool) error {
	// validationErr 表示账号归属校验结果。
	if validationErr := s.validateAccount(ctx, userID, cookieID); validationErr != nil {
		return validationErr
	}
	// owned 表示渠道归属结果；ownershipErr 保存归属查询失败原因。
	owned, ownershipErr := s.repository.OwnsChannel(ctx, channelID, userID)
	if ownershipErr != nil {
		return ownershipErr
	}
	if !owned {
		return ErrChannelForbidden
	}
	return s.repository.SetSingleBinding(ctx, cookieID, channelID, enabled)
}

// DeleteBinding 删除用户的一条绑定，归属由持久化端口再次约束。
func (s *ChannelService) DeleteBinding(ctx context.Context, userID, bindingID int64) error {
	if s == nil || s.repository == nil || userID <= 0 || bindingID <= 0 {
		return ErrChannelInvalidInput
	}
	return s.repository.DeleteBinding(ctx, userID, bindingID)
}

// DeleteAccountBindings 校验账号归属后删除其全部绑定。
func (s *ChannelService) DeleteAccountBindings(ctx context.Context, userID int64, cookieID string) error {
	// validationErr 表示账号归属校验结果。
	if validationErr := s.validateAccount(ctx, userID, cookieID); validationErr != nil {
		return validationErr
	}
	return s.repository.DeleteAccountBindings(ctx, userID, cookieID)
}

// validateAccount 校验用户和账号标识，并确保端口返回的结果属于当前用户。
func (s *ChannelService) validateAccount(ctx context.Context, userID int64, cookieID string) error {
	if s == nil || s.repository == nil || userID <= 0 || strings.TrimSpace(cookieID) == "" {
		return ErrChannelInvalidInput
	}
	// owned 表示账号归属结果；ownershipErr 保存归属查询失败原因。
	owned, ownershipErr := s.repository.OwnsAccount(ctx, userID, cookieID)
	if ownershipErr != nil {
		return ownershipErr
	}
	if !owned {
		return ErrAccountForbidden
	}
	return nil
}

// validateChannelFields 校验业务必填字段，避免将无名或无类型渠道写入存储。
func validateChannelFields(name, channelType string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(channelType) == "" {
		return ErrChannelInvalidInput
	}
	return nil
}

// stringValue 将编辑器安全读取所需的 JSON 标量转换为字符串；复杂值视为空值。
func stringValue(value any) string {
	// textValue 是配置中可安全用于编辑器回显的字符串；ok 表示 JSON 值确实为字符串类型。
	textValue, ok := value.(string)
	if !ok {
		return ""
	}
	return textValue
}

// parseConfigBool 将邮件配置中的布尔兼容值归一化为布尔结果。
func parseConfigBool(value any) bool {
	// typedValue 是当前配置布尔值按其 JSON 实际类型解包后的内容。
	switch typedValue := value.(type) {
	case bool:
		return typedValue
	case string:
		return strings.EqualFold(strings.TrimSpace(typedValue), "true") || strings.TrimSpace(typedValue) == "1"
	case float64:
		return typedValue == 1
	default:
		return false
	}
}
