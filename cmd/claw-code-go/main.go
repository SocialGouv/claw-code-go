package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/SocialGouv/claw-code-go/hooks"
	"github.com/SocialGouv/claw-code-go/internal/auth"
	"github.com/SocialGouv/claw-code-go/internal/commands"
	"github.com/SocialGouv/claw-code-go/internal/compat"
	"github.com/SocialGouv/claw-code-go/internal/config"
	"github.com/SocialGouv/claw-code-go/internal/permissions"
	"github.com/SocialGouv/claw-code-go/internal/plugins"
	"github.com/SocialGouv/claw-code-go/internal/runtime"
	"github.com/SocialGouv/claw-code-go/internal/strutil"
	"github.com/SocialGouv/claw-code-go/internal/tui"
	"github.com/SocialGouv/claw-code-go/plugin"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// Build-time variables set via ldflags (e.g., -X main.version=v1.0.0).
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	// Route diagnostic subcommands before flag parsing.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dump-manifests":
			compat.RunDumpManifests(os.Args[2:])
			return
		case "bootstrap-plan":
			compat.RunBootstrapPlan(os.Args[2:])
			return
		case "print-system-prompt":
			compat.RunPrintSystemPrompt(os.Args[2:])
			return
		case "resume-session":
			compat.RunResumeSession(os.Args[2:])
			return
		case "timeline":
			compat.RunTimeline(os.Args[2:])
			return
		case "plugin":
			rest := os.Args[2:]
			if len(rest) == 0 {
				fmt.Fprintln(os.Stderr, "Usage: claw-code-go plugin install --marketplace <url> <name>")
				os.Exit(1)
			}
			switch rest[0] {
			case "install":
				compat.RunPluginInstall(rest[1:])
				return
			default:
				fmt.Fprintf(os.Stderr, "claw-code-go plugin: unknown subcommand %q (expected: install)\n", rest[0])
				os.Exit(1)
			}
		}
	}

	promptFlag := flag.String("prompt", "", "Run a single prompt and exit")
	modelFlag := flag.String("model", "", "Override the model to use")
	replFlag := flag.Bool("repl", false, "Run in interactive REPL mode (default when no --prompt)")
	sessionFlag := flag.String("session", "", "Session ID to load")
	sessionDirFlag := flag.String("session-dir", "", "Directory to store sessions")
	permModeFlag := flag.String("permission-mode", "default", "Permission mode: default, accept-edits, bypass, plan")
	dangerouslySkipPerms := flag.Bool("dangerously-skip-permissions", false, "Skip all permission checks (DANGER: grants full system access)")
	allowedToolsFlag := flag.String("allowed-tools", "", "Comma-separated list of tools to allow (filter tool registry)")
	resumeFlag := flag.String("resume", "", "Resume a previous session (session ID, 'latest', 'last', or path)")
	printFlag := flag.Bool("print", false, "Single-shot non-interactive output mode")
	compactFlag := flag.Bool("compact", false, "Enable compact output format")
	taskFlag := flag.String("task", "", "Task name for pre-configured task mode")
	noSaveFlag := flag.Bool("no-save", false, "Don't save session to disk")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose output")
	quietFlag := flag.Bool("quiet", false, "Suppress non-essential output")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	workDirFlag := flag.String("work-dir", "", "Set working directory")
	baseCommitFlag := flag.String("base-commit", "", "Base commit for diff context")
	reasoningEffortFlag := flag.String("reasoning-effort", "", "Reasoning effort level (low, medium, high, xhigh, max; xhigh/max are model-dependent)")
	outputFormatFlag := flag.String("output-format", "", "Output format: text (default), json, stream-json")
	systemPromptFlag := flag.String("system-prompt", "", "Replace the built-in base prompt (identity, posture, behavioral sections); context sections keep their toggles")
	appendSystemPromptFlag := flag.String("append-system-prompt", "", "Append text at the end of the assembled system prompt")
	minimalPromptFlag := flag.Bool("minimal-prompt", false, "Disable all automatic system-prompt sections (context, memory, posture, MCP list) — small-model mode")
	promptSectionsFlag := flag.String("prompt-sections", "", "Comma-separated prompt sections to enable EXCLUSIVELY (implies --minimal-prompt). Sections: "+strings.Join(runtime.PromptSectionNames(), ", "))
	disablePromptSectionsFlag := flag.String("disable-prompt-sections", "", "Comma-separated prompt sections to disable (others keep their defaults)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: claw-code-go [subcommand] [options]\n\n")
		fmt.Fprintf(os.Stderr, "Subcommands:\n")
		fmt.Fprintf(os.Stderr, "  dump-manifests [--src <dir>] [--json]   List tools, slash commands, and source manifest\n")
		fmt.Fprintf(os.Stderr, "  bootstrap-plan [--json]                 Print the ordered startup phase plan\n")
		fmt.Fprintf(os.Stderr, "  print-system-prompt [--cwd] [--date]    Render the full system prompt\n")
		fmt.Fprintf(os.Stderr, "  resume-session <file> [commands...]     Replay a saved session file\n")
		fmt.Fprintf(os.Stderr, "  timeline --session <id> [...flags]      Render a saved session timeline\n")
		fmt.Fprintf(os.Stderr, "  plugin install --marketplace <url> <n>  Fetch + verify + install a plugin from a remote marketplace\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment variables:\n")
		fmt.Fprintf(os.Stderr, "  ANTHROPIC_API_KEY        Anthropic API key (takes precedence over stored credentials)\n")
		fmt.Fprintf(os.Stderr, "  OPENAI_API_KEY           OpenAI API key (takes precedence over stored credentials)\n")
		fmt.Fprintf(os.Stderr, "  ANTHROPIC_MODEL          Model to use (default: %s)\n", runtime.DefaultModel)
		fmt.Fprintf(os.Stderr, "  ANTHROPIC_BASE_URL       Base URL for the Anthropic API\n")
		fmt.Fprintf(os.Stderr, "  CLAUDE_CODE_USE_BEDROCK  Set to 1 to use AWS Bedrock (env-var fallback)\n")
		fmt.Fprintf(os.Stderr, "  CLAUDE_CODE_USE_VERTEX   Set to 1 to use Google Vertex AI (env-var fallback)\n")
		fmt.Fprintf(os.Stderr, "  CLAUDE_CODE_USE_FOUNDRY  Set to 1 to use Azure AI Foundry (env-var fallback)\n")
	}

	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		return
	}

	if *workDirFlag != "" {
		if err := os.Chdir(*workDirFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot change to directory %s: %v\n", *workDirFlag, err)
			os.Exit(1)
		}
	}

	cfg := runtime.LoadConfig()

	if *modelFlag != "" {
		cfg.Model = *modelFlag
	}
	if *sessionDirFlag != "" {
		cfg.SessionDir = *sessionDirFlag
	}
	if *compactFlag {
		cfg.Compact = true
	}
	// *replFlag is accepted for compatibility but REPL is the default.
	_ = replFlag
	if *verboseFlag {
		cfg.Verbose = true
	}
	if *quietFlag {
		cfg.Quiet = true
	}
	if *noSaveFlag {
		cfg.NoSave = true
	}
	if *taskFlag != "" {
		cfg.Task = *taskFlag
	}
	if *baseCommitFlag != "" {
		cfg.BaseCommit = *baseCommitFlag
	}
	if *reasoningEffortFlag != "" {
		cfg.ReasoningEffort = *reasoningEffortFlag
	}
	if *outputFormatFlag != "" {
		cfg.OutputFormat = *outputFormatFlag
	}

	// --dangerously-skip-permissions overrides permission mode to full access.
	if *dangerouslySkipPerms {
		cfg.PermissionMode = "danger-full-access"
	}

	if *systemPromptFlag != "" {
		cfg.SystemPrompt = *systemPromptFlag
	}
	if *appendSystemPromptFlag != "" {
		cfg.AppendSystemPrompt = *appendSystemPromptFlag
	}

	// Prompt-section overrides (highest precedence, after settings + frontmatter).
	if *minimalPromptFlag || *promptSectionsFlag != "" || *disablePromptSectionsFlag != "" {
		only := strutil.SplitComma(*promptSectionsFlag)
		disable := strutil.SplitComma(*disablePromptSectionsFlag)
		if err := runtime.ApplyPromptSectionOverrides(cfg, *minimalPromptFlag, only, disable); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	// --allowed-tools filters the tool registry.
	if *allowedToolsFlag != "" {
		tools := strings.Split(*allowedToolsFlag, ",")
		for i, t := range tools {
			tools[i] = strings.TrimSpace(t)
		}
		cfg.AllowedTools = tools
	}

	// Resolve credentials using the multi-provider credential store.
	// Env vars take precedence (ANTHROPIC_API_KEY, OPENAI_API_KEY).
	// Falls back gracefully so the TUI can start and prompt the user to /login.
	provider, token, authMethod, credErr := auth.ResolveCredentials()
	if credErr == nil {
		cfg.ProviderName = provider
		cfg.AuthMethod = authMethod
		if authMethod == "oauth" {
			cfg.OAuthToken = token
		} else {
			cfg.APIKey = token
		}
	} else {
		// No credentials found — start with NoAuthClient so the TUI still opens.
		// The user can run /login inside the TUI.
		fmt.Fprintf(os.Stderr, "Note: no credentials found (%v).\n", credErr)
		fmt.Fprintln(os.Stderr, "      Use /login in the TUI to authenticate.")
	}

	// Create the provider client (or a no-auth placeholder).
	realClient, clientErr := runtime.NewProviderClient(cfg)
	if clientErr != nil {
		fmt.Fprintf(os.Stderr, "Note: could not create %s client: %v\n", cfg.ProviderName, clientErr)
		fmt.Fprintln(os.Stderr, "      Use /login in the TUI to authenticate.")
		realClient = runtime.NewNoAuthClient()
	}

	loop := runtime.NewConversationLoop(cfg, realClient)
	// Wire build-time info so LoopAdapter.Version()/Commit() return real values.
	loop.BuildVersion = version
	loop.BuildCommit = commit

	// Marketplace plugin manager: opt-in via CLAW_MARKETPLACE_URL. When
	// set, /store install|uninstall|list|search resolves against this
	// catalog. We deliberately keep this off by default — the public
	// marketplace contract is still in flux and we don't want users
	// downloading from a placeholder endpoint.
	if mktURL := strings.TrimSpace(os.Getenv("CLAW_MARKETPLACE_URL")); mktURL != "" {
		if pluginDir, err := plugins.DefaultPluginDir(); err == nil {
			loop.MarketplaceMgr = plugins.NewManager(pluginDir, mktURL)
		}
	}

	// Bootstrap wires hooks, plugins, telemetry, and permissions in the correct order.
	bootstrap(cfg, loop, *permModeFlag)

	// Load session from --session or --resume flags.
	sessionToLoad := *sessionFlag
	if sessionToLoad == "" && *resumeFlag != "" {
		sessionToLoad = *resumeFlag
	}
	if sessionToLoad != "" {
		// Handle special values: "latest", "last", "recent" resolve to most recent session.
		if sessionToLoad == "latest" || sessionToLoad == "last" || sessionToLoad == "recent" {
			metas, err := runtime.ListSessionsWithMeta(cfg.SessionDir)
			if err == nil && len(metas) > 0 {
				sessionToLoad = metas[0].ID
			} else {
				fmt.Fprintf(os.Stderr, "Warning: no sessions found to resume\n")
				sessionToLoad = ""
			}
		}
		if sessionToLoad != "" {
			sess, err := runtime.LoadSession(cfg.SessionDir, sessionToLoad)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load session %s: %v\n", sessionToLoad, err)
			} else {
				loop.Session = sess
				fmt.Printf("Loaded session: %s\n", sess.ID)
			}
		}
	}

	// Single prompt (non-interactive) mode — no TUI, plain stdout streaming.
	// --print flag uses the same non-interactive path as --prompt.
	if *printFlag && *promptFlag == "" {
		// --print without --prompt reads from stdin (future), for now just exit.
		fmt.Fprintln(os.Stderr, "Error: --print requires --prompt or stdin input.")
		os.Exit(1)
	}
	if *promptFlag != "" {
		if credErr != nil {
			fmt.Fprintln(os.Stderr, "Error: cannot use --prompt without valid credentials.")
			fmt.Fprintln(os.Stderr, "Set ANTHROPIC_API_KEY or OPENAI_API_KEY, or run the TUI and use /login.")
			os.Exit(1)
		}
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Fprintln(os.Stdout, "\nInterrupted. Saving session...")
			maybeSaveSession(cfg, loop)
			os.Exit(0)
		}()

		ctx := context.Background()
		if err := loop.SendMessage(ctx, *promptFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		maybeSaveSession(cfg, loop)
		return
	}

	// Interactive TUI mode.
	runTUI(cfg, loop)
}

