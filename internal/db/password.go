package db

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// ErrPasswordMismatch 密码不匹配。
var ErrPasswordMismatch = errors.New("密码错误")

// HashPassword 用 bcrypt 哈希明文密码（新用户/改密码用）。
func HashPassword(plain string) (string, error) {
	// b 是 bcrypt 生成的密码摘要字节；err 表示哈希生成失败，失败时不返回可持久化摘要。
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// legacySHA256 兼容旧数据库中的 SHA-256 密码摘要。
func legacySHA256(plain string) string {
	// sum 是兼容旧数据库校验所需的无盐 SHA-256 摘要，调用方不得把明文密码写入日志。
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// isLegacyHash 判断是否为老的无盐 SHA-256 哈希（64 位十六进制）。
// bcrypt 哈希以 $2 开头，据此区分。
// isLegacyHash 判断摘要是否符合旧数据库使用的 64 位十六进制 SHA-256 格式。
func isLegacyHash(h string) bool {
	return len(h) == 64 && !strings.HasPrefix(h, "$2")
}

// VerifyPassword 校验明文与存储哈希。
//   - bcrypt 哈希：bcrypt.CompareHashAndPassword
//   - 老 SHA-256 哈希：逐字节对比（兼容老库）
//
// 返回 (matched, needsUpgrade)。needsUpgrade=true 表示命中老哈希、应升级到 bcrypt。
// VerifyPassword 比较明文密码与已存储摘要；命中旧 SHA-256 时返回 needsUpgrade，提示调用方在成功认证后改存 bcrypt。
func VerifyPassword(stored, plain string) (matched bool, needsUpgrade bool, err error) {
	if isLegacyHash(stored) {
		if stored == legacySHA256(plain) {
			return true, true, nil
		}
		return false, false, ErrPasswordMismatch
	}
	// err 表示 bcrypt 校验摘要与明文密码的比较结果；非密码不匹配错误需要继续向上层返回。
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, false, ErrPasswordMismatch
		}
		return false, false, err
	}
	return true, false, nil
}
