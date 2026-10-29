package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

func newTestModel(t *testing.T) (Model, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
	} {
		gitRun(t, dir, args...)
	}
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "b.txt", "one\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	write(t, dir, "a.txt", "one\ntwo\n")
	write(t, dir, "b.txt", "changed\n")

	repo, err := git.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(repo).WithoutPolling()
	m = pump(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drain(t, m, m.Init())
	return m, dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pump delivers msg and synchronously runs the resulting commands.
func pump(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	return drain(t, next.(Model), cmd)
}

// drain runs cmd and feeds its messages back in. The step limit stops
// self-renewing commands such as the cursor blink.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for steps := 0; cmd != nil && steps < 8; steps++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = drain(t, m, c)
			}
			return m
		}
		if msg == nil {
			return m
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m
}

func keyPress(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func plainView(m Model) string { return ansi.Strip(m.View()) }

func TestInitialView(t *testing.T) {
	m, _ := newTestModel(t)
	v := plainView(m)
	for _, want := range []string{"main", "Unstaged", "a.txt", "b.txt", "a.txt  +1", "+two"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestViewFitsWindow(t *testing.T) {
	m, _ := newTestModel(t)
	for _, size := range [][2]int{{100, 30}, {60, 20}, {200, 50}, {30, 8}, {20, 4}, {10, 2}} {
		m = pump(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, mode := range []string{"main", "help", "commit", "zoom", "zoom help", "confirm", "branches", "log", "log zoom", "log branches"} {
			mm := m
			switch mode {
			case "help":
				mm = pump(t, mm, keyPress("?"))
			case "commit":
				mm = pump(t, mm, keyPress("a"))
				mm = pump(t, mm, keyPress("c"))
			case "zoom":
				mm = pump(t, mm, keyPress("enter"))
			case "zoom help":
				mm = pump(t, mm, keyPress("enter"))
				mm = pump(t, mm, keyPress("?"))
			case "confirm":
				mm = pump(t, mm, keyPress("D"))
			case "branches":
				mm = pump(t, mm, keyPress("b"))
			case "log branches":
				mm = pump(t, mm, keyPress("2"))
				mm = pump(t, mm, keyPress("b"))
			case "log":
				mm = pump(t, mm, keyPress("2"))
			case "log zoom":
				mm = pump(t, mm, keyPress("2"))
				mm = pump(t, mm, keyPress("enter"))
			}
			lines := strings.Split(plainView(mm), "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d %s: %d lines, want %d", size[0], size[1], mode, len(lines), size[1])
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%dx%d %s: line %d is %d wide: %q", size[0], size[1], mode, i, w, l)
				}
			}
		}
	}
}

func TestStageKeepsCursorPosition(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, keyPress(" "))

	if got := m.files.count(git.Staged); got != 1 {
		t.Fatalf("staged = %d, want 1", got)
	}
	c, _ := m.files.selected()
	if c.Group != git.Unstaged || c.Path != "b.txt" {
		t.Errorf("selected = %+v, want unstaged b.txt", c)
	}
}

func TestCommitFlow(t *testing.T) {
	m, _ := newTestModel(t)

	m = pump(t, m, keyPress("c"))
	if m.commit.active {
		t.Fatal("commit prompt opened with nothing staged")
	}

	m = pump(t, m, keyPress("a"))
	m = pump(t, m, keyPress("c"))
	if !m.commit.active {
		t.Fatal("commit prompt did not open")
	}
	for _, r := range "first change" {
		m = pump(t, m, keyPress(string(r)))
	}
	if q := strings.Contains(plainView(m), "first change"); !q {
		t.Fatalf("prompt does not show typed text:\n%s", plainView(m))
	}
	m = pump(t, m, keyPress("enter"))

	v := plainView(m)
	if !strings.Contains(v, "Working tree clean") || !strings.Contains(v, "Committed") {
		t.Errorf("after commit:\n%s", v)
	}
}

func TestCommitCancelKeepsDraft(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, keyPress("a"))
	m = pump(t, m, keyPress("c"))
	m = pump(t, m, keyPress("x"))
	m = pump(t, m, keyPress("esc"))
	if m.commit.active {
		t.Fatal("esc did not close the prompt")
	}
	if got := m.commit.input.Value(); got != "x" {
		t.Errorf("draft = %q, want %q", got, "x")
	}
}

func TestGitErrorShownInStatusLine(t *testing.T) {
	m, dir := newTestModel(t)
	// A held index lock makes staging fail.
	write(t, filepath.Join(dir, ".git"), "index.lock", "")
	m = pump(t, m, keyPress(" "))
	if !m.isErr || !strings.Contains(m.message, "index.lock") {
		t.Errorf("expected lock error in status line:\n%s", plainView(m))
	}
}

