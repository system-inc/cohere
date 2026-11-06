package flagged

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// A file importing syscall is not a pass: a constant with no tie to a system constant is ours.
const LOCAL_FLAG = 1 // want "`LOCAL_FLAG` is in screaming snake case; spell it `LocalFlag`"

// Only a constant mirrors a system constant; a variable holding one is ours to name.
var O_RDONLY_COPY = syscall.O_RDONLY // want "`O_RDONLY_COPY` is in screaming snake case; spell it `ORdonlyCopy`"

// Compared against another of our constants, not a system one, so nothing ties it to syscall.
const OTHER_FLAG = 2 // want "`OTHER_FLAG` is in screaming snake case; spell it `OtherFlag`"

var flagsMatch = LOCAL_FLAG == OTHER_FLAG

// A comparison against a system constant through arithmetic is not a direct mirror.
const SHIFTED_FLAG = 4 // want "`SHIFTED_FLAG` is in screaming snake case; spell it `ShiftedFlag`"

var shiftedMatch = SHIFTED_FLAG+1 == syscall.O_RDONLY

// Names in another script fold the same way, by rune.
var größe_wert = 1 // want "`größe_wert` has an underscore in it; spell it `größeWert`"

var Ärger_zahl = 1 // want "`Ärger_zahl` has an underscore in it; spell it `ÄrgerZahl`"

// A constant tied to a system constant but named otherwise is not the system's spelling of anything, so it mirrors
// nothing: computed from one, assigned from one, or compared against one.
const O_CLOEXEC_FLAGS = syscall.O_RDONLY | 0x1000000 // want "`O_CLOEXEC_FLAGS` is .*; spell it `OCloexecFlags`"

// The system's spelling of a different constant than the one the value names mirrors nothing either.
const O_RDONLY = syscall.O_WRONLY // want "`O_RDONLY` is in screaming snake case; spell it `ORdonly`"

const SYS_IOCTL_NUMBER = unix.SYS_IOCTL // want "`SYS_IOCTL_NUMBER` is .*; spell it `SysIoctlNumber`"

const AF_LOCAL_FAMILY = 1 // want "`AF_LOCAL_FAMILY` is in screaming snake case; spell it `AfLocalFamily`"

const SOCK_LOCAL_STREAM = 1 // want "`SOCK_LOCAL_STREAM` is in screaming snake case; spell it `SockLocalStream`"

var localMatch = AF_LOCAL_FAMILY == syscall.AF_UNIX && unix.SOCK_STREAM != SOCK_LOCAL_STREAM

// A constant group repeats its last values, so the second repeats unix.SIGNAL_BASE, which is the first one's
// spelling and not its own.
const (
	SIGNAL_BASE = unix.SIGNAL_BASE
	SIGNAL_COPY // want "`SIGNAL_COPY` is in screaming snake case; spell it `SignalCopy`"
)

func localSignal() int {
	// A local constant named otherwise is judged as a local, so its suggestion opens lowercase.
	const SIG_LOCAL = unix.SIGNAL_BASE // want "`SIG_LOCAL` is in screaming snake case; spell it `sigLocal`"
	return SIG_LOCAL + SIGNAL_BASE
}
