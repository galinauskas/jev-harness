//go:build !unix

package tools

import "os/exec"

const nonblockFlag = 0

// CommandContext cancels the shell; WaitDelay bounds inherited output pipes.
func configureProcess(cmd *exec.Cmd) {}
