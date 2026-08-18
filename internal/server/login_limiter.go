package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginFailureWindow 用于本次流程后续判断的登录FailureWindow
const (
	loginFailureWindow        = 5 * time.Minute
	loginFailuresPerIP        = 30
	loginFailuresPerPrincipal = 10
)

// loginFailureBucket 用于本次流程后续判断的登录FailureBucket
type loginFailureBucket struct {
	count   int
	expires time.Time
}

// loginFailureLimiter 仅记录失败登录。IP 和账号两个维度同时限制，避免攻击者
// 通过轮换账号绕过 IP 限制，或通过轮换 IP 集中爆破单个账号。
// loginFailureLimiter 用于本次流程后续判断的登录FailureLimiter
type loginFailureLimiter struct {
	mu           sync.Mutex
	buckets      map[string]loginFailureBucket
	window       time.Duration
	perIP        int
	perPrincipal int
}

// newLoginFailureLimiter 封装new登录FailureLimiter业务协调。
func newLoginFailureLimiter() *loginFailureLimiter {
	return &loginFailureLimiter{
		buckets:      make(map[string]loginFailureBucket),
		window:       loginFailureWindow,
		perIP:        loginFailuresPerIP,
		perPrincipal: loginFailuresPerPrincipal,
	}
}

// loginClientIP 封装登录ClientIP业务协调。
func loginClientIP(r *http.Request) string {
	// host、err 用于本次流程后续判断的host、err
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// loginPrincipal 封装登录Principal业务协调。
func loginPrincipal(username, email string) string {
	if // value 用于本次流程后续判断的值
	value := strings.TrimSpace(username); value != "" {
		return strings.ToLower(value)
	}
	return strings.ToLower(strings.TrimSpace(email))
}

// allow 封装allow业务协调。
func (l *loginFailureLimiter) allow(ip, principal string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// retry 用于本次流程后续判断的重试
	var retry time.Duration
	// key 表示当前遍历过程中的key
	for _, key := range []string{"ip:" + ip, "principal:" + principal} {
		// bucket、ok 用于本次流程后续判断的bucket、ok
		bucket, ok := l.buckets[key]
		if !ok || !now.Before(bucket.expires) {
			continue
		}
		// limit 用于本次流程后续判断的上限
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

// failure 封装failure业务协调。
func (l *loginFailureLimiter) failure(ip, principal string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// key 表示当前遍历过程中的key
	for _, key := range []string{"ip:" + ip, "principal:" + principal} {
		// bucket 用于本次流程后续判断的bucket
		bucket := l.buckets[key]
		if !now.Before(bucket.expires) {
			bucket = loginFailureBucket{expires: now.Add(l.window)}
		}
		bucket.count++
		l.buckets[key] = bucket
	}
	if len(l.buckets) > 2048 {
		// key、bucket 表示当前遍历过程中的key、bucket
		for key, bucket := range l.buckets {
			if !now.Before(bucket.expires) {
				delete(l.buckets, key)
			}
		}
	}
}

// success 封装success业务协调。
func (l *loginFailureLimiter) success(ip, principal string) {
	l.mu.Lock()
	delete(l.buckets, "ip:"+ip)
	delete(l.buckets, "principal:"+principal)
	l.mu.Unlock()
}