func TestDiffFromPreviousViewDropped(t *testing.T) {
	m, _ := newTestModel(t)
	seq := m.diffSeq
	// The stale reply arrives after the switch but before the log loads.
	next, loadLog := m.Update(keyPress("2"))
	m = pump(t, next.(Model), diffLoadedMsg{seq: seq, key: diffKey{group: git.Unstaged, path: "a.txt"},
		content: diffContent{lines: []string{"+stale file diff"}}})
	if strings.Contains(plainView(m), "stale file diff") {
		t.Errorf("a file diff from the files view reached the log:\n%s", plainView(m))
	}
	m = drain(t, m, loadLog)
	if !strings.Contains(plainView(m), "Author: Test") {
		t.Errorf("log details missing after the log loaded:\n%s", plainView(m))
	}
}

func TestStaleRepliesIgnored(t *testing.T) {
	m, _ := newTestModel(t)
	before := plainView(m)

	m = pump(t, m, diffLoadedMsg{seq: m.diffSeq - 1, key: m.diff.key, content: diffContent{lines: []string{"+stale"}}})
	m = pump(t, m, statusLoadedMsg{seq: m.statusSeq - 1})

	if after := plainView(m); after != before {
		t.Errorf("stale replies changed the view:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRestoreCursor(t *testing.T) {
	ch := func(g git.Group, p string) git.Change { return git.Change{Group: g, Path: p} }
	var l fileList
	l.setHeight(10)
	l.setChanges([]git.Change{ch(git.Unstaged, "a"), ch(git.Unstaged, "b"), ch(git.Unstaged, "c")})
	l.move(1) // on b

	// b moved to Staged: stay at the same position among unstaged files.
	l.setChanges([]git.Change{ch(git.Staged, "b"), ch(git.Unstaged, "a"), ch(git.Unstaged, "c")})
	if c, _ := l.selected(); c.Path != "c" || c.Group != git.Unstaged {
		t.Errorf("selected %+v, want unstaged c", c)
	}

	// The group emptied: fall back to a valid index.
	l.setChanges([]git.Change{ch(git.Staged, "a")})
	if c, ok := l.selected(); !ok || c.Path != "a" {
		t.Errorf("selected %+v, %v; want staged a", c, ok)
	}

	l.setChanges(nil)
	if _, ok := l.selected(); ok {
		t.Error("selection on empty list")
	}
}

func TestSplitDiff(t *testing.T) {
	got := splitDiff("a\tb\r\n+c\n\n")
	want := []string{"a    b", "+c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitDiff = %q, want %q", got, want)
	}
	if splitDiff("") != nil {
		t.Error("empty diff should have no lines")
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\tb", "a    b"},
		{"crlf\r", "crlf"},
		{"esc\x1b[31mred", "esc?[31mred"},
		{"bell\x07", "bell?"},
		{"c1\u009bcsi", "c1?csi"},
		{"ünïcödé", "ünïcödé"},
	}
	for _, tt := range tests {
		if got := sanitize(tt.in); got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCommitWhileBusyKeepsDraft(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, keyPress("a"))
	m = pump(t, m, keyPress("c"))
	m = pump(t, m, keyPress("x"))

	m.busy = true
	m = pump(t, m, keyPress("enter"))
	if !m.commit.active || m.commit.input.Value() != "x" {
		t.Fatalf("active = %v, draft = %q; want prompt kept with draft", m.commit.active, m.commit.input.Value())
	}
	if !strings.Contains(plainView(m), "Still working") {
		t.Errorf("busy message not visible:\n%s", plainView(m))
	}
}

func TestEmptyCommitMessageErrorVisible(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, keyPress("a"))
	m = pump(t, m, keyPress("c"))
	m = pump(t, m, keyPress("enter"))
	if !m.commit.active || !strings.Contains(plainView(m), "commit message is empty") {
		t.Errorf("expected prompt with visible error:\n%s", plainView(m))
	}
}

func TestDiffCacheIsBounded(t *testing.T) {
	m, _ := newTestModel(t)
	big := diffContent{lines: []string{"+x"}, size: maxDiffCacheBytes/2 + 1}
	for i := range 5 {
		m = pump(t, m, diffLoadedMsg{seq: m.diffSeq, key: diffKey{path: strconv.Itoa(i)}, content: big})
		if m.cacheSize > maxDiffCacheBytes {
			t.Fatalf("cache holds %d bytes, budget %d", m.cacheSize, maxDiffCacheBytes)
		}
	}
}

func TestControlCharsInFileName(t *testing.T) {
	m, dir := newTestModel(t)
	write(t, dir, "evil\x1b[2Jname.txt", "x\n")
	m = pump(t, m, keyPress("r"))
	v := m.View()
	if strings.Contains(v, "\x1b[2J") {
		t.Fatal("raw escape sequence from a file name reached the terminal")
	}
	if !strings.Contains(ansi.Strip(v), "evil?[2Jname.txt") {
		t.Errorf("sanitized name not shown:\n%s", ansi.Strip(v))
	}
}

func TestDiscardAsksFirst(t *testing.T) {
	m, dir := newTestModel(t)
	m = pump(t, m, keyPress("d"))
	if !m.confirm.active || !strings.Contains(plainView(m), "Discard all changes to a.txt?") {
		t.Fatalf("no confirmation shown:\n%s", plainView(m))
	}
	// Only an explicit y confirms; other keys, including enter, do nothing.
	for _, k := range []string{"enter", " ", "j", "n"} {
		m = pump(t, m, keyPress(k))
	}
	if m.confirm.active {
		t.Fatal("n did not close the question")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("a.txt changed without confirmation: %q", got)
	}

	m = pump(t, m, keyPress("d"))
	m = pump(t, m, keyPress("y"))
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "one\n" {
		t.Errorf("a.txt = %q after discard, want %q", got, "one\n")
	}
	if n := m.files.count(git.Unstaged); n != 1 {
		t.Errorf("unstaged = %d after discard, want 1", n)
	}
	if !strings.Contains(plainView(m), "Discarded a.txt") {
		t.Errorf("no confirmation message:\n%s", plainView(m))
	}
}

func TestDiscardAll(t *testing.T) {
	m, dir := newTestModel(t)
	write(t, dir, "new.txt", "x\n")
	m = pump(t, m, keyPress("r"))
	m = pump(t, m, keyPress("D"))
	m = pump(t, m, keyPress("esc"))
	if m.confirm.active || len(m.files.items) != 3 {
		t.Fatalf("esc: active = %v, changes = %d; want closed and untouched", m.confirm.active, len(m.files.items))
	}
	m = pump(t, m, keyPress("D"))
	m = pump(t, m, keyPress("y"))
	if v := plainView(m); !strings.Contains(v, "Working tree clean") {
		t.Errorf("after discard all:\n%s", v)
	}
	m = pump(t, m, keyPress("D"))
	if m.confirm.active {
		t.Error("asked to discard a clean tree")
	}
}

func TestZoomedDiff(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, keyPress("enter"))
	if !m.zoomed || m.diff.height != m.height-chromeHeight {
		t.Fatalf("zoomed = %v, diff height = %d; want fullscreen", m.zoomed, m.diff.height)
	}
	v := plainView(m)
	if !strings.Contains(v, "+two") || strings.Contains(v, "Unstaged") {
		t.Errorf("zoomed view should show only the diff:\n%s", v)
	}

	// Viewing keys only: j scrolls the diff, space does not stage.
	cursor := m.files.cursor
	m = pump(t, m, keyPress("j"))
	m = pump(t, m, keyPress(" "))
	if m.files.cursor != cursor || m.files.count(git.Staged) != 0 {
		t.Error("keys in the zoomed diff changed the file list")
	}

	m = pump(t, m, keyPress("esc"))
	if m.zoomed || !strings.Contains(plainView(m), "Unstaged") {
		t.Errorf("esc did not return to the panes:\n%s", plainView(m))
	}
}

