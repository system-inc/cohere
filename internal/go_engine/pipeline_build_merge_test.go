package go_engine

import (
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
)

// The rules the merge tests use: one that merges if all, as U1000 does, and one that merges if any.
const (
	mergeAllRule = "cohere-go/U1000"
	mergeAnyRule = "cohere-go/SA4023"
)

// mergeTestStrategies is the merge's strategies for the tests: mergeAllRule merges if all.
var mergeTestStrategies = map[string]bool{mergeAllRule: true}

// saying is a said set of files.
func saying(files ...string) map[string]bool {
	said := map[string]bool{}
	for _, file := range files {
		said[file] = true
	}
	return said
}

// mergeTestFinding is a finding of rule in file at line 3, column 5.
func mergeTestFinding(rule string, file string) check.Finding {
	return check.Finding{Rule: rule, File: file, Line: 3, Column: 5, Message: "field is unused"}
}

// unusedDirective is the unused finding a build writes of a directive in file naming rule.
func unusedDirective(file string, rule string) suppression.DirectiveFinding {
	return suppression.DirectiveFinding{
		File: file, Line: 7, Column: 2, Rule: suppression.RuleNoUnusedIgnore,
		MessageId: suppression.MessageIdUnused, Message: "This directive withholds nothing from " + rule + ".",
	}
}

// A mergeIfAll finding one build kept and another with a say on its file did not is gone, so code is unused only if
// it is unused in every build; the same finding where the other build has no say, since it does not compile the file,
// stands as the one build with a say has it.
func TestAMergeIfAllFindingStandsOnlyWhereEveryBuildWithASayKeepsIt(test *testing.T) {
	test.Parallel()
	found := mergeTestFinding(mergeAllRule, "shared.go")
	host := buildOutcome{said: saying("shared.go"), kept: []check.Finding{found}}

	both := mergeBuilds([]buildOutcome{host, {said: saying("shared.go")}}, mergeTestStrategies)
	if len(both.kept) != 0 {
		test.Errorf("a finding one build of two with a say kept stood: %+v", both.kept)
	}
	hostAlone := mergeBuilds([]buildOutcome{host, {said: saying("other.go")}}, mergeTestStrategies)
	if !reflect.DeepEqual(hostAlone.kept, []check.Finding{found}) {
		test.Errorf("the host's finding, where the other build has no say, became %+v", hostAlone.kept)
	}
}

// A finding whose position a `//line` directive maps to a file no build compiled, so no build has a say on it, has no
// build to disagree: a mergeIfAll finding there that one build of two kept stands, and so does a directive there
// that one build found unused.
func TestWhatNoBuildHasASayOnStandsAsAnyBuildSaysIt(test *testing.T) {
	test.Parallel()
	found := mergeTestFinding(mergeAllRule, "grammar.y")
	dead := unusedDirective("grammar.y", mergeAllRule)
	merged := mergeBuilds([]buildOutcome{
		{
			said:              saying("parser.go"),
			kept:              []check.Finding{found},
			directiveFindings: []suppression.DirectiveFinding{dead},
		},
		{said: saying("parser.go")},
	}, mergeTestStrategies)
	if !reflect.DeepEqual(merged.kept, []check.Finding{found}) {
		test.Errorf("kept %+v, where the finding no build has a say on was expected", merged.kept)
	}
	if !slices.Equal(merged.directiveFindings, []suppression.DirectiveFinding{dead}) {
		test.Errorf("directive findings %+v, where the unused one no build has a say on was expected",
			merged.directiveFindings)
	}
}

