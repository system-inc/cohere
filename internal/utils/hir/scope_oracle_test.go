// Scoring our reactive scope structure against upstream's own compiled output.
//
// The second oracle in this tree, after `dependency_oracle_test.go`. That one asks whether a scope
// depends on the right values; this one asks whether the scopes exist at all, and how many.
//
// # Why the answer key is trustworthy
//
// Upstream's `## Code` block is what React's compiler actually emitted for the fixture, and a
// surviving reactive scope appears in it as exactly one cache guard:
//
//	const $ = _c(7);
//	if ($[0] !== propA || $[1] !== t1) {   <- one surviving scope
//
// So counting `if ($[n] !== ` over that block counts upstream's scopes directly. No inference, no
// reconstruction: it is their compiler's own answer, checked into their repository.
//
// # The stage question, which had to be settled before any number here meant anything
//
// Our pipeline holds two different scope populations, and upstream's guards correspond to only one
// of them. Scopes are ASSIGNED by `AssignReactiveScopes`, then a chain of prune passes dissolves
// the ones that turn out not to be worth memoizing. Upstream's output is emitted after their
// equivalent chain, so their guards are the SURVIVORS.
//
// Measured both ways over the same fixtures, one pipeline run counted twice:
//
//	upstream guards                          97
//	assigned   total 160   33 over,  4 under, 11 exact
//	survived   total 114   18 over,  5 under, 25 exact
//
// The totals alone do not settle it -- 114 is closer to 97 than 160 is, but a stage could get the
// aggregate right while being wrong on every individual fixture. The per-fixture agreement is what
// settles it: EXACT more than doubles, 11 of 48 to 25 of 48. A stage that merely netted out would
// leave that flat. So this oracle scores the survived population, and the assigned count is
// reported alongside it as context rather than pinned.
//
// This mattered more than a detail. Read at the assigned stage the gap is 1.65x and looks like a
// systematic fusion defect; read at the correct stage it is 1.18x. Three sessions of theory were
// built on the wrong stage of our own pipeline before anyone counted both.
//
// # Three numbers, because one would have lied
//
// At the survived stage the total improved AND per-fixture agreement doubled AND `under` got worse
// (4 fixtures to 5). A single-number oracle reports that as an unambiguous win. It is not: `under`
// is the correctness direction. An extra scope memoizes something upstream did not, which costs
// performance; a MISSING scope drops a memoization the developer wrote, which changes behaviour.
// Two of the five are our prune chain dissolving scopes upstream keeps.
//
// So `under` is pinned as a ceiling that fails loudly, and it is the number that outranks the other
// two whenever they disagree.
package hir

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/verify/internal/reactconformance"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// upstreamScopeGuard matches one surviving reactive scope in upstream's compiled output.
//
// Anchored on `!==` rather than on `$[` alone, because a cache WRITE (`$[0] = propA`) and a cache
// READ (`t2 = $[2]`) both name a slot and neither opens a scope. Only the guard does.
var upstreamScopeGuard = regexp.MustCompile(`if \(\$\[\d+\] !==`)

// scopeStageCounts is one fixture's scope count at each of the two pipeline stages.
type scopeStageCounts struct {
	assigned int
	survived int
}

// countScopeStages runs the pipeline once and reports the scope population at two points in it.
//
// One run, counted twice, so the only difference between the two numbers is the stage. Running the
// pipeline twice would introduce every difference between two runs as noise in a comparison whose
// entire subject is a difference of 46.
func countScopeStages(function *Function, checker *shimchecker.Checker) scopeStageCounts {
	InferReactive(function, checker)
	DropManualMemoization(function)
	EliminateDeadCode(function)

	ranges := InferMutableRanges(function)
	set := FindDisjointMutableValuesWithRanges(function, ranges)
	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)
	dependencies := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)

	counts := scopeStageCounts{assigned: scopes.Len()}

	tree, _ := BuildReactiveFunction(function)
	if tree == nil {
		return counts
	}
	MergeReactiveScopesThatInvalidateTogether(tree, function, dependencies, checker)
	PruneNonEscapingScopesWithScopes(tree, function, dependencies, scopes, checker)
	PruneUnusedScopes(tree, dependencies)
	PruneAlwaysInvalidatingScopes(tree, function, dependencies)
	PruneNonReactiveDependencies(tree, function, dependencies)

	// Counted by walking the tree rather than by subtracting a pruned count, because a scope block
	// can appear in the tree more than once and the question is how many DISTINCT scopes survive --
	// which is what one of upstream's guards represents.
	surviving := map[ScopeId]bool{}
	VisitReactiveFunction(tree, ReactiveVisitor{
		Scope: func(scope *ReactiveScopeBlock, traverse func()) {
			if !scope.Pruned {
				surviving[scope.Scope] = true
			}
			traverse()
		},
	})
	counts.survived = len(surviving)
	return counts
}

