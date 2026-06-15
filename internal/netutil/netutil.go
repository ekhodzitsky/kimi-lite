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
// multicast, broadcast, reserved, documentation, CGNAT (100.64.0.0/10), and
// localhost names. IPv4-compatible IPv6 addresses (::127.0.0.1, ::10.0.0.1,
// etc.) and IPv6 zone identifiers are handled before parsing.
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
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		// CGNAT 100.64.0.0/10 is not covered by IsPrivate.
		if ipv4[0] == 100 && ipv4[1] >= 64 && ipv4[1] <= 127 {
			return true
		}
		// Broadcast.
		if ipv4.Equal(net.IPv4bcast) {
			return true
		}
		// Reserved for future use (240.0.0.0/4).
		if ipv4[0] >= 240 {
			return true
		}
		// IPv4 documentation ranges (TEST-NET).
		if isDocumentationIPv4(ipv4) {
			return true
		}
		// This network (0.0.0.0/8), including 0.0.0.0 which IsUnspecified already covers.
		if ipv4[0] == 0 {
			return true
		}
		return false
	}
	// IPv4-compatible IPv6 addresses (::/96) embed an IPv4 address. Re-check
	// the embedded IPv4 against the blocklist.
	if isIPv4Compatible(ip) {
		embedded := net.IP(ip[12:16])
		return IsBlockedHost(embedded.String())
	}
	// IPv6 documentation range 2001:db8::/32.
	if isDocumentationIPv6(ip) {
		return true
	}
	return false
}

// isDocumentationIPv4 reports whether ipv4 is one of the RFC 5737 documentation
// ranges: 192.0.2.0/24, 198.51.100.0/24, or 203.0.113.0/24.
func isDocumentationIPv4(ipv4 net.IP) bool {
	switch {
	case ipv4[0] == 192 && ipv4[1] == 0 && ipv4[2] == 2:
		return true
	case ipv4[0] == 198 && ipv4[1] == 51 && ipv4[2] == 100:
		return true
	case ipv4[0] == 203 && ipv4[1] == 0 && ipv4[2] == 113:
		return true
	}
	return false
}

// isDocumentationIPv6 reports whether ip is the RFC 3849 documentation range
// 2001:db8::/32.
func isDocumentationIPv6(ip net.IP) bool {
	return len(ip) == 16 && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8
}

// isIPv4Compatible reports whether ip is an IPv4-compatible IPv6 address
// (::/96) as defined in RFC 4291, excluding IPv4-mapped addresses (::ffff/96).
func isIPv4Compatible(ip net.IP) bool {
	if len(ip) != 16 {
		return false
	}
	for i := 0; i < 12; i++ {
		if ip[i] != 0 {
			return false
		}
	}
	return !(ip[10] == 0xff && ip[11] == 0xff)
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
