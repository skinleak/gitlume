package ui

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/skinleak/gitlume/internal/git"
)

// diffDebounce delays diff loading while the cursor is moving quickly, so
// holding j/k does not spawn a git process per row.
const diffDebounce = 30 * time.Millisecond

// statusLoadedMsg carries a fresh git status. seq identifies the request
// so out-of-order replies can be dropped. A poll also reports whether the
// status is the same as the one it was compared with.
type statusLoadedMsg struct {
	seq       int
	status    git.Status
	poll      bool
	unchanged bool
	took      time.Duration // how long git status ran, to pace polling
	at        time.Time     // when it finished
	err       error
}

// pollTickMsg asks for a background status check.
type pollTickMsg struct{ at time.Time }

// diffTickMsg fires after the debounce delay for diff request seq.
type diffTickMsg struct{ seq int }

// diffLoadedMsg carries the diff for key, split into display lines.
type diffLoadedMsg struct {
	seq     int
	key     diffKey
	content diffContent
	err     error
}

// branchesLoadedMsg carries the branch list. now is when it was loaded,
// so ages render the same in every frame.
type branchesLoadedMsg struct {
	seq      int
	branches []git.BranchRef
	now      time.Time
	err      error
}

// commitsTickMsg fires after the debounce delay for commits request seq.
type commitsTickMsg struct{ seq int }

// commitsLoadedMsg carries the recent commits of ref.
type commitsLoadedMsg struct {
	seq     int
	ref     string
	commits []git.Commit
	err     error
}

// logLoadedMsg carries the first limit commits of HEAD, or of all branches
// with their graph lanes.
type logLoadedMsg struct {
	seq     int
	graph   bool
	commits []git.Commit
	lanes   [][]graphCell
	limit   int
	now     time.Time
	err     error
}

// opDoneMsg reports the end of a mutating git operation.
type opDoneMsg struct {
	info string // shown on success; empty for silent operations
	err  error
}

// loadStatus runs git status. For a poll it also compares the result with
// prev here, off the UI goroutine, since a status can have 50k entries.
func loadStatus(repo *git.Repo, seq int, poll bool, prev git.Status) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		st, err := repo.Status(context.Background())
		msg := statusLoadedMsg{seq: seq, status: st, poll: poll, took: time.Since(start), at: time.Now(), err: err}
		msg.unchanged = poll && err == nil && st.Branch == prev.Branch && slices.Equal(st.Entries, prev.Entries)
		return msg
	}
}

func pollTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return pollTickMsg{at: t} })
}

func diffTick(seq int) tea.Cmd {
	return tea.Tick(diffDebounce, func(time.Time) tea.Msg { return diffTickMsg{seq: seq} })
}

func loadDiff(ctx context.Context, repo *git.Repo, seq int, c git.Change) tea.Cmd {
	return func() tea.Msg {
		d, err := repo.Diff(ctx, c)
		if err != nil {
			return diffLoadedMsg{seq: seq, key: keyOf(c), err: err}
		}
		content := diffContent{
			lines:     splitDiff(d.Text),
			truncated: d.Truncated,
			note:      d.Note,
			size:      len(d.Text),
		}
		return diffLoadedMsg{seq: seq, key: keyOf(c), content: content}
	}
}

// branchLogLength is how many commits the branch view shows per branch.
const branchLogLength = 100

func loadBranches(repo *git.Repo, seq int) tea.Cmd {
	return func() tea.Msg {
		bs, err := repo.Branches(context.Background())
		return branchesLoadedMsg{seq: seq, branches: bs, now: time.Now(), err: err}
	}
}

func commitsTick(seq int) tea.Cmd {
	return tea.Tick(diffDebounce, func(time.Time) tea.Msg { return commitsTickMsg{seq: seq} })
}

func loadCommits(ctx context.Context, repo *git.Repo, seq int, ref string) tea.Cmd {
	return func() tea.Msg {
		cs, err := repo.Log(ctx, ref, branchLogLength)
		return commitsLoadedMsg{seq: seq, ref: ref, commits: cs, err: err}
	}
}

func loadLog(ctx context.Context, repo *git.Repo, seq, limit int) tea.Cmd {
	return func() tea.Msg {
		cs, err := repo.Log(ctx, "HEAD", limit)
		return logLoadedMsg{seq: seq, commits: cs, limit: limit, now: time.Now(), err: err}
	}
}

// loadGraph loads all branches' history and lays out its lanes. head adds
// HEAD, which an unborn branch doesn't have.
func loadGraph(ctx context.Context, repo *git.Repo, seq, limit int, head bool) tea.Cmd {
	return func() tea.Msg {
		cs, err := repo.Graph(ctx, limit, head)
		msg := logLoadedMsg{seq: seq, graph: true, commits: cs, limit: limit, now: time.Now(), err: err}
		if err == nil {
			msg.lanes = layoutGraph(cs)
		}
		return msg
	}
}

// loadShow loads one commit for the diff pane; statWidth is the pane width.
func loadShow(ctx context.Context, repo *git.Repo, seq int, hash string, statWidth int) tea.Cmd {
	return func() tea.Msg {
		key := diffKey{commit: hash}
		d, err := repo.Show(ctx, hash, statWidth)
		if err != nil {
			return diffLoadedMsg{seq: seq, key: key, err: err}
		}
		lines := splitDiff(d.Text)
		header := len(lines)
		for i, l := range lines {
			if strings.HasPrefix(l, "diff --") {
				header = i
				break
			}
		}
		content := diffContent{lines: lines, truncated: d.Truncated, size: len(d.Text), header: header}
		return diffLoadedMsg{seq: seq, key: key, content: content}
	}
}

// request holds the cancel func of a running load. Starting the next load
// of the same kind kills the git process of the previous one, whose reply
// would be dropped anyway, so holding j over large diffs never piles up
// git processes.
type request struct{ cancel context.CancelFunc }

func (r *request) start() context.Context {
	r.stop()
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	return ctx
}

func (r *request) stop() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

func runOp(info string, op func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		return opDoneMsg{info: info, err: op(context.Background())}
	}
}

// splitDiff turns patch text into lines that are safe to measure and
// render. It runs inside the tea.Cmd, off the UI goroutine.
func splitDiff(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = sanitize(l)
	}
	return lines
}

// sanitize makes untrusted text (file contents, file names) safe to print:
// tabs are expanded, carriage returns dropped, and every other control
// character, which could move the cursor or start an escape sequence,
// is replaced with '?'.
func sanitize(s string) string {
	if strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == '\r':
		case unicode.IsControl(r):
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
