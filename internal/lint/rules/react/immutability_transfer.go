package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/static_single_assignment"
)

// ---------------------------------------------------------------------------
// The transfer function
// ---------------------------------------------------------------------------

// immutabilityTransfer applies one instruction to the state and collects any findings.
//
// The arms fall into three groups and it is worth naming them, because reading the switch as one
// flat list hides the structure:
//
//   - Arms that freeze. A hook call freezes its arguments, JSX freezes what it interpolates, a hook
//     return freezes what it hands back. These are the sources of every finding this rule produces.
//   - Arms that write. A property store, a computed store, a delete, an update operator. Each asks
//     the lattice what writing to that value does, and reports when the answer is not "allowed".
//   - Arms that propagate. Object and array construction, destructuring, calls. These move kinds
//     around without deciding anything.
//
// The default arm deliberately does nothing. Unlike refs.go, whose default arm reports
// conservatively, an unmodelled instruction here must not report: this rule's finding is a write,
// and inventing writes from instructions we do not model would report ordinary code. Failing toward
// silence is the safe direction for a mutation rule and toward reporting is the safe direction for
// an access rule, which is why the two neighbours differ.
func immutabilityTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	state *immutabilityState,
	instruction *high_level_intermediate_representation.Instruction,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	target := instruction.LValue.Identifier
	collected := findings

	switch instructionValue := instruction.Value.(type) {

	// --- Freezing ---------------------------------------------------------

	// JSX freezes every value it interpolates: React has read it to render, so a later write
	// changes what the program sees and not what React saw. Measured: the same mutation before the
	// JSX is clean and after it reports.
	case *high_level_intermediate_representation.JsxExpression:
		for _, attribute := range instructionValue.Properties {
			// A callback handed to JSX is held by React past the end of this render, exactly as one
			// handed to a hook is, so the known-mutable-function check belongs here too. Measured:
			// `let l = 0; const cb = () => { l = 1; }; return <div onClick={cb} />;` reports twice
			// under React, once at the reassignment and once at the attribute. Checking only hook
			// arguments left both findings missing and the rule silent on the single most common
			// spelling of this mistake.
			collected = immutabilityCheckFrozenFunction(
				state,
				attribute.Value.Identifier,
				instruction.Node,
				collected,
				order,
			)
			state.freeze(attribute.Value.Identifier, immutabilityReasonJsxCaptured)
		}
		for _, child := range instructionValue.Children {
			collected = immutabilityCheckFrozenFunction(state, child.Identifier, instruction.Node, collected, order)
			state.freeze(child.Identifier, immutabilityReasonJsxCaptured)
		}
		state.setKind(
			target,
			immutabilityAbstractValue{Kind: immutabilityFrozen, Reasons: immutabilityReasonJsxCaptured},
		)

	case *high_level_intermediate_representation.JsxFragment:
		for _, child := range instructionValue.Children {
			state.freeze(child.Identifier, immutabilityReasonJsxCaptured)
		}
		state.setKind(
			target,
			immutabilityAbstractValue{Kind: immutabilityFrozen, Reasons: immutabilityReasonJsxCaptured},
		)

	// --- Writing ----------------------------------------------------------

	case *high_level_intermediate_representation.PropertyStore:
		collected = immutabilityCheckWrite(
			ruleContext,
			function,
			state,
			instructionValue.Object.Identifier,
			instruction.Node,
			collected,
			order,
		)

	case *high_level_intermediate_representation.ComputedStore:
		collected = immutabilityCheckWrite(
			ruleContext,
			function,
			state,
			instructionValue.Object.Identifier,
			instruction.Node,
			collected,
			order,
		)

	case *high_level_intermediate_representation.PropertyDelete:
		collected = immutabilityCheckWrite(
			ruleContext,
			function,
			state,
			instructionValue.Object.Identifier,
			instruction.Node,
			collected,
			order,
		)

	case *high_level_intermediate_representation.ComputedDelete:
		collected = immutabilityCheckWrite(
			ruleContext,
			function,
			state,
			instructionValue.Object.Identifier,
			instruction.Node,
			collected,
			order,
		)

	// `props.count++` and `props.count += 1` are deliberately not handled here, and the reason is a
	// property of the lowering rather than of React. Dumping the representation for `props.count++`
	// shows five instructions: a load of `props`, a property load of `count`, the update itself,
	// another load of `props`, and a property store writing the result back. The update's operand is
	// the loaded number, not the object, so an arm here would ask the lattice about a primitive and
	// answer nothing, while the store that follows is the real write and is already handled above.
	//
	// Both spellings report through that store, measured, with the span on the member access. An arm
	// here reported zero findings on `props.count++` and reading it as a missing feature would have
	// produced a second finding on every compound assignment in the tree.

	// A write to a global binding is upstream's `global_reassignment`, which carries the Globals
	// category rather than this one, so it is deliberately not reported here. The global value's
	// kind is still recorded, because a property store through a global does belong to this rule.
	case *high_level_intermediate_representation.StoreGlobal:
		state.setKind(target, immutabilityAbstractValue{Kind: immutabilityGlobal, Reasons: immutabilityReasonGlobal})

	// --- Propagating ------------------------------------------------------

	// A global read is a value declared outside the component. Writing through one reports with the
	// "defined outside a component or hook" reason.
	case *high_level_intermediate_representation.LoadGlobal:
		state.setKind(target, immutabilityAbstractValue{Kind: immutabilityGlobal, Reasons: immutabilityReasonGlobal})

	// A property load off a frozen value is itself frozen, which is what makes `props.a.q = 1`
	// report: the intermediate `props.a` inherits the freeze from `props`.
	case *high_level_intermediate_representation.PropertyLoad:
		state.setKind(target, immutabilityPropagate(state, instructionValue.Object.Identifier))
		if name := immutabilityIdentifierName(function, instructionValue.Object.Identifier); name != "" {
			state.names[target] = name
		} else if carried, ok := state.names[instructionValue.Object.Identifier]; ok {
			state.names[target] = carried
		}

	case *high_level_intermediate_representation.ComputedLoad:
		state.setKind(target, immutabilityPropagate(state, instructionValue.Object.Identifier))

	// Destructuring distributes the source's kind to every bound name, which is how a destructured
	// parameter's members become frozen without a per-member rule.
	case *high_level_intermediate_representation.Destructure:
		carried := immutabilityPropagate(state, instructionValue.Value.Identifier)
		high_level_intermediate_representation.EachPlace(instruction.Value, func(
			place high_level_intermediate_representation.Place,
			role high_level_intermediate_representation.PlaceRole,
		) {
			if role == high_level_intermediate_representation.PlaceRoleDefine {
				state.setKind(place.Identifier, carried)
			}
		})

	// A literal is a fresh mutable value: this code just made it and nothing has read it yet.
	// A literal is a fresh mutable value: this code just made it and nothing has read it yet. What it
	// holds is recorded so that freezing the aggregate later reaches its contents, which is what
	// makes `useMemo(() => 1, [o]); o.a = 1;` report: the freeze lands on the dependency array and
	// has to travel one hop inward to reach `o`. Without that hop the array froze and the value it
	// named did not, and the fixture read as a missing hook rather than as a missing edge.
	case *high_level_intermediate_representation.ObjectExpression,
		*high_level_intermediate_representation.ArrayExpression:
		elements := []static_single_assignment.IdentifierId{}
		high_level_intermediate_representation.EachPlace(instruction.Value, func(
			place high_level_intermediate_representation.Place,
			role high_level_intermediate_representation.PlaceRole,
		) {
			elements = append(elements, place.Identifier)
		})
		state.aggregateElements[state.resolve(target)] = elements
		state.setKind(target, immutabilityAbstractValue{Kind: immutabilityMutable})

	case *high_level_intermediate_representation.Primitive,
		*high_level_intermediate_representation.BinaryExpression,
		*high_level_intermediate_representation.TemplateLiteral:
		state.setKind(target, immutabilityAbstractValue{Kind: immutabilityPrimitive})

	// A nested function is analysed as an inner subject: which locals it reassigns is recorded on
	// the value, so the finding lands where the function is handed to React rather than at the
	// reassignment. That is what makes the two reassignment diagnostics point where they do.
	case *high_level_intermediate_representation.FunctionExpression:
		collected = immutabilityNestedFunctionTransfer(
			ruleContext,
			function,
			state,
			instructionValue,
			target,
			collected,
			order,
		)

	case *high_level_intermediate_representation.CallExpression:
		collected = immutabilityCallTransfer(
			ruleContext,
			function,
			state,
			instruction,
			instructionValue.Callee.Identifier,
			instructionValue.Arguments,
			target,
			collected,
			order,
		)

	case *high_level_intermediate_representation.MethodCall:
		collected = immutabilityMethodCallTransfer(
			ruleContext,
			function,
			state,
			instruction,
			instructionValue,
			target,
			collected,
			order,
		)

	default:
		// Deliberately silent. See the doc comment: an unmodelled instruction must not invent a
		// write.
	}

	return collected
}

