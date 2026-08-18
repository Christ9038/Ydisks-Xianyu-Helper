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

// mtopDocumentURL 用于本次流程后续判断的mtopDocumentURL
const (
	mtopDocumentURL = "https://www.goofish.com/im"
	goofishTopSite  = "https://goofish.com"
)

// cookieSessionContextKey 用于本次流程后续判断的登录凭证会话上下文Key
type cookieSessionContextKey struct{}

// CookieSession carries an authoritative Cookie Jar through one MTOP workflow.
// It lets direct Go HTTP calls reproduce the browser split between
// document cookies used for signing and cookies scoped to the request URL.
// The same session absorbs every Set-Cookie before response-body processing,
// so callers can persist rotations and deletions even when parsing later fails.
// CookieSession 用于本次流程后续判断的登录凭证会话
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
// WithCookieSnapshot 封装With登录凭证Snapshot业务协调。
func WithCookieSnapshot(ctx context.Context, snapshot []cookierefresh.BrowserCookie) (context.Context, *CookieSession) {
	// normalized 用于本次流程后续判断的normalized
	normalized := cookierefresh.NormalizeSnapshot(snapshot)
	if normalized == nil {
		normalized = []cookierefresh.BrowserCookie{}
	}
	// session 用于本次流程后续判断的会话
	session := &CookieSession{snapshot: normalized, authoritative: true}
	return context.WithValue(ctx, cookieSessionContextKey{}, session), session
}

// WithFlatCookieSession carries a legacy flat Cookie header without claiming
// that it is a complete Cookie Jar. Response Set-Cookie values are still
// observable and persistable, but callers must keep metadata snapshot-free
// until a protocol flow supplies an authoritative Jar.
// WithFlatCookieSession 封装WithFlat登录凭证会话业务协调。
func WithFlatCookieSession(ctx context.Context, cookies string) (context.Context, *CookieSession) {
	// session 用于本次流程后续判断的会话
	session := &CookieSession{flat: cookies}
	return context.WithValue(ctx, cookieSessionContextKey{}, session), session
}

// State returns the /im canonical Cookie header, a copy of the complete Jar,
// and whether the workflow observed an authoritative update.
// State 封装状态业务协调。
func (s *CookieSession) State() (string, []cookierefresh.BrowserCookie, bool) {
	if s == nil {
		return "", nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authoritative {
		return s.flat, nil, s.changed
	}
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := normalizedCompleteSnapshot(s.snapshot)
	// value 用于本次流程后续判断的值
	value, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, mtopDocumentURL, goofishTopSite, time.Now())
	return value, snapshot, s.changed
}

// cookieSessionFromContext 封装登录凭证会话From上下文业务协调。
func cookieSessionFromContext(ctx context.Context) *CookieSession {
	if ctx == nil {
		return nil
	}
	// session 用于本次流程后续判断的会话
	session, _ := ctx.Value(cookieSessionContextKey{}).(*CookieSession)
	return session
}

// CookieSessionFromContext exposes the operation-scoped session to legacy
// helpers that may replace it with a final authoritative Jar.
// CookieSessionFromContext 封装登录凭证会话From上下文业务协调。
func CookieSessionFromContext(ctx context.Context) *CookieSession {
	return cookieSessionFromContext(ctx)
}

// Snapshot returns a copy of the current complete Jar.
// Snapshot 封装Snapshot业务协调。
func (s *CookieSession) Snapshot() []cookierefresh.BrowserCookie {
	// snapshot、authoritative 用于本次流程后续判断的snapshot、authoritative
	snapshot, authoritative, _ := s.requestState()
	if !authoritative {
		return nil
	}
	return snapshot
}

// ReplaceSnapshot records a final complete Jar, including an explicitly empty one.
// ReplaceSnapshot 封装ReplaceSnapshot业务协调。
func (s *CookieSession) ReplaceSnapshot(snapshot []cookierefresh.BrowserCookie) {
	if snapshot == nil {
		snapshot = []cookierefresh.BrowserCookie{}
	}
	s.replace(snapshot)
}

