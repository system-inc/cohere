package react

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/static_single_assignment"
)

// Refs flags reading or writing a ref's `current` property during render.
//
//	valid:   function C() { const r = useRef(null); useEffect(() => { r.current; }, []); return <div ref={r} />; }
//	valid:   function C() { const r = useRef(null); return <div onClick={() => r.current} />; }
//	valid:   function C() { const r = useRef(null); if (r.current == null) { r.current = 1; } }
//	invalid: function C() { const r = useRef(null); const v = r.current; return v; }
//	invalid: function C() { const r = useRef(null); r.current = 1; return null; }
//	invalid: function C() { const r = useRef(null); const x = foo(r); return x; }
//	invalid: function C(props) { const v = props.ref.current; return <div>{v}</div>; }
//
// A ref is deliberately outside React's data flow: writing one does not schedule a render, and
// during render the ref may not yet point at the thing it will point at once the tree commits.
// Reading `current` while rendering therefore produces a value that is either stale or not yet
// attached, and because the read creates no subscription, nothing re-renders when it changes. The
// symptom is an interface that silently shows the previous frame's data and never corrects itself,
// which reads as a caching bug rather than as what it is.
//
// # Where this comes from
//
// React's own `ValidateNoRefAccessInRender.ts`, transcribed by oxc at
// `oxc_react_compiler/src/react_compiler_validation/validate_no_ref_access_in_render.rs`, which is
// the version this follows at 1,060 lines. oxc's *linter* rule at `rules/react/refs.rs` is a
// four-line dispatcher into `run_react_compiler_rule(ctx, ErrorCategory::Refs)`, so reading only
// the rule file makes a large port look trivial. The judgment lives entirely in the compiler crate.
//
// # This rule reads no effect facts, which is why it could be written now
//
// It was widely believed that this rule waited on the mutation-effect table that
// `immutability` needs. It does not.
// `grep -cE 'impure|no_alias|callee_effect|known_incompatible|positional_params|return_value_kind'`
// over all 1,060 lines of the validator returns 0, measured with a live control in the same command
// (`RefAccessType` returns 100 on the same file, so the zero is a real absence rather than a bad
// pattern). What the rule actually consults is its own six-element lattice, two nominal type
// predicates, and the phi/load/store propagation this representation already provides.
//
// # The lattice, and the equality that is not identity
//
// Six elements: none, nullable, guard, ref, refValue, structure. The important part is not the
// elements but the equality relation over them, and it is the single subtlety most worth carrying
// forward. Upstream hand-writes `PartialEq` rather than deriving it, and its doc comment says why:
// a join mints a fresh ref id, so an equality that compared ids would report "changed" on every
// iteration and **the fixpoint would never converge**. `refsTypeEqual` below reproduces that
// deliberately: `ref` ignores its id entirely, and `refValue` compares only its access span.
//
// Equality for convergence is not equality for identity. Two values that are genuinely distinct
// refs compare equal here, and that is correct, because what the fixpoint needs to know is whether
// the lattice element moved, not whether the underlying ref is the same object. Deriving this
// comparison is the natural thing to do in Go and it produces a rule that either loops ten times on
// every function or emits a spurious non-convergence finding. `TestRefsConvergenceEqualityIgnores
// RefIdentity` pins it, and task `#g29j9ab` tracks lifting this traversal onto the shelf when a
// third rule needs it; if that happens, this hazard is the thing to carry with it.
//
// # Why this is a bounded sweep rather than a dataflow solve
//
// Upstream is `for iteration in 0..10` over the whole function, with one `changed` flag on a
// per-identifier map, and it emits a non-convergence diagnostic if still moving at ten. That is
// chaotic iteration keyed by identifier, not a per-block entry/exit lattice, so neither
// `control_flow_graph.Solve` nor a dominator tree is the right instrument even in principle.
//
// It is also worth recording that `control_flow_graph.Solve[V,E]` is mechanically unreachable from this
// representation regardless of fit: `control_flow_graph.Block`'s `index`, `final` and `thrown` fields are
// unexported and set only by `control_flow_graph.Build`, so a caller holding a
// `high_level_intermediate_representation.Function` cannot construct one. `ssa.go` and `postdominator.go` both already
// record this in their headers. Three rules shipped on this representation have now read that machinery and
// deliberately used none of it, so the bar for reaching for it is high and this rule does not clear it.
//
// The bound is reproduced exactly, ten rounds, including the non-convergence finding. On the real
// tree the sweep settled in two rounds on every function it saw; see the test file.
//
// # Two signals identify a ref, and both are required
//
// This is the part a reader is most likely to get wrong, because either signal alone looks
// sufficient and each covers a different part of the corpus.
//
//   - **The checker.** `useRef` returns `RefObject<T>`, and a ref is identified by that type's
//     symbol. `RefObject` is an interface, so its type alias is correctly nil. This is the exact
//     mirror image of `set_state_in_render.go`'s setter predicate, which keys on the alias because
//     `Dispatch` is a type alias whose symbol renders as a non-printable internal marker. A
//     predicate written for one of them silently reports nothing for the other, and an agent
//     writing this rule keyed on the alias first and reported a genuine `useRef` as neither.
//     Probed on our own checker rather than assumed: `const myRef = useRef(null)` gives
//     `alias=<nil> symbol=RefObject`, while `setX` gives `alias=Dispatch symbol=<binary noise>`.
//
//   - **The name.** Upstream's `enableTreatRefLikeIdentifiersAsRefs` defaults to `true`
//     (`environment_config.rs:157`) and matches `/^(?:[a-zA-Z$_][\w$]*)Ref$|^ref$/` against the
//     name of the binding whose `current` is being accessed. Only 1 of the 42 corpus fixtures sets
//     the flag explicitly, so it is silently active across the whole corpus. This is what reaches
//     `props.ref`, a ref arriving as an untyped prop, where there is no `useRef` call for the
//     checker to type.
//
// Neither signal alone covers the corpus and shipping only the first would decline six fixtures
// silently. See the scope note below.
//
// # What the name signal matches, measured against the executable rather than read
//
// The regex reads as though it matches any `somethingRef.current` anywhere. It does not, and the
// difference is the object's binding name rather than the text of the member expression. Driven on
// `eslint-plugin-react-hooks` 7.1.1 through the ESLint Linter API on each input:
//
//	const fooRef = {};      fooRef.current        REPORTS   binding named fooRef
//	function C({fooRef})    fooRef.current        REPORTS   binding named fooRef
//	function C(props)       props.fooRef.current  CLEAN     binding is props, not fooRef
//	function C(props)       props.ref.current     REPORTS   the intermediate load is named ref
//	const q = props.fooRef; q.current             CLEAN     binding named q
//	const ref = props.any;  ref.current           REPORTS   binding named ref
//	const o = props.o;      o.ref.current         CLEAN     binding named o
//
// The apparent inconsistency between rows 3 and 4 is not a special case for `props`. Upstream types
// a property load as `Type::Property{object_name, property_name}` where `object_name` is the name
// of the object's own binding, and `is_ref_like_name` is asked at each unification. For
// `props.ref.current` the inner load produces a value whose binding name is `ref`, and the outer
// `.current` then matches against that. For `props.fooRef.current` the inner load is also named
// `fooRef`, so by the regex it should match too, and upstream is nonetheless silent. Reading
// alone would have produced the wrong rule here in both directions; every row above is a
// measurement, and rows 3 and 5 are reproduced as silence deliberately.
//
// This rule keys the name test on `high_level_intermediate_representation.Identifier.Name`, which is the binding name
// for a named value and empty for a temporary, giving the same partition without re-deriving it.
//
// # `Ref` alone does not match, and that is upstream's text rather than an oversight
//
// The regex requires at least one character before `Ref`, so `fooRef` and `aRef` match while a bare
// `Ref` does not. Upstream states this in a comment at `infer_types.rs:235` and the executable
// agrees (`props.Ref.current` is clean, measured). Reproduced rather than tidied, because a rule
// that helpfully matched `Ref` would disagree with React on real code while looking more correct.
//
// # The component gate is not used here, and that is a deliberate difference from its neighbours
//
// `static_components.go`, `globals.go` and `set_state_in_render.go` all gate on the shelf's
// `IsComponentOrHookLike`, and three of them reuse the predicate whole. This rule does not, and the
// reason is that upstream's own gate for this validator is different. Measured on the executable: a
// lowercase `function helper(props) { return props.ref.current; }` is clean, so a gate is real; but
// the gate that decides it is compilation admission, which for this validator already excludes a
// non-component. What this rule needs beyond that is the `!is_function_expression` condition at
// `infer_mutation_aliasing_ranges.rs:470`, which is "this is the outer function under compilation,
// not a nested function expression" and is a different question from "is this a component".
//
// So the compilation gate is reused (a function must look like a component or a hook and must
// actually create JSX or call a hook), and nested functions are analysed as inner functions through
// the structure element rather than as subjects in their own right. A nested function that touches
// a ref does not report at its own site; it makes its enclosing binding carry a `readRefEffect`, and
// the finding lands where that function is called. That is what makes `capture-ref-for-mutation`
// report at `handleKey('left')()` rather than inside `handleKey`.
//
// # One span where upstream has two, and one message shape
//
// Upstream's `ref_update` diagnostic carries a primary span and a secondary label pointing at the
// ref's origin. Our `Diagnostic` carries a single `Range`, so the primary span is kept where
// upstream puts it and the origin is dropped rather than moved into the message, because unlike
// `static_components` the origin here is usually the same identifier the span already names.
var Refs = rule.Rule{
	Name:             "react-hooks/refs",
	NeedsTypeChecker: true,
	// Reads other files only through shape readers (rule.ExportNameIn), so its findings key on imports' shapes.
	TypeReach: rule.TypeReachShapes,
	Run: func(ruleContext rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			// Taken at the file level for the reason static_components.go gives: lowering already
			// descends into nested functions through the function arena, so listening per function
			// kind would lower every inner function twice, once as a child and once standalone
			// with no enclosing context.
			ast.KindSourceFile: func(node *ast.Node) {
				if ruleContext.TypeChecker == nil {
					return
				}
				// A file that cannot hold a component or a hook is not lowered at all; see
				// high_level_intermediate_representation.MayHoldComponentOrHook for why that is exact.
				if !high_level_intermediate_representation.MayHoldComponentOrHook(ruleContext) {
					return
				}
				refsForEachCompiledFunction(node, func(functionNode *ast.Node) {
					lowered := high_level_intermediate_representation.ForFunctionWithoutManualMemoization(
						ruleContext,
						functionNode,
					)
					if lowered == nil {
						return
					}
					refsAnalyzeCompilationUnit(ruleContext, lowered)
				})
			},
		}
	},
}

