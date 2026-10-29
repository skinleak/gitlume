package git

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func lastMessage(t *testing.T, r *Repo) string {
	t.Helper()
	res, err := r.read(context.Background(), invocation{args: []string{"log", "-1", "--format=%B"}})
	if err != nil {
		t.Fatal(err)
	}
	return string(res.stdout)
}

func commitCount(t *testing.T, r *Repo) int {
	t.Helper()
	log, err := r.Log(context.Background(), "HEAD", 100)
	if err != nil {
		t.Fatal(err)
	}
	return len(log)
}

func TestAmend(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	// A wrapped subject, a body with a line starting with #, a trailer.
	writeFile(t, r.Root(), "tracked.txt", "two\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "Subject line\ncontinued", "-m", "Body one.\n# kept", "-m", "Signed-off-by: T <t@x>")
	if s, err := r.AmendSubject(ctx); err != nil || s != "Subject line continued" {
		t.Fatalf("AmendSubject = %q, %v", s, err)
	}

	// The staged change goes into the commit; the body and trailer stay.
	writeFile(t, r.Root(), "tracked.txt", "three\n")
	if err := r.Stage(ctx, "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	before := commitCount(t, r)
	if err := r.Amend(ctx, "  New subject  "); err != nil {
		t.Fatalf("Amend: %v", err)
	}
	if want := "New subject\n\nBody one.\n# kept\n\nSigned-off-by: T <t@x>\n\n"; lastMessage(t, r) != want {
		t.Errorf("message = %q, want %q", lastMessage(t, r), want)
	}
	if st := mustStatus(t, r); len(st.Entries) != 0 || commitCount(t, r) != before {
		t.Errorf("after amend: entries %+v, %d commits (want %d)", st.Entries, commitCount(t, r), before)
	}

	// Without a body nothing is added.
	writeFile(t, r.Root(), "tracked.txt", "four\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "plain")
	if err := r.Amend(ctx, "still plain"); err != nil || lastMessage(t, r) != "still plain\n\n" {
		t.Errorf("Amend of a plain commit: %q, %v", lastMessage(t, r), err)
	}
	if err := r.Amend(ctx, " "); !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("Amend with a blank subject = %v, want ErrEmptyMessage", err)
	}

	// Staging what undoes the commit would leave it empty: git refuses.
	writeFile(t, r.Root(), "tracked.txt", "three\n")
	if err := r.Stage(ctx, "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if err := r.Amend(ctx, "undone"); !errors.Is(err, ErrAmendEmpty) || headSubject(t, r) != "still plain" {
		t.Errorf("Amend into an empty commit = %v, HEAD %q", err, headSubject(t, r))
	}
}

func TestAmendRefusesPushedCommit(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitCmd(t, r.Root(), "init", "-q", "--bare", "-b", "main", remote)
	gitCmd(t, r.Root(), "remote", "add", "origin", remote)
	gitCmd(t, r.Root(), "push", "-q", "-u", "origin", "main")

	if _, err := r.AmendSubject(ctx); !errors.Is(err, ErrAmendPushed) {
		t.Errorf("AmendSubject of a pushed commit = %v, want ErrAmendPushed", err)
	}
	if err := r.Amend(ctx, "rewrite"); !errors.Is(err, ErrAmendPushed) || headSubject(t, r) != "init" {
		t.Errorf("Amend of a pushed commit = %v, HEAD %q", err, headSubject(t, r))
	}

	// A commit not pushed yet can be amended.
	writeFile(t, r.Root(), "tracked.txt", "local\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "local")
	if err := r.Amend(ctx, "local, amended"); err != nil || headSubject(t, r) != "local, amended" {
		t.Errorf("Amend of a local commit = %v, HEAD %q", err, headSubject(t, r))
	}

	// Detached, there is no upstream to compare with.
	gitCmd(t, r.Root(), "checkout", "-q", "--detach")
	if err := r.Amend(ctx, "detached"); err != nil {
		t.Errorf("Amend on a detached HEAD: %v", err)
	}

	gitCmd(t, r.Root(), "checkout", "-q", "--orphan", "fresh")
	if _, err := r.AmendSubject(ctx); !errors.Is(err, ErrNoCommits) {
		t.Errorf("AmendSubject on an unborn branch = %v, want ErrNoCommits", err)
	}
}
