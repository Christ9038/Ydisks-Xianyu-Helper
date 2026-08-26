package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	accountapp "xianyu-go/internal/application/account"
)

// TestWriteLongLoginErrorMapsApplicationFailures 验证长登录应用错误映射为稳定的 HTTP 状态。
func TestWriteLongLoginErrorMapsApplicationFailures(t *testing.T) {
	// cases 保存长登录错误到 HTTP 状态的映射样本。
	cases := []struct {
		// name 是当前错误映射样本名称。
		name string
		// err 是待映射的应用层错误。
		err error
		// status 是预期返回的 HTTP 状态。
		status int
	}{
		{name: "forbidden", err: accountapp.ErrForbidden, status: http.StatusNotFound},
		{name: "not found", err: accountapp.ErrNotFound, status: http.StatusNotFound},
		{name: "credential missing", err: accountapp.ErrCredentialNotFound, status: http.StatusNotFound},
		{name: "platform", err: accountapp.ErrLongLoginPlatform, status: http.StatusBadGateway},
		{name: "canceled", err: context.Canceled, status: http.StatusBadGateway},
		{name: "deadline", err: context.DeadlineExceeded, status: http.StatusBadGateway},
		{name: "http text", err: errors.New("HTTP status 502"), status: http.StatusBadGateway},
		{name: "platform text", err: errors.New("平台响应无效"), status: http.StatusBadGateway},
		{name: "internal", err: errors.New("数据库写入失败"), status: http.StatusInternalServerError},
	}
	// testCase 表示当前待验证的长登录错误映射样本。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// recorder 捕获错误映射写入的 HTTP 响应。
			recorder := httptest.NewRecorder()
			// server 是不依赖其他字段的错误映射服务实例。
			server := &Server{}
			server.writeLongLoginError(recorder, testCase.err)
			if recorder.Code != testCase.status {
				t.Fatalf("错误=%v status=%d want %d", testCase.err, recorder.Code, testCase.status)
			}
		})
	}
}
