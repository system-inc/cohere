package parity

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"runtime"
	"slices"
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

// writing suggests replacing the call with text, the fix's message naming text, since the checker refuses two fixes
// of one message on a finding.
func writing(text string) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
			Message:   "write " + text,
			TextEdits: []analysis.TextEdit{{Pos: call.Pos(), End: call.End(), NewText: []byte(text)}},
		})
	}
}

// fixingWithNoEdits suggests a fix of message with no edits, which writes nothing anywhere.
func fixingWithNoEdits(message string) shape {
	return func(_ *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{Message: message})
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
	gainedMessages = map[string]string{"gained": "call to gained"}
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

func TestFindingsAreEachSidesInTheirPaths(test *testing.T) {
	test.Parallel()
	originalFindings, portFindings := Findings(test, original, port, analysistest.TestData(), "adjudicated")
	wantedOriginal := []Finding{
		{File: "adjudicated/adjudicated.go", Line: 11, Column: 2, Message: "call to shared"},
		{File: "adjudicated/adjudicated.go", Line: 13, Column: 2, Message: "call to renamed in the original's words"},
	}
	wantedPort := []Finding{
		{File: "adjudicated/adjudicated.go", Line: 11, Column: 2, Message: "call to shared"},
		{File: "adjudicated/adjudicated.go", Line: 12, Column: 2, Message: "call to gained"},
		{File: "adjudicated/adjudicated.go", Line: 13, Column: 2, Message: "call to renamed in the port's words"},
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
	twice := Finding{File: "same/same.go", Line: 1, Column: 1, Message: "call to shared"}
	other := Finding{File: "same/same.go", Line: 2, Column: 1, Message: "call to shared"}
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
		"the original found adjudicated/adjudicated.go:13:2: call to renamed in the original's words, and the port "+
			"did not",
		"the port found adjudicated/adjudicated.go:12:2: call to gained, and the original did not",
		"the port found adjudicated/adjudicated.go:13:2: call to renamed in the port's words, and the original did not",
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
	}), "the original found regression/regression.go:11:0: call to lost, and the port did not, a regression")
}

func TestRequireOnlyAdjudicatedFailsARecordThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unmatched")
	}),
		"the fixture records that the port alone finds unmatched/unmatched.go:10:0: call to shared, and it does not",
		"the fixture records that the port alone finds unmatched/unmatched.go:11:0: call to shared in other words, "+
			"and it does not",
		"the fixture records that the original alone finds unmatched/unmatched.go:11:0: call to shared, renamed, "+
			"and it does",
	)
}

func TestRequireOnlyAdjudicatedFailsAnUnrecordedDifference(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unrecorded")
	}), "the port alone found unrecorded/unrecorded.go:9:0: call to gained, which the fixture does not record")
}

func TestRequireOnlyAdjudicatedFailsAnUnreadableRecord(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unreadable")
	}), `unreadable/unreadable.go:7 holds a comment the harness cannot read: "found: call to gained"`)
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
	}), "the fixture records that the port drops drop_unmatched/drop_unmatched.go:8:0: call to shared, and the "+
		"original alone does not find it")
}

// A drop covers a finding on its own line and no other: one on the line above leaves the finding a regression, and
// the drop itself unmatched.
func TestRequireOnlyAdjudicatedFailsADropOnAnotherLine(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "drop_misplaced")
	}),
		"the original found drop_misplaced/drop_misplaced.go:10:0: call to lost, and the port did not, a regression",
		"the fixture records that the port drops drop_misplaced/drop_misplaced.go:9:0: call to lost, and the original "+
			"alone does not find it",
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
	}), "the fixture records that the port moves moved/moved.go:9:0: call to shared, and the two do not both find it")
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
	}), `the fixture records that the port withholds the fix "delete the call" at withheld/withheld.go:9, and no `+
		"finding")
}

// A withheld record covers only the original's fix: a fix the port alone offers is unrecorded on its side and leaves
// the original's finding a regression, and the record fails too.
func TestRequireOnlyAdjudicatedWithholdsOnlyTheOriginalsFix(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(sharedMessages, fixingShort(0)),
			analysistest.TestData(), "withheld")
	}),
		`the fixture records that the port withholds the fix "delete the call" at withheld/withheld.go:9, and no `+
			"finding",
		"the port alone found withheld/withheld.go:9:0: call to shared, which the fixture does not record",
		"the original found withheld/withheld.go:9:0: call to shared, and the port did not, a regression",
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
	}), `the fixture records that the port omits the related entry "the function called" at omitted/omitted.go:9, `+
		"and no")
}

