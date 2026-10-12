//go:build !unix

package gitexec

import "os/exec"

// setProcessGroup is the fallback for platforms without process groups: only git
// itself is killed on cancel (os.Interrupt is not supported on Windows).
func setProcessGroup(cmd *exec.Cmd) (release func()) {
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	return func() {}
}
