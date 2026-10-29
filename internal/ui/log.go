package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

// logList is the history of every branch, newest first, drawn as a graph.
// It is loaded in pages of limit commits; more is true while git may have
// older commits than were loaded.
type logList struct {
	sectionList[git.Commit]
	lanes  [][]graphCell // one row per commit
	seq    int
	req    request // the running load
	limit  int     // commits requested from git; grows by logPage
	loaded bool
	more   bool
	branch string // HEAD's branch when the list was last set
}

// set replaces the commits, keeping the cursor on the same commit. The
// cursor goes to HEAD instead after HEAD switched to another branch, and
// follows HEAD when HEAD was selected, so a pull or a new commit doesn't
// leave it behind. Otherwise HEAD moving never moves the cursor: someone
// browsing old history keeps their place.
func (l *logList) set(cs []git.Commit, lanes [][]graphCell, more bool, branch string) {
	prev, hadPrev := l.selected()
	toHead := l.loaded && branch != l.branch || hadPrev && isHead(prev.Refs)
	l.setItems(cs, func(a, b git.Commit) bool { return true })
	l.lanes, l.more, l.loaded, l.branch = lanes, more, true, branch
	cursor := 0
	for i, c := range cs {
		if toHead && isHead(c.Refs) || !toHead && hadPrev && c.Hash == prev.Hash {
			cursor = i
			break
		}
	}
	l.setCursor(cursor)
}

func (l *logList) view(st theme.Styles, width int, focused bool) string {
	if !l.loaded {
		return ""
	}
	if len(l.items) == 0 {
		return st.Muted.Render("No commits yet")
	}
	rows := l.visible()
	paints := lanePaints(st)
	// Hashes and subjects line up after the widest graph on screen, unless
	// it would take more than half the row; wider graphs push their own
	// row's text further right.
	graphW := 0
	for _, r := range rows {
		if r.kind == rowItem && r.index < len(l.lanes) {
			graphW = max(graphW, len(l.lanes[r.index]))
		}
	}
	graphW = min(graphW, width/2)
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, l.renderRow(st, paints, r, width, graphW, focused))
	}
	return strings.Join(lines, "\n")
}

func (l *logList) renderRow(st theme.Styles, paints []paint, r listRow, width, graphW int, focused bool) string {
	switch r.kind {
	case rowHeader:
		n := strconv.Itoa(r.count)
		if l.more {
			n += "+"
		}
		return st.Section.Render("All branches") + "  " + st.Muted.Render(n)
	case rowItem:
		c := l.items[r.index]
		prefix, subject := "  ", st.Text
		if r.index == l.cursor {
			prefix = st.Muted.Render("› ")
			if focused {
				prefix, subject = st.Cursor.Render("› "), st.Cursor
			}
		}
		var lanes string
		if r.index < len(l.lanes) {
			cells := l.lanes[r.index]
			lanes = renderGraph(paints, cells) + strings.Repeat(" ", max(0, graphW-len(cells))) + "  "
		}
		line := lanes + st.CommitHash.Render(sanitize(c.Hash)) + "  " + refLabels(st, c.Refs) + subject.Render(sanitize(c.Subject))
		return ansi.Truncate(prefix+line, width, "…")
	}
	return ""
}

// styleCommitLine styles one line of the part of `git show` output before
// the patch: the commit line with its refs, author and date, the message,
// and the diffstat.
func styleCommitLine(st theme.Styles, line string) string {
	switch {
	case strings.HasPrefix(line, "commit "):
		hash, refs, _ := strings.Cut(strings.TrimPrefix(line, "commit "), " ")
		return st.Muted.Render("commit ") + st.CommitHash.Render(hash) + styleRefs(st, refs)
	case strings.HasPrefix(line, "    "):
		return st.Text.Render(line)
	case line == "---":
		return st.Subtle.Render(line)
	case strings.HasPrefix(line, " ") && strings.Contains(line, " | "):
		return styleStatLine(st, line)
	case strings.HasPrefix(line, " ") && strings.Contains(line, " changed"):
		return st.Muted.Render(line)
	}
	if k, v, ok := strings.Cut(line, ":"); ok && k != "" && !strings.Contains(k, " ") {
		return st.Muted.Render(k+":") + st.Text.Render(v)
	}
	return st.Text.Render(line)
}

// styleRefs styles a full decoration such as
// "(HEAD -> refs/heads/main, refs/remotes/origin/main, tag: refs/tags/v1)",
// shortening the ref names.
func styleRefs(st theme.Styles, refs string) string {
	if refs == "" {
		return ""
	}
	inner, ok := strings.CutPrefix(refs, "(")
	inner, ok2 := strings.CutSuffix(inner, ")")
	if !ok || !ok2 {
		return st.Muted.Render(" " + refs)
	}
	parts := strings.Split(inner, ", ")
	for i, p := range parts {
		parts[i] = styleRef(st, p)
	}
	return st.Muted.Render(" (") + strings.Join(parts, st.Muted.Render(", ")) + st.Muted.Render(")")
}

func styleRef(st theme.Styles, ref string) string {
	if head, target, ok := strings.Cut(ref, " -> "); ok {
		return styleRef(st, head) + st.Muted.Render(" → ") + st.BranchCurrent.Render(strings.TrimPrefix(target, "refs/heads/"))
	}
	switch {
	case ref == "HEAD":
		return st.Branch.Render(ref)
	case strings.HasPrefix(ref, "refs/heads/"):
		return st.Ahead.Render(strings.TrimPrefix(ref, "refs/heads/"))
	case strings.HasPrefix(ref, "refs/remotes/"):
		return st.BranchRemote.Render(strings.TrimPrefix(ref, "refs/remotes/"))
	case strings.HasPrefix(ref, "tag: "):
		return st.Gone.Render("tag: " + strings.TrimPrefix(strings.TrimPrefix(ref, "tag: "), "refs/tags/"))
	}
	return st.Muted.Render(strings.TrimPrefix(ref, "refs/"))
}

// styleStatLine colors a diffstat line, " path | 12 ++++--": the path as
// text, the count muted, + green and - red.
func styleStatLine(st theme.Styles, line string) string {
	i := strings.LastIndex(line, " | ")
	name, right := line[:i], line[i:]
	j := strings.IndexAny(right, "+-")
	if j < 0 || strings.Contains(right, "Bin ") {
		return st.Text.Render(name) + st.Muted.Render(right)
	}
	graph := right[j:]
	adds := len(graph) - len(strings.TrimLeft(graph, "+"))
	return st.Text.Render(name) + st.Muted.Render(right[:j]) +
		st.DiffAdd.Render(graph[:adds]) + st.DiffDel.Render(graph[adds:])
}
