// Package keys defines every keybinding in one place. The help overlay
// and footer hints are generated from these bindings, so a binding added
// here is automatically documented.
package keys

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all bindings for the main screen.
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Toggle     key.Binding
	StageAll   key.Binding
	Discard    key.Binding
	DiscardAll key.Binding
	Commit     key.Binding
	Focus      key.Binding
	Open       key.Binding
	Back       key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Refresh    key.Binding
	Help       key.Binding
	Quit       key.Binding
}

// PromptKeyMap holds bindings active while the commit prompt is open.
type PromptKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

// ConfirmKeyMap holds bindings active while a yes/no question is shown.
type ConfirmKeyMap struct {
	Yes key.Binding
	No  key.Binding
}

// Default returns the default main-screen bindings.
func Default() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("↓/j", "down"),
		),
		Toggle: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "stage / unstage"),
		),
		StageAll: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "stage all"),
		),
		Discard: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "discard file"),
		),
		DiscardAll: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "discard all"),
		),
		Commit: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "commit"),
		),
		Focus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "focus files / diff"),
		),
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "view diff"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("ctrl+u", "pgup"),
			key.WithHelp("ctrl+u", "scroll diff up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("ctrl+d", "pgdown"),
			key.WithHelp("ctrl+d", "scroll diff down"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// DefaultPrompt returns the commit prompt bindings.
func DefaultPrompt() PromptKeyMap {
	return PromptKeyMap{
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "commit"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

// DefaultConfirm returns the yes/no question bindings. enter is not a
// yes on purpose: these questions guard destructive operations.
func DefaultConfirm() ConfirmKeyMap {
	return ConfirmKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "yes"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "esc"),
			key.WithHelp("n", "no"),
		),
	}
}

// ShortHelp is the one-line hint shown in the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.StageAll, k.Discard, k.DiscardAll, k.Commit, k.Open, k.Help, k.Quit}
}

// DiffHelp is the footer hint while a diff is shown fullscreen.
func (k KeyMap) DiffHelp() []key.Binding {
	return []key.Binding{k.Down, k.Up, k.ScrollDown, k.ScrollUp, k.Back, k.Help}
}

// FullHelp is the grouped list shown in the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Focus, k.ScrollUp, k.ScrollDown, k.Open, k.Back},
		{k.Toggle, k.StageAll, k.Discard, k.DiscardAll, k.Commit, k.Refresh},
		{k.Help, k.Quit},
	}
}

// ShortHelp is the hint shown under the commit prompt.
func (k PromptKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

// FullHelp implements help.KeyMap.
func (k PromptKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// ShortHelp is the hint shown next to a yes/no question.
func (k ConfirmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Yes, k.No}
}

// FullHelp implements help.KeyMap.
func (k ConfirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
