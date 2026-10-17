package slash

import (
	"os"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/clipboard"
)

// Canonical /mouse modes. The slash layer never imports the bubbletea/charm
// libraries, so it only validates the mode string and hands it to the UI via
// Result.MouseMode; the live enable/disable runs in the bubbletea layer.
const (
	mouseOn  = "on"
	mouseOff = "off"
)

func mouseCommand() Command {
	return Command{
		Name:        "mouse",
		Description: "Toggle mouse mode: wheel scrolling and drag-to-copy (on by default; on/off is remembered)",
		SubCommands: []Command{
			{
				Name:        "on",
				Description: "Enable wheel scrolling and in-app drag selection (remembered)",
				Handler:     func(ctx *Context) Result { return setMouse(ctx, mouseOn) },
			},
			{
				Name:        "off",
				Description: "Disable mouse capture so the terminal's own selection works (remembered)",
				Handler:     func(ctx *Context) Result { return setMouse(ctx, mouseOff) },
			},
			{
				Name:        "toggle",
				Description: "Toggle mouse mode on or off",
				Handler:     func(ctx *Context) Result { return setMouse(ctx, "toggle") },
			},
			{
				Name:        "show",
				Description: "Show how mouse scrolling and text selection interact",
				Handler:     func(ctx *Context) Result { return handleMouseShow() },
			},
		},
		Handler: handleMouseRoot,
	}
}

func handleMouseShow() Result {
	return InfoResult("Mouse mode is on by default: the wheel scrolls the conversation, " +
		"and click-drag selects text and copies it on release\n" +
		"(dragging to the top or bottom edge scrolls; wrapped lines are rejoined).\n" +
		"For the terminal's own selection, select with " + clipboard.NativeSelectionHint(os.Getenv) + ", or run /mouse off (Alt+M).\n" +
		"Keyboard scrollback (PgUp/PgDn, Shift+Up/Down) always works.")
}

func handleMouseRoot(ctx *Context) Result {
	args := strings.TrimSpace(ctx.Args)
	if args == "" {
		return setMouse(ctx, "toggle")
	}
	if strings.EqualFold(args, "show") {
		return handleMouseShow()
	}
	switch strings.ToLower(strings.Fields(args)[0]) {
	case "on", "enable", "yes":
		return setMouse(ctx, mouseOn)
	case "off", "disable", "no":
		return setMouse(ctx, mouseOff)
	case "toggle":
		return setMouse(ctx, "toggle")
	default:
		return InfoResult("Usage: /mouse <on|off|toggle>")
	}
}

// setMouse returns a result carrying MouseMode so the UI applies the live
// enable/disable. on and off are saved globally (the next full-screen launch
// starts that way). toggle lasts for this session only. Hooks is nil in tests
// and the command still switches the live mode.
func setMouse(ctx *Context, mode string) Result {
	if ctx != nil && ctx.Deps.Hooks != nil && (mode == mouseOn || mode == mouseOff) {
		if err := ctx.Deps.Hooks.SetUIMouse(mode == mouseOn); err != nil {
			return ErrorResult(err)
		}
	}
	msg := "Mouse mode toggled for this session."
	switch mode {
	case mouseOn:
		msg = "Mouse mode on: wheel scrolls, drag selects and copies. Remembered for the next launch."
	case mouseOff:
		msg = "Mouse mode off: terminal text selection, keyboard scrolling. Remembered for the next launch."
	}
	return Result{Handled: true, Messages: []string{msg}, MouseMode: mode}
}
