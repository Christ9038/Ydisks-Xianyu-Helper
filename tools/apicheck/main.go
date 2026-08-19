// apicheck 校验 OpenAPI 文档、版本化路由登记和生成契约的结构约束。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// main 加载并验证 OpenAPI 文档的静态结构；真实 chi 路由双向覆盖由 Server 契约测试执行。
func main() {
	// root 是待检查仓库根目录，默认使用当前工作目录。
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	// violations、err 分别是契约检查结果和执行失败原因。
	violations, err := check(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "api-check: %v\n", err)
		os.Exit(1)
	}
	for _, violation := range violations { // violation 是待输出的单条契约违规。
		fmt.Fprintln(os.Stderr, violation)
	}
	if len(violations) > 0 {
		os.Exit(1)
	}
	fmt.Println("api-check: 通过")
}

// check 执行规范语法和 operation 元数据检查。
func check(root string) ([]string, error) {
	// specPath 是仓库中唯一 OpenAPI 契约文件路径。
	specPath := filepath.Join(root, "api", "openapi.yaml")
	// document、err 分别是解析后的 OpenAPI 文档和加载失败原因。
	document, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("加载 OpenAPI 失败: %w", err)
	}
	// err 是 OpenAPI 语义校验失败原因。
	if err := document.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("OpenAPI 规范无效: %w", err)
	}
	// violations 保存规范结构和路由覆盖的全部问题。
	var violations []string
	for _, path := range document.Paths.Keys() { // path 是当前 OpenAPI 路径模板。
		// item 是当前路径的所有 HTTP operation 集合。
		item := document.Paths.Find(path)
		for method, operation := range item.Operations() { // method、operation 是当前 HTTP 方法和定义。
			method = strings.ToLower(method)
			// exceptionKind 是当前 operation 显式声明的成功响应特殊校验类型；空值表示必须具备普通成功响应。
			exceptionKind := contractSuccessExceptionKind(operation)
			if operation.OperationID == "" {
				violations = append(violations, fmt.Sprintf("%s %s 缺少 operationId", strings.ToUpper(method), path))
			}
			if operation.Responses == nil || exceptionKind == "" && !hasSuccessResponse(operation.Responses) {
				violations = append(violations, fmt.Sprintf("%s %s 缺少成功响应", strings.ToUpper(method), path))
			}
			// exceptionViolations 是特殊成功校验元数据不满足其受限语义时产生的全部错误。
			for _, exceptionViolation := range validateContractSuccessException(operation) {
				violations = append(violations, fmt.Sprintf("%s %s %s", strings.ToUpper(method), path, exceptionViolation))
			}
			if operation.Responses == nil || operation.Responses.Value("400") == nil || operation.Responses.Value("401") == nil {
				violations = append(violations, fmt.Sprintf("%s %s 缺少统一错误响应", strings.ToUpper(method), path))
			}
			if operation.Security == nil {
				violations = append(violations, fmt.Sprintf("%s %s 缺少鉴权元数据", strings.ToUpper(method), path))
			}
		}
	}
	return violations, nil
}

// contractSuccessExceptionKind 返回 operation 显式登记的成功响应特殊校验类型；无法解析的值交给校验函数报错。
func contractSuccessExceptionKind(operation *openapi3.Operation) string {
	if operation == nil || operation.Extensions == nil {
		return ""
	}
	// rawException、exists 分别是 YAML 扩展原值及其是否已登记。
	rawException, exists := operation.Extensions["x-contract-success-exception"]
	if !exists {
		return ""
	}
	// exception、ok 分别是扩展对象及其结构是否符合 OpenAPI 扩展的 YAML 解码结果。
	exception, ok := rawException.(map[string]any)
	if !ok {
		return ""
	}
	// kind、ok 分别是特殊校验类别及其字符串类型断言结果。
	kind, ok := exception["kind"].(string)
	if !ok {
		return ""
	}
	return kind
}

// validateContractSuccessException 只允许 WebSocket、二进制和永久关闭接口跳过普通 2xx 成功响应，并验证各自的强制契约。
func validateContractSuccessException(operation *openapi3.Operation) []string {
	if operation == nil || operation.Extensions == nil {
		return nil
	}
	// rawException、exists 分别是特殊校验扩展原值及是否显式登记。
	rawException, exists := operation.Extensions["x-contract-success-exception"]
	if !exists {
		return nil
	}
	// exception、ok 分别是解码后的扩展对象及其结构是否有效。
	exception, ok := rawException.(map[string]any)
	if !ok {
		return []string{"x-contract-success-exception 必须是包含 kind 的对象"}
	}
	// kind、ok 分别是特殊校验类别及其是否为字符串。
	kind, ok := exception["kind"].(string)
	if !ok {
		return []string{"x-contract-success-exception.kind 必须是字符串"}
	}
	switch kind {
	case "websocket":
		if operation.Responses == nil || operation.Responses.Value("101") == nil {
			return []string{"WebSocket 特殊校验必须声明 101 协议升级响应"}
		}
		// exists 表示 WebSocket operation 是否同时登记了可校验的服务端事件 schema。
		if _, exists := operation.Extensions["x-websocket-message-schema"]; !exists {
			return []string{"WebSocket 特殊校验必须声明 x-websocket-message-schema"}
		}
	case "binary":
		if !hasBinarySuccessResponse(operation.Responses) {
			return []string{"二进制特殊校验必须声明含 binary format 的 2xx 响应"}
		}
	case "permanently_disabled":
		if hasSuccessResponse(operation.Responses) || operation.Responses == nil || operation.Responses.Value("501") == nil {
			return []string{"永久关闭特殊校验只能声明 501 错误响应，不得保留 2xx 或 101 成功响应"}
		}
	default:
		return []string{fmt.Sprintf("不支持的 x-contract-success-exception.kind %q", kind)}
	}
	return nil
}

// hasBinarySuccessResponse 判断 operation 是否声明内容格式为 binary 的普通 HTTP 成功响应。
func hasBinarySuccessResponse(responses *openapi3.Responses) bool {
	if responses == nil {
		return false
	}
	// status 是当前待检查的 2xx 成功状态码。
	for _, status := range []string{"200", "201", "202", "204"} {
		// responseRef 是当前状态码对应的 OpenAPI 响应定义。
		responseRef := responses.Value(status)
		if responseRef == nil || responseRef.Value == nil {
			continue
		}
		// mediaType、media 分别是当前响应声明的媒体类型及其 schema 定义。
		for mediaType, media := range responseRef.Value.Content {
			if media != nil && media.Schema != nil && media.Schema.Value != nil && media.Schema.Value.Format == "binary" && !strings.HasPrefix(mediaType, "application/json") {
				return true
			}
		}
	}
	return false
}

// hasSuccessResponse 判断 operation 是否声明了至少一个合法成功或协议升级状态。
func hasSuccessResponse(responses *openapi3.Responses) bool {
	if responses == nil {
		return false
	}
	// status 是当前允许声明为操作成功的 HTTP 或协议升级状态码。
	for _, status := range []string{"200", "201", "202", "204", "101"} {
		if responses.Value(status) != nil {
			return true
		}
	}
	return false
}
