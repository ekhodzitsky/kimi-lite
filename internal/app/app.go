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
	"github.com/ekhodzitsky/kimi-lite/internal/core/hooks"
	"github.com/ekhodzitsky/kimi-lite/internal/core/subagents"
	"github.com/ekhodzitsky/kimi-lite/internal/git"
	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/internal/llm"
	"github.com/ekhodzitsky/kimi-lite/internal/mcp"
	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
	"github.com/ekhodzitsky/kimi-lite/internal/observability"
	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/internal/tui"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// teaProgram is the minimal interface Bubble Tea programs satisfy.
type teaProgram interface {
	Run() (tea.Model, error)
}

// App is the central DI container and lifecycle manager for kimi-lite.
type App struct {
	cfg            *api.Config
	store          api.Store
	llmClient      api.LLMClient
	toolExecutor   api.ToolExecutor
	builtInExec    *core.BuiltInToolExecutor
	approvalGate   *core.ApprovalGate
	sessionManager *core.SessionManager
	turnManager    *core.TurnManager
	compressor     *core.ContextCompressor
	gitProvider    api.GitProvider
	mcpClient      api.MCPClient
	tuiModel       *tui.Model
	logger         *slog.Logger
	newProgram     func(model tea.Model, opts ...tea.ProgramOption) teaProgram
	pprofCancel    context.CancelFunc
	skillsContent  string
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

	// Create metrics collector.
	metrics := observability.NewCollector()

	// Ensure DB directory exists
	dbDir := filepath.Dir(cfg.Session.DBPath)
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	// Open SQLite store
	st, err := store.NewSQLite(cfg.Session.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	// Create LLM client from provider/model-table configuration.
	llmClient, err := llm.NewClientFromConfig(cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("create llm client: %w", err)
	}
	if mc, ok := llmClient.(interface{ SetMetricsCollector(api.MetricsCollector) }); ok {
		mc.SetMetricsCollector(metrics)
	}

	resolvedModel, err := llm.ResolveModelFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve model: %w", err)
	}

	// Determine sandbox root (current working directory)
	sandboxRoot, err := os.Getwd()
	if err != nil {
		sandboxRoot = "."
	}

	// Create web searcher if configured.
	var webSearcher api.WebSearcher
	if cfg.WebSearch.Endpoint != "" {
		ws, err := core.NewHTTPWebSearcher(cfg.WebSearch.Endpoint, cfg.WebSearch.APIKey, nil, cfg.WebSearch.Timeout)
		if err != nil {
			return nil, fmt.Errorf("create web search provider: %w", err)
		}
		webSearcher = ws
	}

	// Discover user skills from the config directory.
	configDir, _ := config.EnsureConfigDir()
	skillsDir := filepath.Join(configDir, "skills")
	allSkills, _ := core.DiscoverSkills(skillsDir)
	skills := core.FilterSkills(allSkills, cfg.Behavior.Skills)
	skillsContent := core.LoadSkillContent(skills)

	// Create built-in tool executor (nil httpClient forces secure default).
	// Protect the app's own config and DB paths regardless of sandbox.
	configPath := filepath.Join(configDir, "config.toml")
	builtInExec, err := core.NewBuiltInToolExecutor(core.ToolExecutorConfig{
		ShellTimeout:   cfg.Behavior.ShellTimeout,
		SandboxRoot:    sandboxRoot,
		HTTPClient:     nil,
		ProtectedPaths: []string{configPath, cfg.Session.DBPath},
		PassEnv:        cfg.Behavior.PassEnv,
		WebSearcher:    webSearcher,
		VideoExtractor: core.NewVideoExtractor(),
	})
	if err != nil {
		return nil, fmt.Errorf("create built-in tool executor: %w", err)
	}
	builtInExec.SetAllowShell(cfg.Behavior.AllowShell)
	builtInExec.SetMetricsCollector(metrics)

	// Wire the ephemeral subagent runner into the built-in tool executor.
	// Subagents receive a restricted tool set and do not persist sessions.
	subRunner := subagents.NewRunner(llmClient, builtInExec, sandboxRoot)
	builtInExec.SetSubagentRunner(subRunner)

	// Wire lifecycle hooks into components that emit events.
	hookRunner := hooks.NewRunner(cfg.Hooks)
	builtInExec.SetHookRunner(hookRunner)

	// Attempt MCP connection (non-fatal)
	var mcpClient api.MCPClient
	var executors []api.ToolExecutor
	executors = append(executors, builtInExec)

	hasDirectServers := false
	for _, srv := range cfg.MCPServers {
		if srv.Enabled {
			hasDirectServers = true
			break
		}
	}

	if hasDirectServers {
		clients := make(map[string]api.MCPClient)
		configs := make(map[string]api.MCPServerConfig)
		for name, srv := range cfg.MCPServers {
			if !srv.Enabled {
				continue
			}
			cli, err := newMCPClientForServer(srv)
			if err != nil {
				logger.Warn("failed to create mcp client", "server", name, "error", err)
				continue
			}
			startup := time.Duration(srv.StartupTimeoutMs) * time.Millisecond
			if startup <= 0 {
				startup = 5 * time.Second
			}
			connectCtx, cancel := context.WithTimeout(context.Background(), startup)
			if err := cli.Connect(connectCtx); err != nil {
				logger.Warn("mcp server unavailable", "server", name, "error", err)
				cancel()
				_ = cli.Close()
				continue
			}
			cancel()
			clients[name] = cli
			configs[name] = srv
		}
		if len(clients) > 0 {
			multi := mcp.NewMultiClient(clients, configs)
			mcpClient = multi
			executors = append(executors, mcp.NewToolExecutor(multi))
			logger.Info("mcp servers connected", "count", len(clients))
		}
	} else {
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
	}

	// Create composite tool executor
	toolExec := core.NewCompositeToolExecutor(executors...)

	// Validate auto-approve entries: drop unknown or non-read-only tools.
	isReadOnly := func(name string) bool {
		return toolExec.IsReadOnly(name)
	}
	var validatedAutoApprove []string
	for _, name := range cfg.Behavior.AutoApprove {
		if isReadOnly(name) {
			validatedAutoApprove = append(validatedAutoApprove, name)
		} else {
			logger.Warn("dropping non-read-only or unknown tool from auto_approve", "tool", name)
		}
	}

	// Create approval gate (start in Auto mode)
	approval := core.NewApprovalGate(core.ModeAuto, validatedAutoApprove, isReadOnly, cfg.Permission.Rules)

	// Attach risk-aware scoring so destructive or escaping operations are not
	// silently auto-approved.
	riskEval := core.NewRiskEvaluator(cfg.Permission.RiskRules, sandboxRoot)
	approval.SetRiskEvaluator(riskEval, cfg.Permission.RiskThreshold)

	// Create session manager
	sessionMgr := core.NewSessionManager(st)
	sessionMgr.SetHookRunner(hookRunner)
	sessionMgr.SetMetricsCollector(metrics)

	// Create turn manager
	turnMgr := core.NewTurnManager(llmClient, toolExec, approval, st, &configProvider{cfg: cfg})
	turnMgr.SetHookRunner(hookRunner)
	turnMgr.SetMetricsCollector(metrics)

	// Create context compressor
	modelInfo := llm.LookupModel(resolvedModel)
	compressor := core.NewContextCompressor(llmClient, modelInfo.ContextWindow, cfg.LLM.Timeout)
	compressor.SetTokenEstimator(core.NewHeuristicTokenEstimator())

	application := &App{
		cfg:            cfg,
		store:          st,
		llmClient:      llmClient,
		toolExecutor:   toolExec,
		builtInExec:    builtInExec,
		approvalGate:   approval,
		sessionManager: sessionMgr,
		turnManager:    turnMgr,
		compressor:     compressor,
		mcpClient:      mcpClient,
		logger:         logger,
		skillsContent:  skillsContent,
	}

	// Start runtime profiling server if requested.
	if cfg.PprofAddr != "" {
		pprofCtx, pprofCancel := context.WithCancel(context.Background())
		application.pprofCancel = pprofCancel
		go func() {
			if err := observability.StartPprof(pprofCtx, cfg.PprofAddr); err != nil {
				logger.Warn("pprof server exited", "addr", cfg.PprofAddr, "error", err)
			}
		}()
	}

	return application, nil
}

