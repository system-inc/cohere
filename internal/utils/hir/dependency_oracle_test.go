package hir

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/reactconformance"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// The compiled output in a golden names upstream's inferred dependencies, verbatim.
//
// React's codegen emits one cache slot per dependency and compares them in a single condition:
//
//	if ($[0] !== propA.x || $[1] !== x) {
//
// Each `$[n] !== <expression>` is one dependency as upstream inferred it, at the depth upstream
// inferred it to. That is the answer key this package has been missing: every measurement of
// dependency collection so far could say a count moved and could not say which direction was right.
var goldenDependencyPattern = regexp.MustCompile(`\$\[[0-9]+\] !== ([A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*)`)

// goldenDependencies returns the dependency expressions upstream's compiled output compares.
func goldenDependencies(t *testing.T, expectPath string) []string {
	t.Helper()
	body, err := os.ReadFile(expectPath)
	if err != nil {
		return nil
	}
	// Only the `## Code` section. The `## Input` section holds the source, which contains the
	// developer's own dependency array and would be scored as though it were upstream's answer.
	text := string(body)
	start := strings.Index(text, "## Code")
	if start < 0 {
		return nil
	}
	var found []string
	seen := map[string]bool{}
	for _, match := range goldenDependencyPattern.FindAllStringSubmatch(text[start:], -1) {
		if seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		found = append(found, match[1])
	}
	sort.Strings(found)
	return found
}

// TestInferredDependenciesAgainstGoldenCacheSlots scores our paths against upstream's own answers.
//
// # Why this exists, and what it replaces
//
// `TestHoistableCorpusDistribution` pins 171 deep against 1,435 flat dependencies and its own
// comment says a gain in `deep` is the over-approximating direction. That is a movement detector,
// not an oracle: it can say a number changed and cannot say whether the new number is closer to
// right. A change that nearly doubled path depth was measured against it and could not be judged.
//
// This scores the actual paths against the actual answers, so a depth change is readable as an
// improvement or a regression rather than only as a delta.
//
// # The score is a floor rather than an equality, deliberately
//
// Two reasons, both measured rather than assumed. Codegen names a temporary `t1` where the dependency
// is an expression upstream chose not to spell out, so some slots are unmatchable by construction.
// And this package declares `DependencyGapOptionalChains`, so an optional chain is expected to score
// short. Asserting equality would fail on upstream's own choices rather than on our defects.
func TestInferredDependenciesAgainstGoldenCacheSlots(t *testing.T) {
	fixtures, err := reactconformance.Load("../../reactconformance/testdata/fixtures")
	if err != nil {
		t.Fatalf("loading the vendored corpus: %v", err)
	}

	scored, matched, missed, unmatchable, flowExcluded := 0, 0, 0, 0, 0
	upstreamTotal, oursTotal := 0, 0
	overProducing, underProducing, exactCount := 0, 0, 0
	for _, fixture := range fixtures {
		if !strings.Contains(fixture.Source, "validatePreserveExistingMemoizationGuarantees") {
			continue
		}
		if fixture.RequiresFlow() {
			// The corpus already classifies these as `VerdictFlowSyntax`, and this test was not
			// asking. A Flow fixture lowers to nothing here, so every dependency upstream inferred
			// in it scores as a miss and reads as a collector defect. Measured: one fixture, three
			// slots, all bare roots, which was a seventh of the entire remaining shortfall.
			flowExcluded++
			continue
		}
		expected := goldenDependencies(t, fixture.ExpectPath)
		if len(expected) == 0 {
			continue
		}
		ours, ok := inferredDependencyStrings(t, fixture.Source)
		if !ok {
			continue
		}
		upstreamTotal += len(expected)
		oursTotal += len(ours)
		switch {
		case len(ours) > len(expected):
			overProducing++
		case len(ours) < len(expected):
			underProducing++
		default:
			exactCount++
		}

		have := map[string]bool{}
		for _, one := range ours {
			have[one] = true
		}
		for _, want := range expected {
			if isCodegenTemporary(want) {
				// Codegen named a temporary where upstream chose not to spell the expression out.
				// Nothing this package produces could match it, so counting it as a miss would put
				// a floor under the score that no correct implementation could ever clear.
				unmatchable++
				continue
			}
			scored++
			if have[want] {
				matched++
			} else {
				missed++
			}
		}
	}

	if scored == 0 {
		t.Fatal("no golden cache slot was scored, so this test asserts nothing; the `## Code` " +
			"section or the cache-slot spelling changed rather than the collector being correct")
	}

	// Measured floor: 70 of 87 matchable slots, 80 percent. Raise it when dependency collection
	// improves; a drop is a regression and the message says which.
	//
	// # The denominator was wrong twice, and neither correction touched the collector
	//
	// The first score was 72 of 123. Then 29 codegen temporaries came out, and then 9 Flow fixtures.
	// Both populations were scoring as collector defects while being facts about what this harness
	// can read at all, and both were found by reading the misses rather than by suspecting them.
	//
	// That is the failure this test was built to prevent, arriving in the test itself: a number that
	// moves for a reason unrelated to the thing it claims to measure.
	//
	// # What this number has already settled
	//
	// Truncating every path to its root scores 61 against 70, so the 9-point gap is what path depth
	// is currently worth and the test is shown able to fail rather than assumed to work.
	//
	// Upstream's `isDeferredDependency` guard, ported and measured, scores IDENTICALLY. It moves the
	// corpus distribution from 171 deep to 322 and matches no additional upstream answer. A movement
	// detector reported a large win on a change worth nothing, which is why this exists and why that
	// guard stayed reverted.
	//
	// The 17 remaining misses are real, and they split roughly evenly between a dependency this
	// collector never produces and one it produces too shallow. The shortfall is not a single defect
	// and is not mostly about depth.
	const knownMatched = 70
	if matched < knownMatched {
		t.Errorf("matched %d of %d golden dependencies, down from %d; dependency collection got "+
			"shallower or lost a path", matched, scored, knownMatched)
	}
	// # The second half of the score, and the oracle was blind to it until now
	//
	// Everything above measures whether an upstream dependency has a match on our side. Nothing
	// measured the reverse: a dependency we produce that upstream does not. Those cost nothing in
	// the matched count and are the dominant error.
	//
	// Measured: we produce 151 dependencies where upstream produces 115, on the same fixtures. 25
	// fixtures over-produce, 6 under-produce, 17 agree exactly. Every session before this one
	// treated the shortfall as missing depth and built toward producing MORE, which is the wrong
	// direction for two thirds of the disagreements.
	//
	// This matters for the rule rather than only for tidiness. A dependency we infer and upstream
	// does not widens the scope that holds it, and a wider scope is one still open when
	// `preserve-manual-memoization` checks it. Ten fixtures both over-produce here and false-positive
	// in the rule's own score, which is what makes this the same defect seen from two ends rather
	// than a second lane.
	//
	// # The two sides count different scope populations, and some of the 36 are that
	//
	// This scores `DependenciesOf` over every ASSIGNED scope. Upstream's cache slots are emitted
	// after the whole prune chain, so they name only surviving scopes. Traced on
	// `useCallback-nonescaping.js`: we produce three dependencies across three scopes, our pipeline
	// prunes two of them exactly as upstream does, and the rule therefore sees the one dependency
	// upstream emits. The extra two are real in this count and invisible to the rule.
	//
	// Restricting to survivors was measured rather than argued, and it is not simply better:
	//
	//	all assigned scopes                70 matched, 151 produced
	//	minus scopes non-escaping dissolves 67 matched, 138 produced
	//	minus every pruned scope            60 matched,  98 produced
	//
	// Ten matches lost to remove the over-production, and 22 fixtures flip to under-producing. So
	// the pruned scopes carry real dependencies too and the population is not simply wrong.
	//
	// Read the 151 as "what collection produces", not as "what the rule sees". Which population
	// upstream's slots correspond to is unsettled and is upstream of every count comparison here.
	const knownUpstreamTotal = 115
	const knownOursTotal = 151
	if upstreamTotal != knownUpstreamTotal {
		t.Errorf("upstream total is %d, want %d; the corpus or the cache-slot spelling changed and "+
			"every count below is against a different population", upstreamTotal, knownUpstreamTotal)
	}
	if oursTotal != knownOursTotal {
		t.Errorf("we produce %d dependencies against upstream's %d, want %d; DOWN toward %d is the "+
			"improvement here and up is a regression, which is the opposite of the matched count "+
			"above", oursTotal, upstreamTotal, knownOursTotal, knownUpstreamTotal)
	}

	t.Logf("golden cache slots: %d scored, %d matched, %d missed, %d unmatchable; "+
		"%d flow fixtures excluded", scored, matched, missed, unmatchable, flowExcluded)
	t.Logf("dependency counts: upstream %d, ours %d (%d over-producing, %d under, %d exact)",
		upstreamTotal, oursTotal, overProducing, underProducing, exactCount)
}

