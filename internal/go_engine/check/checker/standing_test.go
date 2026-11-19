package checker

import (
	"fmt"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// standingKey stands in for the engine's key, rule.Standing, an analyzer that is never run.
var standingKey = &analysis.Analyzer{
	Name: "standing_key",
	Doc:  "keys a package's standing bodies and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("the key ran on %s", pass.Pkg.Path())
	},
}

// standingOf is the standing bodies answered for one package, which holds only the ID it was answered for.
type standingOf struct {
	packageId string
}

// Stands answers that nothing stands.
func (standingOf) Stands(string, int) bool { return false }

// Confines answers that no rule is confined.
func (standingOf) Confines(string) bool { return false }

// Findings answers that the host holds no findings.
func (standingOf) Findings(string, string) ([]rule.InheritedFinding, bool) { return nil, false }

// Took records nothing.
func (standingOf) Took(string, string, int, int) {
	// The checker's test reads what a pass holds, never a take.
}

// heldStanding is an analyzer that records, by its pass's package path, what the pass holds under standingKey, and
// the record it fills: nothing for a pass that holds nothing there.
func heldStanding() (*analysis.Analyzer, *sync.Map) {
	held := &sync.Map{}
	return &analysis.Analyzer{
		Name: "held_standing",
		Doc:  "held_standing records what its pass holds under the standing key",
		Run: func(pass *analysis.Pass) (any, error) {
			if standing, present := pass.ResultOf[standingKey]; present {
				held.Store(pass.Pkg.Path(), standing)
			}
			return nil, nil
		},
	}, held
}

// Under Options.StandingBodies every pass on a package holds, under Options.Standing, what the option answers for that
// package's ID, its own and no other's, and a pass on a package the option answers nil for holds nothing there, no
// nil under the key either. Without the option no pass holds anything under the key.
func TestAPassHoldsItsOwnPackagesStandingBodies(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	// The first package is the one nothing stands in.
	bare := roots[0].ID
	recorder, held := heldStanding()
	graph, analyzeError := Analyze([]*analysis.Analyzer{recorder}, roots, &Options{
		StandingBodies: func(packageId string) rule.StandingBodies {
			if packageId == bare {
				return nil
			}
			return standingOf{packageId: packageId}
		},
		Standing: standingKey,
	})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	for action := range graph.All() {
		if action.Err != nil {
			test.Fatalf("%s failed: %v", action, action.Err)
		}
	}
	for _, root := range roots {
		standing, present := held.Load(root.PkgPath)
		if root.ID == bare {
			if present {
				test.Errorf("the pass on %s, which the option answers nil for, holds %v under Standing", root.ID,
					standing)
			}
			continue
		}
		if standing != (standingOf{packageId: root.ID}) {
			test.Errorf("the pass on %s holds %v under Standing, want what the option answers for its own ID",
				root.ID, standing)
		}
	}

	unsetRecorder, unsetHeld := heldStanding()
	_, unsetError := Analyze([]*analysis.Analyzer{unsetRecorder}, roots, &Options{Standing: standingKey})
	if unsetError != nil {
		test.Fatalf("analyzing without the option: %v", unsetError)
	}
	unsetHeld.Range(func(path any, standing any) bool {
		test.Errorf("without the option the pass on %v holds %v under Standing", path, standing)
		return true
	})
}
