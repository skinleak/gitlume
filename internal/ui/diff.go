package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

// diffKey identifies the diff of one change. The same path can have a
// staged and an unstaged diff, so the group is part of the key.
type diffKey struct {
	group git.Group
	path  string
}

func keyOf(c git.Change) diffKey { return diffKey{group: c.Group, path: c.Path} }

// diffContent is a loaded diff, cached per diffKey.
type diffContent struct {
	lines     []string
	truncated bool
	note      string // shown instead of lines when there is no patch
	size      int    // bytes of patch text, for the cache budget
}

// diffView is a minimal scroller over diff lines. Only visible lines are
// styled and truncated, so rendering cost does not depend on diff size,
// and long lines are cut instead of wrapped.
type diffView struct {
	key   diffKey
	shown bool
	diffContent
	offset int
	height int
}

func (d *diffView) show(key diffKey, c diffContent) {
	if !d.shown || key != d.key {
		d.offset = 0
	}
	d.key, d.shown, d.diffContent = key, true, c
	d.scroll(0)
}

func (d *diffView) clear() { *d = diffView{height: d.height} }

func (d *diffView) setHeight(h int) {
	d.height = max(1, h)
	d.scroll(0)
}

func (d *diffView) scroll(delta int) {
	d.offset = clamp(d.offset+delta, 0, d.lineCount()-d.height)
}

func (d *diffView) lineCount() int {
	if d.truncated {
		return len(d.lines) + 1
	}
	return len(d.lines)
}

func (d *diffView) view(st theme.Styles, width int) string {
	if !d.shown {
		return ""
	}
	if d.note != "" {
		return st.Muted.Render(d.note)
	}
	if len(d.lines) == 0 {
		return st.Muted.Render("No textual changes")
	}
	end := min(d.lineCount(), d.offset+d.height)
	out := make([]string, 0, end-d.offset)
	for i := d.offset; i < end; i++ {
		if i == len(d.lines) {
			out = append(out, st.Muted.Render("… diff truncated"))
			continue
		}
		out = append(out, styleDiffLine(st, ansi.Truncate(d.lines[i], width, "")))
	}
	return strings.Join(out, "\n")
}

func styleDiffLine(st theme.Styles, line string) string {
	switch {
	case line == "":
		return ""
	case strings.HasPrefix(line, "@@"):
		return st.DiffHunk.Render(line)
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return st.DiffMeta.Render(line)
	case line[0] == '+':
		return st.DiffAdd.Render(line)
	case line[0] == '-':
		return st.DiffDel.Render(line)
	case line[0] == ' ':
		return st.Text.Render(line)
	}
	// File headers: "diff --git", "index", "new file mode", "Binary files", ...
	return st.DiffMeta.Render(line)
}
