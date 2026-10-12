//go:build unix

package gitexec

import (
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// setProcessGroup starts git in its own process group and, on cancel, signals the
// whole group so that grandchildren (askpass helpers, ssh, credential helpers,
// remote helpers) die with git instead of keeping its pipes open. If the group is
// still alive when WaitDelay elapses it is killed with SIGKILL (exec itself would
// only kill the leader). The returned release func must be called once Wait has
// returned: it disarms the escalation so a reused pgid is never signalled.
//
// Side effect when run from a terminal (wails dev, go test): Ctrl-C no longer
// reaches in-flight git children. A GUI has no controlling tty, so production is
// unaffected.
func setProcessGroup(cmd *exec.Cmd) (release func()) {
	var (
		mu       sync.Mutex
		timer    *time.Timer
		released bool
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		pgid := cmd.Process.Pid
		err := syscall.Kill(-pgid, syscall.SIGTERM)
		mu.Lock()
		if !released {
			timer = time.AfterFunc(cmd.WaitDelay, func() {
				mu.Lock()
				defer mu.Unlock()
				if !released {
					syscall.Kill(-pgid, syscall.SIGKILL)
				}
			})
		}
		mu.Unlock()
		return err
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		released = true
		if timer != nil {
			timer.Stop()
		}
	}
}
