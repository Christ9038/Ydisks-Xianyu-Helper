package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginFailureWindow 保存登录FailureWindow，供当前处理流程使用
const (
	loginFailureWindow        = 5 * time.Minute
	loginFailuresPerIP        = 30
	loginFailuresPerPrincipal = 10
)

// loginFailureBucket 保存登录FailureBucket，供当前处理流程使用
type loginFailureBucket struct {
	count   int
	expires time.Time
}

// loginFailureLimiter 仅记录失败登录。IP 和账号两个维度同时限制，避免攻击者
// 通过轮换账号绕过 IP 限制，或通过轮换 IP 集中爆破单个账号。
// loginFailureLimiter 保存登录FailureLimiter，供当前处理流程使用
type loginFailureLimiter struct {
	mu           sync.Mutex
	buckets      map[string]loginFailureBucket
	window       time.Duration
	perIP        int
	perPrincipal int
}

// newLoginFailureLimiter 负责new登录FailureLimiter相关处理。
func newLoginFailureLimiter() *loginFailureLimiter {
	return &loginFailureLimiter{
		buckets:      make(map[string]loginFailureBucket),
		window:       loginFailureWindow,
		perIP:        loginFailuresPerIP,
		perPrincipal: loginFailuresPerPrincipal,
	}
}

// loginClientIP 负责登录ClientIP相关处理。
func loginClientIP(r *http.Request) string {
	// host、err 保存host、err，供当前处理流程使用
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// loginPrincipal 负责登录Principal相关处理。
func loginPrincipal(username, email string) string {
	if // value 保存值，供当前处理流程使用
	value := strings.TrimSpace(username); value != "" {
		return strings.ToLower(value)
	}
	return strings.ToLower(strings.TrimSpace(email))
}

// allow 负责allow相关处理。
func (l *loginFailureLimiter) allow(ip, principal string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// retry 保存重试，供当前处理流程使用
	var retry time.Duration
	// key 表示当前遍历过程中的key
	for _, key := range []string{"ip:" + ip, "principal:" + principal} {
		// bucket、ok 保存bucket、ok，供当前处理流程使用
		bucket, ok := l.buckets[key]
		if !ok || !now.Before(bucket.expires) {
			continue
		}
		// limit 保存上限，供当前处理流程使用
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

// failure 负责failure相关处理。
func (l *loginFailureLimiter) failure(ip, principal string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// key 表示当前遍历过程中的key
	for _, key := range []string{"ip:" + ip, "principal:" + principal} {
		// bucket 保存bucket，供当前处理流程使用
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

// success 负责success相关处理。
func (l *loginFailureLimiter) success(ip, principal string) {
	l.mu.Lock()
	delete(l.buckets, "ip:"+ip)
	delete(l.buckets, "principal:"+principal)
	l.mu.Unlock()
}
