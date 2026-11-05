package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	utilsreact "github.com/system-inc/cohere/internal/lint/ecmascript/react"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/static_single_assignment"
)

// messagePurityImpureCallId is the finding's id, and messagePurityImpureCallReason is everything
// after the sentence naming the builtin.
//
// Split rather than held as one `rule.Message` for the reason `globals.go` gives: this rule
// interpolates, the builtin's name is the part a reader needs first, and the invariant half lives
// in one place so a single edit moves every finding. `TestPurityMessageNamesTheBuiltin` asserts the
// rendered result rather than either half, which is the only assertion a format string cannot
// satisfy by accident.
const messagePurityImpureCallId = "impureFunctionCall"

const messagePurityImpureCallReason = "Calling an impure function during render makes the " +
	"component's output depend on when it happened to render rather than only on its props and " +
	"state. React may render a component more than once for a single update, and under Strict " +
	"Mode it deliberately does, so the value changes between renders that should have been " +
	"identical and the interface flickers or resets. Move the call into an event handler or an " +
	"effect, or lift the value into state so it is computed once."

// purityImpureBuiltins is the table, and it is the whole table.
//
// Three entries, which is not a simplification. `grep -c "impure: true"` over
// `oxc_react_compiler/src/react_compiler_hir/globals.rs` returns 3, and the same three appear in
// React's own `DEFAULT_SHAPES` at `eslint-plugin-react-hooks.development.js:30186,30203,30281`,
// each carrying the `canonicalName` this rule prints. Nothing else in either implementation sets
// the flag, so a fourth entry here would be an invention rather than a port.
//
// Keyed by the object name and then the property, because both halves are checked: `Math.floor` is
// pure and `new Date()` is pure, so a rule keyed on the object alone or on the property alone would
// report both. Measured on React, both are silent.
var purityImpureBuiltins = map[string]map[string]string{
	"Math":        {"random": "Math.random"},
	"Date":        {"now": "Date.now"},
	"performance": {"now": "performance.now"},
}

// purityGlobalContainers are the global objects whose properties are themselves the modelled
// globals, so `globalThis.Date.now()` reaches the same signature as `Date.now()`.
//
// Measured rather than assumed, and the asymmetry is real: `globalThis.Date.now()` reports while
// `window.Date.now()` is silent, on React's own rule. The cause is in the table rather than in the
// analysis. `globals.rs:1280` registers `globalThis` with `add_object(shapes, ..., typed_globals)`,
// handing it every typed global as a property, and `global` beside it; `window` is not registered
// at all, so `window.Date` loads an unknown property of an unknown object and the chain dies there.
//
// Reproduced rather than improved on. Treating `window` as an alias of the global object would be
// the sensible reading and it is a divergence, because upstream does not do it.
var purityGlobalContainers = map[string]bool{"globalThis": true, "global": true}

