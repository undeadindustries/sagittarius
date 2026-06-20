// Command sagittarius is the Sagittarius CLI — a Go port of the gemini-cli fork.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"golang.org/x/term"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/bubbletea"
	"github.com/undeadindustries/sagittarius/internal/ui/demo"
	"github.com/undeadindustries/sagittarius/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	showVersionShort := flag.Bool("v", false, "print version and exit")
	screenReader := flag.Bool("screen-reader", false, "plain terminal mode for screen readers (reduced TUI)")
	flag.Parse()

	if *showVersion || *showVersionShort {
		fmt.Println(version.String())
		os.Exit(0)
	}

	if shouldRunInteractive() {
		os.Exit(runInteractive(*screenReader))
	}

	fmt.Fprintln(os.Stderr, "sagittarius: interactive mode requires a terminal (stdin and stdout must be TTYs)")
	fmt.Fprintln(os.Stderr, "  try: ./bin/sagittarius --version")
	os.Exit(0)
}

func shouldRunInteractive() bool {
	if flag.NArg() > 0 {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func runInteractive(screenReader bool) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	termUI := bubbletea.NewTerminal(ui.Options{
		ScreenReader: screenReader,
		BannerTitle:  "Sagittarius",
		Version:      version.String(),
	})

	if err := termUI.Run(ctx, demo.EchoApp{}); err != nil {
		if ctx.Err() != nil {
			return 0
		}
		slog.Error("interactive session failed", "error", err)
		return 1
	}
	return 0
}