// immutabilityPropagate is what a value loaded out of another value inherits.
//
// A member of a frozen value is frozen; a member of a mutable value is mutable. The reasons travel
// with it so the message still names the original source.
//
// A member of a primitive is treated as mutable rather than primitive: reading a property off a
// number yields undefined, not another primitive, and carrying `Primitive` forward would make a
// later write silent for the wrong reason.
func immutabilityPropagate(
	state *immutabilityState,
	object static_single_assignment.IdentifierId,
) immutabilityAbstractValue {
	current := state.kindOf(object)
	if current.Kind == immutabilityPrimitive {
		return immutabilityAbstractValue{Kind: immutabilityMutable}
	}
	return current
}

// immutabilityCheckWrite asks the lattice what writing to a value does and reports when it must.
//
// Mutates: order advances past each finding it numbers, so findings keep instruction order
func immutabilityCheckWrite(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	state *immutabilityState,
	object static_single_assignment.IdentifierId,
	node *ast.Node,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	// A ref is exempt before the lattice is consulted at all, which is upstream's `MutateRef` arm:
	// `mutate_with_span` asks `is_ref_or_ref_value(ty)` first and returns before it looks at any
	// kind. Writing `something.current` is the sanctioned way to hold a value across renders, and a
	// ref is deliberately outside React's data flow, so the whole premise of this rule does not
	// apply to it.
	//
	// This was 180 of 281 findings on Kirk's tree, every single one an ordinary
	// `handlersReference.current = handlers` inside an effect, and it is the reason step 10 exists.
	// The 40 imported fixtures were all green over it, because none of them writes through a ref: a
	// rule can be right about every case its corpus contains and wrong about the most common shape
	// in the repository it actually runs on. Measured on the executable with a live control in the
	// same batch, so the silence is a real exemption rather than a probe that could see nothing:
	// `r.current = 1` is clean in render, in an effect and in a callback, while a non-ref value from
	// the same hook shape reports.
	//
	// `refs.go` owns the render-time half of this question and reports a ref access during render
	// under its own rule, so the exemption here loses no coverage; it routes the finding to the rule
	// whose name matches it.
	if immutabilityIsRefValue(ruleContext, function, state, object) ||
		immutabilityWriteTargetIsReference(ruleContext, node) {
		return findings
	}
	current := state.kindOf(object)
	switch immutabilityMutate(current.Kind) {
	case immutabilityMutationFrozen, immutabilityMutationGlobal:
		*order++
		return append(findings, immutabilityFinding{
			Kind:     immutabilityFindingImmutableValue,
			Reasons:  current.Reasons,
			Variable: state.nameOf(function, object),
			Node:     immutabilityMutatedObjectNode(node, object, function),
			order:    *order,
		})
	}
	return findings
}

