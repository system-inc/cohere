package main

import (
	"path/filepath"
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

// nilnessPair is the census's nilness pair with the gains file at gains, relative to cohere's root.
func nilnessPair(gains string) []censusPair {
	return []censusPair{{pair: parity.Pair{Name: "nilness", Original: nilness.Analyzer, Port: nil_ness.Analyzer},
		gains: gains}}
}

// differencesFixture is the nilness port's differences fixture, as a tree's one package.
var differencesFixture = []string{"./internal/go_engine/rules/nil_ness/testdata/src/differences"}

// Over the nilness port's fixtures the census finds what the port's tests find: x/tools' fixtures and parity's
// agree, 133 against 133, and the differences fixture's six are the port's alone. Its gains file records none of
// them, since they are fixtures, not a tree's, so each is an unexplained gain the census exits on, and the file's own
// gains are listed as not seen.
func TestCensusOverTheFixturesFindsWhatTheTestsFind(test *testing.T) {
	test.Parallel()
	standardOutput := &strings.Builder{}
	standardError := &strings.Builder{}
	arguments := append([]string{"--rule", "nilness", moduleRoot}, nilnessFixtures...)
	exitCode := run(arguments, standardOutput, standardError)
	if exitCode != exitRegression {
		test.Fatalf("the census exited %d, want %d: %s%s", exitCode, exitRegression, standardOutput, standardError)
	}
	report := standardOutput.String()
	test.Log(report)
	for _, wanted := range []string{
		"0 analyzed packages did not load clean\n",
		"nilness: original 133, port 139, gains 0, unexplained 6\n",
		"  unexplained gain: internal/go_engine/rules/nil_ness/testdata/src/differences/differences.go:15:9: ",
		"  gain not seen: go/printer/nodes.go:613:13: impossible condition: nil != nil\n",
		"  the original did not run on 0 packages\n",
		"  the port did not run on 0 packages\n",
	} {
		if !strings.Contains(report, wanted) {
			test.Errorf("the census does not say %q", wanted)
		}
	}
	if strings.Contains(report, "the original alone") || !strings.Contains(report,
		"the ports found 6 findings no recorded gain explains\n") {
		test.Error("the census does not count the six as unexplained gains alone")
	}
	if strings.Contains(report, "unusedwrite") {
		test.Error("the census ran unusedwrite, which --rule did not name")
	}
}

// With a pair's sides swapped, the differences fixture's six findings are the original's alone: a regression, which
// the census names finding by finding and exits on.
func TestCensusExitsOnAFindingTheOriginalMakesAlone(test *testing.T) {
	test.Parallel()
	swapped := []censusPair{{pair: parity.Pair{Name: "swapped", Original: nil_ness.Analyzer, Port: nilness.Analyzer}}}
	result, censusError := census(swapped, moduleRoot, moduleRoot, differencesFixture)
	if censusError != nil {
		test.Fatal(censusError)
	}
	report := result.report
	if result.regressions != 6 || result.unexplained != 0 {
		test.Errorf("the census counts %d regressions and %d unexplained gains, want 6 and 0:\n%s",
			result.regressions, result.unexplained, report)
	}
	for _, wanted := range []string{
		"swapped: original 6, port 0, gains 0, unexplained 0\n",
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
	_, censusError := census(pairs, moduleRoot, moduleRoot,
		[]string{"github.com/system-inc/cohere/no_such_directory/..."})
	if censusError == nil || !strings.Contains(censusError.Error(), "matched no packages") {
		test.Errorf("a load of nothing gave %v, want it refused", censusError)
	}
}

// A package of the tree that does not type-check is named, with its error, and fails nothing: each side's not
// running on it is counted per side.
func TestCensusNamesAPackageThatDoesNotLoadClean(test *testing.T) {
	test.Parallel()
	result, censusError := census(pairs, moduleRoot, moduleRoot,
		[]string{"./internal/go_engine/check/parity/testdata/src/illtyped"})
	if censusError != nil {
		test.Fatal(censusError)
	}
	report := result.report
	if result.regressions+result.unexplained != 0 {
		test.Errorf("the census fails a package neither side ran on:\n%s", report)
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

// Every finding only the port makes over the differences fixture is a recorded gain, so the census passes, and the
// gain recorded for a file the tree does not hold is listed as not seen, failing nothing.
func TestCensusPassesRecordedGains(test *testing.T) {
	test.Parallel()
	result, censusError := census(nilnessPair("command/cohere-parity/testdata/gains_differences.txt"), moduleRoot,
		moduleRoot, differencesFixture)
	if censusError != nil {
		test.Fatal(censusError)
	}
	if result.regressions+result.unexplained != 0 {
		test.Errorf("recorded gains fail the census:\n%s", result.report)
	}
	for _, wanted := range []string{
		"nilness: original 0, port 6, gains 6, unexplained 0\n",
		"  gain: internal/go_engine/rules/nil_ness/testdata/src/differences/differences.go:55:15: impossible " +
			"condition: nil != nil\n    because onlyEverNil: staticcheck merges",
		"  gain not seen: elsewhere/elsewhere.go:1:1: impossible condition: nil != nil\n",
	} {
		if !strings.Contains(result.report, wanted) {
			test.Errorf("the census does not say %q:\n%s", wanted, result.report)
		}
	}
}

// A gain recorded with no reason above it is refused before anything loads.
func TestCensusRefusesAGainWithNoReason(test *testing.T) {
	test.Parallel()
	_, censusError := census(nilnessPair("command/cohere-parity/testdata/gains_unreasoned.txt"), moduleRoot,
		moduleRoot, differencesFixture)
	if censusError == nil || !strings.Contains(censusError.Error(), "gains_unreasoned.txt:3 records a gain with no "+
		"reason above it") {
		test.Errorf("a gain with no reason gave %v, want it refused", censusError)
	}
}

// The census finds the checkout it runs in, from this package's directory, and every gains file the table names
// reads there.
func TestCohereRootHoldsTheGainsFiles(test *testing.T) {
	test.Parallel()
	root, rootError := cohereRoot()
	if rootError != nil {
		test.Fatal(rootError)
	}
	wanted, absoluteError := filepath.Abs(moduleRoot)
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	if root != wanted {
		test.Errorf("cohere's root is %s, want %s", root, wanted)
	}
	for _, each := range pairs {
		if each.gains == "" {
			continue
		}
		if _, readError := readGains(filepath.Join(root, filepath.FromSlash(each.gains))); readError != nil {
			test.Errorf("%s: %v", each.pair.Name, readError)
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
