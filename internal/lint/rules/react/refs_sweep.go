package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/static_single_assignment"
)

// ---------------------------------------------------------------------------
// The environment and the sweep
// ---------------------------------------------------------------------------

// refsSafeBlock is a region where one write to a specific ref is permitted.
//
// Registered on the fallthrough of an `if` whose test is a guard, and consumed by the first write
// it authorizes, which is what makes a second write under one guard still report.
type refsSafeBlock struct {
	Block static_single_assignment.BlockId
	RefId int
}

// refsFindingKind is which of the four diagnostics a finding is.
type refsFindingKind uint8

const (
	// refsFindingValueAccess is reading `ref.current` during render.
	refsFindingValueAccess refsFindingKind = iota
	// refsFindingPassedToFunction is handing a ref to a function that may read it.
	refsFindingPassedToFunction
	// refsFindingUpdate is writing `ref.current` during render.
	refsFindingUpdate
	// refsFindingFunctionAccessesReference is calling a function whose body reads a ref.
	refsFindingFunctionAccessesReference
	// refsFindingDidNotConverge is the invariant: the sweep was still moving after ten rounds.
	refsFindingDidNotConverge
)

// refsFinding is one diagnostic, held as data until the sweep settles.
//
// Deferred rather than reported inline for a reason that is upstream's and is easy to miss: the
// sweep runs the same instructions up to ten times, so reporting at the moment a check fails would
// emit each finding once per round. Upstream clears its error list at the top of every iteration
// and returns early once any error exists; this collects per-round and keeps the last round's set.
type refsFinding struct {
	Kind refsFindingKind
	// Value is the identifier to point the finding at.
	Value    static_single_assignment.IdentifierId
	HasValue bool
	// Node overrides where the finding points when the value alone would point at the wrong syntax.
	//
	// A ref access is a member expression, and upstream underlines the whole of it (`ref.current`,
	// eleven characters in the goldens). The identifier a lattice value carries is `ref`, which is
	// the object rather than the access, so pointing through it underlines three characters and no
	// message-id fixture can see the difference. A read whose value is a pure temporary has no
	// identifier node at all and would otherwise fall back to the whole function.
	Node *ast.Node
}

// refsEnvironment is the per-identifier lattice map plus the temporaries side map.
//
// `temporaries` is upstream's `Env::temporaries`: an aliasing side table letting a load or a store
// resolve to the value it ultimately names, so a chain of assignments behaves like the value at its
// head without the lattice map carrying an entry per link.
type refsEnvironment struct {
	changed     bool
	data        map[static_single_assignment.IdentifierId]*refsAccessType
	temporaries map[static_single_assignment.IdentifierId]static_single_assignment.IdentifierId
	// names carries a source binding name onto the temporary that loaded it.
	//
	// Populated only from LoadLocal, which is upstream's `set_name` and is called from exactly one
	// instruction arm (`infer_types.rs:482`). That single restriction is what makes
	// `props.fooRef.current` silent while `const fooRef = x; fooRef.current` reports: a property
	// load's result never receives a name, so the name test finds nothing to match on the outer
	// access. Propagating names through property loads as well would look more thorough and would
	// disagree with React on real code, measured on the executable in both directions.
	names map[static_single_assignment.IdentifierId]string
	// propertyNames is the property a load read, keyed by the value it produced. Consulted only by
	// the exact `props.ref` shape; see setPropertyName's call site.
	propertyNames map[static_single_assignment.IdentifierId]string
	// declarations and byDeclaration recover a value whose single-assignment numbering did not
	// unify. See `get`.
	// accessNodes is the member-expression node a ref value came from, so a finding underlines the
	// whole access rather than only the object identifier.
	accessNodes     map[static_single_assignment.IdentifierId]*ast.Node
	declarations    map[static_single_assignment.IdentifierId]static_single_assignment.DeclarationId
	byDeclaration   map[static_single_assignment.DeclarationId]*refsAccessType
	referenceIdSeed int
}

func newRefsEnvironment() *refsEnvironment {
	return &refsEnvironment{
		data:          map[static_single_assignment.IdentifierId]*refsAccessType{},
		temporaries:   map[static_single_assignment.IdentifierId]static_single_assignment.IdentifierId{},
		names:         map[static_single_assignment.IdentifierId]string{},
		propertyNames: map[static_single_assignment.IdentifierId]string{},
		accessNodes:   map[static_single_assignment.IdentifierId]*ast.Node{},
		declarations:  map[static_single_assignment.IdentifierId]static_single_assignment.DeclarationId{},
		byDeclaration: map[static_single_assignment.DeclarationId]*refsAccessType{},
	}
}

// nextRefId mints a fresh ref identity. Every call site of this is a place the convergence equality
// must not look, which is why it is funnelled through one method.
func (environment *refsEnvironment) nextRefId() int {
	environment.referenceIdSeed++
	return environment.referenceIdSeed
}

// operandId resolves an identifier through the temporaries side map.
func (environment *refsEnvironment) operandId(
	key static_single_assignment.IdentifierId,
) static_single_assignment.IdentifierId {
	if resolved, found := environment.temporaries[key]; found {
		return resolved
	}
	return key
}

func (environment *refsEnvironment) define(
	key static_single_assignment.IdentifierId,
	value static_single_assignment.IdentifierId,
) {
	resolved := environment.operandId(value)
	environment.temporaries[key] = resolved
	// The access node travels with the value along an alias chain, so `const v = ref.current;`
	// still underlines `ref.current` when the finding is raised at the read of `v`.
	if node, ok := environment.accessNodes[resolved]; ok {
		environment.accessNodes[key] = node
	} else if node, ok := environment.accessNodes[value]; ok {
		environment.accessNodes[key] = node
	}
}

// carryAccessNode copies a recorded access node from one value to another.
func (environment *refsEnvironment) carryAccessNode(
	to static_single_assignment.IdentifierId,
	from static_single_assignment.IdentifierId,
) {
	if node, ok := environment.accessNodes[environment.operandId(from)]; ok {
		environment.accessNodes[to] = node
		return
	}
	if node, ok := environment.accessNodes[from]; ok {
		environment.accessNodes[to] = node
	}
}

// setName carries a source name onto a temporary, upstream's `set_name`.
func (environment *refsEnvironment) setName(
	function *high_level_intermediate_representation.Function,
	target static_single_assignment.IdentifierId,
	source static_single_assignment.IdentifierId,
) {
	if name := refsIdentifierName(function, source); name != "" {
		environment.names[target] = name
	}
}

// setName2 records a name directly onto a value, used for a global's own name.
func (environment *refsEnvironment) setName2(target static_single_assignment.IdentifierId, name string) {
	if name != "" {
		environment.names[target] = name
	}
}

// setPropertyName records the property a load read, as the result's name in this position.
func (environment *refsEnvironment) setPropertyName(target static_single_assignment.IdentifierId, property string) {
	if property != "" {
		environment.propertyNames[target] = property
	}
}

// nameOf is the value's own binding name, or the one a LoadLocal carried onto it.
func (environment *refsEnvironment) nameOf(
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
) string {
	if name := refsIdentifierName(function, id); name != "" {
		return name
	}
	return environment.names[id]
}

