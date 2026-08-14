package mtop

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

// mtopDocumentURL 保存mtopDocumentURL，供当前处理流程使用
const (
	mtopDocumentURL = "https://www.goofish.com/im"
	goofishTopSite  = "https://goofish.com"
)

// cookieSessionContextKey 保存登录凭证会话上下文Key，供当前处理流程使用
type cookieSessionContextKey struct{}

// CookieSession carries an authoritative Cookie Jar through one MTOP workflow.
// It lets direct Go HTTP calls reproduce the browser split between
// document cookies used for signing and cookies scoped to the request URL.
// The same session absorbs every Set-Cookie before response-body processing,
// so callers can persist rotations and deletions even when parsing later fails.
// CookieSession 保存登录凭证会话，供当前处理流程使用
type CookieSession struct {
	mu            sync.Mutex
	snapshot      []cookierefresh.BrowserCookie
	flat          string
	authoritative bool
	changed       bool
}

// WithCookieSnapshot installs an authoritative Cookie Jar on ctx. A nil input
// is normalized to an explicitly empty Jar; callers should invoke this only
// when metadata confirms that a complete snapshot exists.
// WithCookieSnapshot 负责With登录凭证Snapshot相关处理。
func WithCookieSnapshot(ctx context.Context, snapshot []cookierefresh.BrowserCookie) (context.Context, *CookieSession) {
	// normalized 保存normalized，供当前处理流程使用
	normalized := cookierefresh.NormalizeSnapshot(snapshot)
	if normalized == nil {
		normalized = []cookierefresh.BrowserCookie{}
	}
	// session 保存会话，供当前处理流程使用
	session := &CookieSession{snapshot: normalized, authoritative: true}
	return context.WithValue(ctx, cookieSessionContextKey{}, session), session
}

// WithFlatCookieSession carries a legacy flat Cookie header without claiming
// that it is a complete Cookie Jar. Response Set-Cookie values are still
// observable and persistable, but callers must keep metadata snapshot-free
// until a protocol flow supplies an authoritative Jar.
// WithFlatCookieSession 负责WithFlat登录凭证会话相关处理。
func WithFlatCookieSession(ctx context.Context, cookies string) (context.Context, *CookieSession) {
	// session 保存会话，供当前处理流程使用
	session := &CookieSession{flat: cookies}
	return context.WithValue(ctx, cookieSessionContextKey{}, session), session
}

// State returns the /im canonical Cookie header, a copy of the complete Jar,
// and whether the workflow observed an authoritative update.
// State 负责状态相关处理。
func (s *CookieSession) State() (string, []cookierefresh.BrowserCookie, bool) {
	if s == nil {
		return "", nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authoritative {
		return s.flat, nil, s.changed
	}
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := normalizedCompleteSnapshot(s.snapshot)
	// value 保存值，供当前处理流程使用
	value, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, mtopDocumentURL, goofishTopSite, time.Now())
	return value, snapshot, s.changed
}

// cookieSessionFromContext 负责登录凭证会话From上下文相关处理。
func cookieSessionFromContext(ctx context.Context) *CookieSession {
	if ctx == nil {
		return nil
	}
	// session 保存会话，供当前处理流程使用
	session, _ := ctx.Value(cookieSessionContextKey{}).(*CookieSession)
	return session
}

// CookieSessionFromContext exposes the operation-scoped session to legacy
// helpers that may replace it with a final authoritative Jar.
// CookieSessionFromContext 负责登录凭证会话From上下文相关处理。
func CookieSessionFromContext(ctx context.Context) *CookieSession {
	return cookieSessionFromContext(ctx)
}

// Snapshot returns a copy of the current complete Jar.
// Snapshot 负责Snapshot相关处理。
func (s *CookieSession) Snapshot() []cookierefresh.BrowserCookie {
	// snapshot、authoritative 保存snapshot、authoritative，供当前处理流程使用
	snapshot, authoritative, _ := s.requestState()
	if !authoritative {
		return nil
	}
	return snapshot
}

// ReplaceSnapshot records a final complete Jar, including an explicitly empty one.
// ReplaceSnapshot 负责ReplaceSnapshot相关处理。
func (s *CookieSession) ReplaceSnapshot(snapshot []cookierefresh.BrowserCookie) {
	if snapshot == nil {
		snapshot = []cookierefresh.BrowserCookie{}
	}
	s.replace(snapshot)
}

