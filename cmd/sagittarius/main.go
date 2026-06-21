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

	if query != "" {
		return runHeadless(query, modelOverride, resume, fmt_)
	}

	// With --resume but no prompt: open interactive mode on the resumed session.
	if shouldRunInteractive(fs) {
		return runInteractive(*screenReader, modelOverride, resume)
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
								"  To manually set up a worktree: git worktree add .gemini/worktrees/%s -b %s\n",
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

func runHeadless(prompt, modelOverride, resume string, fmt_ outputFormat) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runner, _, _, runtime, err := buildRunner(ctx, modelOverride, false, resume)
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

func runInteractive(screenReader bool, modelOverride, resume string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runner, loader, settings, runtime, err := buildRunner(ctx, modelOverride, true, resume)
	if err != nil {
		writeStartupError(err)
		return 1
	}
	defer func() { _ = runtime.Close() }()

	endpoint, err := provider.ResolveEndpointConfig(settings)
	if err != nil {
		writeStartupError(err)
		return 1
	}

	providerLabel := endpoint.ProviderID
	if def, ok := config.LookupBuiltInProvider(endpoint.ProviderID); ok {
		providerLabel = def.DisplayName
	}

	app := agent.NewApp(agent.AppConfig{
		Runner:        runner,
		Runtime:       runtime,
		ProviderLabel: providerLabel,
		Model:         runner.Model(),
		Loader:        loader,
		Settings:      settings,
		SessionID:     persistentSessionID(),
	})

	var notice string
	if genErr := runner.GeneratorError(); genErr != nil {
		notice = "⚠ " + genErr.Error()
	}

	termUI := bubbletea.NewTerminal(ui.Options{
		ScreenReader:  screenReader,
		BannerTitle:   "Sagittarius",
		Version:       version.String(),
		InitialStatus: app.Status(),
		Notice:        notice,
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

// buildRunner constructs a Runner, optionally loading a resumed session.
func buildRunner(ctx context.Context, modelOverride string, interactive bool, resume string) (*agent.Runner, *config.Loader, *config.Settings, *agent.Runtime, error) {
	settings, loader, err := loadSettings()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	endpoint, err := provider.ResolveEndpointConfig(settings)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	model := endpoint.Model
	modelPinned := modelOverride != ""
	if modelOverride != "" {
		model = modelOverride
	}

	gen, genErr := provider.NewContentGenerator(ctx, settings)
	if genErr != nil {
		if !interactive || !errors.Is(genErr, credentials.ErrAPIKeyMissing) {
			return nil, nil, nil, nil, genErr
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
		return nil, nil, nil, nil, err
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
			return nil, nil, nil, nil, fmt.Errorf("resume session: resolve working directory: %w", wdErr)
		}
		slog.Warn("session recording disabled: cannot determine working directory", "err", wdErr)
	} else if resume != "" {
		chatsDir, cdErr := session.ChatsDir(projectRoot)
		if cdErr != nil {
			_ = runtime.Close()
			return nil, nil, nil, nil, fmt.Errorf("resume session: %w", cdErr)
		}
		sel := session.NewSelector(chatsDir, sessID)
		result, selErr := sel.ResolveSession(resume)
		if selErr != nil {
			_ = runtime.Close()
			return nil, nil, nil, nil, fmt.Errorf("resume session: %w", selErr)
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

	runner, err := agent.NewRunner(agent.RunnerConfig{
		Generator:       gen,
		Model:           model,
		Interactive:     interactive,
		SessionRecorder: sessRecorder,
		InitialHistory:  initialHistory,
		Settings:        settings,
		InitialMode:     modes.DefaultFromSettings(settings),
		ModelPinned:     modelPinned,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, nil, nil, nil, err
	}
	// Build the context manager after the runner so its summarizer reads the
	// runner's live (mode-resolved) model rather than the startup default.
	runner.SetContextManager(agent.NewContextManager(settings, gen, runner.Model, sessID))
	if reg := runtime.Registry(); reg != nil {
		runner.SetRegistry(reg)
	}
	if genErr != nil {
		runner.SetGeneratorError(genErr)
	}
	return runner, loader, settings, runtime, nil
}

// persistentSessionID returns a stable per-process identifier.
// It is reused for context-manager state, session file naming, and offload dirs.
func persistentSessionID() string {
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
