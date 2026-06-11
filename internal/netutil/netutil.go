// Package netutil provides network utilities with SSRF-hardening.
package netutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// IsBlockedHost reports whether a hostname or IP should be blocked to prevent
// SSRF attacks. It covers loopback, unspecified, private, link-local,
// CGNAT (100.64.0.0/10), and localhost names.
func IsBlockedHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// CGNAT 100.64.0.0/10 is not covered by IsPrivate.
	if ipv4 := ip.To4(); ipv4 != nil {
		if ipv4[0] == 100 && ipv4[1] >= 64 && ipv4[1] <= 127 {
			return true
		}
	}
	return false
}

// SecureTransport returns an *http.Transport with SSRF-hardened dial logic:
// it resolves the hostname, checks all returned IPs against IsBlockedHost,
// and dials the first allowed IP.
func SecureTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupHost(ctx, host)
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

			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}
}

// SecureHTTPClient returns an *http.Client using SecureTransport with a 30s
// timeout and redirect guard that re-checks IsBlockedHost on every hop.
// DNS resolution and IP validation happen exactly once per connection in
// DialContext; CheckRedirect only does the cheap hostname blocklist check
// to avoid a DNS-rebinding TOCTOU window.
func SecureHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: SecureTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			if IsBlockedHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to blocked host")
			}
			// DialContext will perform the single DNS resolution and IP
			// validation when the redirect connection is actually opened.
			return nil
		},
	}
}