// requestState 负责请求状态相关处理。
func (s *CookieSession) requestState() ([]cookierefresh.BrowserCookie, bool, string) {
	if s == nil {
		return nil, false, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authoritative {
		return nil, false, s.flat
	}
	return normalizedCompleteSnapshot(s.snapshot), true, ""
}

// replace 负责replace相关处理。
func (s *CookieSession) replace(snapshot []cookierefresh.BrowserCookie) {
	if s == nil || snapshot == nil {
		return
	}
	// normalized 保存normalized，供当前处理流程使用
	normalized := cookierefresh.NormalizeSnapshot(snapshot)
	if normalized == nil {
		normalized = []cookierefresh.BrowserCookie{}
	}
	s.mu.Lock()
	s.snapshot = normalized
	s.flat = ""
	s.authoritative = true
	s.changed = true
	s.mu.Unlock()
}

// replaceFlat 负责replaceFlat相关处理。
func (s *CookieSession) replaceFlat(flat string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authoritative {
		return
	}
	if s.flat != flat {
		s.flat = flat
		s.changed = true
	}
}

// absorb 负责absorb相关处理。
func (s *CookieSession) absorb(requestURL string, setCookies []string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(setCookies) > 0 && s.authoritative {
		// updated 保存updated，供当前处理流程使用
		updated := normalizedCompleteSnapshot(cookierefresh.ApplySetCookies(s.snapshot, requestURL, setCookies, time.Now(), goofishTopSite))
		if !slices.Equal(s.snapshot, updated) {
			s.snapshot = updated
			s.changed = true
		}
		// value 保存值，供当前处理流程使用
		value, _ := cookierefresh.ScopedCookieHeaderForRequest(s.snapshot, mtopDocumentURL, goofishTopSite, time.Now())
		return value
	}
	if len(setCookies) > 0 {
		// updated、changed 保存updated、changed，供当前处理流程使用
		updated, changed := applyFlatSetCookies(s.flat, setCookies, time.Now())
		if changed {
			s.flat = updated
			s.changed = true
		}
	}
	return s.flat
}

// mtopRequestCookies returns the document-visible cookies used by lib-mtop for
// token/sign generation and the URL-scoped Cookie header sent on the request.
// mtopRequestCookies 负责mtop请求Cookies相关处理。
func mtopRequestCookies(ctx context.Context, fallback, documentURL, requestURL string) (string, string) {
	// session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx)
	if session == nil {
		return fallback, fallback
	}
	// snapshot、authoritative、flat 保存snapshot、authoritative、flat，供当前处理流程使用
	snapshot, authoritative, flat := session.requestState()
	if !authoritative {
		return flat, flat
	}
	// documentCookies 保存documentCookies，供当前处理流程使用
	documentCookies := make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range snapshot {
		if !cookie.HTTPOnly {
			documentCookies = append(documentCookies, cookie)
		}
	}
	// signing 保存signing，供当前处理流程使用
	signing, _ := cookierefresh.ScopedCookieHeaderForRequest(documentCookies, documentURL, goofishTopSite, time.Now())
	// requestCookies 保存请求Cookies，供当前处理流程使用
	requestCookies, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, goofishTopSite, time.Now())
	return signing, requestCookies
}

// normalizedCompleteSnapshot 负责normalizedCompleteSnapshot相关处理。
func normalizedCompleteSnapshot(snapshot []cookierefresh.BrowserCookie) []cookierefresh.BrowserCookie {
	// normalized 保存normalized，供当前处理流程使用
	normalized := cookierefresh.NormalizeSnapshot(snapshot)
	if normalized == nil {
		return []cookierefresh.BrowserCookie{}
	}
	return normalized
}

// applyFlatSetCookies 负责applyFlatSetCookies相关处理。
func applyFlatSetCookies(original string, setCookies []string, now time.Time) (string, bool) {
	// values 保存values，供当前处理流程使用
	values := protocol.TransCookies(original)
	// changed 保存changed，供当前处理流程使用
	changed := false
	// raw 表示当前遍历过程中的原始
	for _, raw := range setCookies {
		// parsed、err 保存parsed、err，供当前处理流程使用
		parsed, err := http.ParseSetCookie(raw)
		if err != nil || strings.TrimSpace(parsed.Name) == "" {
			continue
		}
		changed = true
		if parsed.MaxAge < 0 || (parsed.MaxAge == 0 && !parsed.Expires.IsZero() && !parsed.Expires.After(now)) {
			delete(values, parsed.Name)
			continue
		}
		values[parsed.Name] = parsed.Value
	}
	if !changed {
		return original, false
	}
	return cookierefresh.MarshalCookieString(values), true
}

// absorbMTopResponseCookies applies response cookies before body reads. Without
// an authoritative session it preserves the historical flat-cookie fallback.
// absorbMTopResponseCookies 负责absorbMTop响应Cookies相关处理。
func absorbMTopResponseCookies(ctx context.Context, fallback string, resp *http.Response) string {
	if resp == nil {
		return fallback
	}
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		// setCookies 保存setCookies，供当前处理流程使用
		setCookies := resp.Header.Values("Set-Cookie")
		if len(setCookies) == 0 {
			return fallback
		}
		// requestURL 保存请求URL，供当前处理流程使用
		requestURL := ""
		if resp.Request != nil && resp.Request.URL != nil {
			requestURL = resp.Request.URL.String()
		}
		return session.absorb(requestURL, setCookies)
	}
	return mergeSetCookie(fallback, protocol.TransCookies(fallback), resp)
}
