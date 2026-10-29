// Package theme holds every color and Lip Gloss style used by the UI.
// Change the look of qitlog here and nowhere else.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette is the small set of colors the whole UI is built from.
// Colors are ANSI indices, so the terminal's own theme decides the exact
// shades and qitlog matches the rest of the user's CLI.
type Palette struct {
	Text    lipgloss.TerminalColor
	Muted   lipgloss.TerminalColor
	Subtle  lipgloss.TerminalColor
	Accent  lipgloss.TerminalColor
	Added   lipgloss.TerminalColor
	Removed lipgloss.TerminalColor
	Error   lipgloss.TerminalColor
	Warning lipgloss.TerminalColor
	Remote  lipgloss.TerminalColor
	// Lanes colors the branch lines of the commit graph, GitHub style. The
	// first, the accent, is HEAD's line; the others take turns.
	Lanes []lipgloss.TerminalColor
}

// The bright ANSI colors (SGR 90–97).
const (
	brightBlack  = lipgloss.Color("8")
	brightRed    = lipgloss.Color("9")
	brightGreen  = lipgloss.Color("10")
	brightYellow = lipgloss.Color("11")
	brightBlue   = lipgloss.Color("12")
	brightPurple = lipgloss.Color("13")
	brightCyan   = lipgloss.Color("14")
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
		Warning: brightYellow,
		Remote:  brightPurple,
		Lanes:   []lipgloss.TerminalColor{brightBlue, brightGreen, brightYellow, brightPurple, brightCyan, brightRed},
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
	DiffFile lipgloss.Style // the file name in a diff's file header

	Error   lipgloss.Style
	Info    lipgloss.Style
	Prompt  lipgloss.Style
	Confirm lipgloss.Style

	// Status letters in the file list, colored like GitHub and VS Code.
	StatusNew      lipgloss.Style
	StatusModified lipgloss.Style
	StatusDeleted  lipgloss.Style
	StatusConflict lipgloss.Style

	// Branch view, colored like git's own output: current branch green,
	// ahead green, behind red, commit hashes yellow. Remotes are purple
	// rather than git's red, which qitlog reserves for removals.
	BranchCurrent lipgloss.Style
	BranchRemote  lipgloss.Style
	Ahead         lipgloss.Style
	Behind        lipgloss.Style
	Gone          lipgloss.Style
	CommitHash    lipgloss.Style

	// Lanes has one style per graph lane color; Lanes[0] is HEAD's line.
	Lanes []lipgloss.Style

	Tab       lipgloss.Style
	TabActive lipgloss.Style
	TabKey    lipgloss.Style
	HelpKey   lipgloss.Style
	HelpDsc   lipgloss.Style
	HelpSep   lipgloss.Style
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
		DiffFile: s().Foreground(p.Text).Bold(true),

		Error:   s().Foreground(p.Error),
		Info:    s().Foreground(p.Muted),
		Prompt:  s().Foreground(p.Accent).Bold(true),
		Confirm: s().Foreground(p.Error).Bold(true),

		StatusNew:      s().Foreground(p.Added),
		StatusModified: s().Foreground(p.Warning),
		StatusDeleted:  s().Foreground(p.Removed),
		StatusConflict: s().Foreground(p.Error).Bold(true),

		BranchCurrent: s().Foreground(p.Added).Bold(true),
		BranchRemote:  s().Foreground(p.Remote),
		Ahead:         s().Foreground(p.Added),
		Behind:        s().Foreground(p.Removed),
		Gone:          s().Foreground(p.Warning),
		CommitHash:    s().Foreground(p.Warning),
		Lanes:         laneStyles(p.Lanes),

		Tab:       s().Foreground(p.Muted),
		TabActive: s().Foreground(p.Text).Bold(true),
		TabKey:    s().Foreground(p.Subtle).Faint(true),
		HelpKey:   s().Foreground(p.Accent),
		HelpDsc:   s().Foreground(p.Muted),
		HelpSep:   s().Foreground(p.Subtle).Faint(true),
	}
}

func laneStyles(colors []lipgloss.TerminalColor) []lipgloss.Style {
	out := make([]lipgloss.Style, len(colors))
	for i, c := range colors {
		out[i] = lipgloss.NewStyle().Foreground(c)
	}
	return out
}

// Default returns the styles for the default palette.
func Default() Styles { return New(DefaultPalette()) }
