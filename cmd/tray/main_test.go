package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWaitForServiceRequiresHealthyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"degraded","database":"error"}`))
	}))
	t.Cleanup(server.Close)
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	client := server.Client()
	if err := waitForService(client, true, 20*time.Millisecond); err == nil {
		t.Fatal("degraded health response must not count as running")
	}
}

func TestWaitForServiceAcceptsHealthyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","database":"ok"}`))
	}))
	t.Cleanup(server.Close)
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	if err := waitForService(server.Client(), true, time.Second); err != nil {
		t.Fatalf("healthy response should count as running: %v", err)
	}
}

func TestWaitForServiceDoesNotTreatUnhealthyResponseAsStopped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })

	if err := waitForService(server.Client(), false, 20*time.Millisecond); err == nil {
		t.Fatal("reachable unhealthy service must not count as stopped")
	}
}

func TestWaitForServiceAcceptsUnreachableAsStopped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := server.Client()
	originalURL := serviceURL
	serviceURL = server.URL
	t.Cleanup(func() { serviceURL = originalURL })
	server.Close()

	if err := waitForService(client, false, time.Second); err != nil {
		t.Fatalf("unreachable service should count as stopped: %v", err)
	}
}