// refsForEachCompiledFunction visits the outermost function-like node on each branch.
//
// Identical in shape to static_components.go's walker and deliberately not shared: two porters
// collided on unprefixed helper names in this package today and both then removed their copies at
// once, turning a redeclaration error into an undefined-symbol error pointing at nobody. Every
// helper here carries the rule's name. Lifting the shared shape onto the shelf is task `#g29j9ab`.
func refsForEachCompiledFunction(root *ast.Node, visit func(*ast.Node)) {
	if root == nil {
		return
	}
	root.ForEachChild(func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) {
			visit(node)
			return false
		}
		refsForEachCompiledFunction(node, visit)
		return false
	})
}

// refsAnalyzeCompilationUnit runs the sweep over the functions upstream would have compiled.
//
// The gate is upstream's `getReactFunctionType`: a function is admitted only when its name looks
// like a component or a hook and its body actually creates JSX or calls a hook. A function that is
// not a unit is walked past into the functions it contains, because one of those may be a unit even
// when its wrapper is not.
func refsAnalyzeCompilationUnit(ruleContext rule.Context, function *high_level_intermediate_representation.Function) {
	if function == nil {
		return
	}
	if !refsIsCompilationUnit(function) {
		for _, nested := range function.Functions {
			refsAnalyzeCompilationUnit(ruleContext, nested)
		}
		return
	}
	// A unit found nested in a function that is not one is analyzed as React Compiler compiles it,
	// on its own; see AsCompilationUnit.
	compilationUnit := high_level_intermediate_representation.AsCompilationUnit(ruleContext, function,
		high_level_intermediate_representation.ForFunctionWithoutManualMemoization)
	findings := refsSweepFunction(ruleContext, compilationUnit)
	for _, finding := range findings {
		refsReport(ruleContext, compilationUnit, finding)
	}
}

