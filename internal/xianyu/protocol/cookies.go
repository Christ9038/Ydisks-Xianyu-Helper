// Package protocol 提供 cookie 解析、mtop 签名、设备/消息 ID 生成和消息解密。
package protocol

import "strings"

// TransCookies 将 "k1=v1; k2=v2" 形式的 cookie 字符串解析为 map。
func TransCookies(cookiesStr string) map[string]string {
	// cookies 保存cookies，供当前处理流程使用
	cookies := make(map[string]string)
	if cookiesStr == "" {
		return cookies
	}
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(cookiesStr, ";") {
		part = strings.TrimSpace(part)
		// key、value、ok 保存key、value、ok，供当前处理流程使用
		key, value, ok := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		cookies[key] = strings.TrimSpace(value)
	}
	return cookies
}

// SignToken 从 cookie 字符串中提取 _m_h5_tk 的前半段，作为 mtop API 签名用的 token。
// _m_h5_tk 形如 "<token>_<timestamp>"，取 "_" 前的部分。
// SignToken 负责Sign令牌相关处理。
func SignToken(cookiesStr string) string {
	// 浏览器 Cookie header / document.cookie 会把更长 Path 的同名 Cookie
	// 排在前面，官网 cookie getter 读取首个匹配项。不能先压成 map 再取值，
	// 否则同名 token 会被最后一个（通常是更宽作用域）错误覆盖。
	// v 保存v，供当前处理流程使用
	v := ""
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(cookiesStr, ";") {
		// key、value、ok 保存key、value、ok，供当前处理流程使用
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(key) == "_m_h5_tk" {
			v = strings.TrimSpace(value)
			break
		}
	}
	if v == "" {
		return ""
	}
	if // i 保存i，供当前处理流程使用
	i := strings.Index(v, "_"); i >= 0 {
		return v[:i]
	}
	return v
}
