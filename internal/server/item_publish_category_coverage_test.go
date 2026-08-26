package server

import (
	"context"
	"errors"
	"net/http"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// itemCategoryHandlerCoveragePort 为商品类目推荐 handler 注入可控的推荐结果。
type itemCategoryHandlerCoveragePort struct {
	// result 保存测试配置的类目推荐结果。
	result itemapp.BatchPreviewCategory
	// err 保存测试配置的类目推荐错误。
	err error
}

// Recommend 返回测试配置的类目推荐结果或错误。
func (port *itemCategoryHandlerCoveragePort) Recommend(context.Context, int64, string, string) (itemapp.BatchPreviewCategory, error) {
	return port.result, port.err
}

// TestRecommendItemPublishCategoryHandlerCoversValidationAndErrors 覆盖商品类目推荐的输入校验及错误映射。
func TestRecommendItemPublishCategoryHandlerCoversValidationAndErrors(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是当前测试注入的商品类目推荐应用端口。
	port := &itemCategoryHandlerCoveragePort{result: itemapp.BatchPreviewCategory{CatID: "5001", CatName: "虚拟商品", ChannelCatID: "6001", TBCatID: "7001"}}
	srv.applications.itemCategoryRecommendation = port
	// handler 是注入可控类目推荐端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// successRecorder 保存类目推荐成功响应。
	successRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/items/publish-categories/recommend", `{"cookie_id":"acc1","keyword":"资料"}`)
	if successRecorder.Code != http.StatusOK {
		t.Fatalf("success status=%d body=%s", successRecorder.Code, successRecorder.Body.String())
	}

	// validationCases 保存类目推荐请求校验场景。
	validationCases := []string{"{", `{}`, `{"cookie_id":"acc1"}`, `{"keyword":"资料"}`}
	// validationBody 表示当前类目推荐非法请求体。
	for _, validationBody := range validationCases {
		// recorder 保存当前类目推荐校验响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/items/publish-categories/recommend", validationBody)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("validation status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}

	// errorCases 保存平台类目推荐错误及 HTTP 状态码。
	errorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"unsupported", itemapp.ErrCategoryUnsupported, http.StatusNotImplemented},
		{"credential changed", itemapp.ErrCategoryCredentialChanged, http.StatusConflict},
		{"persistence", itemapp.ErrCategoryPersistence, http.StatusInternalServerError},
		{"unrecognized", itemapp.ErrCategoryUnrecognized, http.StatusNotFound},
		{"gateway", errors.New("platform failed"), http.StatusBadGateway},
	}
	// errorCase 表示当前类目推荐错误映射场景。
	for _, errorCase := range errorCases {
		port.err = errorCase.err
		// recorder 保存当前类目推荐错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/items/publish-categories/recommend", `{"cookie_id":"acc1","keyword":"资料"}`)
		if recorder.Code != errorCase.status {
			t.Errorf("%s status=%d want=%d body=%s", errorCase.name, recorder.Code, errorCase.status, recorder.Body.String())
		}
	}
}
