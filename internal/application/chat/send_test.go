package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// sendRepository 是实时发送用例使用的内存持久化替身，记录消息状态迁移。
type sendRepository struct {
	// createErr 表示创建待发送消息时要返回的错误。
	createErr error
	// statusErr 表示更新发送状态时要返回的错误。
	statusErr error
	// message 保存最近一次创建的本地消息。
	message Message
	// statuses 保存按调用顺序写入的消息状态。
	statuses []string
}

// CreateOutgoing 创建测试文字消息。
func (r *sendRepository) CreateOutgoing(_ context.Context, session Session, _ string) (Message, error) {
	if r.createErr != nil {
		return Message{}, r.createErr
	}
	r.message = Message{ID: 1, AccountID: session.AccountID, ChatID: session.ChatID, MessageKey: "local-1", Status: "sending"}
	return r.message, nil
}

// CreateOutgoingMedia 创建测试媒体消息。
func (r *sendRepository) CreateOutgoingMedia(_ context.Context, session Session, _, content string) (Message, error) {
	if r.createErr != nil {
		return Message{}, r.createErr
	}
	r.message = Message{ID: 2, AccountID: session.AccountID, ChatID: session.ChatID, MessageKey: "local-image", Content: content, Status: "sending"}
	return r.message, nil
}

// SetOutgoingStatus 记录状态并返回当前消息。
func (r *sendRepository) SetOutgoingStatus(_ context.Context, _, _ string, status string) (Message, error) {
	r.statuses = append(r.statuses, status)
	if r.statusErr != nil {
		return Message{}, r.statusErr
	}
	r.message.Status = status
	return r.message, nil
}

// sendProvider 是按账号返回固定发送器的测试替身。
type sendProvider struct {
	// sender 保存测试发送器；为 nil 时模拟离线账号。
	sender *sendSender
}

// TestServiceAvailabilityReportsRequiredPorts 验证发送和图片上传能力只由应用端口装配状态决定。
func TestServiceAvailabilityReportsRequiredPorts(t *testing.T) {
	// unavailable 是没有任何外发端口的聊天应用服务。
	unavailable := NewWithSending(nil, nil, nil, nil)
	if unavailable.SendingAvailable() || unavailable.ImageUploadAvailable() {
		t.Fatal("缺少外发端口时不应报告聊天能力可用")
	}
	// available 是同时装配文字发送和图片上传端口的聊天应用服务。
	available := NewWithSending(nil, &sendRepository{}, sendProvider{}, sendUploader{})
	if !available.SendingAvailable() || !available.ImageUploadAvailable() {
		t.Fatal("完整装配的聊天能力应报告可用")
	}
	// identity 是构造函数可选的平台身份端口。
	identity := fakeIdentityResolver{}
	// withIdentity 是验证可选身份端口装配的服务。
	withIdentity := NewWithSending(nil, nil, nil, nil, identity)
	if withIdentity.identityResolver == nil {
		t.Fatal("optional identity resolver was not installed")
	}
}

// Sender 返回测试发送器或模拟离线。
func (p sendProvider) Sender(string) (Sender, bool) {
	return p.sender, p.sender != nil
}

// sendSender 记录平台发送调用及其预设错误。
type sendSender struct {
	// sendErr 表示平台发送需要返回的错误。
	sendErr error
	// sentKey 保存最近一次收到的幂等键。
	sentKey string
	// updatedCookie 保存适配器同步的刷新凭证；测试只记录是否调用，不保存真实秘密。
	updatedCookie bool
	// imageWidth、imageHeight 保存图片发送收到的像素尺寸。
	imageWidth, imageHeight int
}

// SendText 记录文本发送并返回预设错误。
func (s *sendSender) SendText(_ context.Context, _, _, _, messageKey string) error {
	s.sentKey = messageKey
	return s.sendErr
}

// SendImage 记录图片发送并返回预设错误。
func (s *sendSender) SendImage(_ context.Context, _, _, _ string, _ int64, width, height int, messageKey string) error {
	// imageWidth、imageHeight 记录应用层透传的平台图片尺寸。
	s.imageWidth, s.imageHeight = width, height
	s.sentKey = messageKey
	return s.sendErr
}

// UpdateCookie 记录刷新凭证同步动作，但不保存明文内容。
func (s *sendSender) UpdateCookie(string) {
	s.updatedCookie = true
}

// sendUploader 是图片上传端口的测试替身。
type sendUploader struct {
	// result 保存上传成功后返回的图片地址。
	result ImageUpload
	// err 保存上传或凭证刷新阶段需要返回的错误。
	err error
}

