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
		Description: "Toggle mouse-wheel scrolling (off enables native text selection)",
		SubCommands: []Command{
			{
				Name:        "on",
				Description: "Enable mouse-wheel scrolling (hold Shift to select text)",
				Handler:     func(ctx *Context) Result { return setMouse(mouseOn) },
			},
			{
				Name:        "off",
				Description: "Disable mouse capture so native text selection works",
				Handler:     func(ctx *Context) Result { return setMouse(mouseOff) },
			},
			{
				Name:        "toggle",
				Description: "Toggle mouse-wheel scrolling on or off",
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
	return InfoResult("Mouse-wheel scrolling is off by default so the terminal's " +
		"native text selection works.\nUse /mouse on (or Alt+M) to enable wheel " +
		"scrolling; hold Shift to select text while it is on.\n" +
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
// enable/disable. No persistence: mouse capture resets to off on each launch.
func setMouse(mode string) Result {
	msg := "Mouse scroll toggled."
	switch mode {
	case mouseOn:
		msg = "Mouse scroll on (hold Shift to select text)."
	case mouseOff:
		msg = "Mouse scroll off (text selection enabled)."
	}
	return Result{Handled: true, Messages: []string{msg}, MouseMode: mode}
}