// An omitted record covers only the original's related entry: one the port alone adds is unrecorded on its side and
// leaves the original's finding a regression, and the record fails too.
func TestRequireOnlyAdjudicatedOmitsOnlyTheOriginalsRelatedEntry(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(sharedMessages, relatingShort(0)),
			analysistest.TestData(), "omitted")
	}),
		`the fixture records that the port omits the related entry "the function called" at omitted/omitted.go:9, `+
			"and no",
		"the port alone found omitted/omitted.go:9:0: call to shared, which the fixture does not record",
		"the original found omitted/omitted.go:9:0: call to shared, and the port did not, a regression",
	)
}

// A record with no comment above it and no doc comment on the function it is in fails, named by file and line.
func TestRequireOnlyAdjudicatedFailsARecordWithNoReason(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "unreasoned")
	}), "unreasoned/unreasoned.go:8 holds a record with no reason: write why on the line above it, or in the doc "+
		"comment of the declaration it is in")
}

// A record on the line above is no reason for the one below it: the first record's reason is above it, and the
// second, with a record above it and no doc comment on its function, fails.
func TestRequireOnlyAdjudicatedTakesNoRecordAsAReason(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "record_above")
	}), "record_above/record_above.go:10 holds a record with no reason")
}

// A directive explains nothing, so one on the line above a record is no reason for it: each record here, with a
// `//go:` or a `//cohere:` directive above it and no doc comment on its function, fails.
func TestRequireOnlyAdjudicatedTakesNoDirectiveAsAReason(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "reason_directive")
	}),
		"reason_directive/reason_directive.go:9 holds a record with no reason",
		"reason_directive/reason_directive.go:11 holds a record with no reason",
	)
}

// Each kind of directive is no reason, a `//line ` one as much as a `//go:` or a `//cohere:` one, and a comment that
// explains is. The `//line ` directive names the line it is on, so it moves no position.
func TestReasonedLinesSkipsDirectives(test *testing.T) {
	test.Parallel()
	source := strings.Join([]string{
		"package directives",
		"",
		"func calls() {",
		"\t// A comment that explains.",
		"\t_ = 0 // port finds: explained",
		"\t//go:generate echo",
		"\t_ = 0 // port finds: after go",
		"\t//cohere:ignore",
		"\t_ = 0 // port finds: after cohere",
		"\t//line directives.go:11",
		"\t_ = 0 // port finds: after line",
		"}",
	}, "\n")
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "directives.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	reasons := reasonedLines(fileSet, file)
	for line, wanted := range map[int]bool{5: true, 7: false, 9: false, 11: false} {
		if reasons.above(line) != wanted {
			test.Errorf("the record on line %d is reasoned from above: %t, want %t", line, !wanted, wanted)
		}
	}
}

// A drop's reason is its own, on the line directly above it, never the doc comment of the function it is in: of two
// drops in a documented function, the one with a reason above passes and the one with none fails, named by file and
// line, while a find the doc rules passes.
func TestRequireOnlyAdjudicatedTakesADropsReasonOnlyFromAbove(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "drop_documented")
	}), "drop_documented/drop_documented.go:16 holds a drop with no reason: write why on the line directly above it")
}

// A reason on the line above passes, as in dropped, and for any record but a drop so does one in the doc comment of
// the enclosing function alone, as in adjudicated.
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
		{"the end", endingShort, "same/same.go:7:2-7:10: call to shared", "same/same.go:7:2-7:9: call to shared"},
		{
			"a related entry", relatingShort,
			"same/same.go:7:2: call to shared [related same/same.go:7:2-7:8: the function called]",
			"same/same.go:7:2: call to shared [related same/same.go:7:2-7:7: the function called]",
		},
		{
			"a fix", fixingShort,
			`same/same.go:7:2: call to shared [fix "delete the call": same/same.go[`,
			`same/same.go:7:2: call to shared [fix "delete the call": same/same.go[`,
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
	if originalEdit.File != "same/same.go" || originalEdit.End-originalEdit.Start != len("shared()") ||
		portEdit.Start != originalEdit.Start || portEdit.End != originalEdit.End-1 {
		test.Errorf("the edits are %+v and %+v, want same/same.go's call deleted whole by the original and short of "+
			"its last byte by the port", originalEdit, portEdit)
	}
}

