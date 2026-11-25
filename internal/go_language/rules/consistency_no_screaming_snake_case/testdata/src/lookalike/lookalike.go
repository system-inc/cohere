package lookalike

import (
	"syscall"

	sys_unix "golang.org/x/sys/unix"
)

// Another package's snake-case names, used rather than declared, are that package's to spell.
var readOnly = syscall.O_RDONLY

var ioctlNumber = sys_unix.SYS_IOCTL

// An embedded field is named by its type, which is another package's.
type fileStatus struct {
	sys_unix.Stat_t
}

func size(status fileStatus) int64 {
	return status.Stat_t.Size + int64(readOnly) + int64(ioctlNumber)
}

// A name that only escapes a keyword has no MixedCaps form that compiles, so no suggestion could be typed.
var type_ = "kind"

var _range = 2

// Nothing but underscores, or a digit once the underscore goes, has no MixedCaps form either.
var __ = 3

var _2fa = true
