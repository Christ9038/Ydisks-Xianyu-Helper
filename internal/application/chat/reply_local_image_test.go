package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestSendReplyLocalImageUsesUnifiedPipeline 验证本地图片跳过 URL 下载，复用上传、尺寸与分段确认。
func TestSendReplyLocalImageUsesUnifiedPipeline(t *testing.T) {
	// sender 记录真实应用入口调用图片与文字的顺序。
	sender := &sendSender{}
	// uploader 固定上传后的平台地址和真实尺寸。
	uploader := sendUploader{result: ImageUpload{URL: "https://cdn.example/reply.png", Width: 2, Height: 3}}
	// loads 记录文件读取次数，确保一次完整回复只加载一次。
	loads := 0
	// loader 校验应用只把非敏感账号和相对引用交给文件端口。
	loader := func(_ context.Context, accountID, reference string) ([]byte, string, string, error) {
		loads++
		if accountID != "account-a" || reference != "商品/回复.png" {
			t.Fatalf("加载账号或路径错误：%q %q", accountID, reference)
		}
		return []byte("local-image"), "image/png", "回复.png", nil
	}
	// downloader 是不应被本地图片分支使用的网络端口。
	downloader := func(context.Context, string) ([]byte, string, string, error) {
		t.Fatal("本地图片不应下载 URL")
		return nil, "", "", nil
	}
	// service 在构造阶段固定本地图片和 URL 下载两种来源。
	service := NewWithReplyImageSources(nil, &sendRepository{}, sendProvider{sender: sender}, uploader, nil, nil, downloader, loader)
	// result 和 sendErr 保存统一发送入口的分段结果。
	result, sendErr := service.SendReply(context.Background(), ReplyInput{Session: Session{AccountID: "account-a", ChatID: "chat", PeerUserID: "buyer"}, Text: "回复", ImagePath: "商品/回复.png"})
	if sendErr != nil || result == nil || !result.ImageSent || !result.TextSent || loads != 1 || strings.Join(sender.calls, ",") != "image,text" || sender.imageWidth != 2 || sender.imageHeight != 3 {
		t.Fatalf("本地图片统一发送失败：result=%+v err=%v calls=%v", result, sendErr, sender.calls)
	}
}

// TestSendReplyLocalImageFailuresStopText 验证文件错误、未装配、来源冲突和非法内容不会发送残缺回复。
func TestSendReplyLocalImageFailuresStopText(t *testing.T) {
	// cases 描述本地图文回复需要覆盖的确定性错误。
	cases := []struct {
		// name 标识错误场景。
		name string
		// loader 是该场景注入的本地文件端口；nil 表示未配置能力。
		loader LocalImageLoader
		// imageURL 用于构造两个图片来源同时提供的非法请求。
		imageURL string
		// wantErr 是应用应该保留的稳定错误类别。
		wantErr error
	}{
		{name: "未装配", wantErr: ErrUnavailable},
		{name: "读取失败", loader: func(context.Context, string, string) ([]byte, string, string, error) {
			return nil, "", "", errors.New("读取失败")
		}, wantErr: ErrSend},
		{name: "空字节", loader: func(context.Context, string, string) ([]byte, string, string, error) {
			return nil, "image/png", "a.png", nil
		}, wantErr: ErrSendInvalidInput},
		{name: "无媒体类型", loader: func(context.Context, string, string) ([]byte, string, string, error) {
			return []byte("image"), "", "a.png", nil
		}, wantErr: ErrSendInvalidInput},
		{name: "冲突来源", imageURL: "https://origin.example/a.png", wantErr: ErrSendInvalidInput},
	}
	// testCase 保存当前验证的失败场景。
	for _, testCase := range cases {
		// t 是当前子场景的断言上下文。
		t.Run(testCase.name, func(t *testing.T) {
			// sender 记录应当保持为空的平台调用列表。
			sender := &sendSender{}
			// service 绑定当前场景的本地加载能力。
			service := NewWithReplyImageSources(nil, &sendRepository{}, sendProvider{sender: sender}, sendUploader{}, nil, nil, nil, testCase.loader)
			// sendErr 保存发送入口返回的确定性错误。
			_, sendErr := service.SendReply(context.Background(), ReplyInput{Session: Session{AccountID: "account-a", ChatID: "chat", PeerUserID: "buyer"}, Text: "不应发送", ImagePath: "a.png", ImageURL: testCase.imageURL})
			if !errors.Is(sendErr, testCase.wantErr) || len(sender.calls) != 0 {
				t.Fatalf("错误未阻止图文发送：err=%v calls=%v", sendErr, sender.calls)
			}
		})
	}
}