func (environment *refsEnvironment) get(key static_single_assignment.IdentifierId) *refsAccessType {
	if found, ok := environment.data[environment.operandId(key)]; ok {
		return found
	}
	// Fall back to whatever the same source binding holds elsewhere in this function.
	//
	// This exists because single-assignment renaming is not total in our lowering: measured on the
	// corpus fixture `error.invalid-aliased-ref-in-callback-invoked-during-render-`, a closure that
	// builds an alias chain produces two identifiers for one `const current`, ids 8 and 20, both
	// carrying declaration 3, and the load reads id 8 while the store wrote id 20. Without the
	// alias in the same closure the two coincide and everything works, which is why this is
	// invisible on all but one fixture.
	//
	// So the value is looked up by declaration when the identifier itself has no entry. That is
	// sound here because a declaration groups exactly the values one source binding takes
	// (`high_level_intermediate_representation.Identifier.Declaration`), and it is conservative in the direction this
	// rule wants: it can only find a ref that some other numbering of the same binding already proved.
	//
	// This is a workaround for a representation defect rather than a transcription of upstream,
	// which has no such gap. It belongs in lowering; see the report.
	for _, candidate := range [2]static_single_assignment.IdentifierId{key, environment.operandId(key)} {
		if declaration, ok := environment.declarations[candidate]; ok {
			if found, ok := environment.byDeclaration[declaration]; ok {
				return found
			}
		}
	}
	return nil
}

// set widens rather than overwrites, and records whether the element moved.
//
// The widening join is what makes the sweep monotone, and the `changed` flag it maintains is the
// fixpoint's only termination signal. The comparison uses `refsTypeEqual`, which ignores minted ref
// identity: reading that comparison as ordinary equality is the defect that would prevent
// convergence.
func (environment *refsEnvironment) set(key static_single_assignment.IdentifierId, value *refsAccessType) {
	operandId := environment.operandId(key)
	current, hadCurrent := environment.data[operandId]
	widened := refsJoin(value, current, environment.nextRefId)
	if !hadCurrent && widened.Kind == refsNone {
		environment.data[operandId] = widened
		return
	}
	if !hadCurrent || !refsTypeEqual(current, widened) {
		environment.changed = true
	}
	environment.data[operandId] = widened
	// Record against the original key's declaration as well as the resolved one: an alias chain
	// resolves a named binding onto an unnamed temporary, so the declaration lives on the key that
	// was asked about rather than on the value it resolved to.
	if widened.Kind != refsNone {
		if declaration, ok := environment.declarations[key]; ok {
			environment.byDeclaration[declaration] = widened
		}
		if declaration, ok := environment.declarations[operandId]; ok {
			environment.byDeclaration[declaration] = widened
		}
	}
}

// noteDeclaration records which source binding a value belongs to, so `get` can recover a value
// whose single-assignment numbering did not unify with the one that was written.
func (environment *refsEnvironment) noteDeclaration(
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
) {
	if function == nil || int(id) >= len(function.Identifiers) {
		return
	}
	if identifier := function.Identifiers[id]; identifier != nil && identifier.Name != "" {
		environment.declarations[id] = identifier.Declaration
	}
}

// refsSweepFunction runs the bounded ten-round sweep over one function and returns its findings.
//
// The structure is upstream's: seed the parameters, collect the temporaries side map once, then
// iterate the whole function in block order until the environment stops moving or ten rounds pass.
// `Blocks` is in reverse postorder, which is what lets a single forward pass settle an acyclic
// region in one round; a loop needs the second round, which is what the bound is for.
func refsSweepFunction(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
) []refsFinding {
	env := newRefsEnvironment()
	refsCollectTemporaries(ruleContext, function, env)

	var findings []refsFinding
	for iteration := 0; iteration < 10; iteration++ {
		if iteration > 0 && !env.changed {
			break
		}
		env.changed = false
		findings = refsRunOneRound(ruleContext, function, env)
		// Upstream returns as soon as any error exists rather than iterating to a fixpoint over a
		// function it has already decided about. Reproduced: it also bounds the work.
		if len(findings) > 0 {
			return findings
		}
	}

	if env.changed {
		return []refsFinding{{Kind: refsFindingDidNotConverge}}
	}
	return findings
}

// refsCollectTemporaries builds the aliasing side map, upstream's `collect_temporaries_sidemap`.
//
// One detail is load-bearing and looks like an omission: a property load of `current` off a value
// that is a ref is deliberately not aliased. That is what keeps `ref` and `ref.current` distinct,
// which is the entire distinction between "you passed a ref somewhere" and "you read a ref's
// value". Aliasing them would collapse two of the four diagnostics into one.
func refsCollectTemporaries(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
) {
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := refsInstructionAt(function, instructionId)
			if instruction == nil {
				continue
			}
			env.noteDeclaration(function, instruction.LValue.Identifier)
			high_level_intermediate_representation.EachInstructionPlace(instruction, func(
				place high_level_intermediate_representation.Place,
				role high_level_intermediate_representation.PlaceRole,
			) {
				env.noteDeclaration(function, place.Identifier)
			})
			switch instructionValue := instruction.Value.(type) {
			case *high_level_intermediate_representation.LoadLocal:
				env.setName(function, instruction.LValue.Identifier, instructionValue.Place.Identifier)
				env.define(instruction.LValue.Identifier, instructionValue.Place.Identifier)
			// There is deliberately no LoadContext arm here, and it is worth saying why, because
			// adding one is the obvious move and it was in this file until a mutant proved it
			// inert.
			//
			// A captured binding is read through LoadContext rather than LoadLocal, so aliasing it
			// here looks necessary for `const aliasedRef = ref; aliasedRef.current` inside a
			// closure. It is not: the declaration fallback in `get` already recovers that case,
			// because both numberings of the alias carry the same DeclarationId. Measured three
			// ways: deleting this arm alone changes nothing across the whole suite, deleting the
			// fallback alone changes nothing, and deleting both turns
			// `error.invalid-aliased-ref-in-callback-invoked-during-render-` silent. Two mechanisms
			// covering one case is one mechanism plus a thing that will drift, so the redundant one
			// is gone. Upstream has no LoadContext arm here either.
			case *high_level_intermediate_representation.StoreLocal:
				env.define(instruction.LValue.Identifier, instructionValue.Value.Identifier)
				env.define(instructionValue.LValue.Identifier, instructionValue.Value.Identifier)
			case *high_level_intermediate_representation.LoadGlobal:
				// A hook is called through a temporary that a LoadGlobal produced, so the callee's
				// own Name is empty and a hook test keyed on it never fires. Recording the global's
				// name here is what makes `useEffect(...)` recognizable as a hook call at all.
				// Without it every hook call is scored as an ordinary function call and a ref passed
				// to one reports spuriously, which was five of this corpus's residuals.
				env.setName2(instruction.LValue.Identifier, instructionValue.Name)
			case *high_level_intermediate_representation.PropertyLoad:
				// The loaded property names the result, which is what carries `props.ref` into the
				// name test. Upstream reaches the same place by unifying a nested `Type::Property`,
				// whose `object_name` is resolved when the outer `.current` unifies against it
				// (`infer_types.rs:1353`); recording the property name here is that resolution
				// performed eagerly, and it is what makes `props.ref.current` report while
				// `props.fooRef.current` stays silent, because only the former's inner name is a
				// ref-like name that upstream's regex accepts in this position.
				env.setPropertyName(instruction.LValue.Identifier, instructionValue.Property)
				if instructionValue.Property == "current" &&
					refsIsRefBinding(ruleContext, function, env, instructionValue.Object.Identifier) {
					continue
				}
				// A load whose result is itself a ref is not aliased to its object either, for the
				// same reason the `current` case above is not: the alias would make the object and
				// the field indistinguishable, and the object here is a props bag rather than a ref.
				//
				// Without this, reading one ref-typed field marks the whole object as a ref, and
				// every other field read off it then reports as a ref access. Measured on Kirk's
				// tree: an interface carrying two refs beside a `currentWidth: number` and a
				// `columnId: string` produced six findings in one file, all on the non-ref fields.
				// Upstream never has this problem because it types each property independently
				// rather than aliasing through the object.
				if refsIsUseRefType(ruleContext, function, instruction.LValue.Identifier) {
					continue
				}
				env.define(instruction.LValue.Identifier, instructionValue.Object.Identifier)
			}
		}
	}
}

