package ui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/qitlog/internal/git"
	"github.com/skinleak/qitlog/internal/ui/theme"
)

// diffKey identifies what the diff pane shows: the diff of one change, or
// one commit of the log. The same path can have a staged and an unstaged
// diff, so the group is part of the key.
type diffKey struct {
	group  git.Group
	path   string
	commit string // set for a commit; group and path are then unused
}

func keyOf(c git.Change) diffKey { return diffKey{group: c.Group, path: c.Path} }

func (k diffKey) isCommit() bool { return k.commit != "" }

// diffContent is a loaded diff, cached per diffKey.
type diffContent struct {
	lines     []string
	truncated bool
	note      string // shown instead of lines when there is no patch
	size      int    // bytes of patch text, for the cache budget
	header    int    // lines before the patch: a commit's message and diffstat
	heads     []fileHead
}

// fileHead stands in for the lines git prints before each file's hunks
// (diff --git, index, ---, +++, mode and rename lines): one line with the
// file name, what happened to it and how many lines it adds and removes.
type fileHead struct {
	line     int // index in diffContent.lines
	path     string
	orig     string // source of a rename or copy
	note     string // "new", "deleted", "binary", ...
	add, del int
	counted  bool // add and del are complete; not when the patch was cut off
}

// tidyPatch replaces each file's header lines with a fileHead and puts a
// blank line between files. It parses git's own markers rather than
// guessing from prefixes, so a removed line that starts with "--" stays a
// removed line. It runs inside the tea.Cmd, off the UI goroutine.
func tidyPatch(lines []string, truncated bool) ([]string, []fileHead) {
	out := make([]string, 0, len(lines))
	var heads []fileHead
	inHeader := false
	cols := 1 // +/- columns per line: one, or one per parent in a combined diff
	for _, l := range lines {
		if rest, ok := cutDiffLine(l); ok {
			if len(out) > 0 {
				out = append(out, "")
			}
			heads = append(heads, fileHead{line: len(out), path: rest, counted: true})
			out = append(out, rest)
			inHeader, cols = true, 1
			continue
		}
		if len(heads) == 0 {
			out = append(out, l)
			continue
		}
		h := &heads[len(heads)-1]
		if !inHeader {
			if n := len(l) - len(strings.TrimLeft(l, "@")); n > 1 {
				cols = n - 1 // "@@@" starts a hunk of a merge with two parents
			} else if p := l[:min(cols, len(l))]; strings.Contains(p, "+") {
				h.add++
			} else if strings.Contains(p, "-") {
				h.del++
			}
			out = append(out, l)
			continue
		}
		switch {
		case strings.HasPrefix(l, "@@"):
			inHeader = false
			cols = max(1, len(l)-len(strings.TrimLeft(l, "@"))-1)
			out = append(out, l)
		case strings.HasPrefix(l, "--- "), strings.HasPrefix(l, "+++ "):
			if p := l[4:]; p != "/dev/null" {
				h.path = patchPath(p)
			}
		case strings.HasPrefix(l, "rename to "), strings.HasPrefix(l, "copy to "):
			_, p, _ := strings.Cut(l, " to ")
			h.path = unquotePath(p)
		case strings.HasPrefix(l, "rename from "), strings.HasPrefix(l, "copy from "):
			kind, p, _ := strings.Cut(l, " from ")
			h.orig = unquotePath(p)
			if kind == "copy" {
				h.addNote("copy")
			}
		case strings.HasPrefix(l, "new file mode"):
			h.addNote("new")
		case strings.HasPrefix(l, "deleted file mode"):
			h.addNote("deleted")
		case strings.HasPrefix(l, "new mode"):
			h.addNote("mode changed")
		case strings.HasPrefix(l, "Binary files"):
			h.addNote("binary")
		case strings.HasPrefix(l, "index "), strings.HasPrefix(l, "old mode"),
			strings.HasPrefix(l, "similarity index"), strings.HasPrefix(l, "dissimilarity index"):
		default:
			out = append(out, l) // a header line qitlog doesn't know: keep it
		}
	}
	if truncated && len(heads) > 0 {
		heads[len(heads)-1].counted = false
	}
	for _, h := range heads {
		out[h.line] = h.path
	}
	return out, heads
}

