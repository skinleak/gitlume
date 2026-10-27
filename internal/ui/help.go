package ui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"

	"github.com/skinleak/gitlume/internal/ui/theme"
)

func newHelp(st theme.Styles) help.Model {
	h := help.New()
	h.ShortSeparator = " · "
	h.FullSeparator = "      "
	h.Styles = help.Styles{
		ShortKey:       st.HelpKey,
		ShortDesc:      st.HelpDsc,
		ShortSeparator: st.HelpSep,
		FullKey:        st.HelpKey,
		FullDesc:       st.HelpDsc,
		FullSeparator:  st.HelpSep,
		Ellipsis:       st.HelpSep,
	}
	return h
}

// helpOverlay renders the full keybinding list centered in the body area.
func (m Model) helpOverlay(width, height int) string {
	body := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.Section.Render("Keybindings"),
		"",
		m.help.FullHelpView(m.keys.FullHelp()),
		"",
		m.styles.Subtle.Render("press ? or esc to close"),
	)
	body = lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}
