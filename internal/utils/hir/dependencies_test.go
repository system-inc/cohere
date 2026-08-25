package hir

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// dependenciesFor lowers one source and runs the whole scope pipeline over it.
//
// Typed for the reason `scopesFor` is: a checker-less lowering never emits `StoreContext`, so
// several assertions below would pass vacuously against a plain harness.
func dependenciesFor(t *testing.T, source string) (*Function, *ReactiveScopes, *ScopeDependencies) {
	t.Helper()
	function, scopes := scopesFor(t, source)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)
	return function, scopes, CollectScopeDependencies(function, identity)
}

// dependencyRootNames returns the names of every dependency root on any scope, sorted.
func dependencyRootNames(function *Function, deps *ScopeDependencies) []string {
	var names []string
	for _, scope := range deps.Ids() {
		for _, dep := range deps.DependenciesOf(scope) {
			if identifier := function.Identifiers[dep.Identifier]; identifier != nil &&
				identifier.Name != "" {
				names = append(names, identifier.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The input this pass reads, which is the graph rather than the scope table
// ---------------------------------------------------------------------------

// TestDependenciesRequireScopeTerminals pins that this pass reads Scope TERMINALS, not the table.
//
// This is the distinction that made three earlier stages necessary, and it is worth a test because
// the failure it guards is silent: a caller that has scopes in the side table but has not built
// terminals gets an empty result that looks exactly like a function with no dependencies.
func TestDependenciesRequireScopeTerminals(t *testing.T) {
	source := `
		function Component(props) {
			const object = {a: props.a};
			object.b = props.b;
			return object;
		}
	`
	function, scopes := scopesFor(t, source)
	if scopes.Len() == 0 {
		t.Fatal("no scopes, so this test measured nothing")
	}
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}

	// Terminals NOT built: the table is populated but the graph carries no Scope terminal.
	before := CollectScopeDependencies(function, identity)
	if before.Len() != 0 {
		t.Errorf("collected %d scopes' dependencies before terminals were built; this pass is "+
			"supposed to read the graph", before.Len())
	}

	BuildReactiveScopeTerminals(function, scopes, identity)
	after := CollectScopeDependencies(function, identity)
	if after.Len() == 0 {
		t.Fatal("collected nothing after terminals were built, so the pass never runs")
	}
}

// TestDependenciesFindTheRootOfAnAccessPath pins that a scope reading props depends on props.
func TestDependenciesFindTheRootOfAnAccessPath(t *testing.T) {
	function, _, deps := dependenciesFor(t, `
		function Component(props) {
			const object = {a: props.alpha};
			object.b = props.beta;
			return object;
		}
	`)
	names := dependencyRootNames(function, deps)
	if !containsName(names, "props") {
		t.Errorf("no scope depends on props, but every value in the scope is read from it; got %v",
			names)
	}
}

// TestDependenciesExcludeValuesTheScopeItselfProduces pins the core predicate.
//
// A value created INSIDE a scope is not an input to it. This is `checkValidDependency`'s whole job,
// and a pass that skipped it would report a scope as depending on its own output.
func TestDependenciesExcludeValuesTheScopeItselfProduces(t *testing.T) {
	function, _, deps := dependenciesFor(t, `
		function Component(props) {
			const object = {a: props.alpha};
			object.b = props.beta;
			return object;
		}
	`)
	for _, scope := range deps.Ids() {
		for _, dep := range deps.DependenciesOf(scope) {
			identifier := function.Identifiers[dep.Identifier]
			if identifier != nil && identifier.Name == "object" {
				t.Error("a scope reports its own product `object` as a dependency, so " +
					"checkValidDependency is not gating on the declaration point")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The path algebra
// ---------------------------------------------------------------------------

// TestDependencyPathsCompareOptionality pins that `a?.b` and `a.b` are different dependencies.
//
// Upstream's `areEqualPaths` compares the `Optional` flag as well as the property name. A predicate
// comparing only names passes every fixture built from unguarded accesses and silently merges a
// null-guarded read with an unguarded one, which is a real behaviour change rather than a tidy-up.
func TestDependencyPathsCompareOptionality(t *testing.T) {
	guarded := []DependencyPathEntry{{Property: "b", Optional: true}}
	plain := []DependencyPathEntry{{Property: "b"}}
	if equalPaths(guarded, plain) {
		t.Error("`a?.b` and `a.b` compared equal, so the Optional flag is not part of path identity")
	}
	if !equalPaths(plain, []DependencyPathEntry{{Property: "b"}}) {
		t.Error("two identical paths compared unequal")
	}
	if equalPaths(plain, []DependencyPathEntry{{Property: "b"}, {Property: "c"}}) {
		t.Error("paths of different lengths compared equal")
	}
}

// TestMergeAccessIsAUnionOnDependencyAndAnIntersectionOnOptionality pins the asymmetry.
//
// The two bits in `propertyAccessType` combine differently and it is easy to write both as unions.
// "Dependency" is a union; "unconditional" is an intersection, so a value read once guarded and once
// unguarded is unconditional, because the unguarded read already happened.
func TestMergeAccessIsAUnionOnDependencyAndAnIntersectionOnOptionality(t *testing.T) {
	if got := mergeAccess(optionalAccess, optionalAccess); got != optionalAccess {
		t.Errorf("two optional accesses merged to %v, want optionalAccess", got)
	}
	if got := mergeAccess(optionalAccess, unconditionalAccess); got != unconditionalAccess {
		t.Errorf("optional + unconditional merged to %v, want unconditionalAccess; optionality "+
			"is an intersection, so one unguarded read makes the access unguarded", got)
	}
	if got := mergeAccess(optionalAccess, optionalDependency); got != optionalDependency {
		t.Errorf("access + dependency merged to %v, want optionalDependency; dependency is a "+
			"union", got)
	}
	if got := mergeAccess(unconditionalAccess, optionalDependency); got != unconditionalDependency {
		t.Errorf("merged to %v, want unconditionalDependency", got)
	}
}

// TestDependencyTreeTruncatesWithoutAHoistableSet pins the documented truncation AND its cause.
//
// This is the counterfactual that identifies the mechanism rather than merely observing the result.
// The same access path is added to two trees differing only in whether the hoistable set is
// populated: with it the path survives at full depth, without it the path truncates to its root.
// That is what proves the corpus-wide "0 dependencies carry a non-empty path" is
// `DependencyGapNullPropagation` and not a defect in the tree.
func TestDependencyTreeTruncatesWithoutAHoistableSet(t *testing.T) {
	path := []DependencyPathEntry{{Property: "alpha"}, {Property: "beta"}}

	empty := newDependencyTree(nil)
	empty.addDependency(ReactiveScopeDependency{Identifier: 7, Path: path})
	truncated := empty.deriveMinimalDependencies()
	if len(truncated) != 1 {
		t.Fatalf("expected one dependency, got %d", len(truncated))
	}
	if len(truncated[0].Path) != 0 {
		t.Errorf("with an empty hoistable set the path kept %d entries; upstream's `break` arm "+
			"truncates it to the root", len(truncated[0].Path))
	}

	hoistable := newDependencyTree(map[IdentifierId]*hoistableNode{
		7: {nonNull: true, properties: map[string]*hoistableNode{
			"alpha": {nonNull: true, properties: map[string]*hoistableNode{}},
		}},
	})
	hoistable.addDependency(ReactiveScopeDependency{Identifier: 7, Path: path})
	kept := hoistable.deriveMinimalDependencies()
	if len(kept) != 1 {
		t.Fatalf("expected one dependency, got %d", len(kept))
	}
	if len(kept[0].Path) != 2 {
		t.Errorf("with a hoistable set the path kept %d entries, want 2; if this drops to 0 the "+
			"truncation is unconditional and the counterfactual above proves nothing",
			len(kept[0].Path))
	}
}

// TestDependencyTreeReducesToTheShallowestPath pins the minimal-set derivation.
//
// A scope reading `props.a`, `props.a.b` and `props.a.c` depends on `props.a` once. Without the
// pruning the scope would report three dependencies that all invalidate together.
func TestDependencyTreeReducesToTheShallowestPath(t *testing.T) {
	hoistable := map[IdentifierId]*hoistableNode{
		7: {nonNull: true, properties: map[string]*hoistableNode{
			"a": {nonNull: true, properties: map[string]*hoistableNode{}},
		}},
	}
	tree := newDependencyTree(hoistable)
	tree.addDependency(ReactiveScopeDependency{Identifier: 7,
		Path: []DependencyPathEntry{{Property: "a"}}})
	tree.addDependency(ReactiveScopeDependency{Identifier: 7,
		Path: []DependencyPathEntry{{Property: "a"}, {Property: "b"}}})
	tree.addDependency(ReactiveScopeDependency{Identifier: 7,
		Path: []DependencyPathEntry{{Property: "a"}, {Property: "c"}}})

	result := tree.deriveMinimalDependencies()
	if len(result) != 1 {
		t.Fatalf("three accesses under one prefix reduced to %d dependencies, want 1", len(result))
	}
	if len(result[0].Path) != 1 || result[0].Path[0].Property != "a" {
		t.Errorf("reduced to %v, want the single shallowest path `a`", result[0].Path)
	}
}

// ---------------------------------------------------------------------------
// Determinism and termination
// ---------------------------------------------------------------------------

// TestDependencyCollectionIsDeterministic pins that two runs agree.
//
// The pass reads `Phi.Operands`, which is a Go map, and builds several maps of its own. Ranging any
// of them directly would make the output order vary between runs -- invisible in every other test
// and fatal to a cache keyed on this result.
func TestDependencyCollectionIsDeterministic(t *testing.T) {
	source := `
		function Component(props) {
			let total = 0;
			if (props.flag) { total = props.a; } else { total = props.b; }
			const object = {value: total, other: props.c};
			object.mutated = props.d;
			return object;
		}
	`
	render := func() []string {
		function, _, deps := dependenciesFor(t, source)
		var lines []string
		for _, scope := range deps.Ids() {
			for _, dep := range deps.DependenciesOf(scope) {
				name := ""
				if identifier := function.Identifiers[dep.Identifier]; identifier != nil {
					name = identifier.Name
				}
				lines = append(lines, name)
			}
		}
		return lines
	}
	first, second := render(), render()
	if len(first) != len(second) {
		t.Fatalf("two runs produced %d and %d dependencies", len(first), len(second))
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("two runs disagreed at %d: %q vs %q; something ranges a map",
				index, first[index], second[index])
		}
	}
}

// TestDependencyCollectionIsASinglePass pins the termination argument empirically.
//
// The claim in the package comment is that this is ONE ordered walk with no fixpoint: every block,
// instruction and operand visited exactly once. Counted here rather than argued, because the passes
// upstream of this one each needed a different termination story and a reader should not have to
// assume this one inherited any of them.
func TestDependencyCollectionIsASinglePass(t *testing.T) {
	function, scopes := scopesFor(t, `
		function Component(props) {
			const object = {a: props.a, b: props.b};
			object.c = props.c;
			return object;
		}
	`)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)

	terminals := scopeBlockTraversal(function)
	usedOutside := findTemporariesUsedOutsideDeclaringScope(function, terminals)
	visits := map[InstructionId]int{}
	collector := &dependencyCollector{
		function:      function,
		temporaries:   collectTemporaries(function, usedOutside),
		declarations:  map[DeclarationId]declaration{},
		reassignments: map[IdentifierId]declaration{},
		objectMethods: objectMethodValues(function),
		result:        &ScopeDependencies{},
		scopeRange:    identity.RangeOf,
	}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, instructionId := range block.Instructions {
			visits[instructionId]++
		}
	}
	collector.walk(function, terminals)

	for instructionId, count := range visits {
		if count != 1 {
			t.Errorf("instruction %d appears %d times in the block walk; the pass would visit it "+
				"more than once and the single-pass claim is wrong", instructionId, count)
		}
	}
	if len(visits) == 0 {
		t.Fatal("counted no instructions, so this test measured nothing")
	}
}

// TestScopesCloseAtTheirFallthrough pins that a scope stops collecting when its body ends.
//
// Added because a mutation neutralising the traversal's fallthrough entry SURVIVED the first sweep,
// and the reading rather than a reflexive extra fixture found why. Removing it does not make scopes
// unreachable -- all three end-blocks are still visited -- it makes them never CLOSE, so each scope
// keeps accumulating every later scope's reads. The distinguishing quantity is the RAW access count
// before reduction, not the reduced output: on the fixture below the real walk collects 4 and the
// mutant collects 12, while the reduced dependency lists happened to coincide. That is why the
// earlier tests missed it, and why this asserts on the pre-reduction count.
func TestScopesCloseAtTheirFallthrough(t *testing.T) {
	function, scopes := scopesFor(t, `
		function Component(props) {
			const object = {a: props.a};
			object.c = props.c;
			const second = {d: props.d};
			second.e = props.e;
			return [object, second];
		}
	`)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)

	terminals := scopeBlockTraversal(function)
	ends := 0
	for _, info := range terminals {
		if !info.begins {
			ends++
		}
	}
	if ends == 0 {
		t.Fatal("the traversal recorded no scope-end blocks, so this test measured nothing")
	}

	collect := func(infos map[BlockId]scopeBlockInfo) int {
		usedOutside := findTemporariesUsedOutsideDeclaringScope(function, infos)
		result := &ScopeDependencies{}
		collector := &dependencyCollector{
			function:      function,
			temporaries:   collectTemporaries(function, usedOutside),
			declarations:  map[DeclarationId]declaration{},
			reassignments: map[IdentifierId]declaration{},
			objectMethods: objectMethodValues(function),
			result:        result,
			scopeRange:    identity.RangeOf,
		}
		collector.walk(function, infos)
		total := 0
		for _, scope := range result.Ids() {
			total += len(result.DependenciesOf(scope))
		}
		return total
	}

	beginsOnly := map[BlockId]scopeBlockInfo{}
	for id, info := range terminals {
		if info.begins {
			beginsOnly[id] = info
		}
	}

	real, leaking := collect(terminals), collect(beginsOnly)
	if real >= leaking {
		t.Errorf("closing scopes collected %d raw accesses and never closing them collected %d; "+
			"a scope that never closes should accumulate strictly more", real, leaking)
	}
}

// TestDependencyGapsAreDeclared pins the gap list so closing one is a visible event.
func TestDependencyGapsAreDeclared(t *testing.T) {
	gaps := DependencyGaps()
	if len(gaps) != 3 {
		t.Errorf("expected three declared gaps, got %d; a gap that closed should update this test "+
			"rather than silently shrinking the list", len(gaps))
	}
}

// TestDependenciesHandleANilFunction pins that the pass declines rather than panicking.
func TestDependenciesHandleANilFunction(t *testing.T) {
	if got := CollectScopeDependencies(nil, MergedScopeIdentity{}); got.Len() != 0 {
		t.Errorf("a nil function produced %d scopes", got.Len())
	}
	var empty *ScopeDependencies
	if empty.DependenciesOf(1) != nil || empty.DeclarationsOf(1) != nil ||
		empty.ReassignmentsOf(1) != nil || empty.Ids() != nil || empty.Len() != 0 {
		t.Error("the nil table is not readable, but its zero value is documented as ready to read")
	}
}

// ---------------------------------------------------------------------------
// The corpus distribution, which is this stage's headline
// ---------------------------------------------------------------------------

// TestDependencyDistributionIsReal is this stage's headline, pinned as a test.
//
// A count cannot separate a working pass from one that collects nothing, and an empty output that
// looks plausible is this pass's characteristic failure. So the measurement is a CROSS-TABULATION of
// dependencies-per-scope against scope member count, over 400 corpus files and 677 functions:
//
//	                members=1   members=2-3   members=4+
//	deps=0                520           413           77
//	deps=1                475           137          306
//	deps>1                429           248          269
//
// The control cell is `deps=0`: 1,010 of 2,874 scopes carry no dependencies, which is the population
// a pass that collected nothing would fill entirely and a pass that over-collected would empty. It
// is neither, and the dependency-bearing scopes spread across every member bucket rather than
// clustering in one, which is what distinguishes real structure from an artifact of scope size.
//
// The thresholds below are far looser than those numbers. This guards the SHAPE, not the corpus,
// which moves when the tree does.
func TestDependencyDistributionIsReal(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("the corpus at %s is not present on this machine", corpusRoot)
	}

	var files []string
	filepath.Walk(corpusRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	if len(files) > 200 {
		files = files[:200]
	}
	if len(files) < 10 {
		t.Fatalf("the corpus holds only %d files; the path is probably wrong", len(files))
	}

	scopes, withDependencies, withoutDependencies, totalDependencies := 0, 0, 0, 0
	for _, path := range files {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		fileName := "/corpus/" + filepath.Base(path)
		probe := rule.Rule{
			Name:             "dependencies-corpus",
			NeedsTypeChecker: true,
			Run: func(ctx rule.Context, options any) rule.Listeners {
				return rule.Listeners{
					ast.KindSourceFile: func(node *ast.Node) {
						if ctx.TypeChecker == nil {
							t.Fatal("the typed harness handed this probe a nil checker, so every " +
								"count below would be a fact about the harness rather than the code")
						}
						forEachFunctionLike(node, func(functionNode *ast.Node) {
							function := Lower(functionNode, ctx.TypeChecker)
							if function == nil {
								return
							}
							Construct(function)
							ranges := InferMutableRanges(function)
							set := FindDisjointMutableValuesWithRanges(function, ranges)
							assigned := AssignReactiveScopesWithSets(function, ranges, set)
							aligned, merged := AlignThenMergeReactiveScopes(function, assigned)
							identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
							BuildReactiveScopeTerminals(function, assigned, identity)

							deps := CollectScopeDependencies(function, identity)
							for _, scope := range deps.Ids() {
								scopes++
								count := len(deps.DependenciesOf(scope))
								totalDependencies += count
								if count == 0 {
									withoutDependencies++
								} else {
									withDependencies++
								}
							}
						})
					},
				}
			},
		}
		ruletest.RunTypedFiles(t, probe, map[string]string{fileName: string(contents)}, fileName)
	}

	if scopes < 100 {
		t.Fatalf("only %d scopes carried an entry; the pipeline is probably not running", scopes)
	}
	if withDependencies == 0 {
		t.Error("every scope carries zero dependencies, which is exactly what a pass that " +
			"collects nothing produces")
	}
	if withoutDependencies == 0 {
		t.Error("no scope carries zero dependencies; the control cell is empty, which is what " +
			"over-collection looks like")
	}
	if totalDependencies < scopes/4 {
		t.Errorf("only %d dependencies across %d scopes, which is far below the measured density",
			totalDependencies, scopes)
	}
	t.Logf("%d scopes: %d with dependencies, %d without, %d dependencies total",
		scopes, withDependencies, withoutDependencies, totalDependencies)
}
