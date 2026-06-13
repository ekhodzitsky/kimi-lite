# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1.0 | :x:                |

## Reporting a Vulnerability

Please report security vulnerabilities privately via GitHub Security Advisories:

- Open a [private security advisory](https://github.com/ekhodzitsky/kimi-lite/security/advisories/new)
- Or email **ekhodzitsky@gmail.com** with the subject `[kimi-lite security]`

We aim to respond within 72 hours and will coordinate disclosure once a fix is ready.

## Attack Surface and Hardening

kimi-lite executes shell commands and fetches arbitrary URLs on behalf of an LLM, which creates a real attack surface. The following hardening is already in place:

- **SSRF redirect protection** — `fetch_url` limits redirects and validates final URLs
- **DNS rebinding guard** — `newSecureHTTPClient` resolves hosts and blocks private/loopback IPs before dialing
- **Sandboxed file access** — built-in tools operate inside a configurable root directory with path validation
- **Environment isolation** — shell tool execution does not inherit the full host environment
- **Size limits** — 10 MB caps on responses and tool outputs

If you discover a bypass or weakness in any of the above, please report it using the channel above.
