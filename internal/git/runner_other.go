//go:build !unix

package git

import "os/exec"

// detach does nothing where there is no controlling terminal to leave.
func detach(*exec.Cmd) {}
