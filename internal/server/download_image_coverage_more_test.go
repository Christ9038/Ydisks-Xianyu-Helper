package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/netguard"
)

// TestDownloadImageURLCoversLocalResponseBranches 用本地端点覆盖图片下载的响应分类和内容类型分支。
func TestDownloadImageURLCoversLocalResponseBranches(t *testing.T) {
	// previousPublicOnly 保存全局出站策略，避免测试改变其他用例的网络边界。
	previousPublicOnly := netguard.DefaultPublicOnly()
	netguard.SetDefaultPublicOnly(false)
	t.Cleanup(func() { netguard.SetDefaultPublicOnly(previousPublicOnly) })
	// server 提供成功、状态错误和内容类型错误响应。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/png":
			writer.Header().Set("Content-Type", "image/png; charset=utf-8")
			_, _ = writer.Write([]byte("png-data"))
		case "/octet":
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = writer.Write([]byte("\x89PNG\r\n\x1a\n"))
		case "/status":
			writer.WriteHeader(http.StatusBadGateway)
		case "/text":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("not image"))
		default:
			writer.Header().Set("Content-Type", "image/jpeg")
			_, _ = writer.Write([]byte("jpeg-data"))
		}
	}))
	t.Cleanup(server.Close)
	// imageData、imageType 保存带参数 Content-Type 的成功响应。
	imageData, imageType, imageErr := downloadImageURL(context.Background(), server.URL+"/png")
	if imageErr != nil || string(imageData) != "png-data" || imageType != "image/png" {
		t.Fatalf("png data=%q type=%q err=%v", imageData, imageType, imageErr)
	}
	// detectedData、detectedType 保存需要通过数据探测类型的响应。
	detectedData, detectedType, detectedErr := downloadImageURL(context.Background(), server.URL+"/octet")
	if detectedErr != nil || len(detectedData) == 0 || !strings.HasPrefix(detectedType, "image/") {
		t.Fatalf("detected data=%d type=%q err=%v", len(detectedData), detectedType, detectedErr)
	}
	// statusErr 保存远端非 2xx 响应错误。
	_, _, statusErr := downloadImageURL(context.Background(), server.URL+"/status")
	if statusErr == nil {
		t.Fatal("non-2xx response should fail")
	}
	// contentErr 保存远端非图片内容错误。
	_, _, contentErr := downloadImageURL(context.Background(), server.URL+"/text")
	if contentErr == nil {
		t.Fatal("non-image response should fail")
	}
	// canceledContext、cancelCanceled 保存主动取消的请求上下文。
	canceledContext, cancelCanceled := context.WithCancel(context.Background())
	cancelCanceled()
	// canceledErr 保存取消请求的客户端错误。
	_, _, canceledErr := downloadImageURL(canceledContext, server.URL+"/png")
	if canceledErr == nil {
		t.Fatal("canceled request should fail")
	}
}

// TestDownloadImageURLRejectsOversizedImage 验证远程图片大小上限不会被绕过。
func TestDownloadImageURLRejectsOversizedImage(t *testing.T) {
	// previousPublicOnly 保存全局出站策略，避免测试改变其他用例的网络边界。
	previousPublicOnly := netguard.DefaultPublicOnly()
	netguard.SetDefaultPublicOnly(false)
	t.Cleanup(func() { netguard.SetDefaultPublicOnly(previousPublicOnly) })
	// server 提供超过 10 MiB 限制的图片响应。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(make([]byte, (10<<20)+1))
	}))
	t.Cleanup(server.Close)
	// oversizedErr 保存超大图片错误。
	_, _, oversizedErr := downloadImageURL(context.Background(), server.URL)
	if oversizedErr == nil {
		t.Fatal("oversized image should fail")
	}
}
