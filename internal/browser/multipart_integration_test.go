package browser

import (
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// multipartBrowserProbe 保存 Chromium 实际发送的 multipart 请求头和原始请求体，防止 Node 与浏览器编码差异回归。
type multipartBrowserProbe struct {
	// ContentType 是 Chromium 发送的 multipart 媒体类型及 boundary 参数。
	ContentType string
	// Body 是服务端在任何表单解析前读取的完整原始请求体。
	Body []byte
}

// TestChromiumMultipartBrowserIntegration 使用真实 Chromium 验证原生 FormData 的 boundary、重复文件字段、文件名和字节内容。
func TestChromiumMultipartBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to run the Chromium multipart transport smoke test")
	}
	// probes 接收 Chromium 发给本地探针的 multipart 请求，缓冲一个结果避免 HTTP handler 被测试 goroutine 阻塞。
	probes := make(chan multipartBrowserProbe, 1)
	// server 同时提供同源空页面和 multipart 接收端，确保浏览器 smoke 不受跨域策略影响。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/multipart" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><title>multipart smoke</title>")
			return
		}
		// body、readErr 保存请求的原始 multipart 字节和读取失败原因。
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			http.Error(w, "read multipart body", http.StatusBadRequest)
			return
		}
		probes <- multipartBrowserProbe{ContentType: r.Header.Get("Content-Type"), Body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// manager 负责初始化与关闭测试使用的 Playwright runtime。
	manager := NewManager(nil)
	// initErr 保存 Playwright runtime 初始化失败原因。
	if initErr := manager.init(); initErr != nil {
		t.Fatalf("initialize Playwright runtime: %v", initErr)
	}
	defer manager.Close()
	// browser、launchErr 保存真实 Chromium 实例及其启动失败原因。
	browser, launchErr := manager.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(true),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if launchErr != nil {
		t.Fatalf("launch Chromium: %v", launchErr)
	}
	defer browser.Close()
	// context、contextErr 保存本次 browser 隔离上下文及其创建失败原因。
	context, contextErr := browser.NewContext()
	if contextErr != nil {
		t.Fatalf("create Chromium context: %v", contextErr)
	}
	defer context.Close()
	// page、pageErr 保存用于执行浏览器 fetch 的同源页面和创建失败原因。
	page, pageErr := context.NewPage()
	if pageErr != nil {
		t.Fatalf("create Chromium page: %v", pageErr)
	}
	// probeResponse、gotoErr 分别保存本地探针页导航响应和浏览器导航错误；响应状态由后续 fetch 独立验证。
	if probeResponse, gotoErr := page.Goto(server.URL); gotoErr != nil || probeResponse == nil {
		t.Fatalf("open multipart probe page: %v", gotoErr)
	}
	// evaluation、evaluationErr 保存浏览器内 FormData fetch 的返回状态和执行错误。
	evaluation, evaluationErr := page.Evaluate(`async () => {
		const form = new FormData();
		form.append('title', 'chromium multipart smoke');
		form.append('images', new Blob(['image-one'], { type: 'image/png' }), 'first image.png');
		form.append('images', new Blob(['image-two'], { type: 'image/png' }), 'second-image.png');
		const response = await fetch('/multipart', { method: 'POST', body: form, credentials: 'include' });
		return response.status;
	}`)
	if evaluationErr != nil {
		t.Fatalf("execute Chromium multipart fetch: %v", evaluationErr)
	}
	// status、statusOK 分别保存 Playwright 解码后的 HTTP 状态和其整数类型断言结果。
	if status, statusOK := evaluation.(int); !statusOK || status != http.StatusNoContent {
		t.Fatalf("Chromium multipart status=%#v", evaluation)
	}
	// probe 在 fetch 完成后必须已经写入；超时诊断避免测试永久卡住。
	select {
	// probe 保存 Chromium 已提交到本地服务的 multipart 请求头和原始字节。
	case probe := <-probes:
		// mediaType、parameters 保存解析后的媒体类型与浏览器声明的 boundary。
		mediaType, parameters, parseErr := mime.ParseMediaType(probe.ContentType)
		if parseErr != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
			t.Fatalf("Chromium multipart Content-Type=%q parseErr=%v", probe.ContentType, parseErr)
		}
		// rawText 保留请求字节的一对一映射，使文件名和 ASCII 示例文件内容可被稳定验证。
		rawText := string(probe.Body)
		if !strings.Contains(rawText, "--"+parameters["boundary"]) {
			t.Fatalf("Chromium body does not contain declared boundary %q", parameters["boundary"])
		}
		if strings.Count(rawText, `name="images"`) != 2 || !strings.Contains(rawText, `filename="first image.png"`) || !strings.Contains(rawText, `filename="second-image.png"`) {
			t.Fatalf("Chromium multipart file headers=%q", rawText)
		}
		if !strings.Contains(rawText, "image-one") || !strings.Contains(rawText, "image-two") {
			t.Fatalf("Chromium multipart file content missing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Chromium did not submit multipart probe within five seconds")
	}
}
