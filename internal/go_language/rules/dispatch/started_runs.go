package dispatch

import (
	"go/token"
	"reflect"

	"github.com/system-inc/cohere/internal/go_language/timing"
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
	// inherited is, for each started rule marked SyntaxOnly that inherits a file of the package, those files and the
	// host's findings on them, which its outcome reports after its own (rule.InheritedFiles). Only the runs over the
	// whole package hold them, however the package is split, so each is reported once.
	inherited map[string]inheritedTake
	// standing is, for each started rule confined on a file of the package, those files with their standing spans and
	// the host's findings inside them, which its outcome carries for the rule's action to report in place of its own
	// there (rule.StandingFiles). Nil until a rule takes something, and held only by the runs over the whole package,
	// as inherited is.
	standing map[string]standingTake
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

// notConfinedOn is the runs of started whose rules are not confined on tokenFile, in started's order and by kind as
// started holds its own, which are the runs that hear a function standing there (#e4rd5dw). It holds nothing else of
// started: the walk over the representation calls its runs for such a function, and finishes started itself.
func (started *startedRuns) notConfinedOn(tokenFile *token.File) *startedRuns {
	hearing := &startedRuns{byKind: map[reflect.Type][]*listenerRun{}}
	for _, run := range started.runs {
		if len(run.standing[tokenFile]) == 0 {
			hearing.add(run)
		}
	}
	return hearing
}