// immutabilityMutatedObjectNode is the syntax React underlines for a write.
//
// React points at the object being mutated rather than at the assignment or at the member access,
// and the difference is one node in each direction, so a span assertion is the only fixture that can
// see it. Measured on the executable, reading the rendered underline for each shape:
//
//	props.a = 1        underlines `props`      not `props.a` and not `props.a = 1`
//	props.a.q = 1      underlines `props.a`    the object of the write, one level in
//	delete props.a     underlines `props`
//	props.a = 1 inside a callback   underlines `props`
//
// So the node wanted is the left-hand side of the member expression being written, which is the
// instruction node's own object. Falling back to the instruction node keeps a finding rather than
// dropping one when the shape is not a member expression.
func immutabilityMutatedObjectNode(
	node *ast.Node,
	object static_single_assignment.IdentifierId,
	function *high_level_intermediate_representation.Function,
) *ast.Node {
	if node == nil {
		return immutabilityIdentifierNode(function, object)
	}
	target := node
	// A write lowers with the assignment as its node in some shapes and the member access in
	// others, so the assignment is unwrapped first and the member access second.
	if target.Kind == ast.KindBinaryExpression {
		if left := target.AsBinaryExpression().Left; left != nil {
			target = left
		}
	}
	if target.Kind == ast.KindDeleteExpression {
		if inner := target.Expression(); inner != nil {
			target = inner
		}
	}
	switch target.Kind {
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		if expression := target.Expression(); expression != nil {
			return expression
		}
	}
	if resolved := immutabilityIdentifierNode(function, object); resolved != nil {
		return resolved
	}
	return node
}

// immutabilityCallTransfer handles a call, which is where most freezing happens.
//
// A hook call freezes every argument, because React holds onto what it is given across renders. It
// also decides the result's kind: `useState` and `useReducer` return frozen state, `useContext`
// returns a frozen context value, and any other hook returns a frozen hook return. Those three
// reasons select three different messages over one verdict.
//
// A plain call freezes nothing. That is not a simplification: measured on React's executable,
// `const o = {}; doThing(o); o.a = 1;` is clean while the same code with `useThing` reports. React
// only freezes at boundaries it controls.
func immutabilityCallTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	state *immutabilityState,
	instruction *high_level_intermediate_representation.Instruction,
	callee static_single_assignment.IdentifierId,
	arguments []high_level_intermediate_representation.Argument,
	target static_single_assignment.IdentifierId,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	name := state.nameOf(function, callee)
	if !immutabilityIsHookName(name) {
		// A non-hook call leaves everything alone, but a function value that reassigns a local is
		// still being called here rather than handed to React, and calling it during render is
		// ordinary. Nothing to do.
		state.setKind(target, immutabilityAbstractValue{Kind: immutabilityMutable})
		return findings
	}

	collected := findings
	for _, argument := range arguments {
		// Handing a function that reassigns a local to a hook is the "known mutable function"
		// finding: React will hold the function past render and calling it later writes to a
		// variable whose value the next render has already forgotten.
		collected = immutabilityCheckFrozenFunction(
			state,
			argument.Place.Identifier,
			instruction.Node,
			collected,
			order,
		)
		if immutabilityDependencyArrayIsExempt(name) &&
			immutabilityIsDependencyArray(function, state, argument.Place.Identifier) {
			continue
		}
		state.freeze(argument.Place.Identifier, immutabilityReasonHookCaptured)
	}

	state.setKind(target, immutabilityHookResultKind(name))
	return collected
}

