// Package ui defines the swappable terminal UI interface for Sagittarius.
//
// Implementations live in subpackages (e.g. internal/ui/bubbletea). Agent and
// provider code must depend only on ui.UI and ui.App — never on Bubble Tea.
package ui
