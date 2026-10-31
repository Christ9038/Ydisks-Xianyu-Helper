package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestSendLibraryImageUsesTheMessagePageImagePipeline 验证图片库图片读取后复用统一上传链路，并透传真实宽高。
func TestSendLibraryImageUsesTheMessagePageImagePipeline(t *testing.T) {
	// repository、sender 保存统一发送链路的测试端口。
	repository, sender := &sendRepository{}, &sendSender{}
	// uploader 保存平台返回的真实图片地址和像素尺寸。
	uploader := sendUploader{result: ImageUpload{URL: "https://cdn.example/lib.jpg", Width: 800, Height: 600}}
	// service 是同时具备文字、图片上传和图片库读取能力的聊天应用服务。
	service := WithLibraryImages(NewWithSending(nil, repository, sendProvider{sender: sender}, uploader),
		/* accountID 与 imageID 由调用方原样传入，测试只断言它们被转交给读取端口。 */ func(_ context.Context, accountID string, imageID int64) ([]byte, string, string, error) {
			if accountID != "acc-1" || imageID != 7 {
				return nil, "", "", errors.New("unexpected image request")
			}
			return []byte("lib-image"), "image/jpeg", "lib.jpg", nil
		})
	// result、sendErr 保存完整回复投递结果及错误；文字应排在图片之后。
	result, sendErr := service.SendReply(context.Background(), ReplyInput{
		Session: Session{AccountID: "acc-1", ChatID: "chat-1", PeerUserID: "buyer-1"},
		Text:    "图片随后附上", LibraryImageID: 7,
	})
	if sendErr != nil || result == nil || !result.ImageSent || !result.TextSent {
		t.Fatalf("result=%+v err=%v", result, sendErr)
	}
	if sender.imageWidth != 800 || sender.imageHeight != 600 || strings.Join(sender.calls, ",") != "image,text" {
		t.Fatalf("统一发送顺序或尺寸错误 calls=%v size=%dx%d", sender.calls, sender.imageWidth, sender.imageHeight)
	}
}

// TestSendLibraryImageRejectsBadInputAndReadFailures 验证互斥、未装配、读取失败都发生在平台请求之前。
func TestSendLibraryImageRejectsBadInputAndReadFailures(t *testing.T) {
	// session 是所有用例共用的有效会话。
	session := Session{AccountID: "acc-1", ChatID: "chat-1", PeerUserID: "buyer-1"}
	// sender 记录是否发生过平台调用。
	sender := &sendSender{}
	// base 构造未注入图片库读取能力的服务。
	base := NewWithSending(nil, &sendRepository{}, sendProvider{sender: sender}, sendUploader{result: ImageUpload{URL: "https://cdn.example/x.jpg"}})
	// err 保存未装配读取端口时的发送错误。
	if _, err := base.SendLibraryImage(context.Background(), LibraryImageInput{Session: session, ImageID: 1}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("未装配读取端口应不可用, err=%v", err)
	}
	// service 注入总是失败的读取端口，模拟图片已被删除。
	service := WithLibraryImages(base, func(context.Context, string, int64) ([]byte, string, string, error) {
		return nil, "", "", errors.New("图片不存在")
	})
	// err 保存图片读取失败时的发送错误。
	if _, err := service.SendLibraryImage(context.Background(), LibraryImageInput{Session: session, ImageID: 1}); !errors.Is(err, ErrSend) {
		t.Fatalf("读取失败应为 ErrSend, err=%v", err)
	}
	// err 保存图片标识无效时的发送错误。
	if _, err := service.SendLibraryImage(context.Background(), LibraryImageInput{Session: session, ImageID: 0}); !errors.Is(err, ErrSendInvalidInput) {
		t.Fatalf("图片标识无效应拒绝, err=%v", err)
	}
	// err 保存 URL 与图片库同时指定时的发送错误。
	if _, err := service.SendReply(context.Background(), ReplyInput{Session: session, ImageURL: "https://a.example/x.jpg", LibraryImageID: 1}); !errors.Is(err, ErrSendInvalidInput) {
		t.Fatalf("URL 与图片库互斥, err=%v", err)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("失败路径不应触发平台发送 calls=%v", sender.calls)
	}
}
