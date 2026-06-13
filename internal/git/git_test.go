package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type mockRunner struct {
	stdout []byte
	stderr []byte
	err    error
	delay  time.Duration
	calls  []mockCall
}

type mockCall struct {
	dir  string
	name string
	args []string
}

func (m *mockRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	m.calls = append(m.calls, mockCall{dir: dir, name: name, args: args})
	if m.delay > 0 {
		timer := time.NewTimer(m.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return m.stdout, m.stderr, m.err
}

func TestProvider_Status(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dir        string
		stdout     []byte
		stderr     []byte
		err        error
		want       string
		wantErr    bool
		wantErrMsg string
		wantCalls  []mockCall
	}{
		{
			name:   "success",
			dir:    "/project",
			stdout: []byte("1 .M N... 100644 100644 100644 8a1218a1f9ad0e 8a1218a1f9ad0e main.go\n"),
			want:   "1 .M N... 100644 100644 100644 8a1218a1f9ad0e 8a1218a1f9ad0e main.go\n",
			wantCalls: []mockCall{
				{dir: "/project", name: "git", args: []string{"-c", "color.status=never", "status", "--porcelain"}},
			},
		},
		{
			name:       "git not installed",
			err:        exec.ErrNotFound,
			wantErr:    true,
			wantErrMsg: "git is not installed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"-c", "color.status=never", "status", "--porcelain"}},
			},
		},
		{
			name:       "not a git repository",
			stderr:     []byte("fatal: not a git repository (or any of the parent directories): .git\n"),
			err:        errors.New("exit status 128"),
			wantErr:    true,
			wantErrMsg: "not a git repository",
			wantCalls: []mockCall{
				{name: "git", args: []string{"-c", "color.status=never", "status", "--porcelain"}},
			},
		},
		{
			name:       "other error",
			stderr:     []byte("some other error\n"),
			err:        errors.New("exit status 1"),
			wantErr:    true,
			wantErrMsg: "git status failed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"-c", "color.status=never", "status", "--porcelain"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockRunner{stdout: tt.stdout, stderr: tt.stderr, err: tt.err}
			p := newProvider(m, tt.dir)

			got, err := p.Status(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrMsg, err.Error())
				}
				assertCalls(t, m.calls, tt.wantCalls)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
			assertCalls(t, m.calls, tt.wantCalls)
		})
	}
}

func TestProvider_Diff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dir        string
		path       string
		stdout     []byte
		stderr     []byte
		err        error
		want       string
		wantErr    bool
		wantErrMsg string
		wantCalls  []mockCall
	}{
		{
			name:   "success",
			dir:    "/project",
			path:   "main.go",
			stdout: []byte("diff --git a/main.go b/main.go\n+new line\n"),
			want:   "diff --git a/main.go b/main.go\n+new line\n",
			wantCalls: []mockCall{
				{dir: "/project", name: "git", args: []string{"diff", "--", "main.go"}},
			},
		},
		{
			name:       "git not installed",
			path:       "main.go",
			err:        exec.ErrNotFound,
			wantErr:    true,
			wantErrMsg: "git is not installed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"diff", "--", "main.go"}},
			},
		},
		{
			name:       "not a git repository",
			path:       "main.go",
			stderr:     []byte("fatal: not a git repository (or any of the parent directories): .git\n"),
			err:        errors.New("exit status 128"),
			wantErr:    true,
			wantErrMsg: "not a git repository",
			wantCalls: []mockCall{
				{name: "git", args: []string{"diff", "--", "main.go"}},
			},
		},
		{
			name:       "other error",
			path:       "main.go",
			stderr:     []byte("fatal: path does not exist\n"),
			err:        errors.New("exit status 128"),
			wantErr:    true,
			wantErrMsg: "git diff failed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"diff", "--", "main.go"}},
			},
		},
		{
			name:       "empty path",
			path:       "",
			wantErr:    true,
			wantErrMsg: "empty path",
			wantCalls:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockRunner{stdout: tt.stdout, stderr: tt.stderr, err: tt.err}
			p := newProvider(m, tt.dir)

			got, err := p.Diff(context.Background(), tt.path)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrMsg, err.Error())
				}
				assertCalls(t, m.calls, tt.wantCalls)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
			assertCalls(t, m.calls, tt.wantCalls)
		})
	}
}

