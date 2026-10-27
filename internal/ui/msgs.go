package ui

import (
	"context"
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
// so out-of-order replies can be dropped.
type statusLoadedMsg struct {
	seq    int
	status git.Status
	err    error
}

// diffTickMsg fires after the debounce delay for diff request seq.
type diffTickMsg struct{ seq int }

// diffLoadedMsg carries the diff for key, split into display lines.
type diffLoadedMsg struct {
	seq     int
	key     diffKey
	content diffContent
	err     error
}

// opDoneMsg reports the end of a mutating git operation.
type opDoneMsg struct {
	info string // shown on success; empty for silent operations
	err  error
}

func loadStatus(repo *git.Repo, seq int) tea.Cmd {
	return func() tea.Msg {
		st, err := repo.Status(context.Background())
		return statusLoadedMsg{seq: seq, status: st, err: err}
	}
}

func diffTick(seq int) tea.Cmd {
	return tea.Tick(diffDebounce, func(time.Time) tea.Msg { return diffTickMsg{seq: seq} })
}

func loadDiff(repo *git.Repo, seq int, c git.Change) tea.Cmd {
	return func() tea.Msg {
		d, err := repo.Diff(context.Background(), c)
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
