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

func TestTokenCaptchaDiagnosticRedactsSensitiveValues(t *testing.T) {
	d := &tokenCaptchaDiagnostic{includeSensitive: false}
	got := d.safeURL("https://example.test/punish?x5secdata=secret-value&action=captcha#fragment")
	if strings.Contains(got, "secret-value") || strings.Contains(got, "fragment") {
		t.Fatalf("safeURL leaked sensitive data: %q", got)
	}
	if !strings.Contains(got, "x5secdata") || !strings.Contains(got, "query_sha256") {
		t.Fatalf("safeURL lost query diagnostics: %q", got)
	}
	text := `x5secdata=secret-value&token=another-secret sign=third-secret`
	redacted := redactDiagnosticText(text)
	if strings.Contains(redacted, "secret-value") || strings.Contains(redacted, "another-secret") || strings.Contains(redacted, "third-secret") {
		t.Fatalf("redactDiagnosticText leaked sensitive data: %q", redacted)
	}
}

func TestTokenCaptchaDiagnosticBundleBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to exercise token CAPTCHA diagnostics")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>诊断验证码</title></head><body>
<div class="nc-container"><div id="nc_1_n1t" class="nc_scale" style="width:300px;height:34px"><span id="nc_1_n1z" class="nc_iconfont btn_slide" style="width:42px;height:34px"></span></div></div>
</body></html>`)
	}))
	defer server.Close()

	dir := t.TempDir()
	t.Setenv(tokenCaptchaDiagnosticDirEnv, dir)
	t.Setenv(tokenCaptchaDiagnosticSensitiveEnv, "")
	m := NewManager(nil)
	defer m.Close()
	if err := m.init(); err != nil {
		t.Fatal(err)
	}
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     chromiumLaunchArgs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	bctx, err := browser.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bctx.Close() }()
	page, err := bctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := newTokenCaptchaDiagnostic("diagnostic-account", "playwright", server.URL+"/punish?x5secdata=secret", page, m.logger)
	if _, err := page.Goto(server.URL+"/punish?x5secdata=secret", playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		t.Fatal(err)
	}
	diagnostic.snapshotInitial(page)
	diagnostic.capture(page, "test_failure", io.ErrUnexpectedEOF)

	archives, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("diagnostic archive count=%d err=%v", len(archives), err)
	}
	archive, err := zip.OpenReader(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := make(map[string][]byte)
	for _, file := range archive.File {
		reader, readErr := file.Open()
		if readErr != nil {
			t.Fatal(readErr)
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries[file.Name] = data
	}
	for _, name := range []string{"README.txt", "manifest.json", "page.html", "page.png", "initial/page.html", "initial/page.png", "frames/frame-00.html", "initial/frames/frame-00.html"} {
		if len(entries[name]) == 0 {
			t.Fatalf("diagnostic archive missing %s", name)
		}
	}
	var manifest tokenCaptchaDiagnosticManifest
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