// refsIsCompilationUnit reports whether upstream would compile this function.
//
// Both halves of `getReactFunctionType` are required and the name alone is not enough. The name
// half is taken from `high_level_intermediate_representation.Function.Kind` rather than recomputed, so this rule and
// the representation cannot drift about what a component name is. The body half asks whether the
// function creates JSX or calls a hook.
//
// Unlike static_components.go, the hook half is asked here and is reachable. That rule's finding
// requires a JSX tag, so every reportable input contains JSX and the hook branch cannot change its
// answer. This rule reports on a bare `useRef` read with no JSX anywhere, so a hook-calling
// component with no JSX is a real subject: `function useHook({value}) { const ref = useRef(null);
// ref.current = value; return ref; }` is the shipped fixture
// `error.invalid-write-but-dont-read-ref-in-render` and it contains no JSX at all.
func refsIsCompilationUnit(function *high_level_intermediate_representation.Function) bool {
	if function == nil || function.Kind == high_level_intermediate_representation.FunctionKindOther {
		return false
	}
	return refsCreatesJsxOrCallsHook(function)
}

// refsCreatesJsxOrCallsHook asks upstream's body test, without descending into nested functions.
//
// The non-descent is the point rather than an optimization: a component whose body is only a
// callback returning JSX is not itself a component to upstream.
func refsCreatesJsxOrCallsHook(function *high_level_intermediate_representation.Function) bool {
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := refsInstructionAt(function, instructionId)
			if instruction == nil {
				continue
			}
			switch value := instruction.Value.(type) {
			case *high_level_intermediate_representation.JsxExpression,
				*high_level_intermediate_representation.JsxFragment:
				return true
			case *high_level_intermediate_representation.LoadGlobal:
				if refsIsHookName(value.Name) {
					return true
				}

			// `React.useRef(...)` is the same call written through the namespace, and it lowers to
			// a `MethodCall` whose property is a primitive string rather than to a global of its
			// own. Admitting only `LoadGlobal` meant a file that imports React as a default and
			// spells every hook `React.useHook` was never compiled at all, so nothing in it was
			// checked by this rule.
			//
			// Found on a real file rather than by a fixture: `useKingdomLive.tsx` reads a ref
			// during render, ESLint reported it, and this rule was silent because every hook call
			// in that file is spelled through the namespace. `unsupported_syntax.go`'s
			// `isCompilerHookCallee` already accepts both spellings, which is what makes this a gap
			// rather than a decision.
			case *high_level_intermediate_representation.MethodCall:
				if refsIsHookName(refsPrimitiveStringAt(function, value.Property)) {
					return true
				}
			}
		}
	}
	return false
}

