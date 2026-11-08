// Package parity is the one bar every port of a borrowed checker is held to: a port matches its original finding
// for finding, or differs only where it is right, each difference adjudicated with the finding in hand and pinned in
// a fixture. A finding the original makes and the port misses is a regression, unless it was ruled a false positive
// with the finding in hand, the reason written above the record that drops it; nothing else can record one. A finding
// both make that differs in span, fixes or related entries is paired only by a record that it moves, that the port
// withholds the one fix of the original's it leaves out, or that it omits the one related entry. Every record gives
// its reason, on the line above it or in the doc comment of the declaration it is in.
//
// A `moves:` record pairs two findings by line and message alone, so it proves nothing of the port's fix there: the
// port's own tests hold that fix, with a golden file or by applying the edit and checking what it makes.
//
// A port's tests call it in a few lines: RequireUpstreamWants over the original's own fixtures, or
// RequireUpstreamWantsWithFixes where the original's own test holds its fixes to golden files, or
// RequireStaticcheckWants over staticcheck's, RequireIdentical over those and the port's parity fixtures,
// RequireIdenticalDespiteLoadErrors over those that fail to type-check on purpose, for a pair that runs despite errors,
// RequireOnlyAdjudicated over its differences fixture, RequireStaticcheckGolden over staticcheck's golden files for
// a port that offers fixes, and RequirePortWhereOriginalPanics over a fixture the original panics on. Each takes
// testdata and patterns, and a pattern naming a folder of testdata that holds a go.mod loads that folder as a module
// (see fixtureLoad). Each loads for the machine's own target, and a port whose check judges another calls the same on
// the target it judges, as `parity.On("GOOS=linux", "GOARCH=386", "CGO_ENABLED=0").RequireIdentical(...)` (see
// Target). The census command, command/cohere-parity, runs Analyze over a real tree.
package parity

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
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
			slices.SortFunc(side.Findings, compareFindings)
			slices.SortFunc(side.Failures, func(first Failure, second Failure) int {
				return cmp.Or(cmp.Compare(first.Package, second.Package), cmp.Compare(first.Error, second.Error))
			})
		}
	}
	return outcomes, nil
}

