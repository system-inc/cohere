package build_dependence

import (
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
}

// PackageDifference is how one package's files differ between the host and the build.
type PackageDifference struct {
	// HostOnlyFiles are the files the host compiles and the build does not, as written (each package's GoFiles), so
	// they match the graph's units' files, which a reader sees. Every unit in one is build-dependent.
	HostOnlyFiles []string

	// BuildOnlyFiles are the files only the build compiles, parsed for syntax alone: a method one declares changes
	// its receiver's method set, a name of the universe one declares changes what that name means in the package's
	// shared files, and its units are counted apart.
	BuildOnlyFiles []*ast.File

	// Workspace says whether a package only the build has belongs to the checked modules, which the graph cannot
	// say of a package it does not hold. A package both builds have is counted where the host's record puts it.
	Workspace bool
}
