package netutil

import "testing"

// FuzzIsBlockedHost feeds arbitrary host strings to IsBlockedHost and ensures
// the SSRF guard never panics. Known loopback/private seeds must stay blocked.
func FuzzIsBlockedHost(f *testing.F) {
	seeds := []string{
		"localhost",
		"127.0.0.1",
		"0.0.0.0",
		"::1",
		"::",
		"::ffff:127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.1.1",
		"169.254.169.254",
		"100.64.0.1",
		"100.127.255.255",
		"fc00::1",
		"fe80::1",
		"fd12:3456::1",
		"::ffff:10.0.0.1",
		"example.com",
		"8.8.8.8",
		"1.1.1.1",
		"", // empty string
	}

	blockedSeeds := map[string]struct{}{
		"localhost":        {},
		"127.0.0.1":        {},
		"0.0.0.0":          {},
		"::1":              {},
		"::":               {},
		"::ffff:127.0.0.1": {},
		"10.0.0.1":         {},
		"172.16.0.1":       {},
		"192.168.1.1":      {},
		"169.254.1.1":      {},
		"169.254.169.254":  {},
		"100.64.0.1":       {},
		"100.127.255.255":  {},
		"fc00::1":          {},
		"fe80::1":          {},
		"fd12:3456::1":     {},
		"::ffff:10.0.0.1":  {},
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, host string) {
		got := IsBlockedHost(host) // must not panic

		if _, ok := blockedSeeds[host]; ok && !got {
			t.Errorf("IsBlockedHost(%q) = false, want true (seed regression)", host)
		}
	})
}
