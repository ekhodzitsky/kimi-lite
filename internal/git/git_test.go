package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type mockRunner struct {
	output []byte
	err    error
	calls  []mockCall
}

type mockCall struct {
	dir  string
	name string
	args []string
}

func (m *mockRunner) CombinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	m.calls = append(m.calls, mockCall{dir: dir, name: name, args: args})
	return m.output, m.err
}

func TestProvider_Status(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dir        string
		output     []byte
		err        error
		want       string
		wantErr    bool
		wantErrMsg string
		wantCalls  []mockCall
	}{
		{
			name:   "success",
			dir:    "/project",
			output: []byte("On branch main\nnothing to commit\n"),
			want:   "On branch main\nnothing to commit\n",
			wantCalls: []mockCall{
				{dir: "/project", name: "git", args: []string{"status"}},
			},
		},
		{
			name:       "git not installed",
			err:        exec.ErrNotFound,
			wantErr:    true,
			wantErrMsg: "git is not installed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"status"}},
			},
		},
		{
			name:       "not a git repository",
			output:     []byte("fatal: not a git repository (or any of the parent directories): .git\n"),
			err:        errors.New("exit status 128"),
			wantErr:    true,
			wantErrMsg: "not a git repository",
			wantCalls: []mockCall{
				{name: "git", args: []string{"status"}},
			},
		},
		{
			name:       "other error",
			output:     []byte("some other error\n"),
			err:        errors.New("exit status 1"),
			wantErr:    true,
			wantErrMsg: "git status failed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"status"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockRunner{output: tt.output, err: tt.err}
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
		output     []byte
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
			output: []byte("diff --git a/main.go b/main.go\n+new line\n"),
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
			output:     []byte("fatal: not a git repository (or any of the parent directories): .git\n"),
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
			output:     []byte("fatal: path does not exist\n"),
			err:        errors.New("exit status 128"),
			wantErr:    true,
			wantErrMsg: "git diff failed",
			wantCalls: []mockCall{
				{name: "git", args: []string{"diff", "--", "main.go"}},
			},
		},
		{
			name:   "empty path",
			path:   "",
			output: []byte(""),
			want:   "",
			wantCalls: []mockCall{
				{name: "git", args: []string{"diff", "--", ""}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockRunner{output: tt.output, err: tt.err}
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
		name      string
		dir       string
		output    []byte
		err       error
		want      bool
		wantCalls []mockCall
	}{
		{
			name:   "is repo",
			dir:    "/project",
			output: []byte(".git\n"),
			want:   true,
			wantCalls: []mockCall{
				{dir: "/project", name: "git", args: []string{"rev-parse", "--git-dir"}},
			},
		},
		{
			name:   "not a repo",
			output: []byte("fatal: not a git repository\n"),
			err:    errors.New("exit status 128"),
			want:   false,
			wantCalls: []mockCall{
				{name: "git", args: []string{"rev-parse", "--git-dir"}},
			},
		},
		{
			name: "git not installed",
			err:  exec.ErrNotFound,
			want: false,
			wantCalls: []mockCall{
				{name: "git", args: []string{"rev-parse", "--git-dir"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockRunner{output: tt.output, err: tt.err}
			p := newProvider(m, tt.dir)

			got := p.IsRepo(context.Background())

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
