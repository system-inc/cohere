package parity

import (
	"cmp"
	"fmt"
	"go/build"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// on386 is a 32-bit Linux target, the one each targeted fixture's narrow.go is built for, and onAmd64 a 64-bit one,
// which builds the targeted fixture's wide.go instead.
var (
	on386   = On("GOOS=linux", "GOARCH=386", "CGO_ENABLED=0")
	onAmd64 = On("GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
)

// Each target builds its own file of targeted and no other: for 386 both sides find the call in narrow.go, and for
// amd64 the original alone finds the one in wide.go.
func TestFindingsLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	originalFindings, portFindings := on386.Findings(test, original, port, analysistest.TestData(), "targeted")
	wanted := []Finding{{File: "targeted/narrow.go", Line: 6, Column: 2, Message: "call to shared"}}
	if !reflect.DeepEqual(originalFindings, wanted) || !reflect.DeepEqual(portFindings, wanted) {
		test.Errorf("for 386 the original found %v and the port %v, want %v from each", originalFindings,
			portFindings, wanted)
	}
	originalFindings, portFindings = onAmd64.Findings(test, original, port, analysistest.TestData(), "targeted")
	wantedOriginal := []Finding{{File: "targeted/wide.go", Line: 6, Column: 2, Message: "call to lost"}}
	if !reflect.DeepEqual(originalFindings, wantedOriginal) || len(portFindings) != 0 {
		test.Errorf("for amd64 the original found %v and the port %v, want %v and nothing", originalFindings,
			portFindings, wantedOriginal)
	}
}

// Each Require call that passes for 386 fails under the machine's own target, never 386 where these tests run, so
// what passes is the target's load: the two sides agree there and differ on the machine's own.
func TestRequireIdenticalLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireIdentical(recording, original, port, analysistest.TestData(), "targeted")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, original, port, analysistest.TestData(), "targeted")
	}), "the original found targeted/wide.go:6:2: call to lost, and the port did not")
}

// The note is on narrow.go, so the machine's own target builds no noted file.
func TestRequireStaticcheckWantsLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireStaticcheckWants(recording, port, analysistest.TestData(), "targeted")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckWants(recording, port, analysistest.TestData(), "targeted")
	}), "the fixtures hold no note, so the comparison proves nothing")
}

// The record is read whichever target builds its file, so on the machine's own target it is unmatched.
func TestRequireOnlyAdjudicatedLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "targeted_adjudicated")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "targeted_adjudicated")
	}), "the fixture records that the port alone finds targeted_adjudicated/narrow.go:7:0: call to gained, and it "+
		"does not")
}

// The golden file is narrow.go's, so the machine's own target holds none.
func TestRequireStaticcheckGoldenLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "targeted_golden")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "targeted_golden")
	}), "the fixtures hold no golden file, so the comparison proves nothing")
}

// The call the original panics on is in narrow.go, so on the machine's own target the original runs and the record
// there is unmatched.
func TestRequirePortWhereOriginalPanicsLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequirePortWhereOriginalPanics(recording, panicking(originalMessages), port, analysistest.TestData(),
			"targeted_panicking")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), port, analysistest.TestData(),
			"targeted_panicking")
	}),
		"the original no longer panics on 1 of the 1 packages of targeted_panicking",
		"the fixture records that the port finds targeted_panicking/narrow.go:8:0: call to gained, and it does not",
	)
}

// A target that sets what a fixture load sets itself is refused, the tree's `GOPATH` and a module's `GOPROXY`, and so
// is a variable with no value.
func TestTargetRefusesAVariableTheLoadSets(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOPATH=/elsewhere").Findings(recording, original, port, analysistest.TestData(), "same")
	}), "the target sets GOPATH, which the load of [same] sets itself")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOPROXY=direct").Findings(recording, original, port, analysistest.TestData(), "module_one")
	}), "the target sets GOPROXY, which the load of the module module_one sets itself")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOARCH").Findings(recording, original, port, analysistest.TestData(), "same")
	}), `the target's "GOARCH" names no value`)
}

