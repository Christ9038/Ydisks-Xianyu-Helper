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
	// tokenCaptchaDiagnosticDirEnv 指定诊断 zip 的本地目录；未设置时完全不创建诊断器。
	tokenCaptchaDiagnosticDirEnv = "CAPTCHA_DIAGNOSTIC_DIR"
	// tokenCaptchaDiagnosticSensitiveEnv 允许显式保留敏感正文，仅供受控排障环境，默认一律脱敏。
	tokenCaptchaDiagnosticSensitiveEnv = "CAPTCHA_DIAGNOSTIC_INCLUDE_SENSITIVE"
	// tokenCaptchaDiagnosticMaxEvents 限制每类事件最多记录 500 条，避免诊断失败放大内存或磁盘消耗。
	tokenCaptchaDiagnosticMaxEvents = 500
)

// tokenCaptchaDiagnostic 在显式启用时收集一次 token CAPTCHA 失败现场；mu 保护事件与 initial，once 保证只写出一个包，且不得影响验证码结果路径。
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

// tokenCaptchaDiagnosticSnapshot 保存首次现场的页面、frame、选择器和截图，用于与最终失败现场比较。
type tokenCaptchaDiagnosticSnapshot struct {
	CapturedAt string
	PageHTML   string
	Screenshot []byte
	FrameHTML  map[int]string
	Frames     []tokenCaptchaDiagnosticFrame
	Selectors  []tokenCaptchaDiagnosticSelector
}

// tokenCaptchaDiagnosticNetworkEvent 是脱敏后的单条网络请求、响应或失败记录。
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
	// BusinessResult 仅保存滑块接口响应中的脱敏业务结果，帮助区分轨迹被拒绝与网络失败；绝不保存滑块参数、Cookie 或响应正文。
	BusinessResult string `json:"business_result,omitempty"`
}

