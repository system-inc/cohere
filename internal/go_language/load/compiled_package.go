package load

import "slices"

// compiledPackage is what `go list -compiled` says of one package with cgo files: its compiled files, absolute, which
// are its GoFiles and then the cgo tool's outputs in the build cache, and the error go list gave, as when the cgo tool
// failed, which leaves the package no compiled files at all.
type compiledPackage struct {
	ImportPath      string
	CompiledGoFiles []string
	Error           *listedError
}

// translated reports whether compiled holds the cgo tool's translation of listedPackage: files after its GoFiles,
// as go/packages finds them. A nil compiled, a package nobody asked go list -compiled of, holds none.
func (compiled *compiledPackage) translated(listedPackage *listedPackage) bool {
	return compiled != nil && len(compiled.CompiledGoFiles) > len(listedPackage.GoFiles)
}

// withTests is what go test compiles of a translated package with its in-package tests, "p [p.test]": its GoFiles,
// then its TestGoFiles, then the cgo tool's outputs, as go list -test -compiled orders them, with the package's error.
// The tests have no cgo of their own, since go refuses import "C" in a test file, so their translation is p's.
func (compiled *compiledPackage) withTests(root *listedPackage) *compiledPackage {
	if !compiled.translated(root) {
		return compiled
	}
	split := len(root.GoFiles)
	return &compiledPackage{
		ImportPath: compiled.ImportPath,
		CompiledGoFiles: slices.Concat(compiled.CompiledGoFiles[:split], absolute(root.Directory, root.TestGoFiles),
			compiled.CompiledGoFiles[split:]),
		Error: compiled.Error,
	}
}
