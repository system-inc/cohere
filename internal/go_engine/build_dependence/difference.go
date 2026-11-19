package build_dependence

import (
	"bytes"
	"go/ast"
	"go/types"
)

// Difference is what one extra build changes against the host, as its file sets say: the graph's judgment of the
// build (Against) reads nothing else of it.
type Difference struct {
	// Packages are the packages whose files differ, by package ID: each package both builds have whose files differ,
	// and each package only the build has, with all its files.
	Packages map[string]PackageDifference

	// HostOnlyPackages are the IDs of the packages only the host has, every unit of which is build-dependent.
	HostOnlyPackages map[string]bool

	// BuildOnlyPackages are the IDs of the packages only the build has, whose files Packages holds. None of their units
	// is the host's, so they are counted apart, as work the build alone must check.
	BuildOnlyPackages map[string]bool

	// DependentObject answers for an object of a package the graph does not hold, a dependency checked without
	// syntax: whether its name bit, and its underlying bit, are set in the build. nil means no such object is
	// dependent.
	DependentObject func(object types.Object) (name bool, underlying bool)

	// DependentName answers the same for such an object as a record read back from the store names it, which has no
	// live object (#6a3kjvz): by its package's path and its local name there, its bits, and whether a unit of the
	// answering graph declares it. One the answering graph's package holds but no unit declares is read through its
	// type, as the record holds that (answered false); one of a package that graph does not hold answers both bits set,
	// failing wide. nil, with DependentObject set, fails every such object wide.
	DependentName func(path string, local string) (name bool, underlying bool, declared bool)
}

// PackageDifference is how one package's files differ between the host and the build.
type PackageDifference struct {
	// HostOnlyFiles are the files the host compiles and the build does not, as written (each package's GoFiles), so
	// they match the graph's units' files, which a reader sees. Every unit in one is build-dependent.
	HostOnlyFiles []string

	// SharedFiles are the files both compile, as written. A package with a host-only file has every unit
	// build-dependent whose file, as a reader sees it, is not one of these: a //line directive in a host-only file
	// names its units by a file the build lists nowhere, which matches no host-only file, and the record keeps no
	// other name of a unit's file to match instead (#ss4t5g6).
	SharedFiles []string

	// LineDirective says a file the two builds do not share, host-only or build-only, holds a line directive
	// (HoldsLineDirective), which can name its units by any file, one both builds compile among them, so every unit of
	// the package is build-dependent, in memory only (#ss4t5g6).
	LineDirective bool

	// BuildOnlyFiles are the files only the build compiles, parsed for syntax alone: a method one declares changes
	// its receiver's method set, a name of the universe one declares changes what that name means in the package's
	// shared files, and its units are counted apart.
	BuildOnlyFiles []*ast.File

	// Workspace says whether a package only the build has belongs to the checked modules, which the graph cannot
	// say of a package it does not hold. A package both builds have is counted where the host's record puts it.
	Workspace bool
}

// The two spellings that open a line directive. Either anywhere in a file's bytes counts, in a string or a comment's
// middle too, an over-approximation of the directive's rules on purpose: a directive missed leaves a unit unseeded,
// where one too many only seeds a package that needed none.
var (
	lineCommentDirective  = []byte("//line ")
	blockCommentDirective = []byte("/*line ")
)

// HoldsLineDirective reports whether file holds a line directive anywhere in its bytes (lineCommentDirective,
// blockCommentDirective), as read reads them: the caller's, so they are the bytes the graph was built from, which in
// the engine are the run's held bytes rather than the disk's again (pipeline.holdsLineDirective). A file read cannot
// read is said to hold one, failing wide.
func HoldsLineDirective(file string, read func(file string) ([]byte, error)) bool {
	source, readError := read(file)
	if readError != nil {
		return true
	}
	return bytes.Contains(source, lineCommentDirective) || bytes.Contains(source, blockCommentDirective)
}
