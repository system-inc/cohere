package checker

import (
	"slices"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
)

// An action that only restores its package takes no slot of the run's pool, and every action that runs a pass runs
// under one (#ss4t5g6): over packages half of which are restored, on one worker so the slots held are its own, every
// pass runs with a slot held and every restore with none, the slots granted are at least 1 and at most the passes run,
// since a worker keeps its slot across consecutive passes (#ybjbqm2), and every slot is given back once Analyze
// returns. The skip and the copy ask one question, so a package restored runs no pass and holds no slot, and one that
// is not runs its pass under a slot.
func TestOnlyAnActionThatRunsAPassTakesASlotOfThePool(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	identifiers := make([]string, 0, len(roots))
	for _, root := range roots {
		identifiers = append(identifiers, root.ID)
	}
	slices.Sort(identifiers)
	// Every other package, so on one worker, which takes alike packages by ID, each restore follows a pass, and a slot
	// kept from that pass would show.
	restoredPackages := map[string]bool{identifiers[1]: true, identifiers[3]: true}
	var passes atomic.Int64
	var restores atomic.Int64
	var slots atomic.Int64
	// held is the slots held now, which every one given back brings to 0 once Analyze returns.
	var held atomic.Int64
	// passesUnheld and restoresHeld count the passes run without a slot held and the restores run with one.
	var passesUnheld atomic.Int64
	var restoresHeld atomic.Int64
	counted := &analysis.Analyzer{
		Name: "counted",
		Doc:  "counted counts the passes it runs",
		Run: func(*analysis.Pass) (any, error) {
			passes.Add(1)
			if held.Load() != 1 {
				passesUnheld.Add(1)
			}
			return nil, nil
		},
	}
	pool := bounded.NewPool(2)
	pool.Observe(func(change int) {
		held.Add(int64(change))
		if change > 0 {
			slots.Add(1)
		}
	})
	_, analyzeError := Analyze([]*analysis.Analyzer{counted}, roots, &Options{
		Pool:     pool,
		Restored: func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		Restore: func(*Action) error {
			restores.Add(1)
			if held.Load() != 0 {
				restoresHeld.Add(1)
			}
			return nil
		},
		// One worker, so a slot held while an action runs is that action's own.
		workers: 1,
	})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	if want := int64(len(roots) - len(restoredPackages)); passes.Load() != want {
		test.Fatalf("ran %d passes, want one on each of the %d packages not restored", passes.Load(), want)
	}
	if restores.Load() != int64(len(restoredPackages)) {
		test.Errorf("restored %d actions, want one on each of the %d restored packages", restores.Load(),
			len(restoredPackages))
	}
	if slots.Load() < 1 || slots.Load() > passes.Load() {
		test.Errorf("the pool granted %d slots for %d passes and %d restores, want 1 to one a pass", slots.Load(),
			passes.Load(), restores.Load())
	}
	if passesUnheld.Load() != 0 || restoresHeld.Load() != 0 {
		test.Errorf("%d passes ran without a slot and %d restores with one, want every pass under a slot and no "+
			"restore under one", passesUnheld.Load(), restoresHeld.Load())
	}
	if held.Load() != 0 {
		test.Errorf("%d slots of the pool still held once Analyze returned, want every one given back", held.Load())
	}
}
