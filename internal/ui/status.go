package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/qitlog/internal/git"
	"github.com/skinleak/qitlog/internal/ui/theme"
)

// groupOrder is the order sections appear in the file list.
var groupOrder = []git.Group{git.Staged, git.Unstaged, git.Untracked}

// fileList is the grouped list of changed files, kept in groupOrder.
type fileList struct {
	sectionList[git.Change]
}

func allChanges(st git.Status) []git.Change {
	var out []git.Change
	for _, g := range groupOrder {
		out = append(out, st.Changes(g)...)
	}
	return out
}

func (l *fileList) count(g git.Group) int {
	n := 0
	for _, c := range l.items {
		if c.Group == g {
			n++
		}
	}
	return n
}

// renameSource returns the source path of a staged rename to path, or "".
func (l *fileList) renameSource(path string) string {
	for _, c := range l.items {
		if c.Path == path && c.OrigPath != "" {
			return c.OrigPath
		}
	}
	return ""
}

// setChanges replaces the list, keeping the selection on the same file
// when it still exists in the same group, or on the same position within
// that group otherwise. This makes staging several files in a row natural.
func (l *fileList) setChanges(cs []git.Change) {
	prev, ok := l.selected()
	pos := l.positionInGroup()
	l.setItems(cs, func(a, b git.Change) bool { return a.Group == b.Group })
	l.setCursor(l.restoreCursor(prev, ok, pos))
}

func (l *fileList) positionInGroup() int {
	c, ok := l.selected()
	if !ok {
		return 0
	}
	pos := 0
	for _, o := range l.items[:l.cursor] {
		if o.Group == c.Group {
			pos++
		}
	}
	return pos
}

func (l *fileList) restoreCursor(prev git.Change, hadPrev bool, pos int) int {
	if !hadPrev {
		return 0
	}
	first, n := -1, 0
	for i, c := range l.items {
		if c.Group != prev.Group {
			continue
		}
		if c.Path == prev.Path {
			return i
		}
		if first < 0 {
			first = i
		}
		n++
	}
	if n > 0 {
		return first + min(pos, n-1)
	}
	return l.cursor
}

func (l *fileList) view(st theme.Styles, width int, focused bool) string {
	if len(l.items) == 0 {
		return st.Muted.Render("Working tree clean")
	}
	rows := l.visible()
	dir, text := paintOf(st.Muted), paintOf(st.Text)
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, l.renderRow(st, dir, text, r, width, focused))
	}
	return strings.Join(lines, "\n")
}

func (l *fileList) renderRow(st theme.Styles, dir, text paint, r listRow, width int, focused bool) string {
	switch r.kind {
	case rowHeader:
		g := l.items[r.index].Group
		return st.Section.Render(g.String()) + "  " + st.Muted.Render(strconv.Itoa(r.count))
	case rowItem:
		c := l.items[r.index]
		name := truncateLeft(displayPath(c), width-4)
		code := string(displayCode(c.Code))
		if r.index != l.cursor {
			return "  " + codeStyle(st, c.Code).Render(code) + " " + pathView(dir, text, name)
		}
		if focused {
			return st.Cursor.Render("› " + code + " " + name)
		}
		return st.Muted.Render("› ") + codeStyle(st, c.Code).Render(code) + " " + pathView(dir, text, name)
	}
	return ""
}

// paint is the escape codes a style puts around text. Rendering a style
// once per frame and wrapping text in its codes is much cheaper than a
// Render per row or per colored run, and gives the same output for the
// plain color and bold styles used here.
type paint struct{ open, close string }

func paintOf(s lipgloss.Style) paint {
	var p paint
	p.open, p.close, _ = strings.Cut(s.Render("\x00"), "\x00")
	return p
}

func (p paint) render(s string) string { return p.open + s + p.close }

// pathView renders a path with its directories in dir, so the file names
// stand out.
func pathView(dir, name paint, p string) string {
	i := strings.LastIndexByte(p, '/') + 1
	if i == 0 {
		return name.render(p)
	}
	return dir.render(p[:i]) + name.render(p[i:])
}

func displayPath(c git.Change) string {
	if c.OrigPath != "" {
		return sanitize(c.OrigPath) + " → " + sanitize(c.Path)
	}
	return sanitize(c.Path)
}

// displayCode maps git's status letter to the one shown, following the
// GitHub and VS Code convention: untracked files are U. git's own U
// (unmerged) would then be ambiguous, so conflicts are shown as !.
func displayCode(code byte) byte {
	switch code {
	case '?':
		return 'U'
	case 'U':
		return '!'
	}
	return code
}

// codeStyle colors a status letter: new green, modified yellow, deleted
// and conflicted red.
func codeStyle(st theme.Styles, code byte) lipgloss.Style {
	switch code {
	case 'A', '?':
		return st.StatusNew
	case 'M', 'R', 'C', 'T':
		return st.StatusModified
	case 'D':
		return st.StatusDeleted
	case 'U':
		return st.StatusConflict
	}
	return st.Muted
}

// truncateLeft shortens s to width cells, keeping the end (the file name).
func truncateLeft(s string, width int) string {
	w := ansi.StringWidth(s)
	if width <= 0 || w <= width {
		return s
	}
	return ansi.TruncateLeft(s, w-width+1, "…")
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}
