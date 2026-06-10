package netutil

import (
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
