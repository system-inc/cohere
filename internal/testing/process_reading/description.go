package process_reading

import (
	"strconv"
	"strings"
	"time"
)

// Description is what a test reads of a process to name it: its parent, when it started and its command line.
type Description struct {
	Parent  int
	Started time.Time
	Command []string
}

// String is the description as `ps -o ppid=,lstart=,command=` prints it, its parent, its start in ps's lstart form and
// its command line, so a report reads the same as it did when ps wrote it.
func (description Description) String() string {
	return strconv.Itoa(description.Parent) + " " + description.Started.Local().Format("Mon Jan _2 15:04:05 2006") +
		" " + strings.Join(description.Command, " ")
}
