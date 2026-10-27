package ui

import (
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/skinleak/gitlume/internal/ui/theme"
)

// commitLabel prefixes the commit message input.
const commitLabel = "commit › "

// commitPrompt is the single-line commit message input. The draft is kept
// when the prompt is cancelled and cleared only after a commit starts.
type commitPrompt struct {
	input  textinput.Model
	active bool
}

func newCommitPrompt(st theme.Styles) commitPrompt {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "Commit message"
	in.PlaceholderStyle = st.Subtle
	in.TextStyle = st.Text
	in.Cursor.Style = st.Cursor
	// A static cursor is calmer and avoids a redraw every blink.
	in.Cursor.SetMode(cursor.CursorStatic)
	return commitPrompt{input: in}
}

func (p *commitPrompt) open(width int) tea.Cmd {
	p.active = true
	p.input.Width = max(1, width)
	return p.input.Focus()
}

func (p *commitPrompt) close() {
	p.active = false
	p.input.Blur()
}

func (p *commitPrompt) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (p *commitPrompt) view(st theme.Styles) string {
	return st.Prompt.Render(commitLabel) + p.input.View()
}