func (h *fileHead) addNote(n string) {
	if h.note != "" {
		h.note += ", "
	}
	h.note += n
}

// cutDiffLine reports whether l starts a file's diff and returns the path
// it names. "diff --git a/x b/x" names the path twice; when the halves
// differ (a rename) or are quoted, the --- and +++ lines settle it.
func cutDiffLine(l string) (string, bool) {
	if rest, ok := strings.CutPrefix(l, "diff --git "); ok {
		n := (len(rest) - 1) / 2
		if len(rest)%2 == 1 && strings.HasPrefix(rest, "a/") && rest[n:n+3] == " b/" && rest[2:n] == rest[n+3:] {
			return rest[2:n], true
		}
		return rest, true
	}
	if rest, ok := strings.CutPrefix(l, "diff --cc "); ok {
		return rest, true
	}
	rest, ok := strings.CutPrefix(l, "diff --combined ")
	return rest, ok
}

// patchPath reads the path of a --- or +++ line: "a/x", "b/x", or a quoted
// "\"b/x\"" for names with special characters.
func patchPath(p string) string {
	p = unquotePath(p)
	if len(p) > 2 && (p[:2] == "a/" || p[:2] == "b/") {
		return p[2:]
	}
	return p
}

// unquotePath undoes git's C-style quoting of unusual file names.
func unquotePath(p string) string {
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

// view renders the file header: "dir/name  new  +20 -3", the directory
// muted like in the file list.
func (h fileHead) view(st theme.Styles) string {
	var b strings.Builder
	if h.orig != "" {
		b.WriteString(st.Muted.Render(sanitize(h.orig) + " → "))
	}
	b.WriteString(pathView(paintOf(st.Muted), paintOf(st.DiffFile), sanitize(h.path)))
	if h.note != "" {
		b.WriteString("  " + st.Subtle.Render(h.note))
	}
	if h.counted && h.add > 0 {
		b.WriteString("  " + st.DiffAdd.Render("+"+strconv.Itoa(h.add)))
	}
	if h.counted && h.del > 0 {
		b.WriteString("  " + st.DiffDel.Render("-"+strconv.Itoa(h.del)))
	}
	return b.String()
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
	// The first file header on screen; later ones follow in order.
	h, _ := slices.BinarySearchFunc(d.heads, d.offset, func(f fileHead, i int) int { return f.line - i })
	for i := d.offset; i < end; i++ {
		switch {
		case i == len(d.lines):
			out = append(out, st.Muted.Render("… diff truncated"))
		case i < d.header:
			// Styled first: refs are shortened while styling.
			out = append(out, ansi.Truncate(styleCommitLine(st, d.lines[i]), width, ""))
		case h < len(d.heads) && d.heads[h].line == i:
			out = append(out, ansi.Truncate(d.heads[h].view(st), width, "…"))
			h++
		default:
			out = append(out, styleDiffLine(st, ansi.Truncate(d.lines[i], width, "")))
		}
	}
	return strings.Join(out, "\n")
}

func styleDiffLine(st theme.Styles, line string) string {
	switch {
	case line == "":
		return ""
	case strings.HasPrefix(line, "@@"):
		return st.DiffHunk.Render(line)
	case line[0] == '+':
		return st.DiffAdd.Render(line)
	case line[0] == '-':
		return st.DiffDel.Render(line)
	case line[0] == ' ':
		return st.Text.Render(line)
	}
	// "\ No newline at end of file" and header lines tidyPatch doesn't know.
	return st.DiffMeta.Render(line)
}
