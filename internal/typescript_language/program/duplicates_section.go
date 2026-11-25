package program

import (
	"crypto/sha256"

	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// DuplicatesSection is the duplicates report's cache (#jfvhbj7): each project file's functions as the canonical hash
// read them, so a run hashes only the files whose digests could have moved. Key names what every entry depends on
// beyond its file, the tsconfig and the root; a section under another key says nothing.
//
// An entry is keyed on its file's signature fingerprint (SignatureFingerprints), or its type fingerprint when the run
// computed no shapes. A digest reads its own file's bytes, which both hold, and other files only through the checker:
// where an import resolves, and the types that decide whether an operator's operands are numbers. Those are what the
// signature fingerprint already vouches for when the types section replays a file's diagnostics. Once a file's
// declaration signature has graduated (Graph.GraduateSignatures), an edit inside its functions' bodies re-hashes no
// file that imports it; until then its shape is its bytes, and its importers are hashed again, as the types section
// checks them again.
type DuplicatesSection struct {
	Key     string
	Entries map[string]DuplicatesEntry
}

// DuplicatesEntry is one file's functions, with their File left empty, since the entry's path names it.
type DuplicatesEntry struct {
	Fingerprint [sha256.Size]byte
	Functions   []duplicate_functions.Function
	Refused     []duplicate_functions.Refusal
	Bodyless    int
}