// refsIsRefBinding is the two-signal ref test: the checker's type or the binding's name.
//
// Both are required and each covers a part of the corpus the other cannot. See the rule comment for
// the measurements.
//
// # The name must be read through the aliasing side map, and this is the trap
//
// The name test asks for the binding name of the value whose `current` is being accessed, and the
// obvious way to ask that is to read `Identifier.Name` off the property load's object. That is
// wrong and it is silently wrong: lowering emits `LoadLocal ref -> t13` and then
// `PropertyLoad t13.current`, so the object is the temporary, whose Name is empty, while the name
// lives on the source identifier the temporary loaded from. Asked directly, every `props.ref` case
// declines and looks exactly like a rule that simply does not implement them.
//
// Measured by dumping the representation for all three shapes rather than inferred: for
// `props.ref.current` the object is `t11` with an empty name, and `t11` resolves through the
// temporaries map to the value produced by the `ref` property load. `refsEnvironment.operandId` is
// upstream's `Env::operand_id` and is the resolution that recovers it.
func refsIsRefBinding(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	id static_single_assignment.IdentifierId,
) bool {
	if refsIsUseRefType(ruleContext, function, id) {
		return true
	}
	if refsIsRefLikeName(refsIdentifierName(function, id)) {
		return true
	}
	if env == nil {
		return false
	}
	if refsIsRefLikeName(env.nameOf(function, id)) {
		return true
	}
	// A property load names its result by the property read, but only the exact name `ref` is
	// honoured here rather than the full regex. Measured on the executable: `props.ref.current`
	// reports while `props.fooRef.current` is clean, so the wider regex is wrong in this position
	// even though it is right for a binding. Reproduced as measured rather than as read.
	if env.propertyNames[id] == "ref" {
		return true
	}
	resolved := env.operandId(id)
	return resolved != id && refsIsUseRefType(ruleContext, function, resolved)
}

// refsRunOneRound is one pass over every block, phi and instruction.
//
// Returns the findings this round produced. Findings are recomputed each round rather than
// accumulated, because the same instruction is visited on every round and an accumulating list
// would report each finding up to ten times.
func refsRunOneRound(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
) []refsFinding {
	var findings []refsFinding

	// Parameters are seeded from their declared type. A parameter that is a ref by type or by name
	// enters as one, which is how `function Component({ref})` reports with no `useRef` in sight.
	for _, parameter := range function.Parameters {
		env.set(parameter.Identifier, refsSeedType(ruleContext, function, parameter.Identifier, env))
	}

	// A value interpolated as a JSX child is checked more leniently than one passed to a function:
	// `<div>{value}</div>` where value came out of a ref reports, but a function returning a ref is
	// allowed to be rendered. Gathered once per round because a use can precede its definition
	// across a back edge.
	interpolatedAsJsx := map[static_single_assignment.IdentifierId]bool{}
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := refsInstructionAt(function, instructionId)
			if instruction == nil {
				continue
			}
			switch value := instruction.Value.(type) {
			case *high_level_intermediate_representation.JsxExpression:
				for _, child := range value.Children {
					interpolatedAsJsx[child.Identifier] = true
				}
			case *high_level_intermediate_representation.JsxFragment:
				for _, child := range value.Children {
					interpolatedAsJsx[child.Identifier] = true
				}
			}
		}
	}

	// safeBlocks records the fallthrough of an `if` whose test is a guard, so a write to the guarded
	// ref inside that region is permitted. This is what makes the `if (r.current == null) { r.current
	// = 1; }` initialization idiom clean while a bare write reports.
	var safeBlocks []refsSafeBlock

	for _, block := range function.Blocks {
		// Entering a block retires any safe region registered for it: the guard's protection ends
		// where the branch rejoins.
		filtered := safeBlocks[:0]
		for _, safe := range safeBlocks {
			if safe.Block != block.Id {
				filtered = append(filtered, safe)
			}
		}
		safeBlocks = filtered

		for _, phi := range block.Phis {
			operandTypes := make([]*refsAccessType, 0, len(phi.Operands))
			for _, entry := range phi.Operands {
				operandTypes = append(operandTypes, env.get(entry.Place.Identifier))
			}
			env.set(phi.Place.Identifier, refsJoinMany(operandTypes, env.nextRefId))
		}

		for _, instructionId := range block.Instructions {
			instruction := refsInstructionAt(function, instructionId)
			if instruction == nil {
				continue
			}
			findings = refsTransfer(ruleContext, function, env, instruction, interpolatedAsJsx, findings, &safeBlocks)

			// A guard value may only be consumed by an `if` test. Using one anywhere else means the
			// code read `ref.current` for its value rather than to check it against null.
			//
			// Operands only. `EachInstructionPlace` also yields the instruction's own LValue, and
			// including it makes the guard report itself the moment it is created: the binary
			// expression that produces the guard has that guard as its lvalue, so the check fires on
			// the very instruction implementing `ref.current == null` and the sanctioned idiom
			// reports. Upstream walks `each_instruction_value_operand`, which is the value's
			// operands and not the lvalue. Measured: with the lvalue included,
			// `if (ref.current == null) { ref.current = 1; }` reports once and is clean upstream.
			high_level_intermediate_representation.EachPlace(instruction.Value, func(
				place high_level_intermediate_representation.Place,
				role high_level_intermediate_representation.PlaceRole,
			) {
				if current := env.get(place.Identifier); current != nil && current.Kind == refsGuard {
					findings = append(
						findings,
						refsFinding{Kind: refsFindingValueAccess, Value: place.Identifier, HasValue: true},
					)
				}
			})

			// Whatever the transfer decided, a value the checker calls a ref is a ref. This is
			// upstream's post-pass and it is what catches a ref arriving through a shape the
			// instruction switch does not model.
			refsApplyDeclaredType(ruleContext, function, env, instruction.LValue.Identifier)
		}

		if branch, isIf := block.Terminal.(*high_level_intermediate_representation.If); isIf {
			if test := env.get(branch.Test.Identifier); test != nil && test.Kind == refsGuard && test.HasRefId {
				alreadyRegistered := false
				for _, safe := range safeBlocks {
					if safe.RefId == test.RefId {
						alreadyRegistered = true
					}
				}
				if !alreadyRegistered {
					safeBlocks = append(safeBlocks, refsSafeBlock{Block: branch.Fallthrough, RefId: test.RefId})
				}
			}
		}

		_, isReturn := block.Terminal.(*high_level_intermediate_representation.Return)
		high_level_intermediate_representation.EachTerminalPlace(block.Terminal, func(
			place high_level_intermediate_representation.Place,
			role high_level_intermediate_representation.PlaceRole,
		) {
			if isReturn {
				// Returning a ref object is allowed; returning a ref value is not. That asymmetry is
				// what makes `return ref;` clean in a hook while `return ref.current;` reports.
				findings = refsCheckDirectValueAccess(env, place.Identifier, findings)
				return
			}
			findings = refsCheckValueAccess(env, place.Identifier, findings)
		})
	}

	return findings
}

