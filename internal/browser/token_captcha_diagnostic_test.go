package browser

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// TestTokenCaptchaDiagnosticRedactsSensitiveValues 验证默认诊断输出不会保留验证码查询值、fragment 或文本中的敏感凭证。
func TestTokenCaptchaDiagnosticRedactsSensitiveValues(t *testing.T) {
	// d 是禁用敏感内容采集的最小诊断器，不需要真实浏览器或输出目录。
	d := &tokenCaptchaDiagnostic{includeSensitive: false}
	// got 是脱敏后的 URL，允许保留参数名与查询摘要以支持问题关联。
	got := d.safeURL("https://example.test/punish?x5secdata=secret-value&action=captcha#fragment")
	if strings.Contains(got, "secret-value") || strings.Contains(got, "fragment") {
		t.Fatalf("safeURL leaked sensitive data: %q", got)
	}
	if !strings.Contains(got, "x5secdata") || !strings.Contains(got, "query_sha256") {
		t.Fatalf("safeURL lost query diagnostics: %q", got)
	}
	// text 是同时含 Cookie 验证和签名类参数的模拟诊断文本。
	text := `x5secdata=secret-value&token=another-secret sign=third-secret`
	// redacted 是可安全写入默认诊断包的替换后文本。
	redacted := redactDiagnosticText(text)
	if strings.Contains(redacted, "secret-value") || strings.Contains(redacted, "another-secret") || strings.Contains(redacted, "third-secret") {
		t.Fatalf("redactDiagnosticText leaked sensitive data: %q", redacted)
	}
}

// TestTokenCaptchaSlideBusinessResult 验证滑块响应诊断只保留受限顶层结果，并且不会将嵌套滑块数据或秘密值写入工件。
func TestTokenCaptchaSlideBusinessResult(t *testing.T) {
	// body 是包含业务结果、敏感嵌套数据和无关字段的模拟滑块响应，测试仅允许前者进入摘要。
	body := []byte(`{"ret":["FAIL_SYS_USER_VALIDATE::x5secdata=secret-value","SECOND"],"code":403,"success":false,"data":{"slidedata":"secret-slide"}}`)
	// got 是默认诊断模式下的业务摘要。
	got := tokenCaptchaSlideBusinessResult(body)
	if !strings.Contains(got, "ret=FAIL_SYS_USER_VALIDATE::x5secdata=<redacted>,SECOND") || !strings.Contains(got, "code=403") || !strings.Contains(got, "success=false") {
		t.Fatalf("unexpected slide business result: %q", got)
	}
	if strings.Contains(got, "secret-value") || strings.Contains(got, "secret-slide") {
		t.Fatalf("slide business result leaked sensitive data: %q", got)
	}
	// nonSlideURL 是普通 MTOP 地址，不得触发任何响应正文读取。
	nonSlideURL := "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.login.token/1.0/report"
	if isTokenCaptchaSlideResponse(nonSlideURL) {
		t.Fatalf("ordinary response incorrectly treated as slide: %q", nonSlideURL)
	}
	// slideURL 是真实接口形状的滑块地址，尾部斜杠也必须被正确识别。
	slideURL := "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.login.token/1.0/slide/"
	if !isTokenCaptchaSlideResponse(slideURL) {
		t.Fatalf("slide response was not recognized: %q", slideURL)
	}
	// malformedBody 不能被当作可诊断 JSON，必须返回空摘要而非回显原文。
	malformedBody := []byte("secret malformed response")
	// malformedResult 是无法解析的响应摘要，必须保持为空以避免泄漏原始文本。
	malformedResult := tokenCaptchaSlideBusinessResult(malformedBody)
	if malformedResult != "" {
		t.Fatalf("malformed response should not be recorded: %q", malformedResult)
	}
}

