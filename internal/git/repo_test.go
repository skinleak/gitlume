package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestRepo creates an isolated repository with one commit and returns
// it opened. Tests are skipped when git is unavailable.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	gitCmd(t, dir, "config", "user.name", "Test")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	writeFile(t, dir, "tracked.txt", "one\n")
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-q", "-m", "init")

	repo, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return repo
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustStatus(t *testing.T, r *Repo) Status {
	t.Helper()
	st, err := r.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return st
}

func TestOpenNotRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	_, err := Open(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNotRepo) {
		t.Fatalf("Open outside a repo: got %v, want ErrNotRepo", err)
	}
}

func TestOpenFromSubdirectory(t *testing.T) {
	r := newTestRepo(t)
	sub := filepath.Join(r.Root(), "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Open(context.Background(), sub)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got.Root() != r.Root() {
		t.Errorf("Root = %q, want %q", got.Root(), r.Root())
	}
}

func TestStageUnstageCommit(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	writeFile(t, r.Root(), "tracked.txt", "two\n")
	writeFile(t, r.Root(), "new file.txt", "hello\n")

	if err := r.Stage(ctx, "tracked.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	st := mustStatus(t, r)
	if n := len(st.Changes(Staged)); n != 1 {
		t.Fatalf("staged = %d, want 1", n)
	}

	if err := r.Unstage(ctx, "tracked.txt"); err != nil {
		t.Fatalf("Unstage: %v", err)
	}
	st = mustStatus(t, r)
	if n := len(st.Changes(Staged)); n != 0 {
		t.Fatalf("staged after unstage = %d, want 0", n)
	}

	if err := r.StageAll(ctx); err != nil {
		t.Fatalf("StageAll: %v", err)
	}
	st = mustStatus(t, r)
	if n := len(st.Changes(Staged)); n != 2 {
		t.Fatalf("staged after stage all = %d, want 2", n)
	}

	if err := r.Commit(ctx, "  second commit  \n"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	st = mustStatus(t, r)
	if len(st.Entries) != 0 {
		t.Fatalf("entries after commit = %+v, want none", st.Entries)
	}
}

func TestUnstageOnUnbornBranch(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "checkout", "-q", "--orphan", "fresh")
	gitCmd(t, r.Root(), "rm", "-q", "--cached", "tracked.txt")
	if err := r.Stage(ctx, "tracked.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if err := r.Unstage(ctx, "tracked.txt"); err != nil {
		t.Fatalf("Unstage on unborn branch: %v", err)
	}
	if st := mustStatus(t, r); !st.Branch.Unborn() || len(st.Changes(Staged)) != 0 {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestCommitEmptyMessage(t *testing.T) {
	r := newTestRepo(t)
	if err := r.Commit(context.Background(), " \n "); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("Commit: got %v, want ErrEmptyMessage", err)
	}
}

func TestCommitNothingStagedReturnsGitError(t *testing.T) {
	r := newTestRepo(t)
	err := r.Commit(context.Background(), "nothing")
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("Commit: got %v, want *Error", err)
	}
}

func TestDiff(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	writeFile(t, r.Root(), "tracked.txt", "two\n")
	writeFile(t, r.Root(), "fresh.txt", "brand new\n")

	unstaged, err := r.Diff(ctx, Change{Group: Unstaged, Path: "tracked.txt"})
	if err != nil {
		t.Fatalf("unstaged Diff: %v", err)
	}
	if !strings.Contains(unstaged.Text, "+two") {
		t.Errorf("unstaged diff missing change:\n%s", unstaged.Text)
	}

	staged, err := r.Diff(ctx, Change{Group: Staged, Path: "tracked.txt"})
	if err != nil {
		t.Fatalf("staged Diff: %v", err)
	}
	if staged.Text != "" {
		t.Errorf("staged diff should be empty before staging:\n%s", staged.Text)
	}

	untracked, err := r.Diff(ctx, Change{Group: Untracked, Path: "fresh.txt"})
	if err != nil {
		t.Fatalf("untracked Diff: %v", err)
	}
	if !strings.Contains(untracked.Text, "+brand new") {
		t.Errorf("untracked diff missing content:\n%s", untracked.Text)
	}
}

func TestDiffIgnoresPrefixConfig(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	writeFile(t, r.Root(), "tracked.txt", "two\n")
	writeFile(t, r.Root(), "fresh.txt", "new\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "two")
	writeFile(t, r.Root(), "tracked.txt", "three\n")
	for _, cfg := range [][2]string{{"diff.noprefix", "true"}, {"diff.mnemonicPrefix", "true"}} {
		gitCmd(t, r.Root(), "config", cfg[0], cfg[1])
		unstaged, err := r.Diff(ctx, Change{Group: Unstaged, Path: "tracked.txt"})
		if err != nil || !strings.Contains(unstaged.Text, "+++ b/tracked.txt") {
			t.Errorf("%s: unstaged diff = %v\n%s", cfg[0], err, unstaged.Text)
		}
		untracked, err := r.Diff(ctx, Change{Group: Untracked, Path: "fresh.txt"})
		if err != nil || !strings.Contains(untracked.Text, "+++ b/fresh.txt") {
			t.Errorf("%s: untracked diff = %v\n%s", cfg[0], err, untracked.Text)
		}
		show, err := r.Show(ctx, "HEAD", 80)
		if err != nil || !strings.Contains(show.Text, "+++ b/tracked.txt") {
			t.Errorf("%s: show = %v\n%s", cfg[0], err, show.Text)
		}
		gitCmd(t, r.Root(), "config", "--unset", cfg[0])
	}
}

func TestDiffStagedRename(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "mv", "tracked.txt", "moved.txt")

	changes := mustStatus(t, r).Changes(Staged)
	if len(changes) != 1 || changes[0].Code != 'R' {
		t.Fatalf("staged changes = %+v, want one rename", changes)
	}
	d, err := r.Diff(ctx, changes[0])
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(d.Text, "rename from tracked.txt") {
		t.Errorf("rename diff missing header:\n%s", d.Text)
	}
}

func TestDiffTruncatesLargeOutput(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r.Root(), "big.txt", strings.Repeat("0123456789abcdef\n", MaxDiffBytes/8))

	d, err := r.Diff(context.Background(), Change{Group: Untracked, Path: "big.txt"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !d.Truncated || len(d.Text) > MaxDiffBytes || !strings.HasSuffix(d.Text, "\n") {
		t.Fatalf("Truncated = %v, len = %d; want true, <= %d, ending in a full line",
			d.Truncated, len(d.Text), MaxDiffBytes)
	}
}

func TestStageTreatsPathsLiterally(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r.Root(), "*.txt", "glob-looking name\n")
	writeFile(t, r.Root(), "other.txt", "must stay untracked\n")

	if err := r.Stage(context.Background(), "*.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	staged := mustStatus(t, r).Changes(Staged)
	if len(staged) != 1 || staged[0].Path != "*.txt" {
		t.Fatalf("staged = %+v, want only *.txt", staged)
	}
}

func TestCommitReturnsWhenHookLeavesChildRunning(t *testing.T) {
	r := newTestRepo(t)
	// The background sleep inherits git's stdout and stderr pipes.
	writeFile(t, r.Root(), ".git/hooks/pre-commit", "#!/bin/sh\nsleep 30 &\nexit 0\n")
	if err := os.Chmod(filepath.Join(r.Root(), ".git/hooks/pre-commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r.Root(), "tracked.txt", "two\n")
	if err := r.StageAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := r.Commit(context.Background(), "with hook"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("Commit took %v; a lingering hook child blocked it", d)
	}
}

func TestErrorMessageFallsBackToStdout(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r.Root(), "untracked.txt", "x\n")
	err := r.Commit(context.Background(), "nothing staged")
	if err == nil || !strings.Contains(err.Error(), "nothing added to commit") {
		t.Fatalf("Commit error = %v, want git's stdout explanation", err)
	}
}

func TestDiffNestedRepository(t *testing.T) {
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "init", "-q", "nested")
	writeFile(t, r.Root(), "nested/f", "x\n")
	gitCmd(t, filepath.Join(r.Root(), "nested"), "add", "f")

	var nested *Change
	for _, c := range mustStatus(t, r).Changes(Untracked) {
		if c.Path == "nested/" {
			nested = &c
		}
	}
	if nested == nil {
		t.Fatal("nested repository not listed as untracked directory")
	}
	d, err := r.Diff(context.Background(), *nested)
	if err != nil || d.Note == "" {
		t.Fatalf("Diff = %+v, %v; want a note", d, err)
	}
}

func TestErrorSummary(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"\n  hint: something\n", "hint: something"},
		{"hint: a\nerror: real problem\nfatal: later\n", "error: real problem"},
		{"warning: x\nfatal: bad\n", "fatal: bad"},
	}
	for _, tt := range tests {
		if got := summary(tt.in); got != tt.want {
			t.Errorf("summary(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(dir, name string) bool {
	_, err := os.Lstat(filepath.Join(dir, name))
	return err == nil
}

func TestDiscardStagedAndUnstaged(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	writeFile(t, r.Root(), "tracked.txt", "staged\n")
	gitCmd(t, r.Root(), "add", "tracked.txt")
	writeFile(t, r.Root(), "tracked.txt", "staged\nand unstaged\n")

	if err := r.Discard(ctx, Change{Group: Unstaged, Path: "tracked.txt"}); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if st := mustStatus(t, r); len(st.Entries) != 0 {
		t.Fatalf("entries after discard = %+v, want none", st.Entries)
	}
	if got := readFile(t, r.Root(), "tracked.txt"); got != "one\n" {
		t.Errorf("content = %q, want HEAD's %q", got, "one\n")
	}
}

func TestDiscardAddedFileRemovesIt(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r.Root(), "added.txt", "new\n")
	gitCmd(t, r.Root(), "add", "added.txt")

	if err := r.Discard(context.Background(), Change{Group: Staged, Path: "added.txt"}); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if exists(r.Root(), "added.txt") || len(mustStatus(t, r).Entries) != 0 {
		t.Fatal("added file survived discard")
	}
}

func TestDiscardStagedRenameRestoresSource(t *testing.T) {
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "mv", "tracked.txt", "moved.txt")
	writeFile(t, r.Root(), "moved.txt", "one\nedited\n")

	// The unstaged row of a rename has no OrigPath; the caller passes it.
	if err := r.Discard(context.Background(), Change{Group: Unstaged, Path: "moved.txt"}, "tracked.txt"); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if exists(r.Root(), "moved.txt") || !exists(r.Root(), "tracked.txt") {
		t.Fatal("rename not undone")
	}
	if st := mustStatus(t, r); len(st.Entries) != 0 {
		t.Fatalf("entries after discard = %+v, want none", st.Entries)
	}
}

func TestDiscardConflict(t *testing.T) {
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "checkout", "-q", "-b", "other")
	writeFile(t, r.Root(), "tracked.txt", "other\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "other")
	gitCmd(t, r.Root(), "checkout", "-q", "main")
	writeFile(t, r.Root(), "tracked.txt", "main\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "main")
	cmd := exec.Command("git", "merge", "-q", "other")
	cmd.Dir = r.Root()
	_ = cmd.Run() // conflicts, as intended

	if err := r.Discard(context.Background(), Change{Group: Unstaged, Code: 'U', Path: "tracked.txt"}); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if got := readFile(t, r.Root(), "tracked.txt"); got != "main\n" {
		t.Errorf("content = %q, want HEAD's %q", got, "main\n")
	}
}

func TestDiscardUntrackedDeletesFile(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r.Root(), "dir/*.txt", "x\n")
	writeFile(t, r.Root(), "dir/keep.txt", "x\n")

	if err := r.Discard(context.Background(), Change{Group: Untracked, Path: "dir/*.txt"}); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if exists(r.Root(), "dir/*.txt") || !exists(r.Root(), "dir/keep.txt") {
		t.Fatal("discard did not delete exactly the selected file")
	}
}

func TestDiscardRefusesNestedRepo(t *testing.T) {
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "init", "-q", "nested")
	err := r.Discard(context.Background(), Change{Group: Untracked, Path: "nested/"})
	if !errors.Is(err, ErrNestedRepo) || !exists(r.Root(), "nested/.git") {
		t.Fatalf("Discard = %v; want ErrNestedRepo and the repo kept", err)
	}
}

