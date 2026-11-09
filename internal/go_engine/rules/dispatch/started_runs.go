package dispatch

import (
	"reflect"

	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// startedRuns is the listeners a dispatcher calls over one package, each ready with a context of its own.
type startedRuns struct {
	// runs is each started listener, in the order its rule was registered.
	runs []*listenerRun
	// byKind is, for each kind a started listener lists, the started listeners that list it, in runs' order, so a
	// node or an instruction reaches only the listeners declaring its kind, at the cost of one lookup.
	byKind map[reflect.Type][]*listenerRun
	// notStarted is the outcome of each rule of the run whose listener was not started because something it
	// requires failed on the package.
	notStarted Result
	// timing is the run's collector under `--timing`, which the checker hands the pass under rule.Timed, nil
	// otherwise, and dispatcher is the name of the dispatcher's own analyzer, whose row finish takes the listeners'
	// calls out of.
	timing     *timing.Collector
	dispatcher string
}

// add makes run one of the listeners started, heard for every kind its listener lists.
func (started *startedRuns) add(run *listenerRun) {
	started.runs = append(started.runs, run)
	for _, kind := range run.registered.kinds {
		started.byKind[kind] = append(started.byKind[kind], run)
	}
}