// requestState 封装请求状态业务协调。
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

// replace 封装replace业务协调。
func (s *CookieSession) replace(snapshot []cookierefresh.BrowserCookie) {
	if s == nil || snapshot == nil {
		return
	}
	// normalized 用于本次流程后续判断的normalized
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

// replaceFlat 封装replaceFlat业务协调。
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

// absorb 封装absorb业务协调。
func (s *CookieSession) absorb(requestURL string, setCookies []string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(setCookies) > 0 && s.authoritative {
		// updated 用于本次流程后续判断的updated
		updated := normalizedCompleteSnapshot(cookierefresh.ApplySetCookies(s.snapshot, requestURL, setCookies, time.Now(), goofishTopSite))
		if !slices.Equal(s.snapshot, updated) {
			s.snapshot = updated
			s.changed = true
		}
		// value 用于本次流程后续判断的值
		value, _ := cookierefresh.ScopedCookieHeaderForRequest(s.snapshot, mtopDocumentURL, goofishTopSite, time.Now())
		return value
	}
	if len(setCookies) > 0 {
		// updated、changed 用于本次流程后续判断的updated、changed
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
// mtopRequestCookies 封装mtop请求Cookies业务协调。
func mtopRequestCookies(ctx context.Context, fallback, documentURL, requestURL string) (string, string) {
	// session 用于本次流程后续判断的会话
	session := cookieSessionFromContext(ctx)
	if session == nil {
		return fallback, fallback
	}
	// snapshot、authoritative、flat 用于本次流程后续判断的snapshot、authoritative、flat
	snapshot, authoritative, flat := session.requestState()
	if !authoritative {
		return flat, flat
	}
	// documentCookies 用于本次流程后续判断的documentCookies
	documentCookies := make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range snapshot {
		if !cookie.HTTPOnly {
			documentCookies = append(documentCookies, cookie)
		}
	}
	// signing 用于本次流程后续判断的signing
	signing, _ := cookierefresh.ScopedCookieHeaderForRequest(documentCookies, documentURL, goofishTopSite, time.Now())
	// requestCookies 用于本次流程后续判断的请求Cookies
	requestCookies, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, goofishTopSite, time.Now())
	return signing, requestCookies
}

// normalizedCompleteSnapshot 封装normalizedCompleteSnapshot业务协调。
func normalizedCompleteSnapshot(snapshot []cookierefresh.BrowserCookie) []cookierefresh.BrowserCookie {
	// normalized 用于本次流程后续判断的normalized
	normalized := cookierefresh.NormalizeSnapshot(snapshot)
	if normalized == nil {
		return []cookierefresh.BrowserCookie{}
	}
	return normalized
}

// applyFlatSetCookies 封装applyFlatSetCookies业务协调。
func applyFlatSetCookies(original string, setCookies []string, now time.Time) (string, bool) {
	// values 用于本次流程后续判断的values
	values := protocol.TransCookies(original)
	// changed 用于本次流程后续判断的changed
	changed := false
	// raw 表示当前遍历过程中的原始
	for _, raw := range setCookies {
		// parsed、err 用于本次流程后续判断的parsed、err
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
// absorbMTopResponseCookies 封装absorbMTop响应Cookies业务协调。
func absorbMTopResponseCookies(ctx context.Context, fallback string, resp *http.Response) string {
	if resp == nil {
		return fallback
	}
	if // session 用于本次流程后续判断的会话
	session := cookieSessionFromContext(ctx); session != nil {
		// setCookies 用于本次流程后续判断的setCookies
		setCookies := resp.Header.Values("Set-Cookie")
		if len(setCookies) == 0 {
			return fallback
		}
		// requestURL 用于本次流程后续判断的请求URL
		requestURL := ""
		if resp.Request != nil && resp.Request.URL != nil {
			requestURL = resp.Request.URL.String()
		}
		return session.absorb(requestURL, setCookies)
	}
	return mergeSetCookie(fallback, protocol.TransCookies(fallback), resp)
}