// refsPrimitiveStringAt returns the string a place holds, when it holds a primitive string.
//
// A namespaced call lowers its property to a `Primitive` rather than keeping it as syntax, so
// `React.useRef` arrives as a `MethodCall` whose `Property` place is defined by `Primitive{useRef}`.
// Empty for anything else, which reads as "not a hook name" at the one call site.
func refsPrimitiveStringAt(
	function *high_level_intermediate_representation.Function,
	place high_level_intermediate_representation.Place,
) string {
	for _, instruction := range function.Instructions {
		if instruction == nil || instruction.LValue.Identifier != place.Identifier {
			continue
		}
		if primitive, isPrimitive :=
			instruction.Value.(*high_level_intermediate_representation.Primitive); isPrimitive {
			if text, isText := primitive.Value.(string); isText {
				return text
			}
		}
		return ""
	}
	return ""
}

// refsIsHookName reports whether a name is a hook by React's own syntactic test.
//
// `use` followed by an uppercase letter, matching `isHookName` upstream. A bare `use` is a hook
// too under React's rules, but it cannot appear as a `LoadGlobal` callee in any corpus fixture and
// is not admitted here, which is stated rather than left as an accident.
func refsIsHookName(name string) bool {
	if !strings.HasPrefix(name, "use") || len(name) < 4 {
		return false
	}
	initial := name[3]
	return initial >= 'A' && initial <= 'Z'
}

// refsInstructionAt returns one instruction by id, or nil when the id is out of range.
func refsInstructionAt(
	function *high_level_intermediate_representation.Function,
	id high_level_intermediate_representation.InstructionId,
) *high_level_intermediate_representation.Instruction {
	if function == nil || int(id) >= len(function.Instructions) {
		return nil
	}
	return function.Instructions[id]
}

// refsIdentifierNode returns the syntax a value came from, or nil for a pure temporary.
//
// This is the seam `high_level_intermediate_representation.Identifier.Node` documents itself as existing for: the node
// goes to the checker rather than to a local inference pass. Reporting through it rather than through a Place's
// range matters because a Place's range is the range of the node that produced the instruction.
func refsIdentifierNode(
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
) *ast.Node {
	if function == nil || int(id) >= len(function.Identifiers) {
		return nil
	}
	identifier := function.Identifiers[id]
	if identifier == nil {
		return nil
	}
	return identifier.Node
}

// refsIdentifierName returns a value's source binding name, empty for a temporary.
func refsIdentifierName(
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
) string {
	if function == nil || int(id) >= len(function.Identifiers) {
		return ""
	}
	identifier := function.Identifiers[id]
	if identifier == nil {
		return ""
	}
	return identifier.Name
}

// refsIsRefLikeName is upstream's `is_ref_like_name`, minus the property half.
//
// The TypeScript regex is `/^(?:[a-zA-Z$_][\w$]*)Ref$|^ref$/`. Note that `Ref` alone does not
// match: the alternation requires at least one character before `Ref`. Upstream states this at
// `infer_types.rs:235` and the executable agrees, measured on `props.Ref.current`, which is clean.
func refsIsRefLikeName(name string) bool {
	if name == "ref" {
		return true
	}
	if len(name) <= 3 || !strings.HasSuffix(name, "Ref") {
		return false
	}
	initial := name[0]
	isAlphabetic := (initial >= 'a' && initial <= 'z') || (initial >= 'A' && initial <= 'Z')
	return isAlphabetic || initial == '$' || initial == '_'
}

