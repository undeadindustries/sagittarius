package bubbletea

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// newWorkingSpinner builds the Braille-dot spinner used for the working/thinking
// indicator. MiniDot matches gemini-cli's "dots" frames (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏), so the
// motion cue is familiar.
func newWorkingSpinner(th theme.Theme) spinner.Model {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	s.Style = th.Accent
	return s
}

// renderWorkingLine renders the animated working indicator shown above the input
// while the agent is busy (e.g. "⠹ Thinking…"). label is the current activity
// (e.g. "Thinking…" or "Running write_file").
func renderWorkingLine(s spinner.Model, label string, th theme.Theme, width int) string {
	line := s.View() + " " + th.Secondary.Render(label)
	return lipgloss.NewStyle().Width(max(width, 1)).Render(line)
}
