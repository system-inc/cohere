package checker

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// An action on a restored package makes no inputs, since it runs no pass, and still inherits its prerequisites' facts
// before it restores: over a chain of packages, each importing the one before it, every other one restored, with
// Options.FactsBelowRestored giving each restored action the same-package edge to the analyzer that learns facts below
// it, and every key a pass holds set (Dependents, Failed under Tolerant), the graph finishes with no action failed,
// every action on a restored package restored once with no pass, holding as it restores the package fact of every
// package below it, and every action on the others run as a pass that holds what it requires and every key. A
// restored action that wrote its prerequisite's result or a key into the inputs it never made would panic on the nil
// map, and one that restored before it inherited would hold none of the facts below it, which its importers then lose.
func TestARestoredActionRestoresWithNoInputs(test *testing.T) {
	test.Parallel()
	// p0 to p3, each but p0 importing and calling the one before it, so a package's facts reach every one above it.
	files := map[string]string{}
	for index := range 4 {
		name := fmt.Sprintf("p%d", index)
		source := "package " + name + "\n\n"
		call := ""
		if index > 0 {
			below := fmt.Sprintf("p%d", index-1)
			source += "import \"example.com/restored_chain/" + below + "\"\n\n"
			call = " " + below + ".Run() "
		}
		files[name+"/"+name+".go"] = source + "func Run() {" + call + "}\n"
	}
	roots := writeModule(test, "restored_chain", files, "./...")
	identifiers := make([]string, 0, len(roots))
	for _, root := range roots {
		identifiers = append(identifiers, root.ID)
	}
	slices.Sort(identifiers)
	restoredPackages := map[string]bool{identifiers[1]: true, identifiers[3]: true}
	learner := &analysis.Analyzer{
		Name:      "learner",
		Doc:       "learner exports a package fact on each package it runs on and does nothing else",
		FactTypes: []analysis.Fact{new(declaredFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			pass.ExportPackageFact(new(declaredFact))
			return nil, nil
		},
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
	// The path of each package whose fact learner's action on a restored package held as it restored, by package.
	inherited := map[string][]string{}
	graph, analyzeError := Analyze([]*analysis.Analyzer{requiring}, roots, &Options{
		Restored:           func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		FactsBelowRestored: func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		Restore: func(restoring *Action) error {
			restoresMutex.Lock()
			defer restoresMutex.Unlock()
			restores[restoring.Analyzer.Name+"@"+restoring.Package.ID]++
			if restoring.Analyzer == learner {
				for _, fact := range restoring.AllPackageFacts() {
					inherited[restoring.Package.ID] = append(inherited[restoring.Package.ID], fact.Package.Path())
				}
				// What the cache gives a restored package, the fact its own pass once exported, which every
				// package above it then inherits as it would a pass's.
				restoring.RestorePackageFact(new(declaredFact))
			}
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
	for index, identifier := range identifiers {
		// The packages below this one in the chain are those before it, each identifier its package's path.
		if below := identifiers[:index]; restoredPackages[identifier] {
			held := inherited[identifier]
			slices.Sort(held)
			if !slices.Equal(held, below) {
				test.Errorf("learner on %s, restored, held the package facts of %v as it restored, want those below "+
					"it, %v", identifier, held, below)
			}
		}
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