func TestProvider_IsRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dir        string
		stdout     []byte
		stderr     []byte
		err        error
		want       bool
		wantErr    bool
		wantErrMsg string
		wantCalls  []mockCall
	}{
		{
			name:   "is repo",
			dir:    "/project",
			stdout: []byte("true\n"),
			want:   true,
			wantCalls: []mockCall{
				{dir: "/project", name: "git", args: []string{"rev-parse", "--is-inside-work-tree"}},
			},
		},
		{
			name:   "not a repo",
			stderr: []byte("fatal: not a git repository\n"),
			err:    errors.New("exit status 128"),
			want:   false,
			wantCalls: []mockCall{
				{name: "git", args: []string{"rev-parse", "--is-inside-work-tree"}},
			},
		},
		{
			name:       "git not installed",
			err:        exec.ErrNotFound,
			want:       false,
			wantErr:    true,
			wantErrMsg: "git is not installed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"rev-parse", "--is-inside-work-tree"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockRunner{stdout: tt.stdout, stderr: tt.stderr, err: tt.err}
			p := newProvider(m, tt.dir)

			got, err := p.IsRepo(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrMsg, err.Error())
				}
				assertCalls(t, m.calls, tt.wantCalls)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			assertCalls(t, m.calls, tt.wantCalls)
		})
	}
}

func TestNewProvider(t *testing.T) {
	t.Parallel()

	p := NewProvider("/some/dir")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.dir != "/some/dir" {
		t.Fatalf("expected dir %q, got %q", "/some/dir", p.dir)
	}
	if p.runner == nil {
		t.Fatal("expected non-nil runner")
	}
}

func assertCalls(t *testing.T, got, want []mockCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d calls, got %d", len(want), len(got))
	}
	for i := range got {
		if got[i].dir != want[i].dir {
			t.Fatalf("call %d dir: expected %q, got %q", i, want[i].dir, got[i].dir)
		}
		if got[i].name != want[i].name {
			t.Fatalf("call %d name: expected %q, got %q", i, want[i].name, got[i].name)
		}
		if len(got[i].args) != len(want[i].args) {
			t.Fatalf("call %d args length: expected %d, got %d", i, len(want[i].args), len(got[i].args))
		}
		for j := range got[i].args {
			if got[i].args[j] != want[i].args[j] {
				t.Fatalf("call %d arg %d: expected %q, got %q", i, j, want[i].args[j], got[i].args[j])
			}
		}
	}
}

func TestProvider_Status_Timeout(t *testing.T) {
	t.Parallel()

	m := &mockRunner{delay: 10 * time.Second}
	p := newProvider(m, "/project")

	start := time.Now()
	_, err := p.Status(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %q", err.Error())
	}
	if elapsed > gitTimeout+500*time.Millisecond {
		t.Fatalf("expected prompt timeout, took %v", elapsed)
	}
}

func TestProvider_Diff_Timeout(t *testing.T) {
	t.Parallel()

	m := &mockRunner{delay: 10 * time.Second}
	p := newProvider(m, "/project")

	start := time.Now()
	_, err := p.Diff(context.Background(), "main.go")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %q", err.Error())
	}
	if elapsed > gitTimeout+500*time.Millisecond {
		t.Fatalf("expected prompt timeout, took %v", elapsed)
	}
}