// immutabilityDependencyArrayIsExempt reports whether this hook leaves its dependencies unread.
//
// The exemption is not a property of the array-literal shape alone, which is where the first version
// of this went wrong. Measured across eight hooks with the identical `hook(() => 1, [o]); o.a = 1;`
// body, so the shape is held constant and only the callee varies:
//
//	useEffect             CLEAN      useMemo               REPORTS
//	useLayoutEffect       CLEAN      useCallback           REPORTS
//	useInsertionEffect    CLEAN
//	useImperativeHandle   CLEAN
//	useDeferredValue      CLEAN
//	useThing (unknown)    CLEAN
//
// The split is exactly the two memoization hooks, and the reason is that their callbacks run during
// render, so React reads the dependencies on the spot; every other hook defers past the end of
// render and only stores them. So the test is on the hook rather than on the argument, and an
// unknown hook falls on the exempt side because that is where every measured unknown falls.
//
// This is the arm that no amount of reading produced. The obvious rule -- "an array literal in
// argument position is a dependency list and dependency lists are not captures" -- is right for six
// of these eight and silently wrong for the two most common memo spellings in any real codebase.
func immutabilityDependencyArrayIsExempt(name string) bool {
	return name != "useMemo" && name != "useCallback"
}

// immutabilityIsDependencyArray reports whether an argument is an inline array literal.
//
// A dependency array is not a capture, and this is the one arm of the hook-freezing rule that
// reading the source would not have produced. Measured on React's executable with a live control in
// the same batch:
//
//	useThing(o);                  o.a = 1    REPORTS   the value is handed to the hook
//	useThing(() => 1, [o]);       o.a = 1    CLEAN     the value is only named as a dependency
//	useEffect(() => {}, [o]);     o.a = 1    CLEAN     same shape, the common spelling of it
//	useEffect(() => { read(o); }, []); o.a=1 REPORTS   the value is captured by the callback
//
// The distinction is not about which hook it is. `useMemo(() => 1, [o])` reports, and it has the
// identical shape, because a memo callback runs during render and React reads what it closes over.
// So the array itself is frozen either way; what differs is whether anything reads the elements,
// and only an inline array literal in argument position is exempt from distributing its freeze
// inward. Anything reached some other way, including an array built into a variable first, is not
// covered by this and freezes normally, which is upstream's behaviour and is why the test is on the
// argument's own instruction rather than on the value's kind.
//
// Note this is a lookup on the defining instruction rather than on syntax: a value whose defining
// instruction is an array literal is the exemption, and a value that merely happens to hold an
// array is not.
func immutabilityIsDependencyArray(
	function *high_level_intermediate_representation.Function,
	state *immutabilityState,
	id static_single_assignment.IdentifierId,
) bool {
	resolved := state.resolve(id)
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := immutabilityInstructionAt(function, instructionId)
			if instruction == nil {
				continue
			}
			if state.resolve(instruction.LValue.Identifier) != resolved {
				continue
			}
			if _, isArray := instruction.Value.(*high_level_intermediate_representation.ArrayExpression); isArray {
				return true
			}
		}
	}
	return false
}

// immutabilityHookResultKind is what a hook hands back.
//
// The three reasons here are the three different messages a reader sees for one verdict, and they
// were each confirmed against React's executable rather than read off the reason enum:
//
//	const [s] = useState({});   s.a = 1     "returned from 'useState()'"
//	const [s] = useReducer(..); s.a = 1     "returned from 'useReducer()'"
//	const c = useContext(C);    c.a = 1     "returned from 'useContext()'"
//	const h = useThing();       h.a = 1     "returned from a hook"
func immutabilityHookResultKind(name string) immutabilityAbstractValue {
	reason := immutabilityReasonHookReturn
	switch name {
	case "useState":
		reason = immutabilityReasonState
	case "useReducer":
		reason = immutabilityReasonReducerState
	case "useContext":
		reason = immutabilityReasonContext
	case "useEffect", "useLayoutEffect", "useInsertionEffect":
		reason = immutabilityReasonEffect
	}
	return immutabilityAbstractValue{Kind: immutabilityFrozen, Reasons: reason}
}

