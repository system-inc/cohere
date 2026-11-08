package parity

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// calling is an analyzer that reports each call to a function messages names, in that function's message, each
// diagnostic given whatever shapes add to it. The harness is proven on pairs of them, an original and a port, that
// agree and disagree exactly where the fixtures and the shapes say.
func calling(messages map[string]string, shapes ...shape) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "calling",
		Doc:  "calling reports each call to a function it names.",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					call, isCall := node.(*ast.CallExpr)
					if !isCall {
						return true
					}
					if identifier, isIdentifier := call.Fun.(*ast.Ident); isIdentifier {
						if message, named := messages[identifier.Name]; named {
							diagnostic := analysis.Diagnostic{Pos: call.Pos(), Message: message}
							for _, add := range shapes {
								add(call, &diagnostic)
							}
							pass.Report(diagnostic)
						}
					}
					return true
				})
			}
			return nil, nil
		},
	}
}

// shape adds to the diagnostic of a call what a planted pair differs in: an end, a related entry or a fix.
type shape func(call *ast.CallExpr, diagnostic *analysis.Diagnostic)

// endingShort ends the diagnostic short of the call's end by shortBy bytes.
func endingShort(shortBy token.Pos) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.End = call.End() - shortBy
	}
}

// relatingShort relates the function called, its range ending short of the name's end by shortBy bytes.
func relatingShort(shortBy token.Pos) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.Related = append(diagnostic.Related, analysis.RelatedInformation{
			Pos:     call.Fun.Pos(),
			End:     call.Fun.End() - shortBy,
			Message: "the function called",
		})
	}
}

// fixingShort suggests deleting the call, the edit ending short of the call's end by shortBy bytes.
func fixingShort(shortBy token.Pos) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
			Message:   "delete the call",
			TextEdits: []analysis.TextEdit{{Pos: call.Pos(), End: call.End() - shortBy}},
		})
	}
}

// originalMessages are the calls the original finds: to shared and lost, and to renamed in its words. portMessages
// are the port's: to shared and gained, and to renamed in other words.
var (
	originalMessages = map[string]string{
		"shared":  "call to shared",
		"lost":    "call to lost",
		"renamed": "call to renamed in the original's words",
	}
	portMessages = map[string]string{
		"shared":  "call to shared",
		"gained":  "call to gained",
		"renamed": "call to renamed in the port's words",
	}
	sharedMessages = map[string]string{"shared": "call to shared"}
)

// original and port are the pair most tests run, with no end, related entry or fix.
var (
	original = calling(originalMessages)
	port     = calling(portMessages)
)

// despiteErrors is analyzer run despite errors, as the checker runs one over what of an ill-typed package
// type-checked, so its findings there are counted unless the harness refuses the package.
func despiteErrors(analyzer *analysis.Analyzer) *analysis.Analyzer {
	running := *analyzer
	running.RunDespiteErrors = true
	return &running
}

// recorder is a testing.TB that keeps what a harness call failed with rather than failing the test, so a test can
// prove the harness fails where it must. Fatalf ends the goroutine, as testing's does, so failuresOf runs the call on
// one of its own. Every other method is the test's, and the harness calls only Logf and Helper of them.
type recorder struct {
	testing.TB
	failures []string
}

func (recording *recorder) Errorf(format string, arguments ...any) {
	recording.failures = append(recording.failures, fmt.Sprintf(format, arguments...))
}

func (recording *recorder) Fatalf(format string, arguments ...any) {
	recording.Errorf(format, arguments...)
	runtime.Goexit()
}

func (recording *recorder) Helper() {
	// Nothing to mark: a recorded failure names no line, so there is no caller to skip.
}

// failuresOf runs call with a recorder and returns what it failed with.
func failuresOf(test *testing.T, call func(testing.TB)) []string {
	test.Helper()
	recording := &recorder{TB: test}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		call(recording)
	}()
	<-finished
	return recording.failures
}

// requireFailures fails test unless failures are exactly one for each of wanted, each holding its text, in order.
func requireFailures(test *testing.T, failures []string, wanted ...string) {
	test.Helper()
	if len(failures) != len(wanted) {
		test.Fatalf("the harness failed %d times, want %d: %q", len(failures), len(wanted), failures)
	}
	for index, text := range wanted {
		if !strings.Contains(failures[index], text) {
			test.Errorf("failure %d is %q, want it to hold %q", index, failures[index], text)
		}
	}
}