// tokenCaptchaDiagnosticConsoleEvent 保存页面控制台输出，默认先去除可能出现的凭证和验证参数。
type tokenCaptchaDiagnosticConsoleEvent struct {
	At   string `json:"at"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// tokenCaptchaDiagnosticFrame 描述当前页面树中一个 frame 的来源、父级和可读 HTML 状态。
type tokenCaptchaDiagnosticFrame struct {
	Index      int    `json:"index"`
	Name       string `json:"name,omitempty"`
	URL        string `json:"url"`
	ParentURL  string `json:"parent_url,omitempty"`
	HTMLBytes  int    `json:"html_bytes,omitempty"`
	ContentErr string `json:"content_error,omitempty"`
}

// tokenCaptchaDiagnosticSelector 记录冻结选择器在某个 frame 中的发现、可见和边界框状态，不改变选择器本身。
type tokenCaptchaDiagnosticSelector struct {
	FrameIndex int    `json:"frame_index"`
	FrameURL   string `json:"frame_url"`
	Selector   string `json:"selector"`
	Found      bool   `json:"found"`
	Visible    bool   `json:"visible,omitempty"`
	Box        string `json:"box,omitempty"`
	Error      string `json:"error,omitempty"`
}

// tokenCaptchaDiagnosticManifest 是诊断 zip 的脱敏索引，关联失败阶段、页面快照和有限事件流。
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

// newTokenCaptchaDiagnostic 在环境指定目录且 page 可用时安装只读诊断监听器；cookieID 仅存于内存且写包时哈希，返回 nil 表示诊断未启用。
func newTokenCaptchaDiagnostic(cookieID, engine, requestedURL string, page playwright.Page, logger *slog.Logger) *tokenCaptchaDiagnostic {
	// dir 是诊断包输出目录，空值保持功能完全关闭。
	dir := strings.TrimSpace(os.Getenv(tokenCaptchaDiagnosticDirEnv))
	if dir == "" || page == nil {
		return nil
	}
	// d 持有本页监听器共享的诊断状态，回调通过 mu 串行写入有限事件列表。
	d := &tokenCaptchaDiagnostic{
		cookieID:         cookieID,
		engine:           engine,
		requestedURL:     requestedURL,
		includeSensitive: diagnosticBoolEnv(tokenCaptchaDiagnosticSensitiveEnv),
		dir:              dir,
		startedAt:        time.Now().UTC(),
		logger:           logger,
	}
	// request 是 Playwright 提供的请求事件，回调仅记录已脱敏的元数据。
	page.OnRequest(func(request playwright.Request) {
		d.recordRequest(request)
	})
	// response 是 Playwright 提供的响应事件，回调不会读取或持久化响应正文。
	page.OnResponse(func(response playwright.Response) {
		d.recordResponse(response)
	})
	// request 是失败的网络请求，回调在 d.mu 保护下追加一条受限数量的失败事件。
	page.OnRequestFailed(func(request playwright.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.responses) >= tokenCaptchaDiagnosticMaxEvents {
			return
		}
		// failure 保存底层失败文本，默认不会包含请求 Cookie；err 表示 Playwright 无法提供失败详情。
		failure := ""
		// err 表示 Playwright 未能返回底层失败详情，失败事件仍保留其他无敏感元数据。
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
	// message 是页面控制台事件，内容在未授权敏感采集时先经过文本脱敏。
	page.OnConsole(func(message playwright.ConsoleMessage) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.console) >= tokenCaptchaDiagnosticMaxEvents {
			return
		}
		// text 是待写入诊断包的控制台文本，敏感模式关闭时会就地替换秘密参数。
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
		// text 是待脱敏的页面异常文本，不能直接写入普通应用日志。
		text := pageErr.Error()
		if !d.includeSensitive {
			text = redactDiagnosticText(text)
		}
		d.pageError = append(d.pageError, text)
	})
	return d
}

// diagnosticBoolEnv 按受限真值集合读取诊断开关，任何未知文本均按关闭处理。
func diagnosticBoolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// recordRequest 在 d.mu 保护下记录一条脱敏请求元数据；达到上限后静默丢弃后续事件。
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

// recordResponse 在 d.mu 保护下记录一条脱敏响应元数据；仅对 /slide 响应提取受限业务结果，不持久化响应正文。
func (d *tokenCaptchaDiagnostic) recordResponse(response playwright.Response) {
	// request 是与响应关联的请求，只用于记录方法、资源类型与是否为滑块提交。
	request := response.Request()
	// businessResult 是从滑块接口响应提炼出的有限状态，空值表示非滑块请求或响应不可安全解析。
	businessResult := ""
	if isTokenCaptchaSlideResponse(response.URL()) {
		// body、bodyErr 是 Playwright 已接收的响应副本及读取错误；只交给脱敏摘要器，不能写入诊断包或普通日志。
		body, bodyErr := response.Body()
		if bodyErr == nil {
			businessResult = tokenCaptchaSlideBusinessResult(body)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.responses) >= tokenCaptchaDiagnosticMaxEvents {
		return
	}
	// contentType 记录响应的公开媒体类型，不读取可能携带账号数据的响应正文。
	contentType := response.Headers()["content-type"]
	d.responses = append(d.responses, tokenCaptchaDiagnosticNetworkEvent{
		Kind:           "response",
		At:             time.Now().UTC().Format(time.RFC3339Nano),
		URL:            d.safeURL(response.URL()),
		Method:         request.Method(),
		ResourceType:   request.ResourceType(),
		Navigation:     request.IsNavigationRequest(),
		Status:         response.Status(),
		ContentType:    contentType,
		BusinessResult: businessResult,
	})
}

// isTokenCaptchaSlideResponse 判断 URL 是否为当前 token 风控的滑块提交接口；只匹配路径，避免读取其他账号接口的响应体。
func isTokenCaptchaSlideResponse(rawURL string) bool {
	// parsed 是结构化后的请求地址；err 表示地址无法解析，此时按非滑块接口处理以保持诊断最小化。
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/slide")
}

// tokenCaptchaSlideBusinessResult 从 /slide JSON 响应中提取顶层 ret、code 与 success 的脱敏摘要；body 只在函数栈内存在，返回值不含正文和验证参数。
func tokenCaptchaSlideBusinessResult(body []byte) string {
	// decoded 只声明平台响应中允许诊断的顶层状态字段，未知字段包括滑块数据与会话信息均不会被解码或输出。
	decoded := struct {
		// Ret 是 MTOP 风格的业务结果数组，通常说明风控验证接受或拒绝原因。
		Ret []string `json:"ret"`
		// Code 是部分响应使用的数值或字符串状态码；RawMessage 防止把嵌套业务数据展开。
		Code json.RawMessage `json:"code"`
		// Success 是部分响应使用的顶层布尔成功标志。
		Success *bool `json:"success"`
	}{}
	// decodeErr 表示响应不是预期 JSON，诊断保持空而不把原文误写入工件。
	if decodeErr := json.Unmarshal(body, &decoded); decodeErr != nil {
		return ""
	}
	// parts 以固定、少量字段构成诊断摘要，禁止从 data 等嵌套字段拷贝内容。
	parts := make([]string, 0, 3)
	if len(decoded.Ret) > 0 {
		// retValues 是逐项脱敏后的 MTOP 结果，最多记录前三项以限制诊断大小与信息范围。
		retValues := make([]string, 0, min(3, len(decoded.Ret)))
		// retIndex、retValue 分别表示当前结果位置和原始结果文本；只把脱敏后的文本写入摘要。
		for retIndex, retValue := range decoded.Ret {
			if retIndex >= 3 {
				break
			}
			retValues = append(retValues, redactDiagnosticText(retValue))
		}
		parts = append(parts, "ret="+strings.Join(retValues, ","))
	}
	if len(decoded.Code) > 0 && string(decoded.Code) != "null" {
		// code 是顶层原始 JSON 标量；先验证其为字符串或数字，避免嵌套对象进入诊断。
		code := ""
		if json.Unmarshal(decoded.Code, &code) == nil {
			parts = append(parts, "code="+redactDiagnosticText(code))
		} else {
			// numericCode 是仅接受数字的备选状态码，其他 JSON 形状保持不记录。
			var numericCode json.Number
			if json.Unmarshal(decoded.Code, &numericCode) == nil {
				parts = append(parts, "code="+numericCode.String())
			}
		}
	}
	if decoded.Success != nil {
		// success 是明确的布尔结果，格式化后不携带平台任意文本。
		parts = append(parts, fmt.Sprintf("success=%t", *decoded.Success))
	}
	return strings.Join(parts, "; ")
}

// safeURL 在默认模式删除 URL 查询值与 fragment，仅保留排序后的参数名和查询摘要；敏感模式由显式环境授权才原样返回。
func (d *tokenCaptchaDiagnostic) safeURL(raw string) string {
	if d.includeSensitive {
		return raw
	}
	// u 是可结构化脱敏的 URL；err 表示输入不符合完整 URL 格式，此时回退通用日志脱敏。
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return logsafe.URL(raw)
	}
	if u.RawQuery == "" && u.Fragment == "" {
		return logsafe.URL(raw)
	}
	// values 是解析出的查询参数；keys 只保留键名以表达请求形状而不暴露验证值。
	values := u.Query()
	// keys 只收集诊断中允许暴露的参数名，容量按查询字段数预留。
	keys := make([]string, 0, len(values))
	// key 是当前查询参数名，诊断包中允许保留但不保留对应值。
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// queryDigest 是原始查询串的短摘要，用于关联重复请求而不记录实际参数值。
	queryDigest := sha256.Sum256([]byte(u.RawQuery))
	u.RawQuery = "keys=" + strings.Join(keys, ",") + "&query_sha256=" + hex.EncodeToString(queryDigest[:])[:16]
	u.Fragment = ""
	return u.String()
}

// capture 在 d.once 保护下为一次失败写出诊断包；任何写包错误只输出脱敏错误并绝不改变 CAPTCHA 结果。
func (d *tokenCaptchaDiagnostic) capture(page playwright.Page, phase string, cause error) {
	if d == nil || page == nil {
		return
	}
	// once 回调是唯一允许写包的路径，避免多个失败回调竞争创建诊断文件。
	d.once.Do(func() {
		// path 是生成 zip 的本地路径；err 表示诊断写入失败，不能向调用路径传播。
		path, err := d.writeBundle(page, phase, cause)
		if err != nil {
			// Diagnostics must never affect the CAPTCHA result path.
			fmt.Fprintf(os.Stderr, "token captcha diagnostic capture failed: %v\n", err)
		} else if d.logger != nil {
			d.logger.Info("token风控诊断包已生成", "cookieID", d.cookieID, "engine", d.engine, "path", path, "include_sensitive", d.includeSensitive)
		}
	})
}

// snapshotInitial 最多捕获一次导航后的初始现场；先在无锁状态读取 Playwright，再持锁发布完整快照，避免持锁外部 I/O。
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

	// snapshot 是无锁构造的初始页面副本，完成后仅在 initial 仍为空时发布。
	snapshot := tokenCaptchaDiagnosticSnapshot{
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		FrameHTML:  make(map[int]string),
	}
	snapshot.PageHTML, _ = page.Content()
	snapshot.Screenshot, _ = page.Screenshot(playwright.PageScreenshotOptions{FullPage: playwright.Bool(true), Timeout: playwright.Float(5000)})
	snapshot.Frames, snapshot.Selectors = diagnosticFramesAndSelectors(page, d)
	// index 是诊断包中的稳定 frame 序号；frame 是当前待抓取 HTML 的页面 frame。
	for index, frame := range append([]playwright.Frame{page.MainFrame()}, page.Frames()...) {
		// htmlText 是 frame 的初始 HTML；err 表示 frame 内容暂不可读，此时不影响整体诊断。
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

// writeBundle 将当前和初始诊断现场写为原子 zip；返回最终本地路径，错误仅供 capture 记录而不得影响验证码流程。
func (d *tokenCaptchaDiagnostic) writeBundle(page playwright.Page, phase string, cause error) (string, error) {
	// err 表示私有诊断目录无法创建，目录权限固定为仅当前用户可访问。
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return "", err
	}
	// name 是不含明文账号标识的诊断文件名；target 是最终原子替换目标。
	name := fmt.Sprintf("token-captcha-%s-%s-%s.zip", logsafe.ID(d.cookieID), d.startedAt.Format("20060102T150405.000000000Z"), sanitize(d.engine))
	// target 是完成写入后通过同目录 Rename 发布的最终诊断 zip 路径。
	target := filepath.Join(d.dir, name)
	// tmp 是同目录的临时 zip；err 表示临时文件无法创建。
	tmp, err := os.CreateTemp(d.dir, ".token-captcha-*.zip.tmp")
	if err != nil {
		return "", err
	}
	// tmpName 用于 defer 清理和最终同目录 Rename，保证读者不会看到半写入文件。
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	// zw 是写入临时文件的 zip 流；closeZip 负责按成功或失败路径关闭并执行原子发布。
	zw := zip.NewWriter(tmp)
	// closeZip 在任一路径关闭流；成功时关闭临时文件并原子发布，失败时只返回原始操作错误。
	closeZip := func(closeErr error) error {
		if closeErr != nil {
			_ = zw.Close()
			return closeErr
		}
		// err 表示 zip 尾部无法完成写入，此时不发布损坏的诊断包。
		if err := zw.Close(); err != nil {
			return err
		}
		// err 表示临时文件无法落盘关闭，Rename 前必须返回失败。
		if err := tmp.Close(); err != nil {
			return err
		}
		return os.Rename(tmpName, target)
	}

	d.mu.Lock()
	// requests、responses、console、pageErrors 是锁内事件的独立副本，后续 zip I/O 不持有 d.mu。
	requests := append([]tokenCaptchaDiagnosticNetworkEvent(nil), d.requests...)
	// responses 是响应和请求失败事件的锁内副本，后续写 zip 时不访问共享切片。
	responses := append([]tokenCaptchaDiagnosticNetworkEvent(nil), d.responses...)
	// console 是已按敏感模式处理的控制台事件副本。
	console := append([]tokenCaptchaDiagnosticConsoleEvent(nil), d.console...)
	// pageErrors 是已按敏感模式处理的页面异常文本副本。
	pageErrors := append([]string(nil), d.pageError...)
	d.mu.Unlock()
	// manifest 汇总可安全序列化的失败上下文，URL 和正文依赖当前敏感模式决定是否脱敏。
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
	// title 是当前页面标题；titleErr 表示读取失败，此项可缺失且不阻塞诊断。
	if title, titleErr := page.Title(); titleErr == nil {
		manifest.Title = title
	}
	manifest.PageState = diagnosticPageState(page, d.includeSensitive)
	manifest.Frames, manifest.Selectors = diagnosticFramesAndSelectors(page, d)
	d.mu.Lock()
	// initial 是初始快照副本，FrameHTML 与 Screenshot 必须深拷贝后才能在无锁状态写包。
	initial := d.initial
	initial.FrameHTML = cloneDiagnosticFrameHTML(initial.FrameHTML)
	initial.Screenshot = append([]byte(nil), initial.Screenshot...)
	d.mu.Unlock()
	manifest.InitialCapturedAt = initial.CapturedAt
	manifest.InitialFrames = initial.Frames
	manifest.InitialSelectors = initial.Selectors

	// manifestJSON 是格式化后的索引内容；err 表示存在无法编码的诊断字段。
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", closeZip(err)
	}
	// err 表示索引条目未能写入 zip，closeZip 负责关闭资源并保留原始错误。
	if err := writeZipEntry(zw, "manifest.json", manifestJSON); err != nil {
		return "", closeZip(err)
	}
	// readme 说明诊断包来源和默认脱敏边界，不包含任何运行时凭证。
	readme := []byte("This archive was generated by the opt-in token CAPTCHA diagnostic mode.\n" +
		"It contains page/frame/selector/network metadata and a screenshot captured on failure.\n" +
		"Cookie values and verification query values are redacted unless CAPTCHA_DIAGNOSTIC_INCLUDE_SENSITIVE=1.\n")
	// err 表示说明文件写入失败，诊断包不能作为完整工件发布。
	if err := writeZipEntry(zw, "README.txt", readme); err != nil {
		return "", closeZip(err)
	}
	// htmlText 是最终页面 HTML；htmlErr 表示页面内容暂不可读取，可继续输出其余诊断信息。
	if htmlText, htmlErr := page.Content(); htmlErr == nil {
		if !d.includeSensitive {
			htmlText = redactDiagnosticText(htmlText)
		}
		// err 表示最终页面 HTML 条目写入失败。
		if err := writeZipEntry(zw, "page.html", []byte(htmlText)); err != nil {
			return "", closeZip(err)
		}
	}
	if initial.PageHTML != "" {
		// initialHTML 是待输出的初始页面 HTML，敏感模式关闭时在写入前脱敏。
		initialHTML := initial.PageHTML
		if !d.includeSensitive {
			initialHTML = redactDiagnosticText(initialHTML)
		}
		// err 表示初始页面 HTML 条目写入失败。
		if err := writeZipEntry(zw, "initial/page.html", []byte(initialHTML)); err != nil {
			return "", closeZip(err)
		}
	}
	// index 是初始 frame 序号；frameHTML 是对应 HTML 副本，写前按敏感模式脱敏。
	for index, frameHTML := range initial.FrameHTML {
		if !d.includeSensitive {
			frameHTML = redactDiagnosticText(frameHTML)
		}
		// err 表示初始 frame 条目写入失败。
		if err := writeZipEntry(zw, fmt.Sprintf("initial/frames/frame-%02d.html", index), []byte(frameHTML)); err != nil {
			return "", closeZip(err)
		}
	}
	if len(initial.Screenshot) > 0 {
		// err 表示初始截图条目写入失败。
		if err := writeZipEntry(zw, "initial/page.png", initial.Screenshot); err != nil {
			return "", closeZip(err)
		}
	}
	// index 是最终现场 frame 序号；frame 是待读取和输出 HTML 的页面 frame。
	for index, frame := range append([]playwright.Frame{page.MainFrame()}, page.Frames()...) {
		// frameHTML 是最终 HTML；frameErr 表示当前 frame 内容不可用，可跳过而继续打包。
		frameHTML, frameErr := frame.Content()
		if frameErr != nil {
			continue
		}
		if !d.includeSensitive {
			frameHTML = redactDiagnosticText(frameHTML)
		}
		// err 表示最终 frame 条目写入失败。
		if err := writeZipEntry(zw, fmt.Sprintf("frames/frame-%02d.html", index), []byte(frameHTML)); err != nil {
			return "", closeZip(err)
		}
	}
	// screenshot 是最终页面截图；screenshotErr 表示截图暂不可用，此时仍允许发布文本诊断。
	if screenshot, screenshotErr := page.Screenshot(playwright.PageScreenshotOptions{FullPage: playwright.Bool(true), Timeout: playwright.Float(5000)}); screenshotErr == nil {
		// err 表示截图条目写入失败。
		if err := writeZipEntry(zw, "page.png", screenshot); err != nil {
			return "", closeZip(err)
		}
	}
	// err 表示 zip 收尾或原子发布失败，调用方只记录该诊断错误。
	if err := closeZip(nil); err != nil {
		return "", err
	}
	return target, nil
}

// cloneDiagnosticFrameHTML 深拷贝 frame HTML 索引，避免 writeBundle 无锁处理时与 snapshot 发布共享 map。
func cloneDiagnosticFrameHTML(input map[int]string) map[int]string {
	if len(input) == 0 {
		return nil
	}
	// output 是供调用方独占修改的 frame HTML map。
	output := make(map[int]string, len(input))
	// index 是 frame 序号；htmlText 是其已捕获的 HTML 文本。
	for index, htmlText := range input {
		output[index] = htmlText
	}
	return output
}

// writeZipEntry 在 zw 中创建 name 并写入 data，返回创建或写入错误但不关闭调用方拥有的 zip 流。
func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	// writer 是当前 zip 条目的写入器；err 表示条目名冲突或底层临时文件写入失败。
	writer, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

// diagnosticPageState 读取页面公开运行状态；includeSensitive 为 false 时正文文本在返回前脱敏，脚本失败也返回可序列化错误状态。
func diagnosticPageState(page playwright.Page, includeSensitive bool) map[string]any {
	// value 是页面脚本状态对象；err 表示脚本评估失败并转换为诊断字段而非向上返回。
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
	// state 是可写入 manifest 的页面状态对象；ok 表示脚本结果保持了预期结构。
	if state, ok := value.(map[string]any); ok {
		// body 是页面正文摘要；ok 表示该字段可按字符串脱敏处理。
		if body, ok := state["body_text"].(string); ok && !includeSensitive {
			state["body_text"] = redactDiagnosticText(body)
		}
		return state
	}
	return map[string]any{"value": value}
}

// diagnosticFramesAndSelectors 枚举页面及嵌套 frame 中冻结选择器的观测结果；仅读取 DOM，不改变滑块实现、选择器优先级或页面状态。
func diagnosticFramesAndSelectors(page playwright.Page, d *tokenCaptchaDiagnostic) ([]tokenCaptchaDiagnosticFrame, []tokenCaptchaDiagnosticSelector) {
	// frames 是主 frame 加全部已知 frame 的诊断遍历序列。
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	// frameMetadata 保存每个 frame 的来源和 HTML 可读性；selectors 是冻结选择器的本地副本，仅供观测。
	frameMetadata := make([]tokenCaptchaDiagnosticFrame, 0, len(frames))
	// selectors 是三个冻结选择器列表的本地拼接副本，仅用于诊断查询。
	selectors := append([]string{}, sliderButtonSelectors...)
	selectors = append(selectors, sliderTrackSelectors...)
	selectors = append(selectors, sliderRetrySelectors...)
	// index 是 frame 在诊断包中的稳定序号；frame 是当前待记录的页面 frame。
	for index, frame := range frames {
		// parentURL 是脱敏后的父 frame 地址，主 frame 保持空字符串。
		parentURL := ""
		// parent 是当前 frame 的父级，存在时用于记录嵌套来源。
		if parent := frame.ParentFrame(); parent != nil {
			parentURL = d.safeURL(parent.URL())
		}
		// metadata 是当前 frame 的可序列化观测记录。
		metadata := tokenCaptchaDiagnosticFrame{Index: index, Name: frame.Name(), URL: d.safeURL(frame.URL()), ParentURL: parentURL}
		// htmlText 是用来计算大小的 frame HTML；err 表示内容不可读并记录到 metadata。
		if htmlText, err := frame.Content(); err == nil {
			metadata.HTMLBytes = len(htmlText)
		} else {
			metadata.ContentErr = err.Error()
		}
		frameMetadata = append(frameMetadata, metadata)
	}
	// selectorRecords 保存每个 frame/选择器组合的只读探测结果。
	selectorRecords := make([]tokenCaptchaDiagnosticSelector, 0, len(frames)*len(selectors))
	// index 是当前被探测 frame 的序号；frame 是执行只读选择器查询的 frame。
	for index, frame := range frames {
		// frameURL 是写入诊断包的脱敏 frame 地址。
		frameURL := d.safeURL(frame.URL())
		// selector 是当前冻结选择器文本，只读取其可用状态，绝不修改定义或顺序。
		for _, selector := range selectors {
			// entry 是当前 frame/选择器组合的诊断结果。
			entry := tokenCaptchaDiagnosticSelector{FrameIndex: index, FrameURL: frameURL, Selector: selector}
			// el 是匹配到的页面元素；err 表示查询失败并记录在 entry 中。
			el, err := frame.QuerySelector(selector)
			if err != nil {
				entry.Error = err.Error()
			} else if el != nil {
				entry.Found = true
				entry.Visible = elementVisible(el)
				// box 是元素边界框；boxErr 表示当前无法读取位置，仍保留已发现状态。
				if box, boxErr := el.BoundingBox(); boxErr == nil {
					entry.Box = formatBoundingBox(box)
				}
			}
			selectorRecords = append(selectorRecords, entry)
		}
	}
	return frameMetadata, selectorRecords
}

// redactDiagnosticText 替换已知 Cookie、验证码和签名参数值，供默认诊断包的 URL、HTML、控制台和错误文本共同使用。
func redactDiagnosticText(text string) string {
	// key 是当前要隐藏值的敏感参数名，保留键名以便定位问题类别。
	for _, key := range []string{"x5secdata", "x5sec", "_m_h5_tk", "_m_h5_tk_enc", "token", "sign"} {
		text = redactDiagnosticKey(text, key)
	}
	return text
}

// redactDiagnosticKey 用大小写无关模式隐藏 text 中指定 key 的值，不记录 Cookie、Token 或签名明文。
func redactDiagnosticKey(text, key string) string {
	// pattern 匹配参数值直到常见分隔符，键名由 QuoteMeta 处理以避免改变正则语义。
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(key) + `=[^&'"<>\s\\` + "`" + `)]*`)
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		// index 是键和值之间等号的位置，存在时仅保留键和等号。
		if index := strings.IndexByte(match, '='); index >= 0 {
			return match[:index+1] + "<redacted>"
		}
		return match
	})
}