// Purity flags a call to a known-impure builtin during render.
//
//	valid:   function Component() { const f = () => Math.random(); return <div onClick={f} />; }
//	valid:   function Component() { useEffect(() => { console.log(Math.random()); }, []); return <div />; }
//	valid:   function Component() { const [x] = useState(() => Math.random()); return <div>{x}</div>; }
//	valid:   function Component() { const Math = {random: () => 1}; return <div>{Math.random()}</div>; }
//	valid:   function Component() { return <div>{Math.floor(1.5)}</div>; }
//	valid:   function helper() { return Math.random(); }
//	invalid: function Component() { const d = Date.now(); return <div>{d}</div>; }
//	invalid: function Component() { const r = Math.random; return <div>{r()}</div>; }
//	invalid: function Component() { const f = () => Math.random(); return <div>{f()}</div>; }
//	invalid: function Component() { const v = useMemo(() => Math.random(), []); return <div>{v}</div>; }
//
// Ported from React's `purity` rule, `ErrorCategory.Purity` in the React Compiler. Both authorities
// were read and React was run rather than reasoned about, which is what produced most of what is
// recorded below:
//
//	react 7.1.1   node_modules/eslint-plugin-react-hooks/cjs/eslint-plugin-react-hooks.development.js
//	              driven through the ESLint Linter API on 50-plus probe inputs
//	oxc           crates/oxc_react_compiler, emitting at
//	              react_compiler_inference/infer_mutation_aliasing_effects.rs:2425
//
// # The corpus is two files and it cannot see this rule at all
//
// React ships exactly two fixtures whose golden expects this diagnostic, and they are the same
// source twice, differing only in a second pragma:
//
//	function Component() {
//	  const date = Date.now();
//	  const now = performance.now();
//	  const rand = Math.random();
//	  return <Foo date={date} now={now} rand={rand} />;
//	}
//
// Three direct calls, at the top level of a component, with no nesting, no aliasing and no
// conditional. There is not one valid fixture anywhere in the 650-file corpus containing any of the
// three builtins. So a rule that reported on any lexical occurrence of `Math.random` anywhere in
// any file would score 2 of 2, and a rule that is correct scores 2 of 2, and the corpus cannot tell
// them apart.
//
// That is why almost every sentence below cites a probe rather than a fixture. The corpus is a
// floor here in the weakest possible sense, and treating its pass rate as evidence about this rule
// would be the port brief's "a passing case is not evidence about the thing you think it is",
// arriving through the absence of cases rather than through a misleading one.
//
// # The rule is not a lexical match on three names, and that is the whole design
//
// The obvious port is "find a call whose callee spells one of three builtins, inside a component".
// It is wrong in both directions and each was measured on React's own rule:
//
//	const f = () => Math.random(); const x = f();    REPORTS, at the inner call
//	const f = () => Math.random(); <div onClick={f} />   SILENT, same lexical text
//
// One character of difference in whether the wrapper is called decides it, and the lexical rule
// gets one of the two wrong whichever way it answers. The same pair separates `useMemo`, whose
// callback runs during render and reports, from `useCallback`, whose callback does not and is
// silent, with byte-identical callbacks.
//
// And the span moves:
//
//	const r = Math.random; const x = r();            REPORTS, span is `r()`
//	let r = Math.random; r = Date.now; const x = r(); REPORTS `Date.now`, span is `r()`
//
// The second is the one that settles the design. The name printed is `Date.now`, the span is on
// `r`, and no reading of the syntax at the call site can produce either. What upstream is doing is
// following the value: the impure signature is attached to the value loaded out of the global, and
// it travels through assignment, reassignment and control flow to whatever eventually gets called.
// That is single-assignment form with phi nodes, which is exactly what `internal/utilities/hir` is.
//
// So this rule uses the intermediate representation, and the decision was made by measuring the
// alternative rather than by preferring the machinery. Three rules shipped the same day
// deliberately used none of it; this one cannot, and the reassignment probe above is the one-line
// reason.
//
// # What is tracked, stated as the small abstract domain it is
//
// Every value in a lowered function falls in one of four states, and the pass is a single forward
// walk over blocks in reverse postorder assigning them:
//
//	container   the global object `Math`, `Date`, `performance`, or `globalThis`/`global`
//	impure      a value that IS one of the three impure functions, carrying its canonical name
//	callImpure  a nested function that, when called, performs an impure call
//	nothing     everything else
//
// A `MethodCall` on a container whose property names an impure builtin reports. A `CallExpression`
// on an `impure` value reports. A `CallExpression` on a `callImpure` value reports at the site
// recorded inside that function, which is why the nested-arrow finding points at the inner call
// rather than at the call that triggered it, exactly as React does.
//
// Reverse postorder makes one pass correct for everything but a back edge, which is the same
// limitation `static_components.go` records and reproduces.
//
// # Why the type checker is declared when nothing here reads a type
//
// This rule asks no type question. It declares `NeedsTypeChecker` because lowering does: without a
// checker, `internal/utilities/hir` resolves every free identifier to `LoadGlobal` carrying a name, and
// a shadowed binding becomes indistinguishable from the real global. Measured directly, with a nil
// checker `function Component() { const Math = {random: () => 1}; return Math.random(); }` lowers
// to `LoadGlobal Math` and this rule would report it; React is silent on that input, and so is this
// rule once the checker resolves the local binding. The declaration buys shadow correctness and
// nothing else, and `TestPurityRequiresTheTypedHarness` pins it.
//
// # The gate, taken whole and not re-derived
//
// `IsComponentOrHookLike` comes from the react shelf, written for `unsupported-syntax`. Two porters
// paid seventeen probe rounds and seven false positives for it, and a second copy here would be a
// second thing to keep in agreement with React. It is the right gate rather than a convenient one,
// verified on this rule's
// own diagnostics: `function helper() { return Math.random(); }` is silent, `function Component() {
// return Math.random(); }` is also silent because it creates no JSX and calls no hook, and a class
// `render()` is silent. All three measured on React.
//
// A function the gate declines is still descended into, because a component nested inside a plain
// wrapper is a unit even when the wrapper is not. Measured: a `function Inner()` declared inside
// `function Outer()` reports on its own body.
//
// # Four silences that are upstream's, three reproduced and one a real gap
//
// Each was measured on React's own rule, on the input named. The first is this port falling short
// of upstream; the other three are upstream's own behaviour, reproduced deliberately.
//
//   - **A module-scope helper is opaque.** `const g = () => Math.random(); function Component() {
//     const x = g(); ... }` is silent, while the same helper declared inside the component reports.
//     Upstream only knows a function's effects when it lowered that function as part of the unit,
//     and a module-scope one is not part of it. This is the largest hole in the rule and it is
//     upstream's.
//
//   - **`useEffect` and its neighbours freeze their arguments.** `useEffect(() => {
//     Math.random(); }, [])` is silent because `globals.rs:1409` attaches
//     `Freeze { value: "@rest" }` to the signature, and a frozen function's effects are not
//     re-raised on the caller. `useLayoutEffect` behaves the same. But `setTimeout(() => {
//     Math.random(); })` reports, because `setTimeout` carries no such signature. Two inputs of
//     identical shape falling opposite ways, decided entirely by a table.
//
//   - **Any hook-named callee freezes a callback argument too.** `useCustom(() => {
//     Math.random(); })` is silent while `doThing(() => { Math.random(); })` reports. The
//     discrimination is the hook name pattern on the callee, nothing about the function passed.
//
//   - **A property never carries the signature.** `const o = {r: Math.random}; o.r();` is silent,
//     though `const r = Math.random; r();` reports. Storing into an object loses the value, and
//     upstream does not chase it back out.
//
// # The one place this port is narrower than upstream, stated because the sweep found it
//
// Upstream reports when an impure value escapes to somewhere it might be called, not only where it
// is called. Measured: `const f = () => Math.random(); doThing(f);` and `f.z = 1;` both report at
// the inner `Math.random`, and so does `const x = f.Date.now();`, while `const x = f.Date;` alone
// is silent. This port re-raises a nested function's effects when the function is called or passed
// as a callback argument, which covers the shapes that matter, and says nothing when the value
// escapes by being mutated or read through a property chain.
//
// Closing it means the escape half of the mutation aliasing model, which is the machinery
// `immutability` is blocked on. Guessing at a subset would report inputs upstream passes, so the
// gap is recorded and pinned by a fixture rather than approximated. See the fixture named
// "a property chain off a nested function value".
//
// # Where the two implementations differ, and it is one line
//
// oxc defaults `validate_no_impure_functions_in_render` to `false`
// (`environment_config.rs:146`), so oxc's compiler is silent on every input unless the pragma turns
// it on, which is why both corpus fixtures carry `// @validateNoImpureFunctionsInRender`. React's
// ESLint plugin defaults it to `true` (`eslint-plugin-react-hooks.development.js:51788`, inside
// `COMPILER_OPTIONS`), so the rule is live on ordinary source with no pragma at all.
//
// React is the authority and React is what a reader of this codebase runs, so this rule is
// unconditional and reads no pragma. Recorded because it is a real divergence from oxc and the
// differential will show it: any file calling `Date.now()` in a component reports here and does not
// under oxlint. The corpus is unaffected, since its two files set the pragma either way.
var Purity = rule.Rule[CompilerRuleOptions]{
	Name:             "react-hooks/purity",
	NeedsTypeChecker: true,
	// Reads other files only through shape readers (rule.ExportNameIn), so its findings key on imports' shapes.
	TypeReach: rule.TypeReachShapes,
	Run: func(ruleContext rule.Context, options rule.Given[CompilerRuleOptions]) rule.Listeners {
		return rule.Listeners{
			// The whole file at once, for the reason `static_components.go` gives: lowering already
			// descends into nested functions through the function arena, so listening per function
			// kind would lower every inner function twice, the second time without the bindings its
			// parent supplies.
			ast.KindSourceFile: func(node *ast.Node) {
				if ruleContext.TypeChecker == nil {
					return
				}
				// A file that cannot hold a component or a hook is not lowered at all; see
				// high_level_intermediate_representation.MayHoldComponentOrHook for why that is exact.
				if !high_level_intermediate_representation.MayHoldComponentOrHook(ruleContext) {
					return
				}
				forEachCompiledFunction(node, func(functionNode *ast.Node) {
					// Shared with the other rules that lower this same function; see
					// high_level_intermediate_representation.ForFunction.
					lowered := high_level_intermediate_representation.ForFunction(ruleContext, functionNode)
					if lowered == nil {
						return
					}
					purityAnalyzeSubject(ruleContext, lowered)
				})
			},
		}
	},
}

