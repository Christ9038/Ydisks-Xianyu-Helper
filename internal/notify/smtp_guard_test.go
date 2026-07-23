package notify

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/netguard"
)

func TestSendPublicSMTPRejectsInvalidPortAndLoopback(t *testing.T) {
	testDialer := dialPublicSMTP
	dialPublicSMTP = netguard.DialPublicContext
	t.Cleanup(func() { dialPublicSMTP = testDialer })
	if err := sendPublicSMTP(context.Background(), "smtp.example.com:not-a-port", "smtp.example.com", nil, "a@example.com", "b@example.com", nil); err == nil || !strings.Contains(err.Error(), "端口无效") {
		t.Fatalf("invalid port must be rejected, got %v", err)
	}
	if err := sendPublicSMTP(context.Background(), "127.0.0.1:25", "127.0.0.1", nil, "a@example.com", "b@example.com", nil); err == nil || !strings.Contains(err.Error(), "非公网") {
		t.Fatalf("loopback SMTP must be rejected, got %v", err)
	}
}

func TestSendPublicSMTPRequiresAdvertisedSTARTTLS(t *testing.T) {
	original := dialPublicSMTP
	dialPublicSMTP = func(context.Context, string, string, time.Duration) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			_, _ = server.Write([]byte("220 smtp.example.test ESMTP\r\n"))
			_, _ = reader.ReadString('\n')
			_, _ = server.Write([]byte("250 smtp.example.test\r\n"))
		}()
		return client, nil
	}
	t.Cleanup(func() { dialPublicSMTP = original })
	err := sendPublicSMTP(context.Background(), "smtp.example.test:587", "smtp.example.test", nil,
		"a@example.test", "b@example.test", nil, smtpTransportOptions{UseSTARTTLS: true})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("missing STARTTLS must fail, got %v", err)
	}
}
