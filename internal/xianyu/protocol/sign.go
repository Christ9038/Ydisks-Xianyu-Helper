package protocol

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"
)

// mtop 签名使用的 appKey（与 WS 注册用的 APP_CONFIG.app_key 不同）。
const SignAppKey = "34839810"

// GenerateSign 生成 mtop API 签名：MD5(token + "&" + t + "&" + appKey + "&" + data)。
// 对应 Python generate_sign(t, token, data)。
func GenerateSign(t, token, data string) string {
	msg := token + "&" + t + "&" + SignAppKey + "&" + data
	sum := md5.Sum([]byte(msg))
	return hex.EncodeToString(sum[:])
}

// GenerateMid 生成消息 ID，形如 "<0-999随机><毫秒时间戳> 0"。
// 对应 Python generate_mid()。非密码学用途，用 math/rand 即可。
func GenerateMid() string {
	randomPart := rand.Intn(1000)
	ts := time.Now().UnixMilli()
	return fmt.Sprintf("%d%d 0", randomPart, ts)
}

// GenerateUUID 生成 UUID，形如 "-<毫秒时间戳>1"。
// 对应 Python generate_uuid()。
func GenerateUUID() string {
	return fmt.Sprintf("-%d1", time.Now().UnixMilli())
}

const deviceIDChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// GenerateDeviceID 生成设备 ID：36 位 UUID 格式（位置 8/13/18/23 为 "-"，14 为 "4"，
// 19 取 (rand&0x3)|0x8），末尾追加 "-<userID>"。
// 对应 Python generate_device_id(user_id)。
func GenerateDeviceID(userID string) string {
	result := make([]byte, 36)
	for i := 0; i < 36; i++ {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			result[i] = '-'
		case i == 14:
			result[i] = '4'
		case i == 19:
			result[i] = deviceIDChars[(rand.Intn(16)&0x3)|0x8]
		default:
			result[i] = deviceIDChars[rand.Intn(16)]
		}
	}
	return string(result) + "-" + userID
}
