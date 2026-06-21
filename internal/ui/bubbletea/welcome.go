package bubbletea

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// logoLines is the compact Sagittarius launch banner: an arrow in flight (the
// archer's motif) above the spaced wordmark. Original art — not a port of the
// fork's Gemini logos.
var logoLines = []string{
	"     · ✦ ·",
	"  »»»»»»———➤",
	"  S A G I T T A R I U S",
}

// logoGradient is a light→deep purple ramp applied per logo line in color mode.
var logoGradient = []string{"#D7AFFF", "#9B7EDE", "#6C5CE7"}

// welcomeTips mirrors the fork Tips component: the few commands a new user needs.
var welcomeTips = []string{
	"/help for a list of commands",
	"/providers and /models to choose your endpoint and model",
	"/mode or Ctrl+Shift+M to switch interaction modes",
	"Be specific in your requests for the best results",
}

// welcomeText composes the launch banner: ASCII logo, version, optional active
// provider/model line, tips, and any startup notice. The banner and tips are
// independently gated by ui.hideBanner / ui.hideTips.
func welcomeText(opts ui.Options, th theme.Theme) string {
	var sections []string

	if !opts.HideBanner {
		sections = append(sections, renderLogo(opts, th))
	} else {
		sections = append(sections, renderPlainTitle(opts, th))
	}

	if status := strings.TrimSpace(opts.InitialStatus.Left); status != "" {
		sections = append(sections, th.Secondary.Render(status))
	}

	if !opts.HideTips {
		sections = append(sections, renderTips(th))
	}

	if opts.Notice != "" {
		sections = append(sections, th.Warning.Render(opts.Notice))
	}

	return strings.Join(sections, "\n\n") + "\n\n"
}

func renderLogo(opts ui.Options, th theme.Theme) string {
	var b strings.Builder
	for i, line := range logoLines {
		style := th.Title
		if th.Colored && i < len(logoGradient) {
			style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(logoGradient[i]))
		}
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	b.WriteString(renderVersionLine(opts, th))
	return strings.TrimRight(b.String(), "\n")
}

func renderPlainTitle(opts ui.Options, th theme.Theme) string {
	title := opts.BannerTitle
	if title == "" {
		title = "Sagittarius"
	}
	return th.Title.Render(title) + "  " + renderVersionLine(opts, th)
}

func renderVersionLine(opts ui.Options, th theme.Theme) string {
	if opts.Version == "" {
		return th.Secondary.Render("the archer's CLI")
	}
	return th.Secondary.Render(opts.Version + " · the archer's CLI")
}

func renderTips(th theme.Theme) string {
	var b strings.Builder
	b.WriteString(th.Accent.Render("Tips for getting started:"))
	for _, tip := range welcomeTips {
		b.WriteString("\n")
		b.WriteString(th.Secondary.Render("  • " + tip))
	}
	return b.String()
}
