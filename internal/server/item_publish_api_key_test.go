package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"xianyu-go/internal/application/apikey"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// itemSinglePublishPortStub 是外部 API 商品发布契约测试使用的单商品发布应用端口替身。
type itemSinglePublishPortStub struct {
	// input 保存 HTTP Handler 转换后的商品发布业务输入。
	input itemapp.PublishInput
}

// PublishSingle 记录发布输入并返回本地模拟的平台成功结果，不调用闲鱼真实接口。
func (port *itemSinglePublishPortStub) PublishSingle(_ context.Context, input itemapp.PublishInput) (itemapp.PublishOutcome, error) {
	port.input = input
	return itemapp.PublishOutcome{Result: &itemapp.PublishResult{ItemID: "item-published-1", ItemURL: "https://www.goofish.com/item?id=item-published-1", Title: input.Title, PriceText: "9.90", Quantity: input.Quantity, ImageURL: "https://example.invalid/item.png"}}, nil
}

// TestItemPublishUsesBearerScopeAndExistingPublishUseCase 验证 items:publish 令牌可调用现有商品发布用例，不建立第二套发布链路。
func TestItemPublishUsesBearerScopeAndExistingPublishUseCase(t *testing.T) {
	// srv、store、cleanup 是隔离数据库和完整 HTTP 服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// owner 是当前测试令牌所属的用户。
	owner, err := store.Users.GetAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// token 是只授予 items:publish 的外部 API Bearer 凭证。
	token := apikey.TokenPrefix + "item-publish-api-test-token"
	// now 是令牌签发时间。
	now := time.Now().Unix()
	// record 是只授予商品发布权限的令牌记录。
	record := db.APIAccessToken{ID: "item-publish-api-key", UserID: owner.ID, Name: "item publish API test", TokenHash: apikey.HashToken(token), Prefix: token[:len(apikey.TokenPrefix)+8], Scopes: []string{apikey.ScopeItemsPublish}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是将商品发布专用令牌摘要写入隔离数据库时的错误。
	if err := store.APIAccessTokens.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	// publishPort 记录输入并返回模拟平台结果，确保测试不触发真实闲鱼发布。
	publishPort := &itemSinglePublishPortStub{}
	srv.applications.itemSinglePublish = publishPort
	// handler 是包含 Bearer 认证、scope 校验和商品发布路由的完整 Router。
	handler := srv.Router()
	// body 是待发送的 multipart 商品发布请求体。
	var body bytes.Buffer
	// writer 负责写入商品字段和一张最小 PNG 测试图片。
	writer := multipart.NewWriter(&body)
	// fields 保存当前发布请求要求的商品字段和值。
	fields := map[string]string{
		"cookie_id":      "acc1",
		"title":          "API 测试商品",
		"description":    "API scope contract test",
		"price":          "9.90",
		"original_price": "12.00",
		"quantity":       "1",
		"postage_mode":   "free",
		"postage":        "0",
	}
	// key、value 是当前写入 multipart 表单的一项商品字段名称和值。
	for key, value := range fields {
		// err 是将当前商品字段写入 multipart 请求体时的编码错误。
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	// image 是用于验证现有 multipart 图片解析器的 1×1 PNG 数据。
	// image、err 保存测试 PNG 的字节内容和 Base64 解码错误。
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	// imageHeader 明确声明图片文件 MIME，匹配现有上传接口的文件类型校验。
	imageHeader := make(textproto.MIMEHeader)
	imageHeader.Set("Content-Disposition", `form-data; name="images"; filename="one-pixel.png"`)
	imageHeader.Set("Content-Type", "image/png")
	// imagePart 是现有发布接口要求的商品图片字段。
	imagePart, err := writer.CreatePart(imageHeader)
	if err != nil {
		t.Fatal(err)
	}
	// err 是把最小 PNG 测试数据写入 multipart 文件字段时的写入错误。
	if _, err := imagePart.Write(image); err != nil {
		t.Fatal(err)
	}
	// err 是关闭 multipart writer 并完成请求 boundary 时的错误。
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	// request 是只携带 items:publish 权限的外部 API 请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	// recorder 捕获现有发布 Handler 的 HTTP 响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPISuccessResponse(t, request, recorder)
	if publishPort.input.UserID != owner.ID || publishPort.input.CookieID != "acc1" || publishPort.input.Title != "API 测试商品" || publishPort.input.PriceCents != 990 || publishPort.input.Quantity != 1 || len(publishPort.input.Images) != 1 {
		t.Fatalf("existing publish use case did not receive validated user-scoped input: %+v", publishPort.input)
	}
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"item_id":"item-published-1"`)) {
		t.Fatalf("publish API response did not return item status: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
