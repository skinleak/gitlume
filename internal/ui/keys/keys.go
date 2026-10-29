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
	Amend      key.Binding
	Pull       key.Binding
	Push       key.Binding
	Focus      key.Binding
	Open       key.Binding
	Back       key.Binding
	Checkout   key.Binding
	NewBranch  key.Binding
	Files      key.Binding
	Branches   key.Binding
	Log        key.Binding
	PrevView   key.Binding
	NextView   key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Refresh    key.Binding
	Help       key.Binding
	Quit       key.Binding
}

// PromptKeyMap holds bindings active while a prompt is open.
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
			key.WithHelp("space", "stage/unstage"),
		),
		StageAll: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "stage all"),
		),
		Discard: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "discard"),
		),
		DiscardAll: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "discard all"),
		),
		Commit: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "commit"),
		),
		Amend: key.NewBinding(
			key.WithKeys("A"),
			key.WithHelp("A", "amend"),
		),
		Pull: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pull"),
		),
		Push: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "push"),
		),
		Focus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "focus files / diff"),
		),
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "diff"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Checkout: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "checkout"),
		),
		NewBranch: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new branch"),
		),
		Files: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "files"),
		),
		Branches: key.NewBinding(
			key.WithKeys("b"),
			key.WithHelp("b", "branches"),
		),
		Log: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "log"),
		),
		PrevView: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "previous tab"),
		),
		NextView: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "next tab"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("ctrl+u", "pgup"),
			key.WithHelp("ctrl+u", "page up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("ctrl+d", "pgdown"),
			key.WithHelp("ctrl+d", "page down"),
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

// DefaultAmendPrompt returns the amend prompt bindings.
func DefaultAmendPrompt() PromptKeyMap {
	return PromptKeyMap{
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "amend"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

// DefaultBranchPrompt returns the new branch prompt bindings.
func DefaultBranchPrompt() PromptKeyMap {
	return PromptKeyMap{
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "create"),
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

// The footer hints below are ordered by importance: when the terminal is
// too narrow, hints are dropped from the right, except the last two.

// ShortHelp is the one-line hint shown in the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.StageAll, k.Commit, k.Push, k.Discard, k.DiscardAll, k.Open, k.Branches, k.Pull, k.Amend, k.Help, k.Quit}
}

// BranchHelp is the footer hint while the branch list is open.
func (k KeyMap) BranchHelp() []key.Binding {
	return []key.Binding{k.Checkout, k.NewBranch, k.Refresh, k.Back, k.Help}
}

// LogHelp is the footer hint in the log view.
func (k KeyMap) LogHelp() []key.Binding {
	return []key.Binding{k.Open, k.Pull, k.Push, k.Branches, k.ScrollDown, k.ScrollUp, k.Focus, k.Help, k.Quit}
}

// DiffHelp is the footer hint while a diff is shown fullscreen.
func (k KeyMap) DiffHelp() []key.Binding {
	return []key.Binding{k.Down, k.Up, k.ScrollDown, k.ScrollUp, k.Back, k.Help}
}

// FullHelp is the grouped list shown in the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Focus, k.ScrollUp, k.ScrollDown, k.Open, k.Back},
		{k.Toggle, k.StageAll, k.Discard, k.DiscardAll, k.Commit, k.Amend, k.Pull, k.Push, k.Refresh},
		{k.Files, k.Log, k.PrevView, k.NextView, k.Branches, k.Checkout, k.NewBranch, k.Help, k.Quit},
	}
}

// ShortHelp is the hint shown next to a prompt.
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