// The differences fixture records a line and a message, so the port's lone findings match their records whatever
// their ends and related entries, and so does the renaming. Their fixes are held apart: the found call's fix, which
// the fixture does not record, fails alone, and the renamed call's, a finding both make, is not held.
func TestRequireOnlyAdjudicatedMatchesRecordsWithoutTheRestOfAFinding(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording,
			calling(originalMessages, endingShort(0), relatingShort(0)),
			calling(portMessages, endingShort(0), relatingShort(0)),
			analysistest.TestData(), "adjudicated")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording,
			calling(originalMessages, endingShort(0), relatingShort(0), fixingShort(0)),
			calling(portMessages, endingShort(0), relatingShort(0), fixingShort(0)),
			analysistest.TestData(), "adjudicated")
	}), `the port alone fixes its finding at adjudicated/adjudicated.go:12 writing "" at `+
		"adjudicated/adjudicated.go:12, which the fixture does not record")
}

// A finding the port alone makes offers exactly the fixes its records name, one on the line of its `finds:` record and
// one, written as a Go string, on the line after it.
func TestRequireOnlyAdjudicatedPassesRecordedFixes(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages, writing("fixed()")),
			analysistest.TestData(), "fixed")
	}))
}

// A fix the port offers on its lone finding that no record names fails, named by the finding's file and line and the
// text the fix writes.
func TestRequireOnlyAdjudicatedFailsAFixOfferedUnrecorded(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages),
			calling(gainedMessages, writing("fixed()"), writing("again()")), analysistest.TestData(), "fixed")
	}),
		`the port alone fixes its finding at fixed/fixed.go:10 writing "again()" at fixed/fixed.go:10, which the `+
			"fixture does not record",
		`the port alone fixes its finding at fixed/fixed.go:11 writing "again()" at fixed/fixed.go:11, which the `+
			"fixture does not record",
	)
}

// A fix recorded that the port does not offer fails, named the same way.
func TestRequireOnlyAdjudicatedFailsARecordedFixNotOffered(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages), analysistest.TestData(),
			"fixed")
	}),
		`the fixture records that the port alone fixes its finding at fixed/fixed.go:10 writing "fixed()" at `+
			"fixed/fixed.go:10, and it does not",
		`the fixture records that the port alone fixes its finding at fixed/fixed.go:11 writing "fixed()" at `+
			"fixed/fixed.go:11, and it does not",
	)
}

// A fix with no edits writes no text anywhere, so no `fixes:` record can stand for it, and one the port offers on its
// lone finding fails, named by the finding and the fix's message, where the same finding with no fix passes. Both
// sides offer it on the call both find, so only the found call's differs.
func TestRequireOnlyAdjudicatedFailsAFixWithNoEdits(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(originalMessages, fixingWithNoEdits("change nothing")),
			calling(portMessages, fixingWithNoEdits("change nothing")), analysistest.TestData(), "adjudicated")
	}), `the port alone fixes its finding at adjudicated/adjudicated.go:12 with "change nothing", a fix with no `+
		"edits, which no `fixes:` record can stand for")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(originalMessages), calling(portMessages), analysistest.TestData(),
			"adjudicated")
	}))
}

// A `fixes:` record neither on the line of a `finds:` record nor on the line after one names no finding's fix, and
// fails, named by its own file and line.
func TestRequireOnlyAdjudicatedFailsAFixesRecordThatFollowsNoFinds(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages), analysistest.TestData(),
			"fixes_unanchored")
	}), "fixes_unanchored/fixes_unanchored.go:11 records `fixes:` with no `finds:` record on its line or the line "+
		"above it")
}

// withheldBesideTheCall are records given beside the call for same, a file with no record in it, each withholding the
// original's fix on one of its two calls.
var withheldBesideTheCall = []Record{
	{File: "same/same.go", Line: 7, Text: "withholds: delete the call", Reason: "the first call's fix is ruled wrong"},
	{File: "same/same.go", Line: 8, Text: "withholds: delete the call", Reason: "the second call's fix is ruled wrong"},
}