// TestScopeStructureAgainstUpstreamGuards scores our surviving scopes against upstream's.
//
// # The population, and why each exclusion is stated rather than filtered silently
//
// Of 395 fixtures on disk, this scores 48. Every step of that narrowing removes a population where
// a comparison would be meaningless rather than merely inconvenient, and each is counted so the
// cost stays visible:
//
//   - Not gated on `validatePreserveExistingMemoizationGuarantees`: upstream did not run this
//     analysis, so there is nothing to compare against.
//   - Flow component syntax: our parser cannot model it, so the fixture lowers into unrelated
//     arrows. See `knownFlowDeclarationFixtures` in the score test.
//   - No `## Code` block: upstream bailed or errored, so there is no compiled output at all.
//   - A `## Code` block emitting zero guards: upstream compiled the fixture and memoized nothing.
//     Scoring these inflates the gap from +17 to +44 while saying nothing about correspondence,
//     since any scope we produce is over-production by construction.
func TestScopeStructureAgainstUpstreamGuards(t *testing.T) {
	fixtures, err := reactconformance.Load("../../reactconformance/testdata/fixtures")
	if err != nil {
		t.Fatalf("loading the vendored corpus: %v", err)
	}

	var lines []string
	upstreamTotal, assignedTotal, survivedTotal := 0, 0, 0
	assignedOver, assignedUnder, assignedExact := 0, 0, 0
	survivedOver, survivedUnder, survivedExact := 0, 0, 0
	survivedUnderScopes := 0
	var underFixtures []string
	flowExcluded, noCodeSection, zeroGuard := 0, 0, 0

	for _, fixture := range fixtures {
		if !strings.Contains(fixture.Source, "validatePreserveExistingMemoizationGuarantees") {
			continue
		}
		if fixture.RequiresFlow() {
			flowExcluded++
			continue
		}
		body, readErr := os.ReadFile(fixture.ExpectPath)
		if readErr != nil {
			continue
		}
		text := string(body)
		index := strings.Index(text, "## Code")
		if index < 0 {
			noCodeSection++
			continue
		}
		upstream := len(upstreamScopeGuard.FindAllString(text[index:], -1))
		if upstream == 0 {
			zeroGuard++
			continue
		}

		total := scopeStageCounts{}
		ran := false
		probe := rule.Rule{
			Name:             "scope-oracle",
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
							ran = true
							one := countScopeStages(function, ctx.TypeChecker)
							total.assigned += one.assigned
							total.survived += one.survived
						})
					},
				}
			},
		}
		ruletest.RunTypedFiles(t, probe, map[string]string{
			"/react.d.ts":  reactiveDeclarations,
			"/fixture.tsx": fixture.Source,
		}, "/fixture.tsx")
		if !ran {
			continue
		}

		upstreamTotal += upstream
		assignedTotal += total.assigned
		survivedTotal += total.survived

		switch {
		case total.assigned > upstream:
			assignedOver++
		case total.assigned < upstream:
			assignedUnder++
		default:
			assignedExact++
		}

		name := fixture.ExpectPath[strings.LastIndex(fixture.ExpectPath, "/")+1:]
		switch {
		case total.survived > upstream:
			survivedOver++
		case total.survived < upstream:
			survivedUnder++
			survivedUnderScopes += upstream - total.survived
			underFixtures = append(underFixtures, fmt.Sprintf("%s (upstream %d, ours %d)",
				name, upstream, total.survived))
		default:
			survivedExact++
		}

		lines = append(lines, fmt.Sprintf("up=%-3d assigned=%-3d survived=%-3d %s",
			upstream, total.assigned, total.survived, name))
	}

	sort.Strings(lines)
	scored := survivedOver + survivedUnder + survivedExact
	t.Logf("scored=%d upstream=%d assigned=%d survived=%d", scored,
		upstreamTotal, assignedTotal, survivedTotal)
	t.Logf("assigned: over=%d under=%d exact=%d", assignedOver, assignedUnder, assignedExact)
	t.Logf("survived: over=%d under=%d exact=%d underScopes=%d",
		survivedOver, survivedUnder, survivedExact, survivedUnderScopes)
	t.Logf("excluded: flow=%d noCodeSection=%d zeroGuard=%d", flowExcluded, noCodeSection, zeroGuard)

	if scored == 0 {
		t.Fatal("no fixture was scored; the corpus, the gate string or the guard pattern is wrong, " +
			"and every count below would be a meaningless zero")
	}

	// The population itself is pinned. A count below built over a different set of fixtures is not
	// comparable to the numbers this file was calibrated against, and the first sign of that is the
	// denominator moving.
	const knownScored = 48
	if scored != knownScored {
		t.Errorf("scored %d fixtures, want %d; the population changed, so every count below is "+
			"against a different set and none of them can be compared to its pin", scored,
			knownScored)
	}
	const knownUpstreamTotal = 97
	if upstreamTotal != knownUpstreamTotal {
		t.Errorf("upstream guards = %d, want %d; the answer key itself moved, which means the "+
			"corpus was re-vendored or the guard pattern stopped matching", upstreamTotal,
			knownUpstreamTotal)
	}

	// # The three numbers, in the order they should be read
	//
	// `under` first. It is the correctness direction and it outranks the other two whenever they
	// disagree -- which they already have: at the survived stage the total improved and per-fixture
	// agreement doubled while `under` got WORSE than at the assigned stage.
	const knownSurvivedUnder = 5
	const knownSurvivedUnderScopes = 7
	if survivedUnder > knownSurvivedUnder || survivedUnderScopes > knownSurvivedUnderScopes {
		t.Errorf("under-production = %d fixtures / %d scopes, want at most %d / %d; we now produce "+
			"FEWER scopes than upstream somewhere new, which drops a memoization the developer "+
			"wrote rather than adding one they did not. This is a behaviour change, not a "+
			"performance one, and it outranks any improvement in the two counts below.\n  %s",
			survivedUnder, survivedUnderScopes, knownSurvivedUnder, knownSurvivedUnderScopes,
			strings.Join(underFixtures, "\n  "))
	}

	// `exact` second. Per-fixture agreement is the finer signal, because two totals can net out: a
	// change making twenty fixtures worse and twenty better leaves `survivedTotal` untouched.
	const knownSurvivedExact = 25
	if survivedExact < knownSurvivedExact {
		t.Errorf("exact per-fixture agreement = %d of %d, want at least %d; fewer fixtures now "+
			"match upstream's scope count exactly, which a stable total would hide", survivedExact,
			scored, knownSurvivedExact)
	}

	// `ours` last, as a ceiling. Down toward 97 is the improvement.
	const knownSurvivedTotal = 114
	if survivedTotal > knownSurvivedTotal {
		t.Errorf("surviving scopes = %d against upstream's %d, want at most %d; we produce more "+
			"scopes than before, so something split a scope upstream keeps whole or stopped a "+
			"prune pass that used to fire", survivedTotal, upstreamTotal, knownSurvivedTotal)
	}

	// The assigned stage is reported, deliberately not pinned. It is context for anyone reading a
	// survived number that moved: a change can move both stages or only the prune chain, and the
	// two answers point at different passes. Pinning it would also fail this test for changes that
	// improve the population upstream's output does not describe.
	if assignedTotal < survivedTotal {
		t.Errorf("assigned %d < survived %d, which is impossible: pruning cannot create a scope, "+
			"so the two stages are being counted over different populations", assignedTotal,
			survivedTotal)
	}

	if dump := os.Getenv("SCOPE_ORACLE_DUMP"); dump != "" {
		if writeErr := os.WriteFile(dump, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeErr != nil {
			t.Errorf("writing the per-fixture dump to %s: %v", dump, writeErr)
		}
	}
}
