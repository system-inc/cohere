package allowed

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// A constant named exactly as the system constant it is assigned from keeps the system's spelling, so a reader can
// look it up there.
const SYS_IOCTL = unix.SYS_IOCTL

// A conversion still mirrors the constant of its own name.
const O_RDONLY = uint32(syscall.O_RDONLY)

// A constant compared directly against the system constant of its own name mirrors it, on either side of the
// comparison, as a fallback for a platform without it would.
const AF_UNIX = 1

const SOCK_STREAM = 1

func isLocal(family int, kind int) bool {
	return family == AF_UNIX && AF_UNIX == syscall.AF_UNIX && unix.SOCK_STREAM != SOCK_STREAM && kind > 0
}

func localSignal() int {
	// A local constant in such a file mirrors the same way, and the mirror's spelling outranks a local's lower case.
	const SIGNAL_BASE = unix.SIGNAL_BASE
	return SIGNAL_BASE
}
