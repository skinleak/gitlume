package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/gitlume/internal/git"
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
	m := New(repo)
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
	for _, want := range []string{"main", "Unstaged", "a.txt", "b.txt", "+two"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestViewFitsWindow(t *testing.T) {
	m, _ := newTestModel(t)
	for _, size := range [][2]int{{100, 30}, {60, 20}, {200, 50}, {30, 8}, {20, 4}, {10, 2}} {
		m = pump(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, mode := range []string{"main", "help", "commit", "zoom", "zoom help", "confirm"} {
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
	l := fileList{height: 10}
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
	if m.confirm.active || len(m.files.changes) != 3 {
		t.Fatalf("esc: active = %v, changes = %d; want closed and untouched", m.confirm.active, len(m.files.changes))
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
