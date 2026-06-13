package hooks

import (
	"bytes"
	"context"
	"fmt"
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
func NewRunner(hooks []api.HookConfig) *Runner {
	return &Runner{
		hooks:       hooks,
		execCommand: execHook,
	}
}

// Run executes all hooks matching data.Event. Arguments are rendered with
// text/template against data before the hook is invoked. If a hook returns
// an error and ContinueOnError is false, the error is returned immediately.
// When ContinueOnError is true, execution proceeds with the next hook.
func (r *Runner) Run(ctx context.Context, data api.HookData) error {
	for _, h := range r.hooks {
		if h.Event != data.Event {
			continue
		}
		rendered, err := renderArgs(h.Args, data)
		if err != nil {
			return fmt.Errorf("prepare hook %q: %w", h.Event, err)
		}
		cfg := h
		cfg.Args = rendered
		if err := r.execCommand(ctx, cfg, data); err != nil {
			if h.ContinueOnError {
				continue
			}
			return err
		}
	}
	return nil
}

// renderArgs evaluates Go templates in hook arguments against HookData.
func renderArgs(args []string, data api.HookData) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		tmpl, err := template.New("arg").Parse(a)
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
