# kimi-lite Security & Error Handling Audit — Second Pass

**Auditor:** Senior Go Engineer (Subagent)  
**Date:** 2026-06-09  
**Scope:** Verify first-round security fixes, find new issues introduced by fixes, find missed issues.  

---

### Score: 3/10

The first-round fixes are **surface-level and incomplete**. Key security holes remain wide open:
- The "shell sandbox" is just `cmd.Dir` — it does **not** restrict filesystem access, network access, or environment inheritance.
- `validatePath` does **not** resolve symlinks, allowing trivial sandbox escapes.
- `fetch_url` blocks only the *initial* hostname; HTTP redirects to `127.0.0.1` are blindly followed.
- Error messages leak absolute filesystem paths.

The codebase *did* improve structurally (approval gate, timeout limits, WAL mode), but the core attack vectors are still exploitable.

---

### Critical Issues (must fix)

#### 1. `validatePath` does not resolve symlinks — complete sandbox escape
- **File:Line:** `internal/core/tools.go:62-88`
- **Description:** `validatePath` calls `filepath.Clean` + `filepath.Abs` but **never** `filepath.EvalSymlinks`. A symlink inside the sandbox pointing outside (e.g., `sandbox/link -> /etc`) passes validation because the symlink *path* is inside the sandbox. `os.ReadFile` then follows the symlink and reads arbitrary files. I confirmed this live: a symlink to a file outside `t.TempDir()` was read successfully.
- **Impact:** Arbitrary file read/write outside the sandbox. An LLM can exfiltrate `~/.ssh/id_rsa`, `~/.config/kimi-lite/config.toml` (API keys), or overwrite `~/.bashrc`.
- **Recommended fix:** Call `filepath.EvalSymlinks(absPath)` before the prefix check, or reject paths whose final resolved target lies outside `sandboxRoot`.

#### 2. Shell "sandbox" is fiction — unrestricted read/write/execute
- **File:Line:** `internal/core/tools.go:361-364`
- **Description:** The fix for "unrestricted shell" was `cmd.Dir = e.sandboxRoot`. `cmd.Dir` only sets the working directory; it is **not** a chroot. I confirmed live that `cat /etc/passwd`, `echo pwned > /tmp/anywhere`, and `cd / && ls etc` all succeed inside the "sandboxed" shell.
- **Impact:** Full filesystem access with the user's privileges. Can read secrets, modify system files, and execute arbitrary commands anywhere on disk.
- **Recommended fix:** Either remove the `shell` tool entirely, or run it inside an actual sandbox (e.g., `chroot`, `firejail`, `nsjail`, or Docker). At minimum, sanitize the environment with `cmd.Env = []string{"PATH=/usr/bin:/bin"}` and block commands containing absolute paths or `cd`.

#### 3. SSRF bypass via HTTP redirects to blocked hosts
- **File:Line:** `internal/core/tools.go:50-60` and `internal/core/tools.go:427-438`
- **Description:** `isBlockedHost` is called **only once** on the user-provided URL. The `CheckRedirect` hook only limits redirect count to 3. It does **not** re-validate the hostname of redirect destinations. I confirmed live that a request to any external URL returning `Location: http://127.0.0.1:9999/secret` is followed, producing a connection-refused error that names the localhost target — proving the redirect was attempted.
- **Impact:** Full SSRF. An attacker-controlled redirect can scan internal services, hit `localhost`, or reach `169.254.169.254` (cloud metadata endpoints).
- **Recommended fix:** In `CheckRedirect`, parse `req.URL` and call `isBlockedHost(req.URL.Hostname())` before allowing each redirect.

#### 4. Shell inherits full environment — API key leakage
- **File:Line:** `internal/core/tools.go:361-364`
- **Description:** `exec.CommandContext` inherits the parent environment by default. I confirmed live that `echo $TEST_SECRET` prints the value of an environment variable set before running the tool. If the user has `KIMI_LLM_API_KEY`, `MOONSHOT_API_KEY`, or `HOME` set, the LLM can exfiltrate them via `env`, `echo $VAR`, or `curl`.
- **Impact:** Exposure of LLM API keys, PATH, home directory, and any other sensitive env vars.
- **Recommended fix:** Explicitly set `cmd.Env = []string{"PATH=/usr/bin:/bin"}` (or a similarly minimal allowlist) before executing the shell command.

---

### Medium Issues (should fix)

