package engine

import "testing"

// TestExtractChatPeerUserIDBoundaries 覆盖聊天深链缺失、非法、当前账号和合法对端身份。
func TestExtractChatPeerUserIDBoundaries(t *testing.T) {
	// cases 保存深链输入及其期望的对端身份。
	cases := []struct {
		// reminderURL 表示平台通知深链。
		reminderURL string
		// selfUserID 表示当前账号的平台身份。
		selfUserID string
		// want 表示可信对端身份；空值表示应拒绝。
		want string
	}{
		{"", "self", ""},
		{"%zz", "self", ""},
		{"https://example.invalid/chat", "self", ""},
		{"https://example.invalid/chat?peerUserId=self", "self", ""},
		{"https://example.invalid/chat?peerUserId=self%40goofish", "self", ""},
		{"https://example.invalid/chat?peerUserId=buyer%40goofish", "self", "buyer"},
	}
	// testCase 表示当前待验证的深链身份样例。
	for _, testCase := range cases {
		// got 表示解析出的可信对端身份。
		if got := extractChatPeerUserID(testCase.reminderURL, testCase.selfUserID); got != testCase.want {
			t.Errorf("URL=%q got=%q want=%q", testCase.reminderURL, got, testCase.want)
		}
	}
}

// TestInt64ToStringBoundaries 覆盖零、正数和负数的协议数字格式化。
func TestInt64ToStringBoundaries(t *testing.T) {
	// cases 保存整数输入及其十进制文本。
	cases := []struct {
		// input 表示待格式化的整数。
		input int64
		// want 表示稳定的十进制文本。
		want string
	}{
		{0, "0"},
		{7, "7"},
		{1234567890, "1234567890"},
		{-42, "-42"},
	}
	// testCase 表示当前待验证的整数格式化样例。
	for _, testCase := range cases {
		// got 表示整数格式化后的协议文本。
		if got := int64ToString(testCase.input); got != testCase.want {
			t.Errorf("input=%d got=%q want=%q", testCase.input, got, testCase.want)
		}
	}
}
