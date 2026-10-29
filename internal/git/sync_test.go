package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Captured from `git push --porcelain` against a bare repository: a first
// push with --set-upstream, an up-to-date push, a fast-forward, and pushes
// refused by the remote's history and by its pre-receive hook.
const (
	pushNewFixture    = "To ../remote.git\n*\tHEAD:refs/heads/main\t[new branch]\nbranch 'main' set up to track 'origin/main'.\nDone\n"
	pushSameFixture   = "To ../remote.git\n=\trefs/heads/main:refs/heads/main\t[up to date]\nDone\n"
	pushFFFixture     = "To ../remote.git\n \trefs/heads/main:refs/heads/main\t9554853..eb05ef0\nDone\n"
	pushBehindFixture = "To ../remote.git\n!\trefs/heads/main:refs/heads/main\t[rejected] (fetch first)\nDone\n"
	pushHookFixture   = "To ../remote.git\n!\trefs/heads/main:refs/heads/main\t[remote rejected] (pre-receive hook declined)\nDone\n"
	pushHookStderr    = "remote: remote says no        \nerror: failed to push some refs to '../remote.git'\n"
	pushBehindStderr  = "error: failed to push some refs to '../remote.git'\nhint: Updates were rejected because the remote contains work that you do not\n"
)

func TestParsePush(t *testing.T) {
	tests := []struct {
		in   string
		want []PushedRef
	}{
		{pushNewFixture, []PushedRef{{'*', "HEAD:refs/heads/main", "[new branch]"}}},
		{pushSameFixture, []PushedRef{{'=', "refs/heads/main:refs/heads/main", "[up to date]"}}},
		{pushFFFixture, []PushedRef{{' ', "refs/heads/main:refs/heads/main", "9554853..eb05ef0"}}},
		{pushBehindFixture, []PushedRef{{'!', "refs/heads/main:refs/heads/main", "[rejected] (fetch first)"}}},
		{"", nil},
	}
	for _, tt := range tests {
		if got := ParsePush([]byte(tt.in)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParsePush(%q)\n got: %+v\nwant: %+v", tt.in, got, tt.want)
		}
	}
}

func TestRejection(t *testing.T) {
	if err := rejection(ParsePush([]byte(pushBehindFixture)), pushBehindStderr); !errors.Is(err, ErrRejected) {
		t.Errorf("fetch first: %v, want ErrRejected", err)
	}
	nonFF := strings.Replace(pushBehindFixture, "(fetch first)", "(non-fast-forward)", 1)
	if err := rejection(ParsePush([]byte(nonFF)), ""); !errors.Is(err, ErrRejected) {
		t.Errorf("non-fast-forward: %v, want ErrRejected", err)
	}
	err := rejection(ParsePush([]byte(pushHookFixture)), pushHookStderr)
	if want := "push rejected: pre-receive hook declined: remote says no"; err == nil || err.Error() != want {
		t.Errorf("hook: %v, want %q", err, want)
	}
	if err := rejection(ParsePush([]byte(pushFFFixture)), ""); err != nil {
		t.Errorf("accepted push: %v", err)
	}
}

// withRemote gives r a bare remote called origin, with main as its default
// branch, and returns the remote's path.
func withRemote(t *testing.T, r *Repo) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitCmd(t, r.Root(), "init", "-q", "--bare", "-b", "main", remote)
	gitCmd(t, r.Root(), "remote", "add", "origin", remote)
	return remote
}

// commitElsewhere pushes a new commit to remote's main from another clone.
func commitElsewhere(t *testing.T, remote, subject string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	gitCmd(t, filepath.Dir(dir), "clone", "-q", remote, dir)
	gitCmd(t, dir, "-c", "user.name=Other", "-c", "user.email=o@example.com", "commit", "-q", "--allow-empty", "-m", subject)
	gitCmd(t, dir, "push", "-q", "origin", "main")
}

func headSubject(t *testing.T, r *Repo) string {
	t.Helper()
	log, err := r.Log(context.Background(), "HEAD", 1)
	if err != nil || len(log) == 0 {
		t.Fatalf("Log: %v", err)
	}
	return log[0].Subject
}

