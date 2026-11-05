package formatoptions

import (
	"bytes"
	"os"
)

type chainEntry struct {
	resolution Resolution

	// files is the contents of each source on disk the resolution was read from. The embedded sets
	// cannot change under a running binary and are not kept.
	files map[string][]byte
}

// stillReads reports whether every file of the chain reads what it did when the entry was made.
func (entry chainEntry) stillReads() bool {
	for source, recorded := range entry.files {
		contents, readError := os.ReadFile(source)
		if readError != nil || !bytes.Equal(contents, recorded) {
			return false
		}
	}
	return true
}
