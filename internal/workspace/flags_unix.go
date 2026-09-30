//go:build unix

package workspace

import "syscall"

const nonblockFlag = syscall.O_NONBLOCK
