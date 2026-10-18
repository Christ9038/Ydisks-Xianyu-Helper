package automation

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/db"
	"xianyu-go/internal/replyimage"
)

// deliverImage 发送 imageURL 或 imagePath 指定的一张图片，返回一个发货单位的结果与快照。
// 本地图先上传并冻结平台引用，任何未知发送结果只保留该引用，绝不把宿主路径写进 PicList。
func (e *automationActionExecutor) deliverImage(ctx context.Context, task Task, imageURL, imagePath string, cardID int64) (actionExecutionResult, error) {
	if err := replyimage.ValidateSource(imageURL, imagePath); err != nil { // err 阻止历史脏配置绕过来源互斥或路径校验。
		return actionExecutionResult{}, fmt.Errorf("%w: %w", ErrMessageNotSent, err)
	}
	if imageURL == "" && imagePath == "" {
		return actionExecutionResult{}, fmt.Errorf("%w: 图片缺少来源", ErrMessageNotSent)
	}
	// sender、senderErr 保证仅在实际账号可用时准备本地或新模板 URL 图片。
	sender, senderErr := e.preparedImageSender(ctx, task)
	if senderErr != nil {
		return actionExecutionResult{}, senderErr
	}
	// uploaded 与 prepareErr 保存尚未发送买家消息的准备结果，来源读取不会进入快照。
	var uploaded UploadedImage
	// prepareErr 在读取或上传失败时保证没有需要人工重发的消息。
	var prepareErr error
	if imagePath != "" {
		uploaded, prepareErr = sender.PrepareLocalImage(ctx, imagePath)
	} else {
		uploaded, prepareErr = sender.PrepareImageURL(ctx, imageURL)
	}
	if prepareErr != nil {
		return actionExecutionResult{}, fmt.Errorf("%w: %w", ErrMessageNotSent, prepareErr)
	}
	// message 只冻结实际平台引用；旧 image 快照仅在补发入口兼容。
	message := db.AutomationDeliveryMessage{Kind: "uploaded_image", Content: uploaded.URL, Width: uploaded.Width, Height: uploaded.Height}
	// sendErr 在耗时准备结束后再次核验执行权，未知结果保留同一张平台图片供人工核对。
	sendErr := e.sendDeliveryImage(ctx, task, message, cardID)
	if sendErr != nil {
		if errors.Is(sendErr, ErrMessageNotSent) {
			return actionExecutionResult{}, classifyMessageSendError(sendErr)
		}
		return actionExecutionResult{reviewProof: shipmentDeliveryProof{
			unknownUnits: 1, picList: []string{message.Content}, messages: []db.AutomationDeliveryMessage{message},
		}}, classifyMessageSendError(sendErr)
	}
	return actionExecutionResult{sent: 1, proof: shipmentDeliveryProof{
		preparedUnits: 1, picList: []string{message.Content}, messages: []db.AutomationDeliveryMessage{message},
	}}, nil
}

// preparedImageSender 按 task 的实际账号取得图片准备端口，所有准备及快照发送都先核验 ctx 执行权。
func (e *automationActionExecutor) preparedImageSender(ctx context.Context, task Task) (PreparedImageSender, error) {
	if err := checkRunExecution(ctx); err != nil { // err 防止取消或失权后读取文件、上传或发送。
		return nil, fmt.Errorf("%w: %w", ErrMessageNotSent, err)
	}
	if task.ChatID == "" || task.BuyerID == "" {
		return nil, fmt.Errorf("%w: 发送图片缺少 chat_id 或 buyer_id", ErrMessageNotSent)
	}
	if e.senders == nil {
		return nil, fmt.Errorf("%w: 账号发送器未初始化", ErrMessageNotSent)
	}
	// sender、online 保存实际账号的在线发送器，不能使用素材配置指定其他账号。
	sender, online := e.senders.Sender(task.AccountID)
	if !online || sender == nil {
		return nil, fmt.Errorf("%w: 账号未在线，无法发送自动化图片", ErrMessageNotSent)
	}
	// prepared、supported 保持旧 URL 卡密发送器兼容，新模板和本地图片不能静默退回来源 URL 发送。
	prepared, supported := sender.(PreparedImageSender)
	if !supported {
		return nil, fmt.Errorf("%w: 账号发送器不支持图片准备与快照发送", ErrMessageNotSent)
	}
	return prepared, nil
}

// sendDeliveryImage 根据 message 的冻结来源向 task 对应会话发送一张图片，cardID 仅关联首次卡密发送。
// uploaded_image 不经过下载器或本地 loader，历史 image 继续使用原 URL 流程。
func (e *automationActionExecutor) sendDeliveryImage(ctx context.Context, task Task, message db.AutomationDeliveryMessage, cardID int64) error {
	if message.Kind == "image" {
		return e.sendImage(ctx, task, message.Content, cardID)
	}
	if message.Kind != "uploaded_image" || message.Content == "" || message.Width <= 0 || message.Height <= 0 {
		return fmt.Errorf("%w: 图片快照类型、地址或尺寸无效", ErrMessageNotSent)
	}
	// sender、senderErr 在本地读取和平台上传之后重新检查运行执行权与实际账号可用性。
	sender, senderErr := e.preparedImageSender(ctx, task)
	if senderErr != nil {
		return senderErr
	}
	return sender.SendUploadedImage(ctx, task.ChatID, task.BuyerID, UploadedImage{URL: message.Content, Width: message.Width, Height: message.Height}, cardID)
}
