package go_engine

import (
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// A Closure lookup the host's rules recorded differs where the build finds the looked-up path in a package of another
// ID, though it finds it too (#bed0mnk, the Square's must on the three-piece head): the ID half of
// differingClosureLookups, whose presence half the closure modules of TestAPackageWhoseImportsDifferIsReachedWhole
// pin. No module shows this half: go test builds again every package that reaches a test variant, so where io
// resolves to another ID in a package's closure, one of the package's own imports does too, and differingImports
// reaches it first. So the graphs are made here, the gate's types importing io on both sides and its import graph
// resolving io to a test variant in the build alone, beside the same graph with io's own ID, which differs nothing.
func TestAClosureLookupFoundInAPackageOfAnotherIdentifierDiffers(test *testing.T) {
	test.Parallel()

	io := types.NewPackage("io", "io")
	readerName := types.NewTypeName(token.NoPos, io, "Reader", nil)
	types.NewNamed(readerName, types.NewInterfaceType(nil, nil), nil)
	io.Scope().Insert(readerName)
	gate := types.NewPackage("example.test/gate", "gate")
	gate.SetImports([]*types.Package{io})
	hosted := &packages.Package{ID: "example.test/gate", PkgPath: "example.test/gate",
		Imports: map[string]*packages.Package{"io": {ID: "io", PkgPath: "io"}}}
	lookups := []build_reads.Lookup{{Query: build_reads.QueryClosure, Path: "io", Name: "Reader", Found: true,
		Answer: readerName}}

	// Each ID the build's graph resolves io to, by the differences it wants.
	for builtIdentifier, want := range map[string][]string{
		"io": nil,
		"io [example.test/gate.test]": {"io.Reader in the import closure, found in io on the host and in " +
			"io [example.test/gate.test] in the build"},
	} {
		read := &reachBuiltPackage{identifier: hosted.ID, types: gate,
			imports: map[string]*packages.Package{"io": {ID: builtIdentifier, PkgPath: "io"}}}
		if got := differingClosureLookups(lookups, hosted, read); !slices.Equal(got, want) {
			test.Errorf("with io resolving to %s in the build, want %q, got %q", builtIdentifier, want, got)
		}
	}
}
