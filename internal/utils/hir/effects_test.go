package hir

import (
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// effectsFor lowers one function and returns its effect table plus the function.
//
// Runs through the TYPED harness even though this pass reads no types, because `Lower` refuses a
// nil checker and would otherwise hand back nil, which reads as "the pass found nothing" rather
// than "the pass never ran". The explicit fatal below is the guard the brief calls for: a typed
// probe handed a nil checker passes vacuously, and vacuous green is more dangerous than a crash.
func effectsFor(t *testing.T, source string) (*Function, *AliasingEffects) {
	t.Helper()

	var function *Function
	var table *AliasingEffects
	probe := rule.Rule{
		Name:             "effects-harness",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						t.Fatal("the typed harness handed this probe a nil checker, so every " +
							"assertion below would pass vacuously")
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						if function != nil {
							return
						}
						lowered := Lower(functionNode, ctx.TypeChecker)
						if lowered == nil {
							return
						}
						Construct(lowered)
						function = lowered
						table = InferAliasingEffects(lowered)
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{"fixture.tsx": source}, "fixture.tsx")
	if function == nil || table == nil {
		t.Fatal("nothing was lowered, so this test measured nothing")
	}
	return function, table
}

// effectsOn returns the sorted set of effect kinds naming one source binding, as strings.
//
// Keyed by the binding's DECLARATION rather than by IdentifierId or by name, and that distinction
// cost the first version of this file every one of its assertions.
//
// Single-assignment form gives one source binding several numbered values, and only the value the
// programmer wrote carries `Identifier.Name`; the rest are temporaries with an empty name that
// share the original's `DeclarationId`. So `o.a = 1` records its mutation against the temporary
// produced by the LoadLocal of `o`, not against the named value, and a name-keyed assertion sees
// `assign` on the named one and nothing else. Every effect assertion here failed that way while the
// pass was correct, which is the dispatch's "compare declarations, not identifier ids" arriving in
// a test helper rather than in a rule.
//
// Resolving the name to a DeclarationId first, then matching any identifier carrying it, is what
// makes an assertion about "what happens to `o`" mean what it says.
func effectsOn(function *Function, table *AliasingEffects, name string) []string {
	wantedDeclarations := map[DeclarationId]bool{}
	for _, identifier := range function.Identifiers {
		if identifier != nil && identifier.Name == name {
			wantedDeclarations[identifier.Declaration] = true
		}
	}
	if len(wantedDeclarations) == 0 {
		return nil
	}

	// Seed with every value carrying the binding's declaration, then follow assignment edges
	// forward. A `LoadLocal` of `o` produces a FRESH temporary with its own DeclarationId, and it
	// is that temporary the property store mutates, so a set closed only over the declaration stops
	// one hop short of every interesting effect. Closing over Assign edges is what makes "the
	// effects on `o`" include what happens to the value just loaded out of it.
	wanted := map[IdentifierId]bool{}
	for index, identifier := range function.Identifiers {
		if identifier != nil && wantedDeclarations[identifier.Declaration] {
			wanted[IdentifierId(index)] = true
		}
	}
	for round := 0; round < len(function.Instructions)+1; round++ {
		grew := false
		for _, instruction := range function.Instructions {
			if instruction == nil {
				continue
			}
			for _, effect := range table.Get(instruction.Id) {
				if effect.Kind != AliasingEffectAssign || !effect.HasFrom {
					continue
				}
				if wanted[effect.From.Identifier] && !wanted[effect.Into.Identifier] {
					wanted[effect.Into.Identifier] = true
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}

	found := map[string]bool{}
	matches := func(id IdentifierId) bool { return wanted[id] }
	for _, instruction := range function.Instructions {
		if instruction == nil {
			continue
		}
		for _, effect := range table.Get(instruction.Id) {
			if matches(effect.Into.Identifier) {
				found[effect.Kind.String()+":into"] = true
			}
			if effect.HasFrom && matches(effect.From.Identifier) {
				found[effect.Kind.String()+":from"] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for key := range found {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func hasEffect(list []string, want string) bool {
	for _, entry := range list {
		if entry == want {
			return true
		}
	}
	return false
}

// TestPropertyStoreMutatesItsObject is the single most load-bearing fact this pass produces.
//
// `o.a = 1` mutates `o`. Everything downstream -- a mutable range's extension, a frozen-value
// violation, an escape verdict -- reads this one effect. Asserted on the SPECIFIC place rather than
// on a count, because the brief records a phi mutant that survived a name-keyed assertion while the
// merge rule was dead.
func TestPropertyStoreMutatesItsObject(t *testing.T) {
	function, table := effectsFor(t, `function f() { const o = { a: 0 }; o.a = 1; return o; }`)

	effects := effectsOn(function, table, "o")
	if !hasEffect(effects, "mutate:into") {
		t.Fatalf("a property store must record a mutation of its object; got %v", effects)
	}
}

// TestPropertyStoreCapturesTheStoredValue pins the second half of a store.
//
// `o.a = v` does not only mutate `o`, it records that `o` now holds `v`. Without that edge an
// escape analysis cannot follow a value into a structure, which is the whole point of the pass.
func TestPropertyStoreCapturesTheStoredValue(t *testing.T) {
	function, table := effectsFor(t, `function f(v: object) { const o = { a: {} }; o.a = v; return o; }`)

	effects := effectsOn(function, table, "v")
	if !hasEffect(effects, "capture:from") {
		t.Fatalf("storing v into o must record a capture FROM v; got %v", effects)
	}
}

// TestUnknownCallConservativelyMutatesItsArguments is the 85.4% path and the safety property.
//
// A call with no signature must be assumed to mutate what it is handed. The direction matters more
// than the specific variant: an unknown call that recorded NO mutation would make every consumer
// under-report, and an under-reporting escape analysis is unsound in the dangerous direction.
func TestUnknownCallConservativelyMutatesItsArguments(t *testing.T) {
	function, table := effectsFor(t, `function f() { const o = { a: 0 }; unknownFunction(o); return o; }`)

	effects := effectsOn(function, table, "o")
	if !hasEffect(effects, "mutate-transitive-conditionally:into") {
		t.Fatalf("an unknown callee must be assumed to mutate its argument; got %v", effects)
	}
}

// TestKnownReadOnlySignatureDoesNotMutate is the other side of the same measurement.
//
// Two-sided against the test above: `includes` carries Effect::Read on its receiver upstream, so it
// must NOT produce a mutation, while `unknownFunction` must. If the signature table were dead, both
// tests would still describe real behaviour and only this one would fail, which is why it exists
// rather than a bare assertion that some call produced some effect.
func TestKnownReadOnlySignatureDoesNotMutate(t *testing.T) {
	function, table := effectsFor(t, `function f(list: number[]) { const found = list.includes(1); return found; }`)

	effects := effectsOn(function, table, "list")
	if hasEffect(effects, "mutate:into") || hasEffect(effects, "mutate-transitive-conditionally:into") {
		t.Fatalf("includes is Effect::Read on its receiver upstream and must not mutate it; got %v", effects)
	}
}

// TestKnownMutatingSignatureMutatesItsReceiver pins the table's positive arm.
//
// `push` carries Effect::Store upstream and is the single most common mutating method on Kirk's
// tree at 2,202 call sites. Measured against React's executable: a frozen array's `push` reports
// while `sort` on the same array is clean, because `sort` is absent from upstream's table.
func TestKnownMutatingSignatureMutatesItsReceiver(t *testing.T) {
	function, table := effectsFor(t, `function f() { const list: number[] = []; list.push(1); return list; }`)

	effects := effectsOn(function, table, "list")
	if !hasEffect(effects, "mutate:into") {
		t.Fatalf("push carries Effect::Store upstream and must mutate its receiver; got %v", effects)
	}
}

// TestSortIsSilentBecauseUpstreamHasNoSignatureForIt reproduces a defect deliberately.
//
// `sort` mutates its receiver in fact, and React does not report it, because it is not in
// `globals.rs`. Measured on React's executable with a control in the same batch: a frozen array's
// `push` REPORTS and the same array's `sort` is CLEAN.
//
// This is the "do not silently improve on upstream" rule from the brief, pinned as a test so a
// later reader who adds `sort` to the table sees this go red and reads why rather than discovering
// the divergence from a differential run.
func TestSortIsSilentBecauseUpstreamHasNoSignatureForIt(t *testing.T) {
	if _, found := effectSignatures["sort"]; found {
		t.Fatal("sort was added to the signature table; upstream has no entry for it and React " +
			"is measurably silent on a frozen array's sort, so adding it is a divergence that " +
			"needs recording rather than a fix")
	}
	if _, found := effectSignatures["push"]; !found {
		t.Fatal("the control failed: push must be in the table, or the assertion above is " +
			"satisfied by an empty table rather than by a deliberate omission")
	}
}

// TestJsxFreezesWhatItInterpolates is the one React-flavoured effect, and it generalises.
//
// A value read into JSX is frozen from that point. `immutability.go` reports on exactly this, and
// its own doc comment records the ordering pair that proves freezing is a dataflow fact rather than
// a property of a value's origin.
func TestJsxFreezesWhatItInterpolates(t *testing.T) {
	function, table := effectsFor(t, `function C() { const o = { a: 0 }; return <div>{o}</div>; }`)

	effects := effectsOn(function, table, "o")
	if !hasEffect(effects, "freeze:into") {
		t.Fatalf("a value interpolated into JSX must be frozen; got %v", effects)
	}
}

// TestAwaitConditionallyMutatesTheAwaitedValue is the async-boundary fact.
//
// This is the effect a rule asking "was this object mutated after crossing an async boundary" would
// read, and it is named in the dispatch as one of the things this substrate makes possible.
func TestAwaitConditionallyMutatesTheAwaitedValue(t *testing.T) {
	function, table := effectsFor(t, `async function f(p: Promise<object>) { const v = await p; return v; }`)

	effects := effectsOn(function, table, "p")
	if !hasEffect(effects, "mutate-transitive-conditionally:into") {
		t.Fatalf("an await must record that anything may have mutated the value; got %v", effects)
	}
}

// TestEveryInstructionShapeProducesEffects is the coverage guard, two-sided.
//
// Measured on Kirk's tree: 796,725 instructions carry at least one effect and 766 do not, and every
// one of the 766 is an `UnsupportedNode`, which lowering documents as syntax it does not model. So
// coverage is total over every shape the representation actually produces.
//
// The assertion is on the SHAPE rather than on the count, because a count would drift with the
// corpus while the invariant does not: any instruction value other than UnsupportedNode that
// produces no effect is a hole in the switch.
func TestEveryInstructionShapeProducesEffects(t *testing.T) {
	source := `
function f(a: number[], o: { x: number }, p: Promise<number>, fn: (n: number) => number) {
	const sum = a.length + 1;
	const neg = -sum;
	const text = ` + "`v${sum}`" + `;
	let counter = 0;
	counter++;
	--counter;
	const object = { x: 1, [text]: 2, ...o };
	const array = [1, ...a];
	o.x = 5;
	object[text] = 6;
	delete object.x;
	const read = o.x;
	const computed = array[0];
	const called = fn(1);
	const method = a.includes(2);
	const made = new Map<string, number>();
	const closure = () => counter;
	const cast = o as { x: number };
	for (const item of a) { counter += item; }
	return { sum, neg, text, object, array, read, computed, called, method, made, closure, cast, counter };
}`
	function, table := effectsFor(t, source)

	uncovered := map[string]int{}
	for _, instruction := range function.Instructions {
		if instruction == nil {
			continue
		}
		if len(table.Get(instruction.Id)) > 0 {
			continue
		}
		if _, unsupported := instruction.Value.(*UnsupportedNode); unsupported {
			continue
		}
		uncovered[strings.TrimPrefix(typeName(instruction.Value), "*hir.")]++
	}
	if len(uncovered) > 0 {
		t.Fatalf("these instruction shapes produced no effects and are not UnsupportedNode: %v", uncovered)
	}

	// The control. Without it this test is satisfied by a pass that produces nothing at all, for
	// every shape, since a table with no entries has no uncovered shapes either.
	if table.Len() == 0 {
		t.Fatal("the control failed: no instruction produced any effect, so the assertion above " +
			"passed vacuously")
	}
}

// TestEffectsAreDeterministic guards the hazard the package comment names.
//
// `Phi.Operands` is a Go map and this pass never reads one, so the result must be byte-identical
// across runs. Written so that a later addition which does reach for a phi fails here rather than
// producing a message that names a different value between runs, which is a defect this tree has
// already shipped once in `static_components.go`.
func TestEffectsAreDeterministic(t *testing.T) {
	source := `
function f(c: boolean, a: object, b: object) {
	let value = a;
	if (c) { value = b; }
	use(value);
	return value;
}
declare function use(v: object): void;`

	function, first := effectsFor(t, source)
	second := InferAliasingEffects(function)

	render := func(table *AliasingEffects) string {
		var lines []string
		for _, instruction := range function.Instructions {
			if instruction == nil {
				continue
			}
			for _, effect := range table.Get(instruction.Id) {
				lines = append(lines, effect.Kind.String()+
					":"+itoa(int(effect.From.Identifier))+
					"->"+itoa(int(effect.Into.Identifier)))
			}
		}
		return strings.Join(lines, "\n")
	}

	if render(first) != render(second) {
		t.Fatal("two runs of the pass over one function disagreed, so something in it reads a map")
	}
	if render(first) == "" {
		t.Fatal("the control failed: both runs produced nothing, so the comparison was vacuous")
	}
}

// TestProjectEffectsNeedsRangesToDistinguishCaptureFromRead pins the reason Place.Effect is not
// written by this pass.
//
// The five aliasing variants all resolve their SOURCE to either Capture or Read, and the only
// discriminator is whether the target is still mutable after this point -- a fact that lives in the
// mutable-range table, which Stage 2 owns. With a nil range table every aliasing edge reads as
// Read, which under-approximates escape.
//
// This is the test that would go red if someone decided the projection could be done without
// ranges, which is the specific wrong turn the package comment argues against.
func TestProjectEffectsNeedsRangesToDistinguishCaptureFromRead(t *testing.T) {
	from := Place{Identifier: IdentifierId(1)}
	into := Place{Identifier: IdentifierId(2)}
	effects := []AliasingEffect{{Kind: AliasingEffectCapture, From: from, Into: into, HasFrom: true}}

	withoutRanges := ProjectEffects(effects, nil, EvaluationOrder(5))
	if withoutRanges[from.Identifier] != EffectRead {
		t.Fatalf("with no range table an aliasing source must under-approximate to Read, got %v",
			withoutRanges[from.Identifier])
	}

	ranges := &MutableRanges{}
	ranges.set(into.Identifier, MutableRange{Start: EvaluationOrder(1), End: EvaluationOrder(9)})
	withRanges := ProjectEffects(effects, ranges, EvaluationOrder(5))
	if withRanges[from.Identifier] != EffectCapture {
		t.Fatalf("a source flowing into a still-mutable target must be Capture, got %v",
			withRanges[from.Identifier])
	}

	// Both directions asserted, so a projection that ignored ranges entirely fails one of them
	// rather than satisfying both by always answering the same thing.
	if withoutRanges[into.Identifier] != EffectStore || withRanges[into.Identifier] != EffectStore {
		t.Fatal("the target of an aliasing edge is Store regardless of the range test")
	}
}

// TestEffectGapsAreNamedInTheApi pins the direction the dispatch asked for.
//
// A half-filled effect table is worse than an empty one because rules read it and believe it, and
// both React and oxc treat an unknown effect as an invariant violation rather than a skippable
// default. So the gaps are enumerable rather than only described, the way `ReactiveGaps` and
// `RangeGaps` already are.
//
// This test goes RED if a gap is ever closed, which is deliberate: closing one should be a
// deliberate edit here rather than a silent change in what a consumer can trust.
func TestEffectGapsAreNamedInTheApi(t *testing.T) {
	gaps := EffectGaps()
	if len(gaps) != 3 {
		t.Fatalf("EffectGaps changed; if a gap was closed, update the consumers that ask. got %v", gaps)
	}
	seen := map[string]bool{}
	for _, gap := range gaps {
		if gap.String() == "<unknown>" {
			t.Fatalf("a gap has no name, so a caller enumerating them learns nothing: %d", gap)
		}
		seen[gap.String()] = true
	}
	for _, want := range []string{"interprocedural-parameters", "signature-table", "type-directed-shapes"} {
		if !seen[want] {
			t.Fatalf("the %q gap is no longer named; see the package comment before removing it", want)
		}
	}
}

// TestInterproceduralParametersAreNotInferred states the largest gap as a test rather than a hope.
//
// A local function that mutates its parameter is invisible to this pass. Measured against React's
// executable, React is ALSO silent on the same input when the callee is reached by name --
// `function m(x){x.a=1;} m(frozen)` is CLEAN upstream -- so the observable divergence on React's
// own diagnostics is narrow even though the general capability is missing entirely.
//
// Pinned so that implementing it is a deliberate act: this test goes red the moment the effects of
// a named local callee start reaching its call site, which is exactly when every consumer's
// assumptions change.
func TestInterproceduralParametersAreNotInferred(t *testing.T) {
	function, table := effectsFor(t, `
function outer() {
	function inner(target: { a: number }) { target.a = 1; }
	const value = { a: 0 };
	inner(value);
	return value;
}`)

	effects := effectsOn(function, table, "value")

	// The conservative default still records a mutation, which is the safe direction. What is
	// missing is the PRECISE one: upstream would record `Mutate` from inner's own body, and this
	// records only the unknown-callee `MutateTransitiveConditionally`.
	if !hasEffect(effects, "mutate-transitive-conditionally:into") {
		t.Fatalf("the conservative default must still fire for an un-inferred callee; got %v", effects)
	}
	if hasEffect(effects, "mutate:into") {
		t.Fatal("a precise Mutate reached the call site, which means interprocedural parameter " +
			"inference now works; remove EffectGapInterproceduralParameters and update the " +
			"package comment, which currently tells consumers it is missing")
	}
}

// typeName is a local %T without pulling fmt into the non-test file.
func typeName(value InstructionValue) string {
	switch value.(type) {
	case *StartMemoize:
		return "*hir.StartMemoize"
	case *FinishMemoize:
		return "*hir.FinishMemoize"
	case *UnsupportedNode:
		return "*hir.UnsupportedNode"
	default:
		return "*hir.<other>"
	}
}

// itoa is shared with lower_corpus_test.go, which declared it first.

// TestCaptureReceiverIsAliasedExactlyOnce pins upstream's receiver-alias guard.
//
// `compute_effects_for_legacy_signature` aliases the receiver into the result UNLESS the receiver's
// own signature effect is Capture. That is not "a Capture receiver is never aliased": it is
// deduplication. A Capture receiver lands in the `captures` list and the reconciliation at the end
// of the function emits `Alias{capture -> lvalue}` for it, so emitting the branch alias as well
// would record the same edge TWICE.
//
// Added for a surviving mutant that inverted the guard, and the first fixture written for it was
// WRONG even though the mutant was real: it asserted that a Capture receiver produces no Alias at
// all, and the pass produces exactly one, from the reconciliation. The brief's rule applied
// literally -- a fixture written for a survivor is a hypothesis, and a failing one means the
// hypothesis needs replacing rather than the code.
//
// So the property is the COUNT. `map` has a Capture receiver and must yield exactly one Alias;
// `includes` has a Read receiver, never enters the captures list, and must also yield exactly one,
// from the branch instead. The inverted mutant gives `map` two and `includes` zero.
func TestCaptureReceiverIsAliasedExactlyOnce(t *testing.T) {
	if effectSignatures["map"].Receiver != EffectCapture {
		t.Fatal("the control failed: map must carry a Capture receiver or this test measures nothing")
	}
	if effectSignatures["includes"].Receiver != EffectRead {
		t.Fatal("the control failed: includes must carry a Read receiver")
	}

	countAliases := func(source string) int {
		function, table := effectsFor(t, source)
		total := 0
		for _, instruction := range function.Instructions {
			if instruction == nil {
				continue
			}
			if _, isMethod := instruction.Value.(*MethodCall); !isMethod {
				continue
			}
			for _, effect := range table.Get(instruction.Id) {
				if effect.Kind == AliasingEffectAlias {
					total++
				}
			}
		}
		return total
	}

	if got := countAliases(`function f(list: number[]) { const mapped = list.map(n => n); return mapped; }`); got != 1 {
		t.Fatalf("a Capture receiver must be aliased exactly once, through the capture "+
			"reconciliation rather than through the branch; got %d", got)
	}
	if got := countAliases(`function g(list: number[]) { const found = list.includes(1); return found; }`); got != 1 {
		t.Fatalf("a Read receiver must be aliased exactly once, through the branch; got %d", got)
	}
}

// TestAnUnknownMethodCallDoesNotMutateTheMethodItself pins the mutatesCallee guard.
//
// Upstream's default path skips the mutation for an operand that IS the callee when the call does
// not mutate its function -- true for a method call and a construction, false for a plain call
// (`infer_mutation_aliasing_effects.rs:1841-1848`). So `lib.doThing(v)` conservatively mutates
// `lib` and `v`, and does NOT record that calling `doThing` mutated `doThing`.
//
// Added for a surviving mutant that inverted the guard, and the distinguishing input is a METHOD
// call specifically: for a plain call the receiver and the callee are one identifier and
// mutatesCallee is true, so the guard never fires and both versions agree. The first hypothesis
// reached for a plain call and would have been a fixture that could not see the mutation, which is
// the brief's "name the input on which the two versions produce different output" catching a wrong
// guess one step before it became a test.
func TestAnUnknownMethodCallDoesNotMutateTheMethodItself(t *testing.T) {
	function, table := effectsFor(t, `
function f(lib: { doThing(v: object): void }, v: object) { lib.doThing(v); return 1; }`)

	var call *Instruction
	var property Place
	for _, instruction := range function.Instructions {
		if instruction == nil {
			continue
		}
		if method, isMethod := instruction.Value.(*MethodCall); isMethod {
			call = instruction
			property = method.Property
		}
	}
	if call == nil {
		t.Fatal("the control failed: no method call was lowered, so nothing below is measured")
	}

	mutatedProperty := false
	mutatedAnything := false
	for _, effect := range table.Get(call.Id) {
		if effect.Kind != AliasingEffectMutateTransitiveConditionally {
			continue
		}
		mutatedAnything = true
		if effect.Into.Identifier == property.Identifier {
			mutatedProperty = true
		}
	}

	if !mutatedAnything {
		t.Fatal("the control failed: the unknown-callee default recorded no mutation at all, so " +
			"the assertion below is satisfied by a dead default rather than by the guard")
	}
	if mutatedProperty {
		t.Fatal("calling a method must not record that the method itself was mutated; " +
			"mutatesCallee is false for a MethodCall")
	}
}