// refsSeedType is a value's lattice element as decided by the checker and the name alone.
//
// Consulted whenever the instruction switch has nothing better to say. A value the checker types as
// `RefObject` enters as a ref; everything else enters as none.
func refsSeedType(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
	env *refsEnvironment,
) *refsAccessType {
	if refsIsRefBinding(ruleContext, function, env, id) {
		return &refsAccessType{Kind: refsReference, RefId: env.nextRefId(), HasRefId: true}
	}
	return &refsAccessType{Kind: refsNone}
}

// refsApplyDeclaredType widens a value to a ref when the checker says it is one.
//
// Upstream runs this after every instruction, so a ref reaching a value through a shape the switch
// does not model is still recognized. Skipped when the value is already a ref, so the join does not
// mint an id on every round and stall the fixpoint.
func refsApplyDeclaredType(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	id static_single_assignment.IdentifierId,
) {
	if !refsIsRefBinding(ruleContext, function, env, id) {
		return
	}
	current := env.get(id)
	if current != nil && current.Kind == refsReference {
		return
	}
	// A value already known to be a function is never widened into a ref by the name test. The
	// name heuristic is a guess and this is not: a closure named `setRef` is a function whose body
	// was just analysed, and letting `setRef` match the `...Ref$` regex would discard that analysis
	// and score the call as passing a ref rather than as calling a ref-reading function.
	//
	// Measured on the executable, because the collision is not hypothetical: a closure named
	// `setRef` and one named `doIt` both report, so upstream does not let the name win here either.
	// Three of this corpus's fixtures are exactly this shape and all three went silent before this
	// guard existed.
	if current != nil && current.Kind == refsStructure && current.Function != nil {
		return
	}
	env.set(id, &refsAccessType{Kind: refsReference, RefId: env.nextRefId(), HasRefId: true})
}

// refsCheckDirectValueAccess reports only a bare ref value, not a function that reads one.
//
// The looser of the two read checks. Used where a function carrying a ref is legitimately allowed
// to be there, such as a return value or a JSX child.
func refsCheckDirectValueAccess(
	env *refsEnvironment,
	id static_single_assignment.IdentifierId,
	findings []refsFinding,
) []refsFinding {
	current := env.get(id)
	if current == nil {
		return findings
	}
	if refsDestructure(current).Kind == refsRefValue {
		return append(
			findings,
			refsFinding{Kind: refsFindingValueAccess, Value: id, HasValue: true, Node: env.accessNodes[id]},
		)
	}
	return findings
}

// refsCheckValueAccess reports a bare ref value or a function whose body reads one.
func refsCheckValueAccess(
	env *refsEnvironment,
	id static_single_assignment.IdentifierId,
	findings []refsFinding,
) []refsFinding {
	current := env.get(id)
	if current == nil {
		return findings
	}
	resolved := refsDestructure(current)
	if resolved.Kind == refsRefValue {
		return append(
			findings,
			refsFinding{Kind: refsFindingValueAccess, Value: id, HasValue: true, Node: env.accessNodes[id]},
		)
	}
	if resolved.Kind == refsStructure && resolved.Function != nil && resolved.Function.ReadRefEffect {
		return append(
			findings,
			refsFinding{Kind: refsFindingValueAccess, Value: id, HasValue: true, Node: env.accessNodes[id]},
		)
	}
	return findings
}

// refsCheckPassedToFunction reports handing a ref, or a ref-reading function, to a call.
func refsCheckPassedToFunction(
	env *refsEnvironment,
	id static_single_assignment.IdentifierId,
	findings []refsFinding,
) []refsFinding {
	current := env.get(id)
	if current == nil {
		return findings
	}
	resolved := refsDestructure(current)
	switch {
	case resolved.Kind == refsReference || resolved.Kind == refsRefValue:
		return append(findings, refsFinding{Kind: refsFindingPassedToFunction, Value: id, HasValue: true})
	case resolved.Kind == refsStructure && resolved.Function != nil && resolved.Function.ReadRefEffect:
		return append(findings, refsFinding{Kind: refsFindingPassedToFunction, Value: id, HasValue: true})
	}
	return findings
}

// refsCheckUpdate reports writing through a ref during render.
func refsCheckUpdate(
	env *refsEnvironment,
	id static_single_assignment.IdentifierId,
	node *ast.Node,
	findings []refsFinding,
) []refsFinding {
	current := env.get(id)
	if current == nil {
		return findings
	}
	resolved := refsDestructure(current)
	if resolved.Kind == refsReference || resolved.Kind == refsRefValue {
		return append(findings, refsFinding{Kind: refsFindingUpdate, Value: id, HasValue: true, Node: node})
	}
	return findings
}