// purityAnalyzeSubject runs the validator over the functions React would have compiled.
//
// A declined function is descended into rather than skipped, because a component nested inside a
// plain wrapper is still a unit. That is the same shape `set_state_in_render.go` uses and it is
// upstream's traversal rather than a convenience.
func purityAnalyzeSubject(ruleContext rule.Context, function *high_level_intermediate_representation.Function) {
	if function == nil {
		return
	}
	if function.Node == nil || !utilsreact.IsComponentOrHookLike(function.Node) {
		for _, nested := range function.Functions {
			purityAnalyzeSubject(ruleContext, nested)
		}
		return
	}
	// A unit found nested in a function that is not one is analyzed as React Compiler compiles it,
	// on its own; see AsCompilationUnit.
	compilationUnit := high_level_intermediate_representation.AsCompilationUnit(ruleContext, function,
		high_level_intermediate_representation.ForFunction)
	purityReportImpureCalls(ruleContext, compilationUnit, true)
}

// purityValueKind is the small abstract domain described on the rule.
type purityValueKind uint8

const (
	// purityValueNothing is every value the pass has no opinion about.
	purityValueNothing purityValueKind = iota
	// purityValueContainer is a global object whose properties are the modelled globals.
	purityValueContainer
	// purityValueImpure is a value that is one of the three impure functions.
	purityValueImpure
	// purityValueCallsImpure is a nested function that performs an impure call when invoked.
	purityValueCallsImpure
)

