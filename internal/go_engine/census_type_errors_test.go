package go_engine

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// A plain run with the must-check census and no findings store lints beside its load, and the pass's walks hand each
// package to the census's declarations-only graph as the load makes it final, before the load's end shows its type
// errors (plainDeclarations.add): the census-duplicate-field fixture's Shape names its field a twice, so its syntax
// holds two fields where its type holds one, and later, written here, is four thousand functions that read it, which
// the load checks after it, so the pass has fields in hand while the load still runs (#jzzdvcf). The run reports the
// type error and ends as a load with type errors does, the lint not reached. Hand an ill-typed package to the graph
// and take the collector's guard on a struct's typed fields away, and the walk panics and takes the process with it.
func TestACensusRunOverAPackageWithADuplicateFieldEndsWithItsTypeError(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "census-duplicate-field"))
	var later strings.Builder
	later.WriteString("package later\n\nimport \"example.com/censusduplicatefield/fields\"\n\n")
	for index := range 4000 {
		fmt.Fprintf(&later, "func Size%d(shape fields.Shape) int { return fields.Size(shape) + %d }\n\n", index,
			index)
	}
	if makeError := os.MkdirAll(filepath.Join(root, "later"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(root, "later", "later.go"), []byte(later.String()),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	_, standardOutput, standardError, exitCode := runOverExiting(test, root, rules.All(),
		func(engine *pipeline) { engine.mustCheckCensus = test.TempDir() }, "--no-cache")
	if exitCode != exitFindings {
		test.Fatalf("the run exited %d, want %d, the type error's:\n%s\n%s", exitCode, exitFindings, standardOutput,
			standardError)
	}
}

// The census's declarations-only graph adds no package the type checker found errors in, its own or an import's, so
// its objects are answered wide as a package with no type information's are, and never one with type information
// and no errors. Add an ill-typed package and the graph holds it.
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