#### 5. `grep -r` follows symlinks, bypassing `validatePath`
- **File:Line:** `internal/core/tools.go:342`
- **Description:** `execGrep` runs `grep -r -n pattern validPath`. Even if `validPath` is inside the sandbox, `grep -r` follows symlinks by default, traversing into directories outside the sandbox.
- **Impact:** Information disclosure via symlink traversal during recursive grep.
- **Recommended fix:** Use `grep -r -n --dereference-recurse` (GNU grep) or validate that no symlink in the search tree points outside the sandbox before running grep.

#### 6. DNS rebinding vulnerability in `fetch_url`
- **File:Line:** `internal/core/tools.go:372-406`
- **Description:** `isBlockedHost` checks the hostname **string** before DNS resolution. A hostname like `attacker.example.com` that resolves to `127.0.0.1` passes the check because it is neither "localhost" nor a literal IP.
- **Impact:** SSRF to internal services via DNS rebinding.
- **Recommended fix:** Perform a custom `DialContext` that resolves the hostname, checks the resolved IP with `isBlockedHost`, and then connects. Do not rely solely on pre-resolution string checks.

#### 7. `mcp-guard` binary spawned without path validation
- **File:Line:** `internal/mcp/transport.go:94-98`
- **Description:** `Connect` uses `exec.LookPath(t.command)` to find `mcp-guard`. It does not validate whether the discovered binary is in a trusted directory, whether it is the expected binary, or whether the file is executable by the owner only. A malicious `mcp-guard` earlier in `$PATH` will be executed.
- **Impact:** Supply-chain / PATH hijacking. Arbitrary code execution if an attacker controls a directory in `$PATH`.
- **Recommended fix:** Validate the resolved path against an allowlist (e.g., `/usr/local/bin`, `/usr/bin`, `~/.cargo/bin`) and verify the file is not writable by group/others.

#### 8. Approval UI forces all-or-nothing decisions on pending tool calls
- **File:Line:** `internal/tui/model.go:718-727`
- **Description:** In `approveCurrent`, if the user presses `y` or `n` for the first pending call, **all other pending calls are auto-denied** without ever being shown to the user. Only `a` (always) approves all. There is no way to individually approve call #1, deny call #2, and approve call #3.
- **Impact:** LLM-generated multi-tool workflows may be partially and silently denied, leading to data loss or incorrect behavior. Users may press `y` thinking they are only approving the visible call.
- **Recommended fix:** Present each pending call individually and accumulate decisions. Only proceed after the user has explicitly decided on every call.

#### 9. Error messages leak absolute filesystem paths
- **File:Line:** `internal/core/tools.go:75`
- **Description:** `fmt.Errorf("sandbox: access to %s is blocked", absPath)` includes the full absolute path in the error. This reveals the user's home directory name, internal directory structure, and confirms the existence of sensitive files (e.g., `/home/alice/.ssh/id_rsa`).
- **Impact:** Information disclosure aiding targeted attacks.
- **Recommended fix:** Return a generic error like `"path is outside the sandbox"` without echoing the input path.

#### 10. `fetch_url` does not block `0.0.0.0`
- **File:Line:** `internal/core/tools.go:372-406`
- **Description:** `0.0.0.0` is not blocked by `isBlockedHost`. On many systems it resolves to localhost. While the connection may fail if nothing is listening, it is still a valid bypass vector for localhost services bound to `0.0.0.0`.
- **Impact:** Potential SSRF to services listening on `0.0.0.0`.
- **Recommended fix:** Add `0.0.0.0` to the blocked host list.

#### 11. LLM client error responses may contain sensitive data
- **File:Line:** `internal/llm/client.go:248`
- **Description:** On 4xx errors, the client returns `fmt.Errorf("client error %d: %s", resp.StatusCode, string(respBody))`. If the LLM provider returns the API key, account details, or internal stack traces in the error body, they are propagated up to the TUI and displayed to the user.
- **Impact:** Accidental display of API keys or account information in the terminal UI.
- **Recommended fix:** Log the raw response body at `Debug` level, but return a sanitized error to the caller (e.g., `"client error %d: %s"` with only the first 200 chars and redaction patterns).

---

### Minor Issues (nice to have)

#### 12. Auto-approve list references non-existent tool
- **File:Line:** `internal/config/config.go:23`
- **Description:** `AutoApprove` includes `"list_directory"`, but no such tool is defined in `BuiltInToolExecutor.Definitions()`. It is harmless but indicates a configuration drift between tools and approvals.
- **Recommended fix:** Remove `"list_directory"` from the default auto-approve list.