// bootstrap initializes the runtime subsystems in the correct order:
//  1. Permission manager (from CLI flags + config)
//  2. MCP servers (from config)
//  3. Telemetry sink and session tracer
//  4. Plugin discovery, registry, and tool wiring
//  5. Hooks (from config + plugin-provided hooks)
//  6. Plugin lifecycle shutdown is deferred to cleanup
//
// Each subsystem is initialized with nil-guards so that a minimal config
// (no hooks, no plugins, no telemetry) works without panics.
func bootstrap(cfg *runtime.Config, loop *runtime.ConversationLoop, permModeFlag string) {
	// 1. Permission manager.
	resolvedPermMode := cfg.PermissionMode
	if permModeFlag != "default" {
		resolvedPermMode = permModeFlag
	}
	permMode, err := permissions.ParsePermissionMode(resolvedPermMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v; using default mode\n", err)
		permMode = permissions.ModeDefault
	}
	cfg.PermissionMode = permMode.String()

	ruleset, rErr := permissions.LoadRuleset(".claude/settings.json")
	if rErr != nil {
		ruleset = &permissions.Ruleset{}
	}
	if len(cfg.AllowedTools) > 0 || len(cfg.BlockedTools) > 0 {
		extra := permissions.RulesetFromLists(cfg.AllowedTools, cfg.BlockedTools)
		ruleset.Rules = append(ruleset.Rules, extra.Rules...)
	}
	loop.PermManager = permissions.NewManager(permMode, ruleset)

	// 2. MCP servers.
	loop.InitMCPFromConfig(context.Background())

	// 3. Telemetry — JSONL when directory configured, NopSink fallback.
	telemetryPath := resolveTelemetryPath(cfg)
	loop.InitTelemetry(telemetryPath)

	// 4. Extract feature config for hooks and plugins.
	var featureCfg config.RuntimeFeatureConfig
	if s := config.Load(); len(s.RawJSON) > 0 {
		featureCfg = config.ExtractFeatureConfig(s.RawJSON)
	}

	// 5. Plugin discovery and initialization.
	var pluginRegistry *plugin.PluginRegistry
	homeDir, _ := os.UserHomeDir()
	configHome := filepath.Join(homeDir, ".claw-code")
	pluginMgr, pmErr := plugin.NewPluginManager(plugin.PluginManagerConfig{
		ConfigHome:     configHome,
		BundledRoot:    cfg.PluginBundledRoot,
		InstallRoot:    cfg.PluginInstallRoot,
		ExternalDirs:   cfg.PluginExternalDirs,
		EnabledPlugins: cfg.EnabledPlugins,
	})
	if pmErr != nil {
		fmt.Fprintf(os.Stderr, "[plugins] warning: plugin manager init failed: %v\n", pmErr)
	} else {
		discovered, failures := pluginMgr.DiscoverPlugins()
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "[plugins] warning: failed to load %s: %v\n", f.PluginRoot, f.Err)
		}
		if len(discovered) > 0 {
			pluginRegistry = plugin.NewPluginRegistry(discovered)
		}
	}
	loop.InitPlugins(pluginRegistry)

	// 6. Hooks — config hooks merged with plugin-provided hooks.
	hookCfg := hooks.HookConfig{
		PreToolUse:         featureCfg.Hooks.PreToolUse,
		PostToolUse:        featureCfg.Hooks.PostToolUse,
		PostToolUseFailure: featureCfg.Hooks.PostToolUseFailure,
	}
	loop.InitHooksFromConfig(hookCfg)
}

