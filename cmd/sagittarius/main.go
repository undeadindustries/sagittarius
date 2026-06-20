// Command sagittarius is the Sagittarius CLI — a Go port of the gemini-cli fork.
package main

import (
	"context"
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
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/bubbletea"
	"github.com/undeadindustries/sagittarius/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
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

	query := strings.TrimSpace(*prompt)
	if query == "" {
		query = strings.TrimSpace(*promptShort)
	}
	modelOverride := strings.TrimSpace(*modelFlag)
	if modelOverride == "" {
		modelOverride = strings.TrimSpace(*modelShort)
	}

	if query != "" {
		return runHeadless(query, modelOverride)
	}

	if shouldRunInteractive(fs) {
		return runInteractive(*screenReader, modelOverride)
	}

	fmt.Fprintln(os.Stderr, "sagittarius: interactive mode requires a terminal (stdin and stdout must be TTYs)")
	fmt.Fprintln(os.Stderr, "  try: sagittarius -p \"your prompt\"")
	return 1
}

func shouldRunInteractive(fs *flag.FlagSet) bool {
	if fs.NArg() > 0 {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func runHeadless(prompt, modelOverride string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runner, _, _, err := buildRunner(ctx, modelOverride, false)
	if err != nil {
		writeStartupError(err)
		return 1
	}

	if err := runner.RunHeadless(ctx, prompt, os.Stdout); err != nil {
		if errors.Is(err, context.Canceled) {
			return 130
		}
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	return 0
}

func runInteractive(screenReader bool, modelOverride string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runner, loader, settings, err := buildRunner(ctx, modelOverride, true)
	if err != nil {
		writeStartupError(err)
		return 1
	}

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
		ProviderLabel: providerLabel,
		Model:         runner.Model(),
		Loader:        loader,
		Settings:      settings,
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

func buildRunner(ctx context.Context, modelOverride string, interactive bool) (*agent.Runner, *config.Loader, *config.Settings, error) {
	settings, loader, err := loadSettings()
	if err != nil {
		return nil, nil, nil, err
	}

	endpoint, err := provider.ResolveEndpointConfig(settings)
	if err != nil {
		return nil, nil, nil, err
	}

	model := endpoint.Model
	if modelOverride != "" {
		model = modelOverride
	}

	gen, genErr := provider.NewContentGenerator(ctx, settings)
	if genErr != nil {
		// Interactive sessions open even without a usable provider so the user
		// can recover via /auth or /provider use. Headless mode still fails.
		if !(interactive && errors.Is(genErr, credentials.ErrAPIKeyMissing)) {
			return nil, nil, nil, genErr
		}
	}

	runner, err := agent.NewRunner(agent.RunnerConfig{
		Generator:   gen,
		Model:       model,
		Interactive: interactive,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if genErr != nil {
		runner.SetGeneratorError(genErr)
	}
	return runner, loader, settings, nil
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