// TestTokenCaptchaDiagnosticBundleBrowserIntegration 在真实 Chromium 中验证诊断包包含必要现场且默认不泄漏验证码查询值；仅在显式浏览器集成模式运行。
func TestTokenCaptchaDiagnosticBundleBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to exercise token CAPTCHA diagnostics")
	}
	// server 提供含冻结滑块元素的本地页面，避免访问真实平台或使用真实账号。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>诊断验证码</title></head><body>
<div class="nc-container"><div id="nc_1_n1t" class="nc_scale" style="width:300px;height:34px"><span id="nc_1_n1z" class="nc_iconfont btn_slide" style="width:42px;height:34px"></span></div></div>
</body></html>`)
	}))
	defer server.Close()

	// dir 是 testing 管理的临时诊断输出目录，测试结束后自动清理。
	dir := t.TempDir()
	t.Setenv(tokenCaptchaDiagnosticDirEnv, dir)
	t.Setenv(tokenCaptchaDiagnosticSensitiveEnv, "")
	// m 管理本次集成测试的 Playwright runtime，defer Close 负责回收资源。
	m := NewManager(nil)
	defer m.Close()
	// err 表示 runtime 初始化失败，无法继续验证真实浏览器诊断。
	if err := m.init(); err != nil {
		t.Fatal(err)
	}
	// browser 是测试启动的无头 Chromium；err 表示启动失败。
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     chromiumLaunchArgs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	// bctx 是 browser 的隔离页面上下文；err 表示 context 创建失败。
	bctx, err := browser.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bctx.Close() }()
	// page 是承载诊断监听器的本地测试页面；err 表示页面创建失败。
	page, err := bctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	// diagnostic 是由显式临时目录启用的诊断器，账户标识在产物中仅以哈希形式出现。
	diagnostic := newTokenCaptchaDiagnostic("diagnostic-account", "playwright", server.URL+"/punish?x5secdata=secret", page, m.logger)
	// err 表示本地滑块页面导航失败，不能继续捕获诊断快照。
	if _, err := page.Goto(server.URL+"/punish?x5secdata=secret", playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		t.Fatal(err)
	}
	diagnostic.snapshotInitial(page)
	diagnostic.capture(page, "test_failure", io.ErrUnexpectedEOF)

	// archives 是输出目录中生成的诊断 zip；err 表示文件模式匹配失败。
	archives, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("diagnostic archive count=%d err=%v", len(archives), err)
	}
	// archive 是待检查的唯一诊断 zip；err 表示压缩包无法读取。
	archive, err := zip.OpenReader(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	// entries 按条目名保存压缩包内容，供后续完整性和脱敏断言使用。
	entries := make(map[string][]byte)
	// file 是当前压缩包条目，逐个读取后立即关闭其 reader。
	for _, file := range archive.File {
		// reader 是当前条目的读取器；readErr 表示条目打开失败。
		reader, readErr := file.Open()
		if readErr != nil {
			t.Fatal(readErr)
		}
		// data 是条目完整字节内容；readErr 表示读取过程失败。
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries[file.Name] = data
	}
	// name 是每个必须存在的诊断条目路径，缺失说明捕获或写包协议被破坏。
	for _, name := range []string{"README.txt", "manifest.json", "page.html", "page.png", "initial/page.html", "initial/page.png", "frames/frame-00.html", "initial/frames/frame-00.html"} {
		if len(entries[name]) == 0 {
			t.Fatalf("diagnostic archive missing %s", name)
		}
	}
	// manifest 是从包内索引反序列化的结构，供断言失败阶段与初始快照信息。
	var manifest tokenCaptchaDiagnosticManifest
	// err 表示 manifest JSON 不符合诊断包契约。
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Phase != "test_failure" || manifest.Engine != "playwright" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.InitialCapturedAt == "" || len(manifest.InitialFrames) == 0 || len(manifest.InitialSelectors) == 0 {
		t.Fatalf("initial snapshot metadata missing: %+v", manifest)
	}
	if strings.Contains(string(entries["page.html"]), "secret") {
		t.Fatalf("page HTML leaked query value")
	}
}