// refsIsUseRefType asks the checker whether a value is a `RefObject`.
//
// Keyed on the type's symbol, not its alias. `RefObject` is an interface, so the checker records it
// on the type's symbol and the alias is correctly nil. `set_state_in_render.go`'s setter predicate
// is the exact mirror and reads the alias instead, because `Dispatch` is a type alias whose symbol
// renders as a non-printable internal marker. A predicate written for one silently answers false
// for the other; this one was probed on our own checker before being written, not inferred from the
// shape of its neighbour.
//
// The symbol is compared positively rather than checked for non-nil. The shim is a hand-mirrored
// struct read through `unsafe.Pointer` and has returned a silently wrong type before, so asserting
// that what came back is what was asked for is the only check that can see that class of failure.
func refsIsUseRefType(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
) bool {
	if ruleContext.TypeChecker == nil {
		return false
	}
	node := refsIdentifierNode(function, id)
	if node == nil || refsCannotHaveRefType(node) {
		return false
	}
	return refsNodeHasRefType(ruleContext, node)
}

// refsCannotHaveRefType reports whether the language fixes a node's type as something no ref type
// can be, so the checker need not be asked.
//
// The first ask about a node is where the checker computes its type, and for a JSX element that
// means resolving the component and checking every attribute against its props. Profiled on ahra,
// www and connected, 2.1 million asks came from this rule, and only identifiers, calls, property
// accesses and variable declarations ever answered yes. The kinds below answer no by construction,
// not by measurement: a literal, a template, a JSX node, an arithmetic, comparison, bitwise or unary
// result, an object or array literal, and an anonymous function, whose type symbol is the
// anonymous `__function`. `TestRefsCannotHaveRefTypeAgreesWithTheChecker` asks the checker about
// every node this declines in a source written to tempt it.
//
// Deliberately absent: `&&`, `||`, `??`, the comma and the assignments `=`, `&&=`, `||=` and `??=`,
// whose result is an operand and can be a ref; a conditional, an `await`, a `new`, an `as`; and a
// named function, because its type's symbol carries the function's name, so `function RefCallback`
// would answer yes to a test that reads the symbol's name.
func refsCannotHaveRefType(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression,
		ast.KindRegularExpressionLiteral, ast.KindNullKeyword, ast.KindTrueKeyword,
		ast.KindFalseKeyword,
		ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment, ast.KindJsxText,
		ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression, ast.KindArrowFunction,
		ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression, ast.KindTypeOfExpression,
		ast.KindVoidExpression, ast.KindDeleteExpression:
		return true
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().Name() == nil
	case ast.KindBinaryExpression:
		return refsOperatorYieldsAPrimitive(node.AsBinaryExpression().OperatorToken.Kind)
	}
	return false
}

// refsOperatorYieldsAPrimitive reports whether a binary operator's result is a number, a bigint, a
// string or a boolean whatever its operands are.
func refsOperatorYieldsAPrimitive(operator ast.Kind) bool {
	switch operator {
	case ast.KindPlusToken, ast.KindMinusToken, ast.KindAsteriskToken, ast.KindSlashToken,
		ast.KindPercentToken, ast.KindAsteriskAsteriskToken,
		ast.KindAmpersandToken, ast.KindBarToken, ast.KindCaretToken,
		ast.KindLessThanLessThanToken, ast.KindGreaterThanGreaterThanToken,
		ast.KindGreaterThanGreaterThanGreaterThanToken,
		ast.KindLessThanToken, ast.KindGreaterThanToken, ast.KindLessThanEqualsToken,
		ast.KindGreaterThanEqualsToken, ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken,
		ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken,
		ast.KindInstanceOfKeyword, ast.KindInKeyword:
		return true
	}
	return false
}

