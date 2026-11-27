package high_level_intermediate_representation

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/modules/mutation_aliasing"
)

// hoistableFor runs the whole pipeline and returns the analysis.
//
// Typed for the reason `scopesFor` is: a checker-less lowering never emits `StoreContext`, so
// several assertions below would pass vacuously against a plain harness.
func hoistableFor(test *testing.T, source string) (*Function, *ReactiveScopes, ScopeIdentity,
	*mutation_aliasing.MutableRanges, *HoistableAnalysis) {
	test.Helper()
	function, ranges := rangesFor(test, source)
	set := FindDisjointMutableValuesWithRanges(function, ranges)
	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)
	return function, scopes, identity, ranges, analyseHoistableLoads(function, scopes, identity, ranges, nil)
}

// ---------------------------------------------------------------------------
// What the analysis buys, which is path depth
// ---------------------------------------------------------------------------

// TestHoistableAnalysisDeepensDependencyPaths is this stage's headline, pinned as a test.
//
// `dependencies.go` shipped with every dependency truncated to its bare root -- 4,426 of 4,426 with
// an empty path -- because `addDependency` stops at the first entry it cannot prove hoistable. The
// measurement that shows this analysis works is therefore the PATH-length distribution, before and
// after, over 400 files of real code and 677 functions:
//
//	path length      0      1     2     3    4+
//	before        4,426      0     0     0     0
//	after         4,068    421    68     1     1
//
// A length-zero cell that fails to shrink is the tell that the pass is inert, which is why the two
// spellings are both kept and compared rather than the old one being deleted. 491 dependencies gain
// a real path and the total moves from 4,426 to 4,559, because a scope that reported one truncated
// root can report two distinct deeper paths once they stop collapsing together.
//
// The thresholds below are far looser than those numbers. This guards the shape, not the measurement.
func TestHoistableAnalysisDeepensDependencyPaths(test *testing.T) {
	test.Parallel()

	// # Why this fixture and not a simpler one
	//
	// The first two attempts here used a single object literal reading `props.configuration.alpha`, and
	// both produced zero deep paths while the pass found 171 over real code. A fixture failing against
	// working code means the fixture is wrong, so the shape was taken from a real function
	// (`parseListType` in `GraphQlOperationsMetadataPlugin.ts`) rather than invented again.
	//
	// What the invented fixtures missed: a scope that reads its root bare anywhere collapses every
	// deeper path into that root, because `deriveMinimalDependencies` correctly prunes a subtree
	// under a node already marked a dependency. Depending on all of `props` subsumes
	// `props.configuration.alpha`. The shape that actually exercises path depth is repeated deep reads
	// across branches with no bare read of the root -- which is also precisely the shape the
	// backward pass exists to serve, since it proves the deref safe above the branch.
	function, scopes, identity, ranges, _ := hoistableFor(test, `
		function parseListType(listType) {
			if (listType.type.kind === 1) {
				return {requiresItems: false, itemType: listType.type.name.value};
			}
			if (listType.type.kind === 2) {
				return {requiresItems: true, itemType: listType.type.type.name.value};
			}
			return null;
		}
	`)

	before := CollectScopeDependencies(function, identity)
	after := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)

	deepBefore, deepAfter := 0, 0
	for _, scope := range before.Ids() {
		for _, dep := range before.DependenciesOf(scope) {
			if len(dep.Path) > 0 {
				deepBefore++
			}
		}
	}
	for _, scope := range after.Ids() {
		for _, dep := range after.DependenciesOf(scope) {
			if len(dep.Path) > 0 {
				deepAfter++
			}
		}
	}
	if deepBefore != 0 {
		test.Errorf("without the hoistable analysis %d dependencies carried a path; every one should "+
			"truncate to its root", deepBefore)
	}
	if deepAfter == 0 {
		test.Error("with the hoistable analysis no dependency carried a path, so the pass is inert; " +
			"this is the cell whose failure to shrink is the tell")
	}
}