// UploadChatImage 返回预设上传结果，不接收明文 Cookie 参数。
func (u sendUploader) UploadChatImage(context.Context, string, string, string, []byte) (ImageUpload, error) {
	return u.result, u.err
}

// TestSendTextSuccessPreservesIdempotencyKey 验证成功发送会写入 sent 状态并传递本地幂等键。
func TestSendTextSuccessPreservesIdempotencyKey(t *testing.T) {
	// repository、sender 保存实时发送用例的测试端口。
	repository, sender := &sendRepository{}, &sendSender{}
	// service 保存使用测试端口构造的聊天发送服务。
	service := NewWithSending(nil, repository, sendProvider{sender: sender}, nil)
	// message 和 err 保存应用层返回的消息及错误。
	message, err := service.SendText(context.Background(), OutgoingInput{Session: Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}, Text: "  你好  "})
	if err != nil || message == nil || message.Status != "sent" || sender.sentKey != "local-1" {
		t.Fatalf("message=%+v err=%v key=%q", message, err, sender.sentKey)
	}
	if len(repository.statuses) != 1 || repository.statuses[0] != "sent" {
		t.Fatalf("statuses=%v", repository.statuses)
	}
}

// TestSendTextFailureMarksMessageFailed 验证平台失败会保留可重试的本地 failed 状态。
func TestSendTextFailureMarksMessageFailed(t *testing.T) {
	// repository、sender 保存发送失败场景的测试端口。
	repository, sender := &sendRepository{}, &sendSender{sendErr: errors.New("远端拒绝")}
	// service 保存使用测试端口构造的聊天发送服务。
	service := NewWithSending(nil, repository, sendProvider{sender: sender}, nil)
	// message 和 err 保存失败后的本地消息及错误。
	message, err := service.SendText(context.Background(), OutgoingInput{Session: Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}, Text: "你好"})
	if !errors.Is(err, ErrSend) || message == nil || message.Status != "failed" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
	if len(repository.statuses) != 1 || repository.statuses[0] != "failed" {
		t.Fatalf("statuses=%v", repository.statuses)
	}
}

// TestSendTextStatusFailureReturnsSentMessage 验证平台成功但本地状态写入失败会返回 ErrStatusSave。
func TestSendTextStatusFailureReturnsSentMessage(t *testing.T) {
	// repository 保存状态写入错误；sender 保存成功发送记录。
	repository, sender := &sendRepository{statusErr: errors.New("数据库不可用")}, &sendSender{}
	// service 保存使用测试端口构造的聊天发送服务。
	service := NewWithSending(nil, repository, sendProvider{sender: sender}, nil)
	// message 和 err 保存状态写入失败的返回值。
	message, err := service.SendText(context.Background(), OutgoingInput{Session: Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}, Text: "你好"})
	if !errors.Is(err, ErrStatusSave) || message == nil || message.MessageKey != "local-1" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
}

// TestSendTextPropagatesOutgoingCreationFailure 验证本地待发送消息创建失败时不会访问平台发送端口。
func TestSendTextPropagatesOutgoingCreationFailure(t *testing.T) {
	// createErr 是本地待发送消息创建端口返回的稳定错误。
	createErr := errors.New("create outgoing failed")
	// sender 是不应被调用的在线发送器替身。
	sender := &sendSender{}
	// service 是绑定本地创建失败端口的聊天发送服务。
	service := NewWithSending(nil, &sendRepository{createErr: createErr}, sendProvider{sender: sender}, nil)
	// returnedErr 保存应用服务返回的创建失败错误。
	_, returnedErr := service.SendText(context.Background(), OutgoingInput{Session: Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}, Text: "你好"})
	if returnedErr == nil || !strings.Contains(returnedErr.Error(), "create outgoing failed") || sender.sentKey != "" {
		t.Fatalf("创建外发消息失败未透传：err=%v sender=%q", returnedErr, sender.sentKey)
	}
}

