package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/application/apikey"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// itemBatchManagementPortStub 是批量发布 API 契约测试使用的现有批次管理应用端口替身。
type itemBatchManagementPortStub struct {
	// startUserID 和 startBatchID 记录批次启动时的用户归属和预检批次 ID。
	startUserID  int64
	startBatchID string
	// batchID 是测试使用的已启动批次标识。
	batchID string
}

// StartBatch 记录启动请求并返回测试批次标识，不启动真实 worker。
func (port *itemBatchManagementPortStub) StartBatch(_ context.Context, userID int64, batchID string, _ time.Duration) (string, error) {
	port.startUserID, port.startBatchID = userID, batchID
	return port.batchID, nil
}

// ListBatches 返回当前用户拥有的测试批次摘要。
func (port *itemBatchManagementPortStub) ListBatches(_ context.Context, userID int64, _ int) ([]itemapp.BatchInfo, error) {
	return []itemapp.BatchInfo{{ID: port.batchID, UserID: userID, Status: "running", Filename: "products.csv", TotalCount: 1, CreatedAt: "2026-11-16T00:00:00Z", UpdatedAt: "2026-11-16T00:00:00Z"}}, nil
}

// GetBatch 返回当前用户拥有的测试批次详情。
func (port *itemBatchManagementPortStub) GetBatch(_ context.Context, userID int64, batchID string) (itemapp.BatchDetails, error) {
	return itemapp.BatchDetails{Batch: itemapp.BatchInfo{ID: batchID, UserID: userID, Status: "running", Filename: "products.csv", TotalCount: 1, CreatedAt: "2026-11-16T00:00:00Z", UpdatedAt: "2026-11-16T00:00:00Z"}, Rows: []itemapp.BatchRow{}}, nil
}

// CancelBatch 返回测试批次的取消状态。
func (port *itemBatchManagementPortStub) CancelBatch(_ context.Context, _ int64, _ string) (string, error) {
	return "cancelled", nil
}

// DeleteBatch 实现测试批次删除端口；本测试不触发删除。
func (port *itemBatchManagementPortStub) DeleteBatch(context.Context, int64, string) error {
	return nil
}

// CleanupExpiredUploads 实现上传目录清理端口；本测试不清理任何用户文件。
func (port *itemBatchManagementPortStub) CleanupExpiredUploads(context.Context, time.Time, int) error {
	return nil
}

// RetryFailedBatch 返回测试批次重试标识，不启动真实 worker。
func (port *itemBatchManagementPortStub) RetryFailedBatch(_ context.Context, _ int64, _ string, _ time.Duration) (string, error) {
	return port.batchID, nil
}

// TestItemPublishBatchAPIBearerCanStartReadCancelAndRetry 验证 items:publish/items:read 令牌复用现有批量发布管理用例。
func TestItemPublishBatchAPIBearerCanStartReadCancelAndRetry(t *testing.T) {
	// srv、store、cleanup 是隔离数据库和完整 HTTP 服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// owner 是当前测试令牌所属的用户。
	owner, err := store.Users.GetAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// token 是同时拥有批量发布和批次状态读取权限的外部 API 凭证。
	token := apikey.TokenPrefix + "item-batch-api-test-token"
	// now 是令牌签发时间。
	now := time.Now().Unix()
	// record 是仅授予批量发布管理和结果读取的令牌记录。
	record := db.APIAccessToken{ID: "item-batch-api-key", UserID: owner.ID, Name: "item batch API test", TokenHash: apikey.HashToken(token), Prefix: token[:len(apikey.TokenPrefix)+8], Scopes: []string{apikey.ScopeItemsPublish, apikey.ScopeItemsRead}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是将批量发布专用令牌摘要写入隔离数据库时的错误。
	if err := store.APIAccessTokens.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	// batchPort 是复用现有批次管理接口的测试替身，不会启动真实发布 worker。
	batchPort := &itemBatchManagementPortStub{batchID: "batch-api-1"}
	srv.applications.itemBatchManagement = batchPort
	// handler 是包含真实 Bearer 认证、scope 校验和批次路由的 Router。
	handler := srv.Router()
	// startRequest 是启动现有预检批次的外部 API 请求。
	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-batches", strings.NewReader(`{"preview_id":"preview-api-1"}`))
	startRequest.Header.Set("Authorization", "Bearer "+token)
	// startRecorder 捕获批次启动响应。
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startRequest)
	assertOpenAPISuccessResponse(t, startRequest, startRecorder)
	if batchPort.startUserID != owner.ID || batchPort.startBatchID != "preview-api-1" || !strings.Contains(startRecorder.Body.String(), `"batch_id":"batch-api-1"`) {
		t.Fatalf("batch start did not preserve user ownership and existing preview ID: port=%+v body=%s", batchPort, startRecorder.Body.String())
	}
	// listRequest 读取当前用户的批次状态列表。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items/publish-batches", nil)
	listRequest.Header.Set("Authorization", "Bearer "+token)
	// listRecorder 捕获批次状态列表。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	assertOpenAPISuccessResponse(t, listRequest, listRecorder)
	// detailRequest 读取指定批次的运行状态和明细。
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items/publish-batches/"+batchPort.batchID, nil)
	detailRequest.Header.Set("Authorization", "Bearer "+token)
	// detailRecorder 捕获批次详情。
	detailRecorder := httptest.NewRecorder()
	handler.ServeHTTP(detailRecorder, detailRequest)
	assertOpenAPISuccessResponse(t, detailRequest, detailRecorder)
	// cancelRequest 取消当前用户拥有的运行批次。
	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-batches/"+batchPort.batchID+"/cancel", nil)
	cancelRequest.Header.Set("Authorization", "Bearer "+token)
	// cancelRecorder 捕获取消结果。
	cancelRecorder := httptest.NewRecorder()
	handler.ServeHTTP(cancelRecorder, cancelRequest)
	assertOpenAPISuccessResponse(t, cancelRequest, cancelRecorder)
	// retryRequest 对失败明细发起现有重试操作。
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/items/publish-batches/"+batchPort.batchID+"/retry-failed", nil)
	retryRequest.Header.Set("Authorization", "Bearer "+token)
	// retryRecorder 捕获重试结果。
	retryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(retryRecorder, retryRequest)
	assertOpenAPISuccessResponse(t, retryRequest, retryRecorder)
	// response 是批次启动响应的最小 JSON 投影，确保没有意外泄露 worker token。
	var response map[string]any
	// err 是解析批次启动响应以检查是否意外包含内部凭证时的 JSON 错误。
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(startRecorder.Body.String(), "worker_token") || strings.Contains(startRecorder.Body.String(), "Cookie") {
		t.Fatalf("batch start response leaked worker token or Cookie material: %s", startRecorder.Body.String())
	}
}