// purityValue is one entry in the abstract state.
//
// `CanonicalName` is what the finding prints and `Range` is where it points. For an impure function
// the range is unused, because the finding points at the call rather than at the load; for a nested
// function that calls one, the range is the inner call site, which is where React points.
type purityValue struct {
	Kind          purityValueKind
	CanonicalName string
	Node          *ast.Node
	// Function is the nested function this value holds, set only for purityValueCallsImpure.
	Function *high_level_intermediate_representation.Function
}

// purityFindings is what one lowered function does when called, for the caller to re-raise.
type purityFinding struct {
	CanonicalName string
	Node          *ast.Node
}

// purityReportImpureCalls is the forward walk, and it returns what this function does when called.
//
// `emit` separates reporting from detecting. A nested function is analysed to learn whether calling
// it is impure, and that analysis must not report on its own: a `Math.random` inside an uncalled
// arrow is silent upstream, and the arrow is analysed either way. Only the enclosing unit reports,
// and only for a nested function it actually calls.
//
// The return value is the findings this function raises when invoked, which the caller records
// against the value holding it.
//
// State is per unit rather than per function, held in `purityUnit`. A nested function reads a
// binding from its enclosing scope through `LoadGlobal` when the checker resolved it to a
// declaration lowering did not give a local slot, so the wrapper in `const g = () => Math.random();
// const f = () => g(); f();` sees `g` only if the enclosing values are visible to it. Measured on
// React, that input reports, and with per-function state it did not.
func purityReportImpureCalls(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	emit bool,
) []purityFinding {
	unit := &purityUnit{
		ctx:       ruleContext,
		values:    map[static_single_assignment.IdentifierId]purityValue{},
		byName:    map[string]purityValue{},
		nested:    map[*high_level_intermediate_representation.Function][]purityFinding{},
		reported:  map[*ast.Node]bool{},
		analysing: map[*high_level_intermediate_representation.Function]bool{},
	}
	return unit.walk(function, emit)
}

