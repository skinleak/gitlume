package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/qitlog/internal/ui/theme"
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

// openHelp shows the keybindings from the top.
func (m *Model) openHelp() {
	m.showHelp, m.helpOffset = true, 0
}

// scrollHelp scrolls the keybindings when they don't fit the window.
func (m *Model) scrollHelp(delta int) {
	_, scroll := m.helpLines(m.innerWidth(), m.bodyHeight())
	m.helpOffset = clamp(m.helpOffset+delta, 0, scroll)
}

// helpOverlay renders every keybinding in the body area, centered when it
// fits. Nothing is ever cut off: key groups that don't fit side by side go
// below each other, and a list taller than the window scrolls with j/k.
func (m Model) helpOverlay(width, height int) []string {
	all, scroll := m.helpLines(width, height)
	var body []string
	if scroll == 0 {
		body = append(all, "", m.styles.Subtle.Render("press ? or esc to close"))
	} else {
		offset := min(m.helpOffset, scroll)
		hint := "↓ j/k scroll · esc close"
		if offset == scroll {
			hint = "↑ j/k scroll · esc close"
		}
		body = append(append([]string(nil), all[offset:offset+height-1]...), m.styles.Subtle.Render(hint))
	}

	// Measured over every line, not the visible ones, so scrolling never
	// shifts the list sideways.
	contentW := max(blockWidth(all), ansi.StringWidth(body[len(body)-1]))
	margin := strings.Repeat(" ", max(0, (width-contentW)/2))
	out := make([]string, (height-len(body))/2, height)
	for _, l := range body {
		if l != "" {
			l = fit(margin+l, width, false)
		}
		out = append(out, l)
	}
	return out
}

// helpLines lays out the title and the key groups for a body of width ×
// height cells: as many groups side by side as fit, the rest below. It
// also returns how far the lines scroll, which is 0 when they fit with the
// closing hint.
func (m Model) helpLines(width, height int) ([]string, int) {
	h := m.help
	h.Width = 0 // never cut a group; the layout makes room
	var groups [][]string
	for _, g := range m.keys.FullHelp() {
		groups = append(groups, lines(h.FullHelpView([][]key.Binding{g})))
	}
	gap := ansi.StringWidth(h.FullSeparator)

	all := []string{m.styles.Section.Render("Keybindings")}
	for i := 0; i < len(groups); {
		widths := []int{blockWidth(groups[i])}
		total := widths[0]
		j := i + 1
		for ; j < len(groups); j++ {
			w := blockWidth(groups[j])
			if total+gap+w > width {
				break
			}
			widths, total = append(widths, w), total+gap+w
		}
		all = append(all, "")
		all = append(all, sideBySide(groups[i:j], widths, gap)...)
		i = j
	}
	if len(all)+2 <= height {
		return all, 0
	}
	// One line is kept for the scroll hint.
	return all, max(0, len(all)-(height-1))
}

func blockWidth(block []string) int {
	w := 0
	for _, l := range block {
		w = max(w, ansi.StringWidth(l))
	}
	return w
}

// sideBySide joins blocks into lines, each block padded to its width.
func sideBySide(blocks [][]string, widths []int, gap int) []string {
	rows := 0
	for _, b := range blocks {
		rows = max(rows, len(b))
	}
	sep := strings.Repeat(" ", gap)
	out := make([]string, rows)
	for r := range out {
		var b strings.Builder
		for i, block := range blocks {
			last := i == len(blocks)-1
			var l string
			if r < len(block) {
				l = block[r]
			}
			b.WriteString(fit(l, widths[i], !last))
			if !last {
				b.WriteString(sep)
			}
		}
		out[r] = strings.TrimRight(b.String(), " ")
	}
	return out
}
