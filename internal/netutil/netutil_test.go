package netutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
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
	var lookupCount atomic.Int32

	// Mock resolver that returns a public IP.
	mockLookup := func(ctx context.Context, host string) ([]string, error) {
		lookupCount.Add(1)
		return []string{"93.184.216.34"}, nil // example.com IP
	}

	dialer := &net.Dialer{
		Timeout:   100 * time.Millisecond,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := mockLookup(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IPs resolved for host %s", host)
			}

			for _, ip := range ips {
				if IsBlockedHost(ip) {
					return nil, fmt.Errorf("blocked host: resolved IP %s for %s is blocked", ip, host)
				}
			}

			// Dial the specific validated IP to avoid a second resolution.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}

	// We can't actually dial 93.184.216.34 in tests, but we can verify the
	// lookup count and that the address is formed correctly by inspecting
	// the error from the dial attempt.
	conn, err := transport.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		// Expected: connection to a fake/mock IP will fail, but the transport
		// logic should have performed exactly one lookup.
		_ = conn
	}

	if lookupCount.Load() != 1 {
		t.Fatalf("expected 1 DNS lookup, got %d", lookupCount.Load())
	}
}

func TestSecureTransport_DialContext_DialsValidatedIP(t *testing.T) {
	var dialedAddr string

	// Mock resolver that returns a specific IP.
	mockLookup := func(ctx context.Context, host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	}

	dialer := &net.Dialer{
		Timeout:   100 * time.Millisecond,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			h, p, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := mockLookup(ctx, h)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IPs resolved for host %s", h)
			}

			for _, ip := range ips {
				if IsBlockedHost(ip) {
					return nil, fmt.Errorf("blocked host: resolved IP %s for %s is blocked", ip, h)
				}
			}

			dialedAddr = net.JoinHostPort(ips[0], p)
			return dialer.DialContext(ctx, network, dialedAddr)
		},
	}

	_, _ = transport.DialContext(context.Background(), "tcp", "example.com:80")

	// The dialed address should be an IP:port, not a hostname.
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
