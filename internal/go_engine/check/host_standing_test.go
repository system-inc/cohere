package check

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// standingRule is a house rule named for name whose analyzer keys a slot of the host's entries and never runs: a
// package's standing bodies read the slot alone.
func standingRule(name string) rule.Rule {
	return houseRule(&analysis.Analyzer{Name: name, Doc: "keys a slot of a hand built entry"})
}

// neverStands is a build's judgment under which no function stands, for a test that never asks.
func neverStands(string, string, int) bool { return false }

// confining is the names of rules, as the set a HostStanding is told are confined.
func confining(rules ...rule.Rule) map[string]bool {
	confined := map[string]bool{}
	for _, each := range rules {
		confined[each.Name] = true
	}
	return confined
}

// A view's findings are the host's of that rule on that file from its own package's entry, raw (#e4rd5dw): every
// one in the entry's order, a finding the entry holds twice both times, a finding that edits another file with its
// edit, and none of another file's or another rule's. A file the entry compiled and the rule found nothing on is
// held, with no finding, which is the host's answer and not a doubt.
func TestAViewsFindingsAreTheHostsOfItsRuleAndFileRaw(test *testing.T) {
	test.Parallel()
	first, second := standingRule("first"), standingRule("second")
	twice := Finding{Rule: first.Name, File: "/project/a.go", Line: 7, Column: 2, EndLine: 7, EndColumn: 9,
		MessageId: "twice", Message: "held twice"}
	editing := Finding{Rule: first.Name, File: "/project/a.go", Line: 3, Column: 1, MessageId: "editing",
		Message: "edits another file", Suggestions: []Suggestion{{Message: "move it",
			Edits: []Edit{{File: "/project/b.go", Start: 4, End: 9, Text: "moved"}}}}}
	elsewhere := Finding{Rule: first.Name, File: "/project/b.go", Line: 1, Column: 1, MessageId: "elsewhere",
		Message: "on the other file"}
	others := Finding{Rule: second.Name, File: "/project/a.go", Line: 2, Column: 5, MessageId: "others",
		Message: "the other rule's"}
	host := handBuiltHost(map[string]handBuiltEntry{
		"p": {files: []string{"/project/a.go", "/project/b.go", "/project/quiet.go"},
			analyses: map[*analysis.Analyzer]restoredAnalysis{
				first.Analyzer:  {findings: []Finding{twice, elsewhere, editing, twice}},
				second.Analyzer: {findings: []Finding{others}},
			}},
	})
	view := host.Standing([]rule.Rule{first, second}, confining(first, second), neverStands).Of("p")
	if view == nil {
		test.Fatal("the host's package p has no standing bodies, though the host holds its entry")
	}
	expected := map[string]map[string][]rule.InheritedFinding{
		first.Name: {
			"/project/a.go":     {inheritedFinding(twice), inheritedFinding(editing), inheritedFinding(twice)},
			"/project/b.go":     {inheritedFinding(elsewhere)},
			"/project/quiet.go": {},
		},
		second.Name: {
			"/project/a.go":     {inheritedFinding(others)},
			"/project/b.go":     {},
			"/project/quiet.go": {},
		},
	}
	for ruleName, byFile := range expected {
		for filename, want := range byFile {
			findings, held := view.Findings(ruleName, filename)
			if !held || !reflect.DeepEqual(findings, want) {
				test.Errorf("%s on %s is held %t with\n%+v\nwant held with\n%+v", ruleName, filename, held, findings,
					want)
			}
		}
	}
	edited, _ := view.Findings(first.Name, "/project/a.go")
	if len(edited) != 3 || len(edited[1].Suggestions) != 1 || len(edited[1].Suggestions[0].Edits) != 1 ||
		edited[1].Suggestions[0].Edits[0].File != "/project/b.go" {
		test.Errorf("a.go's findings are %+v, want the second to carry its edit into b.go", edited)
	}
}

