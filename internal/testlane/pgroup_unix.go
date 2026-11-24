//go:build !windows

package testlane

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// killGroupOnCancel starts c in a process group of its own and makes a
// cancelled context kill that whole group. Killing only `sh` would leave its
// children running past the deadline the caller reported.
//
// The group is the child's own, never dross's: a negative pid names the group
// whose id is the child's pid, which Setpgid made distinct from ours.
func killGroupOnCancel(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// The group is already gone: nothing was left to kill.
			return os.ErrProcessDone
		}
		return err
	}
}
