// Package theme holds every color and Lip Gloss style used by the UI.
// Change the look of gitlume here and nowhere else.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette is the small set of colors the whole UI is built from.
// Colors are ANSI indices, so the terminal's own theme decides the exact
// shades and gitlume matches the rest of the user's CLI.
type Palette struct {
	Text    lipgloss.TerminalColor
	Muted   lipgloss.TerminalColor
	Subtle  lipgloss.TerminalColor
	Accent  lipgloss.TerminalColor
	Added   lipgloss.TerminalColor
	Removed lipgloss.TerminalColor
	Error   lipgloss.TerminalColor
}

// The bright ANSI colors (SGR 90–97).
const (
	brightBlack  = lipgloss.Color("8")
	brightRed    = lipgloss.Color("9")
	brightGreen  = lipgloss.Color("10")
	brightBlue   = lipgloss.Color("12")
	brightWhite  = lipgloss.Color("15")
	regularBlack = lipgloss.Color("0")
)

// DefaultPalette uses the bright ANSI colors with one accent (blue).
// Text falls back to black on light backgrounds, where bright white
// would be invisible.
func DefaultPalette() Palette {
	return Palette{
		Text:    lipgloss.AdaptiveColor{Light: string(regularBlack), Dark: string(brightWhite)},
		Muted:   brightBlack,
		Subtle:  brightBlack,
		Accent:  brightBlue,
		Added:   brightGreen,
		Removed: brightRed,
		Error:   brightRed,
	}
}

// Styles are the concrete Lip Gloss styles derived from a Palette.
type Styles struct {
	Branch   lipgloss.Style
	Muted    lipgloss.Style
	Subtle   lipgloss.Style
	Text     lipgloss.Style
	Section  lipgloss.Style
	Selected lipgloss.Style
	Cursor   lipgloss.Style
	Dimmed   lipgloss.Style

	DiffAdd  lipgloss.Style
	DiffDel  lipgloss.Style
	DiffHunk lipgloss.Style
	DiffMeta lipgloss.Style

	Error   lipgloss.Style
	Info    lipgloss.Style
	Prompt  lipgloss.Style
	Confirm lipgloss.Style
	HelpKey lipgloss.Style
	HelpDsc lipgloss.Style
	HelpSep lipgloss.Style
}

// New builds Styles from a Palette.
func New(p Palette) Styles {
	s := lipgloss.NewStyle
	return Styles{
		Branch:   s().Foreground(p.Accent).Bold(true),
		Muted:    s().Foreground(p.Muted),
		Subtle:   s().Foreground(p.Subtle).Faint(true),
		Text:     s().Foreground(p.Text),
		Section:  s().Foreground(p.Muted).Bold(true),
		Selected: s().Foreground(p.Accent),
		Cursor:   s().Foreground(p.Accent).Bold(true),
		Dimmed:   s().Foreground(p.Subtle).Faint(true),

		DiffAdd:  s().Foreground(p.Added),
		DiffDel:  s().Foreground(p.Removed),
		DiffHunk: s().Foreground(p.Accent),
		DiffMeta: s().Foreground(p.Muted),

		Error:   s().Foreground(p.Error),
		Info:    s().Foreground(p.Muted),
		Prompt:  s().Foreground(p.Accent).Bold(true),
		Confirm: s().Foreground(p.Error).Bold(true),
		HelpKey: s().Foreground(p.Accent),
		HelpDsc: s().Foreground(p.Muted),
		HelpSep: s().Foreground(p.Subtle).Faint(true),
	}
}

// Default returns the styles for the default palette.
func Default() Styles { return New(DefaultPalette()) }
