package odd_slice_argument_test

import (
	"go/types"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
)

// shapesSources is a package of every place a parameter can be declared, in two files: a function's and a method's
// parameters, results and receiver, blank and unnamed ones, a function literal's inside a function, inside a method,
// inside another literal and at package level, an interface method's, a function type's, and a generic function's,
// which its instance's objects stand for.
var shapesSources = map[string]string{
	"shapes.go": `// Package shapes declares a parameter in every place one can be declared.
package shapes

// Pairs takes a slice, and names its results.
func Pairs(values []string, _ []int, count int) (first string, rest []string) {
	return "", values
}

// Holder holds a method.
type Holder struct{}

// Take takes a slice, by a named receiver.
func (holder *Holder) Take(values []string) {
	consume := func(inner []string, _ int) {
		nested := func(deepest []string) {}
		nested(inner)
	}
	consume(values, 0)
}

// Taker is an interface whose method names its parameter.
type Taker interface {
	Take(values []string)
}

// Passer is a function type whose parameter is named.
type Passer func(values []string)

// Generic takes a slice of any element.
func Generic[Element any](values []Element) {}

// instance calls Generic, so the instance's parameter is an object of its own.
func instance() {
	Generic([]int{1})
}
`,
	"more.go": `package shapes

// atLoad is a function literal at package level, which runs in the initializer.
var atLoad = func(values []string) {
	inner := func(again []string) {}
	inner(values)
}

// unnamed takes parameters it never names.
func unnamed([]string, int) {}
`,
}

// writeShapes writes shapesSources into directory, a tree laid out by import path, as the package owners/shapes.
func writeShapes(test *testing.T, directory string) {
	test.Helper()
	folder := filepath.Join(directory, "src", "owners", "shapes")
	if makeError := os.MkdirAll(folder, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	for name, source := range shapesSources {
		if writeError := os.WriteFile(filepath.Join(folder, name), []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// fixturePatterns is the import path of every package under the rule's testdata.
func fixturePatterns(test *testing.T) []string {
	test.Helper()
	source := filepath.Join(analysistest.TestData(), "src")
	var patterns []string
	walkError := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return walkError
		}
		relative, relativeError := filepath.Rel(source, filepath.Dir(path))
		if relativeError != nil {
			return relativeError
		}
		if pattern := filepath.ToSlash(relative); !slices.Contains(patterns, pattern) {
			patterns = append(patterns, pattern)
		}
		return nil
	})
	if walkError != nil {
		test.Fatalf("listing the fixtures: %v", walkError)
	}
	return patterns
}

// ownersCompared is how many objects one comparison of OwnerOf with the walk covered: every variable compared, the
// parameters the walk found among them, and of those, a function literal's and a package-level literal's.
type ownersCompared struct {
	variables  int
	parameters int
	literals   int
	atLoad     int
}

// compareOwners holds OwnerOf to the walk over every package the resolver learned from patterns in directory: for every
// variable home's syntax defines, and every parameter of an instance of a function, whose object no syntax defines,
// OwnerOf finds the walk's owner, or none where the walk has none.
func compareOwners(test *testing.T, directory string, patterns []string) ownersCompared {
	test.Helper()
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("cohere-go's options hold a %T, not a fact resolver", options.Resolver)
	}
	roots := loadFixtures(test, directory, patterns)
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{odd_slice_argument.MarksAnalyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("analyzing %s: %v", action.Package.ID, action.Err)
		}
	}
	var compared ownersCompared
	packages.Visit(roots, nil, func(visited *packages.Package) {
		home := resolver.Package(visited.Types)
		if home == nil || len(home.Files()) == 0 {
			return
		}
		walked := odd_slice_argument.OwnersByWalk(home)
		compare := func(variable *types.Var) {
			compared.variables++
			wanted, declared := walked[variable]
			got := odd_slice_argument.OwnerOf(home, variable)
			if (got != nil) != declared || (got != nil && *got != wanted) {
				test.Errorf("in %s, %s at %s: the walk finds %+v, declared %t, OwnerOf %+v", visited.ID, variable,
					visited.Fset.Position(variable.Pos()), wanted, declared, got)
			}
			if !declared {
				return
			}
			compared.parameters++
			if odd_slice_argument.IsLiteral(wanted) {
				compared.literals++
				if odd_slice_argument.IsAtLoad(wanted) {
					compared.atLoad++
				}
			}
		}
		for _, defined := range home.Info().Defs {
			if variable, isVariable := defined.(*types.Var); isVariable {
				compare(variable)
			}
		}
		for _, instance := range home.Info().Instances {
			if signature, isSignature := instance.Type.(*types.Signature); isSignature {
				for parameter := range signature.Params().Variables() {
					compare(parameter)
				}
			}
		}
	})
	return compared
}

// Where a parameter is declared, found from its position in the one declaration that holds it, is exactly what the
// walk over every file of its package found, for every variable of every fixture package, of the generated graphs and
// their readers, and of a package declaring a parameter in every place one can be: none for a result, a receiver, an
// interface method's, a function type's, an instance's or a local, the function for a declared function's or method's,
// and the literal and its enclosing function, none at package level, for a literal's (#8d9r2rh).
func TestAParametersOwnerIsTheWalksOwner(test *testing.T) {
	test.Parallel()
	fixtures := compareOwners(test, analysistest.TestData(), fixturePatterns(test))
	directory := test.TempDir()
	writeShapes(test, directory)
	patterns := []string{"owners/shapes"}
	for _, variant := range writeGeneratedVariants(test, directory, rand.New(rand.NewPCG(generatedSeed, 0))) {
		patterns = append(patterns, variant.path+"/reader")
	}
	generated := compareOwners(test, directory, patterns)
	test.Logf("fixtures: %+v; generated graphs and shapes: %+v", fixtures, generated)
	// Floors, so a comparison that reached too little fails rather than passing on nothing.
	if fixtures.parameters < 50 || generated.parameters < 500 || generated.literals < 4 || generated.atLoad < 2 {
		test.Fatalf("want at least 50 fixture parameters, 500 generated, 4 literals' and 2 at package level, got "+
			"fixtures %+v, generated %+v", fixtures, generated)
	}
}
