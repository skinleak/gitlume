//go:build unix

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNetworkRunsWithoutTerminal checks that ssh, started by a push, is in
// a session of its own, so it has no terminal to prompt on underneath the
// UI. It compares session ids rather than opening /dev/tty, so it means the
// same with and without a terminal attached to the test.
func TestNetworkRunsWithoutTerminal(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("ps not installed")
	}
	r := newTestRepo(t)
	dir := t.TempDir()
	sid := filepath.Join(dir, "sid")
	ssh := filepath.Join(dir, "ssh")
	writeFile(t, dir, "ssh", "#!/bin/sh\nps -o sid= -p $$ > '"+sid+"'\nexit 1\n")
	if err := os.Chmod(ssh, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", ssh)
	gitCmd(t, r.Root(), "remote", "add", "origin", "ssh://example.invalid/repo.git")

	if _, err := r.Push(context.Background()); err == nil {
		t.Fatal("Push to an unreachable remote succeeded")
	}
	out, err := os.ReadFile(sid)
	if err != nil {
		t.Fatalf("ssh did not run: %v", err)
	}
	own, err := exec.Command("ps", "-o", "sid=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got == "" || got == strings.TrimSpace(string(own)) {
		t.Errorf("ssh ran in session %q, the test's own; it could prompt on its terminal", got)
	}
}