// purityCopy propagates one value's state onto one or more destinations, and reports whether the
// source had any state to propagate.
//
// # The `purityValueNothing` half of the test is equivalent, and it is kept deliberately
//
// A mutation dropping it survived. That is not a fixture gap: the map never holds a
// `purityValueNothing` entry, so presence in the map and having state are the same question.
// Established by enumerating every write into `values` and `byName`: six construction sites, all
// of which name `purityValueContainer`, `purityValueImpure` or `purityValueCallsImpure`, plus five
// propagation sites, all of which copy a value already guarded here or at the phi merge. No path
// constructs the zero value and stores it.
//
// Kept rather than deleted because it is the invariant's only statement in code, and the zero value
// of the enum is `purityValueNothing`, so a future arm that stores a partly-built value would break
// silently without it. Recorded as measured-equivalent rather than as a live guard, so the next
// reader does not go looking for the input that separates them.
//
// Mutates: values gives every destination the source's signature, so later instructions see it.
func purityCopy(
	values map[static_single_assignment.IdentifierId]purityValue,
	from static_single_assignment.IdentifierId,
	to ...static_single_assignment.IdentifierId,
) bool {
	source, ok := values[from]
	if !ok || source.Kind == purityValueNothing {
		return false
	}
	for _, destination := range to {
		values[destination] = source
	}
	return true
}

// purityLoadProperty resolves `container.property` to an impure signature where one exists.
//
// Two shapes reach here and both were measured. `Math.random` off the container is the impure
// function itself. `globalThis.Date` off a transparent container is another container, which is
// what makes `globalThis.Date.now()` report while `window.Date.now()` does not.
//
// Mutates: values records the loaded property's container or impure signature under the instruction's result.
func purityLoadProperty(
	function *high_level_intermediate_representation.Function,
	instruction *high_level_intermediate_representation.Instruction,
	values map[static_single_assignment.IdentifierId]purityValue,
	object high_level_intermediate_representation.Place,
	property string,
) {
	container, ok := values[object.Identifier]
	if !ok || container.Kind != purityValueContainer {
		return
	}
	if container.CanonicalName == "" {
		// A transparent container: the property names the real one, if it is one of ours.
		if _, isImpureContainer := purityImpureBuiltins[property]; isImpureContainer {
			values[instruction.LValue.Identifier] = purityValue{Kind: purityValueContainer, CanonicalName: property}
		}
		return
	}
	if canonical, isImpure := purityImpureBuiltins[container.CanonicalName][property]; isImpure {
		values[instruction.LValue.Identifier] = purityValue{Kind: purityValueImpure, CanonicalName: canonical}
	}
}

// purityDestructure carries the signature through `const {random} = Math`.
//
// Mutates: values records an impure signature for each destructured binding that names one.
func purityDestructure(
	function *high_level_intermediate_representation.Function,
	instruction *high_level_intermediate_representation.Instruction,
	values map[static_single_assignment.IdentifierId]purityValue,
	destructure *high_level_intermediate_representation.Destructure,
) {
	container, ok := values[destructure.Value.Identifier]
	if !ok || container.Kind != purityValueContainer || container.CanonicalName == "" {
		return
	}
	properties := purityImpureBuiltins[container.CanonicalName]
	if properties == nil {
		return
	}
	object, isObject := destructure.Pattern.(*high_level_intermediate_representation.ObjectPattern)
	if !isObject {
		return
	}
	for _, property := range object.Properties {
		// The table lookup is the only guard needed, and the two that used to sit around it were
		// both measured redundant rather than removed on taste.
		//
		// A computed key was declined explicitly first. `ObjectPatternProperty.Key` is documented
		// as empty when `ComputedKey` is set, and the empty string is not a key in any of these
		// maps, so the lookup already declines it. A mutation neutralizing that guard survived and
		// then produced identical output on `const {[props.k]: r} = Math; r();`, which is silent
		// both ways and silent upstream.
		//
		// A nested pattern was declined explicitly too, on the reasoning that
		// `const {random: {call}} = Math` binds something other than the function. Also subsumed:
		// a non-place pattern has no single identifier to write against, and the mutation
		// substituting a zero pattern wrote to identifier 0, which is the reserved invalid value
		// nothing loads. Measured identical on that exact input.
		canonical, isImpure := properties[property.Key]
		if !isImpure {
			continue
		}
		place, isPlace := property.Value.(*high_level_intermediate_representation.PlacePattern)
		if !isPlace {
			continue
		}
		values[place.Place.Identifier] = purityValue{Kind: purityValueImpure, CanonicalName: canonical}
	}
}

