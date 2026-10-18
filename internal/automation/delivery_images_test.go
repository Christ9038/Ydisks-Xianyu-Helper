package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// localDeliverySender 模拟本地图片的准备及平台直发，嵌入旧文本发送器以验证混合顺序。
type localDeliverySender struct {
	// testSender 保留文本记录和旧 URL 发送的兼容能力。
	testSender
	// paths 记录首次上传需要读取的本地来源。
	paths []string
	// urls 记录新模板图片首次下载的来源，重发不再次读取。
	urls []string
	// images 记录实际平台直发收到的图片，不包含本地路径。
	images []UploadedImage
	// prepareErr 和 imageErr 分别注入准备失败与最终发送结果。
	prepareErr, imageErr error
	// afterPrepare 可在上传结束时取消执行，验证耗时操作之后重新核验执行权。
	afterPrepare func()
}

// PrepareLocalImage 记录 reference，返回固定的平台图片；ctx 生命周期由执行器控制。
func (s *localDeliverySender) PrepareLocalImage(_ context.Context, reference string) (UploadedImage, error) {
	s.paths = append(s.paths, reference)
	if s.afterPrepare != nil {
		s.afterPrepare()
	}
	return UploadedImage{URL: "https://cdn.example/frozen.png", Width: 40, Height: 30}, s.prepareErr
}

// PrepareImageURL 模拟新模板 URL 首次准备；后续来源不可用时仍有平台快照可重发。
func (s *localDeliverySender) PrepareImageURL(_ context.Context, rawURL string) (UploadedImage, error) {
	s.urls = append(s.urls, rawURL)
	if s.afterPrepare != nil {
		s.afterPrepare()
	}
	return UploadedImage{URL: "https://cdn.example/frozen.png", Width: 40, Height: 30}, s.prepareErr
}

// SendUploadedImage 记录 image 及与文本相同的顺序事件，不重新准备原素材。
func (s *localDeliverySender) SendUploadedImage(_ context.Context, _, _ string, image UploadedImage, _ int64) error {
	s.images = append(s.images, image)
	if s.events != nil {
		*s.events = append(*s.events, "image:"+image.URL)
	}
	return s.imageErr
}

// TestDeliverLocalImageProof 验证本地图片成功/未知/确定未发送时的快照和计数语义。
func TestDeliverLocalImageProof(t *testing.T) {
	// scenarios 覆盖发送生命周期中需要不同恢复决策的结果。
	scenarios := []struct {
		// name 描述结果分支；prepareErr 与 sendErr 控制两阶段错误。
		name string
		// prepareErr 是读取或上传失败，不应保存买家消息快照。
		prepareErr error
		// sendErr 是最终消息结果，只有未知结果保存人工核对快照。
		sendErr error
	}{
		{name: "success"},
		{name: "prepare-failed", prepareErr: errors.New("load failed")},
		{name: "not-sent", sendErr: ErrMessageNotSent},
		{name: "uncertain", sendErr: errors.New("no echo")},
	}
	for /* scenario 保存当前两阶段错误配置。 */ _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// sender 和 executor 只注入图片端口，不触及真实账号或文件。
			sender := &localDeliverySender{prepareErr: scenario.prepareErr, imageErr: scenario.sendErr}
			// executor 通过实际账号发送器执行图片准备和发送。
			executor := automationActionExecutor{senders: blockingSenderProvider{sender: sender}}
			// result、err 保存该图片单位的计数和错误分类。
			result, err := executor.deliverImage(context.Background(), Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}, "", "stock/code.png", 3)
			if scenario.name == "success" {
				if err != nil || result.sent != 1 || result.proof.preparedUnits != 1 {
					t.Fatalf("成功图片计数错误: %v", err)
				}
				assertUploadedDeliveryProof(t, result.proof)
			} else if scenario.name == "uncertain" {
				if err == nil || result.sent != 0 || result.reviewProof.unknownUnits != 1 {
					t.Fatalf("未知结果未隔离: %v", err)
				}
				assertUploadedDeliveryProof(t, result.reviewProof)
			} else if !errors.Is(err, ErrMessageNotSent) || len(result.proof.messages)+len(result.reviewProof.messages) != 0 {
				t.Fatalf("确定未发送建立了错误凭证: %v", err)
			}
			if scenario.prepareErr != nil && len(sender.images) != 0 {
				t.Fatal("准备失败后仍发送图片")
			}
		})
	}
}

