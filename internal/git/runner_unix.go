//go:build unix

package git

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a new session, without a controlling terminal. ssh
// and credential helpers otherwise open /dev/tty to ask for a passphrase or
// to confirm a host key, underneath the UI and racing it for keystrokes;
// without a terminal they fail, and the error says why. The session is its
// own process group, so cancelling kills ssh along with git.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
