package browser

// Token CAPTCHA diagnostics are deliberately opt-in.  They collect enough
// information to distinguish a page/DOM change from a server-side rejection,
// without changing the frozen slider implementation or writing credentials to
// the normal application log.

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/logsafe"
)

const (
	tokenCaptchaDiagnosticDirEnv       = "CAPTCHA_DIAGNOSTIC_DIR"
	tokenCaptchaDiagnosticSensitiveEnv = "CAPTCHA_DIAGNOSTIC_INCLUDE_SENSITIVE"
	tokenCaptchaDiagnosticMaxEvents    = 500
)

type tokenCaptchaDiagnostic struct {
	cookieID         string
	engine           string
	requestedURL     string
	includeSensitive bool
	dir              string
	startedAt        time.Time
	logger           *slog.Logger

	mu        sync.Mutex
	requests  []tokenCaptchaDiagnosticNetworkEvent
	responses []tokenCaptchaDiagnosticNetworkEvent
	console   []tokenCaptchaDiagnosticConsoleEvent
	pageError []string
	initial   tokenCaptchaDiagnosticSnapshot
	once      sync.Once
}

type tokenCaptchaDiagnosticSnapshot struct {
	CapturedAt string
	PageHTML   string
	Screenshot []byte
	FrameHTML  map[int]string
	Frames     []tokenCaptchaDiagnosticFrame
	Selectors  []tokenCaptchaDiagnosticSelector
}

