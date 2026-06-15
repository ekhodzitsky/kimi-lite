// Command kimi-lite is the AI coding CLI application.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ekhodzitsky/kimi-lite/internal/acp"
	"github.com/ekhodzitsky/kimi-lite/internal/app"
	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/llm"
	"github.com/ekhodzitsky/kimi-lite/internal/mcp"
	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

var (
	version    = "dev"
	commit     = "unknown"
	date       = "unknown"
	binaryName = "kimi-lite"
)

const (
	outputFormatText = "text"
	outputFormatJSON = "json"
)

type flags struct {
	configPath   string
	model        string
	yolo         bool
	continueLast bool
	sessionID    string
	debug        bool
	pprofAddr    string
	prompt       string
	outputFormat string
}

// appRunner is the interface for the application layer, allowing mocking in tests.
type appRunner interface {
	SetYolo(bool)
	ResumeSession(ctx context.Context, id string) (*api.Session, error)
	ContinueLastSession(ctx context.Context) (*api.Session, error)
	StartSession(ctx context.Context) (*api.Session, error)
	Run(ctx context.Context, session *api.Session) error
	RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error)
	ExportSession(ctx context.Context, sessionID string) (*api.SessionExport, error)
	ImportSession(ctx context.Context, export *api.SessionExport) (*api.Session, error)
	Close() error
}

// newApp creates a real App. Swapped in tests.
var newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
	return app.New(cfg, debug)
}

// writeDefaultConfig ensures the default config exists. Swapped in tests.
var writeDefaultConfig = config.WriteDefaultConfig

// stdout is the writer used by non-interactive prompt mode. Swapped in tests.
var stdout io.Writer = os.Stdout

// osExit is called by main on fatal errors. Swapped in tests.
var osExit = os.Exit

// newStore creates a real SQLite store. Swapped in tests.
var newStore = func(dbPath string) (api.Store, error) {
	return store.NewSQLite(dbPath)
}

// newLLMClient creates a real LLM client. Swapped in tests.
var newLLMClient = llm.NewClientFromConfig

// resolveModelFromConfig resolves the effective model name. Swapped in tests.
var resolveModelFromConfig = llm.ResolveModelFromConfig

// newMCPClient creates a real legacy MCP client. Swapped in tests.
var newMCPClient = func(cfg api.MCPConfig) api.MCPClient {
	return mcp.NewClientFromConfig(cfg)
}

// newMCPClientFromServerConfig creates a real MCP client from server config. Swapped in tests.
var newMCPClientFromServerConfig = func(cfg api.MCPServerConfig, httpClient *http.Client) (api.MCPClient, error) {
	return mcp.NewClientFromServerConfig(cfg, httpClient)
}

// newMCPMultiClient aggregates multiple MCP clients. Swapped in tests.
var newMCPMultiClient = func(clients map[string]api.MCPClient, configs map[string]api.MCPServerConfig) api.MCPClient {
	return mcp.NewMultiClient(clients, configs)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	rootCmd := newRootCmd()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		osExit(1)
	}
}

func newRootCmd() *cobra.Command {
	var f flags

	rootCmd := &cobra.Command{
		Use:   binaryName,
		Short: "A lightweight, fast AI coding CLI",
		Long: fmt.Sprintf(`%s is a native AI coding assistant that runs in your terminal.

It provides a fast, lightweight alternative to TypeScript-based AI CLI tools
with features like streaming LLM responses, built-in file tools, session
persistence, and MCP integration.`, binaryName),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), f)
		},
	}

	rootCmd.PersistentFlags().StringVarP(&f.configPath, "config", "c", "", "config file path")
	rootCmd.Flags().StringVarP(&f.model, "model", "m", "", "override LLM model")
	rootCmd.Flags().BoolVar(&f.yolo, "yolo", false, "auto-approve all tool calls")
	rootCmd.Flags().BoolVar(&f.continueLast, "continue", false, "resume last session in current directory")
	rootCmd.Flags().StringVar(&f.sessionID, "session", "", "resume specific session by ID")
	rootCmd.Flags().StringVarP(&f.prompt, "prompt", "p", "", "run a single prompt non-interactively and print the response")
	rootCmd.Flags().StringVar(&f.outputFormat, "output-format", outputFormatText, "output format for non-interactive mode: text or json")
	rootCmd.PersistentFlags().BoolVar(&f.debug, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().StringVar(&f.pprofAddr, "pprof", "", "enable runtime profiling server on address (e.g. localhost:6060)")

	rootCmd.AddCommand(newExportCmd(&f))
	rootCmd.AddCommand(newImportCmd(&f))
	rootCmd.AddCommand(newDoctorCmd(&f))
	rootCmd.AddCommand(newACPCmd(&f))

	return rootCmd
}

func newACPCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "acp",
		Short: "Start an ACP server over stdio",
		Long:  "Start an Agent Client Protocol (ACP) server speaking JSON-RPC 2.0 over stdin/stdout.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runACP(cmd.Context(), *f)
		},
	}
}

func newExportCmd(f *flags) *cobra.Command {
	var sessionID, outPath string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a session to a JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(cmd.Context(), sessionID, outPath, *f)
		},
	}
	cmd.Flags().StringVarP(&sessionID, "session", "s", "", "session ID to export (required)")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "output file path (required)")
	_ = cmd.MarkFlagRequired("session")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newImportCmd(f *flags) *cobra.Command {
	var inPath string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a session from a JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.Context(), inPath, *f)
		},
	}
	cmd.Flags().StringVarP(&inPath, "input", "i", "", "input file path (required)")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

func newDoctorCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks on configuration and dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), f.configPath)
		},
	}
}

func run(ctx context.Context, f flags) error {
	// Ensure default config exists
	if err := writeDefaultConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write default config: %v\n", err)
	}

	// Load configuration
	cfg, err := loadConfig(f.configPath)
	if err != nil {
		return err
	}

	// Apply CLI overrides
	if f.model != "" {
		model := f.model
		if alias, ok := cfg.Models[f.model]; ok {
			model = alias.Model
		}
		cfg.LLM.Model = model
		cfg.DefaultModel = model
	}
	if f.pprofAddr != "" {
		cfg.PprofAddr = f.pprofAddr
	}

	// Resolve the effective provider and validate its API key. A leading '$'
	// means the value was intended to be resolved from an environment variable
	// but was not found, so treat it as missing.
	_, providerCfg, err := llm.ResolveProviderFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("resolve provider: %w", err)
	}
	if providerCfg.APIKey == "" || strings.HasPrefix(providerCfg.APIKey, "$") {
		return fmt.Errorf("LLM API key is not configured. Set it in ~/.config/kimi-lite/config.toml or via KIMI_LLM_API_KEY environment variable")
	}

	// Create application
	application, err := newApp(cfg, f.debug)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
	}()

	// Apply CLI flags
	if f.yolo {
		application.SetYolo(true)
	}

	// Resolve session
	if f.sessionID != "" && f.continueLast {
		return fmt.Errorf("--session and --continue are mutually exclusive")
	}

	var session *api.Session
	switch {
	case f.sessionID != "":
		session, err = application.ResumeSession(ctx, f.sessionID)
		if err != nil {
			return fmt.Errorf("resume session %s: %w", f.sessionID, err)
		}
	case f.continueLast:
		session, err = application.ContinueLastSession(ctx)
		if errors.Is(err, store.ErrSessionNotFound) {
			session, err = application.StartSession(ctx)
			if err != nil {
				return fmt.Errorf("start session: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("continue last session: %w", err)
		}
	default:
		session, err = application.StartSession(ctx)
		if err != nil {
			return fmt.Errorf("start session: %w", err)
		}
	}

	// Non-interactive prompt mode: skip TUI and stream one turn.
	if f.prompt != "" {
		return runPrompt(ctx, application, session, f)
	}

	// Print startup banner to stderr so stdout is left clean for tools that
	// might pipe the TUI output.
	resolvedModel, err := resolveModelFromConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve model (%v), using configured model\n", err)
		resolvedModel = cfg.LLM.Model
	}
	fmt.Fprintf(os.Stderr, "[%s] v%s | model: %s\n", binaryName, version, resolvedModel)
	if f.continueLast || f.sessionID != "" {
		fmt.Fprintf(os.Stderr, "[Resuming session %s (%s)]\n", session.ID, session.Path)
	}

	// Run the TUI
	return application.Run(ctx, session)
}

