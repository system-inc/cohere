package standing_presence

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// strippedPass is a pass over the fixture package at path, loaded and type-checked whole as the engine loads one, and
// then with the body of the function named stripped taken away, as a build that strips a standing body holds the
// package: its types know what the body did, its syntax no longer says it.
func strippedPass(test *testing.T, path string, stripped string) *analysis.Pass {
	test.Helper()
	directory, directoryError := filepath.Abs("testdata")
	if directoryError != nil {
		test.Fatal(directoryError)
	}
	loaded, loadError := load.Packages(directory, []string{path}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil || len(loaded.Roots) != 1 {
		test.Fatalf("loading the %s fixture: %v", path, loadError)
	}
	root := loaded.Roots[0]
	for _, file := range root.Syntax {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Name.Name == stripped {
				function.Body = nil
			}
		}
	}
	return &analysis.Pass{Fset: root.Fset, Files: root.Syntax, TypesInfo: root.TypesInfo, Pkg: root.Types}
}

// usedVariable is the host's use of the package-level variable named name of pass's package, and the build's finder,
// which finds the package's own objects by name.
func usedVariable(
	pass *analysis.Pass,
	name string,
) ([]fact_reach.UsedObject, func(identity build_reads.Identity) (types.Object, bool)) {
	used := []fact_reach.UsedObject{{Object: fact_reach.Object(pass.Pkg.Path() + "." + name),
		Identity: build_reads.Identity{Kind: build_reads.IdentityObject, PackagePath: pass.Pkg.Path(),
			Path: objectpath.Path(name)}}}
	find := func(identity build_reads.Identity) (types.Object, bool) {
		object := pass.Pkg.Scope().Lookup(string(identity.Path))
		return object, object != nil
	}
	return used, find
}

// The reader's M3 (#xeafx79): a package-level scanner, or rows, made only in a standing body that a strip takes away
// is still named, so its users, the reading body and the assigning one alike, leave. The build's syntax holds no
// trace of setup's assignment, so the row must name input by what it is, not by where it was made.
func TestAVariableMadeOnlyInAStrippedBodyIsNamed(test *testing.T) {
	test.Parallel()

	scanned := strippedPass(test, "scanned", "setup")
	used, find := usedVariable(scanned, "input")
	if named := scannerErrorNamed(scanned, used, find); !slices.Equal(named,
		[]fact_reach.Named{{Object: "scanned.input", Reason: ScannerError.Reason()}}) {
		test.Errorf("scannererr named %v, want scanned.input alone", named)
	}

	queried := strippedPass(test, "queried", "query")
	used, find = usedVariable(queried, "rows")
	if named := sqlRowsErrorNamed(queried, used, find); !slices.Equal(named,
		[]fact_reach.Named{{Object: "queried.rows", Reason: SqlRowsError.Reason()}}) {
		test.Errorf("sqlrowserr named %v, want queried.rows alone", named)
	}
}

// A local marker whose declaring body is stripped is seen only through the host's record (#xeafx79, round 2's mutant
// 6): with Sealed's body taken away, the build's syntax declares no marker, and seal is a marker's name only because
// the record says Sealed declares one.
func TestALocalMarkerOfAStrippedBodyIsReadFromTheRecord(test *testing.T) {
	test.Parallel()

	sealing := strippedPass(test, "sealing", "Sealed")
	if presentMarkers(sealing, nil)["seal"] {
		test.Fatalf("the stripped build's own syntax declares seal's marker, so the record is not what names it")
	}
	if !presentMarkers(sealing, []string{"seal"})["seal"] {
		test.Errorf("seal, a marker the record says Sealed declares, is not among the markers")
	}
}
