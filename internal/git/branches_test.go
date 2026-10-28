package git

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Captured from `git for-each-ref --sort=-committerdate --format=<branchFormat>
// refs/heads refs/remotes` in a temp repo with a unicode branch name, a gone
// upstream, an ahead branch and an origin/HEAD symref.
const branchFixture = "refs/heads/fix-ü\x00 \x00\x00\x001793175191\x00fix with ü\x00\n" +
	"refs/heads/gone\x00 \x00origin/gone\x00gone\x001793175191\x00main ahead\x00\n" +
	"refs/heads/main\x00*\x00origin/main\x00ahead 1\x001793175191\x00main ahead\x00\n" +
	"refs/remotes/origin/HEAD\x00 \x00\x00\x001793175191\x00first one\x00refs/remotes/origin/main\n" +
	"refs/remotes/origin/feature/ui\x00 \x00\x00\x001793175191\x00ui work\x00\n" +
	"refs/remotes/origin/main\x00 \x00\x00\x001793175191\x00first one\x00\n"

func TestParseBranches(t *testing.T) {
	at := time.Unix(1793175191, 0)
	want := []BranchRef{
		{Name: "fix-ü", Date: at, Subject: "fix with ü"},
		{Name: "gone", Upstream: "origin/gone", Gone: true, Date: at, Subject: "main ahead"},
		{Name: "main", Current: true, Upstream: "origin/main", Ahead: 1, Date: at, Subject: "main ahead"},
		{Name: "origin/feature/ui", Remote: true, Date: at, Subject: "ui work"},
		{Name: "origin/main", Remote: true, Date: at, Subject: "first one"},
	}
	got, err := ParseBranches([]byte(branchFixture))
	if err != nil {
		t.Fatalf("ParseBranches: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseBranches\n got: %+v\nwant: %+v", got, want)
	}
}

func TestParseBranchesErrors(t *testing.T) {
	for _, in := range []string{"refs/heads/x\x00*\n", "refs/tags/v1\x00 \x00\x00\x001\x00s\x00\n"} {
		if _, err := ParseBranches([]byte(in)); err == nil {
			t.Errorf("ParseBranches(%q): expected an error", in)
		}
	}
}

func TestParseTrack(t *testing.T) {
	tests := []struct {
		in           string
		ahead, behin int
		gone         bool
	}{
		{"", 0, 0, false},
		{"ahead 3", 3, 0, false},
		{"behind 2", 0, 2, false},
		{"ahead 1, behind 12", 1, 12, false},
		{"gone", 0, 0, true},
	}
	for _, tt := range tests {
		a, b, g := parseTrack(tt.in)
		if a != tt.ahead || b != tt.behin || g != tt.gone {
			t.Errorf("parseTrack(%q) = %d, %d, %v", tt.in, a, b, g)
		}
	}
}

