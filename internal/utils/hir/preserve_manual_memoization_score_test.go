package hir

import (
	"path/filepath"
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

	// The count is pinned, and `fired > 0` above is not enough on its own.
	//
	// # This assertion exists because its absence nearly shipped a regression as an improvement
	//
	// The clean-fixture rate below is pinned exactly and this side was not, so the two instruments
	// were asymmetric in the one direction that matters: a change trading true positives away for
	// false-positive removals moved the pinned number DOWN, which reads as the rule improving, while
	// this side stayed green because it only asked for a non-zero.
	//
	// That is not hypothetical. Freezing component parameters in the aliasing graph took the clean
	// rate from 31 to 19 and this number from 15 to 9 in the same run. Reported through the
	// instruments as they stood, that is "false positives down 39%" with no signal at all that six
	// programs upstream reports on had gone silent.
	//
	// The six were `error.useMemo-aliased-var` plus five optional-member-expression fixtures, and
	// what they show is that the repair was in the wrong pass. Freezing a parameter suppressed the
	// walk that carries a mutation ONWARD, so `x.push(props?.items)` stopped widening `x`, which is
	// mutated, rather than `props`, which is not. Upstream does not do this: oxc's `NodeValue` in
	// `infer_mutation_aliasing_ranges.rs:72` is `Object | Phi` with no third element, and the
	// parameter freeze lives in `InferMutationAliasingEffects` as a `MutateFrozen` EFFECT rather
	// than as a node value this pass reads.
	//
	// So both directions are pinned, and a change moving either one has to say which it moved and
	// why. Silence is not the right answer on ANY of these 33: each is a program where upstream
	// reports a lost memoization.
	const knownTruePositives = 15
	if fired != knownTruePositives {
		t.Errorf("true positives = %d, want %d; if this went UP the rule improved and this number "+
			"should be raised deliberately, and if it went DOWN the rule stopped reporting programs "+
			"upstream reports on, which the clean-fixture rate cannot see", fired, knownTruePositives)
	}
	// Non-degeneracy in the other direction is not asserted: a rule that fires on all 33 could be
	// correct, since all 33 are error fixtures. Over-firing is caught by the clean-fixture check
	// below instead, where silence is the right answer.
}

// TestPreserveManualMemoizationFalsePositiveRate is the over-reporting half, measured directly.
//
// The 33 error fixtures can only show under-reporting: a rule that fires on everything passes all of
// them. This is the population where silence is the right answer, so a finding here is a defect and
// nothing else in the phase can see one.
//
// # It took three attempts to define this population, and the first two are why the number is pinned
//
// The first spelling took every fixture whose `Expected.Raw` lacks this rule's message and which
// mentions `useMemo`. That found 30 fixtures and 11 firings, which read as an 11-false-positive rate.
// The filter was wrong: `Expectation` parses only the `## Error` block, so `Raw` is empty for every
// fixture expecting clean output, and the complement was made almost entirely of error fixtures for
// other rules. Firing on those is not obviously wrong.
//
// The second concluded the corpus held zero fixtures that use manual memoization and expect no error
// at all, and therefore that this rule's over-reporting could not be measured here. That was true of
// the corpus and false about the cause: `Load` rejected every fixture not named `error.*`, so the
// tree was error-only by construction. Upstream ships 70 of these. The loader excluded them, and the
// exclusion was reasoned about for hours as a property of upstream.
//
// So the rate is now a real measurement rather than an upper bound, and it is high: 31 of 70. The
// count is asserted exactly rather than as a ceiling, because a ceiling cannot distinguish a fix
// from a rule that went quiet, and this rule's registration is being held precisely on the strength
// of this number.
func TestPreserveManualMemoizationFalsePositiveRate(t *testing.T) {
	fixtures, err := reactconformance.Load("../../reactconformance/testdata/fixtures")
	if err != nil {
		t.Fatalf("loading the vendored corpus: %v", err)
	}

	const ruleMessage = "Existing memoization could not be preserved"
	clean, fired, silent, unsupported := 0, 0, 0, 0
	for _, fixture := range fixtures {
		base := filepath.Base(fixture.Name)
		if strings.HasPrefix(base, "error.") || strings.HasPrefix(base, "todo.error.") {
			continue
		}
		clean++
		findings, ok := findingsForSource(t, fixture.Source)
		if !ok {
			unsupported++
			continue
		}
		if len(findings) > 0 {
			fired++
			continue
		}
		silent++
	}

	if clean != reactconformance.ExpectedCleanFixtureCount {
		t.Fatalf("scored %d clean fixtures, want %d; the population this test measures is not the "+
			"one it believes it is", clean, reactconformance.ExpectedCleanFixtureCount)
	}
	t.Logf("cleanFixtures=%d silentCorrect=%d falsePositives=%d unsupported=%d (%.0f%% false-positive rate)",
		clean, silent, fired, unsupported, 100*float64(fired)/float64(clean))

	// Every one of these is the rule reporting a lost memoization on a program upstream compiles
	// clean. A gate that does this gets switched off, which is why the registration is held.
	const knownFalsePositives = 31
	if fired != knownFalsePositives {
		t.Errorf("false positives = %d, want %d; if this went DOWN the rule improved and this "+
			"number should be lowered deliberately, and if it went UP something regressed",
			fired, knownFalsePositives)
	}

	// Non-degeneracy. A rule silent on everything scores zero false positives and is worthless, and
	// the error-fixture test above is what would catch that -- restated here so this test cannot
	// pass alone by the rule ceasing to report.
	if silent == 0 {
		t.Error("the rule fired on every clean fixture, which is unconditional reporting")
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
