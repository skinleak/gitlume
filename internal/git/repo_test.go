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