// Each build is counted the findings it brought: added, those that stand which it kept and the host's did not, each
// counted to every build beyond the host's that kept it; and dropped, those it kept that the merge left out.
func TestEachBuildIsCountedWhatItAddedAndWhatWasDropped(test *testing.T) {
	test.Parallel()
	shared := mergeTestFinding(mergeAnyRule, "shared.go")
	windowsOnly := mergeTestFinding(mergeAnyRule, "only_unix_windows.go")
	unusedOnWindows := mergeTestFinding(mergeAllRule, "shared.go")
	merged := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), kept: []check.Finding{shared}},
		{
			said: saying("shared.go", "only_unix_windows.go"),
			kept: []check.Finding{shared, windowsOnly, unusedOnWindows},
		},
		{said: saying("only_unix_windows.go"), kept: []check.Finding{windowsOnly}},
	}, mergeTestStrategies)
	if !slices.Equal(merged.added, []int{0, 1, 1}) || !slices.Equal(merged.dropped, []int{0, 1, 0}) {
		test.Errorf("added %v and dropped %v, where [0 1 1] and [0 1 0] were expected", merged.added, merged.dropped)
	}
}

// A mergeIfAny finding stands when any build with a say kept it, and one finding two builds kept is written once,
// the host's copy.
func TestAMergeIfAnyFindingStandsOnceWhereAnyBuildKeepsIt(test *testing.T) {
	test.Parallel()
	hostFound := mergeTestFinding(mergeAnyRule, "shared.go")
	extraFound := hostFound
	extraFound.EndLine = 9
	windowsOnly := mergeTestFinding(mergeAnyRule, "only_windows.go")
	merged := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), kept: []check.Finding{hostFound}},
		{said: saying("shared.go", "only_windows.go"), kept: []check.Finding{extraFound, windowsOnly, windowsOnly}},
	}, mergeTestStrategies)
	if !reflect.DeepEqual(merged.kept, []check.Finding{windowsOnly, hostFound}) {
		test.Errorf("kept %+v, where the windows-only finding and the host's copy of the shared one, each once, "+
			"were expected", merged.kept)
	}
}

// Two findings alike but in their column, or alike but in their message, are two findings: neither is folded into
// the other, so a mergeIfAll finding that one build reports at another column is not taken for the other's.
func TestFindingsDifferingInColumnOrMessageStayApart(test *testing.T) {
	test.Parallel()
	found := mergeTestFinding(mergeAnyRule, "shared.go")
	otherColumn := found
	otherColumn.Column = 6
	otherMessage := found
	otherMessage.Message = "another message"
	merged := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), kept: []check.Finding{found}},
		{said: saying("shared.go"), kept: []check.Finding{otherColumn, otherMessage}},
	}, mergeTestStrategies)
	if len(merged.kept) != 3 {
		test.Errorf("kept %d findings, where the three were expected apart: %+v", len(merged.kept), merged.kept)
	}
	allFound := mergeTestFinding(mergeAllRule, "shared.go")
	allOtherColumn := allFound
	allOtherColumn.Column = 6
	mergedAll := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), kept: []check.Finding{allFound}},
		{said: saying("shared.go"), kept: []check.Finding{allOtherColumn}},
	}, mergeTestStrategies)
	if len(mergedAll.kept) != 0 {
		test.Errorf("a mergeIfAll finding each build reports at another column stood: %+v", mergedAll.kept)
	}
}

// A directive is unused only if every build with a say on its file found it unused: dead in one build and alive in
// another, it is alive, and dead in the one build with a say, it is dead.
func TestADirectiveIsUnusedOnlyWhereEveryBuildWithASayFindsItUnused(test *testing.T) {
	test.Parallel()
	dead := unusedDirective("shared.go", mergeAnyRule)
	alive := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), directiveFindings: []suppression.DirectiveFinding{dead}},
		{said: saying("shared.go"), withheld: []check.Finding{mergeTestFinding(mergeAnyRule, "shared.go")}},
	}, mergeTestStrategies)
	if len(alive.directiveFindings) != 0 {
		test.Errorf("a directive one build needed was reported: %+v", alive.directiveFindings)
	}
	unused := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), directiveFindings: []suppression.DirectiveFinding{dead}},
		{said: saying("other.go")},
	}, mergeTestStrategies)
	if !slices.Equal(unused.directiveFindings, []suppression.DirectiveFinding{dead}) {
		test.Errorf("a directive dead in the one build with a say became %+v", unused.directiveFindings)
	}
	everywhere := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), directiveFindings: []suppression.DirectiveFinding{dead}},
		{said: saying("shared.go"), directiveFindings: []suppression.DirectiveFinding{dead}},
	}, mergeTestStrategies)
	if !slices.Equal(everywhere.directiveFindings, []suppression.DirectiveFinding{dead}) {
		test.Errorf("a directive dead in both builds became %+v, where it was expected once",
			everywhere.directiveFindings)
	}
}

