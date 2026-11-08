// Package parity is the one bar every port of a borrowed checker is held to: a port matches its original finding
// for finding, or differs only where it is right, each difference adjudicated with the finding in hand and pinned in
// a fixture. A finding the original makes and the port misses is a regression, never an adjudicated difference, so
// nothing here can record one.
//
// A port's tests call it in a few lines: RequireUpstreamWants over the original's own fixtures, RequireIdentical
// over those and the port's parity fixtures, RequireOnlyAdjudicated over its differences fixture. The census command,
// command/cohere-parity, runs Analyze over a real tree.
package parity

import (
	"cmp"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// Pair is an original analyzer and its port, under the name the census prints. The ports carry their originals'
// analyzer names, so the name is the pair's, never read off either analyzer.
type Pair struct {
	Name     string
	Original *analysis.Analyzer
	Port     *analysis.Analyzer
}

// Side is what one analyzer of a pair did over the packages it was given: its findings, sorted, and the packages it
// did not run on, each with why.
type Side struct {
	Findings []Finding
	Failures []Failure
}

// Failure is a package an analyzer did not run on, with the checker's error: the package did not type-check and the
// analyzer does not run despite errors, or an analyzer it requires failed there.
type Failure struct {
	Package string
	Error   string
}

// Outcome is one pair's two sides over the same packages.
type Outcome struct {
	Pair     Pair
	Original Side
	Port     Side
}

// Analyze runs every pair's original and port in one checker over roots and returns each pair's outcome, in the
// order of pairs. The two sides share the checker's prerequisites and see the same packages, so where they differ it
// is the analyzers that differ.
func Analyze(pairs []Pair, roots []*packages.Package) ([]Outcome, error) {
	analyzers := []*analysis.Analyzer{}
	// sideOf is where an analyzer's results go, keyed by the analyzer itself, since a port and its original share
	// a name.
	sideOf := map[*analysis.Analyzer]*Side{}
	outcomes := make([]Outcome, len(pairs))
	for index, pair := range pairs {
		outcomes[index].Pair = pair
		analyzers = append(analyzers, pair.Original, pair.Port)
		sideOf[pair.Original] = &outcomes[index].Original
		sideOf[pair.Port] = &outcomes[index].Port
	}
	graph, analyzeError := checker.Analyze(analyzers, roots, &checker.Options{})
	if analyzeError != nil {
		return nil, fmt.Errorf("analyzing %d packages: %w", len(roots), analyzeError)
	}
	for _, action := range graph.Roots {
		side := sideOf[action.Analyzer]
		if action.Err != nil {
			side.Failures = append(side.Failures, Failure{Package: action.Package.ID, Error: action.Err.Error()})
			continue
		}
		for _, diagnostic := range action.Diagnostics {
			side.Findings = append(side.Findings, newFinding(action.Package.Fset, diagnostic))
		}
	}
	for index := range outcomes {
		for _, side := range []*Side{&outcomes[index].Original, &outcomes[index].Port} {
			slices.SortFunc(side.Findings, func(first Finding, second Finding) int {
				return cmp.Or(cmp.Compare(first.String(), second.String()), cmp.Compare(first.key(), second.key()))
			})
			slices.SortFunc(side.Failures, func(first Failure, second Failure) int {
				return cmp.Or(cmp.Compare(first.Package, second.Package), cmp.Compare(first.Error, second.Error))
			})
		}
	}
	return outcomes, nil
}

// loadFailuresShown is how many of a package's load errors LoadFailures names; the first say why, and the rest
// usually follow from them.
const loadFailuresShown = 3

// LoadFailures is every package of loadedPackages that did not load clean, each with its first errors, in the order
// given. The checker skips such a package for an analyzer that does not run despite errors, and runs one that does
// over what of it type-checked, so its findings there are missing or partial, and a comparison over it proves little.
// A caller fails on one where every package must count, as over fixtures, and names each where some may not, as over
// a whole tree.
func LoadFailures(loadedPackages []*packages.Package) []Failure {
	failures := []Failure{}
	for _, loadedPackage := range loadedPackages {
		if len(loadedPackage.Errors) == 0 {
			continue
		}
		shown := []string{}
		for _, loadError := range loadedPackage.Errors[:min(len(loadedPackage.Errors), loadFailuresShown)] {
			// A type error can span lines, as a call's have and want do, and a failure is one line.
			shown = append(shown, strings.Join(strings.Fields(loadError.Error()), " "))
		}
		described := strings.Join(shown, "; ")
		if hidden := len(loadedPackage.Errors) - len(shown); hidden > 0 {
			described += fmt.Sprintf("; and %d more", hidden)
		}
		failures = append(failures, Failure{Package: loadedPackage.ID, Error: described})
	}
	return failures
}

// Findings loads the fixture packages patterns name from testdata, runs original and port over them in one checker,
// and returns each one's findings, sorted, each in its file's base name. A load that found nothing, a fixture package
// that did not load clean, and an analyzer that did not run on a fixture package each fail the test, since silence
// there would read as agreement.
func Findings(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) (originalFindings []Finding, portFindings []Finding) {
	test.Helper()
	loaded, loadError := load.Packages(testdata, patterns, fixtureEnvironment(testdata), nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// The checker passes over a root that does not type-check without a word, so a fixture with one type error would
	// compare nothing and read green. There must be a root, and every root must load clean.
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, failure := range LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %s",
			failure.Package, failure.Error)
	}
	// The roots, as analysistest analyzes them: every package the patterns match and every test variant of it.
	outcomes, analyzeError := Analyze([]Pair{{Original: original, Port: port}}, loaded.Roots)
	if analyzeError != nil {
		test.Fatalf("%v", analyzeError)
	}
	outcome := outcomes[0]
	for _, failure := range slices.Concat(outcome.Original.Failures, outcome.Port.Failures) {
		test.Fatalf("running on %s: %s", failure.Package, failure.Error)
	}
	return baseNamed(outcome.Original.Findings), baseNamed(outcome.Port.Findings)
}

