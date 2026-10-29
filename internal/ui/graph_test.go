package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

// commits builds a history from "id:parent,parent" specs, children first.
func commits(specs ...string) []git.Commit {
	var out []git.Commit
	for _, s := range specs {
		id, parents, _ := strings.Cut(s, ":")
		c := git.Commit{ID: id, Hash: id, Subject: id}
		if parents != "" {
			c.Parents = strings.Split(parents, ",")
		}
		out = append(out, c)
	}
	return out
}

func graphText(rows [][]graphCell) []string {
	var out []string
	for _, row := range rows {
		var b strings.Builder
		for _, c := range row {
			b.WriteRune(c.r)
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

func TestLayoutGraph(t *testing.T) {
	for _, tc := range []struct {
		name    string
		commits []git.Commit
		want    []string
	}{
		{
			name:    "merge",
			commits: commits("m:t,f", "t:o", "f:o", "o:"),
			want:    []string{"○─╮", "● │", "│ ●", "●─╯"},
		},
		{
			name:    "two tips fork from one commit",
			commits: commits("a:o", "b:o", "o:"),
			want:    []string{"●", "│ ●", "●─╯"},
		},
		{
			name:    "merge into a lane on the left crosses a passing lane",
			commits: commits("y:c", "z:b", "x:a,c"),
			want:    []string{"●", "│ ●", "├─│─○"},
		},
		{
			name:    "fork and merge on the same side share the line",
			commits: commits("p:q", "r:q", "q:s,t"),
			want:    []string{"●", "│ ●", "○─┴─╮"},
		},
		{
			name:    "a parent beyond the loaded page keeps its lane",
			commits: commits("a:x", "b:"),
			want:    []string{"●", "│ ●"},
		},
	} {
		if got := graphText(layoutGraph(tc.commits)); !slices.Equal(got, tc.want) {
			t.Errorf("%s:\n got: %q\nwant: %q", tc.name, got, tc.want)
		}
	}
}

func TestLayoutGraphColors(t *testing.T) {
	rows := layoutGraph(commits("m:t,f", "t:o", "f:o", "o:"))
	color := func(row []graphCell) []int32 {
		var out []int32
		for _, c := range row {
			out = append(out, c.color)
		}
		return out
	}
	// The merged branch keeps one color from where it starts to where it
	// forks off; the main line keeps the other. Without HEAD, no lane gets
	// HEAD's color.
	for i, want := range [][]int32{{1, 2, 2}, {1, -1, 2}, {1, -1, 2}, {1, 2, 2}} {
		if got := color(rows[i]); !slices.Equal(got, want) {
			t.Errorf("row %d colors = %v, want %v", i, got, want)
		}
	}

	// HEAD's line has headColor from HEAD down, and a ◉; a branch ahead of
	// HEAD keeps its own color above it.
	cs := commits("a:h", "h:o", "o:")
	cs[1].Refs = []string{"HEAD -> refs/heads/main"}
	rows = layoutGraph(cs)
	if got := graphText(rows); !slices.Equal(got, []string{"●", "◉", "●"}) {
		t.Errorf("HEAD graph = %q", got)
	}
	for i, want := range []int32{1, headColor, headColor} {
		if got := rows[i][0].color; got != want {
			t.Errorf("row %d color = %d, want %d", i, got, want)
		}
	}
}

func TestLogColumnsAlign(t *testing.T) {
	st := theme.Default()
	cs := commits("a:o", "b:o", "o:")
	var l logList
	l.setHeight(10)
	l.set(cs, layoutGraph(cs), false, "")
	// The graphs "●", "│ ●" and "●─╯" are 1, 3 and 3 cells wide; every
	// hash starts after the widest: "› " + 3 cells + "  ".
	lines := strings.Split(ansi.Strip(l.view(st, 80, true)), "\n")[1:]
	for i, c := range cs {
		if got := ansi.StringWidth(lines[i][:strings.Index(lines[i], c.Hash+"  ")]); got != 7 {
			t.Errorf("row %d: hash at column %d, want 7:\n%s", i, got, strings.Join(lines, "\n"))
		}
	}
}

func TestRenderGraphMatchesLipGloss(t *testing.T) {
	withColors(t)
	st := theme.Default()
	row := layoutGraph(commits("m:t,f", "t:o", "f:o", "o:"))[0]
	want := st.Lanes[1].Render("○") + st.Lanes[2].Render("─╮")
	if got := renderGraph(lanePaints(st), row); got != want {
		t.Errorf("renderGraph = %q, want %q", got, want)
	}
	blank := []graphCell{{'│', 0}, blankCell, {'●', 1}}
	if got, want := renderGraph(lanePaints(st), blank), st.Lanes[0].Render("│")+" "+st.Lanes[1].Render("●"); got != want {
		t.Errorf("renderGraph with a gap = %q, want %q", got, want)
	}
}

func TestRefLabels(t *testing.T) {
	withColors(t)
	st := theme.Default()
	got := refLabels(st, []string{"HEAD -> refs/heads/main", "refs/remotes/origin/main", "refs/remotes/origin/HEAD", "tag: refs/tags/v1"})
	want := st.BranchCurrent.Render("main") + " " + st.BranchRemote.Render("origin/main") + " " + st.Gone.Render("tag: v1") + " "
	if got != want {
		t.Errorf("refLabels = %q, want %q", got, want)
	}
	if refLabels(st, nil) != "" {
		t.Error("no refs should render nothing")
	}
}
