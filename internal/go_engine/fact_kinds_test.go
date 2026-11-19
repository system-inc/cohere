package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// factKindsSettled is what windows's build settled of the fact-kinds fixture's one package under the body reach census
// and the body differential (#73k3wge): the settlement, each of the package's bodies by name, the build's run and the
// host's, and the census file the run wrote.
type factKindsSettled struct {
	settlement *fact_reach.Settlement
	bodies     map[string]reachedBody
	run        buildRun
	host       buildRun
	censusFile string
}

// settleFactKinds runs the fact-kinds fixture under the census and the body differential, and reads what windows's
// build settled of its package.
func settleFactKinds(test *testing.T) *factKindsSettled {
	test.Helper()
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "fact-kinds")), exitFindings,
		func(engine *pipeline) {
			engine.bodyReachCensus = censusFile
			engine.bodyDifferential = true
		}, "--no-cache", "--no-fix", "--build-dependence")
	if len(on.buildRuns) != 2 || on.buildRuns[1].bodyReach == nil || on.buildRuns[1].result == nil {
		test.Fatalf("want the host's build and windows's census, got %d runs", len(on.buildRuns))
	}
	settled := &factKindsSettled{bodies: map[string]reachedBody{}, run: on.buildRuns[1], host: on.buildRuns[0],
		censusFile: censusFile}
	for _, body := range settled.run.bodyReach.bodies {
		if body.packageIdentifier == "example.com/kinds" {
			settled.bodies[body.name] = body
		}
	}
	result, isResult := settled.run.result.FactReachSettled["example.com/kinds"].(*standing_facts.Result)
	if !isResult || !result.Partial || result.Settlement == nil {
		test.Fatalf("windows settled example.com/kinds as %+v, want it settled in part", result)
	}
	settled.settlement = result.Settlement
	return settled
}

// stands is whether the body named name still stands, which the test fails on when the census lists no such body.
func (settled *factKindsSettled) stands(test *testing.T, name string) bool {
	test.Helper()
	body, isListed := settled.bodies[name]
	if !isListed {
		test.Fatalf("the census lists no body %s", name)
	}
	return settled.settlement.Standing[fact_reach.Place{File: body.file, Offset: body.offset}]
}

// removal is why the body named name left the standing set, and whether it did.
func (settled *factKindsSettled) removal(test *testing.T, name string) (fact_reach.Removal, bool) {
	test.Helper()
	body, isListed := settled.bodies[name]
	if !isListed {
		test.Fatalf("the census lists no body %s", name)
	}
	for _, removal := range settled.settlement.Removed {
		if removal.Place == (fact_reach.Place{File: body.file, Offset: body.offset}) {
			return removal, true
		}
	}
	return fact_reach.Removal{}, false
}

// requireRemovedBy fails unless the body named user left the standing set because of the object named callee, its
// last part of the key by name, for reason.
func (settled *factKindsSettled) requireRemovedBy(
	test *testing.T,
	user string,
	callee string,
	reason fact_reach.Reason,
) {
	test.Helper()
	removal, removed := settled.removal(test, user)
	if !removed || removal.Reason != reason ||
		!slices.Contains(strings.Split(string(removal.Object), "\x00"), callee) {
		test.Errorf("%s left the standing set %t, by %q for %q, want it taken out by %s for %q", user, removed,
			removal.Object, removal.Reason, callee, reason)
	}
}

// The reach review's must 6 (#w72zxtq): Warm discards size's count, a finding only where size is pure, which it is on
// windows alone, since platformSize counts its calls everywhere else. Warm names only size, whose signature is the
// same in every build, so the reach leaves it unreached; standing on the host's findings it would lose windows's.
// The facts' reach asks windows size's purity, recorded under size, which the build lints, finds it differs, and takes
// Warm out, so Warm's finding is the build's own; Warm's own purity, which Warmer asks, then differs in turn and takes
// Warmer out. Nothing stands that the second leg (#adq5ahd) could find differing. With the comparison made always
// equal, Warm stands and the second leg reports its purity: host false, build true.
func TestTheFactsReachTakesOutWarmWhoseCalleesPurityDiffers(test *testing.T) {
	test.Parallel()

	settled := settleFactKinds(test)
	if settled.bodies["Warm"].reason != "" {
		test.Fatalf("the reach reached Warm for %q, want it unreached, so that only the facts can take it out",
			settled.bodies["Warm"].reason)
	}
	settled.requireRemovedBy(test, "Warm", "size", fact_reach.DiffersReason("function_purity"))
	settled.requireRemovedBy(test, "Warmer", "Warm", fact_reach.DiffersReason("function_purity"))

	discards := func(findings []check.Finding) bool {
		return slices.ContainsFunc(findings, func(finding check.Finding) bool {
			return finding.Rule == "cohere-go/SA4017" && finding.Line > settled.bodies["Warm"].line &&
				finding.Line < settled.bodies["Warmer"].line
		})
	}
	if !discards(settled.run.result.Findings) || discards(settled.host.result.Findings) {
		test.Errorf("Warm's discarded call is a finding on windows %t and on the host %t, want windows's alone",
			discards(settled.run.result.Findings), discards(settled.host.result.Findings))
	}
	differential := settled.run.bodyDifferential
	if differential == nil {
		test.Fatal("windows's build ran no body differential")
	}
	if len(differential.standingFailed) > 0 {
		test.Errorf("windows's second leg failed %v, among the differential's failures\n%s\nwant nothing standing "+
			"that differs", differential.standingFailed, strings.Join(differential.failures, "\n"))
	}
}