// compareFindings orders findings by how a finding line prints them, and two that print alike by every field.
func compareFindings(first Finding, second Finding) int {
	return cmp.Or(cmp.Compare(first.String(), second.String()), cmp.Compare(first.key(), second.key()))
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

// Findings loads the fixture packages patterns name from testdata, runs original and port over them in one checker
// for each load, and returns each one's findings, sorted, each in its file's base name, after its module's pattern in
// a fixture module (see fixtureLoad). A load that found nothing, a fixture package that did not load clean, and an
// analyzer that did not run on a fixture package each fail the test, since silence there would read as agreement. It
// loads for the machine's own target, and Target.Findings for another.
func Findings(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) (originalFindings []Finding, portFindings []Finding) {
	test.Helper()
	return findingsOver(test, original, port, loadFixtures(test, Target{}, testdata, patterns, nil))
}

// findingsOver is Findings over loads already made, so a caller that reads the loaded packages as well, as
// RequireOnlyAdjudicated reads where its records sit, loads them once.
func findingsOver(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	loads []loadedFixtures,
) (originalFindings []Finding, portFindings []Finding) {
	test.Helper()
	originalFindings, portFindings = []Finding{}, []Finding{}
	for _, fixtures := range loads {
		// The roots, as analysistest analyzes them: every package the patterns match and every test variant of it.
		outcomes, analyzeError := Analyze([]Pair{{Original: original, Port: port}}, fixtures.roots)
		if analyzeError != nil {
			test.Fatalf("%v", analyzeError)
		}
		outcome := outcomes[0]
		for _, failure := range slices.Concat(outcome.Original.Failures, outcome.Port.Failures) {
			test.Fatalf("running on %s: %s", failure.Package, failure.Error)
		}
		for _, finding := range outcome.Original.Findings {
			originalFindings = append(originalFindings, finding.WithFileNames(fixtures.fileName))
		}
		for _, finding := range outcome.Port.Findings {
			portFindings = append(portFindings, finding.WithFileNames(fixtures.fileName))
		}
	}
	slices.SortFunc(originalFindings, compareFindings)
	slices.SortFunc(portFindings, compareFindings)
	return originalFindings, portFindings
}

// RequireIdentical runs original and port over the fixture packages patterns name, and their findings must be the
// same multiset, each the same in every field: its range, its message, its related entries and its fixes. Each side's
// count is logged, since two empty sides would match and prove nothing, and the test fails if the original finds
// nothing at all. It loads for the machine's own target, and Target.RequireIdentical for another.
func RequireIdentical(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireIdentical(test, Target{}, original, port, testdata, nil, patterns...)
}

// requireIdentical is RequireIdentical with the fixtures loaded for target, over fixture packages that load with
// exactly the errors expected, or clean when it expects none (see loadFixtures).
func requireIdentical(
	test testing.TB,
	target Target,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	expected []LoadError,
	patterns ...string,
) {
	test.Helper()
	originalFindings, portFindings := findingsOver(test, original, port,
		loadFixtures(test, target, testdata, patterns, expected))
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
// the message of a finding only the port makes; `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ, as where the two forms number their registers differently; or `drops:` and
// the message of a finding only the original makes, ruled a false positive with the finding in hand; `moves:` and the
// message of a finding both make there whose span or fixes differ; `withholds:` and the message of a fix the
// original offers on a finding both make there and the port leaves out; or `omits:` and the message of a related
// entry the original's finding carries there and the port's leaves out. Each record's reason is in the comment above
// it, and each matches one finding on its own line.
var adjudicatedPattern = regexp.MustCompile(
	`^(?:finds: (.+?)|renames: (.+?) => (.+?)|drops: (.+?)|moves: (.+?)|withholds: (.+?)|omits: (.+?))\s*$`,
)

// RequireOnlyAdjudicated runs original and port over the differences fixture, the package fixture names, its records
// read from the folder the load found it in (see recordsFolder), or the fixture module of that name (see
// fixtureLoad), where the two disagree, and only where each disagreement was
// adjudicated: every finding the port alone makes is recorded on its line, as found or as the port's side of a
// renaming; every finding the original alone makes is recorded on its line, as the original's side of a renaming or as
// dropped, a false positive ruled with the finding in hand; a finding both make in other spans or fixes is recorded as
// moved, one the original offers a fix on that the port leaves out as withheld, and one whose related entry the port
// leaves out as omitted, each pairing the two sides' findings and nothing more; and every record is matched by a
// finding on its own line, never another's. A finding the original makes and the port does not, unrecorded, is a
// regression and fails, and so does any fix or related entry one side alone offers that no record withholds or omits.
// Every record must give its reason, on the line above it or in the doc comment of the declaration it is in, and one
// with none fails, named by file and line. A `// port ` comment it cannot read fails, and so does a fixture that
// records nothing. The census has no fixture, so no record covers anything there.
//
// A `moves:` record pairs by line and message only, so the port's own tests must hold its fix there, with a golden file
// or by applying the edit and checking the result. It reads the one package directory fixture names, not the packages
// nested below it, so a fixture tree with nested packages takes one call for each. It loads for the machine's own
// target, and Target.RequireOnlyAdjudicated for another, and reads the records of every file of that directory,
// whichever target builds it, so a record in a file the target does not build fails unmatched.
func RequireOnlyAdjudicated(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
) {
	test.Helper()
	requireOnlyAdjudicated(test, Target{}, original, port, testdata, fixture)
}

// requireOnlyAdjudicated is RequireOnlyAdjudicated with the fixtures loaded for target.
func requireOnlyAdjudicated(
	test testing.TB,
	target Target,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
) {
	test.Helper()
	loads := loadFixtures(test, target, testdata, []string{fixture}, nil)
	originalFindings, portFindings := findingsOver(test, original, port, loads)
	test.Logf("the original found %d, the port %d", len(originalFindings), len(portFindings))
	recorded := adjudicated(test, recordsFolder(test, loads[0]), loads[0].fileName)
	if len(recorded.portAlone)+len(recorded.dropped)+len(recorded.moved)+len(recorded.withheld)+
		len(recorded.omitted) == 0 {
		test.Fatalf("the differences fixture %s records nothing, so the comparison proves nothing", fixture)
	}
	// A withheld fix, an omitted related entry and a move each pair one finding of each side's, compared whole, before
	// what is left of each is matched by line and message alone.
	originalOnly, portOnly := pairLeftOut(test, "withholds the fix", recorded.withheld, withoutFix,
		Difference(originalFindings, portFindings), Difference(portFindings, originalFindings))
	originalOnly, portOnly = pairLeftOut(test, "omits the related entry", recorded.omitted, withoutRelated,
		originalOnly, portOnly)
	originalOnly, portOnly = pairMoved(test, recorded.moved, originalOnly, portOnly)
	portAlone := asRecorded(portOnly)
	for _, unrecorded := range Difference(portAlone, recorded.portAlone) {
		test.Errorf("the port alone found %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range Difference(recorded.portAlone, portAlone) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", unmatched)
	}
	// The original's lone findings are matched against the renamings, then what is left against the drops, so a
	// finding one record covers is never counted against another.
	originalAlone := asRecorded(originalOnly)
	notRenamed := Difference(originalAlone, recorded.renamed)
	for _, missed := range Difference(notRenamed, recorded.dropped) {
		test.Errorf("the original found %s, and the port did not, a regression", missed)
	}
	for _, unmatched := range Difference(recorded.renamed, originalAlone) {
		test.Errorf("the fixture records that the original alone finds %s, renamed, and it does not", unmatched)
	}
	for _, unmatched := range Difference(recorded.dropped, notRenamed) {
		test.Errorf("the fixture records that the port drops %s, and the original alone does not find it", unmatched)
	}
}

// records are a differences fixture's records, each a file, a line and a message with no column: portAlone, what the
// port alone finds, from `finds:` and the port's side of `renames:`; renamed, the original's side of `renames:`; and
// dropped, what the original alone finds and the port rightly does not, from `drops:`; moved, a finding both make in
// other spans or fixes, from `moves:`; withheld, from `withholds:`, whose Message is the fix's message, not the
// finding's; and omitted, from `omits:`, whose Message is the related entry's.
type records struct {
	portAlone []Finding
	renamed   []Finding
	dropped   []Finding
	moved     []Finding
	withheld  []Finding
	omitted   []Finding
}

// pairLeftOut takes out of originalOnly and portOnly each pair a record covers: a finding of the original's on the
// record's line from which leaveOut can take one part with the record's message, and a finding of the port's that is
// the same finding without it, whole in every other field. A record that pairs nothing fails, saying that the port
// leaves out what, as in "withholds the fix".
func pairLeftOut(
	test testing.TB,
	leavesOut string,
	recorded []Finding,
	leaveOut func(finding Finding, message string) (Finding, bool),
	originalOnly []Finding,
	portOnly []Finding,
) (remainingOriginal []Finding, remainingPort []Finding) {
	test.Helper()
	// Cloned, since a pair is deleted from each in place.
	remainingOriginal, remainingPort = slices.Clone(originalOnly), slices.Clone(portOnly)
	for _, record := range recorded {
		paired := false
		for originalIndex, candidate := range remainingOriginal {
			if candidate.File != record.File || candidate.Line != record.Line {
				continue
			}
			without, held := leaveOut(candidate, record.Message)
			if !held {
				continue
			}
			portIndex := slices.IndexFunc(remainingPort, func(finding Finding) bool {
				return finding.key() == without.key()
			})
			if portIndex < 0 {
				continue
			}
			remainingOriginal = slices.Delete(remainingOriginal, originalIndex, originalIndex+1)
			remainingPort = slices.Delete(remainingPort, portIndex, portIndex+1)
			paired = true
			break
		}
		if !paired {
			test.Errorf("the fixture records that the port %s %q at %s:%d, and no finding of the original's there "+
				"holds it beside the same finding of the port's without it", leavesOut, record.Message, record.File,
				record.Line)
		}
	}
	return remainingOriginal, remainingPort
}

// withoutFix is finding without its first fix with message, and whether it had one.
func withoutFix(finding Finding, message string) (Finding, bool) {
	index := slices.IndexFunc(finding.Fixes, func(fix Fix) bool { return fix.Message == message })
	if index < 0 {
		return finding, false
	}
	finding.Fixes = slices.Delete(slices.Clone(finding.Fixes), index, index+1)
	return finding, true
}

// withoutRelated is finding without its first related entry with message, and whether it had one.
func withoutRelated(finding Finding, message string) (Finding, bool) {
	index := slices.IndexFunc(finding.Related, func(related Related) bool { return related.Message == message })
	if index < 0 {
		return finding, false
	}
	finding.Related = slices.Delete(slices.Clone(finding.Related), index, index+1)
	return finding, true
}

// pairMoved takes out of originalOnly and portOnly each pair a moved record covers: a finding of each side's on the
// record's line with the record's message, which differ in span or fixes. A record that pairs nothing fails. It pairs
// by line and message alone and proves nothing of the port's fix, which the port's own tests hold, with a golden file
// or by applying the edit and checking the result.
func pairMoved(
	test testing.TB,
	moved []Finding,
	originalOnly []Finding,
	portOnly []Finding,
) (remainingOriginal []Finding, remainingPort []Finding) {
	test.Helper()
	// Cloned, since a pair is deleted from each in place.
	remainingOriginal, remainingPort = slices.Clone(originalOnly), slices.Clone(portOnly)
	onRecordedLine := func(record Finding) func(Finding) bool {
		return func(finding Finding) bool {
			return finding.File == record.File && finding.Line == record.Line && finding.Message == record.Message
		}
	}
	for _, record := range moved {
		originalIndex := slices.IndexFunc(remainingOriginal, onRecordedLine(record))
		portIndex := slices.IndexFunc(remainingPort, onRecordedLine(record))
		if originalIndex < 0 || portIndex < 0 {
			test.Errorf("the fixture records that the port moves %s, and the two do not both find it there in "+
				"other spans or fixes", record)
			continue
		}
		remainingOriginal = slices.Delete(remainingOriginal, originalIndex, originalIndex+1)
		remainingPort = slices.Delete(remainingPort, portIndex, portIndex+1)
	}
	return remainingOriginal, remainingPort
}

// adjudicated reads every `// port ` comment of the .go files in directory, each record on the comment's line, its
// file named by fileName as the findings name it, and fails each record that gives no reason (see reasonedLines). It
// reads comments as the parser finds them, so a `// port ` inside a string is no record. It reads that one directory,
// not those below it.
func adjudicated(test testing.TB, directory string, fileName func(string) string) records {
	test.Helper()
	names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatalf("listing the differences fixture %s: %v", directory, globError)
	}
	recorded := records{}
	fileSet := token.NewFileSet()
	for _, name := range names {
		file, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing the differences fixture: %v", parseError)
		}
		reasoned := reasonedLines(fileSet, file)
		for _, group := range file.Comments {
			for _, comment := range group.List {
				line := fileSet.Position(comment.Slash).Line
				for _, record := range strings.Split(comment.Text, "// port ")[1:] {
					match := adjudicatedPattern.FindStringSubmatch(record)
					at := Finding{File: fileName(name), Line: line}
					if match != nil && !reasoned(line) {
						test.Errorf("%s:%d holds a record with no reason: write why on the line above it, or in the "+
							"doc comment of the declaration it is in", at.File, line)
					}
					switch {
					case match == nil:
						test.Fatalf("%s:%d holds a comment the harness cannot read: %q", at.File, line, record)
					case match[1] != "":
						at.Message = match[1]
						recorded.portAlone = append(recorded.portAlone, at)
					case match[4] != "":
						at.Message = match[4]
						recorded.dropped = append(recorded.dropped, at)
					case match[5] != "":
						at.Message = match[5]
						recorded.moved = append(recorded.moved, at)
					case match[6] != "":
						at.Message = match[6]
						recorded.withheld = append(recorded.withheld, at)
					case match[7] != "":
						at.Message = match[7]
						recorded.omitted = append(recorded.omitted, at)
					default:
						renamed := at
						renamed.Message = match[2]
						recorded.renamed = append(recorded.renamed, renamed)
						at.Message = match[3]
						recorded.portAlone = append(recorded.portAlone, at)
					}
				}
			}
		}
	}
	return recorded
}

