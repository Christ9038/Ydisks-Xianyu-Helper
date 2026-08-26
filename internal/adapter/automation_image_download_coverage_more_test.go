package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/netguard"
)

// TestDownloadAutomationImageCoversHTTPAndContentBranches 用本地端点覆盖自动化图片下载的确定性分支。
func TestDownloadAutomationImageCoversHTTPAndContentBranches(t *testing.T) {
	// previousPolicy 保存全局出站策略，避免本地测试改变其他测试的网络边界。
	previousPolicy := netguard.DefaultPublicOnly()
	netguard.SetDefaultPublicOnly(false)
	t.Cleanup(func() { netguard.SetDefaultPublicOnly(previousPolicy) })
	// server 提供图片成功、通用类型探测、HTTP 错误和非图片响应。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ok/photo.png":
			writer.Header().Set("Content-Type", "image/png; charset=utf-8")
			_, _ = writer.Write([]byte("png"))
		case "/octet/photo.bin":
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = writer.Write([]byte("\x89PNG\r\n\x1a\n"))
		case "/status":
			writer.WriteHeader(http.StatusBadGateway)
		case "/empty":
			writer.Header().Set("Content-Type", "image/png")
		case "/text":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("not image"))
		default:
			writer.Header().Set("Content-Type", "image/jpeg")
			_, _ = writer.Write([]byte("jpeg"))
		}
	}))
	t.Cleanup(server.Close)
	// data、contentType、filename 和 downloadErr 保存明确图片响应的下载结果。
	data, contentType, filename, downloadErr := downloadAutomationImage(context.Background(), server.URL+"/ok/photo.png")
	if downloadErr != nil || string(data) != "png" || contentType != "image/png" || filename != "photo.png" {
		t.Fatalf("download data=%q type=%q filename=%q err=%v", data, contentType, filename, downloadErr)
	}
	// detectedData、detectedType、detectedName 和 detectedErr 保存通用 Content-Type 的探测结果。
	detectedData, detectedType, detectedName, detectedErr := downloadAutomationImage(context.Background(), server.URL+"/octet/photo.bin")
	if detectedErr != nil || len(detectedData) == 0 || !strings.HasPrefix(detectedType, "image/") || detectedName != "photo.bin" {
		t.Fatalf("detected data=%d type=%q name=%q err=%v", len(detectedData), detectedType, detectedName, detectedErr)
	}
	// statusErr 保存远端非成功状态错误。
	_, _, _, statusErr := downloadAutomationImage(context.Background(), server.URL+"/status")
	if statusErr == nil {
		t.Fatal("non-2xx response should fail")
	}
	// emptyErr 保存空响应体错误。
	_, _, _, emptyErr := downloadAutomationImage(context.Background(), server.URL+"/empty")
	if emptyErr == nil {
		t.Fatal("empty response should fail")
	}
	// contentErr 保存非图片响应错误。
	_, _, _, contentErr := downloadAutomationImage(context.Background(), server.URL+"/text")
	if contentErr == nil {
		t.Fatal("non-image response should fail")
	}
	// canceledContext、cancelContext 保存已经取消的请求上下文。
	canceledContext, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	// canceledErr 保存取消请求的下载错误。
	_, _, _, canceledErr := downloadAutomationImage(canceledContext, server.URL+"/ok/photo.png")
	if canceledErr == nil {
		t.Fatal("canceled request should fail")
	}
}

// TestDownloadAutomationImageRejectsOversizedResponse 验证自动化图片下载的 10 MiB 上限。
func TestDownloadAutomationImageRejectsOversizedResponse(t *testing.T) {
	// previousPolicy 保存全局出站策略，避免本地测试改变其他测试的网络边界。
	previousPolicy := netguard.DefaultPublicOnly()
	netguard.SetDefaultPublicOnly(false)
	t.Cleanup(func() { netguard.SetDefaultPublicOnly(previousPolicy) })
	// server 提供超过内存读取上限的图片响应。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(make([]byte, automationImageMaxBytes+1))
	}))
	t.Cleanup(server.Close)
	// oversizedErr 保存超大图片下载错误。
	_, _, _, oversizedErr := downloadAutomationImage(context.Background(), server.URL)
	if oversizedErr == nil {
		t.Fatal("oversized response should fail")
	}
}
