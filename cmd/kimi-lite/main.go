// Command kimi-lite is the AI coding CLI application.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

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

type flags struct {
	configPath   string
	model        string
	yolo         bool
	autoApprove  bool
	continueLast bool
	sessionID    string
	debug        bool
}

// appRunner is the interface for the application layer, allowing mocking in tests.
type appRunner interface {
	SetYolo(bool)
	SetAutoApprove(bool)
	ResumeSession(ctx context.Context, id string) (*api.Session, error)
	ContinueLastSession(ctx context.Context) (*api.Session, error)
	StartSession(ctx context.Context) (*api.Session, error)
	Run(ctx context.Context, session *api.Session) error
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

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	rootCmd := newRootCmd()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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

	rootCmd.Flags().StringVarP(&f.configPath, "config", "c", "", "config file path")
	rootCmd.Flags().StringVarP(&f.model, "model", "m", "", "override LLM model")
	rootCmd.Flags().BoolVar(&f.yolo, "yolo", false, "auto-approve all tool calls")
	rootCmd.Flags().BoolVar(&f.autoApprove, "auto", false, "auto-approve read-only tools (default)")
	rootCmd.Flags().BoolVar(&f.continueLast, "continue", false, "resume last session in current directory")
	rootCmd.Flags().StringVar(&f.sessionID, "session", "", "resume specific session by ID")
	rootCmd.Flags().BoolVar(&f.debug, "debug", false, "enable debug logging")

	rootCmd.AddCommand(newExportCmd())
	rootCmd.AddCommand(newImportCmd())
	rootCmd.AddCommand(newDoctorCmd())

	return rootCmd
}

func newExportCmd() *cobra.Command {
	var sessionID, outPath string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a session to a JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(cmd.Context(), sessionID, outPath)
		},
	}
	cmd.Flags().StringVarP(&sessionID, "session", "s", "", "session ID to export (required)")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "output file path (required)")
	_ = cmd.MarkFlagRequired("session")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newImportCmd() *cobra.Command {
	var inPath string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a session from a JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.Context(), inPath)
		},
	}
	cmd.Flags().StringVarP(&inPath, "input", "i", "", "input file path (required)")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks on configuration and dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context())
		},
	}
}

func run(ctx context.Context, f flags) error {
	// Ensure default config exists
	_ = writeDefaultConfig()

	// Load configuration
	loader := config.NewLoader()
	if f.configPath != "" {
		loader.SetConfigFile(f.configPath)
	}
	cfg, err := loader.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config (%v), using defaults\n", err)
		cfg = config.DefaultConfig()
	}

	// Apply CLI overrides
	if f.model != "" {
		cfg.LLM.Model = f.model
	}

	// Validate API key
	if cfg.LLM.APIKey == "" {
		return fmt.Errorf("LLM API key is not configured. Set it in ~/.config/kimi-lite/config.toml or via KIMI_LLM_API_KEY environment variable")
	}

	// Create application
	application, err := newApp(cfg, f.debug)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer func() {
		_ = application.Close()
	}()

	// Apply CLI flags
	if f.yolo {
		application.SetYolo(true)
	}
	if f.autoApprove {
		application.SetAutoApprove(true)
	}

	// Resolve session
	var session *api.Session
	switch {
	case f.sessionID != "":
		session, err = application.ResumeSession(ctx, f.sessionID)
		if err != nil {
			return fmt.Errorf("resume session %s: %w", f.sessionID, err)
		}
	case f.continueLast:
		session, err = application.ContinueLastSession(ctx)
		if err != nil {
			// If no previous session, start a new one
			session, err = application.StartSession(ctx)
			if err != nil {
				return fmt.Errorf("start session: %w", err)
			}
		}
	default:
		session, err = application.StartSession(ctx)
		if err != nil {
			return fmt.Errorf("start session: %w", err)
		}
	}

	// Print startup banner
	fmt.Printf("[%s] v%s | model: %s | context: 0%%\n", binaryName, version, cfg.LLM.Model)
	if f.continueLast || f.sessionID != "" {
		fmt.Printf("[Resuming session %s (%s)]\n", session.ID, session.Path)
	}

	// Run the TUI
	return application.Run(ctx, session)
}

func runExport(ctx context.Context, sessionID, outPath string) error {
	cfg, err := config.NewLoader().Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config (%v), using defaults\n", err)
		cfg = config.DefaultConfig()
	}
	application, err := newApp(cfg, false)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer func() { _ = application.Close() }()

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

func runImport(ctx context.Context, inPath string) error {
	cfg, err := config.NewLoader().Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config (%v), using defaults\n", err)
		cfg = config.DefaultConfig()
	}
	application, err := newApp(cfg, false)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer func() { _ = application.Close() }()

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

func runDoctor(ctx context.Context) error {
	var issues []string

	// Config check
	cfg, err := config.NewLoader().Load()
	if err != nil {
		fmt.Printf("[FAIL] Config: %v\n", err)
		issues = append(issues, "config")
		cfg = config.DefaultConfig()
	} else {
		fmt.Println("[OK]   Config loaded")
	}

	// Database check
	dbDir := filepath.Dir(cfg.Session.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		fmt.Printf("[FAIL] DB directory: %v\n", err)
		issues = append(issues, "db-dir")
	} else {
		st, err := store.NewSQLite(cfg.Session.DBPath)
		if err != nil {
			fmt.Printf("[FAIL] DB open: %v\n", err)
			issues = append(issues, "db-open")
		} else {
			_ = st.Close()
			fmt.Println("[OK]   Database accessible")
		}
	}

	// LLM API key check
	if cfg.LLM.APIKey == "" {
		fmt.Println("[FAIL] LLM API key: not configured")
		issues = append(issues, "llm-api-key")
	} else {
		fmt.Println("[OK]   LLM API key present")
		// Lightweight connectivity check
		client := llm.NewClient(cfg.LLM, nil)
		_, err := client.Chat(ctx, []api.Message{{Role: api.RoleUser, Content: "ping"}}, nil)
		if err != nil {
			fmt.Printf("[WARN] LLM connectivity: %v\n", err)
			issues = append(issues, "llm-connectivity")
		} else {
			fmt.Println("[OK]   LLM connectivity")
		}
	}

	// MCP check (non-fatal)
	mcpClient := mcp.NewClientFromConfig(cfg.MCP)
	if err := mcpClient.Connect(ctx); err != nil {
		fmt.Printf("[WARN] MCP: %v\n", err)
	} else {
		fmt.Println("[OK]   MCP connected")
		_ = mcpClient.Close()
	}

	if len(issues) > 0 {
		fmt.Printf("\n%d issue(s) found\n", len(issues))
		return fmt.Errorf("health check failed")
	}
	fmt.Println("\nAll checks passed")
	return nil
}
