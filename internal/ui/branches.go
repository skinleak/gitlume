package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

// branchList shows local branches, then remote-tracking ones, in the
// order git.Branches returns them.
type branchList struct {
	sectionList[git.BranchRef]
	loaded bool
	now    time.Time // when the list was loaded; ages are relative to it
}

// set replaces the list, keeping the cursor on the same branch. On the
// first load the cursor starts on the current branch.
func (l *branchList) set(bs []git.BranchRef, now time.Time) {
	prev, hadPrev := l.selected()
	l.setItems(bs, func(a, b git.BranchRef) bool { return a.Remote == b.Remote })
	l.now = now
	cursor := l.cursor
	for i, b := range bs {
		if hadPrev && b.Name == prev.Name && b.Remote == prev.Remote || !hadPrev && b.Current {
			cursor = i
			break
		}
	}
	l.loaded = true
	l.setCursor(cursor)
}

// localFor returns the local branch that a checkout of remote b would
// switch to, if one exists: origin/feature → feature.
func (l *branchList) localFor(b git.BranchRef) (git.BranchRef, bool) {
	_, name, ok := strings.Cut(b.Name, "/")
	if !ok {
		return git.BranchRef{}, false
	}
	for _, o := range l.items {
		if !o.Remote && o.Name == name {
			return o, true
		}
	}
	return git.BranchRef{}, false
}

func (l *branchList) view(st theme.Styles, width int) string {
	if !l.loaded {
		return ""
	}
	if len(l.items) == 0 {
		return st.Muted.Render("No branches yet")
	}
	rows := l.visible()
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, l.renderRow(st, r, width))
	}
	return strings.Join(lines, "\n")
}

func (l *branchList) renderRow(st theme.Styles, r listRow, width int) string {
	switch r.kind {
	case rowHeader:
		title := "Local"
		if l.items[r.index].Remote {
			title = "Remote"
		}
		return st.Section.Render(title) + "  " + st.Muted.Render(strconv.Itoa(r.count))
	case rowItem:
		b := l.items[r.index]
		mark := " "
		if b.Current {
			mark = "*"
		}
		track, trackW := trackView(st, b)
		ago := age(l.now.Sub(b.Date))
		// "› * name" on the left, "↑1↓2  3d" right-aligned.
		suffixW := len(ago)
		if trackW > 0 {
			suffixW += trackW + 2
		}
		nameW := width - 4 - suffixW - 2
		showSuffix := nameW >= 8
		if !showSuffix {
			nameW = width - 4
		}
		name := truncateLeft(sanitize(b.Name), nameW)

		nameStyle := st.Text
		switch {
		case r.index == l.cursor:
			nameStyle = st.Cursor
		case b.Current:
			nameStyle = st.BranchCurrent
		case b.Remote:
			nameStyle = st.BranchRemote
		}
		left := "  "
		if r.index == l.cursor {
			left = st.Cursor.Render("› ")
		}
		left += st.BranchCurrent.Render(mark) + " " + nameStyle.Render(name)
		if !showSuffix {
			return left
		}
		right := st.Subtle.Render(ago)
		if trackW > 0 {
			right = track + "  " + right
		}
		pad := strings.Repeat(" ", max(2, width-4-ansi.StringWidth(name)-suffixW))
		return left + pad + right
	}
	return ""
}

// trackView renders the compact upstream state ("↑1↓2", "gone" or "")
// and returns its width.
func trackView(st theme.Styles, b git.BranchRef) (string, int) {
	if b.Gone {
		return st.Gone.Render("gone"), len("gone")
	}
	var s string
	w := 0
	if b.Ahead > 0 {
		n := strconv.Itoa(b.Ahead)
		s += st.Ahead.Render("↑" + n)
		w += 1 + len(n)
	}
	if b.Behind > 0 {
		n := strconv.Itoa(b.Behind)
		s += st.Behind.Render("↓" + n)
		w += 1 + len(n)
	}
	return s, w
}

// age formats d compactly: now, 5m, 3h, 2d, 3w, 4mo, 2y.
func age(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < day:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 14*day:
		return strconv.Itoa(int(d/day)) + "d"
	case d < 60*day:
		return strconv.Itoa(int(d/(7*day))) + "w"
	case d < 365*day:
		return strconv.Itoa(int(d/(30*day))) + "mo"
	}
	return strconv.Itoa(int(d/(365*day))) + "y"
}

// commitsView lists the recent commits of the selected branch.
type commitsView struct {
	ref     string
	shown   bool
	commits []git.Commit
	now     time.Time
	offset  int
	height  int
}

func (v *commitsView) show(ref string, cs []git.Commit, now time.Time) {
	if !v.shown || ref != v.ref {
		v.offset = 0
	}
	v.ref, v.shown, v.commits, v.now = ref, true, cs, now
	v.scroll(0)
}

func (v *commitsView) clear() { *v = commitsView{height: v.height} }

func (v *commitsView) setHeight(h int) {
	v.height = max(1, h)
	v.scroll(0)
}

func (v *commitsView) scroll(delta int) {
	v.offset = clamp(v.offset+delta, 0, len(v.commits)-v.height)
}

func (v *commitsView) view(st theme.Styles, width int) string {
	if !v.shown {
		return ""
	}
	if len(v.commits) == 0 {
		return st.Muted.Render("No commits")
	}
	end := min(len(v.commits), v.offset+v.height)
	out := make([]string, 0, end-v.offset)
	for _, c := range v.commits[v.offset:end] {
		out = append(out, ansi.Truncate(commitLine(st, c, v.now, st.Text), width, "…"))
	}
	return strings.Join(out, "\n")
}

// commitLine renders "hash  age  subject", a row of the branch view's
// commit pane.
func commitLine(st theme.Styles, c git.Commit, now time.Time, subject lipgloss.Style) string {
	ago := age(now.Sub(c.Date))
	return st.CommitHash.Render(sanitize(c.Hash)) + "  " +
		st.Subtle.Render(ago+strings.Repeat(" ", max(0, 3-len(ago)))) + "  " +
		subject.Render(sanitize(c.Subject))
}
