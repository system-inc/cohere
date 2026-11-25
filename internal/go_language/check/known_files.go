package check

import (
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

// knownFiles are every file of a load by path, and every base name among them, so a word of a finding's message whose
// base name no file has is passed over before it is made a path (namedFiles), which most words are.
type knownFiles struct {
	paths     map[string]bool
	baseNames map[string]bool
}

// newKnownFiles is an empty set of a load's files.
func newKnownFiles() *knownFiles {
	return &knownFiles{paths: map[string]bool{}, baseNames: map[string]bool{}}
}

// addPackage adds every file of owner's, of each kind the load lists (ownFiles), by path and by base name.
func (known *knownFiles) addPackage(owner *packages.Package) {
	for file := range ownFiles(owner) {
		known.paths[file] = true
		known.baseNames[filepath.Base(file)] = true
	}
}
