package go_language

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules"
	"github.com/system-inc/cohere/internal/go_language/rules/standing_facts"
	"github.com/system-inc/cohere/internal/go_language/rules/unused_code"
)

// heldSplices are what U1000 said it did with the standing bodies of each package it ran on, over every build of a
// run, as the splice probe read them: none for a pass with no standing answer, as the host's is.
type heldSplices struct {
	lock    sync.Mutex
	spliced []unused_code.SplicedBodies
}

// spliceProbe is a house rule of its own that requires U1000 and keeps what U1000's result on each package says of
// the bodies it spliced (unused_code.SplicedOf), in the heldSplices it returns beside it. It reports nothing, so a
// run's findings are the same with it or without it.
func spliceProbe() (rule.Rule, *heldSplices) {
	held := &heldSplices{}
	analyzer := &analysis.Analyzer{
		Name:     "probe_splice",
		Doc:      "keeps what U1000 spliced on each package",
		Requires: []*analysis.Analyzer{unused_code.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			if spliced, isSpliced := unused_code.SplicedOf(pass.ResultOf[unused_code.Analyzer]); isSpliced {
				held.lock.Lock()
				defer held.lock.Unlock()
				held.spliced = append(held.spliced, spliced)
			}
			return nil, nil
		},
	}
	return rule.Rule{Name: rule.Namespace + "correctness-probe-splice", Origin: rule.OriginHouse,
		Analyzer: analyzer}, held
}

// runSplicedGate is runStandingGate over the standing gate module named with the splice probe beside the registry's
// rules, and returns the pipeline, its stream, normalized, the copy's root, and what the probe held.
func runSplicedGate(
	test *testing.T,
	module string,
	switched bool,
) (*pipeline, string, string, []unused_code.SplicedBodies) {
	test.Helper()
	probe, held := spliceProbe()
	root := copyFixture(test, filepath.Join("testdata", "standing-gates", module))
	engine, stream, _ := runOverRegistered(test, root, exitFindings, append(rules.All(), probe),
		func(engine *pipeline) {
			engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
			engine.standingBodies = switched
		}, "--no-cache", "--no-fix", "--build-dependence")
	held.lock.Lock()
	defer held.lock.Unlock()
	return engine, normalize(stream, root), root, slices.Clone(held.spliced)
}

// unusedFindings are run's own U1000 findings, before the merge, sorted, with root written /project.
func unusedFindings(run buildRun, root string) []string {
	return slices.DeleteFunc(rawFindings(run, root), func(finding string) bool {
		return !strings.Contains(finding, "Rule:"+rule.Namespace+"U1000 ")
	})
}

// On the census path with the standing bodies' switch on, the host records Answer's body and windows's build, where
// Answer stands, splices the host's record of it in place of walking it (#xeafx79): the one splice any pass of the
// run says it made, once the record differential found windows's own record of the body to be the host's, end for
// end, no record differing. Answer's is the body U1000's verdict hangs on, the field group reported only while
// something adds the body's edges, so windows's own U1000 findings, spliced, are those it finds walking with the
// switch off, and the merged findings are the same either way. With the switch off nothing stands, and no pass says
// it spliced.
func TestAStandingBodyU1000NeedsIsSplicedFromTheHostsRecord(test *testing.T) {
	test.Parallel()
	off, offStream, offRoot, offSpliced := runSplicedGate(test, "unused-splice", false)
	on, onStream, onRoot, onSpliced := runSplicedGate(test, "unused-splice", true)
	if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
		test.Fatalf("want the host's build and windows's in both runs, got %d and %d runs", len(off.buildRuns),
			len(on.buildRuns))
	}
	if !stoodIn(on.buildRuns[1], "Answer", "shared.go") {
		test.Fatal("Answer does not stand in windows's build, so the module splices nothing")
	}
	if len(offSpliced) != 0 {
		test.Errorf("with the switch off U1000 says it did %+v with standing bodies, want nothing", offSpliced)
	}
	if !slices.Equal(onSpliced, []unused_code.SplicedBodies{{Spliced: 1}}) {
		test.Errorf("with the switch on U1000 says it did %+v with standing bodies, want Answer's spliced in one pass "+
			"and no record differing from the build's own", onSpliced)
	}
	walked, spliced := unusedFindings(off.buildRuns[1], offRoot), unusedFindings(on.buildRuns[1], onRoot)
	if !slices.ContainsFunc(walked, func(finding string) bool {
		return strings.Contains(finding, "field group is unused")
	}) {
		test.Fatalf("windows's U1000 findings walking are\n%s\nwhich report no field group",
			strings.Join(walked, "\n"))
	}
	if !slices.Equal(spliced, walked) {
		test.Errorf("windows's U1000 findings spliced are\n%s\nwant those walking\n%s", strings.Join(spliced, "\n"),
			strings.Join(walked, "\n"))
	}
	if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
		test.Errorf("the findings with the switch on are\n%s\nwant those with it off\n%s",
			strings.Join(findingLines(onStream), "\n"), strings.Join(findingLines(offStream), "\n"))
	}
}