func TestFindingsAreEachSidesInBaseNames(test *testing.T) {
	test.Parallel()
	originalFindings, portFindings := Findings(test, original, port, analysistest.TestData(), "adjudicated")
	wantedOriginal := []Finding{
		{File: "adjudicated.go", Line: 11, Column: 2, Message: "call to shared"},
		{File: "adjudicated.go", Line: 13, Column: 2, Message: "call to renamed in the original's words"},
	}
	wantedPort := []Finding{
		{File: "adjudicated.go", Line: 11, Column: 2, Message: "call to shared"},
		{File: "adjudicated.go", Line: 12, Column: 2, Message: "call to gained"},
		{File: "adjudicated.go", Line: 13, Column: 2, Message: "call to renamed in the port's words"},
	}
	if !reflect.DeepEqual(originalFindings, wantedOriginal) {
		test.Errorf("the original's findings are %v, want %v", originalFindings, wantedOriginal)
	}
	if !reflect.DeepEqual(portFindings, wantedPort) {
		test.Errorf("the port's findings are %v, want %v", portFindings, wantedPort)
	}
}

// Repeats count: a finding one side makes twice and the other once is a difference.
func TestDifferenceCountsRepeats(test *testing.T) {
	test.Parallel()
	twice := Finding{File: "same.go", Line: 1, Column: 1, Message: "call to shared"}
	other := Finding{File: "same.go", Line: 2, Column: 1, Message: "call to shared"}
	if difference := Difference([]Finding{twice, twice, other}, []Finding{twice}); !reflect.DeepEqual(
		difference, []Finding{twice, other},
	) {
		test.Errorf("the difference is %v, want %v", difference, []Finding{twice, other})
	}
}

func TestRequireUpstreamWantsPassesAPortThatMeetsThem(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWants(recording, port, analysistest.TestData(), "wants")
	}))
}

func TestRequireUpstreamWantsFailsAWantNothingFound(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWants(recording, original, analysistest.TestData(), "wants")
	}), "wants.go:11: no diagnostic was reported matching `call to gained`")
}

func TestRequireIdenticalPassesTheSameFindings(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, original, port, analysistest.TestData(), "same")
	}))
}

// Two empty sides match, and prove nothing.
func TestRequireIdenticalFailsWhenTheOriginalFindsNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, original, port, analysistest.TestData(), "silent")
	}), "the original found nothing")
}

func TestRequireIdenticalNamesEveryFindingOnOneSide(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, original, port, analysistest.TestData(), "adjudicated")
	}),
		"the original found adjudicated.go:13:2: call to renamed in the original's words, and the port did not",
		"the port found adjudicated.go:12:2: call to gained, and the original did not",
		"the port found adjudicated.go:13:2: call to renamed in the port's words, and the original did not",
	)
}

func TestRequireOnlyAdjudicatedPassesWhatTheFixtureRecords(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "adjudicated")
	}))
}

// The planted regression: lost is found by the original alone, and no record can cover it.
func TestRequireOnlyAdjudicatedFailsARegression(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "regression")
	}), "the original found regression.go:11:0: call to lost, and the port did not, a regression")
}

func TestRequireOnlyAdjudicatedFailsARecordThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unmatched")
	}),
		"the fixture records that the port alone finds unmatched.go:10:0: call to shared, and it does not",
		"the fixture records that the port alone finds unmatched.go:11:0: call to shared in other words, and it "+
			"does not",
		"the fixture records that the original alone finds unmatched.go:11:0: call to shared, renamed, and it does",
	)
}

func TestRequireOnlyAdjudicatedFailsAnUnrecordedDifference(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unrecorded")
	}), "the port alone found unrecorded.go:9:0: call to gained, which the fixture does not record")
}

func TestRequireOnlyAdjudicatedFailsAnUnreadableRecord(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unreadable")
	}), `unreadable.go:7 holds a comment the harness cannot read: "found: call to gained"`)
}

func TestRequireOnlyAdjudicatedFailsAFixtureThatRecordsNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "silent")
	}), "the differences fixture silent records nothing")
}

// A fixture with a type error fails before anything is compared. Both analyzers here run despite errors, so without
// the guard each would find the one call in illtyped, the two would agree, and the test would read green.
func TestFindingsFailsAFixtureThatDoesNotLoadClean(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, despiteErrors(original), despiteErrors(port), analysistest.TestData(), "illtyped")
	}), "fixture package illtyped does not load clean, so its findings would go uncounted: ")
}