// fixtureEnvironment is the environment a fixture tree loads under, as analysistest loads one: a module when
// testdata holds a go.mod, and otherwise a tree laid out by import path under src.
func fixtureEnvironment(testdata string) []string {
	if _, statError := os.Stat(filepath.Join(testdata, "go.mod")); statError == nil {
		work := filepath.Join(testdata, "go.work")
		if _, workError := os.Stat(work); workError != nil {
			work = "off"
		}
		return []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=" + work}
	}
	return []string{"GOPATH=" + testdata, "GO111MODULE=off", "GOWORK=off"}
}

// baseNamed is findings with each file they name named by its base name.
func baseNamed(findings []Finding) []Finding {
	named := []Finding{}
	for _, finding := range findings {
		named = append(named, finding.WithFileNames(filepath.Base))
	}
	return named
}

// RequireIdentical runs original and port over the fixture packages patterns name, and their findings must be the
// same multiset, each the same in every field: its range, its message, its related entries and its fixes. Each side's
// count is logged, since two empty sides would match and prove nothing, and the test fails if the original finds
// nothing at all.
func RequireIdentical(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	originalFindings, portFindings := Findings(test, original, port, testdata, patterns...)
	test.Logf("the original found %d, the port %d", len(originalFindings), len(portFindings))
	if len(originalFindings) == 0 {
		test.Fatalf("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range Difference(originalFindings, portFindings) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range Difference(portFindings, originalFindings) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
}

// adjudicatedPattern is one comment in a differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, or `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ, as where the two forms number their registers differently.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?))\s*$`)

// RequireOnlyAdjudicated runs original and port over the differences fixture, the package fixture under testdata's
// src, where the two disagree, and only where each disagreement was adjudicated in the port's favour: every finding
// the port alone makes is recorded on its line, as found or as the port's side of a renaming, every finding the
// original alone makes is the original's side of a recorded renaming, and every record is matched. A finding the
// original makes and the port does not, unrecorded, is a regression and fails, since the port is never worse than the
// original. A `// port ` comment it cannot read fails, and so does a fixture that records nothing.
func RequireOnlyAdjudicated(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
) {
	test.Helper()
	originalFindings, portFindings := Findings(test, original, port, testdata, fixture)
	test.Logf("the original found %d, the port %d", len(originalFindings), len(portFindings))
	expectedPort, expectedOriginal := adjudicated(test, filepath.Join(testdata, "src", filepath.FromSlash(fixture)))
	if len(expectedPort) == 0 {
		test.Fatalf("the differences fixture %s records nothing, so the comparison proves nothing", fixture)
	}
	portAlone := asRecorded(Difference(portFindings, originalFindings))
	for _, unrecorded := range Difference(portAlone, expectedPort) {
		test.Errorf("the port alone found %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range Difference(expectedPort, portAlone) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", unmatched)
	}
	originalAlone := asRecorded(Difference(originalFindings, portFindings))
	for _, missed := range Difference(originalAlone, expectedOriginal) {
		test.Errorf("the original found %s, and the port did not, a regression", missed)
	}
	for _, unmatched := range Difference(expectedOriginal, originalAlone) {
		test.Errorf("the fixture records that the original alone finds %s, renamed, and it does not", unmatched)
	}
}

// adjudicated reads every `// port ` comment of the .go files in directory: what the port alone finds there, from
// `finds:` and the port's side of `renames:`, and what the original alone finds, the original's side of `renames:`,
// each on the comment's line with no column. It reads comments as the parser finds them, so a `// port ` inside a
// string is no record.
func adjudicated(test testing.TB, directory string) (expectedPort []Finding, expectedOriginal []Finding) {
	test.Helper()
	names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatalf("listing the differences fixture %s: %v", directory, globError)
	}
	fileSet := token.NewFileSet()
	for _, name := range names {
		file, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing the differences fixture: %v", parseError)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				line := fileSet.Position(comment.Slash).Line
				for _, record := range strings.Split(comment.Text, "// port ")[1:] {
					match := adjudicatedPattern.FindStringSubmatch(record)
					at := Finding{File: filepath.Base(name), Line: line}
					switch {
					case match == nil:
						test.Fatalf("%s:%d holds a comment the harness cannot read: %q", at.File, line, record)
					case match[1] != "":
						at.Message = match[1]
						expectedPort = append(expectedPort, at)
					default:
						renamed := at
						renamed.Message = match[2]
						expectedOriginal = append(expectedOriginal, renamed)
						at.Message = match[3]
						expectedPort = append(expectedPort, at)
					}
				}
			}
		}
	}
	return expectedPort, expectedOriginal
}

// asRecorded is findings as the differences fixture's comments record them, by file, line and message alone: the
// column, the end, the related entries and the fixes are dropped for that match, and only for it, since a comment
// names a line and a message. RequireIdentical and the census compare every field.
func asRecorded(findings []Finding) []Finding {
	recorded := []Finding{}
	for _, finding := range findings {
		recorded = append(recorded, Finding{File: finding.File, Line: finding.Line, Message: finding.Message})
	}
	return recorded
}
