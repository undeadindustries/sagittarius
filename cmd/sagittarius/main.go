// Command sagittarius is the Sagittarius CLI — a Go port of the gemini-cli fork.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/term"

	"github.com/undeadindustries/sagittarius/internal/agent"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/snapshot"
	"github.com/undeadindustries/sagittarius/internal/storage"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/bubbletea"
	"github.com/undeadindustries/sagittarius/internal/version"
)

// outputFormat controls the headless output encoding.
type outputFormat string

const (
	outputFormatText       outputFormat = "text"
	outputFormatJSON       outputFormat = "json"
	outputFormatStreamJSON outputFormat = "stream-json"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	args = normalizeResumeArgs(args)

	fs := flag.NewFlagSet("sagittarius", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	showVersion := fs.Bool("version", false, "print version and exit")
	showVersionShort := fs.Bool("v", false, "print version and exit")
	screenReader := fs.Bool("screen-reader", false, "plain terminal mode for screen readers (reduced TUI)")
	prompt := fs.String("prompt", "", "non-interactive prompt; writes streamed text to stdout")
	promptShort := fs.String("p", "", "shorthand for --prompt")
	modelFlag := fs.String("model", "", "override active provider model")
	modelShort := fs.String("m", "", "shorthand for --model")
	debug := fs.Bool("debug", false, "enable debug logging")
	debugShort := fs.Bool("d", false, "shorthand for --debug")

	// Approval policy (fork-aligned) and Sagittarius interaction mode overrides.
	approvalModeFlag := fs.String("approval-mode", "", "tool approval policy: default|autoEdit|yolo")
	yoloFlag := fs.Bool("yolo", false, "auto-approve all tools (shorthand for --approval-mode=yolo)")
	yoloShort := fs.Bool("y", false, "shorthand for --yolo")
	modeFlag := fs.String("mode", "", "interaction mode for this run: agent|plan|ask|debug")
	slashFlag := fs.String("slash", "", "run a single slash command headlessly (e.g. \"/mode show\", \"/diff\") and exit")

	// Phase 13: session management flags.
	resumeFlag := fs.String("resume", "", "resume a session by id, index, or 'latest' (omit value for latest)")
	resumeShort := fs.String("r", "", "shorthand for --resume")
	listSessions := fs.Bool("list-sessions", false, "list available sessions for the current project and exit")
	deleteSession := fs.String("delete-session", "", "delete a session by id or index and exit")
	outputFmt := fs.String("output-format", "text", "headless output format: text|json|stream-json")
	worktreeFlag := fs.String("worktree", "", "start in an isolated git worktree (experimental; requires experimental.worktrees: true in settings)")
	worktreeShort := fs.String("w", "", "shorthand for --worktree")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Ensure the global home (~/.sagittarius) exists on first run. Best-effort:
	// a failure here should not block --version or interactive use.
	if _, err := storage.EnsureGlobalHome(); err != nil {
		slog.Warn("could not create sagittarius home directory", "error", err)
	}

	if *showVersion || *showVersionShort {
		fmt.Println(version.String())
		return 0
	}

	if *debug || *debugShort {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	// Worktree flag: stub — check experimental gate but don't execute.
	worktree := strings.TrimSpace(*worktreeFlag)
	if worktree == "" {
		worktree = strings.TrimSpace(*worktreeShort)
	}
	if worktree != "" {
		return runWorktreeStub(worktree)
	}

	// --list-sessions: print and exit.
	if *listSessions {
		return runListSessions()
	}

	// --delete-session: delete and exit.
	if *deleteSession != "" {
		return runDeleteSession(*deleteSession)
	}

	query := strings.TrimSpace(*prompt)
	if query == "" {
		query = strings.TrimSpace(*promptShort)
	}
	// Support positional argument as prompt (e.g. sagittarius "hello").
	if query == "" && fs.NArg() > 0 {
		query = strings.TrimSpace(fs.Arg(0))
	}

	modelOverride := strings.TrimSpace(*modelFlag)
	if modelOverride == "" {
		modelOverride = strings.TrimSpace(*modelShort)
	}

	resume := strings.TrimSpace(*resumeFlag)
	if resume == "" {
		resume = strings.TrimSpace(*resumeShort)
	}

	// Resolve approval policy: --yolo/-y is shorthand for --approval-mode=yolo and
	// the two cannot be combined (matches fork config validation).
	yolo := *yoloFlag || *yoloShort
	approvalMode := agent.ApprovalDefault
	if yolo && strings.TrimSpace(*approvalModeFlag) != "" {
		fmt.Fprintln(os.Stderr, "sagittarius: cannot use both --yolo and --approval-mode together (use --approval-mode=yolo)")
		return 2
	}
	if yolo {
		approvalMode = agent.ApprovalYolo
	} else if raw := strings.TrimSpace(*approvalModeFlag); raw != "" {
		parsed, err := agent.ParseApprovalMode(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sagittarius:", err)
			return 2
		}
		approvalMode = parsed
	}

	// Resolve optional interaction-mode override. When unset, buildRunner falls
	// back to sagittarius.defaultMode (then agent).
	var modeOverride *modes.Mode
	if raw := strings.TrimSpace(*modeFlag); raw != "" {
		parsed, err := modes.ParseMode(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sagittarius:", err)
			return 2
		}
		modeOverride = &parsed
	}

	// Normalise output format.
	fmt_ := outputFormat(strings.ToLower(strings.TrimSpace(*outputFmt)))
	if fmt_ == "" {
		fmt_ = outputFormatText
	}
	switch fmt_ {
	case outputFormatText, outputFormatJSON, outputFormatStreamJSON:
	default:
		fmt.Fprintf(os.Stderr, "sagittarius: unknown --output-format %q (use: text|json|stream-json)\n", fmt_)
		return 2
	}

	opts := runnerOptions{
		modelOverride: modelOverride,
		resume:        resume,
		approvalMode:  approvalMode,
		modeOverride:  modeOverride,
	}

	// --slash: run a single slash command headlessly and exit. Mutually
	// exclusive with a prompt (the two are different headless entry points).
	if slashCmd := strings.TrimSpace(*slashFlag); slashCmd != "" {
		if query != "" {
			fmt.Fprintln(os.Stderr, "sagittarius: --slash cannot be combined with a prompt (-p)")
			return 2
		}
		opts.interactive = false
		return runSlash(slashCmd, opts)
	}

	if query != "" {
		opts.interactive = false
		return runHeadless(query, opts, fmt_)
	}

	// With --resume but no prompt: open interactive mode on the resumed session.
	if shouldRunInteractive(fs) {
		opts.interactive = true
		return runInteractive(*screenReader, opts)
	}

	fmt.Fprintln(os.Stderr, "sagittarius: interactive mode requires a terminal (stdin and stdout must be TTYs)")
	fmt.Fprintln(os.Stderr, "  try: sagittarius -p \"your prompt\"")
	return 1
}

// normalizeResumeArgs rewrites a bare --resume/-r (one with no value: either the
// last argument or immediately followed by another flag) into the explicit
// --resume=latest form. The stdlib flag package cannot express an optional-value
// flag, so without this a bare --resume would either fail parsing or swallow the
// next token. The space-separated value forms the fork accepts (`-r 1`,
// `--resume <uuid>`, `-r latest "query"`) are left untouched for normal parsing.
// Arguments after a "--" terminator are copied verbatim.
func normalizeResumeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		if a == "--resume" || a == "-r" {
			bare := i == len(args)-1 || strings.HasPrefix(args[i+1], "-")
			if bare {
				out = append(out, a+"="+session.ResumeLatest)
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func shouldRunInteractive(fs *flag.FlagSet) bool {
	if fs.NArg() > 0 {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// runListSessions prints all sessions for the current project and exits.
func runListSessions() int {
	projectRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius: cannot determine working directory:", err)
		return 1
	}
	chatsDir, err := session.ChatsDir(projectRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius:", err)
		return 1
	}
	infos, err := session.ListSessions(chatsDir, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius: list sessions:", err)
		return 1
	}
	fmt.Print(session.FormatSessionList(infos))
	return 0
}

// runDeleteSession deletes a session by id or index and exits.
func runDeleteSession(identifier string) int {
	projectRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius: cannot determine working directory:", err)
		return 1
	}
	chatsDir, err := session.ChatsDir(projectRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius:", err)
		return 1
	}

	sel := session.NewSelector(chatsDir, "")
	info, err := sel.Find(identifier)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius:", err)
		return 1
	}

	if err := session.DeleteSession(chatsDir, info.FileName); err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius: delete session:", err)
		return 1
	}
	fmt.Printf("Deleted session %d: %s (%s)\n",
		info.Index,
		info.FirstUserMessage,
		session.FormatRelativeTime(info.LastUpdated),
	)
	return 0
}

// runWorktreeStub handles the --worktree flag. It checks the experimental gate
// in settings and fails with a clear error if the feature is not enabled.
// Full git worktree setup is deferred to a later phase (AD-020).
func runWorktreeStub(name string) int {
	_, loader, err := loadSettings()
	if err != nil {
		writeStartupError(err)
		return 1
	}
	settings, err := loader.Load()
	if err != nil && !errors.Is(err, config.ErrSecretsInSettings) {
		writeStartupError(fmt.Errorf("load settings: %w", err))
		return 1
	}

	// Check experimental.worktrees in raw settings JSON.
	if settings != nil {
		if expRaw, ok := settings.Raw["experimental"]; ok {
			var exp map[string]interface{}
			if json.Unmarshal(expRaw, &exp) == nil {
				if v, ok := exp["worktrees"]; ok {
					if enabled, ok := v.(bool); ok && enabled {
						fmt.Fprintf(os.Stderr,
							"sagittarius: --worktree is experimental and not yet fully implemented.\n"+
								"  The flag is accepted but git worktree creation is deferred (see AD-020).\n"+
								"  To manually set up a worktree: git worktree add .sagittarius/worktrees/%s -b %s\n",
							name, name,
						)
						return 1
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr,
		"sagittarius: --worktree requires experimental.worktrees: true in settings.json\n"+
			"  Add: { \"experimental\": { \"worktrees\": true } }\n",
	)
	return 1
}

func runHeadless(prompt string, opts runnerOptions, fmt_ outputFormat) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runner, _, _, runtime, _, _, err := buildRunner(ctx, opts)
	if err != nil {
		writeStartupError(err)
		return 1
	}
	defer func() { _ = runtime.Close() }()

	switch fmt_ {
	case outputFormatJSON:
		return runHeadlessJSON(ctx, runner, prompt, false)
	case outputFormatStreamJSON:
		return runHeadlessJSON(ctx, runner, prompt, true)
	default:
		if err := runner.RunHeadless(ctx, prompt, os.Stdout); err != nil {
			if errors.Is(err, context.Canceled) {
				return 130
			}
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		return 0
	}
}

// runSlash executes a single slash command headlessly and exits. StreamInfo
// (and any text) output goes to stdout; handler errors go to stderr. Slash
// commands that open an interactive TUI dialog (e.g. bare /providers, /models)
// cannot run without a terminal: they print a clear message and exit 2.
func runSlash(command string, opts runnerOptions) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if !strings.HasPrefix(command, "/") {
		command = "/" + command
	}

	runner, loader, settings, runtime, sessID, baseProviderID, err := buildRunner(ctx, opts)
	if err != nil {
		writeStartupError(err)
		return 1
	}
	defer func() { _ = runtime.Close() }()

	providerLabel := "ready"
	if endpoint, epErr := provider.ResolveEndpointConfig(settings); epErr == nil {
		providerLabel = endpoint.ProviderID
		if def, ok := config.LookupBuiltInProvider(endpoint.ProviderID); ok {
			providerLabel = def.DisplayName
		}
	}

	app := agent.NewApp(agent.AppConfig{
		Runner:         runner,
		Runtime:        runtime,
		ProviderLabel:  providerLabel,
		Model:          runner.Model(),
		Loader:         loader,
		Settings:       settings,
		SessionID:      sessID,
		BaseProviderID: baseProviderID,
	})

	events, err := app.HandleInput(ctx, command)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sagittarius:", err)
		return 1
	}

	exit := 0
	for ev := range events {
		switch ev.Type {
		case ui.StreamInfo, ui.StreamTextDelta:
			fmt.Print(ev.Text)
		case ui.StreamError:
			switch {
			case ev.Err != nil:
				fmt.Fprintln(os.Stderr, "sagittarius:", ev.Err.Error())
			case ev.Text != "":
				fmt.Fprintln(os.Stderr, "sagittarius:", ev.Text)
			}
			exit = 1
		case ui.StreamOpenDialog:
			fmt.Fprintf(os.Stderr, "sagittarius: %q requires an interactive TUI and cannot run headlessly\n", command)
			exit = 2
		}
	}
	return exit
}

// runHeadlessJSON emits events as JSON. streaming=true writes one JSON object
// per line (stream-json); streaming=false writes a single JSON object at the end.
func runHeadlessJSON(ctx context.Context, runner *agent.Runner, prompt string, streaming bool) int {
	events, err := runner.RunTurn(ctx, prompt)
	if err != nil {
		writeJSONError(err, streaming)
		return 1
	}

	var textBuf strings.Builder

	for ev := range events {
		select {
		case <-ctx.Done():
			return 130
		default:
		}

		switch ev.Type {
		case ui.StreamTextDelta:
			if streaming {
				emitJSONLine(map[string]string{"type": "text", "text": ev.Text})
			} else {
				textBuf.WriteString(ev.Text)
			}
		case ui.StreamToolStart:
			if streaming {
				emitJSONLine(map[string]string{"type": "tool_start", "tool": ev.ToolName})
			}
		case ui.StreamToolResult:
			if streaming {
				emitJSONLine(map[string]string{"type": "tool_result", "tool": ev.ToolName, "text": ev.Text})
			}
		case ui.StreamInfo:
			if streaming {
				emitJSONLine(map[string]string{"type": "info", "text": ev.Text})
			}
		case ui.StreamError:
			if ev.Err != nil {
				writeJSONError(ev.Err, streaming)
				return 1
			}
			writeJSONError(fmt.Errorf("%s", ev.Text), streaming)
			return 1
		case ui.StreamDone:
			if !streaming {
				emitJSONLine(map[string]string{"type": "text", "text": textBuf.String()})
			}
			return 0
		}
	}
	return 0
}

// noColorEnv reports whether the NO_COLOR convention (any non-empty value)
// requests monochrome output. See https://no-color.org.
func noColorEnv() bool {
	return os.Getenv("NO_COLOR") != ""
}

func emitJSONLine(v interface{}) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func writeJSONError(err error, streaming bool) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	emitJSONLine(map[string]string{"type": "error", "error": msg})
}

func runInteractive(screenReader bool, opts runnerOptions) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runner, loader, settings, runtime, sessID, baseProviderID, err := buildRunner(ctx, opts)
	if err != nil {
		writeStartupError(err)
		return 1
	}
	defer func() { _ = runtime.Close() }()

	needsOnboarding := agent.NeedsProviderSetup(ctx, settings)

	var endpoint provider.EndpointConfig
	var endpointErr error
	if !needsOnboarding {
		endpoint, endpointErr = provider.ResolveEndpointConfig(settings)
		if endpointErr != nil {
			writeStartupError(endpointErr)
			return 1
		}
	}

	providerLabel := "Setup required"
	if !needsOnboarding {
		providerLabel = endpoint.ProviderID
		if def, ok := config.LookupBuiltInProvider(endpoint.ProviderID); ok {
			providerLabel = def.DisplayName
		}
	}

	app := agent.NewApp(agent.AppConfig{
		Runner:         runner,
		Runtime:        runtime,
		ProviderLabel:  providerLabel,
		Model:          runner.Model(),
		Loader:         loader,
		Settings:       settings,
		SessionID:      sessID,
		BaseProviderID: baseProviderID,
	})

	var notice string
	if !needsOnboarding {
		if genErr := runner.GeneratorError(); genErr != nil {
			notice = "⚠ " + genErr.Error()
		}
	}

	uiCfg := settings.UI()
	termUI := bubbletea.NewTerminal(ui.Options{
		ScreenReader:    screenReader,
		BannerTitle:     "Sagittarius",
		Version:         version.String(),
		InitialStatus:   app.Status(),
		Notice:          notice,
		ThemeName:       uiCfg.Theme,
		NoColor:         noColorEnv(),
		HideBanner:      uiCfg.HideBanner,
		HideTips:        uiCfg.HideTips,
		NeedsOnboarding: needsOnboarding,
	})

	if err := termUI.Run(ctx, app); err != nil {
		if ctx.Err() != nil {
			return 0
		}
		slog.Error("interactive session failed", "error", err)
		return 1
	}
	return 0
}

// runnerOptions carries the resolved CLI inputs buildRunner needs. Zero values
// are valid: empty modelOverride/resume disable those features, approvalMode
// defaults to ApprovalDefault, and a nil modeOverride falls back to the settings
// default mode.
type runnerOptions struct {
	modelOverride string
	interactive   bool
	resume        string
	approvalMode  agent.ApprovalMode
	modeOverride  *modes.Mode
}

// buildRunner constructs a Runner, optionally loading a resumed session.
// The returned session ID keys snapshots, context-manager state, and UI telemetry.
// baseProviderID is the canonical provider id that was active before any mode-driven
// startup override; callers should seed App.BaseProviderID with it.
func buildRunner(ctx context.Context, opts runnerOptions) (*agent.Runner, *config.Loader, *config.Settings, *agent.Runtime, string, string, error) {
	modelOverride := opts.modelOverride
	interactive := opts.interactive
	resume := opts.resume

	settings, loader, err := loadSettings()
	if err != nil {
		return nil, nil, nil, nil, "", "", err
	}
	if wd, wdErr := os.Getwd(); wdErr == nil {
		if mergeErr := config.MergeProjectSystemPrompt(settings, wd); mergeErr != nil {
			slog.Warn("could not merge project system prompt", "error", mergeErr)
		}
	}

	needsSetup := interactive && agent.NeedsProviderSetup(ctx, settings)

	// Resolve the initial mode early — before endpoint/generator — so that a
	// provider-qualified mode override drives the endpoint and generator build
	// from the start. This fixes the startup-routing gap where headless and
	// fresh interactive launches would use the wrong backend when a mode with
	// a provider override was selected.
	initialMode := modes.DefaultFromSettings(settings)
	if opts.modeOverride != nil {
		initialMode = *opts.modeOverride
	}
	baseProviderID := config.NormalizeProviderID(settings.ActiveProvider())
	if !needsSetup {
		if modeProvider, _ := modes.ResolveModeOverride(initialMode, settings.Sagittarius); modeProvider != "" {
			modeProvider = config.NormalizeProviderID(modeProvider)
			if modeProvider != baseProviderID {
				// Switch in-memory only (never persisted) so endpoint + generator
				// reflect the mode's provider at startup without touching settings.json.
				_ = provider.SetActiveProvider(settings, modeProvider)
			}
		}
	}

	var endpoint provider.EndpointConfig
	var endpointErr error
	if !needsSetup {
		endpoint, endpointErr = provider.ResolveEndpointConfig(settings)
		if endpointErr != nil {
			return nil, nil, nil, nil, "", "", endpointErr
		}
	}

	model := agent.PlaceholderModel()
	if !needsSetup {
		model = endpoint.Model
	}
	modelPinned := modelOverride != ""
	if modelOverride != "" {
		model = modelOverride
	}

	gen, genErr := provider.NewContentGenerator(ctx, settings)
	if genErr != nil {
		if !needsSetup && (!interactive || !errors.Is(genErr, credentials.ErrAPIKeyMissing)) {
			return nil, nil, nil, nil, "", "", genErr
		}
	}

	sessID := persistentSessionID()

	runtime, err := agent.NewRuntime(ctx, agent.RuntimeConfig{
		Settings:      settings,
		ClientName:    "sagittarius",
		ClientVersion: version.String(),
		Trusted:       true,
	})
	if err != nil {
		return nil, nil, nil, nil, "", "", err
	}

	// Resolve session (resume or fresh).
	var sessRecorder *session.Recorder
	var initialHistory []provider.Message

	projectRoot, wdErr := os.Getwd()
	if wdErr != nil {
		// A failed working-directory lookup is fatal when the user explicitly
		// asked to resume — silently starting a fresh session would discard the
		// request. Otherwise it only disables recording, which we log and skip.
		if resume != "" {
			_ = runtime.Close()
			return nil, nil, nil, nil, "", "", fmt.Errorf("resume session: resolve working directory: %w", wdErr)
		}
		slog.Warn("session recording disabled: cannot determine working directory", "err", wdErr)
	} else if resume != "" {
		chatsDir, cdErr := session.ChatsDir(projectRoot)
		if cdErr != nil {
			_ = runtime.Close()
			return nil, nil, nil, nil, "", "", fmt.Errorf("resume session: %w", cdErr)
		}
		sel := session.NewSelector(chatsDir, sessID)
		result, selErr := sel.ResolveSession(resume)
		if selErr != nil {
			_ = runtime.Close()
			return nil, nil, nil, nil, "", "", fmt.Errorf("resume session: %w", selErr)
		}
		if id := strings.TrimSpace(result.Record.SessionID); id != "" {
			sessID = id
		}
		slog.Info("resuming session", "info", result.DisplayInfo)
		initialHistory = session.ConvertToProviderHistory(result.Record)
		mgr, mgrErr := session.NewManagerForResume(projectRoot, sessID, result)
		if mgrErr != nil {
			slog.Warn("session recording disabled: cannot open recorder for resumed session", "err", mgrErr)
		} else {
			sessRecorder = mgr.Recorder()
		}
	} else {
		hash := session.ProjectHash(projectRoot)
		chatsDir, cdErr := session.ChatsDir(projectRoot)
		if cdErr != nil {
			slog.Warn("session recording disabled: cannot resolve chats dir", "err", cdErr)
		} else {
			sessRecorder = session.NewRecorder(chatsDir, sessID, hash)
		}
	}

	// Resolve project-boundary + snapshot policy (project settings override
	// global) and build the per-session snapshot manager when enabled.
	boundary, snapMgr := resolveBoundaryAndSnapshots(settings, projectRoot, sessID)

	runner, err := agent.NewRunner(agent.RunnerConfig{
		Generator:       gen,
		Model:           model,
		Interactive:     interactive,
		ApprovalMode:    opts.approvalMode,
		SessionRecorder: sessRecorder,
		InitialHistory:  initialHistory,
		Settings:        settings,
		InitialMode:     initialMode,
		ModelPinned:     modelPinned,
		ProjectBoundary: boundary,
		Snapshotter:     snapMgr,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, nil, nil, nil, "", "", err
	}
	// Build the context manager after the runner so its summarizer reads the
	// runner's live (mode-resolved) model rather than the startup default.
	runner.SetContextManager(agent.NewContextManager(settings, gen, runner.CompressionModel, sessID, runner.RecordUsage))
	if reg := runtime.Registry(); reg != nil {
		runner.SetRegistry(reg)
	}
	if genErr != nil {
		runner.SetGeneratorError(genErr)
	}
	return runner, loader, settings, runtime, sessID, baseProviderID, nil
}

// resolveBoundaryAndSnapshots merges the project settings.json over the global
// one for the boundary + snapshot policy and constructs the snapshot manager
// when snapshots are enabled. projectRoot may be "" (working dir unknown), in
// which case snapshots are disabled and the global boundary flag still applies.
func resolveBoundaryAndSnapshots(global *config.Settings, projectRoot, sessID string) (bool, *snapshot.Manager) {
	var projectSettings *config.Settings
	if projectRoot != "" {
		ps, err := config.LoadProjectSettings(projectRoot)
		if err != nil {
			slog.Warn("could not load project settings", "error", err)
		} else {
			projectSettings = ps
		}
	}

	boundary := config.ProjectBoundaryEnforced(global, projectSettings)

	if projectRoot == "" || !config.SnapshotsEnabled(global, projectSettings) {
		return boundary, nil
	}

	mgr, err := snapshot.NewManager(projectRoot, sessID, snapshot.Options{
		MaxFileBytes: config.SnapshotMaxFileBytes(global, projectSettings),
	})
	if err != nil {
		slog.Warn("file snapshots disabled", "error", err)
		return boundary, nil
	}
	return boundary, mgr
}

// persistentSessionID returns a stable per-process identifier.
// It is reused for context-manager state, session file naming, and offload dirs.
// persistentSessionID returns the session id used for recording and snapshots.
// SAGITTARIUS_SESSION_ID pins it across separate invocations so a headless write
// and a later `--slash /diff` or `--slash /undo` (or a resumed session) share the
// same snapshot index. When unset it defaults to a per-process id.
func persistentSessionID() string {
	if id := strings.TrimSpace(os.Getenv("SAGITTARIUS_SESSION_ID")); id != "" {
		return id
	}
	return fmt.Sprintf("sagittarius-%d", os.Getpid())
}

func loadSettings() (*config.Settings, *config.Loader, error) {
	loader, err := config.NewLoader()
	if err != nil {
		return nil, nil, fmt.Errorf("load settings: %w", err)
	}
	settings, err := loader.Load()
	if err != nil && !errors.Is(err, config.ErrSecretsInSettings) {
		return nil, nil, fmt.Errorf("load settings: %w", err)
	}
	return settings, loader, nil
}

func writeStartupError(err error) {
	if errors.Is(err, credentials.ErrAPIKeyMissing) {
		fmt.Fprintln(os.Stderr, err.Error())
		return
	}
	fmt.Fprintln(os.Stderr, "sagittarius:", err.Error())
}