// refsNodeHasRefType is refsIsUseRefType's checker half, over a node.
func refsNodeHasRefType(ruleContext rule.Context, node *ast.Node) bool {
	valueType := ruleContext.TypeChecker.GetTypeAtLocation(node)
	if valueType == nil {
		return false
	}
	// The alias is checked as well as the symbol, and the two carry different names.
	//
	// `RefObject` is an interface, so it lands on the type's symbol and its alias is nil.
	// `RefCallback` is a type alias, so it lands on the alias and its symbol is an anonymous
	// function type. A predicate reading only one field answers false for the other, which is the
	// same asymmetry `set_state_in_render.go` documents from the opposite side, and
	// `internal/utilities/typecheck/specifier.go:39` already does this alias-then-symbol fallback for
	// exactly this reason. Probed on our own checker rather than inferred.
	if alias := shimchecker.Type_alias(valueType); alias != nil {
		if aliasSymbol := alias.Symbol(); aliasSymbol != nil && aliasSymbol.Name == "RefCallback" {
			return true
		}
	}
	symbol := shimchecker.Type_symbol(valueType)
	if symbol == nil {
		return false
	}
	// `RefCallback` is included for the mergeRefs pattern, and it is upstream's exemption rather
	// than a convenience. A function whose result is a ref legitimately receives refs: that is what
	// merging two refs into one callback means. Upstream expresses this as "the lvalue is a ref"
	// at the call site (`is_ref_lvalue`, the mergeRefs comment at `:697`), and the type our own
	// `mergeReferences` returns is `React.RefCallback<T>`.
	//
	// Measured: `const set = mergeReferences([inner, ref])` is clean upstream and was 8 findings on
	// Kirk's tree, one in every form-field component that forwards a ref. The corpus cannot see this
	// because no fixture merges refs.
	return symbol.Name == "RefObject" || symbol.Name == "MutableRefObject" || symbol.Name == "RefCallback"
}

// ---------------------------------------------------------------------------
// The lattice
// ---------------------------------------------------------------------------

// refsAccessKind is which of the six lattice elements a value holds.
type refsAccessKind uint8

const (
	// refsNone is the bottom element: nothing known, and the join identity.
	refsNone refsAccessKind = iota
	// refsNullable is a null or undefined literal, which participates in guard recognition.
	refsNullable
	// refsGuard is the result of comparing a ref value against null, as in `if (r.current == null)`.
	refsGuard
	// refsRef is the ref object itself, the thing `useRef` returns.
	refsRef
	// refsRefValue is what came out of a ref, the `current` property. Reading one during render is
	// the finding this rule is named for.
	refsRefValue
	// refsStructure is an object, array or function that carries a ref somewhere inside it.
	refsStructure
)

// refsAccessType is one lattice element.
//
// Flattened into a single struct with a kind tag rather than an interface hierarchy, because unlike
// `high_level_intermediate_representation.InstructionValue` this set is small, closed to this file, and every consumer
// switches on the kind anyway. The fields that are meaningful depend on Kind and the equality below is what keeps
// that honest.
type refsAccessType struct {
	Kind refsAccessKind

	// RefId identifies which ref this came from. Meaningful for refsGuard, refsRef and refsRefValue.
	//
	// A join mints a fresh one, which is the whole reason `refsTypeEqual` must ignore it for
	// refsRef and refsRefValue. See the rule comment.
	RefId int
	// HasRefId separates "no ref id" from ref id zero, which a bare int cannot express.
	HasRefId bool

	// Span is where the ref value was accessed, carried so a finding can point at the access rather
	// than at the operand that happened to reach the check.
	Span       static_single_assignment.IdentifierId
	HasSpan    bool
	RefSpan    static_single_assignment.IdentifierId
	HasRefSpan bool

	// Value is what a structure carries inside it, nil when it carries nothing.
	Value *refsAccessType

	// Function is set on a structure that is a function, recording whether calling it reads a ref.
	Function *refsFunctionType
}

// refsFunctionType records what calling a function value would do to a ref.
type refsFunctionType struct {
	// ReadRefEffect is true when analysing the function's body produced a ref finding, so calling it
	// during render commits that finding at the call site rather than inside the function.
	ReadRefEffect bool
	// RefAccessSpan is where inside the function the ref was touched.
	RefAccessSpan    static_single_assignment.IdentifierId
	HasRefAccessSpan bool
	// ReturnType is what the function yields.
	ReturnType *refsAccessType
}