// Records given beside the call for a file that holds none are met as the same records written as comments would be,
// and without them the fixture records nothing.
func TestRequireOnlyAdjudicatedMeetsRecordsGivenBesideTheCall(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, fixingShort(0)), calling(sharedMessages),
			analysistest.TestData(), "same", withheldBesideTheCall...)
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, fixingShort(0)), calling(sharedMessages),
			analysistest.TestData(), "same")
	}), "the differences fixture same records nothing")
}

// A drop given beside the call carries its own reason, so it is met as one with its reason on the line above is.
func TestRequireOnlyAdjudicatedMeetsADropGivenBesideTheCall(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(nil), analysistest.TestData(), "same",
			Record{File: "same/same.go", Line: 7, Text: "drops: call to shared", Reason: "ruled a false positive"},
			Record{File: "same/same.go", Line: 8, Text: "drops: call to shared", Reason: "ruled a false positive"})
	}))
}

// A record given beside the call that no finding meets fails, named by its file and line.
func TestRequireOnlyAdjudicatedFailsARecordGivenBesideTheCallThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(sharedMessages), analysistest.TestData(),
			"same", Record{File: "same/same.go", Line: 9, Text: "finds: call to shared", Reason: "planted unmet"})
	}), "the fixture records that the port alone finds same/same.go:9:0: call to shared, and it does not")
}

// A record given beside the call with no reason fails, named by its file and line, though it is met.
func TestRequireOnlyAdjudicatedFailsARecordGivenBesideTheCallWithNoReason(test *testing.T) {
	test.Parallel()
	unreasoned := slices.Clone(withheldBesideTheCall)
	unreasoned[0].Reason = " "
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, fixingShort(0)), calling(sharedMessages),
			analysistest.TestData(), "same", unreasoned...)
	}), "same/same.go:7 is given beside the call a record with no reason: write why in its Reason")
}

// fixingTheDeclaration is an analyzer that reports each call to gained, as the port does, with a fix that writes text
// before the name where gained is declared, so its edit lands away from the finding, as sigchanyzer's port fixes the
// declaration of a channel in another file.
func fixingTheDeclaration(text string) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "calling",
		Doc:  "calling reports each call to gained, fixed where gained is declared.",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					call, isCall := node.(*ast.CallExpr)
					if !isCall {
						return true
					}
					if identifier, isIdentifier := call.Fun.(*ast.Ident); isIdentifier && identifier.Name == "gained" {
						declared := pass.TypesInfo.Uses[identifier].Pos()
						pass.Report(analysis.Diagnostic{
							Pos:     call.Pos(),
							Message: "call to gained",
							SuggestedFixes: []analysis.SuggestedFix{{
								Message:   "write " + text,
								TextEdits: []analysis.TextEdit{{Pos: declared, End: declared, NewText: []byte(text)}},
							}},
						})
					}
					return true
				})
			}
			return nil, nil
		},
	}
}

// A fix whose edit lands in another file, recorded with where it lands, is met.
func TestRequireOnlyAdjudicatedPassesAFixRecordedWhereItLands(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), fixingTheDeclaration("renamed"),
			analysistest.TestData(), "fixes_elsewhere")
	}))
}

// The right text recorded at the finding's line, where the port's edit lands on the line gained is declared, fails,
// named both ways: the edit where it lands, and the record where it says.
func TestRequireOnlyAdjudicatedFailsAFixRecordedWhereItDoesNotLand(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), fixingTheDeclaration("fixed()"),
			analysistest.TestData(), "fixed")
	}),
		`the port alone fixes its finding at fixed/fixed.go:10 writing "fixed()" at fixed/fixed.go:5, which the `+
			"fixture does not record",
		`the port alone fixes its finding at fixed/fixed.go:11 writing "fixed()" at fixed/fixed.go:5, which the `+
			"fixture does not record",
		`the fixture records that the port alone fixes its finding at fixed/fixed.go:10 writing "fixed()" at `+
			"fixed/fixed.go:10, and it does not",
		`the fixture records that the port alone fixes its finding at fixed/fixed.go:11 writing "fixed()" at `+
			"fixed/fixed.go:11, and it does not",
	)
}

