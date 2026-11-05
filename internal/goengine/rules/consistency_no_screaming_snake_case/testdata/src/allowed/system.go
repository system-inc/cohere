package allowed

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// A constant computed from a system constant keeps the system's spelling, so a reader can look it up there.
const O_CLOEXEC_FLAGS = syscall.O_RDONLY | 0x1000000

const SYS_IOCTL_NUMBER = unix.SYS_IOCTL

// A constant group repeats its last values, so the second is computed from unix's constant too.
const (
	FIRST_SIGNAL = unix.SIGNAL_BASE + iota
	SECOND_SIGNAL
)

// A constant compared directly against a system constant mirrors it, on either side of the comparison.
const AF_LOCAL_FAMILY = 1

const SOCK_LOCAL_STREAM = 1

func isLocal(family int, kind int) bool {
	return family == AF_LOCAL_FAMILY && AF_LOCAL_FAMILY == syscall.AF_UNIX &&
		unix.SOCK_STREAM != SOCK_LOCAL_STREAM && kind > 0
}

func localSignal() int {
	// A local constant in such a file mirrors the same way.
	const SIG_LOCAL = unix.SIGNAL_BASE
	return SIG_LOCAL
}
