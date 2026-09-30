//go:build unix

package session

import "syscall"

const privateAppendFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