// loadConfig loads configuration from the given explicit path, or from the
// default locations. Fallback to the built-in default config is only allowed
// when no explicit path was provided.
func loadConfig(configPath string) (*api.Config, error) {
	loader := config.NewLoader()
	if configPath != "" {
		loader.SetConfigFile(configPath)
	}
	cfg, err := loader.Load()
	if err != nil {
		if configPath != "" {
			return nil, fmt.Errorf("load config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Warning: failed to load config (%v), using defaults\n", err)
		cfg, err = config.Default()
		if err != nil {
			return nil, fmt.Errorf("load default config: %w", err)
		}
	}
	return cfg, nil
}

// runACP starts the ACP server over stdin/stdout.
func runACP(ctx context.Context, f flags) error {
	// Ensure default config exists
	if err := writeDefaultConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write default config: %v\n", err)
	}

	// Load configuration
	cfg, err := loadConfig(f.configPath)
	if err != nil {
		return err
	}

	if f.pprofAddr != "" {
		cfg.PprofAddr = f.pprofAddr
	}

	// Resolve the effective provider and validate its API key. A leading '$'
	// means the value was intended to be resolved from an environment variable
	// but was not found, so treat it as missing.
	_, providerCfg, err := llm.ResolveProviderFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("resolve provider: %w", err)
	}
	if providerCfg.APIKey == "" || strings.HasPrefix(providerCfg.APIKey, "$") {
		return fmt.Errorf("LLM API key is not configured. Set it in ~/.config/kimi-lite/config.toml or via KIMI_LLM_API_KEY environment variable")
	}

	// Create application. The ACP server owns the app lifecycle and will close
	// it when Run returns, so do not register a second deferred Close here.
	application, err := newApp(cfg, f.debug)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}

	srv := acp.NewServer(application, nil)
	if cwd, err := os.Getwd(); err == nil {
		srv.SetAllowedRoot(cwd)
	}
	return srv.Run(ctx, os.Stdin, os.Stdout)
}

// runPrompt executes a single turn without the TUI and prints the result.
func runPrompt(ctx context.Context, application appRunner, session *api.Session, f flags) error {
	// Non-interactive mode cannot display approval dialogs; rely on the --yolo
	// flag or configured auto-approve list. Without them, approval requests are
	// surfaced as a fatal error below.

	if f.outputFormat != outputFormatText && f.outputFormat != outputFormatJSON {
		return fmt.Errorf("unsupported output format %q, expected %q or %q", f.outputFormat, outputFormatText, outputFormatJSON)
	}

	eventCh, err := application.RunTurn(ctx, session.ID, f.prompt)
	if err != nil {
		return fmt.Errorf("run turn: %w", err)
	}

	enc := json.NewEncoder(stdout)
	for event := range eventCh {
		switch event.Type {
		case api.TurnEventContent:
			if f.outputFormat == outputFormatJSON {
				if err := enc.Encode(promptEventJSON{
					Type:    "content",
					Content: event.Content,
				}); err != nil {
					return fmt.Errorf("encode content event: %w", err)
				}
				continue
			}
			if _, err := fmt.Fprint(stdout, event.Content); err != nil {
				return fmt.Errorf("write content: %w", err)
			}

		case api.TurnEventToolResult:
			if f.outputFormat == outputFormatJSON {
				if err := enc.Encode(promptEventJSON{
					Type:   "tool_result",
					Result: event.Result,
				}); err != nil {
					return fmt.Errorf("encode tool result event: %w", err)
				}
			}

		case api.TurnEventApprovalRequest:
			toolName := "unknown"
			if len(event.ToolCalls) > 0 {
				toolName = event.ToolCalls[0].Name
			}
			return fmt.Errorf("tool call %q requires approval; run with --yolo or use interactive mode", toolName)

		case api.TurnEventError:
			if f.outputFormat == outputFormatJSON {
				if err := enc.Encode(promptEventJSON{
					Type:  "error",
					Error: event.Error.Error(),
				}); err != nil {
					return fmt.Errorf("encode error event: %w", err)
				}
			}
			return fmt.Errorf("turn error: %w", event.Error)

		case api.TurnEventDone:
			if f.outputFormat == outputFormatJSON {
				if err := enc.Encode(promptEventJSON{Type: "done"}); err != nil {
					return fmt.Errorf("encode done event: %w", err)
				}
			}
		}
	}

	if f.outputFormat == outputFormatText {
		if _, err := fmt.Fprintln(stdout); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}
	return nil
}

// promptEventJSON is a JSON-serializable representation of a turn event for
// non-interactive prompt output.
type promptEventJSON struct {
	Type    string         `json:"type"`
	Content string         `json:"content,omitempty"`
	Error   string         `json:"error,omitempty"`
	Result  api.ToolResult `json:"result,omitempty"`
}

func runExport(ctx context.Context, sessionID, outPath string, f flags) error {
	if err := writeDefaultConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write default config: %v\n", err)
	}

	cfg, err := loadConfig(f.configPath)
	if err != nil {
		return err
	}
	if f.pprofAddr != "" {
		cfg.PprofAddr = f.pprofAddr
	}

	application, err := newApp(cfg, f.debug)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
	}()

	export, err := application.ExportSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("export session: %w", err)
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal export: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0600); err != nil {
		return fmt.Errorf("write export file: %w", err)
	}
	fmt.Printf("Exported session %s to %s\n", sessionID, outPath)
	return nil
}