func TestPushAndPull(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	remote := withRemote(t, r)

	// The first push sets the upstream.
	res, err := r.Push(ctx)
	if err != nil || res != (SyncResult{Branch: "main"}) {
		t.Fatalf("first Push = %+v, %v", res, err)
	}
	if up := mustStatus(t, r).Branch.Upstream; up != "origin/main" {
		t.Errorf("upstream after the first push = %q, want origin/main", up)
	}
	if res, err := r.Push(ctx); err != nil || !res.UpToDate {
		t.Errorf("second Push = %+v, %v; want up to date", res, err)
	}

	commitElsewhere(t, remote, "from elsewhere")
	if res, err := r.Pull(ctx); err != nil || res != (SyncResult{Branch: "main"}) {
		t.Fatalf("Pull = %+v, %v", res, err)
	}
	if got := headSubject(t, r); got != "from elsewhere" {
		t.Errorf("HEAD after pull = %q", got)
	}
	if res, err := r.Pull(ctx); err != nil || !res.UpToDate {
		t.Errorf("second Pull = %+v, %v; want up to date", res, err)
	}

	gitCmd(t, r.Root(), "commit", "-q", "--allow-empty", "-m", "local")
	if res, err := r.Push(ctx); err != nil || res.UpToDate {
		t.Errorf("Push of a new commit = %+v, %v", res, err)
	}
}

func TestDivergedBranch(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	remote := withRemote(t, r)
	if _, err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	commitElsewhere(t, remote, "theirs")
	gitCmd(t, r.Root(), "commit", "-q", "--allow-empty", "-m", "ours")

	if _, err := r.Push(ctx); !errors.Is(err, ErrRejected) {
		t.Errorf("Push behind the remote = %v, want ErrRejected", err)
	}
	// Neither a merge nor a rebase, even when config asks for one.
	for _, cfg := range []string{"pull.rebase=true", "pull.ff=true", "pull.rebase=false"} {
		k, v, _ := strings.Cut(cfg, "=")
		gitCmd(t, r.Root(), "config", k, v)
		if _, err := r.Pull(ctx); !errors.Is(err, ErrDiverged) {
			t.Errorf("%s: Pull = %v, want ErrDiverged", cfg, err)
		}
		if got := headSubject(t, r); got != "ours" {
			t.Errorf("%s: HEAD after a refused pull = %q, want ours", cfg, got)
		}
		gitCmd(t, r.Root(), "config", "--unset", k)
	}
}

func TestPushRejectedByHook(t *testing.T) {
	r := newTestRepo(t)
	remote := withRemote(t, r)
	writeFile(t, remote, "hooks/pre-receive", "#!/bin/sh\necho 'main is protected' >&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(remote, "hooks/pre-receive"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := r.Push(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pre-receive hook declined: main is protected") {
		t.Errorf("Push = %v, want the hook's refusal", err)
	}
}

func TestSyncRefusals(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	if _, err := r.Push(ctx); !errors.Is(err, ErrNoRemote) {
		t.Errorf("Push without a remote = %v, want ErrNoRemote", err)
	}
	if _, err := r.Pull(ctx); !errors.Is(err, ErrNoUpstream) {
		t.Errorf("Pull without an upstream = %v, want ErrNoUpstream", err)
	}

	// Several remotes, none of them origin: git wouldn't guess either.
	gitCmd(t, r.Root(), "remote", "add", "a", filepath.Join(t.TempDir(), "a.git"))
	gitCmd(t, r.Root(), "remote", "add", "b", filepath.Join(t.TempDir(), "b.git"))
	if _, err := r.Push(ctx); !errors.Is(err, ErrNoRemote) || !strings.Contains(err.Error(), "remote.pushDefault") {
		t.Errorf("Push with several remotes = %v, want ErrNoRemote naming remote.pushDefault", err)
	}

	gitCmd(t, r.Root(), "checkout", "-q", "--detach")
	if _, err := r.Push(ctx); !errors.Is(err, ErrDetached) {
		t.Errorf("Push on a detached HEAD = %v, want ErrDetached", err)
	}
	if _, err := r.Pull(ctx); !errors.Is(err, ErrDetached) {
		t.Errorf("Pull on a detached HEAD = %v, want ErrDetached", err)
	}

	gitCmd(t, r.Root(), "checkout", "-q", "--orphan", "fresh")
	if _, err := r.Push(ctx); !errors.Is(err, ErrNoCommits) {
		t.Errorf("Push on an unborn branch = %v, want ErrNoCommits", err)
	}
}

func TestPushUsesConfiguredRemote(t *testing.T) {
	r := newTestRepo(t)
	withRemote(t, r)
	other := filepath.Join(t.TempDir(), "other.git")
	gitCmd(t, r.Root(), "init", "-q", "--bare", "-b", "main", other)
	gitCmd(t, r.Root(), "remote", "add", "other", other)
	gitCmd(t, r.Root(), "config", "remote.pushDefault", "other")

	if _, err := r.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if up := mustStatus(t, r).Branch.Upstream; up != "other/main" {
		t.Errorf("upstream = %q, want other/main", up)
	}
}
