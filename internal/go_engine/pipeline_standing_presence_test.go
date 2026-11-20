package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_presence"
)

// standingPresence is a module of the standing gates' shape for one presence row (#xeafx79): a file only the host
// compiles, one only the other build does, and shared.go, which both compile and which declares the row's candidate,
// leaves, a body the other build's difference never reaches that names the candidate, and stands, one that names
// none.
type standingPresence struct {
	module string
	row    standing_presence.Row
	leaves string
	stands string
}

// standingPresences are the presence rows' modules, one a row.
var standingPresences = []standingPresence{
	{module: "presence-hostport", row: standing_presence.HostPort, leaves: "Dial", stands: "Port"},
}

// leftFor are the reasons the facts' settlement of run took the declaration named name, in file, out of the standing
// set for, in any package of the build: none for a body that stands or that the reach lints already.
func leftFor(run buildRun, name string, file string) []fact_reach.Reason {
	var reasons []fact_reach.Reason
	for _, body := range run.bodyReach.bodies {
		if body.name != name || filepath.Base(body.file) != file || body.reason != "" {
			continue
		}
		place := fact_reach.Place{File: body.file, Offset: body.offset}
		for _, settled := range run.result.FactReachSettled {
			packageSettled, isResult := settled.(*standing_facts.Result)
			if !isResult || packageSettled.Settlement == nil {
				continue
			}
			for _, removal := range packageSettled.Settlement.Removed {
				if removal.Place == place {
					reasons = append(reasons, removal.Reason)
				}
			}
		}
	}
	return reasons
}

// A standing body that names a row's candidate leaves the standing set in the other build, so the row's rule reads it
// there once standing bodies are stripped (#xeafx79): over each presence module, the reach leaves both of shared.go's
// bodies unreached, and the facts' settlement takes the one that names the candidate out, with the row's reason alone,
// while the other stands. Nothing is stripped yet, so the findings with the switch on are those with it off.
func TestAStandingBodyThatNamesARowsCandidateLeavesTheStandingSet(test *testing.T) {
	test.Parallel()

	for _, presence := range standingPresences {
		test.Run(presence.module, func(test *testing.T) {
			test.Parallel()
			off, offStream, _ := runStandingGate(test, presence.module, false)
			on, onStream, _ := runStandingGate(test, presence.module, true)
			if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
				test.Fatalf("want the host's build and the other in both runs, got %d and %d runs", len(off.buildRuns),
					len(on.buildRuns))
			}
			other := on.buildRuns[1]
			if reasons := leftFor(other, presence.leaves, "shared.go"); !slices.Equal(reasons,
				[]fact_reach.Reason{presence.row.Reason()}) {
				test.Errorf("%s left the other build's standing set for %q, want for %q alone", presence.leaves,
					reasons, presence.row.Reason())
			}
			if stoodIn(other, presence.leaves, "shared.go") {
				test.Errorf("%s stands in the other build, though it names %s's candidate", presence.leaves,
					presence.row)
			}
			if !stoodIn(other, presence.stands, "shared.go") {
				test.Errorf("%s does not stand in the other build, so the module holds nothing that stays",
					presence.stands)
			}
			if other.factReach == nil || other.factReach.presenceNamed[string(presence.row)] != 1 ||
				other.factReach.presenceRemoved[string(presence.row)] != 1 {
				test.Errorf("the other build's census of the facts' reach is %+v, want one object named and one body "+
					"taken out for %s", other.factReach, presence.row)
			}
			if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
				test.Errorf("the findings with the switch on are\n%s\nwant those with it off\n%s",
					strings.Join(findingLines(onStream), "\n"), strings.Join(findingLines(offStream), "\n"))
			}
		})
	}
}
