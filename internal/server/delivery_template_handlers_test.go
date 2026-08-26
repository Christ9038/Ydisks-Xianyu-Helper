package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// deliveryTemplatePortStub 保存模板 HTTP handler 测试需要注入的结果和错误。
type deliveryTemplatePortStub struct {
	// listResult 是列表接口返回的应用模板集合。
	listResult []deliveryapp.Template
	// listErr 是列表接口模拟的应用错误。
	listErr error
	// getResult 是单模板接口返回的应用模板。
	getResult deliveryapp.Template
	// getErr 是单模板接口模拟的应用错误。
	getErr error
	// createID 是创建接口返回的新模板标识。
	createID int64
	// createErr 是创建接口模拟的应用错误。
	createErr error
	// updateErr 是更新接口模拟的应用错误。
	updateErr error
	// deleteErr 是删除接口模拟的应用错误。
	deleteErr error
}

// List 返回测试替身预置的模板列表。
func (s *deliveryTemplatePortStub) List(context.Context, int64) ([]deliveryapp.Template, error) {
	return s.listResult, s.listErr
}

// Get 返回测试替身预置的单模板结果。
func (s *deliveryTemplatePortStub) Get(context.Context, int64, int64) (deliveryapp.Template, error) {
	return s.getResult, s.getErr
}

// Create 返回测试替身预置的创建结果。
func (s *deliveryTemplatePortStub) Create(context.Context, int64, deliveryapp.Draft) (int64, error) {
	return s.createID, s.createErr
}

// Update 返回测试替身预置的更新结果。
func (s *deliveryTemplatePortStub) Update(context.Context, int64, int64, deliveryapp.Draft) error {
	return s.updateErr
}

// Delete 返回测试替身预置的删除结果。
func (s *deliveryTemplatePortStub) Delete(context.Context, int64, int64) error {
	return s.deleteErr
}

// deliveryTemplateRequestWithSession 构造带管理员会话和路径参数的模板请求。
func deliveryTemplateRequestWithSession(method, path, body string) *http.Request {
	// request 保存本次 handler 测试使用的 HTTP 请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	// ctx 保存路径参数和认证会话注入后的请求上下文。
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("template_id", strings.TrimPrefix(path, "/templates/"))
	// ctx 保存路径参数和认证会话注入后的请求上下文。
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	ctx = auth.WithSession(ctx, &db.Session{UserID: 7, IsAdmin: true})
	return request.WithContext(ctx)
}

