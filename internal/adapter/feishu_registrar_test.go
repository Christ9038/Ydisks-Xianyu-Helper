package adapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/feishu"
)

// feishuTestTransport 把所有飞书协议请求重定向到本地 httptest 服务器，保留原路径与查询。
type feishuTestTransport struct {
	// target 是本地测试服务器的 host:port。
	target string
}

// RoundTrip 将请求 scheme 和 host 替换为测试服务器，避免访问真实飞书域名。
// req 是待重定向的飞书协议请求；返回值来自默认传输。
func (t feishuTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// cloned 是保留原请求头和查询参数的重定向副本。
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = t.target
	cloned.Host = t.target
	return http.DefaultTransport.RoundTrip(cloned)
}

// TestNotificationChannelRepositoryListOwnedAccountIDs 验证账号列表只返回自有账号标识。
func TestNotificationChannelRepositoryListOwnedAccountIDs(t *testing.T) {
	// store 是带管理员和测试账号的临时 SQLite 数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库操作的上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 是通知渠道应用端口适配器。
	repository := NewNotificationChannelRepository(store)
	// accountIDs、listErr 保存自有账号标识及查询错误。
	accountIDs, listErr := repository.ListOwnedAccountIDs(ctx, owner.ID)
	if listErr != nil {
		t.Fatalf("ListOwnedAccountIDs: %v", listErr)
	}
	if len(accountIDs) != 1 || accountIDs[0] != "cid" {
		t.Fatalf("账号标识=%v", accountIDs)
	}
	// unknownIDs、unknownErr 保存不存在用户的账号列表，必须为空而不是泄漏其他用户账号。
	unknownIDs, unknownErr := repository.ListOwnedAccountIDs(ctx, owner.ID+1000)
	if unknownErr != nil || len(unknownIDs) != 0 {
		t.Fatalf("未知用户账号=%v err=%v", unknownIDs, unknownErr)
	}
}