// refsTransfer applies one instruction to the environment and collects any findings it produces.
//
// The arms are upstream's, in upstream's order. The default arm is the conservative one: any
// instruction not modelled here checks every operand for a ref value, so a construct this port does
// not know about fails toward reporting rather than toward silence.
func refsTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	instruction *high_level_intermediate_representation.Instruction,
	interpolatedAsJsx map[static_single_assignment.IdentifierId]bool,
	findings []refsFinding,
	safeBlocks *[]refsSafeBlock,
) []refsFinding {
	collected := findings
	target := instruction.LValue.Identifier

	switch instructionValue := instruction.Value.(type) {
	// Rendering a ref value is a read. A function that merely carries a ref may be rendered, which
	// is why this is the direct check rather than the general one.
	case *high_level_intermediate_representation.JsxExpression, *high_level_intermediate_representation.JsxFragment:
		high_level_intermediate_representation.EachPlace(instruction.Value, func(
			place high_level_intermediate_representation.Place,
			role high_level_intermediate_representation.PlaceRole,
		) {
			collected = refsCheckDirectValueAccess(env, place.Identifier, collected)
		})

	// Reading a property off a ref produces a ref value, which is the element the whole rule is
	// about. Reading a property off a structure yields what the structure carries.
	case *high_level_intermediate_representation.PropertyLoad:
		collected = refsPropertyLoadTransfer(
			ruleContext,
			function,
			env,
			instruction,
			instructionValue.Object.Identifier,
			target,
			collected,
		)

	case *high_level_intermediate_representation.ComputedLoad:
		collected = refsCheckDirectValueAccess(env, instructionValue.Property.Identifier, collected)
		collected = refsPropertyLoadTransfer(
			ruleContext,
			function,
			env,
			instruction,
			instructionValue.Object.Identifier,
			target,
			collected,
		)

	// A cast is a no-op at runtime and carries its operand's element through unchanged.
	case *high_level_intermediate_representation.TypeCastExpression:
		env.set(target, refsCarryOrSeed(ruleContext, function, env, instructionValue.Value.Identifier, target))

	case *high_level_intermediate_representation.LoadLocal:
		env.carryAccessNode(target, instructionValue.Place.Identifier)
		env.set(target, refsCarryOrSeed(ruleContext, function, env, instructionValue.Place.Identifier, target))

	case *high_level_intermediate_representation.LoadContext:
		env.carryAccessNode(target, instructionValue.Place.Identifier)
		env.set(target, refsCarryOrSeed(ruleContext, function, env, instructionValue.Place.Identifier, target))

	case *high_level_intermediate_representation.StoreLocal:
		stored := refsCarryOrSeed(
			ruleContext,
			function,
			env,
			instructionValue.Value.Identifier,
			instructionValue.LValue.Identifier,
		)
		env.carryAccessNode(instructionValue.LValue.Identifier, instructionValue.Value.Identifier)
		env.carryAccessNode(target, instructionValue.Value.Identifier)
		env.set(instructionValue.LValue.Identifier, stored)
		env.set(target, refsCarryOrSeed(ruleContext, function, env, instructionValue.Value.Identifier, target))
		// Pin the value under the written binding's declaration. `env.set` records against whatever
		// the lvalue resolves to through the aliasing map, which for a store into a fresh binding is
		// an unnamed temporary carrying no declaration, so the entry the declaration fallback needs
		// would never be written. See `get` for why that fallback exists at all.
		env.noteDeclaration(function, instructionValue.LValue.Identifier)
		if declaration, ok := env.declarations[instructionValue.LValue.Identifier]; ok && stored.Kind != refsNone {
			env.byDeclaration[declaration] = stored
		}

	case *high_level_intermediate_representation.StoreContext:
		env.set(
			instructionValue.LValue.Identifier,
			refsCarryOrSeed(
				ruleContext,
				function,
				env,
				instructionValue.Value.Identifier,
				instructionValue.LValue.Identifier,
			),
		)
		env.set(target, refsCarryOrSeed(ruleContext, function, env, instructionValue.Value.Identifier, target))

	// Destructuring distributes what the source structure carries to every bound name, which is how
	// `const {current} = ref` reaches the same verdict as `ref.current`.
	case *high_level_intermediate_representation.Destructure:
		var carried *refsAccessType
		if source := env.get(
			instructionValue.Value.Identifier,
		); source != nil && source.Kind == refsStructure && source.Value != nil {
			carried = source.Value
		}
		if carried == nil {
			carried = refsSeedType(ruleContext, function, target, env)
		}
		env.set(target, carried)
		high_level_intermediate_representation.EachPlace(instruction.Value, func(
			place high_level_intermediate_representation.Place,
			role high_level_intermediate_representation.PlaceRole,
		) {
			if role == high_level_intermediate_representation.PlaceRoleDefine {
				env.set(place.Identifier, carried)
			}
		})

	// A nested function is analysed as an inner subject and its verdict is recorded on the value.
	// A ref touched inside it does not report there; it reports where the function is called.
	case *high_level_intermediate_representation.FunctionExpression:
		collected = refsNestedFunctionTransfer(
			ruleContext,
			function,
			env,
			instruction,
			instructionValue,
			target,
			collected,
		)

	case *high_level_intermediate_representation.CallExpression:
		collected = refsCallTransfer(
			ruleContext,
			function,
			env,
			instruction,
			instructionValue.Callee.Identifier,
			interpolatedAsJsx,
			collected,
		)

	// A method call keeps its receiver, so `object.foo()` arrives here rather than as a property
	// load plus a call. The callee to score is the property, but the property is a fresh temporary
	// with no lattice entry of its own: what carries the function is the receiver's structure. So
	// the property is seeded from the receiver first, and only then scored.
	//
	// Without that seeding a closure stored on an object and called through it scores as an
	// ordinary call with a ref-carrying argument, which reports the wrong one of the four
	// diagnostics. Two corpus fixtures are this shape and both said `refPassedToFunction` where
	// upstream says `functionAccessesRef`.
	case *high_level_intermediate_representation.MethodCall:
		// `React.useEffect(...)` is a method call, so the hook's name is the property rather than a
		// binding, and the hook test cannot see it without this. Recording it is what makes the
		// namespaced spelling behave like the bare import.
		//
		// This was not a fixture failure and could not have been: every corpus fixture calls hooks
		// bare. It was 235 findings on Kirk's tree, essentially all of them a ref touched inside a
		// `React.useEffect` or `React.useCallback` callback, which upstream is silent on (measured
		// on both shapes). A rule scoring 30 of 30 was reporting a false positive in almost every
		// React file in the repository.
		if refsMethodName(function, instruction) != "" {
			env.setName2(instructionValue.Property.Identifier, refsMethodName(function, instruction))
		}
		if receiverType := env.get(instructionValue.Receiver.Identifier); receiverType != nil &&
			receiverType.Kind == refsStructure && receiverType.Function != nil {
			env.set(instructionValue.Property.Identifier, receiverType)
		}
		collected = refsCallTransfer(
			ruleContext,
			function,
			env,
			instruction,
			instructionValue.Property.Identifier,
			interpolatedAsJsx,
			collected,
		)

	// An aggregate becomes a structure carrying the join of what its elements carry, so a ref put
	// into an object is still findable through the object.
	case *high_level_intermediate_representation.ObjectExpression,
		*high_level_intermediate_representation.ArrayExpression:
		var elementTypes []*refsAccessType
		high_level_intermediate_representation.EachPlace(instruction.Value, func(
			place high_level_intermediate_representation.Place,
			role high_level_intermediate_representation.PlaceRole,
		) {
			// An aggregate feeding a call whose result is a ref is the mergeRefs shape, and
			// collecting refs into a list is the entire point of it. Upstream reaches the same
			// silence through the aliasing effects of the call rather than at the aggregate, so the
			// exemption is expressed here instead and this is a divergence in mechanism that agrees
			// in outcome.
			//
			// Measured: `mergeReferences([inputReference, ref])` is clean upstream, and it was 8
			// findings on Kirk's tree, one in every form-field component that forwards a ref. No
			// corpus fixture merges refs, so nothing imported can see this either way.
			// The exemption covers a ref object only, never a ref value. Collecting refs into a
			// list or an options object and handing it to a hook is ordinary React; reading
			// `ref.current` into that same object is a render-time read whatever the object is for.
			//
			// Measured with three inputs on the executable: `useThing({r: r})` is clean,
			// `useThing({v: r.current})` reports, and `useEffect(() => {}, [ref.current])` reports
			// twice. The corpus fixture `error.hook-ref-value` is that third shape and it is the
			// test that caught this exemption when it was written one step too wide.
			isRefObject := false
			if current := env.get(place.Identifier); current != nil {
				isRefObject = refsDestructure(current).Kind == refsReference
			}
			if !isRefObject || !refsAggregateIsExempt(ruleContext, function, env, instruction) {
				collected = refsCheckDirectValueAccess(env, place.Identifier, collected)
			}
			elementTypes = append(elementTypes, env.get(place.Identifier))
		})
		joined := refsJoinMany(elementTypes, env.nextRefId)
		switch joined.Kind {
		case refsNone, refsGuard, refsNullable:
			env.set(target, &refsAccessType{Kind: refsNone})
		default:
			env.set(target, &refsAccessType{Kind: refsStructure, Value: joined})
		}

	case *high_level_intermediate_representation.PropertyStore:
		collected = refsStoreTransfer(
			env,
			instruction,
			instructionValue.Object.Identifier,
			&instructionValue.Value,
			nil,
			collected,
			safeBlocks,
		)

	case *high_level_intermediate_representation.ComputedStore:
		collected = refsStoreTransfer(
			env,
			instruction,
			instructionValue.Object.Identifier,
			&instructionValue.Value,
			&instructionValue.Property,
			collected,
			safeBlocks,
		)

	case *high_level_intermediate_representation.PropertyDelete:
		collected = refsCheckUpdate(env, instructionValue.Object.Identifier, instruction.Node, collected)

	case *high_level_intermediate_representation.ComputedDelete:
		collected = refsCheckUpdate(env, instructionValue.Object.Identifier, instruction.Node, collected)
		collected = refsCheckValueAccess(env, instructionValue.Property.Identifier, collected)

	// `undefined` and `null` are nullable, which is only interesting because a comparison against
	// one of them is what turns a ref value into a guard.
	case *high_level_intermediate_representation.LoadGlobal:
		if instructionValue.Name == "undefined" {
			env.set(target, &refsAccessType{Kind: refsNullable})
		}

	case *high_level_intermediate_representation.Primitive:
		switch instructionValue.Value.(type) {
		case high_level_intermediate_representation.PrimitiveNull,
			high_level_intermediate_representation.PrimitiveUndefined:
			env.set(target, &refsAccessType{Kind: refsNullable})
		}

	// `!ref.current` is both a read and a guard: it reports, and it marks the result so the write it
	// authorizes does not report a second time on the same line.
	case *high_level_intermediate_representation.UnaryExpression:
		if instructionValue.Operator == "!" {
			if operand := env.get(
				instructionValue.Value.Identifier,
			); operand != nil && operand.Kind == refsRefValue && operand.HasRefId {
				env.set(target, &refsAccessType{Kind: refsGuard, RefId: operand.RefId, HasRefId: true})
				collected = append(
					collected,
					refsFinding{Kind: refsFindingValueAccess, Value: instructionValue.Value.Identifier, HasValue: true},
				)
				break
			}
		}
		collected = refsCheckValueAccess(env, instructionValue.Value.Identifier, collected)

	// Comparing a ref value against null produces a guard rather than a finding. Comparing it
	// against anything else is an ordinary read and reports.
	case *high_level_intermediate_representation.BinaryExpression:
		collected = refsBinaryTransfer(env, instruction, instructionValue, target, collected)

	case *high_level_intermediate_representation.StartMemoize, *high_level_intermediate_representation.FinishMemoize:
		// Markers. Nothing to do, and named explicitly so they do not fall into the default arm's
		// operand check, which would report a memoized ref.

	default:
		high_level_intermediate_representation.EachPlace(instruction.Value, func(
			place high_level_intermediate_representation.Place,
			role high_level_intermediate_representation.PlaceRole,
		) {
			collected = refsCheckValueAccess(env, place.Identifier, collected)
		})
	}

	return collected
}