// TestDeliveryTemplateHandlerHelpers 验证模板路径、请求 DTO 和响应模型转换的边界行为。
func TestDeliveryTemplateHandlerHelpers(t *testing.T) {
	// invalidRequest 是缺少模板路径参数的请求。
	invalidRequest := httptest.NewRequest(http.MethodGet, "/templates/not-a-number", nil)
	// invalidRouteContext 保存非法路径参数。
	invalidRouteContext := chi.NewRouteContext()
	invalidRouteContext.URLParams.Add("template_id", "not-a-number")
	invalidRequest = invalidRequest.WithContext(context.WithValue(invalidRequest.Context(), chi.RouteCtxKey, invalidRouteContext))
	// invalidID、invalidErr 保存非法路径参数解析结果。
	invalidID, invalidErr := parseDeliveryTemplateID(invalidRequest)
	if invalidID != 0 || invalidErr == nil {
		t.Fatalf("invalid template id=%d err=%v", invalidID, invalidErr)
	}
	// validRequest 是带正数路径参数的请求。
	validRequest := httptest.NewRequest(http.MethodGet, "/templates/12", nil)
	// validRouteContext 保存合法路径参数。
	validRouteContext := chi.NewRouteContext()
	validRouteContext.URLParams.Add("template_id", "12")
	validRequest = validRequest.WithContext(context.WithValue(validRequest.Context(), chi.RouteCtxKey, validRouteContext))
	// validID、validErr 保存合法路径参数解析结果。
	validID, validErr := parseDeliveryTemplateID(validRequest)
	if validErr != nil || validID != 12 {
		t.Fatalf("valid template id=%d err=%v", validID, validErr)
	}
	// defaultDraft、defaultErr 保存 enabled 缺省时的请求转换结果。
	defaultDraft, defaultErr := decodeDeliveryTemplateDraft(httptest.NewRequest(http.MethodPost, "/templates", strings.NewReader(`{"name":" 模板 ","messages":[{"content":"正文"}]}`)))
	if defaultErr != nil || !defaultDraft.Enabled || defaultDraft.Name != "模板" || len(defaultDraft.Messages) != 1 {
		t.Fatalf("default draft=%+v err=%v", defaultDraft, defaultErr)
	}
	// explicitDraft、explicitErr 保存显式关闭 enabled 的请求转换结果。
	explicitDraft, explicitErr := decodeDeliveryTemplateDraft(httptest.NewRequest(http.MethodPost, "/templates", strings.NewReader(`{"name":"模板","enabled":false,"messages":[]}`)))
	if explicitErr != nil || explicitDraft.Enabled {
		t.Fatalf("explicit draft=%+v err=%v", explicitDraft, explicitErr)
	}
	// _, decodeErr 保存非法 JSON 请求的解码错误。
	_, decodeErr := decodeDeliveryTemplateDraft(httptest.NewRequest(http.MethodPost, "/templates", strings.NewReader("{")))
	if decodeErr == nil {
		t.Fatal("invalid JSON should fail")
	}
	// response 保存待转换的应用模板模型。
	response := deliveryTemplateResponseModel(deliveryapp.Template{ID: 12, Name: "模板", Enabled: true, Messages: []deliveryapp.Message{{ID: 1, SortOrder: 1, Content: "正文"}}, Keys: []string{"main"}, CustomKeys: []string{"remark"}})
	if response.ID != 12 || len(response.Messages) != 1 || response.Keys[0] != "main" || response.CustomKeys[0] != "remark" {
		t.Fatalf("response=%+v", response)
	}
}

