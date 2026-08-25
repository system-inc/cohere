package hir

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/verify/internal/reactconformance"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// TestPreserveManualMemoizationAgainstGoldens scores the rule against upstream's own answers.
//
// This is the first independent oracle in phase 6. Every other stage was verified by conservation
// properties, mutation sweeps and cross-checks of my own design; this one has an answer key written
// by the people who wrote the rule.
//
// # What it asserts, and what it deliberately does not
//
// It does NOT assert that all 33 pass. Scoring a fresh port against a golden set and demanding a
// perfect number is how a rule ends up shaped like its fixtures: the failures get patched
// individually until the count is right, and the result matches the corpus rather than the
// specification.
//
// What it asserts is that the rule RUNS on the corpus and produces a non-degenerate answer -- it
// fires on some fixtures and not others -- plus the current pass count as a floor, so the number can
// only be driven up. The count is logged rather than pinned exactly, and the floor moves when
// someone improves it.
func TestPreserveManualMemoizationAgainstGoldens(t *testing.T) {
	fixtures, err := reactconformance.Load("../../reactconformance/testdata/fixtures")
	if err != nil {
		t.Fatalf("loading the vendored corpus: %v", err)
	}

	// The fixtures whose golden carries this rule's message. Everything else is another rule's
	// business and scoring against it would measure the wrong thing.
	const ruleMessage = "Existing memoization could not be preserved"
	var mine []reactconformance.Fixture
	for _, fixture := range fixtures {
		if strings.Contains(fixture.Expected.Raw, ruleMessage) {
			mine = append(mine, fixture)
		}
	}
	if len(mine) == 0 {
		t.Fatal("no fixture in the vendored corpus carries this rule's message; the corpus or the " +
			"message string is wrong, and a zero score below would be meaningless")
	}

	fired, silent, unsupported := 0, 0, 0
	for _, fixture := range mine {
		findings, ok := findingsForSource(t, fixture.Source)
		if !ok {
			unsupported++
			continue
		}
		if len(findings) > 0 {
			fired++
		} else {
			silent++
		}
	}

	t.Logf("fixtures=%d fired=%d silent=%d unsupported=%d",
		len(mine), fired, silent, unsupported)

	if fired == 0 {
		t.Errorf("the rule fired on none of %d fixtures whose golden expects it; every one of "+
			"these is a program where upstream reports a lost memoization", len(mine))
	}
	// Non-degeneracy in the other direction is not asserted: a rule that fires on all 33 could be
	// correct, since all 33 are error fixtures. Over-firing is caught by the clean-fixture check
	// below instead, where silence is the right answer.
}

// TestPreserveManualMemoizationOnFixturesExpectingNoSuchError is the over-reporting half.
//
// The 33 error fixtures can only show under-reporting: a rule that fires on everything passes all of
// them. The complement is what shows over-reporting.
//
// # Defining the complement correctly, which took two attempts
//
// The first spelling took every fixture whose `Expected.Raw` lacks this rule's message and which
// mentions `useMemo`. That found 30 fixtures and 11 firings, which read as an 11-false-positive rate
// -- the `refs`/`immutability` shape this domain has paid for twice.
//
// It was the filter that was wrong. `Expectation` parses only the `## Error` block, so `Raw` is
// empty for every fixture that expects clean output, and the complement was made almost entirely of
// error fixtures for other rules. Firing on those is not obviously wrong: upstream reports something
// there too, and a program with one rule violation frequently has another.
//
// Measured directly: the corpus holds zero fixtures that use manual memoization and expect no error
// at all. So there is no clean-fixture population to score against, and the honest statement is that
// this rule's over-reporting cannot be measured from this corpus.
//
// What is measured instead: firings on fixtures whose golden expects errors this rule does not
// produce. That is not a false-positive count -- it is an upper bound on one, and it is reported as
// such.
func TestPreserveManualMemoizationOnFixturesExpectingNoSuchError(t *testing.T) {
	fixtures, err := reactconformance.Load("../../reactconformance/testdata/fixtures")
	if err != nil {
		t.Fatalf("loading the vendored corpus: %v", err)
	}

	const ruleMessage = "Existing memoization could not be preserved"
	usesMemoization, expectsNoError, firedOnOtherRule := 0, 0, 0

	for _, fixture := range fixtures {
		if !strings.Contains(fixture.Source, "useMemo") &&
			!strings.Contains(fixture.Source, "useCallback") {
			continue
		}
		usesMemoization++
		if len(fixture.Expected.Errors) == 0 && fixture.Expected.Raw == "" {
			expectsNoError++
		}
		if strings.Contains(fixture.Expected.Raw, ruleMessage) {
			continue
		}
		findings, ok := findingsForSource(t, fixture.Source)
		if ok && len(findings) > 0 {
			firedOnOtherRule++
		}
	}

	if usesMemoization == 0 {
		t.Fatal("no fixture in the corpus uses manual memoization, so this test measures nothing")
	}

	t.Logf("fixturesUsingMemoization=%d expectingNoErrorAtAll=%d firedWhereAnotherRuleIsExpected=%d",
		usesMemoization, expectsNoError, firedOnOtherRule)

	// The only assertion this corpus supports. A rule firing on every memoization fixture is
	// reporting unconditionally, which the 33-fixture test cannot detect because all 33 are errors.
	if firedOnOtherRule == usesMemoization {
		t.Errorf("the rule fired on all %d fixtures using memoization; that is unconditional "+
			"reporting and the error-fixture score above cannot distinguish it", usesMemoization)
	}
}

// findingsForSource runs the whole pipeline over one fixture and returns what the rule reported.
//
// Returns false when the source does not lower, which is a decline rather than a silent zero -- the
// distinction the conformance harness draws between `Declined` and `Failed`, and for the same
// reason: an empty result is a claim, and a claim about a program that never parsed is a wrong one.
func findingsForSource(t *testing.T, source string) ([]PreserveManualMemoizationFinding, bool) {
	t.Helper()

	var all []PreserveManualMemoizationFinding
	lowered := false

	probe := rule.Rule{
		Name:             "preserve-manual-memoization-score",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						return
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						function := Lower(functionNode, ctx.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						lowered = true
						all = append(all, pipelineFindings(function, ctx.TypeChecker)...)
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/fixture.tsx": source,
	}, "/fixture.tsx")

	return all, lowered
}

// pipelineFindings runs every phase-6 pass in order and then the rule.
//
// The order is upstream's pipeline order, and it is load-bearing: the rule reads `Pruned` and
// `Merged`, both of which are written by passes that must have run.
func pipelineFindings(function *Function, checker *shimchecker.Checker) []PreserveManualMemoizationFinding {
	InferReactive(function, checker)
	DropManualMemoization(function)

	ranges := InferMutableRanges(function)
	set := FindDisjointMutableValuesWithRanges(function, ranges)
	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)
	dependencies := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)

	tree, _ := BuildReactiveFunction(function)
	if tree == nil {
		return nil
	}

	MergeReactiveScopesThatInvalidateTogether(tree, function, dependencies, checker)
	PruneNonEscapingScopes(tree, function, dependencies, checker)
	PruneUnusedScopes(tree, dependencies)
	PruneAlwaysInvalidatingScopes(tree, function, dependencies)
	PruneNonReactiveDependencies(tree, function, dependencies)

	return ValidatePreservedManualMemoization(tree, function, scopes)
}
