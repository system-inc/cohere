// Package unix stands in for golang.org/x/sys/unix, so the fixtures prove the rule reads its constants by import path.
package unix

const (
	SYS_IOCTL   = 54
	SIGNAL_BASE = 32
	SOCK_STREAM = 1
)

// Stat_t is spelled as the operating system spells it, which a struct embedding it carries as its field's name.
type Stat_t struct {
	Size int64
}