// immutabilityMethodCallTransfer handles `object.method(...)`.
//
// The hook test reads the property name, so `React.useEffect(...)` behaves like the bare import.
// refs.go measured this as 235 false positives on Kirk's tree when it was missing, and the same
// shape is live here: a value frozen inside a `React.useCallback` callback must be frozen by the
// same rule as one frozen inside `useCallback`.
//
// A method call on a value does not freeze the receiver and does not report a mutation through it.
// Measured: `props.list.push(1)` and `Object.assign(props, {a:1})` are both clean under React, which
// is upstream declining to model an unknown method's effects rather than deciding the write is
// fine. Reproduced as silence deliberately; a rule that helpfully reported `props.list.push(1)`
// would disagree with React on ordinary code.
func immutabilityMethodCallTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	state *immutabilityState,
	instruction *high_level_intermediate_representation.Instruction,
	value *high_level_intermediate_representation.MethodCall,
	target static_single_assignment.IdentifierId,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	name := immutabilityMethodName(instruction)
	if !immutabilityIsHookName(name) {
		state.setKind(target, immutabilityAbstractValue{Kind: immutabilityMutable})
		return findings
	}
	collected := findings
	for _, argument := range value.Arguments {
		collected = immutabilityCheckFrozenFunction(
			state,
			argument.Place.Identifier,
			instruction.Node,
			collected,
			order,
		)
		if immutabilityDependencyArrayIsExempt(name) &&
			immutabilityIsDependencyArray(function, state, argument.Place.Identifier) {
			continue
		}
		state.freeze(argument.Place.Identifier, immutabilityReasonHookCaptured)
	}
	state.setKind(target, immutabilityHookResultKind(name))
	return collected
}

// immutabilityMethodName is the property being called, read from the syntax.
//
// The representation gives a method call a Place for the property, but the property's name lives on
// the syntax rather than on the value, so it is read back from the node. Identical in shape to
// refs.go's helper and kept separate for the naming reason given at the top of this file.
func immutabilityMethodName(instruction *high_level_intermediate_representation.Instruction) string {
	if instruction == nil || instruction.Node == nil {
		return ""
	}
	node := instruction.Node
	if node.Kind == ast.KindCallExpression {
		if expression := node.Expression(); expression != nil {
			node = expression
		}
	}
	if node.Kind != ast.KindPropertyAccessExpression {
		return ""
	}
	name := node.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return ""
	}
	return name.Text()
}

// immutabilityNestedFunctionTransfer records which locals a nested function reassigns.
//
// The finding does not land inside the nested function. It lands where that function is handed to
// React, which is upstream's design and is why the two reassignment messages talk about "after
// render completes" rather than about the write itself: the write is fine, the problem is that it
// happens at a time when nothing will re-render.
//
// The inner function is examined through its own identifier space. Identifier ids are per-function
// and they overlap, which refs.go measured as a wrong-finding rather than a missing one, so the
// inner walk here reads only names and syntax and never indexes the outer state by an inner id.
func immutabilityNestedFunctionTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	state *immutabilityState,
	value *high_level_intermediate_representation.FunctionExpression,
	target static_single_assignment.IdentifierId,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	inner := immutabilityNestedFunction(function, value.Function)
	if inner == nil {
		state.setKind(target, immutabilityAbstractValue{Kind: immutabilityMutable})
		return findings
	}
	// A closure that writes through a captured value is a different finding from one that reassigns
	// a captured binding, and it lands at the write rather than where the closure is handed over.
	// Measured: `const cb = () => { props.a = 1; };` reports on line 2, inside the callback, while
	// `const cb = () => { local = 1; };` reports on the line that hands `cb` to JSX. The two look
	// alike in source and are opposite in where they point.
	collected := immutabilityCapturedWrites(ruleContext, state, inner, value, findings, order)
	// A reassignment only matters when the binding belongs to this function, which is the render
	// whose end outlives it. `Function.Context` names what the closure captured from here, so a
	// write to anything else is a write to a binding some intermediate scope owns and the render has
	// no stake in it.
	//
	// Three false-positive classes on Kirk's tree came from omitting this, 30 findings between them,
	// and all three are shapes React is silent on (measured, against a control that reports twice):
	//
	//	useEffect(() => { let cancelled = false; ...; return () => { cancelled = true; }; }, [])
	//	useEffect(() => { let id; function h() { id = setTimeout(...); } ... }, [])
	//	const cb = () => { let v; if (c) { v = 1; } else { v = 2; } return v; }
	//
	// Every one declares its variable inside a callback and writes it from a deeper callback, so the
	// binding never belonged to the render at all. The cleanup-function idiom is the most common of
	// the three and it is the single most idiomatic thing in React effects.
	// Compared by declaration rather than by identifier id, which is the trap here. The context
	// value and the store's lvalue are two single-assignment numberings of the same binding and
	// carry different ids: measured on `let local = 0; const cb = () => { local = 1; };`, the
	// capture arrives as context id 2 while the store writes id 6. An identifier comparison answers
	// false for every real case and silences the rule completely, which is what it did on the first
	// run of this check. `high_level_intermediate_representation.Identifier.Declaration` is the identity that survives
	// the renumbering.
	owned := map[static_single_assignment.DeclarationId]bool{}
	for index := range value.Captures {
		if index >= len(inner.Context) {
			continue
		}
		contextId := inner.Context[index].Identifier
		if int(contextId) < len(inner.Identifiers) {
			if identifier := inner.Identifiers[contextId]; identifier != nil {
				owned[identifier.Declaration] = true
			}
		}
	}
	reassignments := immutabilityFilterOwned(immutabilityReassignmentsIn(inner, inner.IsAsync), owned)
	if len(reassignments) > 0 {
		state.functionsReassigning[state.resolve(target)] = reassignments
	}
	// The values this closure captures are recorded on the function value, so that freezing the
	// function freezes what it closes over. That is upstream's
	// `enableTransitivelyFreezeFunctionExpressions`, which defaults to `true`, and it is what makes
	// `const o = {}; useEffect(() => { read(o); }, []); o.a = 1;` report while the same value named
	// only in the dependency array stays clean. Both were measured; the pair is the fixture
	// `f_useEffect_callback_captures` against `s_useEffect_dependency_array`.
	captured := make([]static_single_assignment.IdentifierId, 0, len(value.Captures))
	for _, capture := range value.Captures {
		captured = append(captured, capture.Identifier)
	}
	state.functionCaptures[state.resolve(target)] = captured
	state.setKind(target, immutabilityAbstractValue{Kind: immutabilityMutable})
	return collected
}

