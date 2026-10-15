package slash

import "strings"

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
		Description: "Toggle mouse mode: wheel scrolling and drag-to-copy (on by default)",
		SubCommands: []Command{
			{
				Name:        "on",
				Description: "Enable wheel scrolling and in-app drag selection",
				Handler:     func(ctx *Context) Result { return setMouse(mouseOn) },
			},
			{
				Name:        "off",
				Description: "Disable mouse capture so the terminal's own selection works",
				Handler:     func(ctx *Context) Result { return setMouse(mouseOff) },
			},
			{
				Name:        "toggle",
				Description: "Toggle mouse mode on or off",
				Handler:     func(ctx *Context) Result { return setMouse("toggle") },
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
		"Hold Shift (Option in iTerm2) for the terminal's own selection, or /mouse off (Alt+M).\n" +
		"Keyboard scrollback (PgUp/PgDn, Shift+Up/Down) always works.")
}

func handleMouseRoot(ctx *Context) Result {
	args := strings.TrimSpace(ctx.Args)
	if args == "" {
		return setMouse("toggle")
	}
	if strings.EqualFold(args, "show") {
		return handleMouseShow()
	}
	switch strings.ToLower(strings.Fields(args)[0]) {
	case "on", "enable", "yes":
		return setMouse(mouseOn)
	case "off", "disable", "no":
		return setMouse(mouseOff)
	case "toggle":
		return setMouse("toggle")
	default:
		return InfoResult("Usage: /mouse <on|off|toggle>")
	}
}

// setMouse returns a result carrying MouseMode so the UI applies the live
// enable/disable. No persistence: mouse mode is on again at each launch.
func setMouse(mode string) Result {
	msg := "Mouse mode toggled."
	switch mode {
	case mouseOn:
		msg = "Mouse mode on: wheel scrolls, drag selects and copies."
	case mouseOff:
		msg = "Mouse mode off: terminal text selection, keyboard scrolling."
	}
	return Result{Handled: true, Messages: []string{msg}, MouseMode: mode}
}
