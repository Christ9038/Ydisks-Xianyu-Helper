// Package protocol 提供 cookie 解析、mtop 签名、设备/消息 ID 生成和消息解密。
package protocol

import "strings"

// TransCookies 将 "k1=v1; k2=v2" 形式的 cookie 字符串解析为 map。
func TransCookies(cookiesStr string) map[string]string {
	cookies := make(map[string]string)
	if cookiesStr == "" {
		return cookies
	}
	for _, part := range strings.Split(cookiesStr, "; ") {
		if eq := strings.Index(part, "="); eq >= 0 {
			k := part[:eq]
			v := part[eq+1:]
			cookies[k] = v
		}
	}
	return cookies
}

// SignToken 从 cookie 字符串中提取 _m_h5_tk 的前半段，作为 mtop API 签名用的 token。
// _m_h5_tk 形如 "<token>_<timestamp>"，取 "_" 前的部分。
func SignToken(cookiesStr string) string {
	v := TransCookies(cookiesStr)["_m_h5_tk"]
	if v == "" {
		return ""
	}
	if i := strings.Index(v, "_"); i >= 0 {
		return v[:i]
	}
	return v
}