// resolveTelemetryPath returns the JSONL telemetry file path, or "" to disable.
func resolveTelemetryPath(cfg *runtime.Config) string {
	// Use CLAUDE_CODE_TELEMETRY_DIR env var if set.
	if dir := os.Getenv("CLAUDE_CODE_TELEMETRY_DIR"); dir != "" {
		return filepath.Join(dir, "events.jsonl")
	}
	// Default: ~/.claw-code/telemetry/events.jsonl
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".claw-code", "telemetry", "events.jsonl")
}

// runTUI starts the Bubble Tea TUI for interactive use.
func runTUI(cfg *runtime.Config, loop *runtime.ConversationLoop) {
	// Register slash commands — fully initialized with all categories.
	registry := commands.NewFullRegistry()
	// Auto-load CLAUDE.md slash commands from the cwd up to the
	// filesystem root. Errors are non-fatal: a malformed CLAUDE.md
	// shouldn't block the TUI from booting.
	if cwd, err := os.Getwd(); err == nil {
		_ = commands.LoadClaudeMdCommands(registry, cwd)
		// Also load project slash commands from .claude/commands/*.md (the
		// Claude Code project-commands convention). Non-fatal like the above.
		_ = commands.LoadDirCommands(registry, cwd)
	}
	loop.CommandRegistry = registry

	// Save session on SIGTERM (Ctrl+C is handled by Bubble Tea itself).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		<-sigCh
		shutdownPlugins(loop)
		maybeSaveSession(cfg, loop)
		os.Exit(0)
	}()

	model := tui.NewModel(cfg, loop)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	// Shutdown plugins and save session after the TUI exits.
	shutdownPlugins(loop)
	maybeSaveSession(cfg, loop)
}

// shutdownPlugins gracefully shuts down the plugin registry if present.
func shutdownPlugins(loop *runtime.ConversationLoop) {
	if loop.PluginRegistry != nil {
		if err := loop.PluginRegistry.Shutdown(); err != nil {
			fmt.Fprintf(os.Stderr, "[plugins] warning: shutdown error: %v\n", err)
		}
	}
}

// maybeSaveSession saves the session unless --no-save is active.
func maybeSaveSession(cfg *runtime.Config, loop *runtime.ConversationLoop) {
	if cfg.NoSave {
		return
	}
	if err := runtime.SaveSession(cfg.SessionDir, loop.Session); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save session: %v\n", err)
	}
}
