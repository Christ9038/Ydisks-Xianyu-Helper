package adapter

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// TestAPIDeliveryErrorTypes 验证 API 发货错误类型保留稳定文本和错误链。
func TestAPIDeliveryErrorTypes(t *testing.T) {
	// cause 保存网络错误类型包裹的底层错误。
	cause := errors.New("network cause")
	// retryable 保存可重试网络错误。
	retryable := &apiDeliveryRetryableError{kind: "network", err: cause}
	if retryable.Error() == "" || !errors.Is(retryable, cause) || !shouldRetryAPIDelivery(retryable, 0, 3) {
		t.Fatalf("可重试 API 错误异常 text=%q err=%v", retryable.Error(), retryable)
	}
	// timeout 保存带上下文超时原因的 API 错误。
	timeout := &apiDeliveryRetryableError{kind: "other", err: context.DeadlineExceeded}
	if !shouldRetryAPIDelivery(timeout, 0, 3) || shouldRetryAPIDelivery(timeout, 3, 3) {
		t.Fatal("超时或重试次数边界判断异常")
	}
	// httpErr 保存远端暂时性 HTTP 错误。
	httpErr := &apiDeliveryHTTPError{statusCode: http.StatusTooManyRequests}
	if httpErr.Error() == "" || !shouldRetryAPIDelivery(httpErr, 0, 3) {
		t.Fatalf("HTTP API 错误异常=%v", httpErr)
	}
	// permanentErr 保存不可重试的客户端 HTTP 错误。
	permanentErr := &apiDeliveryHTTPError{statusCode: http.StatusBadRequest}
	if shouldRetryAPIDelivery(permanentErr, 0, 3) || shouldRetryAPIDelivery(nil, 0, 3) {
		t.Fatal("不可重试 API 错误判断异常")
	}
	// unwrapped 保存 retryable 错误的原始错误。
	unwrapped := retryable.Unwrap()
	if unwrapped != cause {
		t.Fatalf("错误链未保留底层错误=%v", unwrapped)
	}
}

// TestItemSyncCookieNotificationGuard 验证商品同步 Cookie 通知只转发有效更新。
func TestItemSyncCookieNotificationGuard(t *testing.T) {
	// ctx 是本测试 Cookie 通知共用的上下文。
	ctx := context.Background()
	// updates 保存有效 Cookie 更新的转发记录。
	updates := make([]string, 0, 1)
	// repository 保存注入通知回调的商品同步适配器。
	repository := &ItemSyncRepository{updateRunningCookie: func(_ context.Context, accountID, value string) {
		updates = append(updates, accountID+":"+value)
	}}
	repository.notifyRunningCookie(ctx, "cid", "cookie")
	repository.notifyRunningCookie(ctx, "cid", "")
	if len(updates) != 1 || updates[0] != "cid:cookie" {
		t.Fatalf("Cookie 通知 guard 异常=%v", updates)
	}
	// noCallbackRepository 保存没有 Cookie 更新回调的同步适配器。
	noCallbackRepository := &ItemSyncRepository{}
	noCallbackRepository.notifyRunningCookie(ctx, "cid", "cookie")
}