// immutabilityWriteTargetIsReference asks the checker whether the thing being written to is a ref.
//
// Asked through the write's own syntax rather than through the value's identifier, and that is the
// whole trick. A property store's object is a temporary produced by a load, so
// `high_level_intermediate_representation.Identifier.Node` is nil for it and every type question asked that way answers
// nothing. The member expression on the left of the assignment still names the object in source, and handing
// that member expression to the checker resolves.
//
// Measured, and the measurement is the reason this function exists in this shape. Asking through
// the identifier returned a nil symbol for all 281 findings, which reads exactly like "the checker
// cannot see React's types here" and would have justified giving up on the exemption. Asking
// through the write's syntax resolved `RefObject` for 215 of 223 on the same tree in the same run.
// One phrasing of the same question is blind and the other is not.
//
// A hand-written `react.d.ts` stub cannot reproduce this either way, which is why it was measured on
// the real tree rather than in a fixture: `rule_testing`'s tsconfig sets `types: []` and resolves no
// `node_modules`, so `@types/react` never resolves there and every symbol is nil. The fixtures
// therefore cannot see this exemption at all, and the two ref fixtures below pin the name half only.
func immutabilityWriteTargetIsReference(ruleContext rule.Context, node *ast.Node) bool {
	if ruleContext.TypeChecker == nil || node == nil {
		return false
	}
	target := node
	if target.Kind == ast.KindBinaryExpression {
		target = target.AsBinaryExpression().Left
	}
	if target != nil && target.Kind == ast.KindDeleteExpression {
		target = target.Expression()
	}
	if target == nil {
		return false
	}
	if target.Kind != ast.KindPropertyAccessExpression && target.Kind != ast.KindElementAccessExpression {
		return false
	}
	// Walk out through the whole member chain rather than testing only the immediate object.
	//
	// `reference.current.style.height = ''` writes to the DOM node a ref holds, and the immediate
	// object is a `CSSStyleDeclaration` rather than a `RefObject`, so a test on one level answers
	// false and reports. It was 7 of the 10 remaining findings on Kirk's tree and every one was
	// ordinary imperative DOM code inside an effect, which React is silent on: measured, both
	// `r.current.style.height = ''` and `r.current.options = {}` are clean, against a control that
	// reports.
	//
	// The walk is bounded rather than recursive because a member chain is short and a bound cannot
	// hang a linter on a pathological input.
	expression := target.Expression()
	for depth := 0; expression != nil && depth < 16; depth++ {
		valueType := ruleContext.TypeChecker.GetTypeAtLocation(expression)
		if valueType != nil {
			// Compared positively rather than checked for non-nil: the shim is a hand-mirrored
			// struct read through `unsafe.Pointer` and has returned a silently wrong type before,
			// so asserting that what came back is what was asked for is the only check that can see
			// that failure.
			if symbol := shimchecker.Type_symbol(valueType); symbol != nil {
				if symbol.Name == "RefObject" || symbol.Name == "MutableRefObject" {
					return true
				}
			}
		}
		if expression.Kind != ast.KindPropertyAccessExpression && expression.Kind != ast.KindElementAccessExpression {
			return false
		}
		expression = expression.Expression()
	}
	return false
}

