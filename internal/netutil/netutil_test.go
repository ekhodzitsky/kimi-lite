package netutil

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIsBlockedHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"::", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true},
		{"100.127.255.255", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"fd12:3456::1", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:127.0.0.1", true},
		{"8.8.8.8", false},
		{"example.com", false},
		{"1.1.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			t.Parallel()
			if got := IsBlockedHost(tt.host); got != tt.want {
				t.Errorf("IsBlockedHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestSecureTransport_DialContext_ResolvesOnce(t *testing.T) {
	t.Parallel()

	var lookupCount atomic.Int32

	mockLookup := func(ctx context.Context, host string) ([]string, error) {
		lookupCount.Add(1)
		return []string{"93.184.216.34"}, nil
	}

	fakeDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		c1, _ := net.Pipe()
		return c1, nil
	}

	transport := secureTransport(mockLookup, fakeDial)
	conn, err := transport.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatalf("unexpected dial error: %v", err)
	}
	defer conn.Close()

	if lookupCount.Load() != 1 {
		t.Fatalf("expected 1 DNS lookup, got %d", lookupCount.Load())
	}
}

func TestSecureTransport_DialContext_DialsValidatedIP(t *testing.T) {
	t.Parallel()

	var dialedAddr string

	mockLookup := func(ctx context.Context, host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	}

	fakeDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialedAddr = addr
		c1, _ := net.Pipe()
		return c1, nil
	}

	transport := secureTransport(mockLookup, fakeDial)
	conn, err := transport.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatalf("unexpected dial error: %v", err)
	}
	defer conn.Close()

	if dialedAddr == "" {
		t.Fatal("expected dialedAddr to be set")
	}
	ip, _, err := net.SplitHostPort(dialedAddr)
	if err != nil {
		t.Fatalf("dialedAddr %q is not a valid host:port: %v", dialedAddr, err)
	}
	if net.ParseIP(ip) == nil {
		t.Fatalf("expected dialed address to be an IP, got %q", ip)
	}
	if ip != "93.184.216.34" {
		t.Fatalf("expected dialed IP 93.184.216.34, got %q", ip)
	}
}

func TestSecureHTTPClient_CheckRedirect_BlocksPrivateHost(t *testing.T) {
	t.Parallel()

	client := SecureHTTPClient()

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	err = client.CheckRedirect(req, []*http.Request{req})
	if err == nil {
		t.Fatal("expected redirect to blocked host to be rejected")
	}
	if !strings.Contains(err.Error(), "blocked host") {
		t.Fatalf("expected blocked host error, got: %v", err)
	}
}

func TestSecureHTTPClient_CheckRedirect_AllowsPublicHost(t *testing.T) {
	t.Parallel()

	client := SecureHTTPClient()

	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	err = client.CheckRedirect(req, []*http.Request{req})
	if err != nil {
		t.Fatalf("expected public host redirect to be allowed, got: %v", err)
	}
}

func TestSecureHTTPClient_CheckRedirect_TooManyHops(t *testing.T) {
	t.Parallel()

	client := SecureHTTPClient()

	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	via := make([]*http.Request, 3)
	for i := range via {
		via[i] = req
	}

	err = client.CheckRedirect(req, via)
	if err == nil {
		t.Fatal("expected too many redirects error")
	}
	if !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("expected too many redirects error, got: %v", err)
	}
}

func TestSecureTransport_BlocksDirectPrivateIP(t *testing.T) {
	t.Parallel()

	transport := SecureTransport()
	_, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected blocked direct private IP")
	}
	if !strings.Contains(err.Error(), "blocked host") {
		t.Fatalf("expected blocked host error, got: %v", err)
	}
}

func TestSecureTransport_DialContext_InvalidAddr(t *testing.T) {
	t.Parallel()

	transport := SecureTransport()
	_, err := transport.DialContext(context.Background(), "tcp", "not-a-valid-address")
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestSecureTransport_DialContext_BlocksLocalhost(t *testing.T) {
	t.Parallel()

	transport := SecureTransport()
	_, err := transport.DialContext(context.Background(), "tcp", "localhost:80")
	if err == nil {
		t.Fatal("expected localhost to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked host") {
		t.Fatalf("expected blocked host error, got: %v", err)
	}
}
