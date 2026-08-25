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

	scored, matched, missed := 0, 0, 0
	for _, fixture := range fixtures {
		if !strings.Contains(fixture.Source, "validatePreserveExistingMemoizationGuarantees") {
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
		have := map[string]bool{}
		for _, one := range ours {
			have[one] = true
		}
		for _, want := range expected {
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

	// Measured floor. Raise it when dependency collection improves; a drop is a regression and the
	// message says which.
	//
	// 72 of 123 today. Two things this number has already settled, neither of which the corpus
	// distribution test could answer:
	//
	// Truncating every path to its root scores 61, so the 11-point gap is what path depth is
	// currently worth and the oracle is shown able to fail rather than assumed to work.
	//
	// Upstream's `isDeferredDependency` guard, ported and measured, scores 72 -- IDENTICAL. It moves
	// the corpus distribution from 171 deep to 322, and matches no additional upstream answer. That
	// is a movement detector reporting a large win on a change worth nothing, which is why this test
	// exists and why that guard stayed reverted.
	const knownMatched = 72
	if matched < knownMatched {
		t.Errorf("matched %d of %d golden dependencies, down from %d; dependency collection got "+
			"shallower or lost a path", matched, scored, knownMatched)
	}
	t.Logf("golden cache slots: %d scored, %d matched, %d missed", scored, matched, missed)
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

func identifierName(function *Function, id IdentifierId) string {
	if function == nil || int(id) >= len(function.Identifiers) {
		return ""
	}
	if identifier := function.Identifiers[id]; identifier != nil {
		return identifier.Name
	}
	return ""
}
