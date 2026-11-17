package cache

import (
	"fmt"
	"strings"
)

// NotKept is why a run keeps no system cache, or cannot keep it as it should: a root it cannot find, a directory it
// cannot make, a write or a trim that fails. It is never an error. The run computes what the cache would have held,
// its findings and exit are as without it, and it says the reason once, as "system cache not kept: <reason>". A
// sealed job's empty, read-only home is exactly this case.
type NotKept struct {
	// reason is the one line after "system cache not kept: ".
	reason string
}

// NotKeptBecause is the NotKept of cause, an error from finding, making, writing or trimming the system cache.
func NotKeptBecause(cause error) *NotKept {
	return notKeptBecause(cause.Error())
}

// lineBreaks turns each line break of a reason into a separator on one line.
var lineBreaks = strings.NewReplacer("\r\n", "; ", "\n", "; ", "\r", "; ")

// notKeptBecause is the NotKept of reason, said on one line: an error joined from several holds a line each, and
// the run's footer gives the system cache one.
func notKeptBecause(reason string) *NotKept {
	return &NotKept{reason: lineBreaks.Replace(strings.TrimSpace(reason))}
}

// Reason is why the system cache is not kept, one line.
func (notKept *NotKept) Reason() string {
	return notKept.reason
}

// String is the line the run prints.
func (notKept *NotKept) String() string {
	return fmt.Sprintf("system cache not kept: %s", notKept.reason)
}