// judging is an analyzer that reports, at each package's clause, the target go/build's default context names while it
// runs, as asmdecl and framepointer read the target they judge there, and, where its call does not hold the default
// context as it must, that too: a call that sets it, isSetting, must hold it to write, so no read hold can be taken
// beside it, and any other to read, so no write hold can. A hold it takes to try is let go at once. Each call makes a
// new one, since the checker tells the two sides of a pair apart by their analyzers.
func judging(isSetting bool) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "judging",
		Doc:  "judging reports the target go/build's default context names.",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				pass.Reportf(file.Name.Pos(), "judged for %s/%s", build.Default.GOOS, build.Default.GOARCH)
				if isSetting && defaultContext.TryRLock() {
					defaultContext.RUnlock()
					pass.Reportf(file.Name.Pos(), "the call sets the default context and does not hold it to write")
				}
				if !isSetting && defaultContext.TryLock() {
					defaultContext.Unlock()
					pass.Reportf(file.Name.Pos(), "the call does not hold the default context to read")
				}
			}
			return nil, nil
		},
	}
}

// machineTarget is the target go/build's default context names before any test runs, the machine's own unless the
// environment named another, read while the package is initialized, so no harness call can have set it.
var machineTarget = build.Default.GOOS + "/" + build.Default.GOARCH

// A Target that names `GOOS` and `GOARCH` sets them in go/build's default context for its call, so a check that reads
// its target there judges the one named, and the zero Target leaves the machine's own. Every target runs beside the
// others, so each finds its own only if no call reads a default context another call has set.
func TestTargetSetsTheDefaultContextForItsRun(test *testing.T) {
	test.Parallel()
	for _, architecture := range []string{"386", "amd64", "arm64", "riscv64", ""} {
		test.Run(cmp.Or(architecture, "the machine's own"), func(subtest *testing.T) {
			subtest.Parallel()
			target, judged := On("GOOS=linux", "GOARCH="+architecture, "CGO_ENABLED=0"), "linux/"+architecture
			if architecture == "" {
				target, judged = Target{}, machineTarget
			}
			isSetting := architecture != ""
			originalFindings, portFindings := target.Findings(subtest, judging(isSetting), judging(isSetting),
				analysistest.TestData(), "same")
			wanted := []Finding{{File: "same/same.go", Line: 2, Column: 9, Message: "judged for " + judged}}
			if !reflect.DeepEqual(originalFindings, wanted) || !reflect.DeepEqual(portFindings, wanted) {
				subtest.Errorf("the original found %v and the port %v, want %v from each", originalFindings,
					portFindings, wanted)
			}
		})
	}
}

// A Target puts go/build's default context back when its call ends, and when the call fails too.
//
// Not parallel: it reads go/build's default context outside a harness call, which no other test may set meanwhile.
func TestTargetPutsTheDefaultContextBack(test *testing.T) {
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.Findings(recording, judging(true), judging(true), analysistest.TestData(), "same")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.Findings(recording, judging(true), judging(true), analysistest.TestData(), "absent")
	}), "fixture package absent does not load clean")
	if judged := build.Default.GOOS + "/" + build.Default.GOARCH; judged != machineTarget {
		test.Errorf("after the calls go/build's default context names %s, want %s", judged, machineTarget)
	}
}

// A call on a Target that sets go/build's default context holds it to write while it runs, and a call on the zero
// Target holds it to read, each proven inside the run, where judging tries the hold the call must keep out: a read
// hold beside the first and a write hold beside the second.
//
// Not parallel: a try fails whenever another test's call holds the default context, so only run alone does each
// try that succeeds say the call itself does not hold it.
func TestEveryCallHoldsTheDefaultContext(test *testing.T) {
	for _, call := range []struct {
		target    Target
		isSetting bool
		judged    string
	}{
		{on386, true, "linux/386"},
		{Target{}, false, machineTarget},
	} {
		originalFindings, portFindings := call.target.Findings(test, judging(call.isSetting),
			judging(call.isSetting), analysistest.TestData(), "same")
		wanted := []Finding{{File: "same/same.go", Line: 2, Column: 9, Message: "judged for " + call.judged}}
		if !reflect.DeepEqual(originalFindings, wanted) || !reflect.DeepEqual(portFindings, wanted) {
			test.Errorf("the original found %v and the port %v, want %v from each", originalFindings, portFindings,
				wanted)
		}
	}
}

