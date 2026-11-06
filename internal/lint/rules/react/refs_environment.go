package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	"github.com/system-inc/cohere/static_single_assignment"
)

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
