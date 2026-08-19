package server

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
)

// TestOpenAPIRoutesMatchRouter 验证 OpenAPI 与实际 chi 路由树双向覆盖，包含动态 prefix 挂载的订单刷新路由。
func TestOpenAPIRoutesMatchRouter(t *testing.T) {
	// specPath 是从 Server 包测试工作目录定位到唯一 OpenAPI 契约源的绝对路径。
	specPath := filepath.Join("..", "..", "api", "openapi.yaml")
	// document、loadErr 分别是已解析规范及其加载失败原因。
	document, loadErr := openapi3.NewLoader().LoadFromFile(specPath)
	if loadErr != nil {
		t.Fatalf("加载 OpenAPI 契约失败: %v", loadErr)
	}
	// validationErr 表示规范不满足 OpenAPI 语义规则的原因。
	if validationErr := document.Validate(context.Background()); validationErr != nil {
		t.Fatalf("OpenAPI 契约无效: %v", validationErr)
	}
	// server、_、cleanup 分别是完整 chi Router、测试数据库句柄和资源释放函数。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// actualRoutes 保存 Router 实际暴露的版本化 HTTP operation。
	actualRoutes := make(map[string]struct{})
	// walkErr 表示 chi 遍历路由树失败的原因。
	walkErr := chi.Walk(server.Router(), func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		// handler、middlewares 是 chi 提供的实际终点与中间件链；本测试仅校验方法和路径。
		_ = handler
		_ = middlewares
		// canonical 表示当前路由是否必须由 OpenAPI 登记。
		canonical := route == "/health" || strings.HasPrefix(route, "/api/v1/")
		if canonical {
			actualRoutes[strings.ToLower(method)+" "+route] = struct{}{}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("遍历 chi 路由树失败: %v", walkErr)
	}
	// specRoutes 保存文档登记的版本化 HTTP operation。
	specRoutes := make(map[string]struct{})
	for _, path := range document.Paths.Keys() { // path 是当前 OpenAPI 路径模板。
		for method := range document.Paths.Find(path).Operations() { // method 是当前路径声明的 HTTP 方法。
			specRoutes[strings.ToLower(method)+" "+path] = struct{}{}
		}
	}
	for route := range actualRoutes { // route 是实际 Router 中需要契约覆盖的 operation。
		if _, exists := specRoutes[route]; !exists {
			t.Errorf("实际路由未登记到 OpenAPI: %s", route)
		}
	}
	for route := range specRoutes { // route 是 OpenAPI 中必须有真实 Router 对应项的 operation。
		if _, exists := actualRoutes[route]; !exists {
			t.Errorf("OpenAPI 路由不存在于实际 Router: %s", route)
		}
	}
}
