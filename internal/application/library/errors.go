// Package library 提供本地资源库（图片库与商品模板）的应用用例与消费者定义的端口。
package library

import (
	"errors"
	"strings"
)

// ErrImageNotFound 表示图片不存在或不属于当前用户。
var ErrImageNotFound = errors.New("图片不存在")

// ErrTemplateNotFound 表示商品模板不存在或不属于当前用户。
var ErrTemplateNotFound = errors.New("商品模板不存在")

// ValidationError 表示可直接展示给用户的输入校验失败，不含任何敏感信息。
type ValidationError struct {
	// Message 是面向用户的失败原因。
	Message string
}

// Error 返回校验失败原因。
func (e *ValidationError) Error() string { return e.Message }

// invalid 构造一个输入校验错误。
func invalid(message string) error { return &ValidationError{Message: message} }

// ImageInUseError 表示图片仍被商品模板引用，不能删除。
type ImageInUseError struct {
	// Templates 是引用该图片的模板名称。
	Templates []string
}

// Error 返回包含引用模板名称的说明。
func (e *ImageInUseError) Error() string {
	return "图片仍被商品模板引用: " + strings.Join(e.Templates, "、")
}