func TestConfirmHintVisibleWhenNarrow(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, tea.WindowSizeMsg{Width: 50, Height: 20})
	m = pump(t, m, keyPress("D"))
	if v := plainView(m); !strings.Contains(v, "y yes · n no") {
		t.Errorf("y/n hint cut off:\n%s", v)
	}
}

func TestCommitHintFits(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, keyPress("a"))
	m = pump(t, m, keyPress("c"))
	for _, r := range "a message long enough to reach the hint at the end of the line" {
		m = pump(t, m, keyPress(string(r)))
	}
	if v := plainView(m); !strings.Contains(v, "esc cancel") {
		t.Errorf("commit hint cut off:\n%s", v)
	}
}

func TestBranchList(t *testing.T) {
	m, dir := newTestModel(t)
	gitRun(t, dir, "branch", "feature")
	if v := plainView(m); !strings.Contains(v, "1 Files   2 Log") || strings.Contains(v, "Branches") {
		t.Fatalf("want two tabs, Files and Log:\n%s", v)
	}

	m = pump(t, m, keyPress("b"))
	v := plainView(m)
	for _, want := range []string{"Local  2", "* main", "feature", "init", "enter checkout", "esc back"} {
		if !strings.Contains(v, want) {
			t.Errorf("branch list missing %q:\n%s", want, v)
		}
	}
	if b, _ := m.branches.selected(); !b.Current {
		t.Errorf("cursor starts on %q, want the current branch", b.Name)
	}

	m = pump(t, m, keyPress("enter"))
	if !m.branchesOpen || !strings.Contains(plainView(m), "Already on main") {
		t.Errorf("checkout of the current branch:\n%s", plainView(m))
	}

	// A checkout closes the list: the files are back, with their diff.
	m = selectBranch(t, m, "feature")
	m = pump(t, m, keyPress("enter"))
	if m.status.Branch.Head != "feature" || !strings.Contains(plainView(m), "Switched to feature") {
		t.Fatalf("head = %q after checkout:\n%s", m.status.Branch.Head, plainView(m))
	}
	if v := plainView(m); m.branchesOpen || !strings.Contains(v, "a.txt") || !strings.Contains(v, "+two") {
		t.Errorf("files view after checkout:\n%s", v)
	}
	m = pump(t, m, keyPress("b"))
	if b, _ := m.branches.selected(); b.Name != "feature" || !b.Current {
		t.Errorf("selection after checkout = %+v, want current feature", b)
	}
}

