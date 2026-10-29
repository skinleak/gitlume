package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

func manyChanges(n int) []git.Change {
	cs := make([]git.Change, n)
	for i := range cs {
		cs[i] = git.Change{Group: groupOrder[i*3/n], Code: 'M', Path: fmt.Sprintf("src/pkg/file%d.go", i)}
	}
	return cs
}

// BenchmarkFileListMoveAndView is one keypress in a 50k-file status.
func BenchmarkFileListMoveAndView(b *testing.B) {
	st := theme.Default()
	var l fileList
	l.setHeight(40)
	l.setChanges(manyChanges(50_000))
	b.ReportAllocs()
	for b.Loop() {
		if !l.move(1) {
			l.cursor = 0
		}
		_ = l.view(st, 40, true)
	}
}

// BenchmarkDiffView renders one frame of a maximum-size diff.
func BenchmarkDiffView(b *testing.B) {
	st := theme.Default()
	line := "+\tsome added line of code with a fair amount of text in it\r"
	lines := splitDiff(strings.Repeat(line+"\n", git.MaxDiffBytes/len(line)))
	d := diffView{height: 50}
	d.show(diffKey{path: "x"}, diffContent{lines: lines})
	b.ReportAllocs()
	for b.Loop() {
		d.scroll(1)
		_ = d.view(st, 120)
	}
}

// BenchmarkSplitDiff prepares a maximum-size diff of many files for
// display, as loadDiff and loadShow do.
func BenchmarkSplitDiff(b *testing.B) {
	file := "diff --git a/src/file.go b/src/file.go\nindex 6cb5c6c..de98044 100644\n--- a/src/file.go\n+++ b/src/file.go\n@@ -1,3 +1,50 @@\n" +
		strings.Repeat("+\tsome added line of code with a fair amount of text\n", 50)
	text := strings.Repeat(file, git.MaxDiffBytes/len(file))
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for b.Loop() {
		_, _ = tidyPatch(splitDiff(text), false)
	}
}

// wideHistory is n commits on 40 branches that run side by side down to a
// shared root, so every row has 40 lanes.
func wideHistory(n int) []git.Commit {
	const branches = 40
	cs := make([]git.Commit, 0, n+1)
	for i := range n {
		c := git.Commit{ID: fmt.Sprint(i), Hash: fmt.Sprint(i), Subject: "a commit subject of ordinary length"}
		if i+branches < n {
			c.Parents = []string{fmt.Sprint(i + branches)}
		} else {
			c.Parents = []string{"root"}
		}
		cs = append(cs, c)
	}
	return append(cs, git.Commit{ID: "root", Hash: "root"})
}

// BenchmarkLayoutGraph lays out ten pages of a 40-lane history.
func BenchmarkLayoutGraph(b *testing.B) {
	cs := wideHistory(10 * logPage)
	b.ReportAllocs()
	for b.Loop() {
		_ = layoutGraph(cs)
	}
}

// BenchmarkLogMoveAndView is one keypress in that graph.
func BenchmarkLogMoveAndView(b *testing.B) {
	st := theme.Default()
	cs := wideHistory(10 * logPage)
	var l logList
	l.setHeight(50)
	l.set(cs, layoutGraph(cs), false, "")
	b.ReportAllocs()
	for b.Loop() {
		if !l.move(1) {
			l.cursor = 0
		}
		_ = l.view(st, 100, true)
	}
}

// BenchmarkFullFrame is one keypress and a full frame at 220×60 with 50k
// changed files and a diff on screen: everything the UI goroutine does.
func BenchmarkFullFrame(b *testing.B) {
	m := New(&git.Repo{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 220, Height: 60})
	m = next.(Model)
	m.files.setChanges(manyChanges(50_000))
	line := "+\tsome added line of code with a fair amount of text in it"
	m.diff.show(diffKey{path: "x"}, diffContent{lines: splitDiff(strings.Repeat(line+"\n", 2000))})
	down := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	b.ReportAllocs()
	for b.Loop() {
		next, _ := m.Update(down)
		m = next.(Model)
		if m.files.cursor == len(m.files.items)-1 {
			m.files.cursor = 0
		}
		_ = m.View()
	}
}