// The consequence agreed on #72n9mq0, pinned so nobody reads it as a bug: a mergeIfAll finding one build withholds
// and the other does not report is gone from the result, and the directive that withheld it stays alive, since the
// one build needed it and removing it would bring the finding back in that build's own run.
func TestADirectiveOverAMergeIfAllFindingOneBuildNeedsStaysAlive(test *testing.T) {
	test.Parallel()
	found := mergeTestFinding(mergeAllRule, "shared.go")
	merged := mergeBuilds([]buildOutcome{
		{said: saying("shared.go"), withheld: []check.Finding{found}},
		{said: saying("shared.go"), directiveFindings: []suppression.DirectiveFinding{
			unusedDirective("shared.go", mergeAllRule),
		}},
	}, mergeTestStrategies)
	if len(merged.kept) != 0 || len(merged.directiveFindings) != 0 {
		test.Errorf("kept %+v and directive findings %+v, where neither was expected", merged.kept,
			merged.directiveFindings)
	}
	if merged.withheldByRule[mergeAllRule] != 1 {
		test.Errorf("withheld %v, where the one finding was expected", merged.withheldByRule)
	}
}

// A finding withheld in two builds counts once; a directive's other findings, which read the same in every build, are
// written once; and crashes and type errors are every build's, each once.
func TestWhatEveryBuildSaysAlikeIsWrittenOnce(test *testing.T) {
	test.Parallel()
	found := mergeTestFinding(mergeAnyRule, "shared.go")
	malformed := suppression.DirectiveFinding{
		File: "shared.go", Line: 2, Column: 1, Rule: suppression.RuleRequireIgnoreReason,
		MessageId: suppression.MessageIdMalformed, Message: "malformed",
	}
	shared := check.Crash{File: "shared.go", Error: "panicked"}
	windowsCrash := check.Crash{File: "only_windows.go", Error: "panicked"}
	sharedError := load.Diagnostic{File: "/workspace/shared.go", Line: 4, Column: 1, Message: "undefined: x"}
	windowsError := load.Diagnostic{File: "/workspace/only_windows.go", Line: 1, Column: 1, Message: "undefined: y"}
	build := buildOutcome{
		said:              saying("shared.go"),
		withheld:          []check.Finding{found},
		directiveFindings: []suppression.DirectiveFinding{malformed},
		crashes:           []check.Crash{shared},
		typeErrors:        []load.Diagnostic{sharedError},
	}
	extra := build
	extra.crashes = []check.Crash{shared, windowsCrash}
	extra.typeErrors = []load.Diagnostic{sharedError, windowsError}
	merged := mergeBuilds([]buildOutcome{build, extra}, mergeTestStrategies)
	if merged.withheldByRule[mergeAnyRule] != 1 {
		test.Errorf("withheld %v, where the one finding was expected once", merged.withheldByRule)
	}
	if !slices.Equal(merged.directiveFindings, []suppression.DirectiveFinding{malformed}) {
		test.Errorf("directive findings %+v, where the malformed one was expected once", merged.directiveFindings)
	}
	if !slices.Equal(merged.crashes, []check.Crash{windowsCrash, shared}) {
		test.Errorf("crashes %+v, where each build's were expected once", merged.crashes)
	}
	if !slices.Equal(merged.typeErrors, []load.Diagnostic{windowsError, sharedError}) {
		test.Errorf("type errors %+v, where each build's were expected once", merged.typeErrors)
	}
}
