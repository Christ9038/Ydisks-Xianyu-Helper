package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSourceRoutesIncludesDynamicOrderRefresh 验证动态 prefix 挂载的订单刷新 operation 不会从路由登记中遗漏。
func TestSourceRoutesIncludesDynamicOrderRefresh(t *testing.T) {
	// serverDir 是本测试构造的版本化路由源码目录。
	serverDir := t.TempDir()
	// source 是包含一个静态路由的最小 chi 路由源码夹具。
	source := []byte(`package server
func mount(r any) {
  r.Get("/api/v1/session", nil)
}
`)
	// writeErr 是写入路由源码夹具失败的原因。
	if writeErr := os.WriteFile(filepath.Join(serverDir, "versioned_session_routes.go"), source, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// routes、readErr 分别是收集到的路由集合和读取失败原因。
	routes, readErr := sourceRoutes(serverDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	// expected 是动态订单刷新 POST operation 的稳定键。
	expected := routeKey{method: "post", path: "/api/v1/orders/refresh"}
	// exists 表示动态订单刷新 operation 是否已被发现。
	if _, exists := routes[expected]; !exists {
		t.Fatalf("动态订单刷新路由未登记: %+v", routes)
	}
	// staticRoute 是夹具中的静态 session operation。
	staticRoute := routeKey{method: "get", path: "/api/v1/session"}
	// exists 表示静态 session operation 是否已被发现。
	if _, exists := routes[staticRoute]; !exists {
		t.Fatalf("静态路由未登记: %+v", routes)
	}
}
