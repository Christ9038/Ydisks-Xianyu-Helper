package protocol

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"
)

// TestGenerateSign 表驱动覆盖 GenerateSign 的边界输入。
// GenerateSign 是纯函数（MD5(token+"&"+t+"&"+SignAppKey+"&"+data)），结果应稳定可复现。
func TestGenerateSign(t *testing.T) {
	cases := []struct {
		name  string
		t     string
		token string
		data  string
	}{
		{"empty token", "1700000000000", "", `{"appKey":"x"}`},
		{"empty data", "1700000000000", "abc_token", ""},
		{"empty all", "", "", ""},
		{"special chars", "1700000000000", "tok&en", `{"k":"v&=,;"}`},
		{"unicode", "1700000000000", "中文token", `{"msg":"你好世界"}`},
		{"long string", "1700000000000", strings.Repeat("a", 10_000), strings.Repeat("x", 10_000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GenerateSign(tc.t, tc.token, tc.data)
			if len(got) != 32 {
				t.Fatalf("GenerateSign length = %d, want 32 (md5 hex)", len(got))
			}
			// 与独立计算的期望值比对，锁定拼接顺序与算法。
			msg := tc.token + "&" + tc.t + "&" + SignAppKey + "&" + tc.data
			sum := md5.Sum([]byte(msg))
			want := hex.EncodeToString(sum[:])
			if got != want {
				t.Fatalf("GenerateSign = %q, want %q", got, want)
			}
			// 字符集应为小写 hex。
			for _, r := range got {
				if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
					t.Fatalf("GenerateSign contains non-hex char %q", r)
				}
			}
		})
	}
}

// TestGenerateSign_Deterministic 相同输入两次调用结果一致。
func TestGenerateSign_Deterministic(t *testing.T) {
	a := GenerateSign("1", "2", "3")
	b := GenerateSign("1", "2", "3")
	if a != b {
		t.Fatalf("GenerateSign not deterministic: %s != %s", a, b)
	}
}

// TestGenerateMid_Format 校验 Mid 的格式与字符集。
func TestGenerateMid_Format(t *testing.T) {
	mid := GenerateMid()
	if !strings.HasSuffix(mid, " 0") {
		t.Fatalf("mid should end with \" 0\", got %q", mid)
	}
	body := strings.TrimSuffix(mid, " 0")
	// body = "<0-999 随机数><毫秒时间戳>"，全部应为十进制数字。
	for _, r := range body {
		if r < '0' || r > '9' {
			t.Fatalf("mid body should be all digits, got %q in %q", r, mid)
		}
	}
	// 随机前缀在 [0,1000)，长度不超过 3 + 13（毫秒时间戳）。
	if len(body) < 1 || len(body) > 16 {
		t.Fatalf("mid body length out of range: %q", mid)
	}
}

// TestGenerateMid_Uniqueness 多次调用应产生不同输出（时间戳/随机量不同）。
func TestGenerateMid_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		seen[GenerateMid()] = struct{}{}
	}
	// 至少应有 2 个不同值（极端情况下时间戳/随机数都碰撞的概率可忽略）。
	if len(seen) < 2 {
		t.Fatalf("GenerateMid produced %d distinct values out of 100", len(seen))
	}
}

// TestGenerateUUID_Format 校验 UUID 形如 "-<毫秒>1"。
func TestGenerateUUID_Format(t *testing.T) {
	u := GenerateUUID()
	if !strings.HasPrefix(u, "-") || !strings.HasSuffix(u, "1") {
		t.Fatalf("invalid uuid: %q", u)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(u, "-"), "1")
	for _, r := range body {
		if r < '0' || r > '9' {
			t.Fatalf("uuid body should be all digits, got %q in %q", r, u)
		}
	}
}

// TestGenerateDeviceID_Format 校验设备 ID 的固定位置约束。
func TestGenerateDeviceID_Format(t *testing.T) {
	cases := []string{"123", "", "user-with-dash", "中文用户", strings.Repeat("u", 100)}
	for _, userID := range cases {
		t.Run(userID, func(t *testing.T) {
			id := GenerateDeviceID(userID)
			// 36 字符 UUID + "-" + userID。
			wantLen := 36 + 1 + len(userID)
			if len(id) != wantLen {
				t.Fatalf("device ID length = %d, want %d (id=%q)", len(id), wantLen, id)
			}
			if !strings.HasSuffix(id, "-"+userID) {
				t.Fatalf("device ID should end with -userID, got %q", id)
			}
			uuid := id[:36]
			// 固定分隔符位置。
			for _, pos := range []int{8, 13, 18, 23} {
				if uuid[pos] != '-' {
					t.Fatalf("device ID pos %d = %q, want '-' (id=%q)", pos, uuid[pos], id)
				}
			}
			// 版本位固定为 '4'。
			if uuid[14] != '4' {
				t.Fatalf("device ID version char = %q, want '4' (id=%q)", uuid[14], id)
			}
			// 变体位取值受限：(rand&0x3)|0x8 → 索引 8..11 → '8','9','A','B'。
			switch uuid[19] {
			case '8', '9', 'A', 'B':
			default:
				t.Fatalf("device ID variant char = %q, want one of 8/9/A/B (id=%q)", uuid[19], id)
			}
			// 其余字符取自 deviceIDChars 字符集。
			const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"
			for i, r := range uuid {
				if !strings.ContainsRune(chars, r) {
					t.Fatalf("device ID pos %d = %q not in allowed charset (id=%q)", i, r, id)
				}
			}
		})
	}
}

// TestGenerateDeviceID_Uniqueness 不同调用产生不同设备 ID（随机位差异）。
func TestGenerateDeviceID_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 50)
	for i := 0; i < 50; i++ {
		seen[GenerateDeviceID("u")] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("GenerateDeviceID produced %d distinct values out of 50", len(seen))
	}
}

// TestGenerateDeviceID_DifferentUsers 不同 userID 产生不同后缀。
func TestGenerateDeviceID_DifferentUsers(t *testing.T) {
	a := GenerateDeviceID("alice")
	b := GenerateDeviceID("bob")
	if strings.HasSuffix(a, "-bob") || strings.HasSuffix(b, "-alice") {
		t.Fatalf("device ID suffix mismatched: a=%q b=%q", a, b)
	}
}

// TestRandomInt_Bounds 覆盖 randomInt 的边界：max<=1 恒返回 0；正常区间值合法。
func TestRandomInt_Bounds(t *testing.T) {
	for _, max := range []int{0, 1, -5} {
		if got := randomInt(max); got != 0 {
			t.Fatalf("randomInt(%d) = %d, want 0", max, got)
		}
	}
	for i := 0; i < 1000; i++ {
		got := randomInt(16)
		if got < 0 || got >= 16 {
			t.Fatalf("randomInt(16) = %d, out of [0,16)", got)
		}
	}
}
