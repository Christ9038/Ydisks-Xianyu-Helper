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
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// legacySHA256 复刻 Python 端 hashlib.sha256(password).hexdigest()，用于兼容老库。
func legacySHA256(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// isLegacyHash 判断是否为老的无盐 SHA-256 哈希（64 位十六进制）。
// bcrypt 哈希以 $2 开头，据此区分。
func isLegacyHash(h string) bool {
	return len(h) == 64 && !strings.HasPrefix(h, "$2")
}

// VerifyPassword 校验明文与存储哈希。
//   - bcrypt 哈希：bcrypt.CompareHashAndPassword
//   - 老 SHA-256 哈希：逐字节对比（兼容老库）
// 返回 (matched, needsUpgrade)。needsUpgrade=true 表示命中老哈希、应升级到 bcrypt。
func VerifyPassword(stored, plain string) (matched bool, needsUpgrade bool, err error) {
	if isLegacyHash(stored) {
		if stored == legacySHA256(plain) {
			return true, true, nil
		}
		return false, false, ErrPasswordMismatch
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, false, ErrPasswordMismatch
		}
		return false, false, err
	}
	return true, false, nil
}
