package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// orderRefreshCountsPort 仅替换批量刷新用例，其余接口由嵌入端口满足且本测试不调用。
type orderRefreshCountsPort struct {
	// OrdersPort 保留不参与计数映射测试的其他订单能力。
	OrdersPort
}

// Refresh 忽略测试请求参数并返回已去重的应用统计，供 HTTP adapter 验证透传。
func (orderRefreshCountsPort) Refresh(context.Context, int64, string, string) (orderapp.RefreshResult, error) {
	return orderapp.RefreshResult{Message: "同步完成", Summary: orderapp.RefreshSummary{Restored: 3, Reassigned: 2}}, nil
}

// TestOrderRefreshCountsAdapter 验证同步 HTTP adapter 不丢失应用层恢复和修正数量；t 提供测试断言。
func TestOrderRefreshCountsAdapter(t *testing.T) {
	// adapter 使用固定统计用例，不访问平台或真实账号。
	adapter := &orderHTTPAdapter{services: orderRefreshCountsPort{}}
	// result、err 保存兼容同步入口的映射结果及调用错误。
	result, err := adapter.Refresh(context.Background(), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// encoded、encodeErr 将结果按客户端实际接收的 JSON 契约序列化。
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if !strings.Contains(string(encoded), `"restored":3`) || !strings.Contains(string(encoded), `"reassigned":2`) {
		t.Fatalf("HTTP adapter 丢失恢复修正统计：%s", encoded)
	}
}

// TestOrderRefreshJobCountsContract 用真实任务查询 handler 验证新旧持久化结果及失败明细；t 提供测试断言。
func TestOrderRefreshJobCountsContract(t *testing.T) {
	// srv、store、cleanup 分别管理隔离 HTTP 服务、SQLite 夹具和资源释放。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 提供带鉴权和任务查询入口的真实路由。
	handler := srv.Router()
	// cookie 仅用于本地测试请求认证。
	cookie := loginHelper(t, handler)
	// admin、err 获取持久化任务的授权用户。
	admin, err := store.Users.GetByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	// extra 分别覆盖旧任务缺字段和新任务包含非零统计的持久化形状。
	for _, extra := range []string{"", `,"restored":3,"reassigned":2`} {
		t.Run("counts"+extra, func(t *testing.T) { // t 承载当前新旧任务兼容场景的断言。
			// job 使用唯一标识保存已完成但存在逐项失败的本地任务。
			job := &db.OrderRefreshJob{ID: "counts-old", UserID: admin.ID, Status: "succeeded", ResultJSON: `{"partial_failure":true,"message":"同步未完成","summary":{"discovered":0,"list_updated":0,"soft_deleted":0,"detail_total":0,"total":0,"updated":0,"no_change":0,"failed":1` + extra + `},"results":[{"success":false,"cookie_id":"account-a","order_id":"order-a","error":"归属冲突","message":"核对卖家"}]}`}
			// 新旧任务使用独立标识，确保从实际持久化记录读取对应结果。
			if extra != "" {
				job.ID = "counts-new"
			}
			// createErr 是本地任务夹具的持久化错误。
			if createErr := store.OrderRefreshJobs.Create(context.Background(), job); createErr != nil {
				t.Fatal(createErr)
			}
			// request 使用实际任务 ID 查询授权用户可见结果。
			request := httptest.NewRequest(http.MethodGet, "/api/v1/orders/refresh/"+job.ID, nil)
			request.AddCookie(cookie)
			// recorder 捕获任务状态 handler 的真实响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertOpenAPISuccessResponse(t, request, recorder)
			// body 是已通过 OpenAPI 校验的真实 JSON 响应。
			body := recorder.Body.String()
			if extra != "" && (!strings.Contains(body, `"restored":3`) || !strings.Contains(body, `"reassigned":2`)) {
				t.Fatalf("持久化任务转换丢失统计：%s", body)
			}
			// expected 是失败结果必须保留的账号、订单和错误说明。
			for _, expected := range []string{"account-a", "order-a", "归属冲突", "核对卖家", `"partial_failure":true`} {
				if !strings.Contains(body, expected) {
					t.Fatalf("任务失败明细缺少 %s", expected)
				}
			}
		})
	}
}

// TestOrderRefreshCountsSchema 拒绝负数及非整数统计，同时保留旧任务省略字段的契约；t 提供断言。
func TestOrderRefreshCountsSchema(t *testing.T) {
	// document 读取真实 OpenAPI 单一契约源。
	document := loadOpenAPIContractForCoverage(t)
	// summary 是订单刷新计数的 schema，新增字段必须明确登记。
	summary := document.Components.Schemas["OrderRefreshSummary"].Value
	// field 分别表示同账号恢复和跨账号修正统计。
	for _, field := range []string{"restored", "reassigned"} {
		// property、exists 保存新统计 schema 及其登记状态。
		property, exists := summary.Properties[field]
		if !exists {
			t.Fatalf("订单 schema 缺少 %s", field)
		}
		if property.Value.VisitJSON(float64(-1)) == nil || property.Value.VisitJSON(1.5) == nil || property.Value.VisitJSON("1") == nil || property.Value.VisitJSON(float64(0)) != nil || property.Value.VisitJSON(float64(3)) != nil {
			t.Fatalf("%s 必须为非负整数", field)
		}
		// required 是旧任务必须具有的字段，新统计不能列为必填。
		for _, required := range summary.Required {
			if required == field {
				t.Fatalf("%s 必须兼容旧任务省略字段", field)
			}
		}
	}
}
