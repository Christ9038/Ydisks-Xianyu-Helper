package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestOpenAPIOperationsHaveContractScenarios 从 OpenAPI operationId 自动生成最小请求，确保每个版本化 operation 都经过真实 Router 响应契约校验。
func TestOpenAPIOperationsHaveContractScenarios(t *testing.T) {
	// document 是唯一 OpenAPI 契约源的解析结果。
	document := loadOpenAPIContractForCoverage(t)
	// operations 保存按路径和方法排序后的全部版本化 operation 场景。
	operations := collectOpenAPIOperations(document)
	// operation 表示当前规范枚举的 operation 场景，副本用于闭包隔离。
	for _, operation := range operations {
		// operation 是由 OpenAPI 文档直接枚举的当前测试场景，不允许手工名单遗漏。
		operation := operation
		t.Run(operation.operation.OperationID, func(t *testing.T) {
			// srv、_、cleanup 分别是当前 operation 独占的真实 Server、测试数据库和资源释放函数。
			srv, _, cleanup := newTestServer(t)
			defer cleanup()
			// handler 是当前 operation 使用的完整 chi Router。
			handler := srv.Router()
			// sessionCookie 是当前 operation 独占的管理员会话，避免删除或登出影响其他场景。
			sessionCookie := loginHelper(t, handler)
			// request 是根据路径参数、必需查询参数和请求体 schema 生成的最小真实请求。
			request := newOpenAPICoverageRequest(t, operation.path, operation.method, operation.operation)
			request.AddCookie(sessionCookie)
			// recorder 捕获真实 Router 对当前 operation 的状态、响应头和响应体。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertOpenAPIResponse(t, request, recorder)
		})
	}
}

// openAPICoverageOperation 保存一个由 OpenAPI 路径、方法和 operationId 构成的自动测试场景。
type openAPICoverageOperation struct {
	// key 是方法和路径组成的稳定场景键。
	key string
	// path 是 OpenAPI 路径模板。
	path string
	// method 是小写 HTTP 方法。
	method string
	// operation 是当前 operation 的完整契约定义。
	operation *openapi3.Operation
}

// loadOpenAPIContractForCoverage 加载并校验自动 operation 覆盖测试使用的契约文档。
func loadOpenAPIContractForCoverage(t *testing.T) *openapi3.T {
	t.Helper()
	// specPath 是从 Server 测试目录定位到唯一 OpenAPI 契约源的路径。
	specPath := filepath.Join("..", "..", "api", "openapi.yaml")
	// document、loadErr 分别是解析后的契约文档和加载失败原因。
	document, loadErr := openapi3.NewLoader().LoadFromFile(specPath)
	if loadErr != nil {
		t.Fatalf("加载 OpenAPI 契约失败: %v", loadErr)
	}
	// validationErr 表示契约未满足 OpenAPI 语义约束的原因。
	if validationErr := document.Validate(context.Background()); validationErr != nil {
		t.Fatalf("OpenAPI 契约无效: %v", validationErr)
	}
	return document
}

// collectOpenAPIOperations 从规范自动收集全部版本化 operation，不维护独立路径或 DTO 名单。
func collectOpenAPIOperations(document *openapi3.T) []openAPICoverageOperation {
	// operations 保存待运行的所有 OpenAPI operation 场景。
	operations := make([]openAPICoverageOperation, 0)
	// path 表示 OpenAPI 中登记的 HTTP 路径模板。
	for _, path := range document.Paths.Keys() {
		// pathItem 是当前路径模板的 operation 集合。
		pathItem := document.Paths.Find(path)
		// method 表示当前路径登记的 HTTP 动词；operation 表示其契约定义。
		for method, operation := range pathItem.Operations() {
			// normalizedMethod 是用于排序和构造请求的标准小写方法。
			normalizedMethod := strings.ToLower(method)
			operations = append(operations, openAPICoverageOperation{
				key:       normalizedMethod + " " + path,
				path:      path,
				method:    normalizedMethod,
				operation: operation,
			})
		}
	}
	sort.Slice(operations, func(left, right int) bool {
		// left、right 是待比较的场景下标，排序保证失败输出稳定。
		return operations[left].key < operations[right].key
	})
	return operations
}

