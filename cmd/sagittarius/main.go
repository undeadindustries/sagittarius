// Command sagittarius is the Sagittarius CLI — a Go port of the gemini-cli fork.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/undeadindustries/sagittarius/internal/agent"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/grill"
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
		return runInteractive(*screenReader, *debug || *debugShort, opts)
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
	wd, _ := os.Getwd()
	docs, err := config.LoadDocuments(wd)
	if err != nil {
		writeStartupError(err)
		return 1
	}
	settings := docs.Merged()

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

	runner, _, runtime, _, _, err := buildRunner(ctx, opts)
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

	runner, docs, runtime, sessID, baseProviderID, err := buildRunner(ctx, opts)
	if err != nil {
		writeStartupError(err)
		return 1
	}
	defer func() { _ = runtime.Close() }()

	providerLabel := "ready"
	if endpoint, epErr := provider.ResolveEndpointConfig(docs.Merged()); epErr == nil {
		providerLabel = config.ProviderDisplayID(endpoint.ProviderID)
	}

	app := agent.NewApp(agent.AppConfig{
		Runner:         runner,
		Runtime:        runtime,
		ProviderLabel:  providerLabel,
		Model:          runner.Model(),
		Loader:         docs.Loader(),
		Settings:       docs.Global,
		Documents:      docs,
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

// configureInteractiveLogging redirects slog away from stderr while the Bubble
// Tea alt-screen owns the terminal. Without this, any log line (e.g. a late
// cancel-path error) is written to stderr and overwrites the bottom row of the
// alt-screen, leaving stray artifacts like `error="...: context canceled"`.
// Logs go to ~/.sagittarius/logs/sagittarius.log; if that cannot be opened we
// fall back to io.Discard — never stderr — so the display stays clean.
func configureInteractiveLogging(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	var w io.Writer = io.Discard
	if dir, err := storage.EnsureGlobalHome(); err == nil {
		logsDir := filepath.Join(dir, "logs")
		if mkErr := os.MkdirAll(logsDir, 0o700); mkErr == nil {
			if f, openErr := os.OpenFile(filepath.Join(logsDir, "sagittarius.log"),
				os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); openErr == nil {
				w = f
			}
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
}

func runInteractive(screenReader bool, debug bool, opts runnerOptions) int {
	configureInteractiveLogging(debug)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runner, docs, runtime, sessID, baseProviderID, err := buildRunner(ctx, opts)
	if err != nil {
		writeStartupError(err)
		return 1
	}
	defer func() { _ = runtime.Close() }()

	needsOnboarding := agent.NeedsProviderSetup(ctx, docs.Merged())

	var endpoint provider.EndpointConfig
	var endpointErr error
	if !needsOnboarding {
		endpoint, endpointErr = provider.ResolveEndpointConfig(docs.Merged())
		if endpointErr != nil {
			writeStartupError(endpointErr)
			return 1
		}
	}

	providerLabel := "Setup required"
	if !needsOnboarding {
		providerLabel = config.ProviderDisplayID(endpoint.ProviderID)
	}

	app := agent.NewApp(agent.AppConfig{
		Runner:         runner,
		Runtime:        runtime,
		ProviderLabel:  providerLabel,
		Model:          runner.Model(),
		Loader:         docs.Loader(),
		Settings:       docs.Global,
		Documents:      docs,
		SessionID:      sessID,
		BaseProviderID: baseProviderID,
	})

	var notice string
	if !needsOnboarding {
		if genErr := runner.GeneratorError(); genErr != nil {
			notice = "⚠ " + genErr.Error()
		}
	}

	uiCfg := docs.Merged().UI()
	termUI := bubbletea.NewTerminal(ui.Options{
		ScreenReader:      screenReader,
		BannerTitle:       "Sagittarius",
		Version:           version.String(),
		InitialStatus:     app.Status(),
		Notice:            notice,
		ThemeName:         uiCfg.Theme,
		NoColor:           noColorEnv(),
		HideBanner:        uiCfg.HideBanner,
		HideTips:          uiCfg.HideTips,
		ShowThinking:      uiCfg.ShowThinking,
		NeedsOnboarding:   needsOnboarding,
		LoadedMemoryFiles: runner.LoadedMemoryFiles(),
		InitialScrollback: historyToScrollback(runner.History()),
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
func buildRunner(ctx context.Context, opts runnerOptions) (*agent.Runner, *config.Documents, *agent.Runtime, string, string, error) {
	modelOverride := opts.modelOverride
	interactive := opts.interactive
	resume := opts.resume

	wd, _ := os.Getwd() // empty on failure — LoadDocuments degrades gracefully
	docs, err := config.LoadDocuments(wd)
	if err != nil {
		return nil, nil, nil, "", "", err
	}

	// Clean up any stale mode overrides (e.g. from older unqualified configs)
	// on startup. This ensures the UI doesn't show invalid "gemini - qwen"
	// states when the user launches.
	if provider.PruneModeOverrides(docs.Global) {
		_ = docs.Save(config.ScopeGlobal)
	}
	if docs.Project != nil {
		if provider.PruneModeOverrides(docs.Project) {
			_ = docs.Save(config.ScopeProject)
		}
	}

	// settings is the merged view (project wins); dialogs still mutate docs.Global.
	settings := docs.Merged()

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
			return nil, nil, nil, "", "", endpointErr
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
			return nil, nil, nil, "", "", genErr
		}
	}

	sessID := persistentSessionID()

	allowFix, suggestVerify := resolveVerifyFlags(settings)

	runtime, err := agent.NewRuntime(ctx, agent.RuntimeConfig{
		Settings:      settings,
		ClientName:    "sagittarius",
		ClientVersion: version.String(),
		Trusted:       true,
		AllowFix:      allowFix,
	})
	if err != nil {
		return nil, nil, nil, "", "", err
	}

	// Resolve session (resume or fresh).
	var sessRecorder *session.Recorder
	var initialHistory []provider.Message
	var initialGrants []string
	var initialGoal *goal.Snapshot
	var initialGrill *grill.Snapshot

	projectRoot := wd
	if projectRoot == "" {
		// A failed working-directory lookup is fatal when the user explicitly
		// asked to resume — silently starting a fresh session would discard the
		// request. Otherwise it only disables recording, which we log and skip.
		if resume != "" {
			_ = runtime.Close()
			return nil, nil, nil, "", "", fmt.Errorf("resume session: resolve working directory: cannot determine cwd")
		}
		slog.Warn("session recording disabled: cannot determine working directory")
	}
	if projectRoot != "" && resume != "" {
		chatsDir, cdErr := session.ChatsDir(projectRoot)
		if cdErr != nil {
			_ = runtime.Close()
			return nil, nil, nil, "", "", fmt.Errorf("resume session: %w", cdErr)
		}
		sel := session.NewSelector(chatsDir, sessID)
		result, selErr := sel.ResolveSession(resume)
		if selErr != nil {
			_ = runtime.Close()
			return nil, nil, nil, "", "", fmt.Errorf("resume session: %w", selErr)
		}
		if id := strings.TrimSpace(result.Record.SessionID); id != "" {
			sessID = id
		}
		slog.Info("resuming session", "info", result.DisplayInfo)
		initialHistory = session.ConvertToProviderHistory(result.Record)
		initialGrants = result.Record.SessionGrants
		initialGoal = result.Record.Goal
		initialGrill = result.Record.Grill
		mgr, mgrErr := session.NewManagerForResume(projectRoot, sessID, result)
		if mgrErr != nil {
			slog.Warn("session recording disabled: cannot open recorder for resumed session", "err", mgrErr)
		} else {
			sessRecorder = mgr.Recorder()
		}
	} else if projectRoot != "" {
		hash := session.ProjectHash(projectRoot)
		chatsDir, cdErr := session.ChatsDir(projectRoot)
		if cdErr != nil {
			slog.Warn("session recording disabled: cannot resolve chats dir", "err", cdErr)
		} else {
			sessRecorder = session.NewRecorder(chatsDir, sessID, hash)
		}
	}

	// Resolve project-boundary + snapshot policy from the already-merged settings.
	boundary, snapMgr := resolveBoundaryAndSnapshots(settings, projectRoot, sessID)

	runner, err := agent.NewRunner(agent.RunnerConfig{
		Generator:               gen,
		Model:                   model,
		Interactive:             interactive,
		ApprovalMode:            opts.approvalMode,
		SessionRecorder:         sessRecorder,
		InitialHistory:          initialHistory,
		InitialSessionGrants:    initialGrants,
		InitialGoal:             initialGoal,
		InitialGrill:            initialGrill,
		Settings:                settings,
		InitialMode:             initialMode,
		ModelPinned:             modelPinned,
		ProjectBoundary:         boundary,
		Snapshotter:             snapMgr,
		AllowFix:                allowFix,
		SuggestVerifyAfterWrite: suggestVerify,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, nil, nil, "", "", err
	}
	// Build the context manager after the runner so its summarizer reads the
	// runner's live (mode-resolved) model rather than the startup default.
	runner.SetContextManager(agent.NewContextManager(settings, gen,
		runner.CompressionModel,
		runner.ActiveProviderID,
		func() string { return runner.InteractionMode().String() },
		sessID,
		runner.RecordUsage))
	if reg := runtime.Registry(); reg != nil {
		runner.SetRegistry(reg)
	}
	if genErr != nil {
		runner.SetGeneratorError(genErr)
	}
	return runner, docs, runtime, sessID, baseProviderID, nil
}

// resolveBoundaryAndSnapshots reads the boundary + snapshot policy from the
// already-merged settings and constructs the per-session snapshot manager.
// projectRoot may be "" (working dir unknown), in which case snapshots are
// disabled and the merged boundary flag still applies.
func resolveBoundaryAndSnapshots(merged *config.Settings, projectRoot, sessID string) (bool, *snapshot.Manager) {
	boundary := config.ProjectBoundaryEnforced(merged, nil)

	if projectRoot == "" || !config.SnapshotsEnabled(merged, nil) {
		return boundary, nil
	}

	mgr, err := snapshot.NewManager(projectRoot, sessID, snapshot.Options{
		MaxFileBytes: config.SnapshotMaxFileBytes(merged, nil),
	})
	if err != nil {
		slog.Warn("file snapshots disabled", "error", err)
		return boundary, nil
	}
	return boundary, mgr
}

// resolveVerifyFlags reads the verify-workflow flags from the already-merged
// settings. Both flags default to false.
func resolveVerifyFlags(merged *config.Settings) (allowFix, suggestAfterWrite bool) {
	return config.VerifyAllowFix(merged, nil),
		config.VerifySuggestAfterWrite(merged, nil)
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

// historyToScrollback converts a provider message history into ui.ScrollbackEntry
// blocks for seeding the TUI on startup (e.g. after --resume). Only text-bearing
// user and model turns are included; tool calls and tool responses are skipped.
func historyToScrollback(history []provider.Message) []ui.ScrollbackEntry {
	if len(history) == 0 {
		return nil
	}
	entries := make([]ui.ScrollbackEntry, 0, len(history))
	for _, msg := range history {
		var text string
		for _, p := range msg.Parts {
			if p.Text != "" {
				if text != "" {
					text += "\n"
				}
				text += p.Text
			}
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		role := ui.ScrollbackUser
		if msg.Role == provider.RoleModel {
			role = ui.ScrollbackAssistant
		}
		entries = append(entries, ui.ScrollbackEntry{Role: role, Text: text})
	}
	return entries
}

func writeStartupError(err error) {
	if errors.Is(err, credentials.ErrAPIKeyMissing) {
		fmt.Fprintln(os.Stderr, err.Error())
		return
	}
	fmt.Fprintln(os.Stderr, "sagittarius:", err.Error())
}
