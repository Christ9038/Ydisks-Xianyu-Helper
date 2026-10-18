package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// deliveryImageContractRequest 使用真实 handler 和会话发送请求，校验状态与版本化 OpenAPI 响应，返回供字段断言的正文。
func deliveryImageContractRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	// request 是使用真实 Router 与认证中间件的图片配置请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	// recorder 保存实际 HTTP 响应和契约校验所需的响应头。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, recorder.Code, status, recorder.Body.String())
	}
	if strings.HasPrefix(path, "/api/v1/") {
		assertOpenAPIExpectedStatusResponse(t, request, recorder, status)
	}
	return recorder
}

// TestDeliveryImageHTTPContracts 验证图片卡与混合模板通过真实 HTTP、应用层和 SQLite 往返，覆盖旧客户端更新保护。
func TestDeliveryImageHTTPContracts(t *testing.T) {
	// srv、store、cleanup 是独立 HTTP 服务、SQLite 数据和资源清理入口。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是实际生产路由树，cookie 是登录接口生成的管理员会话。
	handler := srv.Router()
	// cookie 用于所有具备用户级所有权的测试请求。
	cookie := loginHelper(t, handler)
	// created 是本地图片卡创建的实际响应。
	created := deliveryImageContractRequest(t, handler, cookie, http.MethodPost, "/api/v1/cards", `{"name":"本地图","type":"image","image_path":"说明/图.png","enabled":true}`, http.StatusOK)
	// mutation 是创建响应的具名 DTO，标识随后用于详情与更新。
	var mutation mutationIDResponse
	if err := json.Unmarshal(created.Body.Bytes(), &mutation); err != nil { // err 是真实创建响应解码错误。
		t.Fatal(err)
	}
	// cardPath 是版本化卡券资源路径。
	cardPath := "/api/v1/cards/" + strconv.FormatInt(mutation.ID, 10)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, cardPath, `{"name":"旧客户端改名","type":"image","enabled":true}`, http.StatusOK)
	// cardReply 保存省略图片字段更新后的完整详情。
	cardReply := deliveryImageContractRequest(t, handler, cookie, http.MethodGet, cardPath, "", http.StatusOK)
	// card 是用于验证字段保留的具名 HTTP DTO。
	var card cardResponse
	if err := json.Unmarshal(cardReply.Body.Bytes(), &card); err != nil || card.ImagePath != "说明/图.png" || card.ImageURL != "" { // err 是本地图详情反序列化错误。
		t.Fatalf("省略更新丢失本地图: %+v err=%v", card, err)
	}
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, cardPath, `{"name":"冲突","type":"image","image_url":"https://example.test/a.png"}`, http.StatusBadRequest)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, cardPath, `{"name":"URL","type":"image","image_url":"https://example.test/a.png","image_path":""}`, http.StatusOK)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, cardPath, `{"name":"本地","type":"image","image_url":"","image_path":"b.png"}`, http.StatusOK)
	deliveryImageContractRequest(t, handler, cookie, http.MethodGet, "/api/v1/cards", "", http.StatusOK)
	deliveryImageContractRequest(t, handler, cookie, http.MethodGet, "/cards/"+strconv.FormatInt(mutation.ID, 10), "", http.StatusOK)
	// invalidBody 是不允许保存的卡券图片来源，所有分支都经真实 handler 拒绝。
	for _, invalidBody := range []string{
		`{"name":"图","type":"image","image_path":"../secret.png"}`,
		`{"name":"图","type":"image","image_path":"/absolute.png"}`,
		`{"name":"图","type":"image","image_path":"C:\\secret.png"}`,
		`{"name":"图","type":"image","image_path":"a.png","image_url":"https://example.test/a.png"}`,
	} {
		deliveryImageContractRequest(t, handler, cookie, http.MethodPost, "/api/v1/cards", invalidBody, http.StatusBadRequest)
	}
	// templateBody 含旧格式文本、URL 图片、本地图片，全部按原始顺序保存。
	templateBody := `{"name":"图文模板","messages":[{"content":"订单 {{order_id}}"},{"type":"image","image_url":"https://example.test/guide.png"},{"type":"image","image_path":"指南/图.png"}]}`
	// templateCreated 是模板创建真实响应。
	templateCreated := deliveryImageContractRequest(t, handler, cookie, http.MethodPost, "/api/v1/delivery-templates", templateBody, http.StatusOK)
	// templateMutation 保存模板新标识。
	var templateMutation deliveryTemplateMutationResponse
	if err := json.Unmarshal(templateCreated.Body.Bytes(), &templateMutation); err != nil { // err 是模板创建响应解码错误。
		t.Fatal(err)
	}
	// templatePath 是当前用户模板资源路径。
	templatePath := "/api/v1/delivery-templates/" + strconv.FormatInt(templateMutation.ID, 10)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, templatePath, `{"name":"旧客户端","messages":[{"content":"只看到正文"}]}`, http.StatusConflict)
	// templateReply 验证拒绝覆盖之后原有图片仍存在。
	templateReply := deliveryImageContractRequest(t, handler, cookie, http.MethodGet, templatePath, "", http.StatusOK)
	// template 是完整消息响应 DTO，不直接序列化数据库模型。
	var template deliveryTemplateResponse
	if err := json.Unmarshal(templateReply.Body.Bytes(), &template); err != nil || len(template.Messages) != 3 || template.Messages[1].Type != "image" || template.Messages[2].ImagePath != "指南/图.png" { // err 是模板详情解码错误。
		t.Fatalf("模板图片未保留: %+v err=%v", template, err)
	}
	deliveryImageContractRequest(t, handler, cookie, http.MethodGet, "/api/v1/delivery-templates", "", http.StatusOK)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, templatePath, `{"name":"明确删图","messages":[{"type":"text","content":"只发文字"}]}`, http.StatusOK)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, templatePath, `{"name":"只发图片","messages":[{"type":"image","image_path":"new.png"}]}`, http.StatusOK)
	// invalidBody 是非法模板消息类型、来源、正文或路径。
	for _, invalidBody := range []string{
		`{"name":"图","messages":[{"type":"image"}]}`,
		`{"name":"图","messages":[{"type":"image","image_path":"a.png","image_url":"https://example.test/a.png"}]}`,
		`{"name":"图","messages":[{"type":"image","image_path":"../a.png"}]}`,
		`{"name":"图","messages":[{"type":"image","image_path":"a.png","content":"不能有正文"}]}`,
		`{"name":"图","messages":[{"type":"text","content":"正文","image_path":"a.png"}]}`,
		`{"name":"图","messages":[{"type":"image","image_url":"https://example.test/{{order_id}}.png"}]}`,
	} {
		deliveryImageContractRequest(t, handler, cookie, http.MethodPost, "/api/v1/delivery-templates", invalidBody, http.StatusBadRequest)
	}
	// ctx 是本地用户隔离夹具的上下文。
	ctx := context.Background()
	if created, err := store.Users.Create(ctx, "image-other", "image-other@example.test", "pw"); err != nil || !created { // created、err 是另一个本地用户的创建结果。
		t.Fatalf("创建跨用户夹具: %v", err)
	}
	// owner、ownerErr 保存另一用户的非敏感标识。
	owner, ownerErr := store.Users.GetByUsername(ctx, "image-other")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// otherTemplate、otherErr 是不属于管理员的图片模板。
	otherTemplate, otherErr := store.DeliveryTemplates.Create(ctx, db.DeliveryTemplateInput{UserID: owner.ID, Name: "他人图片", MessageItems: []deliverytemplate.Message{{Type: "image", ImagePath: "private.png"}}})
	if otherErr != nil {
		t.Fatal(otherErr)
	}
	deliveryImageContractRequest(t, handler, cookie, http.MethodGet, "/api/v1/delivery-templates/"+strconv.FormatInt(otherTemplate, 10), "", http.StatusNotFound)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, "/api/v1/delivery-templates/"+strconv.FormatInt(otherTemplate, 10), templateBody, http.StatusNotFound)
	// otherCard、otherCardErr 是另一用户拥有的本地图卡券。
	otherCard, otherCardErr := store.Cards.Create(ctx, &db.CardFull{UserID: owner.ID, Name: "他人卡券", Type: "image", ImagePath: "private.png"})
	if otherCardErr != nil {
		t.Fatal(otherCardErr)
	}
	deliveryImageContractRequest(t, handler, cookie, http.MethodGet, "/api/v1/cards/"+strconv.FormatInt(otherCard, 10), "", http.StatusForbidden)
	deliveryImageContractRequest(t, handler, cookie, http.MethodPut, "/api/v1/cards/"+strconv.FormatInt(otherCard, 10), `{"name":"篡改","type":"image","image_path":"other.png"}`, http.StatusForbidden)
}
