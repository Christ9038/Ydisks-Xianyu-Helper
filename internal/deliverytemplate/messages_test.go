package deliverytemplate

import (
	"reflect"
	"testing"
)

// TestParseMessagesImagesAndLegacy 验证纯图片、混合顺序、旧文本字段及变量仅从正文提取。
func TestParseMessagesImagesAndLegacy(t *testing.T) {
	// messages 保存混合独立图片与文本的固定输入。
	messages := []Message{{Type: "image", ImagePath: "说明/图.png"}, {Content: "{{cards.code}} {{custom.note}}"}, {Type: "image", ImageURL: "https://example.test/a.png"}}
	// parsed、err 保存结构化解析结果及校验错误。
	parsed, err := ParseMessages(messages)
	if err != nil || len(parsed.MessageItems) != 3 || parsed.MessageItems[1].Type != "text" || !reflect.DeepEqual(parsed.Messages, []string{messages[1].Content}) || !reflect.DeepEqual(parsed.Keys, []string{"code"}) || !reflect.DeepEqual(parsed.CustomKeys, []string{"note"}) {
		t.Fatalf("混合消息解析异常: %+v err=%v", parsed, err)
	}
	messages[0].ImagePath = "changed.png"
	if parsed.MessageItems[0].ImagePath != "说明/图.png" {
		t.Fatal("解析结果共享调用方切片")
	}
	// onlyImage、imageErr 保存没有文本的合法模板解析结果。
	onlyImage, imageErr := ParseMessages([]Message{{Type: "image", ImagePath: "image.png"}})
	if imageErr != nil || len(onlyImage.MessageItems) != 1 || len(onlyImage.Messages) != 0 || len(onlyImage.Keys) != 0 {
		t.Fatalf("纯图片模板异常: %+v err=%v", onlyImage, imageErr)
	}
	// legacy、legacyErr 验证历史文本入口仍保留原正文和兼容切片。
	legacy, legacyErr := Parse([]string{" 正文 "})
	if legacyErr != nil || legacy.Messages[0] != " 正文 " || legacy.MessageItems[0].Type != "text" {
		t.Fatalf("旧文本解析异常: %+v err=%v", legacy, legacyErr)
	}
}

// TestParseMessagesRejectsInvalidImages 覆盖图片来源冲突、空来源、文本污染、非法路径和不支持的插值。
func TestParseMessagesRejectsInvalidImages(t *testing.T) {
	// cases 中每条消息均不能保存为合法模板。
	cases := []Message{
		{Type: "video", Content: "正文"}, {Content: ""}, {Content: "{{bad}}"},
		{Content: "text", ImagePath: "a.png"}, {Content: "text", ImageURL: "https://example.test/a.png"},
		{Type: "image"}, {Type: "image", Content: " ", ImagePath: "a.png"},
		{Type: "image", ImagePath: "a.png", ImageURL: "https://example.test/a.png"},
		{Type: "image", ImageURL: "file:///tmp/a.png"}, {Type: "image", ImageURL: "https://user:pass@example.test/a"},
		{Type: "image", ImageURL: "https://example.test/{{order_id}}.png"}, {Type: "image", ImageURL: "%zz"},
		{Type: "image", ImagePath: "../a.png"}, {Type: "image", ImagePath: "/a.png"}, {Type: "image", ImagePath: `C:\a.png`},
		{Type: "image", ImagePath: "a/../b.png"}, {Type: "image", ImagePath: "a\x00.png"}, {Type: "image", ImagePath: "{{cards.code}}.png"},
	}
	// index 和 message 分别是非法场景位置与具体输入。
	for index, message := range cases {
		if _, err := ParseMessages([]Message{message}); err == nil { // err 是应当出现的纯校验错误。
			t.Fatalf("非法消息 %d 被接受", index)
		}
	}
	if _, err := ParseMessages(nil); err == nil { // err 验证完全缺失消息也必须拒绝。
		t.Fatal("空模板被接受")
	}
}
