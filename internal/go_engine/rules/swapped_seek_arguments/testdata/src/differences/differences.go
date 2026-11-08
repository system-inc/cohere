// Package differences holds the Seek calls where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a dot import, whose whence constant is an identifier and no selector, the same
// package imported again under a name, a type whose Seek returns a named int64, and arguments the fix writes across
// lines and with a comment among them.
package differences

import (
	. "io"
	seeking "io"
	"os"
)

type position int64

type named struct{}

func (named) Seek(offset int64, whence int) (position, error) { return 0, nil }

func plausible(seeker Seeker, file *os.File) {
	seeker.Seek(SeekStart, 0)
	named{}.Seek(seeking.SeekStart, 0)
	file.Seek(seeking.SeekEnd, 0)
	file.Seek(seeking.SeekCurrent,
		0)
	file.Seek(seeking.SeekCurrent /* the offset */, 0)
}
