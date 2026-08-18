package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSPAServing 前端静态资源 + index.html 提供。
func TestSPAServing(t *testing.T) {
	// 构造一个临时 web 目录（模拟构建产物）。
	webDir := t.TempDir()
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>SPA</html>"), 0644)
	os.MkdirAll(filepath.Join(webDir, "assets"), 0755)
	os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte("console.log(1)"), 0644)
	os.WriteFile(filepath.Join(webDir, "favicon.svg"), []byte("<svg/>"), 0644)

	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.WebDir = webDir
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// 1) / 应返回 index.html。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/ status=%d", rec.Code)
	}
	if rec.Body.String() != "<html>SPA</html>" {
		t.Errorf("/ 应返回 index.html，got %q", rec.Body.String())
	}

	// 2) /static/assets/app.js 应返回 JS（vite base=/static/）。
	req2 := httptest.NewRequest(http.MethodGet, "/static/assets/app.js", nil)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("/static/assets/app.js status=%d", rec2.Code)
	}
	if rec2.Body.String() != "console.log(1)" {
		t.Errorf("JS 内容异常: %q", rec2.Body.String())
	}

	// 3) /static/favicon.svg。
	// req3 用于本次流程后续判断的req3
	req3 := httptest.NewRequest(http.MethodGet, "/static/favicon.svg", nil)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("favicon status=%d", rec3.Code)
	}

	// 4) 客户端路由（如 /dashboard）应返回 index.html（React Router 接管）。
	req4 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	// rec4 用于本次流程后续判断的rec4
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != 200 || rec4.Body.String() != "<html>SPA</html>" {
		t.Errorf("客户端路由应返回 index.html，status=%d body=%q", rec4.Code, rec4.Body.String())
	}

	// 5) API 路径不应返回 index.html。
	req5 := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	// rec5 用于本次流程后续判断的rec5
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	// /api/orders 需认证，应 401（不是 index.html）。
	if rec5.Body.String() == "<html>SPA</html>" {
		t.Error("API 路径不应返回 index.html")
	}
}

// TestSPAEmbeddedWithoutWebDir 未配置 WebDir 时使用内嵌 SPA。
func TestSPAEmbeddedWithoutWebDir(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.WebDir = ""
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("无 WebDir 应返回内嵌 SPA，got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("内嵌 SPA index.html 异常: %q", rec.Body.String())
	}
}

// init 封装init业务协调。
func init() {
	// newTestServer 用 context.Background，确保可用。
	_ = context.Background
}