// SetYolo sets the approval gate to yolo mode (auto-approve everything).
func (a *App) SetYolo(yolo bool) {
	if yolo {
		a.approvalGate.SetMode(core.ModeYolo)
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

// RunTurn executes a single turn for the given session and input.
// It returns a channel that streams turn events.
func (a *App) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	return a.turnManager.RunTurn(ctx, sessionID, input)
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
	model.SetMCPClient(a.mcpClient)
	model.SetStore(a.store)

	// Wire approval callbacks
	model.SetAutoApproveSetter(func(name string) {
		a.approvalGate.AddAutoApprove(name)
	})
	model.SetApprovalModeSetter(func(mode int) {
		a.approvalGate.SetMode(core.ApprovalMode(mode))
	})
	model.SetApprovalMode(int(a.approvalGate.GetMode()))

	// Set context stats from model info
	resolvedModel, err := llm.ResolveModelFromConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("resolve model: %w", err)
	}
	modelInfo := llm.LookupModel(resolvedModel)
	model.SetContextStats(0, modelInfo.ContextWindow)
	model.SetModelName(resolvedModel)

	// Count tools
	model.SetToolCount(len(a.toolExecutor.Definitions(ctx)))

	// Construct the git provider with the resolved session directory so the
	// dir code path is exercised in production.
	a.gitProvider = git.NewProvider(session.Path)
	model.SetGitProvider(a.gitProvider)

	now := time.Now().UTC()

	// Add git status to initial context if in a repo
	if isRepo, repoErr := a.gitProvider.IsRepo(ctx); isRepo {
		if status, err := a.gitProvider.Status(ctx); err == nil && status != "" {
			if appendErr := a.store.AppendMessage(ctx, session.ID, api.Message{
				ID:        idgen.GenerateID(),
				Role:      api.RoleSystem,
				Content:   fmt.Sprintf("Current git status:\n%s", status),
				CreatedAt: now,
			}); appendErr != nil {
				a.logger.Warn("failed to append git status message", "error", appendErr)
			}
		} else if err != nil {
			a.logger.Debug("git status failed", "error", err)
		}
	} else if repoErr != nil {
		a.logger.Debug("git is-repo failed", "error", repoErr)
	}

	// Add system message with agentic prompt. Its timestamp is slightly after
	// the optional git-status message so ordering is deterministic.
	if appendErr := a.store.AppendMessage(ctx, session.ID, api.Message{
		ID:        idgen.GenerateID(),
		Role:      api.RoleSystem,
		Content:   systemPrompt(session.Path, a.skillsContent),
		CreatedAt: now.Add(time.Millisecond),
	}); appendErr != nil {
		a.logger.Warn("failed to append system message", "error", appendErr)
	}

	a.tuiModel = model

	var p teaProgram
	if a.newProgram != nil {
		p = a.newProgram(model, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion())
	} else {
		p = tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion())
	}
	_, err = p.Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
		return nil
	}
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
		Version:    api.SessionExportVersion,
		ExportedAt: time.Now().UTC(),
		Session:    *sess,
		Messages:   msgs,
		Turns:      turns,
	}, nil
}