type tokenCaptchaDiagnosticNetworkEvent struct {
	Kind         string `json:"kind"`
	At           string `json:"at"`
	URL          string `json:"url"`
	Method       string `json:"method,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	Navigation   bool   `json:"navigation,omitempty"`
	Status       int    `json:"status,omitempty"`
	ContentType  string `json:"content_type,omitempty"`
	Failure      string `json:"failure,omitempty"`
}

type tokenCaptchaDiagnosticConsoleEvent struct {
	At   string `json:"at"`
	Type string `json:"type"`
	Text string `json:"text"`
}

type tokenCaptchaDiagnosticFrame struct {
	Index      int    `json:"index"`
	Name       string `json:"name,omitempty"`
	URL        string `json:"url"`
	ParentURL  string `json:"parent_url,omitempty"`
	HTMLBytes  int    `json:"html_bytes,omitempty"`
	ContentErr string `json:"content_error,omitempty"`
}

type tokenCaptchaDiagnosticSelector struct {
	FrameIndex int    `json:"frame_index"`
	FrameURL   string `json:"frame_url"`
	Selector   string `json:"selector"`
	Found      bool   `json:"found"`
	Visible    bool   `json:"visible,omitempty"`
	Box        string `json:"box,omitempty"`
	Error      string `json:"error,omitempty"`
}

type tokenCaptchaDiagnosticManifest struct {
	Version           int                                  `json:"version"`
	CreatedAt         string                               `json:"created_at"`
	CookieIDHash      string                               `json:"cookie_id_hash"`
	Engine            string                               `json:"engine"`
	Phase             string                               `json:"phase"`
	Cause             string                               `json:"cause,omitempty"`
	RequestedURL      string                               `json:"requested_url"`
	CurrentURL        string                               `json:"current_url"`
	Title             string                               `json:"title,omitempty"`
	IncludeSensitive  bool                                 `json:"include_sensitive"`
	PageState         map[string]any                       `json:"page_state,omitempty"`
	Frames            []tokenCaptchaDiagnosticFrame        `json:"frames,omitempty"`
	Selectors         []tokenCaptchaDiagnosticSelector     `json:"selectors,omitempty"`
	InitialCapturedAt string                               `json:"initial_captured_at,omitempty"`
	InitialFrames     []tokenCaptchaDiagnosticFrame        `json:"initial_frames,omitempty"`
	InitialSelectors  []tokenCaptchaDiagnosticSelector     `json:"initial_selectors,omitempty"`
	Requests          []tokenCaptchaDiagnosticNetworkEvent `json:"requests,omitempty"`
	Responses         []tokenCaptchaDiagnosticNetworkEvent `json:"responses,omitempty"`
	Console           []tokenCaptchaDiagnosticConsoleEvent `json:"console,omitempty"`
	PageErrors        []string                             `json:"page_errors,omitempty"`
}

func newTokenCaptchaDiagnostic(cookieID, engine, requestedURL string, page playwright.Page, logger *slog.Logger) *tokenCaptchaDiagnostic {
	dir := strings.TrimSpace(os.Getenv(tokenCaptchaDiagnosticDirEnv))
	if dir == "" || page == nil {
		return nil
	}
	d := &tokenCaptchaDiagnostic{
		cookieID:         cookieID,
		engine:           engine,
		requestedURL:     requestedURL,
		includeSensitive: diagnosticBoolEnv(tokenCaptchaDiagnosticSensitiveEnv),
		dir:              dir,
		startedAt:        time.Now().UTC(),
		logger:           logger,
	}
	page.OnRequest(func(request playwright.Request) {
		d.recordRequest(request)
	})
	page.OnResponse(func(response playwright.Response) {
		d.recordResponse(response)
	})
	page.OnRequestFailed(func(request playwright.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.responses) >= tokenCaptchaDiagnosticMaxEvents {
			return
		}
		failure := ""
		if err := request.Failure(); err != nil {
			failure = err.Error()
		}
		d.responses = append(d.responses, tokenCaptchaDiagnosticNetworkEvent{
			Kind:         "request_failed",
			At:           time.Now().UTC().Format(time.RFC3339Nano),
			URL:          d.safeURL(request.URL()),
			Method:       request.Method(),
			ResourceType: request.ResourceType(),
			Navigation:   request.IsNavigationRequest(),
			Failure:      failure,
		})
	})
	page.OnConsole(func(message playwright.ConsoleMessage) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.console) >= tokenCaptchaDiagnosticMaxEvents {
			return
		}
		text := message.Text()
		if !d.includeSensitive {
			text = redactDiagnosticText(text)
		}
		d.console = append(d.console, tokenCaptchaDiagnosticConsoleEvent{
			At:   time.Now().UTC().Format(time.RFC3339Nano),
			Type: message.Type(),
			Text: text,
		})
	})
	page.OnPageError(func(pageErr error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if pageErr == nil || len(d.pageError) >= tokenCaptchaDiagnosticMaxEvents {
			return
		}
		text := pageErr.Error()
		if !d.includeSensitive {
			text = redactDiagnosticText(text)
		}
		d.pageError = append(d.pageError, text)
	})
	return d
}

func diagnosticBoolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (d *tokenCaptchaDiagnostic) recordRequest(request playwright.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.requests) >= tokenCaptchaDiagnosticMaxEvents {
		return
	}
	d.requests = append(d.requests, tokenCaptchaDiagnosticNetworkEvent{
		Kind:         "request",
		At:           time.Now().UTC().Format(time.RFC3339Nano),
		URL:          d.safeURL(request.URL()),
		Method:       request.Method(),
		ResourceType: request.ResourceType(),
		Navigation:   request.IsNavigationRequest(),
	})
}

func (d *tokenCaptchaDiagnostic) recordResponse(response playwright.Response) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.responses) >= tokenCaptchaDiagnosticMaxEvents {
		return
	}
	request := response.Request()
	contentType := response.Headers()["content-type"]
	d.responses = append(d.responses, tokenCaptchaDiagnosticNetworkEvent{
		Kind:         "response",
		At:           time.Now().UTC().Format(time.RFC3339Nano),
		URL:          d.safeURL(response.URL()),
		Method:       request.Method(),
		ResourceType: request.ResourceType(),
		Navigation:   request.IsNavigationRequest(),
		Status:       response.Status(),
		ContentType:  contentType,
	})
}

func (d *tokenCaptchaDiagnostic) safeURL(raw string) string {
	if d.includeSensitive {
		return raw
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return logsafe.URL(raw)
	}
	if u.RawQuery == "" && u.Fragment == "" {
		return logsafe.URL(raw)
	}
	values := u.Query()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	queryDigest := sha256.Sum256([]byte(u.RawQuery))
	u.RawQuery = "keys=" + strings.Join(keys, ",") + "&query_sha256=" + hex.EncodeToString(queryDigest[:])[:16]
	u.Fragment = ""
	return u.String()
}

func (d *tokenCaptchaDiagnostic) capture(page playwright.Page, phase string, cause error) {
	if d == nil || page == nil {
		return
	}
	d.once.Do(func() {
		path, err := d.writeBundle(page, phase, cause)
		if err != nil {
			// Diagnostics must never affect the CAPTCHA result path.
			fmt.Fprintf(os.Stderr, "token captcha diagnostic capture failed: %v\n", err)
		} else if d.logger != nil {
			d.logger.Info("token风控诊断包已生成", "cookieID", d.cookieID, "engine", d.engine, "path", path, "include_sensitive", d.includeSensitive)
		}
	})
}

func (d *tokenCaptchaDiagnostic) snapshotInitial(page playwright.Page) {
	if d == nil || page == nil {
		return
	}
	d.mu.Lock()
	if d.initial.CapturedAt != "" {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	snapshot := tokenCaptchaDiagnosticSnapshot{
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		FrameHTML:  make(map[int]string),
	}
	snapshot.PageHTML, _ = page.Content()
	snapshot.Screenshot, _ = page.Screenshot(playwright.PageScreenshotOptions{FullPage: playwright.Bool(true), Timeout: playwright.Float(5000)})
	snapshot.Frames, snapshot.Selectors = diagnosticFramesAndSelectors(page, d)
	for index, frame := range append([]playwright.Frame{page.MainFrame()}, page.Frames()...) {
		if htmlText, err := frame.Content(); err == nil {
			snapshot.FrameHTML[index] = htmlText
		}
	}
	d.mu.Lock()
	if d.initial.CapturedAt == "" {
		d.initial = snapshot
	}
	d.mu.Unlock()
}

func (d *tokenCaptchaDiagnostic) writeBundle(page playwright.Page, phase string, cause error) (string, error) {
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("token-captcha-%s-%s-%s.zip", logsafe.ID(d.cookieID), d.startedAt.Format("20060102T150405.000000000Z"), sanitize(d.engine))
	target := filepath.Join(d.dir, name)
	tmp, err := os.CreateTemp(d.dir, ".token-captcha-*.zip.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	zw := zip.NewWriter(tmp)
	closeZip := func(closeErr error) error {
		if closeErr != nil {
			_ = zw.Close()
			return closeErr
		}
		if err := zw.Close(); err != nil {
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		return os.Rename(tmpName, target)
	}

	d.mu.Lock()
	requests := append([]tokenCaptchaDiagnosticNetworkEvent(nil), d.requests...)
	responses := append([]tokenCaptchaDiagnosticNetworkEvent(nil), d.responses...)
	console := append([]tokenCaptchaDiagnosticConsoleEvent(nil), d.console...)
	pageErrors := append([]string(nil), d.pageError...)
	d.mu.Unlock()
	manifest := tokenCaptchaDiagnosticManifest{
		Version:          1,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		CookieIDHash:     logsafe.ID(d.cookieID),
		Engine:           d.engine,
		Phase:            phase,
		RequestedURL:     d.safeURL(d.requestedURL),
		CurrentURL:       d.safeURL(page.URL()),
		IncludeSensitive: d.includeSensitive,
		Requests:         requests,
		Responses:        responses,
		Console:          console,
		PageErrors:       pageErrors,
	}
	if cause != nil {
		manifest.Cause = cause.Error()
	}
	if title, titleErr := page.Title(); titleErr == nil {
		manifest.Title = title
	}
	manifest.PageState = diagnosticPageState(page, d.includeSensitive)
	manifest.Frames, manifest.Selectors = diagnosticFramesAndSelectors(page, d)
	d.mu.Lock()
	initial := d.initial
	initial.FrameHTML = cloneDiagnosticFrameHTML(initial.FrameHTML)
	initial.Screenshot = append([]byte(nil), initial.Screenshot...)
	d.mu.Unlock()
	manifest.InitialCapturedAt = initial.CapturedAt
	manifest.InitialFrames = initial.Frames
	manifest.InitialSelectors = initial.Selectors

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", closeZip(err)
	}
	if err := writeZipEntry(zw, "manifest.json", manifestJSON); err != nil {
		return "", closeZip(err)
	}
	readme := []byte("This archive was generated by the opt-in token CAPTCHA diagnostic mode.\n" +
		"It contains page/frame/selector/network metadata and a screenshot captured on failure.\n" +
		"Cookie values and verification query values are redacted unless CAPTCHA_DIAGNOSTIC_INCLUDE_SENSITIVE=1.\n")
	if err := writeZipEntry(zw, "README.txt", readme); err != nil {
		return "", closeZip(err)
	}
	if htmlText, htmlErr := page.Content(); htmlErr == nil {
		if !d.includeSensitive {
			htmlText = redactDiagnosticText(htmlText)
		}
		if err := writeZipEntry(zw, "page.html", []byte(htmlText)); err != nil {
			return "", closeZip(err)
		}
	}
	if initial.PageHTML != "" {
		initialHTML := initial.PageHTML
		if !d.includeSensitive {
			initialHTML = redactDiagnosticText(initialHTML)
		}
		if err := writeZipEntry(zw, "initial/page.html", []byte(initialHTML)); err != nil {
			return "", closeZip(err)
		}
	}
	for index, frameHTML := range initial.FrameHTML {
		if !d.includeSensitive {
			frameHTML = redactDiagnosticText(frameHTML)
		}
		if err := writeZipEntry(zw, fmt.Sprintf("initial/frames/frame-%02d.html", index), []byte(frameHTML)); err != nil {
			return "", closeZip(err)
		}
	}
	if len(initial.Screenshot) > 0 {
		if err := writeZipEntry(zw, "initial/page.png", initial.Screenshot); err != nil {
			return "", closeZip(err)
		}
	}
	for index, frame := range append([]playwright.Frame{page.MainFrame()}, page.Frames()...) {
		frameHTML, frameErr := frame.Content()
		if frameErr != nil {
			continue
		}
		if !d.includeSensitive {
			frameHTML = redactDiagnosticText(frameHTML)
		}
		if err := writeZipEntry(zw, fmt.Sprintf("frames/frame-%02d.html", index), []byte(frameHTML)); err != nil {
			return "", closeZip(err)
		}
	}
	if screenshot, screenshotErr := page.Screenshot(playwright.PageScreenshotOptions{FullPage: playwright.Bool(true), Timeout: playwright.Float(5000)}); screenshotErr == nil {
		if err := writeZipEntry(zw, "page.png", screenshot); err != nil {
			return "", closeZip(err)
		}
	}
	if err := closeZip(nil); err != nil {
		return "", err
	}
	return target, nil
}

func cloneDiagnosticFrameHTML(input map[int]string) map[int]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[int]string, len(input))
	for index, htmlText := range input {
		output[index] = htmlText
	}
	return output
}

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	writer, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func diagnosticPageState(page playwright.Page, includeSensitive bool) map[string]any {
	value, err := page.Evaluate(`async () => {
		let userAgentData = null;
		if (navigator.userAgentData) {
			userAgentData = {
				brands: navigator.userAgentData.brands,
				mobile: navigator.userAgentData.mobile,
				platform: navigator.userAgentData.platform
			};
			try {
				userAgentData.high_entropy = await navigator.userAgentData.getHighEntropyValues([
					'architecture', 'bitness', 'fullVersionList', 'model', 'platformVersion', 'wow64'
				]);
			} catch (error) {
				userAgentData.high_entropy_error = String(error);
			}
		}
		return {
			ready_state: document.readyState,
			location_path: window.location.pathname,
			location_query_present: Boolean(window.location.search),
			inner_width: window.innerWidth,
			inner_height: window.innerHeight,
			device_pixel_ratio: window.devicePixelRatio,
			user_agent: navigator.userAgent,
			user_agent_data: userAgentData,
			webdriver: navigator.webdriver,
			has_nc_container: Boolean(document.querySelector('.nc-container')),
			has_errloading: Boolean(document.querySelector('.errloading')),
			body_text: (document.body && document.body.innerText || '').slice(0, 1200)
		};
	}`)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if state, ok := value.(map[string]any); ok {
		if body, ok := state["body_text"].(string); ok && !includeSensitive {
			state["body_text"] = redactDiagnosticText(body)
		}
		return state
	}
	return map[string]any{"value": value}
}

func diagnosticFramesAndSelectors(page playwright.Page, d *tokenCaptchaDiagnostic) ([]tokenCaptchaDiagnosticFrame, []tokenCaptchaDiagnosticSelector) {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	frameMetadata := make([]tokenCaptchaDiagnosticFrame, 0, len(frames))
	selectors := append([]string{}, sliderButtonSelectors...)
	selectors = append(selectors, sliderTrackSelectors...)
	selectors = append(selectors, sliderRetrySelectors...)
	for index, frame := range frames {
		parentURL := ""
		if parent := frame.ParentFrame(); parent != nil {
			parentURL = d.safeURL(parent.URL())
		}
		metadata := tokenCaptchaDiagnosticFrame{Index: index, Name: frame.Name(), URL: d.safeURL(frame.URL()), ParentURL: parentURL}
		if htmlText, err := frame.Content(); err == nil {
			metadata.HTMLBytes = len(htmlText)
		} else {
			metadata.ContentErr = err.Error()
		}
		frameMetadata = append(frameMetadata, metadata)
	}
	selectorRecords := make([]tokenCaptchaDiagnosticSelector, 0, len(frames)*len(selectors))
	for index, frame := range frames {
		frameURL := d.safeURL(frame.URL())
		for _, selector := range selectors {
			entry := tokenCaptchaDiagnosticSelector{FrameIndex: index, FrameURL: frameURL, Selector: selector}
			el, err := frame.QuerySelector(selector)
			if err != nil {
				entry.Error = err.Error()
			} else if el != nil {
				entry.Found = true
				entry.Visible = elementVisible(el)
				if box, boxErr := el.BoundingBox(); boxErr == nil {
					entry.Box = formatBoundingBox(box)
				}
			}
			selectorRecords = append(selectorRecords, entry)
		}
	}
	return frameMetadata, selectorRecords
}

func redactDiagnosticText(text string) string {
	for _, key := range []string{"x5secdata", "x5sec", "_m_h5_tk", "_m_h5_tk_enc", "token", "sign"} {
		text = redactDiagnosticKey(text, key)
	}
	return text
}

func redactDiagnosticKey(text, key string) string {
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(key) + `=[^&'"<>\s\\` + "`" + `)]*`)
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		if index := strings.IndexByte(match, '='); index >= 0 {
			return match[:index+1] + "<redacted>"
		}
		return match
	})
}