// foundBesideTheCall are records given beside the call for same, a file with no record in it: the port alone finds
// each of its two calls, and fixes each by writing fixed() in its place.
var foundBesideTheCall = []Record{
	{File: "same/same.go", Line: 7, Text: "finds: call to shared", Reason: "the first call is ruled found"},
	{File: "same/same.go", Line: 7, Text: "fixes: fixed()", Reason: "the first call's fix is ruled right"},
	{File: "same/same.go", Line: 8, Text: "finds: call to shared", Reason: "the second call is ruled found"},
	{File: "same/same.go", Line: 8, Text: "fixes at same/same.go:8: fixed()", Reason: "the second call's fix too"},
}

// A `fixes:` record given beside the call follows a `finds:` given there, met as written records are, and one whose
// text the port does not write fails, named by where it says the edit lands.
func TestRequireOnlyAdjudicatedHoldsFixesGivenBesideTheCall(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(nil), calling(sharedMessages, writing("fixed()")),
			analysistest.TestData(), "same", foundBesideTheCall...)
	}))
	unmet := slices.Clone(foundBesideTheCall)
	unmet[3].Text = "fixes at same/same.go:8: other()"
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(nil), calling(sharedMessages, writing("fixed()")),
			analysistest.TestData(), "same", unmet...)
	}),
		`the port alone fixes its finding at same/same.go:8 writing "fixed()" at same/same.go:8, which the fixture `+
			"does not record",
		`the fixture records that the port alone fixes its finding at same/same.go:8 writing "other()" at `+
			"same/same.go:8, and it does not",
	)
}

// assembling is calling, and also reports each line of a package's assembly files that calls a function messages
// names, as `CALL ·gained(SB)`, at the line's start, as asmdecl reports its findings, so a finding sits in assembly.
func assembling(messages map[string]string) *analysis.Analyzer {
	inGo := calling(messages)
	return &analysis.Analyzer{
		Name: "calling",
		Doc:  "calling reports each call to a function it names, in Go and in assembly.",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, name := range pass.OtherFiles {
				if !strings.HasSuffix(name, ".s") {
					continue
				}
				content, readError := pass.ReadFile(name)
				if readError != nil {
					return nil, readError
				}
				file := pass.Fset.AddFile(name, -1, len(content))
				file.SetLinesForContent(content)
				for index, line := range strings.Split(string(content), "\n") {
					for called, message := range messages {
						if strings.Contains(line, "CALL ·"+called+"(SB)") {
							pass.Reportf(file.LineStart(index+1), "%s", message)
						}
					}
				}
			}
			return inGo.Run(pass)
		},
	}
}

// A record in an assembly file is read as one in Go is, on its own line: the port's lone call to gained in assembled.s
// is met by the record there, beside the one in assembled.go.
func TestRequireOnlyAdjudicatedReadsRecordsInAssembly(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, assembling(originalMessages), assembling(portMessages),
			analysistest.TestData(), "assembled")
	}))
}

// The same fixture with the record in its assembly removed fails on the finding there, named by its file and line.
func TestRequireOnlyAdjudicatedFailsAnUnrecordedFindingInAssembly(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, assembling(originalMessages), assembling(portMessages),
			analysistest.TestData(), "assembled_unrecorded")
	}), "the port alone found assembled_unrecorded/assembled_unrecorded.s:4:0: call to gained, which the fixture does "+
		"not record")
}

// An assembly file has no declarations, so a record there with no comment on the line directly above it has no
// reason, and fails, named by file and line.
func TestRequireOnlyAdjudicatedFailsARecordInAssemblyWithNoReasonAbove(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, assembling(originalMessages), assembling(portMessages),
			analysistest.TestData(), "assembled_unreasoned")
	}), "assembled_unreasoned/assembled_unreasoned.s:4 holds a record with no reason: write why on the line directly "+
		"above it, since a file with no declarations has no doc comment to give it")
}

// startingLater starts the diagnostic later along its line than the call, by laterBy bytes.
func startingLater(laterBy token.Pos) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.Pos = call.Pos() + laterBy
	}
}

// A `finds:` record that names a column is met only by a finding that starts there, and one that names none by a
// finding anywhere on its line: the port's calls found at the call pass, and found one byte later fail on each record
// with a column, named with it, while the record with none is still met.
func TestRequireOnlyAdjudicatedHoldsAFindsRecordsColumn(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages), analysistest.TestData(),
			"found_at_column")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages, startingLater(1)),
			analysistest.TestData(), "found_at_column")
	}),
		"the fixture records that the port alone finds found_at_column/found_at_column.go:9:2: call to gained, and "+
			"no finding of the port's there starts at column 2",
		"the fixture records that the port alone finds found_at_column/found_at_column.go:10:6: call to gained, and "+
			"no finding of the port's there starts at column 6",
	)
}

