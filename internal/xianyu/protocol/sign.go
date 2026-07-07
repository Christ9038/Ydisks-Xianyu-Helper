package protocol

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"
)

// SignAppKey 是 mtop 签名使用的 appKey（与 WS 注册用的 appKey 不同）。
const SignAppKey = "34839810"

// GenerateSign 生成 mtop API 签名：MD5(token + "&" + t + "&" + appKey + "&" + data)。
func GenerateSign(t, token, data string) string {
	msg := token + "&" + t + "&" + SignAppKey + "&" + data
	// #nosec G401 -- MTOP 协议明确要求 MD5，不能替换为其他摘要算法。
	sum := md5.Sum([]byte(msg))
	return hex.EncodeToString(sum[:])
}

// GenerateMid 生成非密码学用途的消息 ID，形如 "<0-999随机><毫秒时间戳> 0"。
func GenerateMid() string {
	randomPart := randomInt(1000)
	ts := time.Now().UnixMilli()
	return fmt.Sprintf("%d%d 0", randomPart, ts)
}

// GenerateUUID 生成 UUID，形如 "-<毫秒时间戳>1"。
func GenerateUUID() string {
	return fmt.Sprintf("-%d1", time.Now().UnixMilli())
}

const deviceIDChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// GenerateDeviceID 生成设备 ID：36 位 UUID 格式（位置 8/13/18/23 为 "-"，14 为 "4"，
// 19 取 (rand&0x3)|0x8），末尾追加 "-<userID>"。
func GenerateDeviceID(userID string) string {
	result := make([]byte, 36)
	for i := 0; i < 36; i++ {
		switch i {
		case 8, 13, 18, 23:
			result[i] = '-'
		case 14:
			result[i] = '4'
		case 19:
			result[i] = deviceIDChars[(randomInt(16)&0x3)|0x8]
		default:
			result[i] = deviceIDChars[randomInt(16)]
		}
	}
	return string(result) + "-" + userID
}

var randomFallback atomic.Uint64

// randomInt 返回 [0,max) 的密码学随机数；系统熵源异常时使用进程内单调计数兜底。
func randomInt(max int) int {
	if max <= 1 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err == nil {
		return int(n.Int64())
	}
	return int(randomFallback.Add(1) % uint64(max))
}
