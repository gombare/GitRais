//go:build !unix

package gitexec

import (
	"os"
	"os/exec"
)

// setProcessGroup is the fallback for platforms without process groups: only git
// itself is terminated on cancel.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
}
