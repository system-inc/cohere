package go_language

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

// The census's declarations-only graph adds no package the type checker found errors in, its own or an import's, so
// its objects are answered wide as a package with no type information's are, and never one with type information
// and no errors. A plain run with the census and no findings store lints beside its load, and its walks hand each
// package on as the load makes it final, before the load's end shows its type errors (#jzzdvcf), so one whose syntax
// does not match its types, a field named twice, would otherwise reach the collector. Add an ill-typed package and
// the graph holds it.
func TestTheCensusGraphAddsNoIllTypedPackage(test *testing.T) {
	test.Parallel()

	declarations := newPlainDeclarations(func(*packages.Package) bool { return true },
		func(*packages.Package) []string { return nil })
	broken := &packages.Package{ID: "broken", PkgPath: "example.com/broken", IllTyped: true,
		Types: types.NewPackage("example.com/broken", "broken"), TypesInfo: &types.Info{}}
	sound := &packages.Package{ID: "sound", PkgPath: "example.com/sound",
		Types: types.NewPackage("example.com/sound", "sound"), TypesInfo: &types.Info{}}
	declarations.add(broken)
	declarations.add(sound)
	if declarations.holds(broken.ID) {
		test.Error("the census graph holds a package the type checker found errors in")
	}
	if !declarations.holds(sound.ID) {
		test.Error("the census graph does not hold a package with type information and no errors")
	}
}