#### 13. In-memory SQLite database may have connection isolation issues
- **File:Line:** `internal/store/sqlite.go:30-38`
- **Description:** For `:memory:`, `cache=shared` is set and `MaxOpenConns(2)` is applied. With `modernc.org/sqlite`, two connections to `:memory:` may still isolate state unless `mode=memory` with a shared cache name is used. No tests exercise `:memory:` with concurrent writers.
- **Recommended fix:** Use a named in-memory DSN (`file:memdb1?mode=memory&cache=shared`) or set `MaxOpenConns(1)` when `dbPath == ":memory:"`.

#### 14. Logger level suppresses intended Info logs
- **File:Line:** `internal/app/app.go:44-45`
- **Description:** The logger is configured with `Level: slog.LevelWarn`. The code then calls `logger.Info("mcp-guard connected")` at line 94, which is silently dropped. This is a functional bug, not a security issue, but it means the operator never sees the MCP connection success message.
- **Recommended fix:** Lower the level to `Info` or move the MCP connection log to `Warn` (or remove it).

#### 15. `write_file` creates directories with overly permissive mode
- **File:Line:** `internal/core/tools.go:281`
- **Description:** `os.MkdirAll(filepath.Dir(validPath), 0755)` creates parent directories world-readable and executable. In a multi-user environment, this could expose created directories.
- **Recommended fix:** Use `0750` or respect the system `umask`.

---

### Strengths (what's done well)

1. **Approval gate is thread-safe and correctly gated**  
   `internal/core/approval.go:22-69` — `ApprovalGate` uses `sync.RWMutex`, correctly distinguishes `ModeAuto` / `ModeManual` / `ModeYolo`, and all tool calls in `turn.go` flow through it. No bypass paths were found.

2. **10MB response body limit is strictly enforced**  
   `internal/core/tools.go:433` — `io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))` prevents memory exhaustion from massive responses.

3. **Shell timeout is enforced via context**  
   `internal/core/tools.go:358` — `context.WithTimeout(ctx, e.shellTimeout)` combined with `exec.CommandContext` ensures long-running shell commands are killed. Tests confirm `sleep 5` with 100ms timeout fails correctly.

4. **URL scheme whitelist prevents file:// and other protocols**  
   `internal/core/tools.go:417-419` — Only `http` and `https` schemes are allowed, blocking `file:///etc/passwd` and `ftp://` attacks.

5. **SQLite WAL mode + foreign keys + busy timeout are properly configured**  
   `internal/store/sqlite.go:38-51` — `PRAGMA journal_mode=WAL`, `foreign_keys=ON`, and `busy_timeout=5000` provide safe concurrent access. Tests confirm concurrent message appends succeed without corruption.

---

### Top 5 Actionable Recommendations

1. **Replace the shell pseudo-sandbox with real isolation**  
   `cmd.Dir` is not a sandbox. Remove the `shell` tool, or run it under `chroot`, `nsjail`, or a minimal container. At an absolute minimum, sanitize `cmd.Env` to a bare `PATH` and block commands containing absolute paths.

2. **Resolve symlinks in `validatePath`**  
   Add `filepath.EvalSymlinks(absPath)` before the prefix check. If the resolved path escapes `sandboxRoot`, reject it. Apply the same check to `execGrep` (or use `grep --dereference-recurse`).

3. **Validate redirect destinations in the HTTP client**  
   In `newSecureHTTPClient`, update `CheckRedirect` to call `isBlockedHost(req.URL.Hostname())` for every redirect. A redirect to `localhost`, `127.0.0.1`, `169.254.169.254`, or any private IP must be rejected.

4. **Resolve and validate IPs before outbound HTTP connections**  
   Replace the simple string-based `isBlockedHost` with a custom `http.Transport.DialContext` that resolves the hostname, validates the resolved IP against private ranges, and then dials. This closes both the DNS rebinding and the `0.0.0.0` bypass.

5. **Sanitize error messages and shell environment**  
   - Never echo user-provided paths in error messages (use generic "path escapes sandbox").  
   - Set `cmd.Env = []string{"PATH=/usr/bin:/bin"}` for all shell invocations.  
   - Strip or redact potentially sensitive API response bodies before surfacing them in the TUI.
