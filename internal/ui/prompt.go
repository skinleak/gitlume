package ui

import (
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/skinleak/qitlog/internal/ui/keys"
	"github.com/skinleak/qitlog/internal/ui/theme"
)

// prompt is a single-line input in the footer: the commit message or a new
// branch's name. Its draft is kept when it is cancelled and cleared only
// once its operation starts.
type prompt struct {
	label  string // e.g. "commit › "
	keys   keys.PromptKeyMap
	input  textinput.Model
	active bool
}

func newPrompt(st theme.Styles, label, placeholder string, k keys.PromptKeyMap) prompt {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.PlaceholderStyle = st.Subtle
	in.TextStyle = st.Text
	in.Cursor.Style = st.Cursor
	// A static cursor is calmer and avoids a redraw every blink.
	in.Cursor.SetMode(cursor.CursorStatic)
	return prompt{label: label, keys: k, input: in}
}

func (p *prompt) open(width int) tea.Cmd {
	p.active = true
	p.input.Width = max(1, width)
	return p.input.Focus()
}

func (p *prompt) close() {
	p.active = false
	p.input.Blur()
}

func (p *prompt) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (p *prompt) view(st theme.Styles) string {
	return st.Prompt.Render(p.label) + p.input.View()
}