// recordLeavings are the reasons the facts' settlement of run took the declaration named name, in file, out of the
// standing set for its U1000 record, in any package of the build, and how many bodies each package's settlement took
// out for a record, summed by why.
func recordLeavings(
	run buildRun,
	name string,
	file string,
) ([]fact_reach.Reason, map[unused_code.RecordUse]int) {
	var reasons []fact_reach.Reason
	removed := map[unused_code.RecordUse]int{}
	for _, settled := range run.result.FactReachSettled {
		packageSettled, isResult := settled.(*standing_facts.Result)
		if !isResult {
			continue
		}
		for use, count := range packageSettled.RecordRemoved {
			removed[use] += count
		}
		for _, body := range run.bodyReach.bodies {
			if body.name != name || filepath.Base(body.file) != file {
				continue
			}
			for _, left := range packageSettled.RecordLeft {
				if left.Place == (fact_reach.Place{File: body.file, Offset: body.offset}) {
					reasons = append(reasons, left.Reason)
				}
			}
		}
	}
	return reasons, removed
}

// On the census path with the standing bodies' switch on, a body the reach leaves standing whose host's record is
// flagged, address's unsafe.Pointer conversion of a named pointer type, leaves the standing set in windows's build
// before U1000 runs (#xeafx79), with U1000's record reason and counted, so no pass walks a standing body for want of
// a record it could take, and U1000 lints address itself. Address, beside it, still stands. windows's own U1000
// findings are those it finds with the switch off, and the merged findings are the same either way.
func TestAStandingBodyWithAFlaggedRecordLeavesTheStandingSet(test *testing.T) {
	test.Parallel()
	off, offStream, offRoot, _ := runSplicedGate(test, "unused-flagged", false)
	on, onStream, onRoot, onSpliced := runSplicedGate(test, "unused-flagged", true)
	if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
		test.Fatalf("want the host's build and windows's in both runs, got %d and %d runs", len(off.buildRuns),
			len(on.buildRuns))
	}
	windows := on.buildRuns[1]
	if !slices.ContainsFunc(windows.bodyReach.bodies, func(body reachedBody) bool {
		return body.name == "address" && filepath.Base(body.file) == "shared.go" && body.reason == ""
	}) {
		test.Fatal("the reach does not leave address unreached in windows's build, so nothing could take it out")
	}
	if !stoodIn(windows, "Address", "shared.go") {
		test.Fatal("Address does not stand in windows's build, so the module leaves nothing standing")
	}
	if stoodIn(windows, "address", "shared.go") {
		test.Error("address stands in windows's build, though its host's record is flagged")
	}
	reasons, removed := recordLeavings(windows, "address", "shared.go")
	if want := []fact_reach.Reason{standing_facts.RecordReason(unused_code.RecordFlagged)}; !slices.Equal(reasons,
		want) {
		test.Errorf("address left for its record with %q, want %q", reasons, want)
	}
	if want := map[unused_code.RecordUse]int{unused_code.RecordFlagged: 1}; !maps.Equal(removed, want) {
		test.Errorf("windows's settlement took out %v for records, want %v", removed, want)
	}
	if !slices.Equal(onSpliced, []unused_code.SplicedBodies{{Spliced: 2}}) {
		test.Errorf("with the switch on U1000 says it did %+v with standing bodies, want Address's and unreferenced's "+
			"spliced in one pass and no body walked for its record", onSpliced)
	}
	plain, settled := unusedFindings(off.buildRuns[1], offRoot), unusedFindings(windows, onRoot)
	if len(plain) == 0 {
		test.Fatal("windows's U1000 finds nothing with the switch off, so its findings compare nothing")
	}
	if !slices.Equal(settled, plain) {
		test.Errorf("windows's U1000 findings with the switch on are\n%s\nwant those with it off\n%s",
			strings.Join(settled, "\n"), strings.Join(plain, "\n"))
	}
	if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
		test.Errorf("the findings with the switch on are\n%s\nwant those with it off\n%s",
			strings.Join(findingLines(onStream), "\n"), strings.Join(findingLines(offStream), "\n"))
	}
}
