// Package differences holds where the port's fix parts from the original's, which writes bytes.Equal whatever the
// file calls the package bytes: here a local named bytes over the file's import of it. Its two packages beside it
// hold a file that imports bytes under another name alone, and a dot import. Each record says what the port does,
// its reason on the line above. The shapes no record is on are the original's to the byte.
package differences

import (
	"bytes"
	other "bytes"
)

type shadow struct{}

func (shadow) Compare(first []byte, second []byte) int { return 0 }

func arguments(first []byte, second []byte, rows [][]byte) {
	// The fix's call holds the arguments' own nodes, so format prints them from their positions, across lines and
	// without the comments among them, as the original's does.
	_ = bytes.Compare(
		first,
		second,
	) == 0
	_ = bytes.Compare(first, // the first
		second /* the second */) != 0
	_ = bytes.Compare(rows[len(rows)-1][:], append([]byte{}, first...)) == 0
	_ = bytes.Compare(func() []byte { return first }(), second) != 0
	// Where bytes names the import, the fix writes bytes.Equal, the original's, whatever name the call uses.
	_ = other.Compare(first, second) == 0
}

func renamed(first []byte, second []byte) {
	bytes := shadow{}
	// The local bytes has no Equal, so the original's bytes.Equal(first, second) does not compile, and the fix
	// writes other.Equal(first, second), the name the call of Compare already uses.
	_ = other.Compare(first, second) == 0 // port moves: should use bytes.Equal(first, second) instead
	// The local's method is not bytes.Compare.
	_ = bytes.Compare(first, second) == 0
}

func later(first []byte, second []byte) {
	// A bytes declared after the comparison does not stand where it stands, so bytes still names the import there.
	_ = other.Compare(first, second) != 0
	bytes := 1
	_ = bytes
}
