// Package parity is the one bar every port of a borrowed checker is held to: a port matches its original finding
// for finding, or differs only where it is right, each difference adjudicated with the finding in hand and pinned in
// a fixture. A finding the original makes and the port misses is a regression, unless it was ruled a false positive
// with the finding in hand, the reason written above the record that drops it; nothing else can record one. A finding
// both make that differs in span, fixes or related entries is paired only by a record that it moves, that the port
// withholds the one fix of the original's it leaves out, or that it omits the one related entry. A finding the port
// alone makes is recorded with the text each of its fixes writes, so no fix of the port's goes unread, and one it
// offers with no edits, which no record can stand for, fails. Records are comments of the fixture's Go files and its
// assembly files, and every record gives its reason: a drop on the line directly above it, any other in a Go file
// there or in the doc comment of the declaration it is in, and any in an assembly file, which has no declarations,
// on the line directly above it too.
//
// A `moves:` record pairs two findings by line and message alone, so it proves nothing of the port's fix there: a
// fixed file beside the fixture's file holds that fix, the source every fix the port offers there leaves, byte for
// byte (see requireFixedFiles), or else the port's own tests do, with a golden file or by applying the edit and
// checking what it makes.
//
// A port's tests call it in a few lines: RequireUpstreamWants over the original's own fixtures, or
// RequireUpstreamWantsWithFixes where the original's own test holds its fixes to golden files, each on
// `parity.Renaming(...)` where the port meets a want there in other words, adjudicated, or RequireStaticcheckWants
// over staticcheck's, RequireIdentical over those and the port's parity fixtures,
// RequireIdenticalDespiteLoadErrors over those that fail to type-check on purpose, for a pair that runs despite errors,
// RequireIdenticalFacts over them for a pair that exports facts, RequireOnlyAdjudicated over its differences fixture,
// RequireOnlyAdjudicatedUnderUserCacheDirectory over one that must sit under the user's cache directory,
// RequireStaticcheckGolden over staticcheck's golden files for a port that offers fixes, RequirePortWhereOriginalPanics
// over a fixture the original panics on, RequireNeitherFinds over what both must leave alone, counting what the run
// handed them there, and RequireIdenticalLeavingAlone over packages holding files both judge beside files both must
// leave alone, named. Each takes testdata and patterns, and a pattern naming a folder of testdata that holds a go.mod
// loads that folder as a module (see fixtureLoad). Each loads for the machine's own target, and a port whose check
// judges another calls the same on the target it judges, as `parity.On("GOOS=linux", "GOARCH=386",
// "CGO_ENABLED=0").RequireIdentical(...)`, which sets the `GOOS` and `GOARCH` it names in go/build's default context as
// well for the call, where asmdecl and framepointer read their target (see Target), and `.JudgingBy("GOARCH=sparc64")`
// sets them there apart from the load, for a file the load hands over whose architecture the check must not be able to
// name. The census command, command/cohere-parity, runs Analyze over a real tree.
//
// The originals, staticcheck's checks and x/tools' passes, stay in the tests as live oracles, never frozen into
// recorded findings: a frozen oracle can't tell us when a staticcheck or x/tools release moves away from a port, and
// the live comparison is the only check that holds a port to upstream across releases. It costs the binary nothing,
// since the registry's guard keeps every original out of ./command/cohere.
package parity

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
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
// is the analyzers that differ. It holds go/build's default context to read while it runs, as every call of the
// harness does, so no Target's call sets it meanwhile (see Target.held); the load of roots is the caller's.
func Analyze(pairs []Pair, roots []*packages.Package) ([]Outcome, error) {
	defaultContext.RLock()
	defer defaultContext.RUnlock()
	return analyze(pairs, roots)
}

