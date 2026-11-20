package intermediate_representation_test

// The strongly connected components buildSccs leaves on each function, read over the package's fixtures (#z844663).

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// sccFixtureFunctions are the source functions of the package's fixtures, each fixedbugs program and the every-form
// fixture, built as the engine builds them.
func sccFixtureFunctions(test *testing.T) []*intermediate_representation.Function {
	test.Helper()
	var functions []*intermediate_representation.Function
	for _, unit := range differentialTestdataUnits(test) {
		program := intermediate_representation.NewProgram(unit.fileSet)
		for _, imported := range unit.types.Imports() {
			program.CreatePackage(imported, nil, nil, true)
		}
		built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
		built.Build()
		functions = append(functions, differentialOurSourceFunctions(built)...)
	}
	return functions
}

// sccClosure is the reachability go/ir builds eagerly and ours no longer does: for each component, by index, the
// components it reaches, itself among them. Sccs are in reverse topological order, so each component's successors
// are closed before it.
func sccClosure(function *intermediate_representation.Function) [][]bool {
	closure := make([][]bool, len(function.Sccs))
	for _, scc := range function.Sccs {
		reached := make([]bool, len(function.Sccs))
		reached[scc.Index] = true
		for _, successor := range scc.Successors {
			for index, isReached := range closure[successor.Index] {
				reached[index] = reached[index] || isReached
			}
		}
		closure[scc.Index] = reached
	}
	return closure
}

// TestReachesAnswersAsTheEagerClosure holds Reaches, which walks when asked, to the reachability set go/ir builds
// for every component, on every pair of blocks the entry reaches in every fixture function. The Recover block is in
// no component, the eager answer having none for it.
func TestReachesAnswersAsTheEagerClosure(test *testing.T) {
	test.Parallel()
	pairs, reachingPairs := 0, 0
	for _, function := range sccFixtureFunctions(test) {
		closure := sccClosure(function)
		for _, from := range function.Blocks {
			if from.Scc == nil {
				continue
			}
			for _, to := range function.Blocks {
				if to.Scc == nil {
					continue
				}
				pairs++
				want := closure[from.Scc.Index][to.Scc.Index]
				if want {
					reachingPairs++
				}
				if got := from.Reaches(to); got != want {
					test.Errorf("%s: block %d reaches block %d is %t, the eager closure says %t",
						function.RelativeString(nil), from.Index, to.Index, got, want)
				}
			}
		}
	}
	if reachingPairs == 0 || reachingPairs == pairs {
		test.Fatalf("%d of %d block pairs reach, so the comparison told nothing apart", reachingPairs, pairs)
	}
	test.Logf("%d block pairs compared, %d of them reaching", pairs, reachingPairs)
}
