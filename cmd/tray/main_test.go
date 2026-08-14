package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWaitForServiceRequiresHealthyResponse 负责TestWaitForServiceRequiresHealthy响应相关处理。
func TestWaitForServiceRequiresHealthyResponse(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"degraded","database":"error"}`))
	}))
	t.Cleanup(server.Close)
	// originalURL 保存originalURL，供当前处理流程使用
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	// client 保存client，供当前处理流程使用
	client := server.Client()
	if // err 保存err，供当前处理流程使用
	err := waitForService(client, true, 20*time.Millisecond); err == nil {
		t.Fatal("degraded health response must not count as running")
	}
}

// TestWaitForServiceAcceptsHealthyResponse 负责TestWaitForServiceAcceptsHealthy响应相关处理。
func TestWaitForServiceAcceptsHealthyResponse(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","database":"ok"}`))
	}))
	t.Cleanup(server.Close)
	// originalURL 保存originalURL，供当前处理流程使用
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	if // err 保存err，供当前处理流程使用
	err := waitForService(server.Client(), true, time.Second); err != nil {
		t.Fatalf("healthy response should count as running: %v", err)
	}
}

// TestWaitForServiceDoesNotTreatUnhealthyResponseAsStopped 负责TestWaitForServiceDoesNotTreatUnhealthy响应AsStopped相关处理。
func TestWaitForServiceDoesNotTreatUnhealthyResponseAsStopped(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	// originalURL 保存originalURL，供当前处理流程使用
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	if // err 保存err，供当前处理流程使用
	err := waitForService(server.Client(), false, 20*time.Millisecond); err == nil {
		t.Fatal("reachable unhealthy service must not count as stopped")
	}
}

// TestWaitForServiceAcceptsUnreachableAsStopped 负责TestWaitForServiceAcceptsUnreachableAsStopped相关处理。
func TestWaitForServiceAcceptsUnreachableAsStopped(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	// client 保存client，供当前处理流程使用
	client := server.Client()
	// originalURL 保存originalURL，供当前处理流程使用
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })
	server.Close()

	if // err 保存err，供当前处理流程使用
	err := waitForService(client, false, time.Second); err != nil {
		t.Fatalf("unreachable service should count as stopped: %v", err)
	}
}