// refsCallResultIsReference asks the checker what a call yields, through the call's own syntax.
//
// `refsIsRefBinding` reaches the checker through `Identifier.Node`, which a call's temporary lvalue
// does not have. This asks the same question of the call expression instead.
func refsCallResultIsReference(
	ruleContext rule.Context,
	instruction *high_level_intermediate_representation.Instruction,
) bool {
	if ruleContext.TypeChecker == nil || instruction == nil || instruction.Node == nil {
		return false
	}
	node := instruction.Node
	if node.Kind != ast.KindCallExpression {
		return false
	}
	resultType := ruleContext.TypeChecker.GetTypeAtLocation(node)
	if resultType == nil {
		return false
	}
	if alias := shimchecker.Type_alias(resultType); alias != nil {
		if aliasSymbol := alias.Symbol(); aliasSymbol != nil && aliasSymbol.Name == "RefCallback" {
			return true
		}
	}
	if symbol := shimchecker.Type_symbol(resultType); symbol != nil {
		// `bivarianceHack` is what the real `@types/react` `RefCallback` resolves to, and matching it
		// is not a workaround on our side.
		//
		// `RefCallback<T>` is declared as an indexed access over an object type whose single member
		// is named `bivarianceHack` (`@types/react/index.d.ts:176-185`), the standard idiom for
		// opting one member into bivariant parameter checking. Resolving that indexed access erases
		// the alias and leaves the method's own symbol, so the type arrives with a nil alias and the
		// symbol `bivarianceHack`.
		//
		// Measured on Kirk's tree rather than reasoned about, and a hand-written stub does not
		// reproduce it: a fixture declaring `type RefCallback<T> = (i: T) => void` carries the alias
		// and passes, which is precisely the failure where code and fixtures share one wrong belief.
		// The eight findings this silenced were all `mergeReferences([inner, ref])`, one in every
		// form-field component that forwards a ref, and all clean upstream.
		return symbol.Name == "RefObject" || symbol.Name == "MutableRefObject" ||
			symbol.Name == "bivarianceHack"
	}
	return false
}

// refsAggregateIsExempt reports whether an aggregate is consumed by a call that may hold refs.
//
// Asked at the aggregate rather than at the call, because an argument list is evaluated first and
// would otherwise have already reported by the time the call's own exemptions are considered. Two
// consumers qualify, and both are upstream's:
//
//   - a call whose result is a ref, which is the mergeRefs shape
//   - a hook call, which upstream exempts because a hook is independently validated
//
// Both were measured on the executable with live controls rather than inferred. Collecting refs
// into an object and handing it to `useTableColumnResize` is clean; handing the identical object to
// a plain `doThing` reports; and a plain `useRef` read during render still reports, which is the
// control proving the probe could see a finding at all.
//
// The state hooks are deliberately not exempt here, for the same reason they are not at the call:
// `useState`, `useReducer` and `useMemo` run what they are given during render.
func refsAggregateIsExempt(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	instruction *high_level_intermediate_representation.Instruction,
) bool {
	aggregate := instruction.LValue.Identifier
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			candidate := refsInstructionAt(function, instructionId)
			if candidate == nil || candidate.Id <= instruction.Id {
				continue
			}
			var arguments []high_level_intermediate_representation.Argument
			switch call := candidate.Value.(type) {
			case *high_level_intermediate_representation.CallExpression:
				arguments = call.Arguments
			case *high_level_intermediate_representation.MethodCall:
				arguments = call.Arguments
			default:
				continue
			}
			for _, argument := range arguments {
				if env.operandId(argument.Place.Identifier) != env.operandId(aggregate) {
					continue
				}
				if refsIsRefBinding(ruleContext, function, env, candidate.LValue.Identifier) ||
					refsCallResultIsReference(ruleContext, candidate) {
					return true
				}
				calleeName := ""
				switch call := candidate.Value.(type) {
				case *high_level_intermediate_representation.CallExpression:
					calleeName = env.nameOf(function, call.Callee.Identifier)
				case *high_level_intermediate_representation.MethodCall:
					calleeName = refsMethodName(function, candidate)
				}
				if refsIsHookName(calleeName) && calleeName != "useState" &&
					calleeName != "useReducer" && calleeName != "useMemo" {
					return true
				}
			}
		}
	}
	return false
}

// refsCarryOrSeed passes an operand's element through, falling back to its declared type.
func refsCarryOrSeed(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	from static_single_assignment.IdentifierId,
	to static_single_assignment.IdentifierId,
) *refsAccessType {
	if current := env.get(from); current != nil {
		return current
	}
	return refsSeedType(ruleContext, function, to, env)
}