// analyze is Analyze within a call that already holds go/build's default context.
func analyze(pairs []Pair, roots []*packages.Package) ([]Outcome, error) {
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
	graph, analyzeError := checker.Analyze(analyzers, roots, engineOptions())
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
// for each load, and returns each one's findings, sorted, each file named by its path below the fixtures' root, after
// its module's pattern in a fixture module (see fixtureLoad.fileName). A load that found nothing, a fixture package
// that did not load clean, and an analyzer that did not run on a fixture package each fail the test, since silence
// there would read as agreement. It loads for the machine's own target, and Target.Findings for another.
func Findings(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) (originalFindings []Finding, portFindings []Finding) {
	test.Helper()
	defer Target{}.held(test)()
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
		outcomes, analyzeError := analyze([]Pair{{Original: original, Port: port}}, fixtures.roots)
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
	defer target.held(test)()
	originalFindings, portFindings := findingsOver(test, original, port,
		loadFixtures(test, target, testdata, patterns, expected))
	compareIdentical(test, originalFindings, portFindings)
}

// compareIdentical is RequireIdentical's comparison of the findings each side made: each side's count logged, the test
// failed when the original found nothing, and every finding one side alone makes named.
func compareIdentical(test testing.TB, originalFindings []Finding, portFindings []Finding) {
	test.Helper()
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
// entry the original's finding carries there and the port's leaves out; or `fixes:` and the text one edit of a fix
// writes, on a finding a `finds:` record covers, on that record's line or the line after it (see fixesText), the edit
// starting on the finding's own line, or `fixes at <file>:<line>:` for one that starts elsewhere, the file named as
// findings name theirs, below the fixtures' root; or `finds nothing`, with no message, where the original panics on
// the fixture and the port rightly finds nothing in it at all, which only RequirePortWhereOriginalPanics holds.
//
// A `finds:` or `moves:` record may place the port's finding: `finds at column <column>:` for one that must start at
// that column, counted in bytes from 1 as go/token counts it, and `finds at column <column> to <end column>:`, `finds
// at column <column> to <end line>:<end column>:` or `finds at column <column>, no end:` for one that must also end
// there, or have no end, each written `moves at column` for the port's side of a move. A `moves:` record may also name
// the message of a fix the port's side offers, after any place, as `moves with "<message>":`, and a `fixes:` record
// the message of the fix its edit is of, before any `at`, as `fixes with "<message>":`, each message a Go string
// literal. The plain `moves:` alternative comes first, and the pattern matches leftmost first, so a move with neither
// is read there.
//
// Each record's reason is in the comment above it, or for any but a drop in the doc comment of the declaration it is
// in, and each but `finds nothing` matches one finding on its own line, a `fixes:` record its `finds:` record's.
//
// Every group it captures is named, and read by its name (see recordGroup), so an alternative added anywhere shifts
// no other's read.
var adjudicatedPattern = regexp.MustCompile(
	`^(?:finds: (?P<Finds>.+?)|renames: (?P<RenamesFrom>.+?) => (?P<RenamesTo>.+?)|drops: (?P<Drops>.+?)|` +
		`moves: (?P<Moves>.+?)|withholds: (?P<Withholds>.+?)|omits: (?P<Omits>.+?)|` +
		`fixes(?: with (?P<FixesMessage>"(?:[^"\\]|\\.)*"))?(?: at (?P<FixesFile>\S+?):(?P<FixesLine>\d+))?: ` +
		`(?P<FixesText>.+?)|(?P<FindsNothing>finds nothing)|` +
		`finds at column (?P<FindsColumn>\d+)(?:(?P<FindsNoEnd>, no end)| to (?:(?P<FindsEndLine>\d+):)?` +
		`(?P<FindsEndColumn>\d+))?: (?P<FindsPlaced>.+?)|` +
		`moves(?: at column (?P<MovesColumn>\d+)(?:(?P<MovesNoEnd>, no end)| to (?:(?P<MovesEndLine>\d+):)?` +
		`(?P<MovesEndColumn>\d+))?)?(?: with (?P<MovesFixMessage>"(?:[^"\\]|\\.)*"))?: (?P<MovesPlaced>.+?))\s*$`,
)

// The index of each group adjudicatedPattern captures, in a match, by the group's name: a record's message for each
// plain kind; a rename's two messages; a `fixes:` record's message literal, the file and line it names and its text;
// `finds nothing`; and for a placed `finds:` and a `moves:`, the column, `, no end`, the end line, the end column, the
// fix message literal a move may name, and the message.
var (
	findsGroup           = recordGroup("Finds")
	renamesFromGroup     = recordGroup("RenamesFrom")
	renamesToGroup       = recordGroup("RenamesTo")
	dropsGroup           = recordGroup("Drops")
	movesGroup           = recordGroup("Moves")
	withholdsGroup       = recordGroup("Withholds")
	omitsGroup           = recordGroup("Omits")
	fixesMessageGroup    = recordGroup("FixesMessage")
	fixesFileGroup       = recordGroup("FixesFile")
	fixesLineGroup       = recordGroup("FixesLine")
	fixesTextGroup       = recordGroup("FixesText")
	findsNothingGroup    = recordGroup("FindsNothing")
	findsColumnGroup     = recordGroup("FindsColumn")
	findsNoEndGroup      = recordGroup("FindsNoEnd")
	findsEndLineGroup    = recordGroup("FindsEndLine")
	findsEndColumnGroup  = recordGroup("FindsEndColumn")
	findsPlacedGroup     = recordGroup("FindsPlaced")
	movesColumnGroup     = recordGroup("MovesColumn")
	movesNoEndGroup      = recordGroup("MovesNoEnd")
	movesEndLineGroup    = recordGroup("MovesEndLine")
	movesEndColumnGroup  = recordGroup("MovesEndColumn")
	movesFixMessageGroup = recordGroup("MovesFixMessage")
	movesPlacedGroup     = recordGroup("MovesPlaced")
)

// recordGroup is the index of the group of adjudicatedPattern named name.
func recordGroup(name string) int {
	index := adjudicatedPattern.SubexpIndex(name)
	if index < 0 {
		// Invariant: every name read here is one the pattern beside it names, so a missing one is a mistake in this
		// file, never in a fixture, and panicking as the package initializes fails every test before any runs.
		panic("adjudicatedPattern names no group " + name)
	}
	return index
}

// fixesText is the new text a `fixes:` record names: the text as written, or, written as a Go string literal opening
// with a double quote, the string it spells, so a text that holds a newline, or that writes nothing, is recorded as
// `fixes: "import \"strconv\"\n"` or `fixes: ""`, and a text that opens with a double quote is quoted too. A literal it
// cannot read is no record.
func fixesText(written string) (string, bool) {
	if !strings.HasPrefix(written, `"`) {
		return written, true
	}
	text, unquoteError := strconv.Unquote(written)
	return text, unquoteError == nil
}

// RequireOnlyAdjudicated runs original and port over the differences fixture, the package fixture names, its records
// read from the folder the load found it in (see recordsFolder), or the fixture module of that name (see
// fixtureLoad), where the two disagree, and only where each disagreement was
// adjudicated: every finding the port alone makes is recorded on its line, as found or as the port's side of a
// renaming; every finding the original alone makes is recorded on its line, as the original's side of a renaming or as
// dropped, a false positive ruled with the finding in hand; a finding both make in other spans or fixes is recorded as
// moved, one the original offers a fix on that the port leaves out as withheld, and one whose related entry the port
// leaves out as omitted, each pairing the two sides' findings and nothing more; and every record is matched by a
// finding on its own line, never another's. A `finds:` record matches by line and message, so a finding that moved
// along its line still meets it, and where it starts matters, as for a finding with no end, whose start is all
// of its place, the record names the column, `finds at column <column>:`, and is met only there, and where its span
// matters too, its end, `finds at column <column> to <end column>:`, with `<end line>:` before the end column on
// another line, or `finds at column <column>, no end:`, met only by a finding that ends there or has no end. A
// finding the original makes and the port does not, unrecorded, is a regression and fails, and so does any fix or
// related entry one side alone offers that no record withholds or omits.
// A finding the port alone makes, recorded as found, offers fixes only as the fixture records them: each edit of each
// fix is a `fixes:` record of where it lands and its new text, on the `finds:` record's line or the line after it, and
// every `fixes:` record is met by an edit of a finding that record covers, both ways as a multiset by the file and line
// the edit starts on and its text, and by its fix's message where the record names one, `fixes with "<message>":`, so
// a fix the port offers there unrecorded fails, and so does one recorded that it
// does not offer, or offers elsewhere, and one with no edits, which no record can stand for. No original's edit is
// compared with a lone finding's, so the record holds where it lands as well as what it writes.
// Every record must give its reason, a drop on the line directly above it and any other there or in the doc comment
// of the declaration it is in, but in an assembly file, which has no declarations, on the line directly above it
// alone, and one with none fails, named by file and line. A `// port ` comment it cannot read fails, and so does a
// fixture that records nothing, and a `finds nothing` record, since where the original runs it has nothing to hold.
// The census has no fixture, so no record covers anything there.
//
// A file that cannot carry a comment, as x/tools' fixtures copied byte for byte cannot, takes its records beside the
// call, as given: each a Record, held as a comment record is, matched by a finding on its own line, met, and one with
// no reason failing, named by file and line. Its Reason is its own, so a drop given there holds the drop's rule. The
// test logs how many fixes the records withhold and related entries they omit beside each side's count, so the counts
// read as one line.
//
// A `moves:` record pairs by line and message only, so its fix there is held by a fixed file beside the fixture's
// file, `<name>.go.fixed`, which every fix the port offers in that file, on a finding both make or one it alone makes,
// applied, must make byte for byte, unformatted (see requireFixedFiles), or else by the port's own tests, with a
// golden file or by applying the edit and checking the result. A fixed file also pins what a `fixes:` record cannot,
// each edit's exact span and the source it leaves, and beside one a move must prove the original's fix wrong. A
// `moves:` record that places the port's side, `moves at column <column>:` with any end, or names the message of a fix
// it offers, `moves with "<message>":`, holds the port's finding to that too (see requireMovesAsRecorded), and one
// that does neither pairs as before. It reads the one package directory fixture names, not the packages
// nested below it, so a fixture tree with nested packages takes one call for each. It loads for the machine's own
// target, and Target.RequireOnlyAdjudicated for another, and reads the records of every Go and assembly file of that
// directory (see recordFiles), whichever target builds it, so a record in a file the target does not build fails
// unmatched.
func RequireOnlyAdjudicated(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
	given ...Record,
) {
	test.Helper()
	requireOnlyAdjudicated(test, Target{}, original, port, testdata, fixture, "", given)
}

// Record is a record of a differences fixture's given beside the call, to RequireOnlyAdjudicated or
// RequirePortWhereOriginalPanics, or a rename given to Renaming, for a file that cannot carry it as a comment: File,
// the file as findings name it, by its path below the fixtures' root, as `typeparams/typeparams.go`; Line, the line it
// is on; Text, the record as a comment writes it after `// port `, as `withholds: Format the number as a decimal`; and
// Reason, why, as the comment above a record says it.
type Record struct {
	File   string
	Line   int
	Text   string
	Reason string
}

// requireOnlyAdjudicated is RequireOnlyAdjudicated with the fixtures loaded for target, and run with userCacheDirectory
// as the user's cache directory where it is not empty (see RequireOnlyAdjudicatedUnderUserCacheDirectory).
func requireOnlyAdjudicated(
	test testing.TB,
	target Target,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
	userCacheDirectory string,
	given []Record,
) {
	test.Helper()
	defer target.held(test)()
	loads := loadFixturesHoldingFixed(test, target, testdata, []string{fixture}, nil)
	var originalFindings []Finding
	var portFindings []Finding
	runUnderUserCacheDirectory(test, userCacheDirectory, loads, func() {
		originalFindings, portFindings = findingsOver(test, original, port, loads)
	})
	folder := recordsFolder(test, loads[0])
	recorded := adjudicated(test, folder, loads[0].fileName, given)
	test.Logf("the original found %d, the port %d, and the records withhold %d fixes and omit %d related entries",
		len(originalFindings), len(portFindings), len(recorded.withheld), len(recorded.omitted))
	for _, record := range recorded.findsNothing {
		test.Errorf("%s:%d records `finds nothing`, which holds only where the original panics: call "+
			"RequirePortWhereOriginalPanics", record.File, record.Line)
	}
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
	originalOnly, portOnly, moved := pairMoved(test, recorded.moved, originalOnly, portOnly)
	requireMovesAsRecorded(test, moved)
	requireRecordedFixes(test, recorded, portOnly, editLines(loads))
	requireFixedFiles(test, folder, loads[0].fileName, fixture, portFindings, moved)
	portAlone := asRecorded(portOnly)
	for _, unrecorded := range Difference(portAlone, recorded.portAlone) {
		test.Errorf("the port alone found %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range Difference(recorded.portAlone, portAlone) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", unmatched)
	}
	requirePlaced(test, "the port alone finds", recorded.placed, portOnly)
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
// port alone finds, from `finds:` and the port's side of `renames:`; found, the `finds:` records alone of those;
// renamed, the original's side of `renames:`; and dropped, what the original alone finds and the port rightly does
// not, from `drops:`; moved, a finding both make in other spans or fixes, from `moves:`; withheld, from `withholds:`,
// whose Message is the fix's message, not the finding's; omitted, from `omits:`, whose Message is the related
// entry's; fixed, from `fixes:`, each on the line of the `finds:` record it follows; findsNothing, from `finds
// nothing`, each where it is written with no Message, since it names no finding; and placed, the `finds at column`
// records of found, each with its Column and any end it holds, which their copies in portAlone and found leave 0.
type records struct {
	portAlone    []Finding
	found        []Finding
	placed       []placedFind
	renamed      []Finding
	dropped      []Finding
	moved        []movedRecord
	withheld     []Finding
	omitted      []Finding
	fixed        []recordedFix
	findsNothing []Finding
}

// recordedFix is one edit of a fix on a finding the port alone makes, as a `fixes:` record names it and as the port
// offers it: the finding's file and line, the file and line the edit starts on, each file named as findings name
// theirs, the text it writes, and, where holdsMessage, the message of its fix, as the port always offers one and a
// record names one only when written.
type recordedFix struct {
	findingFile  string
	findingLine  int
	editFile     string
	editLine     int
	text         string
	message      string
	holdsMessage bool
}

// editLines is the line an edit starts on, read from the files of the packages loads loaded, each by the name findings
// give it, and whether the edit's file is one of them.
func editLines(loads []loadedFixtures) func(edit Edit) (int, bool) {
	files := map[string]*token.File{}
	for _, fixtures := range loads {
		packages.Visit(fixtures.roots, nil, func(loaded *packages.Package) {
			for _, syntax := range loaded.Syntax {
				if file := loaded.Fset.File(syntax.FileStart); file != nil {
					files[fixtures.fileName(file.Name())] = file
				}
			}
		})
	}
	return func(edit Edit) (int, bool) {
		file, isLoaded := files[edit.File]
		if !isLoaded || edit.Start > file.Size() {
			return 0, false
		}
		return file.Line(file.Pos(edit.Start)), true
	}
}

// requireRecordedFixes fails the test unless the edits of the fixes the port offers on its lone findings, portOnly,
// that a `finds:` record covers are the fixture's `fixes:` records, as a multiset by the finding, the file and line
// the edit starts on, read by lineOf, and the edit's new text, naming each edit no record holds and each record no
// edit meets. An edit in a file no fixture package holds fails, since where it lands cannot be read, and so does a fix
// with no edits, named by its finding and its message, since records count edits and none can stand for it. The
// port's side of a renaming is a finding both make, so its fixes are not held here.
func requireRecordedFixes(
	test testing.TB,
	recorded records,
	portOnly []Finding,
	lineOf func(edit Edit) (int, bool),
) {
	test.Helper()
	found := map[string]bool{}
	for _, record := range recorded.found {
		found[record.key()] = true
	}
	offered := []recordedFix{}
	for _, finding := range portOnly {
		if !found[Finding{File: finding.File, Line: finding.Line, Message: finding.Message}.key()] {
			continue
		}
		for _, fix := range finding.Fixes {
			if len(fix.Edits) == 0 {
				test.Errorf("the port alone fixes its finding at %s:%d with %q, a fix with no edits, which no "+
					"`fixes:` record can stand for", finding.File, finding.Line, fix.Message)
			}
			for _, edit := range fix.Edits {
				line, isRead := lineOf(edit)
				if !isRead {
					test.Errorf("the port alone fixes its finding at %s:%d with an edit in %s at offset %d, which no "+
						"fixture package holds, so where it lands cannot be read", finding.File, finding.Line,
						edit.File, edit.Start)
					continue
				}
				offered = append(offered, recordedFix{
					findingFile:  finding.File,
					findingLine:  finding.Line,
					editFile:     edit.File,
					editLine:     line,
					text:         edit.NewText,
					message:      fix.Message,
					holdsMessage: true,
				})
			}
		}
	}
	// The records that name a message are met first, by edits whole, and the rest by edits with their messages set
	// aside, so a record with none never takes the edit one with a message needs.
	messaged := slices.DeleteFunc(slices.Clone(recorded.fixed), func(record recordedFix) bool {
		return !record.holdsMessage
	})
	unmessaged := slices.DeleteFunc(slices.Clone(recorded.fixed), func(record recordedFix) bool {
		return record.holdsMessage
	})
	notMessaged := []recordedFix{}
	for _, edit := range countedWithout(offered, messaged) {
		edit.message, edit.holdsMessage = "", false
		notMessaged = append(notMessaged, edit)
	}
	for _, unrecorded := range countedWithout(notMessaged, unmessaged) {
		test.Errorf("the port alone fixes its finding at %s:%d writing %q at %s:%d, which the fixture does not record",
			unrecorded.findingFile, unrecorded.findingLine, unrecorded.text, unrecorded.editFile, unrecorded.editLine)
	}
	for _, unmatched := range countedWithout(messaged, offered) {
		test.Errorf("the fixture records that the port alone fixes its finding at %s:%d with %q writing %q at %s:%d, "+
			"and it does not", unmatched.findingFile, unmatched.findingLine, unmatched.message, unmatched.text,
			unmatched.editFile, unmatched.editLine)
	}
	for _, unmatched := range countedWithout(unmessaged, notMessaged) {
		test.Errorf("the fixture records that the port alone fixes its finding at %s:%d writing %q at %s:%d, and it "+
			"does not", unmatched.findingFile, unmatched.findingLine, unmatched.text, unmatched.editFile,
			unmatched.editLine)
	}
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

// movedRecord is a `moves:` record: at, its file, line and message, by which it pairs; where isPlaced, placed, where
// the port's side of the move sits, as a `finds at column` record places a finding; and where holdsFixMessage,
// fixMessage, the message of a fix the port's side offers.
type movedRecord struct {
	at              Finding
	placed          placedFind
	isPlaced        bool
	fixMessage      string
	holdsFixMessage bool
}

// movedPair is the two findings a `moves:` record pairs, the original's and the port's, beside the record.
type movedPair struct {
	record   movedRecord
	original Finding
	port     Finding
}

// pairMoved takes out of originalOnly and portOnly each pair a moved record covers, a finding of each side's on the
// record's line with the record's message, which differ in span or fixes, and returns the pairs beside what remains.
// A record that pairs nothing fails. It pairs by line and message alone and proves nothing of the port's fix, which a
// fixed file holds (see requireFixedFiles), or the port's own tests, with a golden file or by applying the edit and
// checking the result.
func pairMoved(
	test testing.TB,
	moved []movedRecord,
	originalOnly []Finding,
	portOnly []Finding,
) (remainingOriginal []Finding, remainingPort []Finding, pairs []movedPair) {
	test.Helper()
	// Cloned, since a pair is deleted from each in place.
	remainingOriginal, remainingPort = slices.Clone(originalOnly), slices.Clone(portOnly)
	onRecordedLine := func(record Finding) func(Finding) bool {
		return func(finding Finding) bool {
			return finding.File == record.File && finding.Line == record.Line && finding.Message == record.Message
		}
	}
	for _, record := range moved {
		originalIndex := slices.IndexFunc(remainingOriginal, onRecordedLine(record.at))
		portIndex := slices.IndexFunc(remainingPort, onRecordedLine(record.at))
		if originalIndex < 0 || portIndex < 0 {
			test.Errorf("the fixture records that the port moves %s, and the two do not both find it there in "+
				"other spans or fixes", record.at)
			continue
		}
		pairs = append(pairs, movedPair{
			record:   record,
			original: remainingOriginal[originalIndex],
			port:     remainingPort[portIndex],
		})
		remainingOriginal = slices.Delete(remainingOriginal, originalIndex, originalIndex+1)
		remainingPort = slices.Delete(remainingPort, portIndex, portIndex+1)
	}
	return remainingOriginal, remainingPort, pairs
}

// requireMovesAsRecorded fails the test for each of pairs whose record places the port's finding where it does not
// sit, as requirePlaced holds a `finds at column` record, or names the message of a fix the port's finding does not
// offer, each named by the finding's file and line. A record that does neither holds nothing here.
func requireMovesAsRecorded(test testing.TB, pairs []movedPair) {
	test.Helper()
	for _, pair := range pairs {
		if pair.record.isPlaced && !pair.record.placed.isMetBy(pair.port) {
			test.Errorf("the fixture records that the port's moved finding at %s:%d %s, and it does not",
				pair.port.File, pair.port.Line, pair.record.placed.placement())
		}
		offers := slices.ContainsFunc(pair.port.Fixes, func(fix Fix) bool {
			return fix.Message == pair.record.fixMessage
		})
		if pair.record.holdsFixMessage && !offers {
			test.Errorf("the fixture records that the port's moved finding at %s:%d offers a fix %q, and it does not",
				pair.port.File, pair.port.Line, pair.record.fixMessage)
		}
	}
}

// recordFiles are the files of a differences fixture's folder that can hold its records, by pattern: its Go files, and
// its assembly files, the Go assembler's, whose line comments open with `//` as Go's do.
var recordFiles = []string{"*.go", "*.s"}

// adjudicated reads every `// port ` comment of the Go and assembly files in directory (see recordFiles), each record
// on the comment's line, its file named by fileName as the findings name it, and fails each record that gives no
// reason (see lineReasons), and then the records given beside the call, each failed that gives no Reason. A `fixes:`
// record is put on the line of the `finds:` record it follows, on its own line or the line above, written as a comment
// or given, and one that follows none fails. It reads a Go file's comments as the parser finds them and an assembly
// file's as the assembler lexes them, so a `// port ` inside a string is no record. It reads that one directory, not
// those below it.
func adjudicated(test testing.TB, directory string, fileName func(string) string, given []Record) records {
	test.Helper()
	recorded := records{}
	fileSet := token.NewFileSet()
	for _, pattern := range recordFiles {
		names, globError := filepath.Glob(filepath.Join(directory, pattern))
		if globError != nil {
			test.Fatalf("listing the differences fixture %s: %v", directory, globError)
		}
		for _, name := range names {
			comments, reasoned := fixtureComments(test, fileSet, name)
			for _, comment := range comments {
				for _, record := range strings.Split(comment.text, "// port ")[1:] {
					match, text, isReadable := readRecord(record)
					at := Finding{File: fileName(name), Line: comment.line}
					switch {
					case !isReadable || reasoned.above(comment.line):
						// Unreadable, which fails below, or reasoned on the line above, as any record may be.
					case match[dropsGroup] != "":
						// A drop is ruled with its own finding in hand, so a declaration's doc, which often rules a
						// whole group of records, rules no drop.
						test.Errorf("%s:%d holds a drop with no reason: write why on the line directly above it, "+
							"since a doc comment rules no drop", at.File, at.Line)
					case reasoned.isDocumented == nil:
						test.Errorf("%s:%d holds a record with no reason: write why on the line directly above it, "+
							"since a file with no declarations has no doc comment to give it", at.File, at.Line)
					case !reasoned.isDocumented(comment.line):
						test.Errorf("%s:%d holds a record with no reason: write why on the line above it, or in "+
							"the doc comment of the declaration it is in", at.File, at.Line)
					}
					if !isReadable {
						test.Fatalf("%s:%d holds a comment the harness cannot read: %q", at.File, at.Line, record)
					}
					if placesAtZero(match) {
						test.Fatalf("%s:%d holds a record that places a finding at 0, where lines and columns count "+
							"from 1: %q", at.File, at.Line, record)
					}
					recorded = withRecord(recorded, at, match, text)
				}
			}
		}
	}
	for _, record := range given {
		match, text, isReadable := readRecord(record.Text)
		if !isReadable {
			test.Fatalf("%s:%d is given beside the call a record the harness cannot read: %q", record.File,
				record.Line, record.Text)
		}
		if placesAtZero(match) {
			test.Fatalf("%s:%d holds a record that places a finding at 0, where lines and columns count from 1: %q",
				record.File, record.Line, record.Text)
		}
		// A given record's reason is its own, as a drop's is, so it rules a drop as much as any other.
		if strings.TrimSpace(record.Reason) == "" {
			test.Errorf("%s:%d is given beside the call a record with no reason: write why in its Reason",
				record.File, record.Line)
		}
		recorded = withRecord(recorded, Finding{File: record.File, Line: record.Line}, match, text)
	}
	recorded.fixed = followingFinds(test, recorded.fixed, recorded.found)
	return recorded
}

// readRecord is written, a record's text after its `// port ` opening, read: the submatches of adjudicatedPattern,
// the new text of a `fixes:` record (see fixesText), and whether it could be read at all.
func readRecord(written string) (match []string, text string, isReadable bool) {
	match = adjudicatedPattern.FindStringSubmatch(written)
	if match == nil {
		return nil, "", false
	}
	for _, literal := range []string{match[fixesMessageGroup], match[movesFixMessageGroup]} {
		if _, unquoteError := strconv.Unquote(literal); literal != "" && unquoteError != nil {
			return nil, "", false
		}
	}
	if match[fixesTextGroup] == "" {
		return match, "", true
	}
	text, isReadable = fixesText(match[fixesTextGroup])
	return match, text, isReadable
}

// withRecord is recorded with the record read as match, and for a `fixes:` record text, at at's file and line, among
// the records of its kind. A `fixes:` record's edit is on at's line until followingFinds puts it on its finding's,
// unless the record names where it lands.
func withRecord(recorded records, at Finding, match []string, text string) records {
	switch {
	case match[findsGroup] != "":
		at.Message = match[findsGroup]
		recorded.portAlone = append(recorded.portAlone, at)
		recorded.found = append(recorded.found, at)
	case match[dropsGroup] != "":
		at.Message = match[dropsGroup]
		recorded.dropped = append(recorded.dropped, at)
	case match[movesGroup] != "":
		at.Message = match[movesGroup]
		recorded.moved = append(recorded.moved, movedRecord{at: at})
	case match[withholdsGroup] != "":
		at.Message = match[withholdsGroup]
		recorded.withheld = append(recorded.withheld, at)
	case match[omitsGroup] != "":
		at.Message = match[omitsGroup]
		recorded.omitted = append(recorded.omitted, at)
	case match[fixesTextGroup] != "":
		fix := recordedFix{findingFile: at.File, findingLine: at.Line, text: text}
		if match[fixesMessageGroup] != "" {
			// Ignored: readRecord refuses a record whose literal does not unquote, so this one does.
			fix.message, _ = strconv.Unquote(match[fixesMessageGroup])
			fix.holdsMessage = true
		}
		if match[fixesFileGroup] != "" {
			// The pattern holds the line to digits, so only a line past an int's range fails to convert, and none
			// that long is a file's.
			fix.editFile, fix.editLine = match[fixesFileGroup], atoiOrZero(match[fixesLineGroup])
		}
		recorded.fixed = append(recorded.fixed, fix)
	case match[findsNothingGroup] != "":
		recorded.findsNothing = append(recorded.findsNothing, at)
	case match[findsPlacedGroup] != "":
		at.Message = match[findsPlacedGroup]
		recorded.portAlone = append(recorded.portAlone, at)
		recorded.found = append(recorded.found, at)
		recorded.placed = append(recorded.placed, placedAt(at, match[findsColumnGroup], match[findsNoEndGroup],
			match[findsEndLineGroup], match[findsEndColumnGroup]))
	case match[movesPlacedGroup] != "":
		at.Message = match[movesPlacedGroup]
		moved := movedRecord{at: at, isPlaced: match[movesColumnGroup] != ""}
		if moved.isPlaced {
			moved.placed = placedAt(at, match[movesColumnGroup], match[movesNoEndGroup], match[movesEndLineGroup],
				match[movesEndColumnGroup])
		}
		if match[movesFixMessageGroup] != "" {
			// Ignored: readRecord refuses a record whose literal does not unquote, so this one does.
			moved.fixMessage, _ = strconv.Unquote(match[movesFixMessageGroup])
			moved.holdsFixMessage = true
		}
		recorded.moved = append(recorded.moved, moved)
	default:
		renamed := at
		renamed.Message = match[renamesFromGroup]
		recorded.renamed = append(recorded.renamed, renamed)
		at.Message = match[renamesToGroup]
		recorded.portAlone = append(recorded.portAlone, at)
	}
	return recorded
}

// followingFinds is each of fixed, a `fixes:` record, on the line of the `finds:` record of found it follows: one on
// its own line, or else one on the line above, where a line could not hold both, its edit on that finding's line
// unless it names where it lands. One that follows none fails, named by file and line, and is left out, since it names
// no finding's fix.
func followingFinds(test testing.TB, fixed []recordedFix, found []Finding) []recordedFix {
	test.Helper()
	onLine := func(file string, line int) func(Finding) bool {
		return func(record Finding) bool { return record.File == file && record.Line == line }
	}
	following := []recordedFix{}
	for _, record := range fixed {
		switch {
		case slices.ContainsFunc(found, onLine(record.findingFile, record.findingLine)):
		case slices.ContainsFunc(found, onLine(record.findingFile, record.findingLine-1)):
			record.findingLine--
		default:
			test.Errorf("%s:%d records `fixes:` with no `finds:` record on its line or the line above it, so it "+
				"names no finding's fix", record.findingFile, record.findingLine)
			continue
		}
		if record.editFile == "" {
			record.editFile, record.editLine = record.findingFile, record.findingLine
		}
		following = append(following, record)
	}
	return following
}

// placedAt is the finding at, as a record places it: at column, with noEnd its `, no end`, and at endLine and
// endColumn its end, each as written or empty, as adjudicatedPattern reads them. The pattern holds each to digits,
// and placesAtZero refuses one that reads as 0, past an int's range or written so.
func placedAt(at Finding, column string, noEnd string, endLine string, endColumn string) placedFind {
	placed := placedFind{at: at, holdsEnd: noEnd != "" || endColumn != ""}
	placed.at.Column = atoiOrZero(column)
	if endColumn != "" {
		placed.at.EndLine, placed.at.EndColumn = at.Line, atoiOrZero(endColumn)
	}
	if endLine != "" {
		placed.at.EndLine = atoiOrZero(endLine)
	}
	return placed
}

// placesAtZero is whether match, a record read, names a column, an end line or an end column of 0, written so or past
// an int's range, where lines and columns count from 1.
func placesAtZero(match []string) bool {
	placing := []string{match[findsColumnGroup], match[findsEndLineGroup], match[findsEndColumnGroup],
		match[movesColumnGroup], match[movesEndLineGroup], match[movesEndColumnGroup]}
	return slices.ContainsFunc(placing,
		func(written string) bool { return written != "" && atoiOrZero(written) == 0 })
}

// atoiOrZero is digits as an int, or 0 where they overflow one.
func atoiOrZero(digits string) int {
	number, convertError := strconv.Atoi(digits)
	if convertError != nil {
		return 0
	}
	return number
}

// directivePrefixes open the directives a comment can be, to the go command, the compiler or cohere, as `//go:build
// never` is. A directive explains nothing, so it is no record's reason.
var directivePrefixes = []string{"//go:", "//line ", "//cohere:"}

// lineReasons is where a file gives its records' reasons, the Square's term for every record. above is whether a
// comment that is neither a record nor a directive ends on the line directly above line, the one reason a drop
// takes, and isDocumented whether line is in a declaration with a doc comment, the top-level one or the one in a
// group, a reason for every record but a drop. Wave 1's fixtures give their reasons as the doc comment of the function
// the records are in. isDocumented is nil for a file with no declarations, as an assembly file, so every record there
// takes its reason from the line directly above it, as a drop does.
type lineReasons struct {
	above        func(line int) bool
	isDocumented func(line int) bool
}

// fixtureComments are the comments of the differences fixture's file named name, and where it gives its records'
// reasons: a Go file's comments as the parser finds them, and an assembly file's as the assembler lexes them (see
// assemblyComments), with no declaration to document a record.
func fixtureComments(test testing.TB, fileSet *token.FileSet, name string) ([]fixtureComment, lineReasons) {
	test.Helper()
	if filepath.Ext(name) == ".s" {
		content, readError := os.ReadFile(name)
		if readError != nil {
			test.Fatalf("reading the differences fixture: %v", readError)
		}
		comments, lexError := assemblyComments(name, content)
		if lexError != nil {
			test.Fatalf("reading the differences fixture: %v", lexError)
		}
		return comments, lineReasons{above: reasonedAbove(comments)}
	}
	file, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments|parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatalf("parsing the differences fixture: %v", parseError)
	}
	return goComments(fileSet, file), reasonedLines(fileSet, file)
}

// goComments are file's comments, in the order the parser found them.
func goComments(fileSet *token.FileSet, file *ast.File) []fixtureComment {
	comments := []fixtureComment{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			comments = append(comments, fixtureComment{
				line:    fileSet.Position(comment.Slash).Line,
				endLine: fileSet.Position(comment.End()).Line,
				text:    comment.Text,
			})
		}
	}
	return comments
}

// reasonedAbove is lineReasons.above over comments: whether one that is neither a record nor a directive ends on the
// line directly above line.
func reasonedAbove(comments []fixtureComment) func(line int) bool {
	reasonEndsOn := map[int]bool{}
	for _, comment := range comments {
		isDirective := slices.ContainsFunc(directivePrefixes, func(prefix string) bool {
			return strings.HasPrefix(comment.text, prefix)
		})
		if !isDirective && !strings.Contains(comment.text, "// port ") {
			reasonEndsOn[comment.endLine] = true
		}
	}
	return func(line int) bool { return reasonEndsOn[line-1] }
}

// reasonedLines is where the Go file file gives its records' reasons.
func reasonedLines(fileSet *token.FileSet, file *ast.File) lineReasons {
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
	return lineReasons{
		above: reasonedAbove(goComments(fileSet, file)),
		isDocumented: func(line int) bool {
			for _, each := range documented {
				if each.first <= line && line <= each.last {
					return true
				}
			}
			return false
		},
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

// engineOptions is the checker's options for every run of the harness: those every run of the engine's rules sets
// (see check.DispatchOptions), so a port runs in its tests as it runs in the engine. A dispatched rule's listener
// is called only where its rule runs, and a port that reads the engine's own representation's expression map has it
// only where its listener declares so: one that reads the map without declaring it finds nothing here, as it would in
// the engine, never more here than there.
func engineOptions() *checker.Options {
	return check.DispatchOptions()
}