func TestBranchListReturnsToItsView(t *testing.T) {
	m, _ := newTestModel(t)
	m = pump(t, m, keyPress("2"))
	for _, closer := range []string{"esc", "b"} {
		m = pump(t, m, keyPress("b"))
		if !m.branchesOpen || !strings.Contains(plainView(m), "Local  1") {
			t.Fatalf("b in the log did not open the branch list:\n%s", plainView(m))
		}
		m = pump(t, m, keyPress(closer))
		if v := plainView(m); m.branchesOpen || m.view != viewLog || !strings.Contains(v, "All branches") || !strings.Contains(v, "Author:") {
			t.Errorf("%s did not return to the log:\n%s", closer, v)
		}
	}

	// A tab key closes the list and shows that tab, even the one beneath.
	m = pump(t, m, keyPress("b"))
	m = pump(t, m, keyPress("1"))
	if v := plainView(m); m.branchesOpen || m.view != viewFiles || !strings.Contains(v, "+two") {
		t.Errorf("1 did not show the files:\n%s", v)
	}
	m = pump(t, m, keyPress("b"))
	m = pump(t, m, keyPress("1"))
	if m.branchesOpen {
		t.Error("1 did not close the list over the files")
	}

	// Files keys do nothing while the list covers the files.
	m = pump(t, m, keyPress("b"))
	m = pump(t, m, keyPress(" "))
	if m.files.count(git.Staged) != 0 {
		t.Error("space staged a file under the branch list")
	}
}

func TestCheckoutRefusalShown(t *testing.T) {
	m, dir := newTestModel(t)
	gitRun(t, dir, "stash", "-q")
	gitRun(t, dir, "switch", "-q", "-c", "other")
	write(t, dir, "a.txt", "other\n")
	gitRun(t, dir, "commit", "-q", "-am", "other")
	gitRun(t, dir, "switch", "-q", "main")
	gitRun(t, dir, "stash", "pop", "-q")

	m = pump(t, m, keyPress("b"))
	m = selectBranch(t, m, "other")
	m = pump(t, m, keyPress("enter"))
	if !m.isErr || !strings.Contains(m.message, "would be overwritten") || m.status.Branch.Head != "main" {
		t.Errorf("want git's refusal in the status line, head main:\n%s", plainView(m))
	}
}

// selectBranch moves the branch cursor to name with k/j presses.
func selectBranch(t *testing.T, m Model, name string) Model {
	t.Helper()
	for range m.branches.items {
		m = pump(t, m, keyPress("k"))
	}
	for range m.branches.items {
		if b, _ := m.branches.selected(); b.Name == name {
			return m
		}
		m = pump(t, m, keyPress("j"))
	}
	t.Fatalf("branch %q not listed", name)
	return m
}

func TestFooterKeepsHelpAndQuit(t *testing.T) {
	m, _ := newTestModel(t)
	for _, w := range []int{120, 90, 70, 50} {
		m = pump(t, m, tea.WindowSizeMsg{Width: w, Height: 20})
		lines := strings.Split(plainView(m), "\n")
		footer := lines[len(lines)-1]
		if !strings.HasSuffix(strings.TrimSpace(footer), "? help · q quit") {
			t.Errorf("width %d: footer = %q, want it to end with help and quit", w, footer)
		}
		if !strings.Contains(footer, "space stage/unstage") {
			t.Errorf("width %d: footer lost its first hint: %q", w, footer)
		}
	}
}