// newOpenAPICoverageRequest 根据 operation 的参数和请求体 schema 生成真实 HTTP 请求。
func newOpenAPICoverageRequest(t *testing.T, path string, method string, operation *openapi3.Operation) *http.Request {
	t.Helper()
	// resolvedPath 是将 OpenAPI 路径参数替换为可解析占位值后的 URL 路径。
	resolvedPath := path
	// parameter 表示当前 operation 的路径参数定义。
	for _, parameter := range operation.Parameters {
		// parameterValue 是当前路径或查询参数的最小稳定值。
		if parameter.Value == nil || parameter.Value.In != "path" {
			continue
		}
		resolvedPath = strings.ReplaceAll(resolvedPath, "{"+parameter.Value.Name+"}", openAPIParameterValue(parameter.Value.Name, parameter.Value.Schema))
	}
	// queryValues 保存规范声明的必需查询参数，避免自动场景依赖手工路径列表。
	queryValues := url.Values{}
	// parameter 表示当前 operation 的查询参数定义。
	for _, parameter := range operation.Parameters {
		// parameterValue 是当前参数引用解析后的值。
		parameterValue := parameter.Value
		if parameterValue == nil || parameterValue.In != "query" || !parameterValue.Required {
			continue
		}
		queryValues.Set(parameterValue.Name, openAPIParameterValue(parameterValue.Name, parameterValue.Schema))
	}
	// encodedQuery 是将必需查询参数按 URL 规则编码后的字符串。
	if encodedQuery := queryValues.Encode(); encodedQuery != "" {
		resolvedPath += "?" + encodedQuery
	}
	// body、contentType 分别是按请求体 schema 生成的载荷和媒体类型。
	body, contentType := openAPICoverageRequestBody(t, operation.RequestBody)
	// request 是供真实 chi Router 执行的最小 HTTP 请求。
	request := httptest.NewRequest(strings.ToUpper(method), resolvedPath, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request
}

// openAPICoverageRequestBody 按 OpenAPI 请求体媒体类型生成 JSON 或 multipart 载荷。
func openAPICoverageRequestBody(t *testing.T, requestBody *openapi3.RequestBodyRef) ([]byte, string) {
	t.Helper()
	if requestBody == nil || requestBody.Value == nil || len(requestBody.Value.Content) == 0 {
		return nil, ""
	}
	// mediaType 是优先选择 JSON、其次 multipart、最后按稳定顺序选择的请求媒体类型。
	mediaType := ""
	// candidate 表示请求体支持的一种媒体类型。
	for candidate := range requestBody.Value.Content {
		if candidate == "application/json" {
			mediaType = candidate
			break
		}
		if mediaType == "" && strings.HasPrefix(candidate, "multipart/") {
			mediaType = candidate
		}
		if mediaType == "" {
			mediaType = candidate
		}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		// buffer、writer 分别保存 multipart 请求体和字段编码器。
		buffer := &bytes.Buffer{}
		// writer 负责以边界格式编码最小 multipart 请求体。
		writer := multipart.NewWriter(buffer)
		// closeErr 表示 multipart 编码器关闭并写入结尾边界时的错误。
		if closeErr := writer.Close(); closeErr != nil {
			t.Fatalf("生成 multipart 契约请求失败: %v", closeErr)
		}
		return buffer.Bytes(), writer.FormDataContentType()
	}
	// mediaSchema 是当前媒体类型声明的 JSON schema。
	mediaSchema := requestBody.Value.Content[mediaType]
	if mediaSchema == nil || mediaSchema.Schema == nil {
		return []byte("{}"), mediaType
	}
	// payload 是依据 schema 已知字段递归生成的 JSON 值，主动覆盖可选输入字段的类型约束。
	payload := openAPICoverageRequestValue(mediaSchema.Schema)
	// encoded、marshalErr 分别是 JSON 请求体和编码失败原因。
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		t.Fatalf("生成 JSON 契约请求失败: %v", marshalErr)
	}
	return encoded, mediaType
}

// openAPICoverageRequestValue 按请求 schema 生成包含全部已知属性的最小 JSON 值，避免可选 DTO 字段遗漏长期未被调用。
func openAPICoverageRequestValue(schemaRef *openapi3.SchemaRef) any {
	if schemaRef == nil || schemaRef.Value == nil {
		return nil
	}
	// schema 是当前递归层的具体请求 schema 定义。
	schema := schemaRef.Value
	if schema.Example != nil || schema.Default != nil || schema.Const != nil || len(schema.Enum) > 0 || len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return openAPICoverageValue(schemaRef)
	}
	if schema.Type == nil || len(*schema.Type) == 0 || (*schema.Type)[0] != "object" {
		return openAPICoverageValue(schemaRef)
	}
	// propertyNames 保存按字典序生成的已知请求字段，保证失败输出和负载稳定。
	propertyNames := make([]string, 0, len(schema.Properties))
	// propertyName 表示当前 schema 中发现的请求属性名称。
	for propertyName := range schema.Properties {
		propertyNames = append(propertyNames, propertyName)
	}
	sort.Strings(propertyNames)
	// objectValue 保存所有已声明请求字段的递归最小值。
	objectValue := make(map[string]any, len(propertyNames))
	// propertyName 表示当前需要编码到请求 JSON 中的 schema 属性名称。
	for _, propertyName := range propertyNames {
		// property 是当前请求字段的类型约束。
		property := schema.Properties[propertyName]
		objectValue[propertyName] = openAPICoverageRequestValue(property)
	}
	return objectValue
}

