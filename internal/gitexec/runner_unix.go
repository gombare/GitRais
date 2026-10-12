//go:build unix

package gitexec

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup starts git in its own process group and, on cancel, signals the
// whole group so that grandchildren (askpass helpers, ssh, credential helpers,
// remote helpers) die with git instead of keeping its pipes open. If the group is
// still alive when WaitDelay elapses it is killed with SIGKILL (exec itself would
// only kill the leader).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		pgid := cmd.Process.Pid
		err := syscall.Kill(-pgid, syscall.SIGTERM)
		// Escalation: a no-op (ESRCH) when the group is already gone.
		time.AfterFunc(cmd.WaitDelay, func() { syscall.Kill(-pgid, syscall.SIGKILL) })
		return err
	}
}
