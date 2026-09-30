//go:build !unix

package tools

import "os/exec"

const nonblockFlag = 0

// configureProcess leaves CommandContext to cancel the process; WaitDelay bounds inherited output pipes.
func configureProcess(cmd *exec.Cmd) {}
