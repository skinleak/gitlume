package ui

import (
	"slices"
	"strings"
	"testing"

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
			want:    []string{"●─╮", "● │", "│ ●", "●─╯"},
		},
		{
			name:    "two tips fork from one commit",
			commits: commits("a:o", "b:o", "o:"),
			want:    []string{"●", "│ ●", "●─╯"},
		},
		{
			name:    "merge into a lane on the left crosses a passing lane",
			commits: commits("y:c", "z:b", "x:a,c"),
			want:    []string{"●", "│ ●", "├─│─●"},
		},
		{
			name:    "fork and merge on the same side share the line",
			commits: commits("p:q", "r:q", "q:s,t"),
			want:    []string{"●", "│ ●", "●─┴─╮"},
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
	// forks off; the main line keeps the other.
	for i, want := range [][]int32{{0, 1, 1}, {0, -1, 1}, {0, -1, 1}, {0, 1, 1}} {
		if got := color(rows[i]); !slices.Equal(got, want) {
			t.Errorf("row %d colors = %v, want %v", i, got, want)
		}
	}
}

func TestRenderGraphMatchesLipGloss(t *testing.T) {
	withColors(t)
	st := theme.Default()
	row := layoutGraph(commits("m:t,f", "t:o", "f:o", "o:"))[0]
	want := st.Lanes[0].Render("●") + st.Lanes[1].Render("─╮")
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