// purityMethodCall handles `Math.random()`, where the receiver and the property arrive together.
//
// Lowering keeps a method call whole rather than splitting it into a property load plus a call,
// because the receiver is the `this` of the call. So this arm is the direct form and it is the one
// both corpus fixtures exercise. The property is a `Primitive` holding the name, which is how a
// computed `Math['random']()` reaches the same place: measured on React, that input reports.
func purityMethodCall(
	function *high_level_intermediate_representation.Function,
	instruction *high_level_intermediate_representation.Instruction,
	values map[static_single_assignment.IdentifierId]purityValue,
	value *high_level_intermediate_representation.MethodCall,
	report func(purityFinding),
) {
	container, ok := values[value.Receiver.Identifier]
	if !ok || container.Kind != purityValueContainer || container.CanonicalName == "" {
		return
	}
	property, named := purityPropertyName(function, value.Property)
	if !named {
		return
	}
	if canonical, isImpure := purityImpureBuiltins[container.CanonicalName][property]; isImpure {
		report(purityFinding{CanonicalName: canonical, Node: instruction.Node})
	}
}

// purityCalleeName reads a method call's property name, for the hook test.
func purityCalleeName(
	function *high_level_intermediate_representation.Function,
	property high_level_intermediate_representation.Place,
) string {
	name, _ := purityPropertyName(function, property)
	return name
}

// purityCalleeIdentifierName reads a plain call's callee name from the value that defined it.
//
// A hook is normally called through a bare identifier, which lowering gives a `LoadGlobal` carrying
// the name whether it is imported or global, so this reads that instruction rather than asking the
// checker.
func purityCalleeIdentifierName(
	function *high_level_intermediate_representation.Function,
	callee high_level_intermediate_representation.Place,
) string {
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil || instruction.LValue.Identifier != callee.Identifier {
				continue
			}
			if global, isGlobal := instruction.Value.(*high_level_intermediate_representation.LoadGlobal); isGlobal {
				return global.Name
			}
			return ""
		}
	}
	return ""
}

// purityPropertyName reads the static name out of a method call's property place.
//
// The property of a `MethodCall` is a place holding a `Primitive` whose value is the name, so the
// instruction that defined it is what has to be read. A dotted access lowers the name directly
// (`lower_expression.go:218`); an element access lowers the argument expression, so `Math[k]()`
// arrives here as whatever `k` lowered to. Declining a non-`Primitive` is what makes a genuinely
// dynamic key silent, which is measured: React passes `Math[props.k]()`.
//
// # There is no separate string test, and that is measured rather than an oversight
//
// The obvious form returns the ok flag of the `PrimitiveString` assertion as well. A mutation
// ignoring that flag survived, and it is subsumed rather than a fixture gap: a failed assertion
// yields the empty string, the empty string is not a key in `purityImpureBuiltins` or in any of
// its inner maps, and the only consumers of this name are those two lookups. So a numeric or
// boolean key reaches silence either way. Confirmed on `(Math as any)[0]()` and `(Math as any)[true]()`, both silent
// with the flag and without it.
func purityPropertyName(
	function *high_level_intermediate_representation.Function,
	property high_level_intermediate_representation.Place,
) (string, bool) {
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil || instruction.LValue.Identifier != property.Identifier {
				continue
			}
			primitive, isPrimitive := instruction.Value.(*high_level_intermediate_representation.Primitive)
			if !isPrimitive {
				return "", false
			}
			name, _ := primitive.Value.(high_level_intermediate_representation.PrimitiveString)
			return string(name), true
		}
	}
	return "", false
}