// For each fact kind a build compares, the user of a callee whose answer differs on windows leaves the standing set,
// taken out by that callee for that kind, and the user of a callee whose answer is the same stands (#73k3wge). The
// no-return fact is compared for every function before any answer is asked; each other kind's answer is asked of the
// callee, which the reach lints since it names a function or type each platform's file declares.
//
// Triviality and the stopping calls are not registered with the resolver (fact_resolver.Register), so the build cannot
// ask either again and reads every answer of theirs as unknown: the users of their equal callees leave too, as
// unknown, which is the sound answer until the build can ask them. The day it can, their stable users stand, and this
// test says so.
func TestEachFactKindTakesOutTheUserOfADifferingCalleeAndLeavesAnEqualOnesStanding(test *testing.T) {
	test.Parallel()

	settled := settleFactKinds(test)
	for _, kind := range []struct {
		name   string
		user   string
		callee string
		reason fact_reach.Reason
		stable string
		// stableCallee names the equal callee that still takes stable out, for reason, where the build cannot ask the
		// kind; empty where stable stands.
		stableCallee string
	}{
		{"no-return", "NoReturnUser", "halt", "no-return", "NoReturnStable", ""},
		{"nil-ness", "NilnessUser", "wrapped", fact_reach.DiffersReason("return_nil_ness"), "NilnessStable", ""},
		{"purity", "Warm", "size", fact_reach.DiffersReason("function_purity"), "PurityStable", ""},
		{"odd slice", "OddUser", "pairs", fact_reach.DiffersReason("odd_slice_argument"), "OddStable", ""},
		{"triviality", "TrivialUser", "typedNil", fact_reach.UnknownReason("function_triviality"), "TrivialStable",
			"typedNilAlways"},
		{"stopping calls", "StoppingUser", "stopVia", fact_reach.UnknownReason("test_stopping_calls"),
			"StoppingStable", "stopAlways"},
	} {
		test.Run(kind.name, func(test *testing.T) {
			// The settlement is only read, by every kind at once.
			test.Parallel()
			if settled.bodies[kind.user].reason != "" {
				test.Fatalf("the reach reached %s for %q, want it unreached", kind.user,
					settled.bodies[kind.user].reason)
			}
			settled.requireRemovedBy(test, kind.user, kind.callee, kind.reason)
			if kind.stableCallee != "" {
				settled.requireRemovedBy(test, kind.stable, kind.stableCallee, kind.reason)
				return
			}
			if !settled.stands(test, kind.stable) {
				removal, _ := settled.removal(test, kind.stable)
				test.Errorf("%s left the standing set, by %q for %q, want it standing", kind.stable, removal.Object,
					removal.Reason)
			}
		})
	}
}

// The order rule's three shapes (#p018fh3), each named by return_nil_ness's order bound before any answer is asked,
// so the bodies that use what it names never stand: a cycle of two above its floor, cycleHead and cycleTail, each the
// other's user and CycleUser cycleHead's; makeFailure, read by viaInstance through its instantiation; and node.fresh,
// read by viaWrapper through its method value's wrapper.
func TestTheOrderRuleNamesEachShapeSoItsUsersNeverStand(test *testing.T) {
	test.Parallel()

	settled := settleFactKinds(test)
	order := fact_reach.Reason("order")
	settled.requireRemovedBy(test, "cycleHead", "cycleTail", order)
	settled.requireRemovedBy(test, "cycleTail", "cycleHead", order)
	settled.requireRemovedBy(test, "CycleUser", "cycleHead", order)
	settled.requireRemovedBy(test, "viaInstance", "makeFailure", order)
	settled.requireRemovedBy(test, "viaWrapper", "fresh", order)
}

// The first leg reads the facts' reach's settlement: a body it took out is the build's to lint, so its findings are
// the build's own and never compared with the host's, only counted, on a line of their own. Five of the fourteen it
// took out differ by their findings, Warm, Warmer, OddUser, TrivialUser and StoppingUser, each a finding that turns on
// a callee's answer, and none of them is a failure or a difference owed to a later stage.
func TestTheFirstLegLeavesTheBodiesTheFactsReachTookOutToTheBuild(test *testing.T) {
	test.Parallel()

	settled := settleFactKinds(test)
	differential := settled.run.bodyDifferential
	if differential == nil {
		test.Fatal("windows's build ran no body differential")
	}
	if !differential.factsSettled || differential.takenOut != len(settled.settlement.Removed) ||
		differential.takenOut != 14 {
		test.Errorf("the first leg read the settlement %t and left %d bodies to the build, want it read and the 14 "+
			"the facts' reach took out, %d", differential.factsSettled, differential.takenOut,
			len(settled.settlement.Removed))
	}
	var lines []string
	lines = append(lines, differential.failures...)
	for _, classed := range differential.classed {
		lines = append(lines, classed...)
	}
	for _, name := range []string{"Warm", "Warmer", "OddUser", "TrivialUser", "StoppingUser"} {
		if slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, " "+name+": ") }) {
			test.Errorf("the first leg compared %s's findings, which the facts' reach left to the build:\n%s", name,
				strings.Join(lines, "\n"))
		}
	}
	written, readError := os.ReadFile(settled.censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	if line := "  taken out by the facts' reach 14"; !slices.Contains(strings.Split(string(written), "\n"), line) {
		test.Errorf("the census file has no line %q:\n%s", line, written)
	}
}