// refsPropertyLoadTransfer decides what reading a property off a value yields.
//
// Off a ref, it is a ref value carrying where the access happened. Off a structure, it is whatever
// the structure carries. Otherwise it falls back to the declared type.
func refsPropertyLoadTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	instruction *high_level_intermediate_representation.Instruction,
	object static_single_assignment.IdentifierId,
	target static_single_assignment.IdentifierId,
	findings []refsFinding,
) []refsFinding {
	objectType := env.get(object)
	switch {
	// A structure carrying a function hands that function out. Without this arm a closure stored on
	// an object property and then called through it loses its `readRefEffect`, and the call scores
	// as passing a ref rather than as calling a ref-reading function. Two corpus fixtures are this
	// exact shape (`object.foo = () => ref.current; object.foo()`), and upstream reaches it because
	// its structure carries the function in the same slot the value uses.
	case objectType != nil && objectType.Kind == refsStructure && objectType.Function != nil && objectType.Value == nil:
		env.set(target, objectType)
	case objectType != nil && objectType.Kind == refsStructure && objectType.Value != nil:
		env.set(target, objectType.Value)
	case objectType != nil && objectType.Kind == refsReference:
		if instruction.Node != nil {
			env.accessNodes[target] = instruction.Node
		}
		env.set(target, &refsAccessType{
			Kind:       refsRefValue,
			Span:       target,
			HasSpan:    true,
			RefSpan:    object,
			HasRefSpan: true,
			RefId:      objectType.RefId,
			HasRefId:   objectType.HasRefId,
		})
	default:
		env.set(target, refsSeedType(ruleContext, function, target, env))
	}
	return findings
}

// refsNestedFunctionTransfer analyses an inner function and records its verdict on the value.
//
// This is what makes a ref touched inside a callback report at the call rather than at the touch.
// `capture-ref-for-mutation` is the shipped fixture: the ref is read inside `handleKey`, and the
// finding lands on `handleKey('left')()` because that is where the reading actually happens during
// render.
//
// The inner sweep shares the outer environment rather than starting fresh, which is upstream's
// choice and is what lets a captured ref be visible inside the nested function at all.
//
// Mutates: env advances its ref id seed past every id the nested function minted, so ids never collide.
func refsNestedFunctionTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	instruction *high_level_intermediate_representation.Instruction,
	value *high_level_intermediate_representation.FunctionExpression,
	target static_single_assignment.IdentifierId,
	findings []refsFinding,
) []refsFinding {
	inner := refsNestedFunction(function, value.Function)
	if inner == nil {
		env.set(target, &refsAccessType{Kind: refsNone})
		return findings
	}

	// The inner function gets its own environment, and this is the single most load-bearing line in
	// the nested analysis.
	//
	// Identifier ids are per-function and they overlap. Measured by dumping the representation for
	// the corpus's `capture-ref-for-mutation` fixture: the outer function uses ids 18 through 35 and
	// the innermost closure independently uses 20 through 37. Sweeping the inner function into the
	// outer environment therefore conflates two unrelated values at every shared id, and the failure
	// is not a dropped finding but a wrong one: the inner `pos`, a ref value, lands on an outer id
	// that the outer function then passes to a call, so the rule reported `refPassedToFunction` in a
	// function that never passed a ref anywhere. Upstream reports once at the outer call site and
	// nothing at the inner read, measured on the executable.
	//
	// This is the trap the porting brief names as "a value identifier does not survive a function
	// boundary", and it arrives here in its most misleading form: the id is valid in both spaces, so
	// nothing is nil, nothing panics, and every message-id fixture that does not involve a nested
	// closure stays green.
	innerEnvironment := newRefsEnvironment()
	innerEnvironment.referenceIdSeed = env.referenceIdSeed

	// A captured value must be translated into the inner function's identifier space before the
	// inner sweep can see it. `Function.Context` is the capture edge that performs the translation:
	// the Nth capture at the call site corresponds to the Nth context value inside.
	for index, captured := range value.Captures {
		if index >= len(inner.Context) {
			continue
		}
		contextId := inner.Context[index].Identifier
		if outerType := env.get(captured.Identifier); outerType != nil {
			innerEnvironment.data[contextId] = outerType
		}
		// The captured value's name crosses the boundary too. Without it a ref that the outer
		// function knows only by name rather than by type arrives inside as an anonymous context
		// value, and the name signal cannot fire on the alias chain the closure builds from it.
		// `error.invalid-aliased-ref-in-callback-invoked-during-render-` is exactly that shape:
		// `const aliasedRef = ref; aliasedRef.current` inside a callback.
		if name := env.nameOf(function, captured.Identifier); name != "" {
			innerEnvironment.names[contextId] = name
		}
	}

	refsCollectTemporaries(ruleContext, inner, innerEnvironment)
	innerFindings := refsRunOneRound(ruleContext, inner, innerEnvironment)
	// Ref identity is minted from a single counter so two functions cannot collide on an id; the
	// seed travels back out rather than the whole environment.
	env.referenceIdSeed = innerEnvironment.referenceIdSeed
	functionType := &refsFunctionType{ReturnType: &refsAccessType{Kind: refsNone}}
	if len(innerFindings) == 0 {
		if returned := innerEnvironment.get(refsReturnedValue(inner)); returned != nil {
			functionType.ReturnType = returned
		}
	} else {
		functionType.ReadRefEffect = true
		for _, finding := range innerFindings {
			if finding.HasValue {
				functionType.RefAccessSpan, functionType.HasRefAccessSpan = finding.Value, true
				break
			}
		}
	}
	env.set(target, &refsAccessType{Kind: refsStructure, Function: functionType})
	return findings
}

// refsReturnedValue is the identifier a function actually yields.
//
// `Function.Returns` is the documented seam and it is not sufficient on its own: lowering populates
// it only from an explicit `return` statement (`lower.go:596`), while a concise arrow body
// terminates with `Return{Value: value}` and leaves `Returns` an empty temporary
// (`lower.go:164-166`). So `d => () => {...}` yields nothing through `Returns`, and a rule reading
// only that field loses the curried closure entirely.
//
// Measured rather than reasoned: dumping the representation for the corpus's curried fixture shows
// `handleKey`'s `Returns` as id 4 while its `FunctionExpression` writes to id 5. Reading the
// terminal recovers the real value, and `Returns` stays the fallback for the explicit-return case
// where the terminal names it anyway.
//
// This is a gap in lowering rather than in this rule, and it is worked around here rather than
// repaired there because a change to `Returns` would move every pass built on this representation
// at once. It deserves a fix upstream of this file; see the report.
func refsReturnedValue(
	function *high_level_intermediate_representation.Function,
) static_single_assignment.IdentifierId {
	for _, block := range function.Blocks {
		if terminal, isReturn := block.Terminal.(*high_level_intermediate_representation.Return); isReturn {
			return terminal.Value.Identifier
		}
	}
	return function.Returns.Identifier
}