func TestExecRunner_BuildCmd(t *testing.T) {
	t.Parallel()

	r := &execRunner{}
	cmd := r.buildCmd(context.Background(), "/project", "git", "status")

	if cmd.Stdin != nil {
		t.Fatalf("expected nil stdin, got %v", cmd.Stdin)
	}
	if cmd.Dir != "/project" {
		t.Fatalf("expected dir /project, got %q", cmd.Dir)
	}

	envMap := make(map[string]string)
	for _, e := range cmd.Env {
		if i := strings.Index(e, "="); i >= 0 {
			envMap[e[:i]] = e[i+1:]
		}
	}

	if envMap["GIT_TERMINAL_PROMPT"] != "0" {
		t.Fatalf("expected GIT_TERMINAL_PROMPT=0, got %q", envMap["GIT_TERMINAL_PROMPT"])
	}
	if envMap["GIT_OPTIONAL_LOCKS"] != "0" {
		t.Fatalf("expected GIT_OPTIONAL_LOCKS=0, got %q", envMap["GIT_OPTIONAL_LOCKS"])
	}
	if envMap["GIT_PAGER"] != "cat" {
		t.Fatalf("expected GIT_PAGER=cat, got %q", envMap["GIT_PAGER"])
	}
	if envMap["LC_ALL"] != "C" {
		t.Fatalf("expected LC_ALL=C, got %q", envMap["LC_ALL"])
	}
}

func TestProvider_Status_StderrWarning(t *testing.T) {
	t.Parallel()

	// Successful git status may emit warnings to stderr (e.g. CRLF replacement).
	// The returned status should contain only stdout, and isNotRepo must
	// inspect stderr, not stdout.
	m := &mockRunner{
		stdout: []byte("1 .M N... 100644 100644 100644 8a1218a1f9ad0e 8a1218a1f9ad0e main.go\n"),
		stderr: []byte("warning: CRLF will be replaced by LF\n"),
	}
	p := newProvider(m, "/project")

	got, err := p.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "CRLF") {
		t.Fatalf("status output should not contain stderr warning, got: %q", got)
	}
	if got != "1 .M N... 100644 100644 100644 8a1218a1f9ad0e 8a1218a1f9ad0e main.go\n" {
		t.Fatalf("expected stdout only, got: %q", got)
	}
}

func TestProvider_IsRepo_Timeout(t *testing.T) {
	t.Parallel()

	m := &mockRunner{delay: 10 * time.Second}
	p := newProvider(m, "/project")

	start := time.Now()
	_, err := p.IsRepo(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %q", err.Error())
	}
	if elapsed > gitTimeout+500*time.Millisecond {
		t.Fatalf("expected prompt timeout, took %v", elapsed)
	}
}

func TestProvider_Commit(t *testing.T) {
	t.Parallel()

	m := &mockRunner{}
	p := newProvider(m, "/project")

	err := p.Commit(context.Background(), "test checkpoint")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(m.calls))
	}
	if m.calls[0].name != "git" || len(m.calls[0].args) != 2 || m.calls[0].args[0] != "add" || m.calls[0].args[1] != "-A" {
		t.Errorf("expected git add -A, got %v", m.calls[0])
	}
	if m.calls[1].name != "git" || len(m.calls[1].args) != 3 || m.calls[1].args[0] != "commit" || m.calls[1].args[1] != "-m" || m.calls[1].args[2] != "test checkpoint" {
		t.Errorf("expected git commit -m test checkpoint, got %v", m.calls[1])
	}
}

func TestProvider_Commit_DefaultMessage(t *testing.T) {
	t.Parallel()

	m := &mockRunner{}
	p := newProvider(m, "/project")

	err := p.Commit(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(m.calls))
	}
	if m.calls[1].args[2] != "kimi-lite checkpoint" {
		t.Errorf("expected default message, got %q", m.calls[1].args[2])
	}
}

func TestProvider_Commit_Timeout(t *testing.T) {
	t.Parallel()

	m := &mockRunner{delay: 10 * time.Second}
	p := newProvider(m, "/project")

	start := time.Now()
	err := p.Commit(context.Background(), "msg")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %q", err.Error())
	}
	if elapsed > gitTimeout+500*time.Millisecond {
		t.Fatalf("expected prompt timeout, took %v", elapsed)
	}
}
