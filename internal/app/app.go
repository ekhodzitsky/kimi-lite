// Package app provides the application layer and DI container for kimi-lite.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/git"
	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/internal/llm"
	"github.com/ekhodzitsky/kimi-lite/internal/mcp"
	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/internal/tui"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// App is the central DI container and lifecycle manager for kimi-lite.
type App struct {
	cfg            *api.Config
	store          api.Store
	llmClient      api.LLMClient
	toolExecutor   api.ToolExecutor
	approvalGate   *core.ApprovalGate
	sessionManager *core.SessionManager
	turnManager    *core.TurnManager
	compressor     *core.ContextCompressor
	gitProvider    api.GitProvider
	mcpClient      api.MCPClient
	tuiModel       *tui.Model
	logger         *slog.Logger
}

// New creates a fully wired App from configuration.
func New(cfg *api.Config, debug bool) (*App, error) {
	logLevel := slog.LevelWarn
	if debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Ensure DB directory exists
	dbDir := filepath.Dir(cfg.Session.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	// Open SQLite store
	st, err := store.NewSQLite(cfg.Session.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	// Create LLM client with optional fallback.
	var llmClient api.LLMClient = llm.NewClient(cfg.LLM, nil)
	if cfg.LLM.Fallback != nil {
		fallbackClient := llm.NewClient(*cfg.LLM.Fallback, nil)
		llmClient = llm.NewFallbackClient(llmClient, fallbackClient)
	}

	// Determine sandbox root (current working directory)
	sandboxRoot, err := os.Getwd()
	if err != nil {
		sandboxRoot = "."
	}

	// Create built-in tool executor (nil httpClient forces secure default).
	// Protect the app's own config and DB paths regardless of sandbox.
	configDir, _ := config.EnsureConfigDir()
	configPath := filepath.Join(configDir, "config.toml")
	builtInExec := core.NewBuiltInToolExecutor(cfg.Behavior.ShellTimeout, sandboxRoot, nil, configPath, cfg.Session.DBPath)
	builtInExec.SetAllowShell(cfg.Behavior.AllowShell)

	// Attempt MCP connection (non-fatal)
	var mcpClient api.MCPClient
	var executors []api.ToolExecutor
	executors = append(executors, builtInExec)

	mcpCli := mcp.NewClientFromConfig(cfg.MCP)
	mcpConnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mcpCli.Connect(mcpConnectCtx); err != nil {
		logger.Warn("mcp-guard not available, running without MCP tools", "error", err)
	} else {
		mcpClient = mcpCli
		executors = append(executors, mcp.NewToolExecutor(mcpClient))
		logger.Info("mcp-guard connected")
	}

	// Create composite tool executor
	toolExec := core.NewCompositeToolExecutor(executors...)

	// Create approval gate (start in Auto mode)
	approval := core.NewApprovalGate(core.ModeAuto, cfg.Behavior.AutoApprove)

	// Create session manager
	sessionMgr := core.NewSessionManager(st)

	// Create turn manager
	turnMgr := core.NewTurnManager(llmClient, toolExec, approval, st, &configProvider{cfg: cfg})

	// Create context compressor
	compressor := core.NewContextCompressor(llmClient)

	// Create git provider
	gitProvider := git.NewProvider("")

	application := &App{
		cfg:            cfg,
		store:          st,
		llmClient:      llmClient,
		toolExecutor:   toolExec,
		approvalGate:   approval,
		sessionManager: sessionMgr,
		turnManager:    turnMgr,
		compressor:     compressor,
		gitProvider:    gitProvider,
		mcpClient:      mcpClient,
		logger:         logger,
	}

	return application, nil
}

// SetYolo sets the approval gate to yolo mode (auto-approve everything).
func (a *App) SetYolo(yolo bool) {
	if yolo {
		a.approvalGate.SetMode(core.ModeYolo)
	}
}

// SetAutoApprove sets the approval gate to auto mode (auto-approve read-only).
func (a *App) SetAutoApprove(auto bool) {
	if auto {
		a.approvalGate.SetMode(core.ModeAuto)
	}
}

// StartSession creates a new session in the current directory.
func (a *App) StartSession(ctx context.Context) (*api.Session, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	return a.sessionManager.Start(ctx, wd)
}

// ResumeSession resumes a session by ID.
func (a *App) ResumeSession(ctx context.Context, id string) (*api.Session, error) {
	return a.sessionManager.Resume(ctx, id)
}

// ContinueLastSession resumes the most recent session in the current directory.
func (a *App) ContinueLastSession(ctx context.Context) (*api.Session, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	return a.sessionManager.ContinueLast(ctx, wd)
}

// Run initializes the TUI and starts the Bubble Tea program.
func (a *App) Run(ctx context.Context, session *api.Session) error {
	// Create TUI model
	model, err := tui.New(a.cfg, session, ctx)
	if err != nil {
		return fmt.Errorf("create tui model: %w", err)
	}

	// Wire services into TUI
	model.SetSessionManager(a.sessionManager)
	model.SetTurnManager(a.turnManager)
	model.SetCompressor(a.compressor)
	model.SetGitProvider(a.gitProvider)
	model.SetMCPClient(a.mcpClient)
	model.SetStore(a.store)

	// Set context stats from model info
	modelInfo := llm.LookupModel(a.cfg.LLM.Model)
	model.SetContextStats(0, modelInfo.ContextWindow)
	model.SetModelName(a.cfg.LLM.Model)

	// Count tools
	model.SetToolCount(len(a.toolExecutor.Definitions()))

	// Add git status to initial context if in a repo
	if a.gitProvider.IsRepo(ctx) {
		if status, err := a.gitProvider.Status(ctx); err == nil && status != "" {
			if appendErr := a.store.AppendMessage(ctx, session.ID, api.Message{
				ID:      idgen.GenerateID(),
				Role:    api.RoleSystem,
				Content: fmt.Sprintf("Current git status:\n%s", status),
			}); appendErr != nil {
				a.logger.Warn("failed to append git status message", "error", appendErr)
			}
		}
	}

	// Add system message with context
	if appendErr := a.store.AppendMessage(ctx, session.ID, api.Message{
		ID:      idgen.GenerateID(),
		Role:    api.RoleSystem,
		Content: fmt.Sprintf("You are kimi-lite, a helpful AI coding assistant. Current directory: %s", session.Path),
	}); appendErr != nil {
		a.logger.Warn("failed to append system message", "error", appendErr)
	}

	a.tuiModel = model

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	if err != nil {
		return fmt.Errorf("run tui: %w", err)
	}

	return nil
}

// ExportSession exports a session with all messages and turns.
func (a *App) ExportSession(ctx context.Context, sessionID string) (*api.SessionExport, error) {
	sess, err := a.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	msgs, err := a.store.GetMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	turns, err := a.store.GetTurns(ctx, sessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("get turns: %w", err)
	}
	return &api.SessionExport{
		Version:    "1.0",
		ExportedAt: time.Now().UTC(),
		Session:    *sess,
		Messages:   msgs,
		Turns:      turns,
	}, nil
}

// ImportSession imports a session from an export, creating a new session with a new ID.
func (a *App) ImportSession(ctx context.Context, export *api.SessionExport) (*api.Session, error) {
	created, err := a.store.CreateSession(ctx, export.Session.Path)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	// Restore messages
	for _, msg := range export.Messages {
		if err := a.store.AppendMessage(ctx, created.ID, msg); err != nil {
			return nil, fmt.Errorf("append message: %w", err)
		}
	}
	// Restore turns
	for _, turn := range export.Turns {
		if err := a.store.SaveTurn(ctx, created.ID, turn); err != nil {
			return nil, fmt.Errorf("save turn: %w", err)
		}
	}
	return created, nil
}

// Close gracefully shuts down all resources.
func (a *App) Close() error {
	var errs []error

	// Wait for in-flight turns with timeout.
	if a.turnManager != nil {
		done := make(chan struct{})
		go func() {
			a.turnManager.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			errs = append(errs, fmt.Errorf("turn shutdown timeout"))
		}
	}

	if a.mcpClient != nil {
		if err := a.mcpClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close mcp: %w", err))
		}
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close store: %w", err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// configProvider implements api.ConfigProvider.
type configProvider struct {
	cfg *api.Config
}

func (p *configProvider) Get() *api.Config {
	return p.cfg
}
