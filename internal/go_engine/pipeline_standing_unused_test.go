package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_code"
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

// runSplicedGate is runStandingGate over the unused-splice module with the splice probe beside the registry's rules,
// and returns the pipeline, its stream, normalized, the copy's root, and what the probe held.
func runSplicedGate(test *testing.T, switched bool) (*pipeline, string, string, []unused_code.SplicedBodies) {
	test.Helper()
	probe, held := spliceProbe()
	root := copyFixture(test, filepath.Join("testdata", "standing-gates", "unused-splice"))
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
// run says it made. Answer's is the body U1000's verdict hangs on, the field group reported only while something
// adds the body's edges, so windows's own U1000 findings, spliced, are those it finds walking with the switch off,
// and the merged findings are the same either way. With the switch off nothing stands, and no pass says it spliced.
func TestAStandingBodyU1000NeedsIsSplicedFromTheHostsRecord(test *testing.T) {
	test.Parallel()
	off, offStream, offRoot, offSpliced := runSplicedGate(test, false)
	on, onStream, onRoot, onSpliced := runSplicedGate(test, true)
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
		test.Errorf("with the switch on U1000 says it did %+v with standing bodies, want Answer's spliced in one pass",
			onSpliced)
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