// TestHoistablePathsAreAlwaysPrefixesOfRealAccesses pins the SAFE error direction.
//
// Under-approximating costs precision: a path truncates early and the scope recomputes more often
// than needed. Over-approximating is the dangerous direction, because a dependency on `props.a.b`
// for a program where `props.a` can be nullish makes the memoized read throw where the original did
// not. Measured over 400 files of real code: 564 path entries, 0 naming a property never loaded.
func TestHoistablePathsAreAlwaysPrefixesOfRealAccesses(test *testing.T) {
	test.Parallel()

	// The same shape taken from real code as the headline test, for the reason given there: a fixture
	// whose root is read bare produces no path entries and would make this assertion vacuous.
	function, scopes, identity, ranges, _ := hoistableFor(test, `
		function parseListType(listType) {
			if (listType.type.kind === 1) {
				return {requiresItems: false, itemType: listType.type.name.value};
			}
			if (listType.type.kind === 2) {
				return {requiresItems: true, itemType: listType.type.type.name.value};
			}
			return null;
		}
	`)
	loaded := map[string]bool{}
	for _, instruction := range function.Instructions {
		if instruction == nil {
			continue
		}
		if load, ok := instruction.Value.(*PropertyLoad); ok {
			loaded[load.Property] = true
		}
	}
	if len(loaded) == 0 {
		test.Fatal("no property loads, so this test measured nothing")
	}
	after := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)
	entries := 0
	for _, scope := range after.Ids() {
		for _, dep := range after.DependenciesOf(scope) {
			for _, entry := range dep.Path {
				entries++
				if !loaded[entry.Property] {
					test.Errorf("derived a path through %q, which is never loaded; that is the "+
						"over-approximating direction and it invents a crash", entry.Property)
				}
			}
		}
	}
	if entries == 0 {
		test.Error("no path entries were checked, so this test proves nothing about safety")
	}
}

// ---------------------------------------------------------------------------
// Termination, which is a bound rather than a proof
// ---------------------------------------------------------------------------

// TestHoistableConvergenceIsFarInsideTheBound measures how close real code comes to the cap.
//
// The outer loop is capped at 100 alternating passes. That cap is not decoration: the monotonicity
// argument that would make this a true fixpoint does not close, because `reduceOptionalChains`
// replaces nodes rather than adding them. So the honest claim is a bound, and a bound that is
// routinely brushed is a materially different fact from one never approached.
//
// Measured over 400 files of real code and 677 functions:
//
//	iterations    1     2     3    4+   not converged
//	functions   376   299     2     0               0
//
// The maximum observed is 3 against a cap of 100. This test pins that real code stays far inside it,
// so a future change that starts approaching the cap is a visible event rather than a slow drift
// into silent truncation.
func TestHoistableConvergenceIsFarInsideTheBound(test *testing.T) {
	test.Parallel()

	_, _, _, _, analysis := hoistableFor(test, `
		function Component(props) {
			let total = props.seed.value;
			for (const item of props.items) {
				if (item.flag) { total = props.seed.other; } else { total = item.value; }
			}
			const object = {total: total};
			object.mutated = props.seed.third;
			return object;
		}
	`)
	if analysis == nil {
		test.Fatal("no analysis, so this test measured nothing")
	}
	if !analysis.Converged() {
		test.Errorf("a function with one loop failed to converge in %d iterations; the cap is %d",
			analysis.Iterations(), hoistableIterationCap)
	}
	if analysis.Iterations() > 10 {
		test.Errorf("converged only after %d iterations against a cap of %d; the measured maximum on "+
			"real code is 3, so this is a drift toward silent truncation",
			analysis.Iterations(), hoistableIterationCap)
	}
}

// TestHoistableReportsRatherThanHidingNonConvergence pins the divergence from oxc.
//
// React raises a hard invariant when the loop fails to converge. oxc writes `for _ in 0..100` and
// exits silently, so a truncated answer is indistinguishable from a converged one. A linter can do
// neither, so it reports -- and this pins that the reporting channel exists and answers true on
// ordinary input, because a `Converged` that returned false everywhere would be just as useless as
// one that always returned true.
func TestHoistableReportsRatherThanHidingNonConvergence(test *testing.T) {
	test.Parallel()

	_, _, _, _, analysis := hoistableFor(test, `
		function Component(props) {
			const object = {a: props.configuration.alpha};
			object.b = props.configuration.beta;
			return object;
		}
	`)
	if analysis == nil {
		test.Fatal("no analysis")
	}
	if !analysis.Converged() {
		test.Error("ordinary input reported as non-converged, so the channel carries no signal")
	}
	if analysis.Iterations() < 1 {
		test.Error("reported zero iterations, but the loop always runs at least one pass")
	}
}

