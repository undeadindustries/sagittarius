package slash

import (
	"os"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/clipboard"
	"github.com/undeadindustries/sagittarius/internal/config"
)

const (
	// screenShow asks the UI to report the current layout without changing it.
	screenShow = "show"
	// screenHelp is the subcommand that explains the layouts. It is text only,
	// so it carries no ScreenMode and works the same in every session.
	screenHelp = "help"
)

func screenCommand() Command {
	return Command{
		Name:        "screen",
		Description: "Switch the terminal layout for this session: fullscreen or inline (not saved). /screen help explains the modes",
		SubCommands: []Command{
			{
				Name:        screenHelp,
				Description: "Explain what each layout is for and how they differ",
				Handler:     func(ctx *Context) Result { return helpScreen() },
			},
			{
				Name:        "fullscreen",
				Description: "Draw the conversation in place for this session",
				Handler:     func(ctx *Context) Result { return setScreen(ctx, config.UIScreenFullscreen) },
			},
			{
				Name:        "inline",
				Description: "Print finished output into the terminal scrollback for this session",
				Handler:     func(ctx *Context) Result { return setScreen(ctx, config.UIScreenInline) },
			},
			{
				Name:        "show",
				Description: "Show the current layout",
				Handler:     showScreen,
			},
		},
		Handler: handleScreenRoot,
	}
}

func handleScreenRoot(ctx *Context) Result {
	if screenReaderOn(ctx) {
		return screenReaderRefusal()
	}
	args := ""
	if ctx != nil {
		args = strings.TrimSpace(ctx.Args)
	}
	if args == "" || strings.EqualFold(args, screenShow) {
		return showScreen(ctx)
	}
	switch strings.ToLower(strings.Fields(args)[0]) {
	case screenHelp:
		return helpScreen()
	case config.UIScreenFullscreen, "full":
		return setScreen(ctx, config.UIScreenFullscreen)
	case config.UIScreenInline:
		return setScreen(ctx, config.UIScreenInline)
	default:
		return InfoResult("Usage: /screen <fullscreen|inline|help>")
	}
}

// helpScreen explains why each layout exists and recommends inline. It changes
// nothing, which is why it skips the screen-reader refusal: reading about the
// layouts is harmless in any session. The native-selection key comes from the
// same helper as /mouse show, so the advice names the key this terminal uses.
func helpScreen() Result {
	key := clipboard.NativeSelectionHint(os.Getenv)
	return InfoResult(
		"Screen layouts\n" +
			"\n" +
			"Inline (recommended)\n" +
			"  Finished output is printed into your terminal's own scrollback. The terminal handles scrolling, " +
			"search, and selection, so Cmd-C or Ctrl-Shift-C copies what you select. That works over SSH and in " +
			"Terminal.app with no clipboard setup. Prose keeps its line breaks and tool " +
			"cards keep their frames when you copy them. Dialogs such as /providers still open full screen.\n" +
			"  Trade-offs: printed history doesn't recolor if you change theme, and a streaming reply shows " +
			"only the tail that fits until it finishes.\n" +
			"\n" +
			"Full screen\n" +
			"  The conversation is drawn in place on the terminal's alternate screen, with in-app scrolling and " +
			"click-drag selection that copies clean source text. Copies go to the system clipboard, or over " +
			"OSC 52 on SSH, which Terminal.app ignores. To copy with the terminal itself, select with " + key +
			". The conversation doesn't stay in your terminal's scrollback after you quit.\n" +
			"\n" +
			"Auto (ui.screen = auto)\n" +
			"  Full screen on a local session. Over SSH it is inline, unless the terminal is known to accept " +
			"OSC 52 (kitty, WezTerm, Ghostty, foot). Over SSH inside tmux or GNU screen it is always inline.\n" +
			"\n" +
			"Switching\n" +
			"  /screen inline or /screen fullscreen changes this session only and keeps the conversation. " +
			"To start in a layout, run sagittarius --screen inline, or set Screen layout in /settings.",
	)
}

func showScreen(ctx *Context) Result {
	if screenReaderOn(ctx) {
		return screenReaderRefusal()
	}
	return Result{
		Handled: true,
		Messages: []string{
			"Full screen draws the conversation in place. You copy what is on screen with your terminal's selection key. " +
				"Inline prints finished output into the terminal scrollback, so you can scroll and select all of it. " +
				"/screen fullscreen or /screen inline changes this session and is not saved. /screen help explains the differences.",
		},
		ScreenMode: screenShow,
	}
}

func setScreen(ctx *Context, mode string) Result {
	if screenReaderOn(ctx) {
		return screenReaderRefusal()
	}
	msg := "Full screen for this session."
	if mode == config.UIScreenInline {
		msg = "Inline for this session. Finished output prints into the terminal scrollback. This choice is not saved."
	}
	return Result{Handled: true, Messages: []string{msg}, ScreenMode: mode}
}

func screenReaderOn(ctx *Context) bool {
	return ctx != nil && ctx.Deps.ScreenReader
}

func screenReaderRefusal() Result {
	return InfoResult("Screen reader mode uses the plain terminal. /screen does not change it.")
}