// ImportSession imports a session from an export, creating a new session with a new ID.
func (a *App) ImportSession(ctx context.Context, export *api.SessionExport) (*api.Session, error) {
	if export.Version != "" && export.Version != api.SessionExportVersion {
		return nil, fmt.Errorf("unsupported export version %q", export.Version)
	}
	created, err := a.store.CreateSession(ctx, export.Session.Path)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	created.Name = export.Session.Name
	if err := a.store.UpdateSession(ctx, created); err != nil {
		cleanupErr := a.store.DeleteSession(ctx, created.ID)
		return nil, errors.Join(fmt.Errorf("update session name: %w", err), cleanupErr)
	}
	// Restore messages
	for _, msg := range export.Messages {
		if err := a.store.AppendMessage(ctx, created.ID, msg); err != nil {
			cleanupErr := a.store.DeleteSession(ctx, created.ID)
			return nil, errors.Join(fmt.Errorf("append message: %w", err), cleanupErr)
		}
	}
	// Restore turns
	for _, turn := range export.Turns {
		if err := a.store.SaveTurn(ctx, created.ID, turn); err != nil {
			cleanupErr := a.store.DeleteSession(ctx, created.ID)
			return nil, errors.Join(fmt.Errorf("save turn: %w", err), cleanupErr)
		}
	}
	return created, nil
}

// newMCPClientForServer creates an api.MCPClient for a single direct server
// configuration. It is a package-level variable so tests can inject fakes.
var newMCPClientForServer = func(cfg api.MCPServerConfig) (api.MCPClient, error) {
	switch cfg.Transport {
	case api.MCPTransportStdio:
		tr := mcp.NewStdioTransport(cfg.Command, cfg.Args...)
		tr.SetEnv(cfg.Env)
		tr.SetCWD(cfg.CWD)
		return mcp.NewClient(tr), nil
	case api.MCPTransportHTTP:
		tr := mcp.NewHTTPTransport(cfg.URL, cfg.Headers, cfg.BearerTokenEnvVar, netutil.SecureHTTPClient())
		return mcp.NewClient(tr), nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport %q", cfg.Transport)
	}
}

// Close gracefully shuts down all resources.
func (a *App) Close() error {
	var errs []error

	// Stop the profiling server if it was started.
	if a.pprofCancel != nil {
		a.pprofCancel()
	}

	// Cancel in-flight turns before waiting so blocked turns abort promptly.
	if a.turnManager != nil {
		a.turnManager.CancelAll()
	}

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
	if a.builtInExec != nil {
		if err := a.builtInExec.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close built-in tool executor: %w", err))
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