// A file two of the host's packages hold, p and `p [p.test]`, is taken by each package from its own entry (#e4rd5dw):
// a confined rule reads types, and the test variant's differ, so where the rule found one thing in p and another in
// the test variant, p's view answers p's and the test variant's view its own, never the lowest holder's, which is the
// host inheritance's choice for a rule that reads only syntax. A variant whose entry lost the rule answers nothing,
// though p beside it holds the rule's findings on the same file.
func TestAFileTwoHostPackagesHoldIsTakenFromTheViewsOwnPackage(test *testing.T) {
	test.Parallel()
	typed := standingRule("typed")
	plain := Finding{Rule: typed.Name, File: "/project/p.go", Line: 4, Column: 1, MessageId: "typed",
		Message: "as p types it"}
	variant := Finding{Rule: typed.Name, File: "/project/p.go", Line: 9, Column: 3, MessageId: "typed",
		Message: "as the test variant types it"}
	files := []string{"/project/p.go"}
	host := handBuiltHost(map[string]handBuiltEntry{
		"p": {files: files,
			analyses: map[*analysis.Analyzer]restoredAnalysis{typed.Analyzer: {findings: []Finding{plain}}}},
		"p [p.test]": {files: files,
			analyses: map[*analysis.Analyzer]restoredAnalysis{typed.Analyzer: {findings: []Finding{variant}}}},
		// A second variant, where the rule crashed and left no slot.
		"p [q.test]": {files: files, analyses: map[*analysis.Analyzer]restoredAnalysis{}},
	})
	if holders := host.holdersOf("/project/p.go"); len(holders) != 3 || holders[0] != "p" {
		test.Fatalf("p.go's holders are %q, want three, p the lowest", holders)
	}
	standing := host.Standing([]rule.Rule{typed}, confining(typed), neverStands)
	for identifier, want := range map[string]Finding{"p": plain, "p [p.test]": variant} {
		findings, held := standing.Of(identifier).Findings(typed.Name, "/project/p.go")
		if !held || !reflect.DeepEqual(findings, []rule.InheritedFinding{inheritedFinding(want)}) {
			test.Errorf("the view of %q holds %t\n%+v\nwant its own entry's\n%+v", identifier, held, findings, want)
		}
	}
	if findings, held := standing.Of("p [q.test]").Findings(typed.Name, "/project/p.go"); held {
		test.Errorf("the view of the variant that lost the rule holds %+v, want nothing, never p's", findings)
	}
}