// TestSendRejectsUnavailableOfflineAndInvalidInputs 验证不可用、离线和非法输入均在访问端口前失败。
func TestSendRejectsUnavailableOfflineAndInvalidInputs(t *testing.T) {
	// session 保存可复用的有效会话参数。
	session := Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}
	// cases 描述发送服务边界分支。
	cases := []struct {
		// name 标识当前测试分支。
		name string
		// service 保存当前分支使用的聊天服务。
		service *Service
		// input 保存当前分支使用的发送输入。
		input OutgoingInput
		// wantErr 保存预期应用错误。
		wantErr error
	}{
		{name: "unavailable", service: NewWithSending(nil, nil, nil, nil), input: OutgoingInput{Session: session, Text: "你好"}, wantErr: ErrUnavailable},
		{name: "offline", service: NewWithSending(nil, &sendRepository{}, sendProvider{}, nil), input: OutgoingInput{Session: session, Text: "你好"}, wantErr: ErrOffline},
		{name: "invalid", service: NewWithSending(nil, &sendRepository{}, sendProvider{sender: &sendSender{}}, nil), input: OutgoingInput{Session: session, Text: ""}, wantErr: ErrSendInvalidInput},
	}
	// testCase 表示当前遍历的发送服务边界分支。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 保存当前边界分支返回的应用错误。
			_, err := testCase.service.SendText(context.Background(), testCase.input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error=%v want=%v", err, testCase.wantErr)
			}
		})
	}
}

// TestSendImageDoesNotExposeCredentials 验证图片应用端口只接收账号标识，不传递 Cookie 字段。
func TestSendImageDoesNotExposeCredentials(t *testing.T) {
	// repository、sender、uploader 保存图片发送的测试端口。
	repository, sender := &sendRepository{}, &sendSender{}
	// uploader 保存固定图片地址的测试上传端口。
	uploader := sendUploader{result: ImageUpload{URL: "https://cdn.example/image.jpg", Width: 1280, Height: 720}}
	// service 保存使用测试端口构造的聊天发送服务。
	service := NewWithSending(nil, repository, sendProvider{sender: sender}, uploader)
	// message 和 err 保存图片发送结果。
	message, err := service.SendImage(context.Background(), ImageInput{Session: Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}, Filename: "a.jpg", ContentType: "image/jpeg", Data: []byte("image")})
	if err != nil || message == nil || message.Content != "https://cdn.example/image.jpg" || sender.sentKey != "local-image" || sender.imageWidth != 1280 || sender.imageHeight != 720 {
		t.Fatalf("message=%+v err=%v key=%q", message, err, sender.sentKey)
	}
}

// TestSendImagePropagatesCredentialWritebackFailure 验证平台适配器返回凭证写回错误时不会静默继续发送。
func TestSendImagePropagatesCredentialWritebackFailure(t *testing.T) {
	// repository、sender、uploader 保存凭证写回失败场景的测试端口。
	repository, sender := &sendRepository{}, &sendSender{}
	// uploader 保存模拟凭证写回失败的测试上传端口。
	uploader := sendUploader{err: errors.New("凭证写回失败")}
	// service 保存使用测试端口构造的聊天发送服务。
	service := NewWithSending(nil, repository, sendProvider{sender: sender}, uploader)
	// _, err 保存图片上传适配器返回的确定性错误。
	_, err := service.SendImage(context.Background(), ImageInput{Session: Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}, Filename: "a.jpg", ContentType: "image/jpeg", Data: []byte("image")})
	if !errors.Is(err, ErrSend) || sender.sentKey != "" || len(repository.statuses) != 0 {
		t.Fatalf("err=%v sentKey=%q statuses=%v", err, sender.sentKey, repository.statuses)
	}
}

// TestSendImageStatusFailureReturnsSentMessage 验证图片平台成功但本地状态写入失败仍返回幂等消息。
func TestSendImageStatusFailureReturnsSentMessage(t *testing.T) {
	// repository、sender、uploader 保存图片状态写入失败场景的测试端口。
	repository, sender := &sendRepository{statusErr: errors.New("状态写入失败")}, &sendSender{}
	// uploader 保存固定图片地址的测试上传端口。
	uploader := sendUploader{result: ImageUpload{URL: "https://cdn.example/image.jpg"}}
	// service 保存使用测试端口构造的聊天发送服务。
	service := NewWithSending(nil, repository, sendProvider{sender: sender}, uploader)
	// message 和 err 保存图片状态写入失败后的返回值。
	message, err := service.SendImage(context.Background(), ImageInput{Session: Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}, Filename: "a.jpg", ContentType: "image/jpeg", Data: []byte("image")})
	if !errors.Is(err, ErrStatusSave) || message == nil || message.MessageKey != "local-image" || sender.sentKey != "local-image" {
		t.Fatalf("message=%+v err=%v key=%q", message, err, sender.sentKey)
	}
}

