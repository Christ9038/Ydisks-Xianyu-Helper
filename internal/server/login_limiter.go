package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureWindow        = 5 * time.Minute
	loginFailuresPerIP        = 30
	loginFailuresPerPrincipal = 10
)

type loginFailureBucket struct {
	count   int
	expires time.Time
}

// loginFailureLimiter 仅记录失败登录。IP 和账号两个维度同时限制，避免攻击者
// 通过轮换账号绕过 IP 限制，或通过轮换 IP 集中爆破单个账号。
type loginFailureLimiter struct {
	mu           sync.Mutex
	buckets      map[string]loginFailureBucket
	window       time.Duration
	perIP        int
	perPrincipal int
}

func newLoginFailureLimiter() *loginFailureLimiter {
	return &loginFailureLimiter{
		buckets:      make(map[string]loginFailureBucket),
		window:       loginFailureWindow,
		perIP:        loginFailuresPerIP,
		perPrincipal: loginFailuresPerPrincipal,
	}
}

func loginClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func loginPrincipal(username, email string) string {
	if value := strings.TrimSpace(username); value != "" {
		return strings.ToLower(value)
	}
	return strings.ToLower(strings.TrimSpace(email))
}

func (l *loginFailureLimiter) allow(ip, principal string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var retry time.Duration
	for _, key := range []string{"ip:" + ip, "principal:" + principal} {
		bucket, ok := l.buckets[key]
		if !ok || !now.Before(bucket.expires) {
			continue
		}
		limit := l.perPrincipal
		if strings.HasPrefix(key, "ip:") {
			limit = l.perIP
		}
		if bucket.count >= limit && bucket.expires.Sub(now) > retry {
			retry = bucket.expires.Sub(now)
		}
	}
	return retry == 0, retry
}

func (l *loginFailureLimiter) failure(ip, principal string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range []string{"ip:" + ip, "principal:" + principal} {
		bucket := l.buckets[key]
		if !now.Before(bucket.expires) {
			bucket = loginFailureBucket{expires: now.Add(l.window)}
		}
		bucket.count++
		l.buckets[key] = bucket
	}
	if len(l.buckets) > 2048 {
		for key, bucket := range l.buckets {
			if !now.Before(bucket.expires) {
				delete(l.buckets, key)
			}
		}
	}
}

func (l *loginFailureLimiter) success(ip, principal string) {
	l.mu.Lock()
	delete(l.buckets, "ip:"+ip)
	delete(l.buckets, "principal:"+principal)
	l.mu.Unlock()
}