// openAPICoverageValue 按 schema 生成满足结构约束的最小 JSON 值。
func openAPICoverageValue(schemaRef *openapi3.SchemaRef) any {
	if schemaRef == nil || schemaRef.Value == nil {
		return nil
	}
	// schema 是当前递归层的具体 schema 定义。
	schema := schemaRef.Value
	if schema.Example != nil {
		return schema.Example
	}
	if schema.Default != nil {
		return schema.Default
	}
	if schema.Const != nil {
		return schema.Const
	}
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}
	if len(schema.OneOf) > 0 {
		return openAPICoverageValue(schema.OneOf[0])
	}
	if len(schema.AnyOf) > 0 {
		return openAPICoverageValue(schema.AnyOf[0])
	}
	// schemaType 是当前 schema 的首个声明类型。
	schemaType := ""
	if schema.Type != nil && len(*schema.Type) > 0 {
		schemaType = (*schema.Type)[0]
	}
	switch schemaType {
	case "object":
		// objectValue 保存对象必需字段的递归结果。
		objectValue := make(map[string]any, len(schema.Required))
		// requiredName 表示当前对象必须生成的字段名称。
		for _, requiredName := range schema.Required {
			// property 是必需字段的 schema 定义。
			property := schema.Properties[requiredName]
			objectValue[requiredName] = openAPICoverageValue(property)
		}
		return objectValue
	case "array":
		if schema.Items == nil {
			return []any{}
		}
		if schema.MinItems > 0 {
			return []any{openAPICoverageValue(schema.Items)}
		}
		return []any{}
	case "integer":
		if schema.Min != nil {
			return int64(*schema.Min)
		}
		return int64(1)
	case "number":
		if schema.Min != nil {
			return *schema.Min
		}
		return float64(1)
	case "boolean":
		return true
	default:
		if schema.Format == "date-time" {
			return "2026-01-01T00:00:00Z"
		}
		return "contract-test"
	}
}

// openAPIParameterValue 依据参数名称和 schema 生成可被 handler 解析的路径或查询值。
func openAPIParameterValue(name string, schemaRef *openapi3.SchemaRef) string {
	// normalizedName 是便于判断业务语义的参数名称。
	normalizedName := strings.ToLower(name)
	switch {
	case strings.Contains(normalizedName, "user_id"), strings.Contains(normalizedName, "card_id"), strings.Contains(normalizedName, "channel_id"):
		return "999999"
	case strings.Contains(normalizedName, "index"):
		return "0"
	case strings.Contains(normalizedName, "cookie"), strings.HasSuffix(normalizedName, "cid"), strings.Contains(normalizedName, "account"):
		return "acc1"
	case strings.Contains(normalizedName, "order"):
		return "missing-order"
	case strings.Contains(normalizedName, "session"):
		return "missing-session"
	case strings.Contains(normalizedName, "batch"):
		return "missing-batch"
	case strings.Contains(normalizedName, "job"):
		return "missing-job"
	case strings.Contains(normalizedName, "task"), strings.Contains(normalizedName, "run"), strings.Contains(normalizedName, "rule"):
		return "missing-resource"
	case strings.Contains(normalizedName, "item"):
		return "missing-item"
	case normalizedName == "key":
		return "theme_color"
	}
	// value 是没有业务名称提示时按 schema 生成的通用参数值。
	value := openAPICoverageValue(schemaRef)
	// typedValue 是按 OpenAPI 参数 schema 生成的具体基础类型值。
	switch typedValue := value.(type) {
	case string:
		return typedValue
	case bool:
		return strconv.FormatBool(typedValue)
	case int64:
		return strconv.FormatInt(typedValue, 10)
	case float64:
		return strconv.FormatFloat(typedValue, 'f', -1, 64)
	default:
		return fmt.Sprint(typedValue)
	}
}
