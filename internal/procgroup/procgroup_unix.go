//go:build !windows

package procgroup

import (
	"os/exec"
	"syscall"
)

// isolate puts the child in its own process group (Setpgid) and replaces the
// default cancel — which SIGKILLs only cmd.Process.Pid — with a group kill so the
// child's descendants die with it. Getpgid returns the child's own pgid (it is
// the group leader once Setpgid took effect); syscall.Kill(-pgid, ...) signals
// the entire group. If the pgid cannot be read (the child never started, or a
// race), fall back to killing the direct process.
func isolate(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
			return syscall.Kill(-pgid, syscall.SIGKILL)
		}
		return cmd.Process.Kill()
	}
}