func TestStatusLetters(t *testing.T) {
	m, dir := newTestModel(t)
	write(t, dir, "new.txt", "x\n")
	m = pump(t, m, keyPress("r"))
	v := plainView(m)
	for _, want := range []string{"M a.txt", "U new.txt"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	for code, want := range map[byte]byte{'?': 'U', 'U': '!', 'M': 'M', 'A': 'A', 'D': 'D'} {
		if got := displayCode(code); got != want {
			t.Errorf("displayCode(%c) = %c, want %c", code, got, want)
		}
	}
}

func TestLogView(t *testing.T) {
	m, dir := newTestModel(t)
	gitRun(t, dir, "commit", "-q", "-am", "second commit")
	m = pump(t, m, keyPress("r"))
	m = pump(t, m, keyPress("2"))
	v := plainView(m)
	for _, want := range []string{
		"2 Log", "All branches  2", "second commit", "init",
		"(HEAD → main)", "Author: Test", " a.txt | 1 +", " b.txt | 2 +-", "a.txt  +1", "b.txt  +1  -1",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("log view missing %q:\n%s", want, v)
		}
	}
	// git's own file header lines are replaced by one line per file.
	for _, gone := range []string{"diff --git", "index ", "+++ b/"} {
		if strings.Contains(v, gone) {
			t.Errorf("log view shows %q:\n%s", gone, v)
		}
	}

	m = pump(t, m, keyPress("j"))
	if v := plainView(m); !strings.Contains(v, "    init") {
		t.Errorf("details did not follow the cursor:\n%s", v)
	}
	m = pump(t, m, keyPress("enter"))
	if !m.zoomed || !strings.Contains(plainView(m), "Author: Test") {
		t.Errorf("enter should show the commit fullscreen:\n%s", plainView(m))
	}
	m = pump(t, m, keyPress("esc"))
	m = pump(t, m, keyPress("1"))
	if v := plainView(m); !strings.Contains(v, "Working tree clean") {
		t.Errorf("files view after the log:\n%s", v)
	}
}

func TestLogLoadsMoreWhenNearTheEnd(t *testing.T) {
	m, dir := newTestModel(t)
	gitRun(t, dir, "commit", "-q", "-am", "second")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "third")
	m.log.limit = 1
	m = pump(t, m, keyPress("2"))
	if len(m.log.items) != 1 || !strings.Contains(plainView(m), "All branches  1+") {
		t.Fatalf("first page: %d commits:\n%s", len(m.log.items), plainView(m))
	}
	m = pump(t, m, keyPress("j"))
	if len(m.log.items) != 3 || m.log.more {
		t.Errorf("after paging: %d commits, more = %v; want 3, false", len(m.log.items), m.log.more)
	}
}

func TestLogOnUnbornBranch(t *testing.T) {
	m, dir := newTestModel(t)
	gitRun(t, dir, "checkout", "-q", "--orphan", "fresh")
	m = pump(t, m, keyPress("r"))
	m = pump(t, m, keyPress("2"))
	// HEAD has no commits, but the other branches still show.
	if v := plainView(m); !strings.Contains(v, "All branches  1") || !strings.Contains(v, "main init") || m.isErr {
		t.Errorf("log on an unborn branch:\n%s", v)
	}

	m = newEmptyModel(t, "empty")
	m = pump(t, m, keyPress("2"))
	if v := plainView(m); !strings.Contains(v, "No commits yet") || m.isErr {
		t.Errorf("log of an empty repository:\n%s", v)
	}
}

func TestArrowsSwitchTabs(t *testing.T) {
	m, _ := newTestModel(t)
	right, left := tea.KeyMsg{Type: tea.KeyRight}, tea.KeyMsg{Type: tea.KeyLeft}
	for _, want := range []viewKind{viewLog, viewFiles} {
		if m = pump(t, m, right); m.view != want {
			t.Fatalf("→: view %d, want %d", m.view, want)
		}
	}
	if m = pump(t, m, left); m.view != viewLog {
		t.Errorf("← from files should wrap to the log, got view %d", m.view)
	}
	if v := plainView(m); !strings.Contains(v, "All branches") {
		t.Errorf("← did not load the log:\n%s", v)
	}

	// While typing a commit message the arrows move the text cursor.
	m = pump(t, m, keyPress("1"))
	m = pump(t, m, keyPress("a"))
	m = pump(t, m, keyPress("c"))
	if m = pump(t, m, right); m.view != viewFiles || !m.commit.active {
		t.Errorf("→ in the commit prompt switched tabs")
	}
	m = pump(t, m, keyPress("esc"))

	// A fullscreen diff stays put.
	m = pump(t, m, keyPress("enter"))
	if m = pump(t, m, right); m.view != viewFiles || !m.zoomed {
		t.Errorf("→ in a fullscreen diff switched tabs")
	}
}