// assertUploadedDeliveryProof 校验 proof 中只有平台引用和实际尺寸，没有原始宿主路径。
func assertUploadedDeliveryProof(t *testing.T, proof shipmentDeliveryProof) {
	t.Helper()
	if len(proof.picList) != 1 || proof.picList[0] != "https://cdn.example/frozen.png" || len(proof.messages) != 1 {
		t.Fatal("图片凭证不是已上传的平台引用")
	}
	// message 是加密快照应保留的唯一图片消息。
	message := proof.messages[0]
	if message.Kind != "uploaded_image" || message.Content != proof.picList[0] || message.Width != 40 || message.Height != 30 {
		t.Fatal("快照缺少原样发送的类型或尺寸")
	}
}

// TestUploadedImageReplaySkipsSource 验证快照发送不调用本地准备或旧 URL 下载发送通道。
func TestUploadedImageReplaySkipsSource(t *testing.T) {
	// sender 令任何再次准备失败，确保补发只使用已有图片。
	sender := &localDeliverySender{prepareErr: errors.New("source deleted")}
	// executor 无数据库或卡密依赖，快照自身就足以原样发送。
	executor := automationActionExecutor{senders: blockingSenderProvider{sender: sender}}
	// message 模拟从加密凭证恢复的平台图片元数据。
	message := db.AutomationDeliveryMessage{Kind: "uploaded_image", Content: "https://cdn.example/original.png", Width: 11, Height: 12}
	if err := executor.sendDeliveryImage(context.Background(), Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}, message, 0); err != nil { // err 不得包含重新读取来源失败。
		t.Fatal(err)
	}
	if len(sender.paths) != 0 || len(sender.images) != 1 || sender.images[0].URL != message.Content || sender.images[0].Width != 11 {
		t.Fatal("快照补发重新读取了来源或改变了图片")
	}
}

// TestLocalDeliveryRechecksCancellationAfterUpload 验证慢上传结束后取消会阻止最终发送且不生成未知结果。
func TestLocalDeliveryRechecksCancellationAfterUpload(t *testing.T) {
	// ctx、cancel 模拟上传阶段完成时由运行所有者取消执行。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// sender 在准备完成后取消，而不是在进入动作之前取消。
	sender := &localDeliverySender{afterPrepare: cancel}
	// executor 必须在最终图片消息前重新检查同一执行上下文。
	executor := automationActionExecutor{senders: blockingSenderProvider{sender: sender}}
	// result、err 保存取消后单位结果。
	result, err := executor.deliverImage(ctx, Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}, "", "code.png", 0)
	if !errors.Is(err, ErrMessageNotSent) || !errors.Is(err, context.Canceled) || len(sender.images) != 0 || len(result.reviewProof.messages) != 0 {
		t.Fatalf("上传后取消仍然发送或丢失错误类别: %v", err)
	}
}

// TestLocalDeliveryRejectsInvalidSources 验证来源错误、缺少账号能力和损坏快照在任何发送前被拒绝。
func TestLocalDeliveryRejectsInvalidSources(t *testing.T) {
	// task 提供其余合法的发送身份。
	task := Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}
	// sender 保持旧 URL 能力但没有新增的本地图片端口。
	sender := &testSender{}
	// executor 用于断言不能把本地路径退回旧 URL 发送通道。
	executor := automationActionExecutor{senders: testSenderProvider{sender: sender}}
	for /* source 表示空、冲突、穿越或缺少能力的来源组合。 */ _, source := range [][2]string{{"", ""}, {"https://example.test/a.png", "a.png"}, {"", "../a.png"}, {"", "a.png"}} {
		if _, err := executor.deliverImage(context.Background(), task, source[0], source[1], 0); !errors.Is(err, ErrMessageNotSent) { // err 必须可判定为尚未发送。
			t.Fatalf("非法来源或缺少能力未被拒绝: %v", err)
		}
	}
	for /* message 是缺少类型、内容或尺寸的损坏快照。 */ _, message := range []db.AutomationDeliveryMessage{{Kind: "invalid"}, {Kind: "uploaded_image"}, {Kind: "uploaded_image", Content: "https://example.test/a.png", Width: 1}} {
		if err := executor.sendDeliveryImage(context.Background(), task, message, 0); !errors.Is(err, ErrMessageNotSent) { // err 不得进入最终发送的不确定分支。
			t.Fatalf("损坏图片快照未被拒绝: %v", err)
		}
	}
}