// TestDeliveryTemplateHandlerErrorMapping 验证模板 handler 将应用错误映射为稳定 HTTP 状态。
func TestDeliveryTemplateHandlerErrorMapping(t *testing.T) {
	// cases 描述不同 handler、应用错误和预期 HTTP 状态。
	cases := []struct {
		// name 是子测试名称。
		name string
		// invoke 调用当前场景的 handler。
		invoke func(*Server, http.ResponseWriter, *http.Request)
		// stub 构造当前场景的应用端口替身。
		stub func() *deliveryTemplatePortStub
		// wantStatus 是预期的 HTTP 状态码。
		wantStatus int
	}{
		{name: "list internal", invoke: (*Server).listDeliveryTemplates, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{listErr: errors.New("db")} }, wantStatus: http.StatusInternalServerError},
		{name: "get not found", invoke: (*Server).getDeliveryTemplate, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{getErr: deliveryapp.ErrNotFound} }, wantStatus: http.StatusNotFound},
		{name: "get internal", invoke: (*Server).getDeliveryTemplate, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{getErr: errors.New("db")} }, wantStatus: http.StatusInternalServerError},
		{name: "create invalid", invoke: (*Server).createDeliveryTemplate, stub: func() *deliveryTemplatePortStub {
			return &deliveryTemplatePortStub{createErr: deliveryapp.ErrInvalidInput}
		}, wantStatus: http.StatusBadRequest},
		{name: "create internal", invoke: (*Server).createDeliveryTemplate, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{createErr: errors.New("db")} }, wantStatus: http.StatusInternalServerError},
		{name: "update not found", invoke: (*Server).updateDeliveryTemplate, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{updateErr: deliveryapp.ErrNotFound} }, wantStatus: http.StatusNotFound},
		{name: "update conflict", invoke: (*Server).updateDeliveryTemplate, stub: func() *deliveryTemplatePortStub {
			return &deliveryTemplatePortStub{updateErr: deliveryapp.ErrVariableConflict}
		}, wantStatus: http.StatusConflict},
		{name: "update invalid", invoke: (*Server).updateDeliveryTemplate, stub: func() *deliveryTemplatePortStub {
			return &deliveryTemplatePortStub{updateErr: deliveryapp.ErrInvalidInput}
		}, wantStatus: http.StatusBadRequest},
		{name: "update internal", invoke: (*Server).updateDeliveryTemplate, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{updateErr: errors.New("db")} }, wantStatus: http.StatusInternalServerError},
		{name: "delete not found", invoke: (*Server).deleteDeliveryTemplate, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{deleteErr: deliveryapp.ErrNotFound} }, wantStatus: http.StatusNotFound},
		{name: "delete conflict", invoke: (*Server).deleteDeliveryTemplate, stub: func() *deliveryTemplatePortStub {
			return &deliveryTemplatePortStub{deleteErr: deliveryapp.ErrReferenced}
		}, wantStatus: http.StatusConflict},
		{name: "delete internal", invoke: (*Server).deleteDeliveryTemplate, stub: func() *deliveryTemplatePortStub { return &deliveryTemplatePortStub{deleteErr: errors.New("db")} }, wantStatus: http.StatusInternalServerError},
	}
	for /* item 表示当前模板 handler 错误映射场景。 */ _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// server 保存当前场景注入应用端口后的 HTTP 服务。
			server := &Server{applications: &ApplicationPorts{deliveryTemplates: item.stub()}}
			// body 是创建和更新接口使用的合法请求体。
			body := `{"name":"模板","messages":[{"content":"正文"}]}`
			// requestPath 保存当前场景对应的路径。
			requestPath := "/templates/1"
			if strings.HasPrefix(item.name, "create") {
				requestPath = "/templates"
			}
			// request 是包含认证上下文的测试请求。
			request := deliveryTemplateRequestWithSession(http.MethodPost, requestPath, body)
			if strings.HasPrefix(item.name, "list") || strings.HasPrefix(item.name, "get") {
				request = deliveryTemplateRequestWithSession(http.MethodGet, requestPath, "")
			}
			// recorder 保存当前 handler 写入的 HTTP 响应。
			recorder := httptest.NewRecorder()
			item.invoke(server, recorder, request)
			if recorder.Code != item.wantStatus {
				t.Fatalf("status=%d want %d body=%s", recorder.Code, item.wantStatus, recorder.Body.String())
			}
		})
	}
}

// TestDeliveryTemplateHandlerRejectsMalformedPathAndBody 验证 handler 在进入应用层前拒绝路径和 JSON 格式错误。
func TestDeliveryTemplateHandlerRejectsMalformedPathAndBody(t *testing.T) {
	// server 保存不应被调用的空应用服务端口。
	server := &Server{applications: &ApplicationPorts{deliveryTemplates: &deliveryTemplatePortStub{}}}
	// request 是带非法路径参数的更新请求。
	request := deliveryTemplateRequestWithSession(http.MethodPut, "/templates/not-number", `{"name":"模板","messages":[]}`)
	// recorder 保存非法路径响应。
	recorder := httptest.NewRecorder()
	server.updateDeliveryTemplate(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid path status=%d", recorder.Code)
	}
	// malformedRequest 是带合法路径但非法 JSON 的创建请求。
	malformedRequest := deliveryTemplateRequestWithSession(http.MethodPost, "/templates", "{")
	// malformedRecorder 保存非法 JSON 响应。
	malformedRecorder := httptest.NewRecorder()
	server.createDeliveryTemplate(malformedRecorder, malformedRequest)
	if malformedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid body status=%d", malformedRecorder.Code)
	}
	// malformedUpdateRequest 是带合法路径但非法 JSON 的更新请求。
	malformedUpdateRequest := deliveryTemplateRequestWithSession(http.MethodPut, "/templates/1", "{")
	// malformedUpdateRecorder 保存更新接口非法 JSON 响应。
	malformedUpdateRecorder := httptest.NewRecorder()
	server.updateDeliveryTemplate(malformedUpdateRecorder, malformedUpdateRequest)
	if malformedUpdateRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid update body status=%d", malformedUpdateRecorder.Code)
	}
}

var _ DeliveryTemplatesPort = (*deliveryTemplatePortStub)(nil)