func TestLogGraph(t *testing.T) {
	m, dir := newTestModel(t)
	gitRun(t, dir, "stash", "-q")
	gitRun(t, dir, "switch", "-q", "-c", "feat")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "feat work")
	gitRun(t, dir, "switch", "-q", "main")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "main work")
	gitRun(t, dir, "merge", "-q", "--no-ff", "--no-edit", "feat")
	m = pump(t, m, keyPress("r"))
	m = pump(t, m, keyPress("2"))
	if len(m.log.items) != 4 {
		t.Fatalf("log has %d commits, want 4", len(m.log.items))
	}
	hash := func(i int) string { return m.log.items[i].Hash }
	v := plainView(m)
	// Each row: lanes, hash, refs, subject. The selected commit's details
	// show next to the graph.
	for _, want := range []string{
		"All branches  4",
		"› ◉─╮  " + hash(0) + "  main Merge branch 'feat'",
		"│ ●  " + hash(2) + "  feat feat work",
		"●─╯  " + hash(3) + "  init",
		"Merge: ", "Author: Test",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("log missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Graph") {
		t.Errorf("the graph tab is still shown:\n%s", v)
	}
	if next, _ := m.Update(keyPress("3")); next.(Model).view != viewLog {
		t.Error("3 still switches views")
	}

	m = pump(t, m, keyPress("j"))
	m = pump(t, m, keyPress("j"))
	if v := plainView(m); !strings.Contains(v, "    feat work") {
		t.Errorf("details did not follow the cursor:\n%s", v)
	}
}