// reasonedLines is, for file, whether a record on a line has its reason, the Square's term for every record: a
// comment that is no record ending on the line directly above, or the doc comment of the top-level declaration, or
// of the declaration in a group, that the line is in. Wave 1's fixtures give their reasons as the doc comment of the
// function the records are in.
func reasonedLines(fileSet *token.FileSet, file *ast.File) func(line int) bool {
	reasonEndsOn := map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if !strings.Contains(comment.Text, "// port ") {
				reasonEndsOn[fileSet.Position(comment.End()).Line] = true
			}
		}
	}
	// documented is the line span of each declaration with a doc comment.
	type span struct{ first, last int }
	documented := []span{}
	add := func(node ast.Node) {
		documented = append(documented, span{fileSet.Position(node.Pos()).Line, fileSet.Position(node.End()).Line})
	}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Doc != nil {
				add(declaration)
			}
		case *ast.GenDecl:
			if declaration.Doc != nil {
				add(declaration)
				continue
			}
			for _, specification := range declaration.Specs {
				switch specification := specification.(type) {
				case *ast.ValueSpec:
					if specification.Doc != nil {
						add(specification)
					}
				case *ast.TypeSpec:
					if specification.Doc != nil {
						add(specification)
					}
				}
			}
		}
	}
	return func(line int) bool {
		if reasonEndsOn[line-1] {
			return true
		}
		for _, each := range documented {
			if each.first <= line && line <= each.last {
				return true
			}
		}
		return false
	}
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
