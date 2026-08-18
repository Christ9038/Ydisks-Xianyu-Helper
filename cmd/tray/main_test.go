package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWaitForServiceRequiresHealthyResponse 封装TestWaitForServiceRequiresHealthy响应业务协调。
func TestWaitForServiceRequiresHealthyResponse(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"degraded","database":"error"}`))
	}))
	t.Cleanup(server.Close)
	// originalURL 用于本次流程后续判断的originalURL
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	// client 用于本次流程后续判断的client
	client := server.Client()
	if // err 用于本次流程后续判断的err
	err := waitForService(client, true, 20*time.Millisecond); err == nil {
		t.Fatal("degraded health response must not count as running")
	}
}

// TestWaitForServiceAcceptsHealthyResponse 封装TestWaitForServiceAcceptsHealthy响应业务协调。
func TestWaitForServiceAcceptsHealthyResponse(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","database":"ok"}`))
	}))
	t.Cleanup(server.Close)
	// originalURL 用于本次流程后续判断的originalURL
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	if // err 用于本次流程后续判断的err
	err := waitForService(server.Client(), true, time.Second); err != nil {
		t.Fatalf("healthy response should count as running: %v", err)
	}
}

// TestWaitForServiceDoesNotTreatUnhealthyResponseAsStopped 封装TestWaitForServiceDoesNotTreatUnhealthy响应AsStopped业务协调。
func TestWaitForServiceDoesNotTreatUnhealthyResponseAsStopped(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	// originalURL 用于本次流程后续判断的originalURL
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	if // err 用于本次流程后续判断的err
	err := waitForService(server.Client(), false, 20*time.Millisecond); err == nil {
		t.Fatal("reachable unhealthy service must not count as stopped")
	}
}

// TestWaitForServiceAcceptsUnreachableAsStopped 封装TestWaitForServiceAcceptsUnreachableAsStopped业务协调。
func TestWaitForServiceAcceptsUnreachableAsStopped(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	// client 用于本次流程后续判断的client
	client := server.Client()
	// originalURL 用于本次流程后续判断的originalURL
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })
	server.Close()

	if // err 用于本次流程后续判断的err
	err := waitForService(client, false, time.Second); err != nil {
		t.Fatalf("unreachable service should count as stopped: %v", err)
	}
}