func TestLogListIsWider(t *testing.T) {
	m, dir := newTestModel(t)
	subject := "Explain why the cache keeps hourly forecasts"
	gitRun(t, dir, "commit", "-q", "-am", subject)
	m = pump(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	m = pump(t, m, keyPress("r"))
	m = pump(t, m, keyPress("2"))
	// The file list's width would cut the row off after the hash and refs.
	// The details pane shows the subject too, so look for the row.
	if v := plainView(m); !strings.Contains(v, "main "+subject) {
		t.Errorf("subject cut off in a 140-wide log:\n%s", v)
	}
}

// withColors makes Lip Gloss emit ANSI colors, which it otherwise skips
// when the output is not a terminal, so tests can tell styles apart.
func withColors(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestStyleCommitLine(t *testing.T) {
	withColors(t)
	st := theme.Default()
	tests := []struct{ in, want string }{
		{"commit abc1234 (HEAD -> refs/heads/main, refs/remotes/origin/main, tag: refs/tags/v1)",
			"commit abc1234 (HEAD → main, origin/main, tag: v1)"},
		{"commit abc1234", "commit abc1234"},
		{" ui/app.go | 12 +++++---", " ui/app.go | 12 +++++---"},
		{" logo.png  | Bin 0 -> 12 bytes", " logo.png  | Bin 0 -> 12 bytes"},
	}
	for _, tt := range tests {
		if got := ansi.Strip(styleCommitLine(st, tt.in)); got != tt.want {
			t.Errorf("styleCommitLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := styleCommitLine(st, "Author: a | b <x@y>"); !strings.HasPrefix(got, st.Muted.Render("Author:")) {
		t.Errorf("author line with a pipe styled as a diffstat: %q", got)
	}
	// The graph is split into an added and a removed run.
	got := styleStatLine(st, " a | 3 ++-")
	if !strings.Contains(got, st.DiffAdd.Render("++")) || !strings.Contains(got, st.DiffDel.Render("-")) {
		t.Errorf("stat graph not colored: %q", got)
	}
}

func TestAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{20 * 24 * time.Hour, "2w"},
		{100 * 24 * time.Hour, "3mo"},
		{800 * 24 * time.Hour, "2y"},
	}
	for _, tt := range tests {
		if got := age(tt.d); got != tt.want {
			t.Errorf("age(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTabSwitchNeverShowsOtherViewsDiff(t *testing.T) {
	m, dir := newTestModel(t)
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "second")
	m = pump(t, m, keyPress("r"))
	// Before the log has loaded, the file diff must already be gone.
	next, cmd := m.Update(keyPress("2"))
	m = next.(Model)
	if v := plainView(m); strings.Contains(v, "+two") {
		t.Errorf("the files view's diff shows in the log:\n%s", v)
	}
	m = drain(t, m, cmd)
	m.clearDiffCache()
	next, _ = m.Update(keyPress("1"))
	if v := plainView(next.(Model)); strings.Contains(v, "Author:") {
		t.Errorf("the log's commit shows in the files view:\n%s", v)
	}
}

func TestHookOutputSanitizedInStatusLine(t *testing.T) {
	m, dir := newTestModel(t)
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	write(t, filepath.Join(dir, ".git", "hooks"), "pre-commit", "#!/bin/sh\nprintf '\\033[31merror: lint failed\\033[0m\\n' >&2\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, keyPress("a"))
	m = pump(t, m, keyPress("c"))
	m = pump(t, m, keyPress("x"))
	m = pump(t, m, keyPress("enter"))
	if !m.isErr || !strings.Contains(m.message, "lint failed") {
		t.Fatalf("hook error not shown: %q", m.message)
	}
	if strings.Contains(m.View(), "\x1b[31merror") {
		t.Error("raw escape codes from hook output reached the terminal")
	}
}

// newEmptyModel opens a new repository without commits in a directory
// called name.
func newEmptyModel(t *testing.T, name string) Model {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-q", "-b", "main")
	repo, err := git.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(repo).WithoutPolling()
	m = pump(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return drain(t, m, m.Init())
}

func TestHeaderSanitizesRepoName(t *testing.T) {
	m := newEmptyModel(t, "evil\x1b[2Jrepo")
	if v := m.View(); strings.Contains(v, "\x1b[2J") || !strings.Contains(ansi.Strip(v), "evil?[2Jrepo") {
		t.Errorf("repo name not sanitized in the header:\n%q", v)
	}
}

func TestRefreshRunsOneStatusAtATime(t *testing.T) {
	m, _ := newTestModel(t)
	first := m.refresh()
	if first == nil || m.refresh() != nil || !m.reload {
		t.Fatal("a second refresh while loading should wait for the first")
	}
	next, then := m.Update(first())
	m = next.(Model)
	if then == nil || !m.loading || m.reload {
		t.Fatal("the waiting refresh did not start after the first finished")
	}
	if m = drain(t, m, then); m.loading {
		t.Error("still loading after both refreshes finished")
	}
}

func TestPollUpdatesDiffOfModifiedFile(t *testing.T) {
	m, dir := newTestModel(t)
	// a.txt stays modified, so the status does not change; its diff does.
	write(t, dir, "a.txt", "one\ntwo\nthree\n")
	m = pump(t, m, pollTickMsg{at: time.Now().Add(time.Hour)})
	if v := plainView(m); !strings.Contains(v, "+three") {
		t.Errorf("poll did not pick up the edit:\n%s", v)
	}

	// An unchanged status reloads nothing else.
	m = pump(t, m, keyPress("2"))
	seq := m.log.seq
	m = pump(t, m, pollTickMsg{at: time.Now().Add(2 * time.Hour)})
	if m.log.seq != seq {
		t.Error("an unchanged poll reloaded the log")
	}

	// A change made elsewhere is picked up in any view.
	gitRun(t, dir, "commit", "-q", "-am", "elsewhere")
	m = pump(t, m, pollTickMsg{at: time.Now().Add(3 * time.Hour)})
	if v := plainView(m); !strings.Contains(v, "elsewhere") {
		t.Errorf("poll did not show the new commit:\n%s", v)
	}

	// No poll while a question is open: its answer refreshes anyway.
	m = pump(t, m, keyPress("1"))
	write(t, dir, "new.txt", "x\n")
	m = pump(t, m, keyPress("r"))
	m = pump(t, m, keyPress("D"))
	if !m.confirm.active {
		t.Fatal("no question open")
	}
	if cmd := m.poll(time.Now().Add(4 * time.Hour)); cmd != nil || m.loading {
		t.Error("polled while a question was open")
	}
}

func TestMovingCancelsRunningDiff(t *testing.T) {
	m, _ := newTestModel(t)
	ctx := m.diffReq.start() // a diff load in flight
	// At once, not only when the next load starts after the debounce.
	m.Update(keyPress("j"))
	if ctx.Err() == nil {
		t.Error("moving on did not stop the previous diff's git process")
	}
}

// TestHelpShowsEveryKey checks that every keybinding can be read in the
// help screen, scrolling if needed, in windows split side by side or one
// above the other, and that the screen always fits the window.
func TestHelpShowsEveryKey(t *testing.T) {
	m, _ := newTestModel(t)
	for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 24}, {60, 14}, {120, 14}, {44, 16}, {34, 10}} {
		m = pump(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		mm := pump(t, m, keyPress("?"))
		seen := ""
		for range 40 {
			v := plainView(mm)
			lines := strings.Split(v, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d: %d lines, want %d", size[0], size[1], len(lines), size[1])
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%dx%d: line %d is %d wide: %q", size[0], size[1], i, w, l)
				}
			}
			seen += v
			mm = pump(t, mm, keyPress("j"))
		}
		for _, group := range mm.keys.FullHelp() {
			for _, b := range group {
				if h := b.Help(); !strings.Contains(seen, h.Key) || !strings.Contains(seen, h.Desc) {
					t.Errorf("%dx%d: %q %q never shown in help", size[0], size[1], h.Key, h.Desc)
				}
			}
		}
		if !strings.Contains(seen, "esc") || !mm.showHelp {
			t.Errorf("%dx%d: no way out shown, or j closed help", size[0], size[1])
		}
		if mm = pump(t, mm, keyPress("esc")); mm.showHelp {
			t.Errorf("%dx%d: esc did not close help", size[0], size[1])
		}
	}
}

// footer is the last line of the frame.
func footer(m Model) string {
	lines := strings.Split(plainView(m), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestPushAndPull(t *testing.T) {
	m, dir := newTestModel(t)
	m = pump(t, m, keyPress("P"))
	if !m.isErr || !strings.Contains(m.message, "no remote to push to") {
		t.Errorf("push without a remote: %q", m.message)
	}

	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, dir, "init", "-q", "--bare", "-b", "main", remote)
	gitRun(t, dir, "remote", "add", "origin", remote)
	m = pump(t, m, keyPress("P"))
	if footer(m) != "Pushed main" || m.status.Branch.Upstream != "origin/main" {
		t.Fatalf("first push: footer %q, upstream %q", footer(m), m.status.Branch.Upstream)
	}
	m = pump(t, m, keyPress("P"))
	if footer(m) != "Nothing to push" {
		t.Errorf("second push: footer %q", footer(m))
	}

	// A commit pushed from another clone arrives with p, in the log too.
	clone := filepath.Join(t.TempDir(), "clone")
	gitRun(t, dir, "clone", "-q", remote, clone)
	gitRun(t, clone, "-c", "user.name=Other", "-c", "user.email=o@example.com", "commit", "-q", "--allow-empty", "-m", "from elsewhere")
	gitRun(t, clone, "push", "-q")
	m = pump(t, m, keyPress("2"))
	m = pump(t, m, keyPress("p"))
	if v := plainView(m); footer(m) != "Pulled main" || !strings.Contains(v, "from elsewhere") {
		t.Errorf("pull in the log:\n%s", v)
	}
	m = pump(t, m, keyPress("p"))
	if footer(m) != "Already up to date" {
		t.Errorf("second pull: footer %q", footer(m))
	}
}

func TestPendingStaysUntilDone(t *testing.T) {
	m, _ := newTestModel(t)
	// The push is "running": its command is never run, so it can't finish.
	if m.push() == nil || !m.busy {
		t.Fatal("push did not start")
	}
	m = pump(t, m, keyPress("j"))
	if footer(m) != "Pushing…" {
		t.Errorf("progress gone after a keypress: footer %q", footer(m))
	}
	m = pump(t, m, opDoneMsg{info: "Pushed main"})
	if footer(m) != "Pushed main" {
		t.Errorf("after the push: footer %q", footer(m))
	}
	m = pump(t, m, keyPress("j"))
	if !strings.HasSuffix(footer(m), "q quit") {
		t.Errorf("hints did not come back: footer %q", footer(m))
	}
}

func TestInfoMessageFades(t *testing.T) {
	m, _ := newTestModel(t)
	m.infoTimeout = time.Millisecond
	m = pump(t, m, keyPress("c")) // nothing staged
	if strings.Contains(footer(m), "Nothing staged") || !strings.HasSuffix(footer(m), "q quit") {
		t.Errorf("info message did not fade: footer %q", footer(m))
	}

	// An older message's fade leaves a newer one alone.
	m.infoTimeout = 0
	m = pump(t, m, keyPress("c"))
	old := m.messageSeq
	m.setMessage("newer", nil)
	if m = pump(t, m, fadeMsg{seq: old}); footer(m) != "newer" {
		t.Errorf("a stale fade cleared a newer message: footer %q", footer(m))
	}

	// Errors stay until the next key.
	m.infoTimeout = time.Millisecond
	m = pump(t, m, opDoneMsg{err: errors.New("hook failed")})
	if m = pump(t, m, fadeMsg{seq: m.messageSeq}); footer(m) != "hook failed" {
		t.Errorf("an error faded: footer %q", footer(m))
	}
	if m = pump(t, m, keyPress("j")); footer(m) == "hook failed" {
		t.Error("a key did not clear the error")
	}
}
