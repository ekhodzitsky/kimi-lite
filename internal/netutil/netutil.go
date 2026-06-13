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
// CGNAT (100.64.0.0/10), and localhost names. IPv6 zone identifiers are
// stripped before parsing.
func IsBlockedHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	// Strip IPv6 zone identifiers (e.g. "fe80::1%eth0") before parsing.
	if i := strings.Index(hostname, "%"); i >= 0 {
		hostname = hostname[:i]
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
// it resolves the hostname, blocks the connection if any resolved IP is
// disallowed, and dials only the first resolved IP.
func SecureTransport() *http.Transport {
	return secureTransport(net.DefaultResolver.LookupHost, defaultDialer().DialContext)
}

func defaultDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
}

func secureTransport(
	lookupHost func(context.Context, string) ([]string, error),
	dialContext func(context.Context, string, string) (net.Conn, error),
) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("split host port: %w", err)
			}

			ips, err := lookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("lookup host %q: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IPs resolved for host %q", host)
			}

			for _, ip := range ips {
				if IsBlockedHost(ip) {
					return nil, fmt.Errorf("blocked host: resolved IP %q for %q is blocked", ip, host)
				}
			}

			conn, err := dialContext(ctx, network, net.JoinHostPort(ips[0], port))
			if err != nil {
				return nil, fmt.Errorf("dial %q: %w", ips[0], err)
			}
			return conn, nil
		},
	}
}

// SecureHTTPClient returns a new *http.Client using SecureTransport with a 30s
// timeout and a redirect guard that re-checks IsBlockedHost on every hop.
// The returned client is safe for reuse across multiple concurrent requests.
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
