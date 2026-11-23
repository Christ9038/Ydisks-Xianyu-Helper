package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLogInternalFailureLevelsAndRedaction 验证状态码对应的日志等级、字段、caller 以及凭证脱敏。
func TestLogInternalFailureLevelsAndRedaction(t *testing.T) {
	// cases 覆盖 500/502/504 记 Error、503 与 4xx 记 Warn 的等级约定。
	cases := []struct {
		name   string
		status int
		want   string
	}{
		{"internal", http.StatusInternalServerError, "level=ERROR"},
		{"bad gateway", http.StatusBadGateway, "level=ERROR"},
		{"gateway timeout", http.StatusGatewayTimeout, "level=ERROR"},
		{"unavailable", http.StatusServiceUnavailable, "level=WARN"},
		{"client", http.StatusBadRequest, "level=WARN"},
	}
	// testCase 是当前状态码场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// buffer 是本场景捕获的日志输出。
			buffer := captureDefaultLogger(t)
			logInternalFailure(context.Background(), testCase.status, errors.New("db down cookie=SECRETVALUE"))
			// output 是写入的日志文本。
			output := buffer.String()
			if !strings.Contains(output, testCase.want) || !strings.Contains(output, "接口处理失败") || !strings.Contains(output, "db down") {
				t.Fatalf("日志字段不符: %s", output)
			}
			if strings.Contains(output, "SECRETVALUE") {
				t.Fatalf("凭证未脱敏: %s", output)
			}
			if !strings.Contains(output, "internal_error_test.go") {
				t.Fatalf("caller 应指向调用方: %s", output)
			}
		})
	}
}

// TestLogInternalFailureIgnoresNilError 验证没有底层错误时不产生日志。
func TestLogInternalFailureIgnoresNilError(t *testing.T) {
	// buffer 是捕获的日志输出。
	buffer := captureDefaultLogger(t)
	logInternalFailure(context.Background(), http.StatusInternalServerError, nil)
	if buffer.Len() != 0 {
		t.Fatalf("nil 错误不应记录日志: %s", buffer.String())
	}
}

// TestWriteInternalErrKeepsResponseAndLogsCause 验证两个封装写出与原函数一致的响应，且不泄露底层错误文本。
func TestWriteInternalErrKeepsResponseAndLogsCause(t *testing.T) {
	// writers 是两个封装及其对应的原始响应函数，用来比对响应一致。
	writers := []struct {
		name     string
		internal func(http.ResponseWriter, *http.Request)
		plain    func(http.ResponseWriter, *http.Request)
	}{
		{"writeInternalErr",
			func(w http.ResponseWriter, r *http.Request) {
				writeInternalErr(w, r, http.StatusInternalServerError, "保存失败", errors.New("SQLSTATE 23505"))
			},
			func(w http.ResponseWriter, r *http.Request) {
				writeErr(w, http.StatusInternalServerError, "保存失败")
			}},
		{"writeInternalErrRequest",
			func(w http.ResponseWriter, r *http.Request) {
				writeInternalErrRequest(w, r, http.StatusBadGateway, "上游失败", errors.New("SQLSTATE 23505"))
			},
			func(w http.ResponseWriter, r *http.Request) {
				writeErrRequest(w, r, http.StatusBadGateway, "上游失败")
			}},
	}
	// writer 是当前封装场景。
	for _, writer := range writers {
		t.Run(writer.name, func(t *testing.T) {
			// buffer 是捕获的日志输出。
			buffer := captureDefaultLogger(t)
			// request 是共用的测试请求。
			request := httptest.NewRequest(http.MethodGet, "/x", nil)
			// got、want 分别是封装与原函数写出的响应。
			got, want := httptest.NewRecorder(), httptest.NewRecorder()
			writer.internal(got, request)
			writer.plain(want, request)
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				t.Fatalf("响应不一致: %d %s vs %d %s", got.Code, got.Body.String(), want.Code, want.Body.String())
			}
			if strings.Contains(got.Body.String(), "SQLSTATE") {
				t.Fatalf("响应泄露了底层错误: %s", got.Body.String())
			}
			if !strings.Contains(buffer.String(), "internal_error_test.go") {
				t.Fatalf("caller 应指向调用 writeInternalErr 的业务位置: %s", buffer.String())
			}
			if !strings.Contains(buffer.String(), "SQLSTATE 23505") {
				t.Fatalf("日志应记录底层错误: %s", buffer.String())
			}
		})
	}
}
