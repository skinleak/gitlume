// Package theme holds every color and Lip Gloss style used by the UI.
// Change the look of gitlume here and nowhere else.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette is the small set of colors the whole UI is built from.
// Each color adapts to light and dark terminal backgrounds.
type Palette struct {
	Text    lipgloss.AdaptiveColor
	Muted   lipgloss.AdaptiveColor
	Subtle  lipgloss.AdaptiveColor
	Accent  lipgloss.AdaptiveColor
	Added   lipgloss.AdaptiveColor
	Removed lipgloss.AdaptiveColor
	Error   lipgloss.AdaptiveColor
}

// DefaultPalette is calm and low-contrast with one soft accent.
// Added/Removed are deliberately desaturated so diffs stay readable
// without competing with the accent.
func DefaultPalette() Palette {
	return Palette{
		Text:    lipgloss.AdaptiveColor{Light: "#2E3440", Dark: "#D8DEE9"},
		Muted:   lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#7B8394"},
		Subtle:  lipgloss.AdaptiveColor{Light: "#B8BEC8", Dark: "#4C5564"},
		Accent:  lipgloss.AdaptiveColor{Light: "#4C78A8", Dark: "#88B4E0"},
		Added:   lipgloss.AdaptiveColor{Light: "#4F7A4F", Dark: "#9CC59C"},
		Removed: lipgloss.AdaptiveColor{Light: "#A35454", Dark: "#D49A9A"},
		Error:   lipgloss.AdaptiveColor{Light: "#A35454", Dark: "#D49A9A"},
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
		Subtle:   s().Foreground(p.Subtle),
		Text:     s().Foreground(p.Text),
		Section:  s().Foreground(p.Muted).Bold(true),
		Selected: s().Foreground(p.Accent),
		Cursor:   s().Foreground(p.Accent).Bold(true),
		Dimmed:   s().Foreground(p.Subtle),

		DiffAdd:  s().Foreground(p.Added),
		DiffDel:  s().Foreground(p.Removed),
		DiffHunk: s().Foreground(p.Accent),
		DiffMeta: s().Foreground(p.Muted),

		Error:   s().Foreground(p.Error),
		Info:    s().Foreground(p.Muted),
		Prompt:  s().Foreground(p.Accent).Bold(true),
		HelpKey: s().Foreground(p.Accent),
		HelpDsc: s().Foreground(p.Muted),
		HelpSep: s().Foreground(p.Subtle),
	}
}

// Default returns the styles for the default palette.
func Default() Styles { return New(DefaultPalette()) }
