package slash

import "strings"

// Canonical TUI theme names. These are plain strings on purpose: the slash
// layer must not import internal/ui/theme (which pulls in lipgloss/charm), so
// theme validation stays here and theme.Resolve runs only in the bubbletea
// layer.
const (
	themeDefault   = "default"
	themeGreyscale = "greyscale"
)

// parseThemeName maps a user token (case-insensitive) to a canonical theme name,
// reporting ok=false for unrecognized input.
func parseThemeName(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "default", "normal", "color", "colour":
		return themeDefault, true
	case "greyscale", "grayscale", "mono", "monochrome", "none", "no-color", "nocolor":
		return themeGreyscale, true
	default:
		return "", false
	}
}

func themeCommand() Command {
	return Command{
		Name:        "theme",
		Description: "Show or switch the TUI color theme (default or greyscale)",
		SubCommands: []Command{
			{
				Name:        "show",
				Description: "Show the active TUI color theme",
				Handler:     handleThemeShow,
			},
			{
				Name:        "default",
				Description: "Switch to the default (colored) theme",
				Handler:     func(ctx *Context) Result { return setTheme(ctx, themeDefault) },
			},
			{
				Name:        "greyscale",
				Description: "Switch to the greyscale (monochrome) theme",
				Handler:     func(ctx *Context) Result { return setTheme(ctx, themeGreyscale) },
			},
		},
		Handler: handleThemeRoot,
	}
}

func handleThemeShow(ctx *Context) Result {
	name := themeDefault
	if ctx.Deps.Settings != nil {
		if t := ctx.Deps.Settings.UI().Theme; t != "" {
			name = t
		}
	}
	return InfoResult("Current theme: " + name + "\nUsage: /theme <default|greyscale>")
}

func handleThemeRoot(ctx *Context) Result {
	args := strings.TrimSpace(ctx.Args)
	if args == "" || strings.EqualFold(args, "show") {
		return handleThemeShow(ctx)
	}
	parts := strings.Fields(args)
	// Support a leading "set" token: /theme set greyscale.
	if strings.EqualFold(parts[0], "set") {
		if len(parts) < 2 {
			return InfoResult("Usage: /theme <default|greyscale>")
		}
		parts = parts[1:]
	}
	name, ok := parseThemeName(parts[0])
	if !ok {
		return InfoResult("Usage: /theme <default|greyscale>")
	}
	return setTheme(ctx, name)
}

// setTheme persists the chosen theme (when Hooks is present) and returns a
// result carrying ThemeName so the UI performs the live switch. Hooks is nil in
// tests and degrades gracefully by skipping persistence.
func setTheme(ctx *Context, name string) Result {
	if ctx.Deps.Hooks != nil {
		if err := ctx.Deps.Hooks.SetUITheme(name); err != nil {
			return ErrorResult(err)
		}
	}
	return Result{Handled: true, Messages: []string{"Theme set to " + name + "."}, ThemeName: name}
}