// Every doubt answers nothing, so the build lints (#e4rd5dw). A package the host holds no entry of has no standing
// bodies at all, as a nil standing has none for any package, and the nil is one the checker hands on as nothing. Where
// the host holds the entry, a rule's findings are not held when the entry does not decode, when it holds no slot for
// the rule, when the host's package never compiled the file, when the rule is not among those enabled, and when it is
// enabled and not confined, and neither of the last two is confined.
func TestStandingBodiesFallClosed(test *testing.T) {
	test.Parallel()
	confined, lost, unconfined, disabled := standingRule("confined"), standingRule("lost"), standingRule("unconfined"),
		standingRule("disabled")
	finding := func(named rule.Rule) Finding {
		return Finding{Rule: named.Name, File: "/project/held.go", Line: 1, Column: 1, MessageId: "found",
			Message: "the host's"}
	}
	held := map[*analysis.Analyzer]restoredAnalysis{}
	for _, each := range []rule.Rule{confined, unconfined, disabled} {
		held[each.Analyzer] = restoredAnalysis{findings: []Finding{finding(each)}}
	}
	host := handBuiltHost(map[string]handBuiltEntry{
		"held":      {files: []string{"/project/held.go"}, analyses: held},
		"undecoded": {files: []string{"/project/held.go"}},
	})
	decodeError := errors.New("the entry does not decode")
	host.entries["undecoded"].decoded = func() (*restoredPackage, error) { return nil, decodeError }
	// disabled is named confined and lost is enabled and confined, so each falls for its one reason alone.
	standing := host.Standing([]rule.Rule{confined, lost, unconfined}, confining(confined, lost, disabled), neverStands)

	if view := standing.Of("unheld"); view != nil {
		test.Errorf("a package the host holds no entry of has the standing bodies %+v, want a nil interface", view)
	}
	var none *HostStanding
	if view := none.Of("held"); view != nil {
		test.Errorf("a nil standing answers %+v for a package, want a nil interface", view)
	}
	var unkept *HostRun
	if unkept.Standing([]rule.Rule{confined}, confining(confined), neverStands) != nil {
		test.Error("a host that kept nothing has standing bodies, want none")
	}

	view := standing.Of("held")
	if view == nil {
		test.Fatal("the package the host holds has no standing bodies")
	}
	if findings, isHeld := view.Findings(confined.Name, "/project/held.go"); !isHeld || len(findings) != 1 {
		test.Fatalf("the confined rule's findings on held.go are held %t, %+v, want its one, or no case below shows "+
			"a fall", isHeld, findings)
	}
	if !view.Confines(confined.Name) || !view.Confines(lost.Name) {
		test.Error("a rule enabled and named confined is not confined")
	}
	cases := []struct {
		name       string
		identifier string
		ruleName   string
		filename   string
	}{
		{"an entry that does not decode", "undecoded", confined.Name, "/project/held.go"},
		{"an entry with no slot for the rule", "held", lost.Name, "/project/held.go"},
		{"a file the host's package never compiled", "held", confined.Name, "/project/uncompiled.go"},
		{"a rule that is not enabled", "held", disabled.Name, "/project/held.go"},
		{"a rule enabled and not confined", "held", unconfined.Name, "/project/held.go"},
	}
	for _, each := range cases {
		if findings, isHeld := standing.Of(each.identifier).Findings(each.ruleName, each.filename); isHeld ||
			findings != nil {
			test.Errorf("%s: the findings are held %t, %+v, want not held and none", each.name, isHeld, findings)
		}
	}
	for _, each := range []rule.Rule{disabled, unconfined} {
		if view.Confines(each.Name) {
			test.Errorf("%s is confined, want only a rule both enabled and named confined", each.Name)
		}
	}
}

// A view asks the build's judgment with its own package identifier (#e4rd5dw): of one file two packages hold, the
// function at one offset stands in p and not in its test variant, and each view answers for its own.
func TestStandsIsAskedWithTheViewsOwnIdentifier(test *testing.T) {
	test.Parallel()
	host := handBuiltHost(map[string]handBuiltEntry{
		"p":          {files: []string{"/project/p.go"}},
		"p [p.test]": {files: []string{"/project/p.go"}},
	})
	type asked struct {
		packageIdentifier string
		filename          string
		offset            int
	}
	var lock sync.Mutex
	var asks []asked
	standing := host.Standing(nil, nil, func(packageIdentifier string, filename string, offset int) bool {
		lock.Lock()
		defer lock.Unlock()
		asks = append(asks, asked{packageIdentifier, filename, offset})
		return packageIdentifier == "p" && filename == "/project/p.go" && offset == 40
	})
	if !standing.Of("p").Stands("/project/p.go", 40) {
		test.Error("the function at 40 does not stand in p, though the build's judgment says it does")
	}
	if standing.Of("p [p.test]").Stands("/project/p.go", 40) {
		test.Error("the function at 40 stands in the test variant, which the build's judgment answers no for")
	}
	if standing.Of("p").Stands("/project/p.go", 41) {
		test.Error("the function at 41 stands in p, which the build's judgment answers no for")
	}
	expected := []asked{{"p", "/project/p.go", 40}, {"p [p.test]", "/project/p.go", 40}, {"p", "/project/p.go", 41}}
	if !slices.Equal(asks, expected) {
		test.Errorf("the build's judgment was asked %+v, want %+v", asks, expected)
	}
}

