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
// compiles, one only the other build does, and file, which both compile and which holds leaves, a body the other
// build's difference never reaches that the row takes out, and stands, one it leaves standing. named is how many
// objects the row names in the other build: none for an edge row, whose bodies leave by themselves. The other build is
// windows's in every module but presence-requireparallel's and presence-caller's, whose is the tag presence_b's: a
// subtest's signature, and a caller's that testinggoroutine hears, holds *testing.T, whose type each platform declares
// otherwise, so in windows's build the reach lints every such body already. Every module reads no type a platform
// declares otherwise where its bodies could see one, for the same reason. presence-caller and presence-owning take
// their bodies out by the host's presence record: Start starts helper, which the tagged build reaches, and Loose
// writes interface{} of its own. The two presence-marker modules take out the empty method a marker's name declares,
// whose finding hangs on a selection only one build compiles: windows's alone in -win, the host's alone in -host.
type standingPresence struct {
	module string
	row    standing_presence.Row
	file   string
	leaves string
	stands string
	named  int
}

// standingPresences are the presence rows' modules, one a row.
var standingPresences = []standingPresence{
	{module: "presence-hostport", row: standing_presence.HostPort, file: "shared.go", leaves: "Dial", stands: "Port",
		named: 1},
	{module: "presence-scannererr", row: standing_presence.ScannerError, file: "shared.go", leaves: "Lines",
		stands: "Port", named: 1},
	{module: "presence-sqlrowserr", row: standing_presence.SqlRowsError, file: "shared.go", leaves: "Next",
		stands: "Port", named: 1},
	{module: "presence-screaming", row: standing_presence.Screaming, file: "shared.go", leaves: "Reads",
		stands: "Port", named: 1},
	{module: "presence-emptyfunction", row: standing_presence.EmptyFunction, file: "shared.go", leaves: "Check",
		stands: "Port", named: 1},
	{module: "presence-noany", row: standing_presence.NoAny, file: "shared.go", leaves: "Build", stands: "Port",
		named: 1},
	{module: "presence-testinggoroutine", row: standing_presence.TestingGoroutine, file: "shared.go",
		leaves: "helper", stands: "Port"},
	{module: "presence-requireparallel", row: standing_presence.RequireParallel, file: "shared_test.go",
		leaves: "subtest", stands: "count"},
	{module: "presence-caller", row: standing_presence.TestingGoroutine, file: "shared.go", leaves: "Start",
		stands: "Port"},
	{module: "presence-owning", row: standing_presence.NoAny, file: "shared.go", leaves: "Loose", stands: "Port"},
	{module: "presence-marker-win", row: standing_presence.EmptyFunction, file: "shared.go", leaves: "Circle.isShape",
		stands: "Port"},
	{module: "presence-marker-host", row: standing_presence.EmptyFunction, file: "shared.go",
		leaves: "Circle.isShape", stands: "Port", named: 1},
}

// leftFor are the reasons the facts' settlement of run took the declaration named name, in file, out of the standing
// set for, and those presence took it out by itself for, in any package of the build: none for a body that stands or
// that the reach lints already.
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
			for _, left := range packageSettled.PresenceLeft {
				if left.Place == place {
					reasons = append(reasons, left.Reason)
				}
			}
		}
	}
	return reasons
}

// A standing body a row reads leaves the standing set in the other build, so the row's rule reads it there once
// standing bodies are stripped (#xeafx79): over each presence module, the reach leaves both of its file's bodies
// unreached, and presence takes the one the row reads out, with the row's reason alone, while the other stands.
// Nothing is stripped yet, so the findings with the switch on are those with it off.
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
			if reasons := leftFor(other, presence.leaves, presence.file); !slices.Equal(reasons,
				[]fact_reach.Reason{presence.row.Reason()}) {
				test.Errorf("%s left the other build's standing set for %q, want for %q alone", presence.leaves,
					reasons, presence.row.Reason())
			}
			if stoodIn(other, presence.leaves, presence.file) {
				test.Errorf("%s stands in the other build, though it names %s's candidate", presence.leaves,
					presence.row)
			}
			if !stoodIn(other, presence.stands, presence.file) {
				test.Errorf("%s does not stand in the other build, so the module holds nothing that stays",
					presence.stands)
			}
			if other.factReach == nil || other.factReach.presenceNamed[string(presence.row)] != presence.named ||
				other.factReach.presenceRemoved[string(presence.row)] != 1 {
				test.Errorf("the other build's census of the facts' reach is %+v, want %d objects named and one body "+
					"taken out for %s", other.factReach, presence.named, presence.row)
			}
			if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
				test.Errorf("the findings with the switch on are\n%s\nwant those with it off\n%s",
					strings.Join(findingLines(onStream), "\n"), strings.Join(findingLines(offStream), "\n"))
			}
		})
	}
}