// immutabilityCapturedWrites reports a nested function writing through a captured frozen value.
//
// The write happens inside the closure and is reported there, which is upstream's own placement and
// is measurably different from where a captured-binding reassignment reports. The two are easy to
// conflate because both are "a closure touching an outer value".
//
// The capture edge is what makes this answerable: `FunctionExpression.Captures` at the call site and
// `Function.Context` inside are positionally paired, so the Nth capture is the Nth context value.
// That translation is required rather than convenient, because identifier ids are per-function and
// overlap, so indexing the outer state with an inner id silently reads an unrelated value. refs.go
// measured that failure as a wrong finding rather than a missing one.
//
// Recurses one level only. A closure inside a closure writing through a doubly-captured value is a
// real shape and it is not covered here; see the report for what that costs.
func immutabilityCapturedWrites(
	ruleContext rule.Context,
	state *immutabilityState,
	inner *high_level_intermediate_representation.Function,
	value *high_level_intermediate_representation.FunctionExpression,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	// Translate the outer kinds into the inner function's identifier space through the capture edge.
	innerKinds := map[static_single_assignment.IdentifierId]immutabilityAbstractValue{}
	innerNames := map[static_single_assignment.IdentifierId]string{}
	for index, capture := range value.Captures {
		if index >= len(inner.Context) {
			continue
		}
		contextId := inner.Context[index].Identifier
		innerKinds[contextId] = state.kindOf(capture.Identifier)
		innerNames[contextId] = state.nameOf(nil, capture.Identifier)
	}
	// An alias built inside the closure carries the captured value's kind along with it.
	aliases := map[static_single_assignment.IdentifierId]static_single_assignment.IdentifierId{}
	resolve := func(id static_single_assignment.IdentifierId) static_single_assignment.IdentifierId {
		current := id
		for round := 0; round < 32; round++ {
			next, found := aliases[current]
			if !found || next == current {
				return current
			}
			current = next
		}
		return current
	}
	collected := findings
	for _, block := range inner.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := immutabilityInstructionAt(inner, instructionId)
			if instruction == nil {
				continue
			}
			switch instructionValue := instruction.Value.(type) {
			case *high_level_intermediate_representation.LoadContext:
				aliases[instruction.LValue.Identifier] = instructionValue.Place.Identifier
			case *high_level_intermediate_representation.LoadLocal:
				aliases[instruction.LValue.Identifier] = instructionValue.Place.Identifier
			case *high_level_intermediate_representation.PropertyLoad:
				// A member of a captured frozen value is frozen too, which is what reaches
				// `props.a.q = 1` written inside a callback.
				if carried, known := innerKinds[resolve(instructionValue.Object.Identifier)]; known {
					innerKinds[instruction.LValue.Identifier] = carried
				}
			case *high_level_intermediate_representation.PropertyStore:
				collected = immutabilityCapturedWriteFinding(
					ruleContext,
					inner,
					innerKinds,
					innerNames,
					resolve(instructionValue.Object.Identifier),
					instruction,
					collected,
					order,
				)
			case *high_level_intermediate_representation.ComputedStore:
				collected = immutabilityCapturedWriteFinding(
					ruleContext,
					inner,
					innerKinds,
					innerNames,
					resolve(instructionValue.Object.Identifier),
					instruction,
					collected,
					order,
				)
			case *high_level_intermediate_representation.PropertyDelete:
				collected = immutabilityCapturedWriteFinding(
					ruleContext,
					inner,
					innerKinds,
					innerNames,
					resolve(instructionValue.Object.Identifier),
					instruction,
					collected,
					order,
				)
			case *high_level_intermediate_representation.ComputedDelete:
				collected = immutabilityCapturedWriteFinding(
					ruleContext,
					inner,
					innerKinds,
					innerNames,
					resolve(instructionValue.Object.Identifier),
					instruction,
					collected,
					order,
				)
			}
		}
	}
	return collected
}