// TestOptionalChainReductionTerminates pins the inner loop, which has no bound at all.
//
// `reduceOptionalChains` is an unbounded `do/while` upstream and here. It terminates because each
// replacement strictly decreases the number of optional entries in a path, and a path has finitely
// many entries -- so a node is replaced at most as many times as its path is long. That argument is
// implicit in upstream's construction rather than stated by it, so it is pinned on the shape that
// would spin forever if the optional-to-unconditional flip were ever reversible.
func TestOptionalChainReductionTerminates(test *testing.T) {
	test.Parallel()

	registry := newPathRegistry()
	root := ReactiveScopeDependency{Identifier: 7}
	rootIndex := registry.pathIndex(root)

	guarded := registry.pathIndex(ReactiveScopeDependency{
		Identifier: 7,
		Path:       []DependencyPathEntry{{Property: "a", Optional: true}},
	})
	nodes := map[int]bool{rootIndex: true, guarded: true}

	// The root being present is what makes `a?.b` reducible to `a.b`. If the flip were reversible
	// this would not return.
	reduceOptionalChains(nodes, registry)

	for index := range nodes {
		if registry.nodes[index].hasOptional && index == guarded {
			test.Error("the guarded node survived reduction even though its object is known non-null")
		}
	}
	if len(nodes) != 2 {
		test.Errorf("reduction changed the set size to %d, want 2; it replaces nodes rather than "+
			"adding or dropping them", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// The dataflow's own semantics
// ---------------------------------------------------------------------------

// TestForwardPropagationIntersectsRatherThanUnions pins soundness of the forward direction.
//
// A value is non-null at a join only if it was non-null on every path reaching it. Unioning instead
// would let one branch's dereference vouch for a path the other branch never took, which is the
// over-approximating direction that invents crashes.
func TestForwardPropagationIntersectsRatherThanUnions(test *testing.T) {
	test.Parallel()

	function, scopes, identity, ranges, _ := hoistableFor(test, `
		function Component(props) {
			let value;
			if (props.flag) { value = props.left.deep; } else { value = props.right; }
			const object = {value: value};
			object.mutated = props.other;
			return object;
		}
	`)
	after := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)
	for _, scope := range after.Ids() {
		for _, dep := range after.DependenciesOf(scope) {
			for _, entry := range dep.Path {
				// `left` is dereferenced on only one branch, so nothing after the join may claim it
				// is non-null. A union would let `left` through here.
				if entry.Property == "left" && len(dep.Path) > 1 {
					test.Error("a path descended through `left`, which is only dereferenced on one " +
						"branch; the join is unioning where it must intersect")
				}
			}
		}
	}
}

// A top-level scope beginning inside a branch is seeded with that branch's own facts.
//
// The intersection above covers the join: after an `if`, a fact true on one arm alone is gone. This
// covers the other half, which the intersection cannot reach. A scope whose first block sits inside
// the arm is asking a block where the fact is still true, and upstream's keying -- which this tree
// reproduces exactly -- hands it that block's set.
//
// The old assertion rejected that seed for every branch scope. That fixed the nested allocation in
// `error.useMemo-infer-less-specific-conditional-access`, but broke the paired top-level case
// `useMemo-conditional-access-own-scope`, where upstream's guard is exactly `propB.x.y`. The actual
// distinction is whether another reactive scope dominates the branch scope: only that nested shape
// falls back to entry facts. `TestBranchLocalHoistableFactsRespectDominatingReactiveScope` pins the
// pair end to end; this test pins the top-level dataflow answer directly.
//
// A fact from the untaken arm is still unavailable; that is the intersection invariant above, not
// a reason to erase facts at a scope that genuinely begins inside the taken arm.
func TestATopLevelScopeInsideABranchUsesItsFacts(test *testing.T) {
	test.Parallel()

	function, scopes, identity, ranges, _ := hoistableFor(test, `
		function Component(props) {
			if (props.flag) {
				return {value: props.deep.inner};
			}
			return null;
		}
	`)
	seeds := CollectHoistablePropertyLoads(function, scopes, identity, ranges, nil)
	found := false
	for scope, paths := range seeds {
		for _, path := range paths {
			if len(path.Path) == 0 {
				continue
			}
			// `deep` is dereferenced under the `if`, so the top-level scope beginning in that arm
			// must carry it and may descend to `inner`.
			if path.Path[0].Property == "deep" {
				found = true
				test.Logf("scope %d retains the branch-local `deep` fact", scope)
			}
		}
	}
	if !found {
		test.Error("the top-level branch scope lost `deep`, even though its body starts after that " +
			"dereference in the same branch")
	}
}

// TestHoistableHandlesNilInputs pins that the pass declines rather than panicking.
func TestHoistableHandlesNilInputs(test *testing.T) {
	test.Parallel()

	if got := analyseHoistableLoads(nil, nil, nil, nil, nil); got != nil {
		test.Error("a nil function produced an analysis")
	}
	if got := CollectHoistablePropertyLoads(nil, nil, nil, nil, nil); got != nil {
		test.Error("a nil function produced hoistable loads")
	}
	if got := CollectScopeDependenciesWithHoistable(nil, nil, nil, nil); got.Length() != 0 {
		test.Errorf("a nil function produced %d scopes", got.Length())
	}
	var empty *HoistableAnalysis
	if empty.Converged() || empty.Iterations() != 0 || empty.hoistableAt(1) != nil {
		test.Error("the nil analysis is not readable")
	}
}

// TestHoistableConflictsAreReportedNotSwallowed pins the divergence from oxc, now observable.
//
// oxc sorts its hoistable objects and silently takes first-wins on a conflicting access type,
// claiming to match TypeScript. React does neither: no sort, and a hard invariant on a conflict.
// This reports instead of raising, because a linter must not panic. Measured at zero conflicts
// across 400 files of real code, so this pins the zero rather than describing a behaviour that fires.
func TestHoistableConflictsAreReportedNotSwallowed(test *testing.T) {
	test.Parallel()

	function, scopes, identity, ranges, _ := hoistableFor(test, `
		function Component(props) {
			const object = {a: props.configuration.alpha};
			object.b = props.configuration.beta;
			return object;
		}
	`)
	after := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)
	if after.Conflicts() != 0 {
		test.Errorf("%d conflicting access types on ordinary input; upstream raises on any of these",
			after.Conflicts())
	}
}

// TestHoistableDistributionOverAMixedModule pins the shape over a whole module, through the production pipeline.
//
// TestHoistableCorpusDistribution read 150 files of Structure, a private repository, at a fixed commit, so it went
// with the corpus. The fixtures above each hold one shape; this holds a module written to mix them, the components
// and hooks of testdata/lowering/Dashboard.tsx, whose nested property reads and closures give the analysis paths to
// deepen and roots to keep. Every function is run through the same stages the deleted test ran, with a real checker.
//
// The counts are exact for the reason that test gave: each mutation of this analysis moves a different count by a
// small amount, a gain in `deep` is the over-approximating direction, and only an exact count sees either. The module
// is fixed, so a move here is the analysis moving.
func TestHoistableDistributionOverAMixedModule(test *testing.T) {
	test.Parallel()

	fileName := "/repository/source/Dashboard.tsx"
	deep, flat, notConverged, maximumIterations, conflicts, functions := 0, 0, 0, 0, 0, 0
	probe := rule.Rule[rule.NoOptions]{
		Name:             "hoistable-mixed-module",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ruleContext.TypeChecker == nil {
						test.Fatal("the typed harness handed this probe a nil checker, so every count below would " +
							"be a fact about the harness rather than the code")
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						function := Lower(functionNode, ruleContext.TypeChecker)
						if function == nil {
							return
						}
						functions++
						Construct(function)
						OutlineFunctions(function)
						InferReactive(function, ruleContext.TypeChecker)
						DropManualMemoization(function)
						if InlineImmediatelyInvokedFunctionExpressionsIncludingMemoCallbacks(function) > 0 {
							MergeConsecutiveBlocks(function)
						}
						EliminateDeadCode(function)
						ranges := InferMutableRanges(function)
						set := FindDisjointMutableValuesWithRanges(function, ranges)
						scopes := AssignReactiveScopesWithSets(function, ranges, set)
						aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
						identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
						BuildReactiveScopeTerminals(function, scopes, identity)

						if analysis := analyseHoistableLoads(function, scopes, identity, ranges, nil); analysis != nil {
							if !analysis.Converged() {
								notConverged++
							}
							maximumIterations = max(maximumIterations, analysis.Iterations())
						}
						after := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)
						conflicts += after.Conflicts()
						for _, scope := range after.Ids() {
							for _, dependency := range after.DependenciesOf(scope) {
								if len(dependency.Path) > 0 {
									deep++
								} else {
									flat++
								}
							}
						}
					})
				},
			}
		},
	}
	rule_testing.RunTypedFiles(test, probe, map[string]string{fileName: readMixedModule(test)}, fileName)

	if functions != 11 {
		test.Errorf("the probe lowered %d functions, want 11; the walk is not reaching the module", functions)
	}
	if notConverged != 0 {
		test.Errorf("%d functions failed to converge inside %d iterations", notConverged, hoistableIterationCap)
	}
	if conflicts != 0 {
		test.Errorf("%d conflicting access types, want 0; upstream raises on any", conflicts)
	}
	if deep != 9 || flat != 51 || maximumIterations != 2 {
		test.Errorf("got %d deep and %d flat dependencies in at most %d iterations, want 9, 51 and 2; a gain in "+
			"`deep` is the over-approximating direction unless an oracle says otherwise", deep, flat, maximumIterations)
	}
}