// A target that names a variable twice is refused, since which value counts would be the order's accident, and so is
// a `GOOS` or `GOARCH` that is empty, a target go/build's default context cannot judge by.
func TestTargetRefusesATargetTheDefaultContextCannotTake(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOOS=linux", "GOARCH=386", "GOARCH=amd64").Findings(recording, original, port, analysistest.TestData(),
			"same")
	}), `the target names GOARCH twice, as "GOARCH=386" and "GOARCH=amd64"`)
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOOS=linux", "GOARCH=").Findings(recording, original, port, analysistest.TestData(), "same")
	}), "the target's GOARCH is empty, and go/build's default context judges by no empty target")
}

// Calls on a target that sets go/build's default context run beside plain calls, each holding it once, so none waits
// forever on a hold another took, and each passes as it does alone. A load by hand here would read build.Default
// outside the harness, racing the target's calls, as the Target doc warns.
func TestTargetCallsRunBesidePlainCalls(test *testing.T) {
	test.Parallel()
	calls := map[string]func(recording testing.TB){
		"on 386": func(recording testing.TB) {
			on386.RequireIdentical(recording, original, port, analysistest.TestData(), "targeted")
		},
		"plain": func(recording testing.TB) {
			RequireIdentical(recording, original, port, analysistest.TestData(), "same")
		},
		"upstream": func(recording testing.TB) {
			RequireUpstreamWants(recording, port, analysistest.TestData(), "wants")
		},
	}
	for round := range 3 {
		for name, call := range calls {
			test.Run(fmt.Sprintf("%s %d", name, round), func(subtest *testing.T) {
				subtest.Parallel()
				requireFailures(subtest, failuresOf(subtest, call))
			})
		}
	}
}

// JudgingBy sets go/build's default context apart from the load: loaded for 386, targeted hands over narrow.go, which
// only 386 builds, while the check reads amd64, and the call holds the context to write as one that sets it by On does.
// The zero Target judging by amd64 loads for the machine's own, handing over wide.go.
func TestJudgingBySetsTheDefaultContextApartFromTheLoad(test *testing.T) {
	test.Parallel()
	originalFindings, portFindings := on386.JudgingBy("GOARCH=amd64").Findings(test, judging(true), judging(true),
		analysistest.TestData(), "targeted")
	wanted := []Finding{
		{File: "targeted/narrow.go", Line: 3, Column: 9, Message: "judged for linux/amd64"},
		{File: "targeted/targeted.go", Line: 4, Column: 9, Message: "judged for linux/amd64"},
	}
	if !reflect.DeepEqual(originalFindings, wanted) || !reflect.DeepEqual(portFindings, wanted) {
		test.Errorf("the original found %v and the port %v, want %v from each", originalFindings, portFindings,
			wanted)
	}
	originalFindings, portFindings = Target{}.JudgingBy("GOOS=plan9", "GOARCH=sparc64").Findings(test,
		judging(true), judging(true), analysistest.TestData(), "targeted")
	wanted = []Finding{
		{File: "targeted/targeted.go", Line: 4, Column: 9, Message: "judged for plan9/sparc64"},
		{File: "targeted/wide.go", Line: 3, Column: 9, Message: "judged for plan9/sparc64"},
	}
	if !reflect.DeepEqual(originalFindings, wanted) || !reflect.DeepEqual(portFindings, wanted) {
		test.Errorf("on the machine's own the original found %v and the port %v, want %v from each",
			originalFindings, portFindings, wanted)
	}
}

// JudgingBy is refused what On is, a variable with no value, named twice or empty, and any variable but `GOOS` and
// `GOARCH`, which the default context holds no other of for a check.
func TestJudgingByRefusesWhatTheDefaultContextCannotTake(test *testing.T) {
	test.Parallel()
	for _, refused := range []struct {
		variables []string
		failure   string
	}{
		{[]string{"GOARCH"}, `the target's "GOARCH" names no value`},
		{[]string{"GOARCH=amd64", "GOARCH=arm64"}, `the target names GOARCH twice`},
		{[]string{"GOARCH="}, "the target's GOARCH is empty"},
		{[]string{"CGO_ENABLED=0"}, "the target judges by CGO_ENABLED, and go/build's default context holds only " +
			"GOOS and GOARCH for a check to read"},
	} {
		requireFailures(test, failuresOf(test, func(recording testing.TB) {
			on386.JudgingBy(refused.variables...).Findings(recording, original, port, analysistest.TestData(), "same")
		}), refused.failure)
	}
}
