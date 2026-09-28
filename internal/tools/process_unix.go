//go:build unix

package tools

import (
	"os"
	"os/exec"
	"syscall"
)

const nonblockFlag = syscall.O_NONBLOCK

// Cancel the process group so ordinary shell children cannot outlive a timeout.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
