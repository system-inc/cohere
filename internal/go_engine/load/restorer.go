package load

import "golang.org/x/tools/go/packages"

// The loaders a Restorer is told ran, which the findings cache keys every package on, since each gives a package
// other files: the load from source, and go/packages with the project's cgo translated.
const (
	LoaderSource        = "source"
	LoaderCgoTranslated = "cgo-translated"
)

// Restorer is the findings cache as the load sees it. Once go list has run and the graph is built, and before any
// package is parsed, Restore is given every package the load reaches, files and imports set, and the packages to
// analyze, and returns those it holds an entry for. The load from source parses and type-checks none of those: it
// reads one's types from its export data, and only when a package it does check imports it, directly or through
// other restored packages. go/packages has checked every package by then, so on that load the answer changes
// nothing the load does.
type Restorer interface {
	Restore(loader string, reachable []*packages.Package, analyzed []*packages.Package) map[*packages.Package]*Restored
}

// Restored is what the load needs of a package the findings cache holds, in place of its source.
type Restored struct {
	// ExportData is the package's types as gcexportdata.Write wrote them, empty for unsafe, whose types are the
	// type checker's own.
	ExportData []byte

	// Imports are the paths of the packages its types import, in the order its type check left them, which export
	// data does not keep: a package read from it imports every package its export data names.
	Imports []string

	// Withheld is what the load withheld of the package when it last type-checked it.
	Withheld PackageWithheld
}

// PackageWithheld is what the load withheld of one package outside the workspace with cgo files (see Withheld and
// withholdCgoDeclarations), kept per package so a package the cache restores contributes what it did when it was
// checked.
type PackageWithheld struct {
	Errors []WithheldError

	// Declarations are the positions, "file:line:column", of the declarations given to the analyzers without their
	// bodies or initial values.
	Declarations []string
}

// WithheldError is one type error withheld in a package.
type WithheldError struct {
	File    string
	Line    int
	Column  int
	Message string
}

// loadedOf is the packages of nodes, in order.
func loadedOf(nodes []*node) []*packages.Package {
	loaded := make([]*packages.Package, 0, len(nodes))
	for _, each := range nodes {
		loaded = append(loaded, each.loaded)
	}
	return loaded
}