func runImport(ctx context.Context, inPath string, f flags) error {
	if err := writeDefaultConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write default config: %v\n", err)
	}

	cfg, err := loadConfig(f.configPath)
	if err != nil {
		return err
	}
	if f.pprofAddr != "" {
		cfg.PprofAddr = f.pprofAddr
	}

	application, err := newApp(cfg, f.debug)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
	}()

	data, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("read import file: %w", err)
	}
	var export api.SessionExport
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("parse import file: %w", err)
	}
	sess, err := application.ImportSession(ctx, &export)
	if err != nil {
		return fmt.Errorf("import session: %w", err)
	}
	fmt.Printf("Imported session as %s (%s)\n", sess.ID, sess.Path)
	return nil
}

func runDoctor(ctx context.Context, configPath string) error {
	var issues []string

	// Config check
	loader := config.NewLoader()
	if configPath != "" {
		loader.SetConfigFile(configPath)
	}
	cfg, err := loader.Load()
	if err != nil {
		if configPath != "" {
			return fmt.Errorf("load config: %w", err)
		}
		fmt.Printf("[FAIL] Config: %v\n", err)
		issues = append(issues, "config")
		var err2 error
		cfg, err2 = config.Default()
		if err2 != nil {
			return fmt.Errorf("load default config: %w", err2)
		}
	} else {
		fmt.Println("[OK]   Config loaded")
	}

	// Database check
	dbDir := filepath.Dir(cfg.Session.DBPath)
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		fmt.Printf("[FAIL] DB directory: %v\n", err)
		issues = append(issues, "db-dir")
	} else {
		st, err := newStore(cfg.Session.DBPath)
		if err != nil {
			fmt.Printf("[FAIL] DB open: %v\n", err)
			issues = append(issues, "db-open")
		} else {
			_ = st.Close()
			fmt.Println("[OK]   Database accessible")
		}
	}

	// LLM API key check
	_, providerCfg, err := llm.ResolveProviderFromConfig(cfg)
	if err != nil {
		fmt.Printf("[FAIL] LLM provider: %v\n", err)
		issues = append(issues, "llm-provider")
	} else if providerCfg.APIKey == "" {
		fmt.Println("[FAIL] LLM API key: not configured")
		issues = append(issues, "llm-api-key")
	} else {
		fmt.Println("[OK]   LLM API key present")
		// Lightweight connectivity check. The client is short-lived; close it
		// promptly if the implementation supports it.
		client, err := newLLMClient(cfg, nil)
		if err != nil {
			fmt.Printf("[FAIL] LLM client: %v\n", err)
			issues = append(issues, "llm-client")
		} else {
			if closer, ok := client.(io.Closer); ok {
				defer func() { _ = closer.Close() }()
			}
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			_, err := client.Chat(cctx, []api.Message{{Role: api.RoleUser, Content: "ping"}}, nil)
			if err != nil {
				var apiErr *api.APIError
				if errors.As(err, &apiErr) {
					switch apiErr.StatusCode {
					case 401, 403:
						fmt.Printf("[FAIL] LLM auth: invalid API key\n")
						issues = append(issues, "llm-auth")
					case 400:
						fmt.Printf("[FAIL] LLM request: %s\n", apiErr.Body)
						issues = append(issues, "llm-request")
					default:
						fmt.Printf("[WARN] LLM connectivity: %v\n", err)
						issues = append(issues, "llm-connectivity")
					}
				} else {
					fmt.Printf("[WARN] LLM connectivity: %v\n", err)
					issues = append(issues, "llm-connectivity")
				}
			} else {
				fmt.Println("[OK]   LLM connectivity")
			}
		}
	}

	// MCP check (non-fatal)
	var mcpClient api.MCPClient
	if len(cfg.MCPServers) > 0 {
		clients := make(map[string]api.MCPClient, len(cfg.MCPServers))
		for name, serverCfg := range cfg.MCPServers {
			if !serverCfg.Enabled {
				continue
			}
			cli, err := newMCPClientFromServerConfig(serverCfg, nil)
			if err != nil {
				fmt.Printf("[WARN] MCP server %s: invalid config: %v\n", name, err)
				continue
			}
			clients[name] = cli
		}
		mcpClient = newMCPMultiClient(clients, cfg.MCPServers)
	} else {
		mcpClient = newMCPClient(cfg.MCP)
	}
	defer func() { _ = mcpClient.Close() }()

	mcpCtx, mcpCancel := context.WithTimeout(ctx, 10*time.Second)
	defer mcpCancel()
	if err := mcpClient.Connect(mcpCtx); err != nil {
		fmt.Printf("[WARN] MCP: %v\n", err)
	} else {
		fmt.Println("[OK]   MCP connected")
	}

	if len(issues) > 0 {
		fmt.Printf("\n%d issue(s) found\n", len(issues))
		return fmt.Errorf("health check failed")
	}
	fmt.Println("\nAll checks passed")
	return nil
}
