//go:build unix

package privatefile

import "golang.org/x/sys/unix"

const readFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW
