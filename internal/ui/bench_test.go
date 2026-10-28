package ui

import (
	"fmt"
	"strings"
	"testing"

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

// BenchmarkSplitDiff prepares a maximum-size diff for display.
func BenchmarkSplitDiff(b *testing.B) {
	text := strings.Repeat("+\tsome added line of code with a fair amount of text\n", git.MaxDiffBytes/52)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for b.Loop() {
		_ = splitDiff(text)
	}
}
