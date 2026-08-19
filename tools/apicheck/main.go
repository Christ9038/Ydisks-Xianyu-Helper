// apicheck 校验 OpenAPI 文档、版本化路由登记和生成契约的结构约束。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// routeKey 是路由方法与路径的稳定组合键。
type routeKey struct {
	// method 是小写 HTTP 方法。
	method string
	// path 是 chi 路由路径。
	path string
}

// main 加载并验证 OpenAPI 文档，同时检查真实版本化路由的双向覆盖。
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

// check 执行规范语法、operation 元数据和路由双向覆盖检查。
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
			if operation.OperationID == "" {
				violations = append(violations, fmt.Sprintf("%s %s 缺少 operationId", strings.ToUpper(method), path))
			}
			if operation.Responses == nil || !hasSuccessResponse(operation.Responses) {
				violations = append(violations, fmt.Sprintf("%s %s 缺少成功响应", strings.ToUpper(method), path))
			}
			if operation.Responses == nil || operation.Responses.Value("400") == nil || operation.Responses.Value("401") == nil {
				violations = append(violations, fmt.Sprintf("%s %s 缺少统一错误响应", strings.ToUpper(method), path))
			}
			if operation.Security == nil {
				violations = append(violations, fmt.Sprintf("%s %s 缺少鉴权元数据", strings.ToUpper(method), path))
			}
		}
	}
	// routes、err 分别是真实版本化路由集合和收集失败原因。
	routes, err := sourceRoutes(filepath.Join(root, "internal", "server"))
	if err != nil {
		return nil, err
	}
	// specRoutes 保存规范登记的路由方法和路径组合键。
	specRoutes := make(map[routeKey]struct{})
	for _, path := range document.Paths.Keys() { // path 是当前规范路径模板。
		for method := range document.Paths.Find(path).Operations() { // method 是当前规范 HTTP 方法。
			specRoutes[routeKey{method: strings.ToLower(method), path: path}] = struct{}{}
		}
	}
	for route := range routes { // route 是真实 Router 中发现的 operation。
		// exists 表示真实 operation 是否已经登记到 OpenAPI。
		if _, exists := specRoutes[route]; !exists {
			violations = append(violations, fmt.Sprintf("真实路由 %s %s 未登记到 api/openapi.yaml", strings.ToUpper(route.method), route.path))
		}
	}
	for route := range specRoutes { // route 是规范中声明的 operation。
		// exists 表示规范 operation 是否存在真实版本化路由。
		if _, exists := routes[route]; !exists {
			violations = append(violations, fmt.Sprintf("OpenAPI 登记 %s %s 不存在真实版本化路由", strings.ToUpper(route.method), route.path))
		}
	}
	sort.Strings(violations)
	return violations, nil
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

// sourceRoutes 从版本化 route 文件和动态订单刷新挂载点收集真实路由。
func sourceRoutes(serverDir string) (map[routeKey]struct{}, error) {
	// methodPattern 匹配版本化文件中的静态 chi 方法调用。
	methodPattern := regexp.MustCompile(`r\.(Get|Post|Put|Patch|Delete)\("([^"\n]+)"`)
	// routes 保存扫描到的真实版本化路由。
	routes := make(map[routeKey]struct{})
	// entries、err 分别是路由目录项和读取失败原因。
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return nil, fmt.Errorf("读取 Server 路由目录失败: %w", err)
	}
	for _, entry := range entries { // entry 是当前版本化路由源文件目录项。
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "versioned_") || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		// source、readErr 分别是路由源码和读取失败原因。
		source, readErr := os.ReadFile(filepath.Join(serverDir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("读取路由文件 %s 失败: %w", entry.Name(), readErr)
		}
		for _, match := range methodPattern.FindAllSubmatch(source, -1) { // match 是当前静态路由匹配结果。
			// path 是匹配结果中的 chi 路由路径。
			path := string(match[2])
			if path == "/health" || strings.HasPrefix(path, "/api/v1/") {
				routes[routeKey{method: strings.ToLower(string(match[1])), path: path}] = struct{}{}
			}
		}
	}
	// mountOrderRefreshJobRoutes 使用 prefix 拼接路径，静态正则无法看到其三个 operation。
	routes[routeKey{method: "post", path: "/api/v1/orders/refresh"}] = struct{}{}
	routes[routeKey{method: "get", path: "/api/v1/orders/refresh/{job_id}"}] = struct{}{}
	routes[routeKey{method: "delete", path: "/api/v1/orders/refresh/{job_id}"}] = struct{}{}
	return routes, nil
}
