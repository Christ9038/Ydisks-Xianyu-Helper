package composition

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
)

// TestReadBatchImageFileCoversPathAndContentGuards 覆盖批量图片读取的路径、文件大小和媒体类型边界。
func TestReadBatchImageFileCoversPathAndContentGuards(t *testing.T) {
	// uploadDir 是受测批次图片的隔离根目录。
	uploadDir := t.TempDir()
	// nestedDir 是上传目录中的合法子目录。
	nestedDir := filepath.Join(uploadDir, "nested")
	// mkdirErr 保存创建隔离图片目录的错误。
	if mkdirErr := os.MkdirAll(nestedDir, 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// imageBuffer 保存测试用 PNG 字节，避免依赖外部图片文件。
	imageBuffer := bytes.NewBuffer(nil)
	// imageValue 是用于编码的单像素图片。
	imageValue := image.NewRGBA(image.Rect(0, 0, 1, 1))
	imageValue.Set(0, 0, color.RGBA{R: 255, A: 255})
	// encodeErr 保存测试图片编码错误。
	if encodeErr := png.Encode(imageBuffer, imageValue); encodeErr != nil {
		t.Fatal(encodeErr)
	}
	// imagePath 是上传目录内的合法图片路径。
	imagePath := filepath.Join(nestedDir, "photo.png")
	// writeErr 保存测试图片写入错误。
	if writeErr := os.WriteFile(imagePath, imageBuffer.Bytes(), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// data、contentType、name、readErr 保存合法图片读取结果。
	data, contentType, name, readErr := readBatchImageFile(uploadDir, "nested/photo.png")
	if readErr != nil || len(data) == 0 || !strings.HasPrefix(contentType, "image/") || name != "photo.png" {
		t.Fatalf("valid image data=%d type=%q name=%q err=%v", len(data), contentType, name, readErr)
	}
	// invalidReferences 保存必须拒绝的路径引用。
	invalidReferences := []string{"", ".", "../photo.png", filepath.Join(uploadDir, "photo.png")}
	// reference 表示当前待验证的批次图片引用。
	for _, reference := range invalidReferences {
		// invalidErr 保存当前越界或空路径的拒绝结果。
		if _, _, _, invalidErr := readBatchImageFile(uploadDir, reference); invalidErr == nil {
			t.Fatalf("invalid reference %q should fail", reference)
		}
	}
	// missingErr 验证不存在文件的错误映射。
	if _, _, _, missingErr := readBatchImageFile(uploadDir, "missing.png"); missingErr == nil {
		t.Fatal("missing image should fail")
	}
	// emptyPath 是空文件路径，用于覆盖读取空内容分支。
	emptyPath := filepath.Join(uploadDir, "empty.png")
	// emptyErr 保存空文件写入错误。
	if emptyErr := os.WriteFile(emptyPath, nil, 0o600); emptyErr != nil {
		t.Fatal(emptyErr)
	}
	// emptyReadErr 保存读取空文件的业务错误。
	if _, _, _, emptyReadErr := readBatchImageFile(uploadDir, "empty.png"); emptyReadErr == nil {
		t.Fatal("empty image should fail")
	}
	// textPath 是内容不是图片的文件路径。
	textPath := filepath.Join(uploadDir, "text.png")
	// textErr 保存非图片测试文件写入错误。
	if textErr := os.WriteFile(textPath, []byte("not an image"), 0o600); textErr != nil {
		t.Fatal(textErr)
	}
	// textReadErr 保存读取非图片内容的业务错误。
	if _, _, _, textReadErr := readBatchImageFile(uploadDir, "text.png"); textReadErr == nil {
		t.Fatal("non-image content should fail")
	}
	// missingRootErr 验证上传根目录无法打开时的错误映射。
	if _, _, _, missingRootErr := readBatchImageFile(filepath.Join(uploadDir, "not-found"), "photo.png"); missingRootErr == nil {
		t.Fatal("missing upload root should fail")
	}
}

// TestPublishBatchFailureCoversStableClassifications 覆盖批量发布错误的稳定分类和取消文案。
func TestPublishBatchFailureCoversStableClassifications(t *testing.T) {
	// plainMessage、plainKind 保存普通发布错误的稳定结果。
	plainMessage, plainKind := publishBatchFailure(errors.New("普通失败"), "running")
	if plainMessage != "普通失败" || plainKind != "publish" {
		t.Fatalf("plain failure message=%q kind=%q", plainMessage, plainKind)
	}
	// uncertainMessage、uncertainKind 保存远端结果不确定错误的稳定结果。
	uncertainMessage, uncertainKind := publishBatchFailure(&itemapp.UncertainRemotePublishError{Err: errors.New("远端未知")}, "running")
	if uncertainKind != "uncertain_remote" || !strings.Contains(uncertainMessage, "禁止自动重试") {
		t.Fatalf("uncertain failure message=%q kind=%q", uncertainMessage, uncertainKind)
	}
	// canceledUncertainMessage、canceledUncertainKind 保存取消状态下的不确定错误文案。
	canceledUncertainMessage, canceledUncertainKind := publishBatchFailure(&itemapp.UncertainRemotePublishError{Err: errors.New("远端未知")}, "canceled")
	if canceledUncertainKind != "uncertain_remote" || !strings.HasPrefix(canceledUncertainMessage, "任务已取消；") {
		t.Fatalf("canceled uncertain message=%q kind=%q", canceledUncertainMessage, canceledUncertainKind)
	}
	// postMessage、postKind 保存远端成功后本地收口失败的分类。
	postMessage, postKind := publishBatchFailure(&itemapp.PostPublishError{Err: errors.New("收口失败")}, "running")
	if postMessage != "收口失败" || postKind != "post_publish" {
		t.Fatalf("post failure message=%q kind=%q", postMessage, postKind)
	}
	// canceledMessage、canceledKind 保存普通取消批次的统一文案。
	canceledMessage, canceledKind := publishBatchFailure(errors.New("普通失败"), "canceling")
	if canceledMessage != "任务已取消" || canceledKind != "publish" {
		t.Fatalf("canceled failure message=%q kind=%q", canceledMessage, canceledKind)
	}
}

// TestDownloadImageURLRejectsNonHTTPInputs 覆盖图片下载入口拒绝非法协议和缺少主机名的边界。
func TestDownloadImageURLRejectsNonHTTPInputs(t *testing.T) {
	// invalidURLs 保存不应触发公网请求的图片地址样本。
	invalidURLs := []string{"", "://bad", "ftp://example.com/image.png", "http://"}
	// rawURL 表示当前待校验的图片地址。
	for _, rawURL := range invalidURLs {
		// downloadErr 保存当前非法地址的下载错误。
		if _, _, downloadErr := downloadImageURL(context.Background(), rawURL); downloadErr == nil {
			t.Fatalf("invalid image URL %q should fail", rawURL)
		}
	}
}

// TestServicesBoundaryMethodsAreNilSafe 覆盖组合服务投影在未初始化阶段的安全返回值。
func TestServicesBoundaryMethodsAreNilSafe(t *testing.T) {
	// nilServices 表示组合根尚未构造完成的服务指针。
	var nilServices *Services
	if nilServices.LifecycleContext() != nil || nilServices.TransportPorts() != (TransportPorts{}) {
		t.Fatal("nil services should return empty projections")
	}
	if nilServices.UpdateRunningCookie(context.Background(), "account", "cookie") != nil {
		t.Fatal("nil services should ignore cookie updates")
	}
	if nilServices.RecoverExpiredCredential(context.Background(), "account") {
		t.Fatal("nil services should not recover credentials")
	}
	// emptyServices 表示已分配但尚未填充应用服务字段的组合对象。
	emptyServices := &Services{}
	// components 保存空组合服务应返回的生命周期组件列表。
	if components := emptyServices.LifecycleComponents(); len(components) != 0 {
		t.Fatalf("empty services components=%v", components)
	}
}

// TestAccountLoginServiceBoundaryMethodsAreNilSafe 覆盖账号登录组合服务在半初始化状态下的保护和字段归一化。
func TestAccountLoginServiceBoundaryMethodsAreNilSafe(t *testing.T) {
	// nilService 表示账号登录应用服务尚未装配完成。
	var nilService *accountLoginService
	// createErr、updateErr 保存未初始化登录命令的错误。
	createErr := nilService.CreateCookie(context.Background(), "account", "cookie", 1, "manual")
	// updateErr 保存未初始化更新命令的错误。
	updateErr := nilService.UpdateCookie(context.Background(), "account", "cookie", 1, "manual", 2)
	if createErr == nil || updateErr == nil {
		t.Fatalf("nil login service errors: create=%v update=%v", createErr, updateErr)
	}
	// persistErr 保存未初始化扫码持久化命令的错误。
	if _, persistErr := nilService.PersistQRLoginSuccess(context.Background(), 1, "session", nil, ""); persistErr == nil {
		t.Fatal("nil QR login service should fail")
	}
	// register、cleanup 验证 nil 接收者不会触发会话状态操作。
	nilService.RegisterQRSession("session", 1, time.Now())
	// cleanup 保存 nil 接收者的清理结果。
	if cleanup := nilService.CleanupQRSessions(time.Now()); cleanup != nil {
		t.Fatalf("nil cleanup result=%v", cleanup)
	}
	if nilService.AuthorizeQRSession("session", 1) == nil {
		t.Fatal("nil QR session service should reject authorization")
	}
	if nilService.CredentialSessionPort() != nil {
		t.Fatal("nil QR session service should expose no credential port")
	}
	// emptyService 表示实例存在但内部应用依赖仍未装配。
	emptyService := &accountLoginService{}
	if emptyService.CreateCookie(context.Background(), "account", "cookie", 1, "manual") == nil || emptyService.UpdateCookie(context.Background(), "account", "cookie", 1, "manual", 2) == nil {
		t.Fatal("empty login service should reject commands")
	}
	// result 保存用于字段类型归一化的平台结果。
	result := map[string]any{"text": "value", "number": 1}
	if resultString(result, "text") != "value" || resultString(result, "number") != "" || resultString(nil, "text") != "" {
		t.Fatal("resultString should only return string fields")
	}
	if firstNonEmpty("", "  ", "value", "later") != "value" || firstNonEmpty("", " ") != "" {
		t.Fatal("firstNonEmpty normalization failed")
	}
}