// A build counts the standing declarations it showed the host's findings in once however many rules took them, and
// each rule's findings (#e4rd5dw): two rules taking a.go's three declarations in p count three bodies and both rules'
// findings; a reload that takes the file again for one rule replaces that rule's findings and the file's
// declarations; the same file in p's test variant counts apart; another file adds its own; and a nil standing counts
// nothing.
func TestABuildCountsEachStandingBodyOnceAndEachRulesFindings(test *testing.T) {
	test.Parallel()
	first, second := standingRule("first"), standingRule("second")
	host := handBuiltHost(map[string]handBuiltEntry{
		"p":          {files: []string{"/project/a.go", "/project/b.go"}},
		"p [p.test]": {files: []string{"/project/a.go", "/project/b.go"}},
	})
	standing := host.Standing([]rule.Rule{first, second}, confining(first, second), neverStands)
	counted := func(step string, wantBodies int, wantFindings int) {
		test.Helper()
		if bodies, findings := standing.Counts(); bodies != wantBodies || findings != wantFindings {
			test.Errorf("%s: counted %d bodies and %d findings, want %d and %d", step, bodies, findings, wantBodies,
				wantFindings)
		}
	}
	counted("before any rule took a file", 0, 0)
	standing.Of("p").Took(first.Name, "/project/a.go", 3, 2)
	counted("one rule took a.go", 3, 2)
	standing.Of("p").Took(second.Name, "/project/a.go", 3, 1)
	counted("a second rule took a.go", 3, 3)
	standing.Of("p").Took(first.Name, "/project/a.go", 2, 1)
	counted("a reload took a.go again for the first rule", 2, 2)
	standing.Of("p [p.test]").Took(first.Name, "/project/a.go", 3, 4)
	counted("the test variant took a.go", 5, 6)
	standing.Of("p").Took(second.Name, "/project/b.go", 1, 0)
	counted("a rule took b.go, with no finding there", 6, 6)

	var none *HostStanding
	if bodies, findings := none.Counts(); bodies != 0 || findings != 0 {
		test.Errorf("a nil standing counted %d bodies and %d findings, want none", bodies, findings)
	}
}

// Every method may be called at once (#e4rd5dw): sixteen goroutines, as a build's actions are, each read a package's
// view, ask it everything a dispatcher asks and tell it a take, while reading the counts, and the counts end as the
// same takes told one after another would leave them. The counts are held under a lock, and everything else a view
// reads is never written once the standing is made. The land gate runs this package again under the race detector
// (raceGated), which is what catches a count read or written outside the lock.
func TestStandingBodiesTakenAndCountedAtOnceCountAsOneAfterAnother(test *testing.T) {
	test.Parallel()
	rules := []rule.Rule{standingRule("first"), standingRule("second")}
	identifiers := []string{"p", "p [p.test]"}
	files := []string{"/project/a.go", "/project/b.go"}
	entries := map[string]handBuiltEntry{}
	for _, identifier := range identifiers {
		analyses := map[*analysis.Analyzer]restoredAnalysis{}
		for _, each := range rules {
			analyses[each.Analyzer] = restoredAnalysis{findings: []Finding{
				{Rule: each.Name, File: files[0], Line: 1, Column: 1, MessageId: "found", Message: "the host's"},
			}}
		}
		entries[identifier] = handBuiltEntry{files: files, analyses: analyses}
	}
	standing := handBuiltHost(entries).Standing(rules, confining(rules...),
		func(_ string, _ string, offset int) bool { return offset == 0 })
	const rounds = 200
	var running sync.WaitGroup
	for worker := range 16 {
		running.Go(func() {
			// Sixteen workers over two packages, two rules and two files, so every take is told by two goroutines at
			// once.
			identifier, taking, filename := identifiers[worker%2], rules[worker/2%2], files[worker/4%2]
			for range rounds {
				view := standing.Of(identifier)
				if view == nil || !view.Stands(filename, 0) || !view.Confines(taking.Name) {
					test.Errorf("worker %d: the view of %q is %v, or nothing stands or confines there", worker,
						identifier, view)
					return
				}
				if findings, held := view.Findings(taking.Name, files[0]); !held || len(findings) != 1 {
					test.Errorf("worker %d: %s on %s is held %t with %+v, want the host's one", worker, taking.Name,
						files[0], held, findings)
					return
				}
				view.Took(taking.Name, filename, 2, 1)
				if bodies, findings := standing.Counts(); bodies < 2 || findings < 1 {
					test.Errorf("worker %d: counted %d bodies and %d findings after its own take", worker, bodies,
						findings)
					return
				}
			}
		})
	}
	running.Wait()
	// Two packages each hold two files of two declarations, and each of the two rules took every one of them once.
	if bodies, findings := standing.Counts(); bodies != 8 || findings != 8 {
		test.Errorf("counted %d bodies and %d findings, want 8 and 8", bodies, findings)
	}
}

