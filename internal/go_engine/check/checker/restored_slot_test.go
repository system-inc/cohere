package checker

import (
	"slices"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
)

// An action that only restores its package takes no slot of the run's pool, and every action that runs a pass takes
// one (#ss4t5g6): over packages half of which are restored, the slots taken are the passes run, and the restores are
// copied all the same. The skip and the copy ask one question, so a package restored runs no pass and holds no slot,
// and one that is not runs its pass under a slot.
func TestOnlyAnActionThatRunsAPassTakesASlotOfThePool(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	identifiers := make([]string, 0, len(roots))
	for _, root := range roots {
		identifiers = append(identifiers, root.ID)
	}
	slices.Sort(identifiers)
	restoredPackages := map[string]bool{identifiers[0]: true, identifiers[1]: true}
	var passes atomic.Int64
	var restores atomic.Int64
	var slots atomic.Int64
	counted := &analysis.Analyzer{
		Name: "counted",
		Doc:  "counted counts the passes it runs",
		Run: func(*analysis.Pass) (any, error) {
			passes.Add(1)
			return nil, nil
		},
	}
	pool := bounded.NewPool(2)
	pool.Observe(func(change int) {
		if change > 0 {
			slots.Add(1)
		}
	})
	_, analyzeError := Analyze([]*analysis.Analyzer{counted}, roots, &Options{
		Pool:     pool,
		Restored: func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		Restore: func(*Action) error {
			restores.Add(1)
			return nil
		},
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
	if slots.Load() != passes.Load() {
		test.Errorf("the pool granted %d slots for %d passes and %d restores, want one slot a pass and none a restore",
			slots.Load(), passes.Load(), restores.Load())
	}
}
