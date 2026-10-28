package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

// groupOrder is the order sections appear in the file list.
var groupOrder = []git.Group{git.Staged, git.Unstaged, git.Untracked}

type rowKind int

const (
	rowBlank rowKind = iota
	rowHeader
	rowChange
)

// row is one rendered line of the file list.
type row struct {
	kind  rowKind
	group git.Group
	count int // rowHeader: number of changes in the group
	index int // rowChange: index into fileList.changes
}

// fileList is the grouped list of changed files. changes are kept in
// groupOrder; cursor indexes changes, offset is the first visible row.
// rows and rowOf are derived from changes once per update, so moving and
// rendering never rebuild them.
type fileList struct {
	changes []git.Change
	rows    []row
	rowOf   []int // rowOf[i] is the row index of changes[i]
	cursor  int
	offset  int
	height  int
}

func allChanges(st git.Status) []git.Change {
	var out []git.Change
	for _, g := range groupOrder {
		out = append(out, st.Changes(g)...)
	}
	return out
}

func (l *fileList) selected() (git.Change, bool) {
	if l.cursor < 0 || l.cursor >= len(l.changes) {
		return git.Change{}, false
	}
	return l.changes[l.cursor], true
}

func (l *fileList) count(g git.Group) int {
	n := 0
	for _, c := range l.changes {
		if c.Group == g {
			n++
		}
	}
	return n
}

// renameSource returns the source path of a staged rename to path, or "".
func (l *fileList) renameSource(path string) string {
	for _, c := range l.changes {
		if c.Path == path && c.OrigPath != "" {
			return c.OrigPath
		}
	}
	return ""
}

// move shifts the cursor by delta and reports whether it changed.
func (l *fileList) move(delta int) bool {
	next := clamp(l.cursor+delta, 0, len(l.changes)-1)
	if next == l.cursor || len(l.changes) == 0 {
		return false
	}
	l.cursor = next
	l.ensureVisible()
	return true
}

func (l *fileList) setHeight(h int) {
	l.height = max(1, h)
	l.ensureVisible()
}

// setChanges replaces the list, keeping the selection on the same file
// when it still exists in the same group, or on the same position within
// that group otherwise. This makes staging several files in a row natural.
func (l *fileList) setChanges(cs []git.Change) {
	prev, ok := l.selected()
	pos := l.positionInGroup()
	l.changes = cs
	l.rows, l.rowOf = buildRows(cs)
	l.cursor = l.restoreCursor(prev, ok, pos)
	l.ensureVisible()
}

func (l *fileList) positionInGroup() int {
	c, ok := l.selected()
	if !ok {
		return 0
	}
	pos := 0
	for _, o := range l.changes[:l.cursor] {
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
	for i, c := range l.changes {
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
	return clamp(l.cursor, 0, len(l.changes)-1)
}

// buildRows lays out cs as section headers, separators and file rows.
func buildRows(cs []git.Change) ([]row, []int) {
	rows := make([]row, 0, len(cs)+3*len(groupOrder))
	rowOf := make([]int, len(cs))
	for i := 0; i < len(cs); {
		g := cs[i].Group
		j := i
		for j < len(cs) && cs[j].Group == g {
			j++
		}
		if len(rows) > 0 {
			rows = append(rows, row{kind: rowBlank})
		}
		rows = append(rows, row{kind: rowHeader, group: g, count: j - i})
		for k := i; k < j; k++ {
			rowOf[k] = len(rows)
			rows = append(rows, row{kind: rowChange, index: k})
		}
		i = j
	}
	return rows, rowOf
}

// ensureVisible scrolls so the cursor row, and its section header when it
// is the first file of a group, are on screen.
func (l *fileList) ensureVisible() {
	rows := l.rows
	if len(rows) == 0 {
		l.offset = 0
		return
	}
	cur := l.rowOf[l.cursor]
	top := cur
	if cur > 0 && rows[cur-1].kind == rowHeader {
		top = cur - 1
	}
	if top < l.offset {
		l.offset = top
	}
	if cur >= l.offset+l.height {
		l.offset = cur - l.height + 1
	}
	l.offset = clamp(l.offset, 0, max(0, len(rows)-l.height))
}

func (l *fileList) view(st theme.Styles, width int, focused bool) string {
	if len(l.changes) == 0 {
		return st.Muted.Render("Working tree clean")
	}
	end := min(len(l.rows), l.offset+l.height)
	lines := make([]string, 0, end-l.offset)
	for _, r := range l.rows[l.offset:end] {
		lines = append(lines, l.renderRow(st, r, width, focused))
	}
	return strings.Join(lines, "\n")
}

func (l *fileList) renderRow(st theme.Styles, r row, width int, focused bool) string {
	switch r.kind {
	case rowHeader:
		return st.Section.Render(r.group.String()) + "  " + st.Muted.Render(strconv.Itoa(r.count))
	case rowChange:
		c := l.changes[r.index]
		name := truncateLeft(displayPath(c), width-4)
		code := string(c.Code)
		if r.index != l.cursor {
			return "  " + codeStyle(st, c.Code).Render(code) + " " + st.Text.Render(name)
		}
		if focused {
			return st.Cursor.Render("› " + code + " " + name)
		}
		return st.Muted.Render("› ") + codeStyle(st, c.Code).Render(code) + " " + st.Text.Render(name)
	}
	return ""
}

func displayPath(c git.Change) string {
	if c.OrigPath != "" {
		return sanitize(c.OrigPath) + " → " + sanitize(c.Path)
	}
	return sanitize(c.Path)
}

func codeStyle(st theme.Styles, code byte) lipgloss.Style {
	switch code {
	case 'A', '?':
		return st.DiffAdd
	case 'D':
		return st.DiffDel
	case 'U':
		return st.Error
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
