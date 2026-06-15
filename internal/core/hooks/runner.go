package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"text/template"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// execCommandFunc is the signature used to execute a single hook.
// It is exposed as a field on Runner so tests can inject a fake.
type execCommandFunc func(ctx context.Context, cfg api.HookConfig, data api.HookData) error

// Runner executes lifecycle hooks configured for specific events.
type Runner struct {
	hooks       []api.HookConfig
	execCommand execCommandFunc
}

// NewRunner creates a new hook runner for the provided configurations.
// The provided slice and per-hook environment maps are deep-copied to avoid
// accidental mutation after construction.
func NewRunner(hooks []api.HookConfig) *Runner {
	copied := make([]api.HookConfig, len(hooks))
	for i, h := range hooks {
		copied[i] = h
		if h.Args != nil {
			copied[i].Args = make([]string, len(h.Args))
			copy(copied[i].Args, h.Args)
		}
		if h.Env != nil {
			copied[i].Env = make(map[string]string, len(h.Env))
			for k, v := range h.Env {
				copied[i].Env[k] = v
			}
		}
	}
	return &Runner{
		hooks:       copied,
		execCommand: execHook,
	}
}

// Run executes all hooks matching data.Event. Arguments are rendered with
// text/template against data before the hook is invoked. If a hook returns
// an error and ContinueOnError is false, the error is returned immediately.
// When ContinueOnError is true, execution proceeds with the next hook and
// any errors are logged and aggregated into a single returned error.
func (r *Runner) Run(ctx context.Context, data api.HookData) error {
	var errs []error
	for _, h := range r.hooks {
		if h.Event != data.Event {
			continue
		}
		rendered, err := renderArgs(h.Args, data)
		if err != nil {
			err = fmt.Errorf("prepare hook %q: %w", h.Event, err)
			if h.ContinueOnError {
				slog.Warn("hook template failed, continuing", "event", h.Event, "error", err)
				errs = append(errs, err)
				continue
			}
			return err
		}
		cfg := h
		cfg.Args = rendered
		if err := r.execCommand(ctx, cfg, data); err != nil {
			if h.ContinueOnError {
				slog.Warn("hook failed, continuing", "event", h.Event, "error", err)
				errs = append(errs, err)
				continue
			}
			return fmt.Errorf("run hook %q: %w", h.Event, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("hooks completed with %d error(s): %w", len(errs), errors.Join(errs...))
	}
	return nil
}

// renderArgs evaluates Go templates in hook arguments against HookData.
// Missing template keys produce an error so that interpolated user data does
// not silently render as "<no value>".
//
// Security note: when a hook uses a shell such as `sh -c`, rendered fields
// from HookData are interpolated into the command string. Malicious tool
// arguments, file names, or error text could inject shell metacharacters.
// Avoid shell wrappers when possible, or sanitize/quote rendered values.
func renderArgs(args []string, data api.HookData) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		tmpl, err := template.New("arg").Option("missingkey=error").Parse(a)
		if err != nil {
			return nil, fmt.Errorf("parse arg %q: %w", a, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("execute arg %q: %w", a, err)
		}
		out[i] = buf.String()
	}
	return out, nil
}

// Ensure Runner implements api.HookRunner.
var _ api.HookRunner = (*Runner)(nil)