// TestSendImageCoversValidationUploadCreationAndPlatformFailures 验证图片发送在各外发阶段失败时的稳定错误语义。
func TestSendImageCoversValidationUploadCreationAndPlatformFailures(t *testing.T) {
	// session 是图片发送测试共用的有效会话。
	session := Session{AccountID: "acc-1", ChatID: "chat-1", BuyerID: "buyer-1"}
	// input 是图片发送测试共用的有效输入。
	input := ImageInput{Session: session, Filename: "a.jpg", ContentType: "image/jpeg", Data: []byte("image")}
	// unavailable 是缺少图片发送端口的服务。
	unavailable := NewWithSending(nil, nil, nil, nil)
	// unavailableErr 保存缺少图片发送端口时的错误。
	if _, unavailableErr := unavailable.SendImage(context.Background(), input); !errors.Is(unavailableErr, ErrUnavailable) {
		t.Fatalf("unavailable image error=%v", unavailableErr)
	}
	// emptyData 是没有图片内容的非法输入。
	emptyData := input
	emptyData.Data = nil
	// ready 是具备所有图片发送端口的服务。
	ready := NewWithSending(nil, &sendRepository{}, sendProvider{sender: &sendSender{}}, sendUploader{result: ImageUpload{URL: "https://cdn.example/image.jpg"}})
	// emptyDataErr 保存空图片内容的输入错误。
	if _, emptyDataErr := ready.SendImage(context.Background(), emptyData); !errors.Is(emptyDataErr, ErrSendInvalidInput) {
		t.Fatalf("empty image error=%v", emptyDataErr)
	}
	// invalidSession 保存缺少账号标识的图片发送输入。
	invalidSession := input
	invalidSession.Session.AccountID = ""
	// invalidSessionErr 保存图片会话标识校验错误。
	if _, invalidSessionErr := ready.SendImage(context.Background(), invalidSession); !errors.Is(invalidSessionErr, ErrSendInvalidInput) {
		t.Fatalf("invalid image session error=%v", invalidSessionErr)
	}
	// offline 是没有在线发送器的图片服务。
	offline := NewWithSending(nil, &sendRepository{}, sendProvider{}, sendUploader{})
	// offlineErr 保存离线账号的图片发送错误。
	if _, offlineErr := offline.SendImage(context.Background(), input); !errors.Is(offlineErr, ErrOffline) {
		t.Fatalf("offline image error=%v", offlineErr)
	}
	// uploadFailure 是图片上传返回错误的服务。
	uploadFailure := NewWithSending(nil, &sendRepository{}, sendProvider{sender: &sendSender{}}, sendUploader{err: errors.New("upload failed")})
	// uploadErr 保存图片上传失败的错误。
	if _, uploadErr := uploadFailure.SendImage(context.Background(), input); !errors.Is(uploadErr, ErrSend) {
		t.Fatalf("upload failure=%v", uploadErr)
	}
	// emptyURL 是图片上传没有返回可发送地址的服务。
	emptyURL := NewWithSending(nil, &sendRepository{}, sendProvider{sender: &sendSender{}}, sendUploader{result: ImageUpload{}})
	// emptyURLErr 保存图片上传没有地址时的错误。
	if _, emptyURLErr := emptyURL.SendImage(context.Background(), input); !errors.Is(emptyURLErr, ErrSend) {
		t.Fatalf("empty URL error=%v", emptyURLErr)
	}
	// createFailure 是本地待发送图片消息写入失败的服务。
	createFailure := NewWithSending(nil, &sendRepository{createErr: errors.New("create failed")}, sendProvider{sender: &sendSender{}}, sendUploader{result: ImageUpload{URL: "https://cdn.example/image.jpg"}})
	// createErr 保存本地图片消息写入失败的错误。
	if _, createErr := createFailure.SendImage(context.Background(), input); createErr == nil {
		t.Fatalf("create failure=%v", createErr)
	}
	// senderFailure 是平台图片发送失败的服务。
	// senderFailureRepository 保存平台发送失败后的本地状态记录。
	senderFailureRepository := &sendRepository{}
	// senderFailure 是平台图片发送失败的聊天服务。
	senderFailure := NewWithSending(nil, senderFailureRepository, sendProvider{sender: &sendSender{sendErr: errors.New("send failed")}}, sendUploader{result: ImageUpload{URL: "https://cdn.example/image.jpg"}})
	// message、sendErr 保存平台图片发送失败后的本地消息及错误。
	message, sendErr := senderFailure.SendImage(context.Background(), input)
	if !errors.Is(sendErr, ErrSend) || message == nil || len(senderFailureRepository.statuses) != 1 || senderFailureRepository.statuses[0] != "failed" {
		t.Fatalf("message=%+v err=%v statuses=%v", message, sendErr, senderFailureRepository.statuses)
	}
}