// A column given beside the call is held as one written as a comment is.
func TestRequireOnlyAdjudicatedHoldsAColumnGivenBesideTheCall(test *testing.T) {
	test.Parallel()
	for _, column := range []string{"2", "3"} {
		failures := failuresOf(test, func(recording testing.TB) {
			RequireOnlyAdjudicated(recording, calling(nil), calling(sharedMessages), analysistest.TestData(), "same",
				Record{File: "same/same.go", Line: 7, Text: "finds at column " + column + ": call to shared",
					Reason: "the first call is ruled found, at the call"},
				Record{File: "same/same.go", Line: 8, Text: "finds: call to shared", Reason: "the second call too"})
		})
		if column == "2" {
			requireFailures(test, failures)
			continue
		}
		requireFailures(test, failures, "the fixture records that the port alone finds same/same.go:7:3: call to "+
			"shared, and no finding of the port's there starts at column 3")
	}
}

// A `finds at column` record that names an end is met only by a finding that starts and ends where it says, the end a
// column on the record's own line or a line and a column: the port's calls found over the whole call pass, and found
// ending one byte short, or with no end, fail on each record, named with where it says the finding ends.
func TestRequireOnlyAdjudicatedHoldsAFindsRecordsEnd(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages, endingShort(0)),
			analysistest.TestData(), "found_spanning")
	}))
	for _, shapes := range [][]shape{{endingShort(1)}, nil} {
		requireFailures(test, failuresOf(test, func(recording testing.TB) {
			RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages, shapes...),
				analysistest.TestData(), "found_spanning")
		}),
			"the fixture records that the port alone finds found_spanning/found_spanning.go:9:2-9:10: call to gained, "+
				"and no finding of the port's there starts at column 2 and ends at line 9, column 10",
			"the fixture records that the port alone finds found_spanning/found_spanning.go:10:2-10:10: call to "+
				"gained, and no finding of the port's there starts at column 2 and ends at line 10, column 10",
		)
	}
}

// A `finds at column` record that says its finding has no end is met only by one with none: the port's calls found
// with no end pass, and found with an end fail, named so.
func TestRequireOnlyAdjudicatedHoldsAFindsRecordsNoEnd(test *testing.T) {
	test.Parallel()
	noEnd := []Record{
		{File: "same/same.go", Line: 7, Text: "finds at column 2, no end: call to shared", Reason: "found at the call"},
		{File: "same/same.go", Line: 8, Text: "finds: call to shared", Reason: "the second call too"},
	}
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(nil), calling(sharedMessages), analysistest.TestData(), "same",
			noEnd...)
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(nil), calling(sharedMessages, endingShort(0)),
			analysistest.TestData(), "same", noEnd...)
	}), "the fixture records that the port alone finds same/same.go:7:2: call to shared, and no finding of the "+
		"port's there starts at column 2 and has no end")
}

// fixingWith suggests replacing the call with text, under message.
func fixingWith(message string, text string) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
			Message:   message,
			TextEdits: []analysis.TextEdit{{Pos: call.Pos(), End: call.End(), NewText: []byte(text)}},
		})
	}
}

// A `fixes:` record that names its fix's message, `fixes with "<message>":`, is met only by an edit of a fix with that
// message, and one that names none by an edit of any: the port's fixes under "write fixed()" pass, and the same edits
// under another message fail on the record that names it, named with the message, its edit left unrecorded, while the
// record with none is still met.
func TestRequireOnlyAdjudicatedHoldsAFixesRecordsMessage(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages), calling(gainedMessages, writing("fixed()")),
			analysistest.TestData(), "fixes_messaged")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages),
			calling(gainedMessages, fixingWith("replace the call", "fixed()")), analysistest.TestData(),
			"fixes_messaged")
	}),
		`the port alone fixes its finding at fixes_messaged/fixes_messaged.go:10 writing "fixed()" at `+
			"fixes_messaged/fixes_messaged.go:10, which the fixture does not record",
		`the fixture records that the port alone fixes its finding at fixes_messaged/fixes_messaged.go:10 with `+
			`"write fixed()" writing "fixed()" at fixes_messaged/fixes_messaged.go:10, and it does not`,
	)
}