// Each package that did not load clean is named once, with its first errors.
func TestLoadFailuresNamesEachIllTypedPackage(test *testing.T) {
	test.Parallel()
	loaded, loadError := load.Packages(analysistest.TestData(), []string{"same", "illtyped"},
		fixtureEnvironment(analysistest.TestData()), nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	failures := LoadFailures(loaded.Roots)
	if len(failures) != 1 || failures[0].Package != "illtyped" ||
		!strings.Contains(failures[0].Error, "illtyped.go:11") {
		test.Errorf("the load failures are %+v, want illtyped alone, with its error at illtyped.go:11", failures)
	}
}

// A finding only the original makes, recorded on its line as dropped, passes.
func TestRequireOnlyAdjudicatedPassesARecordedDrop(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "dropped")
	}))
}

// A drop recorded where the original makes no lone finding fails: here both sides find the call.
func TestRequireOnlyAdjudicatedFailsADropThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "drop_unmatched")
	}), "the fixture records that the port drops drop_unmatched.go:8:0: call to shared, and the original alone does "+
		"not find it")
}

// A drop covers a finding on its own line and no other: one on the line above leaves the finding a regression, and
// the drop itself unmatched.
func TestRequireOnlyAdjudicatedFailsADropOnAnotherLine(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "drop_misplaced")
	}),
		"the original found drop_misplaced.go:10:0: call to lost, and the port did not, a regression",
		"the fixture records that the port drops drop_misplaced.go:9:0: call to lost, and the original alone does "+
			"not find it",
	)
}

// A finding both make in other spans, recorded as moved, pairs, and so does one in other fixes.
func TestRequireOnlyAdjudicatedPairsAMove(test *testing.T) {
	test.Parallel()
	for _, shapedBy := range []func(shortBy token.Pos) shape{endingShort, fixingShort} {
		requireFailures(test, failuresOf(test, func(recording testing.TB) {
			RequireOnlyAdjudicated(recording, calling(sharedMessages, shapedBy(0)),
				calling(sharedMessages, shapedBy(1)), analysistest.TestData(), "moved")
		}))
	}
}

// A move recorded where the two find the same finding whole does not happen, and fails.
func TestRequireOnlyAdjudicatedFailsAMoveThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, endingShort(0)),
			calling(sharedMessages, endingShort(0)), analysistest.TestData(), "moved")
	}), "the fixture records that the port moves moved.go:9:0: call to shared, and the two do not both find it")
}

// The original's fix the port leaves out, recorded as withheld, pairs the two findings.
func TestRequireOnlyAdjudicatedPairsAWithheldFix(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, fixingShort(0)), calling(sharedMessages),
			analysistest.TestData(), "withheld")
	}))
}

// A withheld fix recorded where both offer it does not happen, and fails.
func TestRequireOnlyAdjudicatedFailsAWithholdThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, fixingShort(0)),
			calling(sharedMessages, fixingShort(0)), analysistest.TestData(), "withheld")
	}), `the fixture records that the port withholds the fix "delete the call" at withheld.go:9, and no finding`)
}

// A withheld record covers only the original's fix: a fix the port alone offers is unrecorded on its side and leaves
// the original's finding a regression, and the record fails too.
func TestRequireOnlyAdjudicatedWithholdsOnlyTheOriginalsFix(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(sharedMessages, fixingShort(0)),
			analysistest.TestData(), "withheld")
	}),
		`the fixture records that the port withholds the fix "delete the call" at withheld.go:9, and no finding`,
		"the port alone found withheld.go:9:0: call to shared, which the fixture does not record",
		"the original found withheld.go:9:0: call to shared, and the port did not, a regression",
	)
}

// The original's related entry the port leaves out, recorded as omitted, pairs the two findings.
func TestRequireOnlyAdjudicatedPairsAnOmittedRelatedEntry(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, relatingShort(0)), calling(sharedMessages),
			analysistest.TestData(), "omitted")
	}))
}

// An omitted related entry recorded where both carry it does not happen, and fails.
func TestRequireOnlyAdjudicatedFailsAnOmitThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, relatingShort(0)),
			calling(sharedMessages, relatingShort(0)), analysistest.TestData(), "omitted")
	}), `the fixture records that the port omits the related entry "the function called" at omitted.go:9, and no`)
}

// An omitted record covers only the original's related entry: one the port alone adds is unrecorded on its side and
// leaves the original's finding a regression, and the record fails too.
func TestRequireOnlyAdjudicatedOmitsOnlyTheOriginalsRelatedEntry(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(sharedMessages, relatingShort(0)),
			analysistest.TestData(), "omitted")
	}),
		`the fixture records that the port omits the related entry "the function called" at omitted.go:9, and no`,
		"the port alone found omitted.go:9:0: call to shared, which the fixture does not record",
		"the original found omitted.go:9:0: call to shared, and the port did not, a regression",
	)
}