// refsTypeEqual is equality for convergence, and it is deliberately not equality for identity.
//
// This reproduces upstream's hand-written `PartialEq`, and the reason it is hand-written rather
// than derived is the single most load-bearing detail in this rule. `refsJoin` mints a fresh ref id
// whenever it merges two different refs. A derived comparison would see that fresh id, report the
// environment as changed, and do so again on the next round: the fixpoint would never converge, and
// every function carrying a joined ref would burn all ten rounds and then emit a spurious
// non-convergence finding.
//
// So: refsRef ignores its ref id entirely, and refsRefValue compares only its access span. Two
// genuinely distinct refs therefore compare equal here. That is correct, because the question the
// sweep asks is "did this lattice element move", not "is this the same ref object".
//
// refsGuard is the exception and does compare its id, because a guard is only sound for the
// specific ref it guards; treating two guards as interchangeable would let `if (a.current == null)`
// authorize a write to `b.current`.
func refsTypeEqual(left, right *refsAccessType) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case refsNone, refsNullable:
		return true
	case refsGuard:
		return left.RefId == right.RefId && left.HasRefId == right.HasRefId
	case refsRef:
		// Ref identity deliberately ignored. See the doc comment.
		return true
	case refsRefValue:
		// Only the access span, deliberately. Ref origin and ref id ignored.
		return left.Span == right.Span && left.HasSpan == right.HasSpan
	case refsStructure:
		return refsTypeEqual(left.Value, right.Value) && refsFunctionEqual(left.Function, right.Function)
	}
	return false
}

// refsFunctionEqual compares two function types for convergence.
func refsFunctionEqual(left, right *refsFunctionType) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ReadRefEffect == right.ReadRefEffect && refsTypeEqual(left.ReturnType, right.ReturnType)
}

// refsJoin merges two lattice elements, reproducing upstream's `join_ref_access_types`.
//
// The ordering of the cases is upstream's and is load-bearing: refsNone is the identity, a guard
// meeting a nullable collapses to refsNone (the guard is no longer sound once a non-guarded path
// reaches the same value), and two different guards also collapse. Everything that survives to the
// bottom is a ref-carrying element, where a mismatch mints a fresh ref id. That mint is exactly the
// thing `refsTypeEqual` must not look at.
func refsJoin(a, b *refsAccessType, nextRefId func() int) *refsAccessType {
	left := a
	if left == nil {
		left = &refsAccessType{Kind: refsNone}
	}
	right := b
	if right == nil {
		right = &refsAccessType{Kind: refsNone}
	}
	if left.Kind == refsNone {
		return right
	}
	if right.Kind == refsNone {
		return left
	}
	if left.Kind == refsGuard && right.Kind == refsGuard {
		if left.RefId == right.RefId && left.HasRefId == right.HasRefId {
			return left
		}
		return &refsAccessType{Kind: refsNone}
	}
	// A guard meeting a nullable is no longer a guard: one path proved the ref null and the other
	// did not, so nothing downstream may rely on it.
	if (left.Kind == refsGuard && right.Kind == refsNullable) ||
		(left.Kind == refsNullable && right.Kind == refsGuard) {
		return &refsAccessType{Kind: refsNone}
	}
	if left.Kind == refsGuard {
		return right
	}
	if right.Kind == refsGuard {
		return left
	}
	if left.Kind == refsNullable {
		return right
	}
	if right.Kind == refsNullable {
		return left
	}
	return refsJoinRefCarrying(left, right, nextRefId)
}

// refsJoinRefCarrying merges two elements that both carry a ref, upstream's
// `join_ref_access_ref_types`.
//
// A ref value joined with anything is a ref value with its provenance erased, because the result
// may or may not have come out of a ref and the conservative answer is that it did. Two refs that
// are not the same ref produce a fresh id, which is the mint the convergence equality ignores.
func refsJoinRefCarrying(left, right *refsAccessType, nextRefId func() int) *refsAccessType {
	if left.Kind == refsRefValue && right.Kind == refsRefValue {
		if left.HasRefId && right.HasRefId && left.RefId == right.RefId {
			return left
		}
		return &refsAccessType{Kind: refsRefValue}
	}
	if left.Kind == refsRefValue || right.Kind == refsRefValue {
		return &refsAccessType{Kind: refsRefValue}
	}
	if left.Kind == refsRef && right.Kind == refsRef {
		if left.HasRefId && right.HasRefId && left.RefId == right.RefId {
			return left
		}
		return &refsAccessType{Kind: refsRef, RefId: nextRefId(), HasRefId: true}
	}
	if left.Kind == refsRef || right.Kind == refsRef {
		return &refsAccessType{Kind: refsRef, RefId: nextRefId(), HasRefId: true}
	}
	// Both structures.
	joined := &refsAccessType{Kind: refsStructure}
	switch {
	case left.Function == nil:
		joined.Function = right.Function
	case right.Function == nil:
		joined.Function = left.Function
	default:
		returnType := refsJoin(left.Function.ReturnType, right.Function.ReturnType, nextRefId)
		merged := &refsFunctionType{
			ReadRefEffect: left.Function.ReadRefEffect || right.Function.ReadRefEffect,
			ReturnType:    returnType,
		}
		if left.Function.HasRefAccessSpan {
			merged.RefAccessSpan, merged.HasRefAccessSpan = left.Function.RefAccessSpan, true
		} else if right.Function.HasRefAccessSpan {
			merged.RefAccessSpan, merged.HasRefAccessSpan = right.Function.RefAccessSpan, true
		}
		joined.Function = merged
	}
	switch {
	case left.Value == nil:
		joined.Value = right.Value
	case right.Value == nil:
		joined.Value = left.Value
	default:
		joined.Value = refsJoinRefCarrying(left.Value, right.Value, nextRefId)
	}
	return joined
}

