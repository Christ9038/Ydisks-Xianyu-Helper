package engine

import (
	"context"
	"strings"
	"testing"
)

// TestOutgoingCoordinatorCoversInitializationAndInputBoundaries 验证出站消息协调器的空接收者、空输入、本地图片和连接状态边界。
func TestOutgoingCoordinatorCoversInitializationAndInputBoundaries(t *testing.T) {
	// nilCoordinator 表示未完成装配的出站协调器。
	nilCoordinator := &outgoingMessageCoordinator{}
	if nilCoordinator.sendText(context.Background(), "chat", "buyer", "text") == nil {
		t.Fatal("空文本协调器应返回初始化错误")
	}
	if nilCoordinator.sendImage(context.Background(), "chat", "buyer", "https://cdn/image", 0, 1, 1) == nil {
		t.Fatal("空图片协调器应返回初始化错误")
	}

	// account 是尚未建立 WebSocket 连接的账号运行时。
	account := New(Config{CookieID: "outgoing-boundary", CookieStr: "unb=buyer"})
	// coordinator 保存账号出站协调器。
	coordinator := &outgoingMessageCoordinator{account: account}
	// emptyTextErr 表示空文本输入的发送结果。
	emptyTextErr := coordinator.sendText(context.Background(), "chat", "buyer", "   ")
	if emptyTextErr != nil {
		t.Fatalf("空文本应静默跳过：%v", emptyTextErr)
	}
	// emptyImageErr 表示空图片 URL 的发送结果。
	emptyImageErr := coordinator.sendImage(context.Background(), "chat", "buyer", " ", 0, 1, 1)
	if emptyImageErr != nil {
		t.Fatalf("空图片 URL 应静默跳过：%v", emptyImageErr)
	}
	// localImageErr 表示本地图片 URL 的拒绝错误。
	localImageErr := coordinator.sendImage(context.Background(), "chat", "buyer", "/static/local.png", 0, 1, 1)
	if localImageErr == nil {
		t.Fatal("本地图片 URL 应明确拒绝")
	}
	// missingConnectionErr 表示没有 WebSocket 连接时的发送状态错误。
	_, _, missingConnectionErr := coordinator.currentSenderState()
	if missingConnectionErr == nil {
		t.Fatal("没有连接时应返回发送状态错误")
	}
	if coordinator.automationReady() {
		t.Fatal("没有连接时不应允许自动化发送")
	}

	// conn 是记录文本和图片发送参数的本地连接。
	conn := &fakeWSConn{}
	account.runtimeMu.Lock()
	account.conn = conn
	account.runtimeState = RuntimeOnline
	account.runtimeMu.Unlock()
	// account.UserID 清空后由 Cookie 中的 unb 回退提供发送身份。
	account.UserID = ""
	// textSendErr 表示连接可用时的文本发送结果。
	textSendErr := coordinator.sendText(context.Background(), "chat", "buyer", "  hello  ")
	if textSendErr != nil {
		t.Fatalf("文本发送失败：%v", textSendErr)
	}
	if len(conn.sentTexts) != 1 || conn.sentTexts[0] != "hello" {
		t.Fatalf("文本发送结果=%v", conn.sentTexts)
	}
	// imageSendErr 表示连接可用时的图片发送结果。
	imageSendErr := coordinator.sendImage(context.Background(), "chat", "buyer", "https://cdn/image", 0, 640, 480)
	if imageSendErr != nil {
		t.Fatalf("图片发送失败：%v", imageSendErr)
	}
	if len(conn.sentImages) != 1 || !strings.Contains(conn.sentImages[0], "https://cdn/image") {
		t.Fatalf("图片发送结果=%v", conn.sentImages)
	}
	if !coordinator.automationReady() {
		t.Fatal("在线连接应允许自动化发送")
	}
}

// TestOutgoingCoordinatorRejectsMissingMessageIdentity 验证连接存在但账号缺少 unb 身份时拒绝出站消息。
func TestOutgoingCoordinatorRejectsMissingMessageIdentity(t *testing.T) {
	// account 是只有连接、没有可用用户身份的账号运行时。
	account := New(Config{CookieID: "missing-identity", CookieStr: "sid=1"})
	// conn 是可用但无法弥补账号身份缺失的本地连接。
	conn := &fakeWSConn{}
	account.runtimeMu.Lock()
	account.conn = conn
	account.runtimeMu.Unlock()
	// coordinator 保存账号出站协调器。
	coordinator := &outgoingMessageCoordinator{account: account}
	// missingIdentityErr 表示连接存在但账号缺少 unb 身份时的错误。
	_, _, missingIdentityErr := coordinator.currentSenderState()
	if missingIdentityErr == nil {
		t.Fatal("缺少 unb 时应拒绝出站状态")
	}
}
