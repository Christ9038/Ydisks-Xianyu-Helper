// Package replyimage 定义默认回复图片引用的纯校验规则，不读取文件或环境配置。
package replyimage

import (
	"errors"
	"io/fs"
	"strings"
	"unicode"
)

// ValidatePath 校验 reference 是账号图片目录内的相对文件引用；空串表示不配置图片。
// 路径保留原文，禁止绝对路径、目录穿越和平台特殊分隔符；不检查文件是否存在。
func ValidatePath(reference string) error {
	if reference == "" {
		return nil
	}
	if len(reference) > 1024 || reference == "." || !fs.ValidPath(reference) || strings.ContainsAny(reference, `\:`) || strings.TrimSpace(reference) != reference {
		return errors.New("本地图片必须使用图片目录内的相对路径，不能包含盘符或目录穿越")
	}
	// character 是路径中的当前字符；控制字符不能进入文件名或错误诊断。
	for _, character := range reference {
		if unicode.IsControl(character) {
			return errors.New("本地图片路径不能包含控制字符")
		}
	}
	return nil
}

// ValidateSource 校验 imageURL 和 imagePath 互斥；仅本地路径使用新格式约束，旧 URL 行为保持不变。
func ValidateSource(imageURL, imagePath string) error {
	if imageURL != "" && imagePath != "" {
		return errors.New("图片 URL 和本地图片路径不能同时配置")
	}
	return ValidatePath(imagePath)
}
