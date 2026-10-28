package ui

import (
	"slices"
	"strings"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

// graphCell is one terminal cell of the commit graph. Even cells are
// lanes, odd cells the gaps between them. color indexes theme.Lanes; -1
// is a blank cell.
type graphCell struct {
	r     rune
	color int32
}

var blankCell = graphCell{' ', -1}

// graphEdge is a line that leaves the commit's lane sideways in its row.
type graphEdge struct {
	lane  int
	color int32
	kind  edgeKind
}

type edgeKind int

const (
	edgeFork  edgeKind = iota // a lane from above ends here: the commit is its parent
	edgeMerge                 // a merge parent starts a new lane below
	edgeJoin                  // a merge parent is already on a lane, which goes on
)

// layoutGraph draws one row of lanes per commit. commits must list
// children before parents, as git log does. Each lane waits for the
// commit it leads to; a commit takes the first lane waiting for it, and
// other lanes waiting for it end in its row. Parents beyond the loaded
// commits leave their lanes running off the bottom.
//
// It is proportional to commits × lanes, so it runs with the load, never
// in View.
func layoutGraph(commits []git.Commit) [][]graphCell {
	var lanes []string // the commit each lane leads to; "" is free
	var colors []int32
	var next int32 // the color of the next new lane
	rows := make([][]graphCell, len(commits))
	for n, c := range commits {
		col := slices.Index(lanes, c.ID)
		if col < 0 {
			col = slices.Index(lanes, "")
			if col < 0 {
				col = len(lanes)
				lanes, colors = append(lanes, ""), append(colors, 0)
			}
			colors[col], next = next, next+1
		}
		var edges []graphEdge
		for j, id := range lanes {
			if j != col && id == c.ID {
				edges = append(edges, graphEdge{j, colors[j], edgeFork})
				lanes[j] = ""
			}
		}
		passing := make([]bool, len(lanes))
		for j, id := range lanes {
			passing[j] = j != col && id != ""
		}

		lanes[col] = ""
		var merged []string
		if len(c.Parents) > 0 {
			lanes[col], merged = c.Parents[0], c.Parents[1:]
		}
		for _, p := range merged {
			if k := slices.Index(lanes, p); k >= 0 && k != col {
				edges = append(edges, graphEdge{k, colors[k], edgeJoin})
				continue
			}
			k := freeLane(lanes, col, edges)
			if k == len(lanes) {
				lanes, colors = append(lanes, ""), append(colors, 0)
			}
			lanes[k], colors[k], next = p, next, next+1
			edges = append(edges, graphEdge{k, colors[k], edgeMerge})
		}

		rows[n] = drawRow(len(lanes), col, colors[col], passing, colors, edges)
		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes, colors = lanes[:len(lanes)-1], colors[:len(colors)-1]
		}
	}
	return rows
}

// freeLane picks a lane for a new merge parent: the first free lane right
// of col that no line in this row uses, or a new lane.
func freeLane(lanes []string, col int, edges []graphEdge) int {
	for k := col + 1; k < len(lanes); k++ {
		if lanes[k] == "" && !slices.ContainsFunc(edges, func(e graphEdge) bool { return e.lane == k }) {
			return k
		}
	}
	return len(lanes)
}

func drawRow(width, col int, color int32, passing []bool, colors []int32, edges []graphEdge) []graphCell {
	cells := make([]graphCell, 2*width-1)
	for i := range cells {
		cells[i] = blankCell
	}
	for j, p := range passing {
		if p {
			cells[2*j] = graphCell{'│', colors[j]}
		}
	}
	cells[2*col] = graphCell{'●', color}

	// The horizontal lines: the farthest first, so the nearer ones keep
	// their own color next to the commit. Lanes passing through stay
	// vertical.
	slices.SortFunc(edges, func(a, b graphEdge) int { return abs(b.lane-col) - abs(a.lane-col) })
	isEdge := func(j int) bool {
		return slices.ContainsFunc(edges, func(e graphEdge) bool { return e.lane == j })
	}
	for _, e := range edges {
		lo, hi := 2*min(col, e.lane), 2*max(col, e.lane)
		for i := lo + 1; i < hi; i++ {
			if i%2 == 1 || (!passing[i/2] && !isEdge(i/2)) {
				cells[i] = graphCell{'─', e.color}
			}
		}
	}
	for i, e := range edges {
		right := e.lane > col
		// Only the farthest edge on each side ends the line; nearer ones
		// sit on it.
		end := !slices.ContainsFunc(edges[:i], func(f graphEdge) bool { return (f.lane > col) == right })
		cells[2*e.lane] = graphCell{edgeRune(e.kind, right, end), e.color}
	}
	return cells
}

// edgeGlyphs are, per kind: the line's end on the right, its end on the
// left, and a lane sitting on the line.
var edgeGlyphs = [...][3]rune{
	edgeFork:  {'╯', '╰', '┴'},
	edgeMerge: {'╮', '╭', '┬'},
	edgeJoin:  {'┤', '├', '┼'},
}

func edgeRune(k edgeKind, right, end bool) rune {
	g := edgeGlyphs[k]
	switch {
	case !end:
		return g[2]
	case right:
		return g[0]
	}
	return g[1]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// lanePaint is the escape codes around text in one lane color. Calling
// Render for every run of a wide graph would cost more than the rest of
// the frame, so each lane style renders a placeholder once per frame.
type lanePaint struct{ open, close string }

func lanePaints(st theme.Styles) []lanePaint {
	out := make([]lanePaint, len(st.Lanes))
	for i, s := range st.Lanes {
		out[i].open, out[i].close, _ = strings.Cut(s.Render("\x00"), "\x00")
	}
	return out
}

// renderGraph paints one row of cells, one color per run.
func renderGraph(paints []lanePaint, cells []graphCell) string {
	var b strings.Builder
	for i := 0; i < len(cells); {
		var p lanePaint
		if c := cells[i].color; c >= 0 && len(paints) > 0 {
			p = paints[int(c)%len(paints)]
		}
		b.WriteString(p.open)
		j := i
		for ; j < len(cells) && cells[j].color == cells[i].color; j++ {
			b.WriteRune(cells[j].r)
		}
		b.WriteString(p.close)
		i = j
	}
	return b.String()
}

// refLabels renders a commit's decorations for a graph row, each colored
// like in the branch view. Remote HEAD symrefs are noise and left out.
func refLabels(st theme.Styles, refs []string) string {
	var out []string
	for _, r := range refs {
		r = sanitize(r)
		if _, target, ok := strings.Cut(r, " -> "); ok {
			out = append(out, st.BranchCurrent.Render(strings.TrimPrefix(target, "refs/heads/")))
			continue
		}
		if strings.HasPrefix(r, "refs/remotes/") && strings.HasSuffix(r, "/HEAD") {
			continue
		}
		out = append(out, styleRef(st, r))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ") + " "
}