// refsMethodName is the property being called in a method call, read from the syntax.
//
// The representation gives a method call its receiver and a Place for the property, but the
// property's name lives on the syntax rather than on the value, so it is read back from the node.
func refsMethodName(
	function *high_level_intermediate_representation.Function,
	instruction *high_level_intermediate_representation.Instruction,
) string {
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

// refsNestedFunction resolves a function id within its parent.
func refsNestedFunction(
	function *high_level_intermediate_representation.Function,
	id high_level_intermediate_representation.FunctionId,
) *high_level_intermediate_representation.Function {
	if function == nil || int(id) >= len(function.Functions) {
		return nil
	}
	return function.Functions[id]
}

// refsCallTransfer decides what calling a value does, and it is the busiest arm.
//
// Three questions in order: does the callee itself read a ref (report at the callee), is the result
// a ref or is this a hook (then only direct ref values in the arguments matter), and otherwise does
// passing these arguments risk a read.
//
// The hook exemption is upstream's and is narrow: a hook call is independently validated, so a ref
// passed to one is allowed, except for `useState` and `useReducer`, whose initializers run during
// render and so genuinely would read the ref then.
func refsCallTransfer(
	ruleContext rule.Context,
	function *high_level_intermediate_representation.Function,
	env *refsEnvironment,
	instruction *high_level_intermediate_representation.Instruction,
	callee static_single_assignment.IdentifierId,
	interpolatedAsJsx map[static_single_assignment.IdentifierId]bool,
	findings []refsFinding,
) []refsFinding {
	collected := findings
	target := instruction.LValue.Identifier
	returnType := &refsAccessType{Kind: refsNone}
	didError := false

	if calleeType := env.get(
		callee,
	); calleeType != nil && calleeType.Kind == refsStructure && calleeType.Function != nil {
		if calleeType.Function.ReturnType != nil {
			returnType = calleeType.Function.ReturnType
		}
		if calleeType.Function.ReadRefEffect {
			didError = true
			collected = append(
				collected,
				refsFinding{Kind: refsFindingFunctionAccessesReference, Value: callee, HasValue: true},
			)
		}
	}

	if !didError {
		// The call's own node is consulted as well as the target value, because a call's lvalue is a
		// temporary with no `Identifier.Node`, so asking the checker about the value alone answers
		// false for every call. Handing it the call expression recovers the result type, which is
		// what makes the mergeRefs shape (`mergeReferences(...)` returning `RefCallback`) exempt.
		//
		// Measured on Kirk's tree: without this the eight `mergeReferences([inner, ref])` sites all
		// report, and upstream is silent on that exact input.
		isRefResult := refsIsRefBinding(ruleContext, function, env, target) ||
			refsCallResultIsReference(ruleContext, instruction)
		hookName := env.nameOf(function, callee)
		isHook := refsIsHookName(hookName)
		// `useState`, `useReducer` and `useMemo` are not exempt, because each runs the argument it
		// is given during render: a state initializer and a memo callback both execute on the spot,
		// so a ref reaching one is genuinely read during render. Every other hook defers.
		//
		// Upstream's own condition names only UseState and UseReducer (`:691`), and reading it
		// would have produced a rule that lets `useMemo(() => foo(ref), [])` through. Measured on
		// the executable across ten hooks, `useMemo` reports, and the fixture
		// `error.maybe-mutable-ref-not-preserved` is exactly that shape. oxc reaches the same
		// verdict by a different route (the memo callback is lowered and its body analysed rather
		// than the call being exempted), so this is a divergence in mechanism that agrees in
		// outcome, recorded here rather than silently matched.
		isStateHook := hookName == "useState" || hookName == "useReducer" || hookName == "useMemo"

		switch {
		// Producing a ref (the `mergeRefs` shape) or calling a hook: only a bare ref value in the
		// arguments is a problem, because the ref object itself is what these legitimately take.
		case isRefResult || (isHook && !isStateHook):
			high_level_intermediate_representation.EachPlace(instruction.Value, func(
				place high_level_intermediate_representation.Place,
				role high_level_intermediate_representation.PlaceRole,
			) {
				collected = refsCheckDirectValueAccess(env, place.Identifier, collected)
			})
		// The result is rendered, so a function carrying a ref is acceptable but a ref value is not.
		case interpolatedAsJsx[target]:
			high_level_intermediate_representation.EachPlace(instruction.Value, func(
				place high_level_intermediate_representation.Place,
				role high_level_intermediate_representation.PlaceRole,
			) {
				collected = refsCheckValueAccess(env, place.Identifier, collected)
			})
		// An ordinary call: passing a ref to it may read the ref during render.
		default:
			high_level_intermediate_representation.EachPlace(instruction.Value, func(
				place high_level_intermediate_representation.Place,
				role high_level_intermediate_representation.PlaceRole,
			) {
				if place.Identifier == callee {
					return
				}
				collected = refsCheckPassedToFunction(env, place.Identifier, collected)
			})
		}
	}

	env.set(target, returnType)
	return collected
}

// refsStoreTransfer handles writing through a property or a computed key.
//
// The safe-block consultation is what permits the guarded initialization idiom: inside the
// fallthrough of `if (ref.current == null)`, one write to that specific ref is allowed, and the
// permission is consumed so a second write on the same guard still reports. That consumption is why
// `ref-initialization-linear`, which writes twice under one guard, reports exactly once.
//
// Mutates: safeBlocks removes the guard a permitted write consumes, so a second write under it reports.
func refsStoreTransfer(
	env *refsEnvironment,
	instruction *high_level_intermediate_representation.Instruction,
	object static_single_assignment.IdentifierId,
	stored *high_level_intermediate_representation.Place,
	computedKey *high_level_intermediate_representation.Place,
	findings []refsFinding,
	safeBlocks *[]refsSafeBlock,
) []refsFinding {
	collected := findings
	foundSafe := false
	if target := env.get(
		object,
	); target != nil && target.Kind == refsReference && target.HasRefId && computedKey == nil {
		for index, safe := range *safeBlocks {
			if safe.RefId == target.RefId {
				*safeBlocks = append((*safeBlocks)[:index], (*safeBlocks)[index+1:]...)
				foundSafe = true
				break
			}
		}
	}
	if !foundSafe {
		collected = refsCheckUpdate(env, object, instruction.Node, collected)
	}
	if computedKey != nil {
		collected = refsCheckValueAccess(env, computedKey.Identifier, collected)
	}
	if stored != nil {
		collected = refsCheckDirectValueAccess(env, stored.Identifier, collected)
		// Storing a structure into an object makes the object carry what the structure carried, so a
		// ref reaching an object through a property write is still findable.
		if storedType := env.get(stored.Identifier); storedType != nil && storedType.Kind == refsStructure {
			merged := storedType
			if target := env.get(object); target != nil {
				merged = refsJoin(merged, target, env.nextRefId)
			}
			env.set(object, merged)
		}
	}
	return collected
}

// refsBinaryTransfer recognizes the null comparison that turns a ref value into a guard.
//
// `ref.current == null` is the sanctioned way to ask whether a ref has been initialized, so it is
// clean and its result authorizes one write. Comparing a ref value against anything else is an
// ordinary read and reports.
func refsBinaryTransfer(
	env *refsEnvironment,
	instruction *high_level_intermediate_representation.Instruction,
	value *high_level_intermediate_representation.BinaryExpression,
	target static_single_assignment.IdentifierId,
	findings []refsFinding,
) []refsFinding {
	collected := findings
	leftType := env.get(value.Left.Identifier)
	rightType := env.get(value.Right.Identifier)

	foundRefId, hasRefId := 0, false
	if leftType != nil && leftType.Kind == refsRefValue && leftType.HasRefId {
		foundRefId, hasRefId = leftType.RefId, true
	} else if rightType != nil && rightType.Kind == refsRefValue && rightType.HasRefId {
		foundRefId, hasRefId = rightType.RefId, true
	}

	nullish := (leftType != nil && leftType.Kind == refsNullable) ||
		(rightType != nil && rightType.Kind == refsNullable)

	if hasRefId && nullish {
		env.set(target, &refsAccessType{Kind: refsGuard, RefId: foundRefId, HasRefId: true})
		return collected
	}
	collected = refsCheckValueAccess(env, value.Left.Identifier, collected)
	collected = refsCheckValueAccess(env, value.Right.Identifier, collected)
	return collected
}
