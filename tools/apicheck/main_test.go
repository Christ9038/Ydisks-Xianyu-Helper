package main

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestContractSuccessExceptionValidation 验证特殊成功校验只接受 WebSocket、二进制和永久关闭三类受限契约。
func TestContractSuccessExceptionValidation(t *testing.T) {
	// websocketResponses 是包含 101 协议升级响应的 WebSocket 最小响应集合。
	websocketResponses := openapi3.NewResponses(openapi3.WithStatus(101, &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("upgrade")}))
	// websocketOperation 是登记消息 schema 的合法 WebSocket operation。
	websocketOperation := &openapi3.Operation{Responses: websocketResponses, Extensions: map[string]any{
		"x-contract-success-exception": map[string]any{"kind": "websocket"},
		"x-websocket-message-schema":   map[string]any{"$ref": "#/components/schemas/Event"},
	}}
	// got 是从合法 WebSocket operation 解析出的特殊校验类别。
	if got := contractSuccessExceptionKind(websocketOperation); got != "websocket" {
		t.Fatalf("特殊类型=%q", got)
	}
	// violations 保存合法 WebSocket operation 产生的结构违规，预期为空。
	if violations := validateContractSuccessException(websocketOperation); len(violations) != 0 {
		t.Fatalf("合法 WebSocket 特殊校验失败: %v", violations)
	}
	// missingUpgradeOperation 是缺少 101 响应的 WebSocket 错误夹具。
	missingUpgradeOperation := &openapi3.Operation{Responses: openapi3.NewResponses(), Extensions: websocketOperation.Extensions}
	// violations 保存缺少升级响应时必须出现的结构违规。
	if violations := validateContractSuccessException(missingUpgradeOperation); len(violations) == 0 {
		t.Fatal("缺少 101 的 WebSocket 未被拒绝")
	}
	// missingSchemaOperation 是缺少消息 schema 扩展的 WebSocket 错误夹具。
	missingSchemaOperation := &openapi3.Operation{Responses: websocketResponses, Extensions: map[string]any{"x-contract-success-exception": map[string]any{"kind": "websocket"}}}
	// violations 保存缺少事件 schema 时必须出现的结构违规。
	if violations := validateContractSuccessException(missingSchemaOperation); len(violations) == 0 {
		t.Fatal("缺少消息 schema 的 WebSocket 未被拒绝")
	}
	// binaryResponses 是包含 binary schema 的 CSV 成功响应集合。
	binaryResponses := openapi3.NewResponses(openapi3.WithStatus(200, &openapi3.ResponseRef{Value: openapi3.NewResponse().WithContent(openapi3.Content{
		"text/csv": &openapi3.MediaType{Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Format: "binary"}}},
	})}))
	// binaryOperation 是合法二进制下载 operation。
	binaryOperation := &openapi3.Operation{Responses: binaryResponses, Extensions: map[string]any{"x-contract-success-exception": map[string]any{"kind": "binary"}}}
	if !hasBinarySuccessResponse(binaryResponses) || len(validateContractSuccessException(binaryOperation)) != 0 {
		t.Fatal("合法二进制特殊校验失败")
	}
	// invalidBinaryOperation 是缺少 binary format 的二进制错误夹具。
	invalidBinaryOperation := &openapi3.Operation{Responses: openapi3.NewResponses(openapi3.WithStatus(200, &openapi3.ResponseRef{Value: openapi3.NewResponse()})), Extensions: binaryOperation.Extensions}
	// violations 保存二进制下载缺少 binary schema 时必须出现的结构违规。
	if violations := validateContractSuccessException(invalidBinaryOperation); len(violations) == 0 {
		t.Fatal("缺少 binary schema 的特殊校验未被拒绝")
	}
	// disabledOperation 是不声明任何 2xx、只声明 501 的永久关闭 operation。
	disabledOperation := &openapi3.Operation{Responses: openapi3.NewResponses(openapi3.WithStatus(501, &openapi3.ResponseRef{Value: openapi3.NewResponse()})), Extensions: map[string]any{"x-contract-success-exception": map[string]any{"kind": "permanently_disabled"}}}
	if len(validateContractSuccessException(disabledOperation)) != 0 {
		t.Fatal("合法永久关闭特殊校验失败")
	}
	// invalidDisabledOperation 是错误保留 200 成功响应的永久关闭 operation。
	invalidDisabledOperation := &openapi3.Operation{Responses: openapi3.NewResponses(
		openapi3.WithStatus(200, &openapi3.ResponseRef{Value: openapi3.NewResponse()}),
		openapi3.WithStatus(501, &openapi3.ResponseRef{Value: openapi3.NewResponse()}),
	), Extensions: disabledOperation.Extensions}
	// violations 保存永久关闭 operation 仍声明成功码时必须出现的结构违规。
	if violations := validateContractSuccessException(invalidDisabledOperation); len(violations) == 0 {
		t.Fatal("保留 200 的永久关闭 operation 未被拒绝")
	}
	// invalidKindOperation 是未登记类别的特殊校验 operation。
	invalidKindOperation := &openapi3.Operation{Extensions: map[string]any{"x-contract-success-exception": map[string]any{"kind": "other"}}}
	// violations 保存未知特殊校验类别必须产生的结构违规。
	if violations := validateContractSuccessException(invalidKindOperation); len(violations) == 0 {
		t.Fatal("未知特殊校验类别未被拒绝")
	}
	// malformedOperation 是扩展值不是对象的错误 operation。
	malformedOperation := &openapi3.Operation{Extensions: map[string]any{"x-contract-success-exception": "websocket"}}
	if contractSuccessExceptionKind(malformedOperation) != "" || len(validateContractSuccessException(malformedOperation)) == 0 {
		t.Fatal("格式错误的特殊校验扩展未被拒绝")
	}
}
