package adapter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	accountmanager "xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/netguard"
	"xianyu-go/internal/xianyu/mtop"
)

// automationImageMaxBytes 限制一次自动发货从远程 URL 读取的图片体积，避免无状态容器被单个卡密占满内存。
const automationImageMaxBytes = 10 << 20

// automationImageDownloader 只在一次自动发货内下载图片内容；返回值不会写入本地文件或数据库。
type automationImageDownloader func(ctx context.Context, rawURL string) (data []byte, contentType string, filename string, err error)

// automationImageUploader 定义自动化图片上传所需的最小平台能力，凭证始终封装在 adapter 内部。
type automationImageUploader interface {
	// UploadChatImage 将内存中的图片上传为当前账号可发送的闲鱼图片地址。
	UploadChatImage(ctx context.Context, accountID, filename, contentType string, data []byte) (chatapp.ImageUpload, error)
}

// automationImageSenderProvider 将在线账号发送器包装为图片卡密的下载、上传、发送流水线。
type automationImageSenderProvider struct {
	// manager 按账号标识返回当前在线的 WebSocket 发送器。
	manager *accountmanager.Manager
	// uploader 负责以实际发货账号上传图片，并在需要时同步刷新后的凭证。
	uploader automationImageUploader
	// downloader 在每次发送前临时读取卡密 URL 指向的图片字节。
	downloader automationImageDownloader
}

// automationImageSender 把单个账号的原始发送器与图片下载、上传能力绑定，避免自动化领域接触协议或凭证。
type automationImageSender struct {
	// accountID 是本次自动化发货实际使用的平台账号标识。
	accountID string
	// sender 是账号运行时提供的最终 WebSocket 发送器。
	sender automation.MessageSender
	// uploader 将下载结果转换为闲鱼图片地址。
	uploader automationImageUploader
	// downloader 只在发送图片时执行远程读取。
	downloader automationImageDownloader
}

// NewAutomationImageSenderProvider 创建自动化中心使用的图片发送器来源。
// 图片 URL 不持久化为本地文件，下载和上传都限定在单次自动化动作的上下文内。
func NewAutomationImageSenderProvider(store *db.Store, manager *accountmanager.Manager, clientProvider func() mtop.Client) automation.SenderProvider {
	// uploader 复用聊天图片上传适配器，确保凭证刷新写回和在线运行时同步保持一致。
	uploader := NewChatImageUploader(store, clientProvider, manager)
	return automationImageSenderProvider{
		manager: manager, uploader: uploader, downloader: downloadAutomationImage,
	}
}

// Sender 返回指定在线账号的发送包装器；账号离线或初始化不完整时维持既有不可发送语义。
func (p automationImageSenderProvider) Sender(accountID string) (automation.MessageSender, bool) {
	if p.manager == nil || p.uploader == nil || p.downloader == nil {
		return nil, false
	}
	// sender、ok 分别表示账号运行时的原始发送器和其在线可用状态。
	sender, ok := p.manager.GetInstance(accountID)
	if !ok || sender == nil {
		return nil, false
	}
	return automationImageSender{
		accountID: accountID, sender: sender, uploader: p.uploader, downloader: p.downloader,
	}, true
}

// SendText 保持文本自动发货原有的直接 WebSocket 发送行为。
func (s automationImageSender) SendText(ctx context.Context, chatID, toUserID, text string) error {
	if s.sender == nil {
		return fmt.Errorf("%w: 账号发送器未初始化", automation.ErrMessageNotSent)
	}
	return s.sender.SendText(ctx, chatID, toUserID, text)
}

// SendImage 将图片卡密 URL 在内存中下载、上传到闲鱼图片服务后，再交给账号运行时发送。
// 下载或上传失败发生在 WebSocket 写入之前，必须标记为确定未发送以允许自动化安全重试。
func (s automationImageSender) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, _, _ int) error {
	if s.sender == nil || s.uploader == nil || s.downloader == nil {
		return fmt.Errorf("%w: 图片发送器未初始化", automation.ErrMessageNotSent)
	}
	// data、contentType、filename 和 err 分别保存远程图片的瞬时字节、媒体类型、上传名称和下载错误。
	data, contentType, filename, err := s.downloader(ctx, imageURL)
	if err != nil {
		return fmt.Errorf("%w: 下载图片卡密失败: %v", automation.ErrMessageNotSent, err)
	}
	// uploaded、err 保存闲鱼图片服务返回的发送地址和上传阶段错误。
	uploaded, err := s.uploader.UploadChatImage(ctx, s.accountID, filename, contentType, data)
	if err != nil {
		return fmt.Errorf("%w: 上传图片卡密失败: %v", automation.ErrMessageNotSent, err)
	}
	if strings.TrimSpace(uploaded.URL) == "" {
		return fmt.Errorf("%w: 上传图片卡密未返回地址", automation.ErrMessageNotSent)
	}
	return s.sender.SendImage(ctx, chatID, toUserID, uploaded.URL, cardID, uploaded.Width, uploaded.Height)
}

// UpdateCookie 将账号运行时主动更新的 Cookie 透传给原始发送器，不改变既有凭证协调责任。
func (s automationImageSender) UpdateCookie(cookieStr string) {
	if s.sender != nil {
		s.sender.UpdateCookie(cookieStr)
	}
}

// downloadAutomationImage 从 HTTP(S) 图片 URL 读取有限大小的内存数据，拒绝内网、重定向降级和非图片响应。
func downloadAutomationImage(ctx context.Context, rawURL string) ([]byte, string, string, error) {
	// parsed、err 分别保存规范化后的来源地址和解析错误。
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", "", fmt.Errorf("图片 URL 无效")
	}
	// request、err 保存绑定自动化取消上下文的下载请求及构造错误。
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("创建图片下载请求失败")
	}
	// client 只允许访问公网地址，并在重定向时重复校验目标，避免卡密 URL 触发 SSRF。
	client := netguard.PublicHTTPClient(30 * time.Second)
	// response、err 保存远程图片响应及网络访问失败原因。
	response, err := client.Do(request)
	if err != nil {
		return nil, "", "", fmt.Errorf("下载图片失败")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", "", fmt.Errorf("下载图片返回 HTTP %d", response.StatusCode)
	}
	// data、err 保存受最大体积限制的响应内容及读取错误。
	data, err := io.ReadAll(io.LimitReader(response.Body, automationImageMaxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("读取图片失败")
	}
	if len(data) == 0 || len(data) > automationImageMaxBytes {
		return nil, "", "", fmt.Errorf("图片大小必须在 1 B 到 10 MiB 之间")
	}
	// contentType 保存去除参数后的响应媒体类型；缺失或通用类型时根据图片字节检测。
	contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return nil, "", "", fmt.Errorf("远程内容不是图片")
	}
	// filename 保存来源路径中的展示名称；缺失名称时使用与媒体类型无关的默认值。
	filename := filepath.Base(parsed.Path)
	if filename == "." || filename == "/" || filename == "" {
		filename = "image"
	}
	return data, contentType, filename, nil
}

// 编译期确认自动化图片包装器保持自动化中心依赖的最小发送契约。
var _ automation.MessageSender = automationImageSender{}
