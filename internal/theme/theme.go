// Package theme is the palette and the width-aware text helpers every
// screen draws with.
package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The hues are claude-inbox's, tuned to Atom One Dark as 256-color indices.
// Bubble Tea brings them down to whatever the terminal and NO_COLOR allow.
var (
	Red    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	Green  = lipgloss.NewStyle().Foreground(lipgloss.Color("108"))
	Yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
	Blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	Purple = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))
	Cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("73"))
	Faint  = lipgloss.NewStyle().Faint(true)
	Plain  = lipgloss.NewStyle()
)

// Dim draws s faint.
func Dim(s string) string {
	return Faint.Render(s)
}

// KeyName draws the name of a key in a hint.
func KeyName(s string) string {
	return Cyan.Bold(true).Render(s)
}

// Hint draws a key and what it does, as a footer shows them.
func Hint(key, what string) string {
	return KeyName(key) + " " + Dim(what)
}

// Width is how many columns s takes on screen, styles aside.
func Width(s string) int {
	return ansi.StringWidth(s)
}

// Truncate cuts s to w columns, ending it with … when it had to cut.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// Pad truncates or fills s with spaces to exactly w columns.
func Pad(s string, w int) string {
	s = Truncate(s, w)
	return s + strings.Repeat(" ", max(w-Width(s), 0))
}
