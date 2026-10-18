package deliverytemplate

import (
	"fmt"
	"net/url"
	"strings"

	"xianyu-go/internal/replyimage"
)

// Message 是一条独立发送的文本或图片；图片来源固定，不参与任何模板插值。
type Message struct {
	// Type 缺省为 text；image 使用恰好一个图片来源并禁止正文。
	Type string `json:"type"`
	// Content 是文本正文，图片消息必须为空。
	Content string `json:"content"`
	// ImageURL 是固定 HTTP(S) 图片地址，与 ImagePath 互斥。
	ImageURL string `json:"image_url,omitempty"`
	// ImagePath 是实际执行账号 reply-images 目录内的相对路径，不绑定保存模板时的账号。
	ImagePath string `json:"image_path,omitempty"`
}

// TextMessages 把历史文本列表复制为显式文本消息，保留原顺序与正文空白。
func TextMessages(contents []string) []Message {
	// messages 保存独立分配的消息列表，避免草稿共享可变切片。
	messages := make([]Message, 0, len(contents))
	// content 是当前历史文本正文。
	for _, content := range contents {
		messages = append(messages, Message{Type: "text", Content: content})
	}
	return messages
}

// ParseMessages 校验 messages 并返回有序结构化消息；兼容 Messages 只包含文本，图片不提取变量。
func ParseMessages(messages []Message) (Parsed, error) {
	if len(messages) == 0 {
		return Parsed{}, fmt.Errorf("发货模板至少需要一条消息")
	}
	// items 保存校验后的消息副本；texts 保存供历史文本解析器提取变量的正文。
	items := make([]Message, 0, len(messages))
	// texts 不含图片占位，避免旧调用方把图片来源当正文发送。
	texts := make([]string, 0, len(messages))
	// index 是发送顺序，message 是不共享调用方状态的当前消息副本。
	for index, message := range messages {
		if message.Type == "" {
			message.Type = "text"
		}
		switch message.Type {
		case "text":
			if message.ImageURL != "" || message.ImagePath != "" {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条文本消息不能包含图片来源", index+1)
			}
			texts = append(texts, message.Content)
		case "image":
			if message.Content != "" {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条图片消息正文必须为空", index+1)
			}
			// err 保存固定图片来源的校验错误，不返回用户提交的地址或路径。
			if err := ValidateImageSource(message.ImageURL, message.ImagePath); err != nil {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条消息: %w", index+1, err)
			}
		default:
			return Parsed{}, fmt.Errorf("发货模板第 %d 条消息类型必须为 text 或 image", index+1)
		}
		items = append(items, message)
	}
	// parsed 保存文本变量解析结果；纯图片模板不要求额外的文本消息。
	parsed := Parsed{Messages: texts}
	if len(texts) > 0 {
		// err 保存文本正文或变量语法校验错误。
		var err error
		parsed, err = parseTextMessages(texts)
		if err != nil {
			return Parsed{}, err
		}
	}
	parsed.MessageItems = items
	return parsed, nil
}

// ValidateImageSource 要求 imageURL、imagePath 恰好提供一个静态图片来源；本地文件由执行账号的受限加载器读取。
func ValidateImageSource(imageURL, imagePath string) error {
	if (imageURL == "") == (imagePath == "") {
		return fmt.Errorf("图片 URL 和本地图片路径必须且只能配置一个")
	}
	if strings.Contains(imageURL+imagePath, "{{") || strings.Contains(imageURL+imagePath, "}}") {
		return fmt.Errorf("图片来源不支持模板变量")
	}
	if imagePath != "" {
		return replyimage.ValidatePath(imagePath)
	}
	// parsed、err 保存固定远程图片地址的结构及语法错误。
	parsed, err := url.Parse(imageURL)
	if err != nil || strings.TrimSpace(imageURL) != imageURL || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("图片 URL 必须是不含用户凭据的 HTTP(S) 地址")
	}
	return nil
}