// inferredDependencyStrings renders every scope dependency the way source would spell it.
func inferredDependencyStrings(t *testing.T, source string) ([]string, bool) {
	t.Helper()
	var rendered []string
	ran := false
	probe := rule.Rule{
		Name:             "inferred-dependency-strings",
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
						InferReactive(function, ctx.TypeChecker)
						DropManualMemoization(function)
						ranges := InferMutableRanges(function)
						disjoint := FindDisjointMutableValuesWithRanges(function, ranges)
						scopes := AssignReactiveScopesWithSets(function, ranges, disjoint)
						aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
						identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
						BuildReactiveScopeTerminals(function, scopes, identity)
						dependencies := CollectScopeDependenciesWithHoistable(
							function, scopes, identity, ranges)
						ran = true
						for _, scope := range scopes.Ids() {
							for _, dependency := range dependencies.DependenciesOf(scope) {
								name := identifierName(function, dependency.Identifier)
								if name == "" {
									continue
								}
								for _, entry := range dependency.Path {
									name += "." + entry.Property
								}
								rendered = append(rendered, name)
							}
						}
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/fixture.tsx": source,
	}, "/fixture.tsx")
	return rendered, ran
}

// codegenTemporaryPattern matches a name React's codegen invented rather than took from source.
//
// `t0`, `t1` and `rest_0` are printer artifacts: upstream inferred a dependency it chose not to
// spell out as an expression, so the golden names the slot after the temporary holding it. Measured
// on this corpus, 29 of 123 cache slots are one of these.
var codegenTemporaryPattern = regexp.MustCompile(`^(t[0-9]+|rest_[0-9]+)(\.|$)`)

func isCodegenTemporary(dependency string) bool {
	return codegenTemporaryPattern.MatchString(dependency)
}

func identifierName(function *Function, id IdentifierId) string {
	if function == nil || int(id) >= len(function.Identifiers) {
		return ""
	}
	if identifier := function.Identifiers[id]; identifier != nil {
		return identifier.Name
	}
	return ""
}
