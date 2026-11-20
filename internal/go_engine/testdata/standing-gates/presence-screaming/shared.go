package gate

// The import makes this a system file, and no constant of syscall's is read, since each platform declares its own.
import _ "syscall"

// O_RDONLY is screaming's candidate, declared in the file both builds compile, which imports syscall.
const O_RDONLY = 0

// Reads compares with O_RDONLY, so the rule reads it.
func Reads(flags int) bool { return flags == O_RDONLY }

// Port names no candidate.
func Port() int { return 80 }