// refsJoinMany folds a slice, starting from the identity.
func refsJoinMany(types []*refsAccessType, nextRefId func() int) *refsAccessType {
	result := &refsAccessType{Kind: refsNone}
	for _, one := range types {
		result = refsJoin(result, one, nextRefId)
	}
	return result
}

// refsDestructure unwraps a structure down to what it carries, upstream's `destructure`.
//
// A structure holding a ref answers the validation checks as though it were that ref, which is what
// makes `const o = {r}; foo(o)` report where `foo(r)` reports.
func refsDestructure(accessType *refsAccessType) *refsAccessType {
	current := accessType
	for current != nil && current.Kind == refsStructure && current.Value != nil {
		current = current.Value
	}
	if current == nil {
		return &refsAccessType{Kind: refsNone}
	}
	return current
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

// The four message ids, matching upstream's four distinct diagnostics for this category. They share
// one headline ("Cannot access refs during render") upstream and differ in the label under the span,
// which is the text reproduced in each Description's first sentence.
const (
	messageRefsValueAccessId         = "refValueAccess"
	messageRefsPassedToFunctionId    = "refPassedToFunction"
	messageRefsUpdateId              = "refUpdate"
	messageRefsFunctionAccessesRefId = "functionAccessesRef"
	messageRefsDidNotConvergeId      = "refTypeEnvironmentDidNotConverge"
)

// refsWhyRefsAreOutsideRender is the shared explanation, held once so a single edit moves every
// finding and the four Descriptions cannot drift apart.
const refsWhyRefsAreOutsideRender = "A ref is deliberately outside React's data flow: writing one " +
	"does not schedule a render, and during render the ref may not yet point at the element it will " +
	"point at once the tree commits. Reading it here therefore gives a value that is stale or not " +
	"yet attached, and because the read creates no subscription nothing re-renders when it changes. " +
	"Move the access into an event handler or an effect, where the ref is attached and a stale read " +
	"cannot be observed."

// refsMessageFor renders the finding.
func refsMessageFor(kind refsFindingKind) rule.Message {
	switch kind {
	case refsFindingPassedToFunction:
		return rule.Message{
			Id: messageRefsPassedToFunctionId,
			Description: "Passing a ref to a function may read its value during render. " +
				refsWhyRefsAreOutsideRender,
		}
	case refsFindingUpdate:
		return rule.Message{
			Id:          messageRefsUpdateId,
			Description: "Cannot update ref during render. " + refsWhyRefsAreOutsideRender,
		}
	case refsFindingFunctionAccessesRef:
		return rule.Message{
			Id: messageRefsFunctionAccessesRefId,
			Description: "This function accesses a ref value, and calling it during render reads " +
				"the ref. " + refsWhyRefsAreOutsideRender,
		}
	case refsFindingDidNotConverge:
		return rule.Message{
			Id: messageRefsDidNotConvergeId,
			Description: "The ref analysis did not settle after ten passes over this function, so " +
				"its result is not trustworthy and no ref finding is reported for it. This is an " +
				"internal invariant rather than a problem with the code, and it reproduces " +
				"upstream's own bound.",
		}
	default:
		return rule.Message{
			Id:          messageRefsValueAccessId,
			Description: "Cannot access ref value during render. " + refsWhyRefsAreOutsideRender,
		}
	}
}

// refsReport points a finding at the syntax its value came from.
//
// Through `ctx.ReportNode` rather than a Place's range, because a Place's range is the range of the
// node that produced the instruction, and reporting through the node routes via `rule.TokenRange`,
// which trims leading trivia at the harness. A finding whose value is a pure temporary with no
// syntactic source falls back to the function's own node rather than being dropped.
func refsReport(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	finding refsFinding,
) {
	message := refsMessageFor(finding.Kind)
	if finding.Node != nil {
		ruleContext.ReportNode(finding.Node, message)
		return
	}
	if finding.HasValue {
		if node := refsIdentifierNode(function, finding.Value); node != nil {
			ruleContext.ReportNode(node, message)
			return
		}
	}
	if function.Node != nil {
		ruleContext.ReportNode(function.Node, message)
	}
}