// A record with no comment above it and no doc comment on the function it is in fails, named by file and line.
func TestRequireOnlyAdjudicatedFailsARecordWithNoReason(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unreasoned")
	}), "unreasoned.go:8 holds a record with no reason: write why on the line above it, or in the doc comment of the "+
		"declaration it is in")
}

// A record on the line above is no reason for the one below it: the first record's reason is above it, and the
// second, with a record above it and no doc comment on its function, fails.
func TestRequireOnlyAdjudicatedTakesNoRecordAsAReason(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "record_above")
	}), "record_above.go:10 holds a record with no reason")
}

// A reason on the line above passes, as in dropped, and so does one in the doc comment of the enclosing function
// alone, as in adjudicated and wave 1's fixtures.
func TestRequireOnlyAdjudicatedTakesEitherReason(test *testing.T) {
	test.Parallel()
	for _, fixture := range []string{"dropped", "adjudicated"} {
		requireFailures(test, failuresOf(test, func(recording testing.TB) {
			RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), fixture)
		}))
	}
}

// Each planted pair agrees in everything but one part of a finding, and RequireIdentical fails on that part alone,
// naming both calls on each side. With both sides shaped alike it passes, so what fails is the difference, not the
// shape.
func TestRequireIdenticalComparesEveryPartOfAFinding(test *testing.T) {
	test.Parallel()
	for _, planted := range []struct {
		name     string
		shapedBy func(shortBy token.Pos) shape
		original string
		port     string
	}{
		{"the end", endingShort, "same.go:7:2-7:10: call to shared", "same.go:7:2-7:9: call to shared"},
		{
			"a related entry", relatingShort,
			"same.go:7:2: call to shared [related same.go:7:2-7:8: the function called]",
			"same.go:7:2: call to shared [related same.go:7:2-7:7: the function called]",
		},
		{
			"a fix", fixingShort,
			`same.go:7:2: call to shared [fix "delete the call": same.go[`,
			`same.go:7:2: call to shared [fix "delete the call": same.go[`,
		},
	} {
		test.Run(planted.name, func(test *testing.T) {
			test.Parallel()
			requireFailures(test, failuresOf(test, func(recording testing.TB) {
				RequireIdentical(recording, calling(sharedMessages, planted.shapedBy(0)),
					calling(sharedMessages, planted.shapedBy(0)), analysistest.TestData(), "same")
			}))
			failures := failuresOf(test, func(recording testing.TB) {
				RequireIdentical(recording, calling(sharedMessages, planted.shapedBy(0)),
					calling(sharedMessages, planted.shapedBy(1)), analysistest.TestData(), "same")
			})
			requireFailures(test, failures,
				"the original found "+planted.original,
				"the original found "+strings.Replace(planted.original, "7:", "8:", 3),
				"the port found "+planted.port,
				"the port found "+strings.Replace(planted.port, "7:", "8:", 3),
			)
		})
	}
}

// A fix is compared by its edits' offsets in the file, which differ by the byte the port's edit ends short.
func TestFindingsRecordAFixByOffsets(test *testing.T) {
	test.Parallel()
	originalFindings, portFindings := Findings(test, calling(sharedMessages, fixingShort(0)),
		calling(sharedMessages, fixingShort(1)), analysistest.TestData(), "same")
	originalEdit := originalFindings[0].Fixes[0].Edits[0]
	portEdit := portFindings[0].Fixes[0].Edits[0]
	if originalEdit.File != "same.go" || originalEdit.End-originalEdit.Start != len("shared()") ||
		portEdit.Start != originalEdit.Start || portEdit.End != originalEdit.End-1 {
		test.Errorf("the edits are %+v and %+v, want same.go's call deleted whole by the original and short of its "+
			"last byte by the port", originalEdit, portEdit)
	}
}

// The differences fixture records a line and a message, so the port's lone findings match their records whatever
// their ends, related entries and fixes, and so does the renaming.
func TestRequireOnlyAdjudicatedMatchesRecordsWithoutTheRestOfAFinding(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording,
			calling(originalMessages, endingShort(0), relatingShort(0), fixingShort(0)),
			calling(portMessages, endingShort(0), relatingShort(0), fixingShort(0)),
			analysistest.TestData(), "adjudicated")
	}))
}