// immutabilityCapturedWriteFinding reports one write through a captured value when it is frozen.
//
// Mutates: order advances past each finding it numbers, so findings keep instruction order
func immutabilityCapturedWriteFinding(
	ruleContext rule.Context,
	inner *high_level_intermediate_representation.Function,
	kinds map[static_single_assignment.IdentifierId]immutabilityAbstractValue,
	names map[static_single_assignment.IdentifierId]string,
	object static_single_assignment.IdentifierId,
	instruction *high_level_intermediate_representation.Instruction,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	current, known := kinds[object]
	if !known {
		return findings
	}
	// The same ref exemption as the outer write check. A ref captured by a callback and written
	// there is the single most common React idiom there is, and it accounted for most of this
	// rule's first dry run.
	if names[object] != "" && refsIsRefLikeName(names[object]) {
		return findings
	}
	if immutabilityWriteTargetIsReference(ruleContext, instruction.Node) {
		return findings
	}
	switch immutabilityMutate(current.Kind) {
	case immutabilityMutationFrozen, immutabilityMutationGlobal:
		*order++
		return append(findings, immutabilityFinding{
			Kind:     immutabilityFindingImmutableValue,
			Reasons:  current.Reasons,
			Variable: names[object],
			Node:     immutabilityMutatedObjectNode(instruction.Node, object, nil),
			order:    *order,
		})
	}
	return findings
}

// immutabilityNestedFunction resolves a function id within its parent.
func immutabilityNestedFunction(
	function *high_level_intermediate_representation.Function,
	id high_level_intermediate_representation.FunctionId,
) *high_level_intermediate_representation.Function {
	if function == nil || int(id) >= len(function.Functions) {
		return nil
	}
	return function.Functions[id]
}

// immutabilityReassignmentsIn collects the captured locals a function writes to.
//
// A write to a context value is the tell: lowering emits `StoreContext` for a write to a binding
// the function closed over, and `StoreLocal` for one it owns. That distinction is exactly the
// question "does this write escape the function", and it comes free from the representation rather
// than needing a capture analysis.
//
// Recurses into further nested functions, because a callback inside a callback that reassigns an
// outer local is the same finding. `error.invalid-nested-function-reassign-local-variable-in-effect`
// is that shape.
func immutabilityReassignmentsIn(
	function *high_level_intermediate_representation.Function,
	isAsync bool,
) []immutabilityReassignment {
	return immutabilityReassignmentsInner(function, isAsync, true)
}

// immutabilityReassignmentsInner carries whether this level is the immediate closure.
func immutabilityReassignmentsInner(
	function *high_level_intermediate_representation.Function,
	isAsync bool,
	direct bool,
) []immutabilityReassignment {
	if function == nil {
		return nil
	}
	var found []immutabilityReassignment
	seen := map[string]bool{}
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := immutabilityInstructionAt(function, instructionId)
			if instruction == nil {
				continue
			}
			store, isStore := instruction.Value.(*high_level_intermediate_representation.StoreContext)
			if !isStore {
				continue
			}
			name := immutabilityIdentifierName(function, store.LValue.Identifier)
			if name == "" || seen[name] {
				continue
			}
			declarationOf := static_single_assignment.DeclarationId(0)
			if int(store.LValue.Identifier) < len(function.Identifiers) {
				if identifier := function.Identifiers[store.LValue.Identifier]; identifier != nil {
					declarationOf = identifier.Declaration
				}
			}
			seen[name] = true
			found = append(found, immutabilityReassignment{
				Variable: name,
				Node:     instruction.Node,
				IsAsync:  isAsync,
				Target:   declarationOf,
				Direct:   direct,
			})
		}
	}
	for _, nested := range function.Functions {
		for _, reassignment := range immutabilityReassignmentsInner(nested, isAsync || nested.IsAsync, false) {
			if seen[reassignment.Variable] {
				continue
			}
			seen[reassignment.Variable] = true
			found = append(found, reassignment)
		}
	}
	return found
}

// immutabilityCheckFrozenFunction reports handing React a function that reassigns a local.
//
// Two findings come out of one condition, split on whether the reassigning function is async, which
// is upstream's own split between `reassigned_after_render` and `reassigned_in_async_function`.
// Both are reported at the reassignment, plus one `known_mutable_function` at the site where the
// function is handed over, which is why React emits two diagnostics for a single such callback.
//
// Mutates: order advances past each finding it numbers, so findings keep instruction order
func immutabilityCheckFrozenFunction(
	state *immutabilityState,
	id static_single_assignment.IdentifierId,
	node *ast.Node,
	findings []immutabilityFinding,
	order *int,
) []immutabilityFinding {
	reassignments, found := state.functionsReassigning[state.resolve(id)]
	if !found || len(reassignments) == 0 {
		return findings
	}
	collected := findings
	for _, reassignment := range reassignments {
		kind := immutabilityFindingReassignedAfterRender
		if reassignment.IsAsync {
			kind = immutabilityFindingReassignedInAsync
		}
		*order++
		collected = append(collected, immutabilityFinding{
			Kind:     kind,
			Variable: reassignment.Variable,
			Node:     reassignment.Node,
			order:    *order,
		})
	}
	*order++
	collected = append(collected, immutabilityFinding{
		Kind:     immutabilityFindingKnownMutableFunction,
		Variable: reassignments[0].Variable,
		Node:     node,
		order:    *order,
	})
	return collected
}
