package checker

import (
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// An action on a restored package makes no inputs, since it runs no pass, and still restores: over packages half of
// which are restored, with Options.FactsBelowRestored giving each restored action the same-package edge to the
// analyzer that learns facts below it, and every key a pass holds set (Dependents, Failed under Tolerant), the graph
// finishes with no action failed, every action on a restored package restored once with no pass, and every action on
// the others run as a pass that holds what it requires and every key. A restored action that wrote its prerequisite's
// result or a key into the inputs it never made would panic on the nil map.
func TestARestoredActionRestoresWithNoInputs(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	identifiers := make([]string, 0, len(roots))
	for _, root := range roots {
		identifiers = append(identifiers, root.ID)
	}
	slices.Sort(identifiers)
	restoredPackages := map[string]bool{identifiers[1]: true, identifiers[3]: true}
	learner := &analysis.Analyzer{
		Name:      "learner",
		Doc:       "learner learns facts and does nothing else",
		FactTypes: []analysis.Fact{new(declaredFact)},
		Run:       func(*analysis.Pass) (any, error) { return nil, nil },
	}
	dependentsKey := &analysis.Analyzer{Name: "dependents", Doc: "dependents is a key, never run"}
	failedKey := &analysis.Analyzer{Name: "failed", Doc: "failed is a key, never run"}
	var passes sync.Map
	requiring := &analysis.Analyzer{
		Name:       "requiring",
		Doc:        "requiring requires learner and records what its pass holds",
		Requires:   []*analysis.Analyzer{learner},
		ResultType: reflect.TypeFor[bool](),
		Run: func(pass *analysis.Pass) (any, error) {
			_, holdsLearner := pass.ResultOf[learner]
			_, holdsDependents := pass.ResultOf[dependentsKey]
			_, holdsFailed := pass.ResultOf[failedKey]
			passes.Store(pass.Pkg.Path(), holdsLearner && holdsDependents && holdsFailed)
			return true, nil
		},
	}
	var restoresMutex sync.Mutex
	restores := map[string]int{}
	graph, analyzeError := Analyze([]*analysis.Analyzer{requiring}, roots, &Options{
		Restored:           func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		FactsBelowRestored: func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		Restore: func(restoring *Action) error {
			restoresMutex.Lock()
			restores[restoring.Analyzer.Name+"@"+restoring.Package.ID]++
			restoresMutex.Unlock()
			return nil
		},
		Dependents: dependentsKey,
		Failed:     failedKey,
		Tolerant:   func(*analysis.Analyzer) bool { return true },
	})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	for action := range graph.All() {
		if action.Err != nil {
			test.Errorf("%s failed: %v", action, action.Err)
		}
	}
	for _, identifier := range identifiers {
		for _, analyzer := range []*analysis.Analyzer{requiring, learner} {
			restored := restores[analyzer.Name+"@"+identifier]
			if restoredPackages[identifier] && restored != 1 || !restoredPackages[identifier] && restored != 0 {
				test.Errorf("%s on %s, restored %t, was restored %d times", analyzer.Name, identifier,
					restoredPackages[identifier], restored)
			}
		}
		held, ran := passes.Load(identifier)
		if ran == restoredPackages[identifier] {
			test.Errorf("requiring on %s, restored %t, ran a pass %t", identifier, restoredPackages[identifier], ran)
		}
		if holdsAll, isHolds := held.(bool); ran && (!isHolds || !holdsAll) {
			test.Errorf("requiring's pass on %s did not hold learner's result and every key", identifier)
		}
	}
}