// TestFeishuRegistrarMapsProtocolResults 验证适配器把协议包结果映射为应用层 plain 类型。
func TestFeishuRegistrarMapsProtocolResults(t *testing.T) {
	// server 模拟飞书设备码 begin 与成功 poll。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// parseErr 表示表单解析失败原因；action 是当前注册阶段。
		if parseErr := r.ParseForm(); parseErr != nil {
			t.Errorf("解析表单失败: %v", parseErr)
			return
		}
		// action 是当前设备码注册阶段。
		action := r.PostForm.Get("action")
		switch action {
		case "begin":
			_, _ = io.WriteString(w, `{"device_code":"device-adapter","verification_uri_complete":"https://accounts.feishu.cn/qr?code=abc","expires_in":600,"interval":5}`)
		case "poll":
			_, _ = io.WriteString(w, `{"client_id":"cli-adapter","client_secret":"secret-adapter","user_info":{"open_id":"ou-adapter","tenant_brand":"lark"}}`)
		default:
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	// target 是测试服务器的 host:port。
	target := server.URL[len("http://"):]
	// client 是重定向到本地服务器的协议客户端。
	client := feishu.NewClient(&http.Client{Transport: feishuTestTransport{target: target}})
	// registrar 是待验证的适配器。
	registrar := newFeishuRegistrarWithClient(client)
	// deviceCode、beginErr 保存适配后的 begin 结果。
	deviceCode, beginErr := registrar.BeginRegistration(context.Background())
	if beginErr != nil {
		t.Fatalf("BeginRegistration: %v", beginErr)
	}
	if deviceCode.DeviceCode != "device-adapter" || deviceCode.QRCodeURL == "" || deviceCode.ExpiresIn <= 0 || deviceCode.Interval <= 0 {
		t.Fatalf("适配结果异常: %+v", deviceCode)
	}
	// pollResult、pollErr 保存适配后的成功轮询结果。
	pollResult, pollErr := registrar.PollRegistration(context.Background(), notificationsapp.FeishuPollRequest{Domain: "feishu", DeviceCode: deviceCode.DeviceCode})
	if pollErr != nil {
		t.Fatalf("PollRegistration: %v", pollErr)
	}
	if pollResult.Status != notificationsapp.FeishuPollSuccess || pollResult.Domain != "lark" || pollResult.AppID != "cli-adapter" || pollResult.AppSecret != "secret-adapter" || pollResult.OpenID != "ou-adapter" {
		t.Fatalf("轮询适配结果异常: %+v", pollResult)
	}
}

// TestFeishuRegistrarReturnsProtocolErrorsAndRejectsNilClient 验证适配器透传协议错误并拒绝未初始化客户端。
func TestFeishuRegistrarReturnsProtocolErrorsAndRejectsNilClient(t *testing.T) {
	// server 始终返回服务端错误，模拟协议失败。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	// target 是测试服务器的 host:port。
	target := server.URL[len("http://"):]
	// client 是重定向到失败服务的协议客户端。
	client := feishu.NewClient(&http.Client{Transport: feishuTestTransport{target: target}})
	// registrar 是待验证的适配器。
	registrar := newFeishuRegistrarWithClient(client)
	// beginErr 是协议失败时适配器返回的 begin 错误。
	if _, beginErr := registrar.BeginRegistration(context.Background()); beginErr == nil {
		t.Fatal("协议失败应返回 begin 错误")
	}
	// pollErr 是协议失败时适配器返回的 poll 错误。
	if _, pollErr := registrar.PollRegistration(context.Background(), notificationsapp.FeishuPollRequest{Domain: "feishu", DeviceCode: "device"}); pollErr == nil {
		t.Fatal("协议失败应返回 poll 错误")
	}
	// nilRegistrar 是没有协议客户端的适配器。
	nilRegistrar := FeishuRegistrar{}
	// beginErr 是 nil 客户端返回的 begin 错误。
	if _, beginErr := nilRegistrar.BeginRegistration(context.Background()); beginErr == nil {
		t.Fatal("nil 客户端应返回 begin 错误")
	}
	// pollErr 是 nil 客户端返回的 poll 错误。
	if _, pollErr := nilRegistrar.PollRegistration(context.Background(), notificationsapp.FeishuPollRequest{}); pollErr == nil {
		t.Fatal("nil 客户端应返回 poll 错误")
	}
	// productionRegistrar 是生产构造结果，必须非空且可赋值给应用端口。
	productionRegistrar := NewFeishuRegistrar()
	if productionRegistrar == nil {
		t.Fatal("生产飞书注册适配器不能为空")
	}
	// interfaceAssertion 验证生产适配器满足消费者接口。
	var interfaceAssertion notificationsapp.FeishuRegistrar = productionRegistrar
	if interfaceAssertion == nil {
		t.Fatal("生产适配器未满足应用端口")
	}
}

// TestNotificationChannelRepositoryListOwnedAccountIDsClosedStore 验证数据库关闭后账号列表返回基础设施错误。
func TestNotificationChannelRepositoryListOwnedAccountIDsClosedStore(t *testing.T) {
	// store 是随后关闭连接的临时 SQLite 数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试存储的通知渠道适配器。
	repository := NewNotificationChannelRepository(store)
	// closeErr 保存关闭数据库的错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// _, listErr 保存数据库关闭后的账号列表错误。
	if _, listErr := repository.ListOwnedAccountIDs(context.Background(), 1); listErr == nil {
		t.Fatal("数据库关闭后账号列表应返回错误")
	}
	// invalidErr 保存非法用户 ID 的校验错误；该错误不能被适配器映射为通知业务错误。
	_, invalidErr := repository.ListOwnedAccountIDs(context.Background(), 0)
	if invalidErr == nil {
		t.Fatal("非法用户 ID 应返回错误")
	}
	if errors.Is(invalidErr, notificationsapp.ErrChannelNotFound) {
		t.Fatal("非法用户 ID 不应映射为渠道不存在")
	}
}
