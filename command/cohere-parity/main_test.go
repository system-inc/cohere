package main

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/passes/nilness"

	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_ness"
)

// moduleRoot is cohere's root, from this package's directory, where the tests run.
const moduleRoot = "../.."

// nilnessFixtures are the nilness port's fixture packages, loaded as packages of cohere's module, as the census loads
// a tree. Each is named, since a wildcard never matches a package below testdata.
var nilnessFixtures = []string{
	"./internal/go_engine/rules/nil_ness/testdata/src/a",
	"./internal/go_engine/rules/nil_ness/testdata/src/b",
	"./internal/go_engine/rules/nil_ness/testdata/src/c",
	"./internal/go_engine/rules/nil_ness/testdata/src/d",
	"./internal/go_engine/rules/nil_ness/testdata/src/parity",
	"./internal/go_engine/rules/nil_ness/testdata/src/differences",
}

// Over the nilness port's fixtures the census finds what the port's tests find: x/tools' fixtures and parity's
// agree, 133 against 133, and the differences fixture's six are the port's alone, so the run is no regression.
func TestCensusOverTheFixturesFindsWhatTheTestsFind(test *testing.T) {
	test.Parallel()
	standardOutput := &strings.Builder{}
	standardError := &strings.Builder{}
	arguments := append([]string{"--rule", "nilness", moduleRoot}, nilnessFixtures...)
	exitCode := run(arguments, standardOutput, standardError)
	if exitCode != exitAgreed {
		test.Fatalf("the census exited %d, want %d: %s%s", exitCode, exitAgreed, standardOutput, standardError)
	}
	report := standardOutput.String()
	test.Log(report)
	for _, wanted := range []string{
		"0 analyzed packages did not load clean\n",
		"nilness: the original found 133, the port 139\n",
		"  the port alone: internal/go_engine/rules/nil_ness/testdata/src/differences/differences.go:15:",
		"  the original did not run on 0 packages\n",
		"  the port did not run on 0 packages\n",
	} {
		if !strings.Contains(report, wanted) {
			test.Errorf("the census does not say %q", wanted)
		}
	}
	if strings.Contains(report, "unusedwrite") {
		test.Error("the census ran unusedwrite, which --rule did not name")
	}
}

// With a pair's sides swapped, the differences fixture's six findings are the original's alone: a regression, which
// the census names finding by finding and exits on.
func TestCensusExitsOnAFindingTheOriginalMakesAlone(test *testing.T) {
	test.Parallel()
	swapped := []parity.Pair{{Name: "swapped", Original: nil_ness.Analyzer, Port: nilness.Analyzer}}
	report, regressions, censusError := census(
		swapped, moduleRoot, []string{"./internal/go_engine/rules/nil_ness/testdata/src/differences"},
	)
	if censusError != nil {
		test.Fatal(censusError)
	}
	if regressions != 6 {
		test.Errorf("the census counts %d regressions, want 6:\n%s", regressions, report)
	}
	for _, wanted := range []string{
		"swapped: the original found 6, the port 0\n",
		"  the original alone: internal/go_engine/rules/nil_ness/testdata/src/differences/differences.go:15:",
		"the originals found 6 findings the ports did not\n",
	} {
		if !strings.Contains(report, wanted) {
			test.Errorf("the census does not say %q:\n%s", wanted, report)
		}
	}
}

// A load that matched nothing is refused, since two empty sides agree and prove nothing.
func TestCensusRefusesPatternsThatMatchNothing(test *testing.T) {
	test.Parallel()
	_, _, censusError := census(pairs, moduleRoot, []string{"github.com/system-inc/cohere/no_such_directory/..."})
	if censusError == nil || !strings.Contains(censusError.Error(), "matched no packages") {
		test.Errorf("a load of nothing gave %v, want it refused", censusError)
	}
}

// A package of the tree that does not type-check is named, with its error, and fails nothing: each side's not
// running on it is counted per side.
func TestCensusNamesAPackageThatDoesNotLoadClean(test *testing.T) {
	test.Parallel()
	report, regressions, censusError := census(pairs, moduleRoot,
		[]string{"./internal/go_engine/check/parity/testdata/src/illtyped"})
	if censusError != nil {
		test.Fatal(censusError)
	}
	if regressions != 0 {
		test.Errorf("the census counts %d regressions over a package neither side ran on:\n%s", regressions, report)
	}
	for _, wanted := range []string{
		"1 analyzed packages did not load clean\n",
		"  github.com/system-inc/cohere/internal/go_engine/check/parity/testdata/src/illtyped: ",
		"illtyped.go:11",
		"  the original did not run on 1 packages\n",
		"  the port did not run on 1 packages\n",
	} {
		if !strings.Contains(report, wanted) {
			test.Errorf("the census does not say %q:\n%s", wanted, report)
		}
	}
}

func TestParseArgumentsRefusesAnUnknownRule(test *testing.T) {
	test.Parallel()
	standardError := &strings.Builder{}
	exitCode := run([]string{"--rule", "nilnes", moduleRoot}, &strings.Builder{}, standardError)
	if exitCode != exitFailed {
		test.Errorf("an unknown rule exited %d, want %d", exitCode, exitFailed)
	}
	if !strings.Contains(standardError.String(), `no pair is named "nilnes"; the pairs are nilness, unusedwrite`) {
		test.Errorf("an unknown rule says %q", standardError.String())
	}
}

func TestParseArgumentsDefaultsToEveryPairOverTheWholeTree(test *testing.T) {
	test.Parallel()
	asked, parseError := parseArguments([]string{moduleRoot})
	if parseError != nil {
		test.Fatal(parseError)
	}
	if len(asked.pairs) != len(pairs) || asked.directory != moduleRoot || strings.Join(asked.patterns, " ") != "./..." {
		test.Errorf("a bare directory asks for %+v, want every pair over ./... of it", asked)
	}
}