// A rule takes the host's through its package's view as the dispatchers do (#e4rd5dw), through rule.StandingSpans,
// rule.StandingFiles and rule.ReportStanding: of a file whose first function stands and whose second the build's
// difference reaches, with a finding of the host's in each, the rule is confined on the file over the one standing
// declaration, reports the host's finding inside it alone, and the build counts one body and one finding.
func TestARuleTakesTheHostsInAStandingBodyThroughItsPackagesView(test *testing.T) {
	test.Parallel()
	const filename = "/project/seam.go"
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, filename,
		"package seam\n\nfunc Standing() {\n\t_ = 1\n}\n\nfunc Reached() {\n\t_ = 2\n}\n", 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	standingOffset := fileSet.Position(file.Decls[0].Pos()).Offset
	taking := standingRule("taking")
	host := handBuiltHost(map[string]handBuiltEntry{
		"seam": {files: []string{filename},
			analyses: map[*analysis.Analyzer]restoredAnalysis{taking.Analyzer: {findings: []Finding{
				{File: filename, Line: 4, Column: 2, MessageId: "found", Message: "in the standing body"},
				{File: filename, Line: 8, Column: 2, MessageId: "found", Message: "in the reached body"},
			}}}},
	})
	standing := host.Standing([]rule.Rule{taking}, confining(taking),
		func(packageIdentifier string, asked string, offset int) bool {
			return packageIdentifier == "seam" && asked == filename && offset == standingOffset
		})
	reported := []string{}
	pass := &analysis.Pass{Analyzer: taking.Analyzer, Fset: fileSet, Files: []*ast.File{file},
		ResultOf: map[*analysis.Analyzer]any{rule.Standing: standing.Of("seam")},
		Report: func(diagnostic analysis.Diagnostic) {
			reported = append(reported, diagnostic.Message)
		}}
	spans := rule.StandingSpans(pass)
	tokenFile := fileSet.File(file.FileStart)
	if len(spans) != 1 || len(spans[tokenFile]) != 1 || spans[tokenFile][0].Start != file.Decls[0].Pos() {
		test.Fatalf("the standing spans are %+v, want the first function's alone", spans)
	}
	confined, diagnostics := rule.StandingFiles(pass, taking.Name, spans, nil)
	if len(confined) != 1 || len(confined[tokenFile]) != 1 || len(diagnostics) != 1 {
		test.Fatalf("the rule is confined on %+v with %+v, want the file's one span and the host's one finding in it",
			confined, diagnostics)
	}
	if bodies, findings := standing.Counts(); bodies != 0 || findings != 0 {
		test.Errorf("counted %d bodies and %d findings before the rule reported any, want none", bodies, findings)
	}
	rule.ReportStanding(pass, taking.Name, confined, diagnostics)
	if !slices.Equal(reported, []string{"in the standing body"}) {
		test.Errorf("the rule reported %q, want the host's finding in the standing body alone", reported)
	}
	if bodies, findings := standing.Counts(); bodies != 1 || findings != 1 {
		test.Errorf("counted %d bodies and %d findings, want one of each", bodies, findings)
	}
}