func TestParseLog(t *testing.T) {
	// Captured from `git log --format=%h%x00%ct%x00%s`.
	in := "caa0837\x001793175191\x00main ahead\n6a12f77\x001793175191\x00first one: with\x00odd bytes\n"
	got, err := ParseLog([]byte(in))
	if err != nil {
		t.Fatalf("ParseLog: %v", err)
	}
	at := time.Unix(1793175191, 0)
	want := []Commit{
		{Hash: "caa0837", Date: at, Subject: "main ahead"},
		{Hash: "6a12f77", Date: at, Subject: "first one: with\x00odd bytes"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseLog\n got: %+v\nwant: %+v", got, want)
	}
	if _, err := ParseLog([]byte("abc\n")); err == nil {
		t.Error("expected an error for a malformed record")
	}
}

func branchNamed(t *testing.T, bs []BranchRef, name string) BranchRef {
	t.Helper()
	for _, b := range bs {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("branch %q not in %+v", name, bs)
	return BranchRef{}
}

func TestBranchesAndCheckout(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	remote := t.TempDir()
	gitCmd(t, remote, "init", "-q", "--bare")
	gitCmd(t, r.Root(), "remote", "add", "origin", remote)
	gitCmd(t, r.Root(), "push", "-q", "-u", "origin", "main")
	// origin/feature exists only on the remote.
	gitCmd(t, r.Root(), "switch", "-q", "-c", "feature")
	gitCmd(t, r.Root(), "commit", "-q", "--allow-empty", "-m", "feature work")
	gitCmd(t, r.Root(), "push", "-q", "origin", "feature")
	gitCmd(t, r.Root(), "switch", "-q", "main")
	gitCmd(t, r.Root(), "branch", "-q", "-D", "feature")
	// main diverges from origin/main: ahead 1, behind 1.
	gitCmd(t, r.Root(), "commit", "-q", "--allow-empty", "-m", "remote side")
	gitCmd(t, r.Root(), "push", "-q", "origin", "main")
	gitCmd(t, r.Root(), "reset", "-q", "--hard", "HEAD~1")
	gitCmd(t, r.Root(), "commit", "-q", "--allow-empty", "-m", "local side")

	bs, err := r.Branches(ctx)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	main := branchNamed(t, bs, "main")
	if !main.Current || main.Ahead != 1 || main.Behind != 1 || main.Subject != "local side" {
		t.Errorf("main = %+v; want current, ahead 1, behind 1", main)
	}
	feature := branchNamed(t, bs, "origin/feature")
	if !feature.Remote || bs[0].Remote {
		t.Errorf("want local branches first, got %+v", bs)
	}

	log, err := r.Log(ctx, feature.Ref(), 10)
	if err != nil || len(log) != 2 || log[0].Subject != "feature work" {
		t.Fatalf("Log = %+v, %v", log, err)
	}

	if err := r.Checkout(ctx, feature); err != nil {
		t.Fatalf("Checkout remote: %v", err)
	}
	bs, _ = r.Branches(ctx)
	if local := branchNamed(t, bs, "feature"); !local.Current || local.Upstream != "origin/feature" {
		t.Errorf("feature = %+v; want current, tracking origin/feature", local)
	}

	// Local changes that the switch would overwrite: git refuses.
	gitCmd(t, r.Root(), "switch", "-q", "main")
	gitCmd(t, r.Root(), "switch", "-q", "-c", "other")
	writeFile(t, r.Root(), "tracked.txt", "on other\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "other")
	writeFile(t, r.Root(), "tracked.txt", "uncommitted\n")
	err = r.Checkout(ctx, BranchRef{Name: "main"})
	if err == nil || !strings.Contains(err.Error(), "would be overwritten") {
		t.Fatalf("Checkout over local changes = %v; want git's refusal", err)
	}
}

func TestBranchesOnUnbornRepo(t *testing.T) {
	r := newTestRepo(t)
	gitCmd(t, r.Root(), "checkout", "-q", "--orphan", "fresh")
	bs, err := r.Branches(context.Background())
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if b := branchNamed(t, bs, "main"); b.Current {
		t.Error("main must not be current on an orphan branch")
	}
}

func TestLogIgnoresShowSignature(t *testing.T) {
	r := newTestRepo(t)
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	key := filepath.Join(t.TempDir(), "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	gitCmd(t, r.Root(), "config", "gpg.format", "ssh")
	gitCmd(t, r.Root(), "config", "user.signingkey", key)
	gitCmd(t, r.Root(), "commit", "-q", "-S", "--allow-empty", "-m", "signed")
	// Without an allowed signers file git still prints a verification line.
	gitCmd(t, r.Root(), "config", "log.showSignature", "true")

	log, err := r.Log(context.Background(), "HEAD", 10)
	if err != nil || len(log) != 2 || log[0].Subject != "signed" {
		t.Fatalf("Log with log.showSignature = %+v, %v", log, err)
	}
}

func TestShow(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r.Root(), "tracked.txt", "one\ntwo\n")
	gitCmd(t, r.Root(), "commit", "-q", "-am", "second line")

	log, err := r.Log(context.Background(), "HEAD", 10)
	if err != nil || len(log) != 2 {
		t.Fatalf("Log = %+v, %v", log, err)
	}
	d, err := r.Show(context.Background(), log[0].Hash, 80)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	for _, want := range []string{
		"(HEAD -> refs/heads/main)", "    second line", " tracked.txt | 1 +", "diff --git", "+two",
	} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("Show output missing %q:\n%s", want, d.Text)
		}
	}
}