func TestDiscardOnUnbornBranch(t *testing.T) {
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "checkout", "-q", "--orphan", "fresh")
	writeFile(t, r.Root(), "tracked.txt", "edited\n")

	if err := r.Discard(context.Background(), Change{Group: Unstaged, Path: "tracked.txt"}); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if exists(r.Root(), "tracked.txt") || len(mustStatus(t, r).Entries) != 0 {
		t.Fatal("file survived discard on unborn branch")
	}
}

func TestDiscardAll(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r.Root(), ".gitignore", "ignored.txt\n")
	gitCmd(t, r.Root(), "add", ".gitignore")
	gitCmd(t, r.Root(), "commit", "-q", "-m", "ignore")
	writeFile(t, r.Root(), "tracked.txt", "edited\n")
	writeFile(t, r.Root(), "staged.txt", "s\n")
	gitCmd(t, r.Root(), "add", "staged.txt")
	writeFile(t, r.Root(), "dir/untracked.txt", "u\n")
	writeFile(t, r.Root(), "ignored.txt", "keep me\n")

	if err := r.DiscardAll(context.Background()); err != nil {
		t.Fatalf("DiscardAll: %v", err)
	}
	if st := mustStatus(t, r); len(st.Entries) != 0 {
		t.Fatalf("entries after discard all = %+v, want none", st.Entries)
	}
	if exists(r.Root(), "dir") || !exists(r.Root(), "ignored.txt") {
		t.Error("untracked dir must be deleted and ignored files kept")
	}
	if got := readFile(t, r.Root(), "tracked.txt"); got != "one\n" {
		t.Errorf("content = %q, want HEAD's %q", got, "one\n")
	}
}

func TestDiscardAllOnUnbornBranch(t *testing.T) {
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "checkout", "-q", "--orphan", "fresh")
	if err := r.DiscardAll(context.Background()); err != nil {
		t.Fatalf("DiscardAll: %v", err)
	}
	if st := mustStatus(t, r); len(st.Entries) != 0 || exists(r.Root(), "tracked.txt") {
		t.Fatalf("entries after discard all = %+v, want none", st.Entries)
	}
}
