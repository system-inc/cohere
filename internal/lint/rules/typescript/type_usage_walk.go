package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// typeUsageWalk carries the state upstream keeps in the closure variables of
// `collectTypeParameterUsageCounts`. One walk is one call to that function.
type typeUsageWalk struct {
	ruleContext rule.Context

	// counts is upstream's `foundIdentifierUsages`, keyed on the type parameter declaration node
	// rather than on its name identifier. Same identity, one dereference fewer.
	counts map[*ast.Node]int

	// typeUsages is upstream's recursion guard. Seeing one type more than nine times means either a
	// recursive type such as `type T = { [P in keyof T]: T }`, or a type seen often enough that
	// anything it references has already been counted past the threshold either way.
	typeUsages map[*checker.Type]int

	// visitedConstraints and visitedDefault stop a constrained type parameter's constraint from
	// being walked twice, which upstream guards for the same reason: visiting the type of a
	// constrained parameter recurses into the constraint.
	visitedConstraints map[*checker.Type]bool
	visitedDefault     bool

	// visitedPropertyLists is upstream's `visitedSymbolLists`, keyed on the object type whose
	// properties were walked. See visitObjectType for why the key is the type.
	visitedPropertyLists map[*checker.Type]bool

	// fromClass records that this walk is over a class or one of its members, where upstream
	// accepts every type argument as a multiple use.
	fromClass bool

	// functionLikeType records that a signature was visited, which suppresses the plain type visit.
	functionLikeType bool
}

// visitType is upstream's `visitType`, branch for branch and in upstream's order.
//
// The order is load-bearing rather than stylistic: the catch-all object arm has to come after the
// type-reference arm, or a generic interface such as `Map<K, V>` would be descended into property
// by property instead of being read as a reference with two type arguments.
//
// `assumeMultipleUses` is threaded through every branch and means "count an arrival here as two
// uses rather than one". It is set wherever the walk cannot see far enough to be sure, and
// over-counting is the safe direction because a higher count produces silence rather than a report.
func (walk *typeUsageWalk) visitType(subject *checker.Type, assumeMultipleUses bool, isReturnType bool) {
	// Seeing the same type more than nine times means a likely recursive type. If it is not
	// recursive, it has been seen often enough that anything it references is counted past the
	// threshold regardless.
	if subject == nil || walk.incrementTypeUsages(subject) > 9 {
		return
	}

	if subject.IsTypeParameter() {
		walk.visitTypeParameter(subject, assumeMultipleUses)
		return
	}

	// Catch-all for a generic type alias such as `Exclude<T, null>`. The walk does not descend into
	// the alias definition, so it cannot know how many times an argument is used inside and assumes
	// multiple uses.
	if alias := checker.Type_alias(subject); alias != nil && len(alias.TypeArguments()) > 0 {
		// Survives mutation. Flipping this to false leaves every probed shape unchanged, including
		// `type Wrapper<X> = { value: X }` and `type Pair<X, Y>` reached through a parameter, since
		// a type parameter passed to an alias is already treated as repeated by the syntactic pass
		// before the walk is consulted. Kept because the two passes are independent and upstream's
		// comment states the reasoning: the definition is not descended into, so it is safest to
		// assume the argument is used more than once.
		walk.visitTypesList(alias.TypeArguments(), true)
		return
	}

	flags := checker.Type_flags(subject)

	// Unions and intersections like `0 | 1`.
	if flags&checker.TypeFlagsUnionOrIntersection != 0 {
		walk.visitTypesList(subject.Types(), assumeMultipleUses)
		return
	}

	// Index access types like `T[K]`.
	if flags&checker.TypeFlagsIndexedAccess != 0 {
		indexedAccess := subject.AsIndexedAccessType()
		walk.visitType(indexedAccess.ObjectType(), assumeMultipleUses, false)
		walk.visitType(indexedAccess.IndexType(), assumeMultipleUses, false)
		return
	}

	// Tuple types like `[K, V]` and generic type references like `Map<K, V>`.
	//
	// This guard is crash protection as well as a branch selector. `GetTypeArguments` dereferences
	// the type's reference data (TypeScript/tsc/internal/checker/checker.go:22011) and takes the
	// process down on a type that is not a reference. Measured with a probe that called it
	// unguarded on the mapped type reached by upstream's `<T extends string>(t: T) => t as
	// { [K in 'a' as T]: 0 }`: immediate nil dereference. Since the walk recovers per file rather
	// than per rule, that would cost every rule its verdict on any file containing such a shape.
	if isTypeReferenceType(subject) {
		walk.visitTypeReference(subject, assumeMultipleUses, isReturnType)
		return
	}

	// Template literals like `a${T}b`.
	if flags&checker.TypeFlagsTemplateLiteral != 0 {
		for _, member := range subject.AsTemplateLiteralType().Types() {
			walk.visitType(member, assumeMultipleUses, false)
		}
		return
	}

	// Conditional types like `T extends string ? T : never`. Upstream visits only the check and
	// extends halves, not the branches, so `T extends string ? T : never` counts T twice.
	if flags&checker.TypeFlagsConditional != 0 {
		conditional := subject.AsConditionalType()
		walk.visitType(conditional.CheckType(), assumeMultipleUses, false)
		walk.visitType(conditional.ExtendsType(), assumeMultipleUses, false)
		return
	}

	// Catch-all for inferred object types like `{ K: V }`. This has to stay after the type
	// reference arm above, or a generic interface would be descended into property by property.
	if flags&checker.TypeFlagsObject != 0 {
		walk.visitObjectType(subject)
		return
	}

	// Catch-all for operator types like `keyof T`.
	if flags&checker.TypeFlagsIndex != 0 {
		walk.visitType(subject.AsIndexType().Target(), assumeMultipleUses, false)
	}
}

// visitTypeParameter counts one arrival at a type parameter and then walks its constraint and its
// default, each at most once.
func (walk *typeUsageWalk) visitTypeParameter(subject *checker.Type, assumeMultipleUses bool) {
	symbol := subject.Symbol()
	if symbol == nil || len(symbol.Declarations) == 0 {
		return
	}

	// Upstream reads `getDeclarations()?.[0]`. This is the "what single thing is this" question
	// rather than the "does any declaration match" one: a type parameter is one declaration, and
	// upstream records exactly one. Looping would be wider than upstream rather than safer.
	declaration := symbol.Declarations[0]
	if declaration.Kind != ast.KindTypeParameter {
		return
	}

	walk.incrementIdentifierCount(declaration, assumeMultipleUses)

	declarationData := declaration.AsTypeParameterDeclaration()

	// Visiting the type of a constrained type parameter recurses into the constraint, so each
	// constraint is visited at most once to avoid an infinite loop.
	if constraint := declarationData.Constraint; constraint != nil {
		constraintType := walk.ruleContext.TypeChecker.GetTypeAtLocation(constraint)
		if constraintType != nil && !walk.visitedConstraints[constraintType] {
			walk.visitedConstraints[constraintType] = true
			walk.visitType(constraintType, false, false)
		}
	}

	// Survives mutation and is kept as upstream's own line. Deleting the default visit leaves every
	// verdict and every message unchanged, including on shapes built to reach it: `<T, U = T>`,
	// `<T, U = T[]>` and `<U = string>` all agree with the installed build either way, because a
	// default is written in the signature where the syntactic pass has already counted it. No
	// distinguishing input was found, so this is recorded as unmeasured rather than as equivalent.
	if defaultType := declarationData.DefaultType; defaultType != nil && !walk.visitedDefault {
		walk.visitedDefault = true
		walk.visitType(walk.ruleContext.TypeChecker.GetTypeAtLocation(defaultType), false, false)
	}
}

// visitTypeReference is upstream's `isTypeReference` arm, including the two special cases that
// decide whether `T[]` counts once or twice.
//
// Upstream's ladder, reproduced in its order:
//
//	a class context accepts everything as multiple uses
//	a TUPLE counts multiply only when it is returned and is not readonly
//	an ARRAY counts multiply only when it is returned and is a mutable Array
//	any other reference always counts as multiple uses
//
// The readonly and return-position carve-outs are what separate upstream's own neighbouring cases:
// `<T,>(param: T[]) => null` reports while a returned `T[]` does not.
func (walk *typeUsageWalk) visitTypeReference(subject *checker.Type, assumeMultipleUses bool, isReturnType bool) {
	target := subject.Target()

	for _, typeArgument := range walk.ruleContext.TypeChecker.GetTypeArguments(subject) {
		// In a class context everything is accepted.
		thisAssumeMultipleUses := walk.fromClass || assumeMultipleUses

		if !thisAssumeMultipleUses {
			switch {
			case target != nil && target.IsTupleType():
				// A readonly tuple is considered to use the parameter once; a mutable one counts
				// multiply only when it is returned.
				thisAssumeMultipleUses = isReturnType && !target.AsTupleType().IsReadonly()
			case target != nil && walk.ruleContext.TypeChecker.IsArrayType(target):
				// ReadonlyArray is considered a single use, so the symbol name is what separates
				// the two rather than the target's shape.
				symbol := subject.Symbol()
				thisAssumeMultipleUses = isReturnType && symbol != nil && symbol.Name == "Array"
			default:
				// Any other kind of type reference always counts as multiple uses.
				thisAssumeMultipleUses = true
			}
		}

		walk.visitType(typeArgument, thisAssumeMultipleUses, isReturnType)
	}
}

// visitObjectType is upstream's catch-all object arm.
//
// The mapped-type half of upstream's arm is absent here, and deliberately so: the four fields it
// reads are unexported on checker.MappedType with no accessor, measured with a compiling probe.
// See the note on the rule variable for what that costs and why the direction is safe.
//
// # An object type's properties are walked once per walk, which is upstream's list guard
//
// Upstream's `visitSymbolsListOnce` skips a property list it has already walked, keyed on the
// array's identity. `type.getProperties()` returns the array cached on the type's resolved members,
// so the same type always hands back the same array and the guard is "each object type's
// properties once". Go slices carry no identity, so the key here is the type itself, which is the
// same set.
//
// It changes counts, not only recursion. `Extract<Union, { type: Kind }>` distributes over the
// union, and every constituent's conditional carries the same `{ type: Kind }` object as its extends
// type. Upstream walks its properties once and counts `Kind` once, so a `Kind` used nowhere else in
// the signature reports. Without the guard each constituent walked them again, `Kind` counted once
// per union member, and the rule was silent where ESLint reports: Structure's `surveyFieldRendererFor`
// (#vn5vpfs) at ea00a681, fixed in 3146950.
func (walk *typeUsageWalk) visitObjectType(subject *checker.Type) {
	if !walk.visitedPropertyLists[subject] {
		walk.visitedPropertyLists[subject] = true
		walk.visitSymbolsList(walk.ruleContext.TypeChecker.GetPropertiesOfType(subject), false)
	}

	walk.visitType(walk.ruleContext.TypeChecker.GetNumberIndexType(subject), true, false)
	walk.visitType(walk.ruleContext.TypeChecker.GetStringIndexType(subject), true, false)

	for _, signature := range walk.ruleContext.TypeChecker.GetSignaturesOfType(subject, checker.SignatureKindCall) {
		walk.functionLikeType = true
		walk.visitSignature(signature)
	}

	for _, signature := range walk.ruleContext.TypeChecker.GetSignaturesOfType(
		subject,
		checker.SignatureKindConstruct,
	) {
		walk.functionLikeType = true
		walk.visitSignature(signature)
	}
}

// visitSignature is upstream's `visitSignature`: the receiver, then each parameter, then each of
// the signature's own type parameters, then the return type in return position.
func (walk *typeUsageWalk) visitSignature(signature *checker.Signature) {
	if signature == nil {
		return
	}

	// Subsumed in practice, measured, and kept. Deleting this visit survives every fixture and
	// every shape probed against the installed build: `<T>(this: T): T`, `<T>(this: T, other: T)`,
	// `<T>(this: Record<'p', T>)` and upstream's own two `this` cases all keep their verdict. The
	// counts really do drop (4 to 2, and 2 to 1 on upstream's `foo<T>(this: T): void`), so the
	// branch is not inert; the drop simply never crosses the report threshold, because a `this`
	// parameter is written in the signature, where the syntactic pass has already seen it.
	//
	// Kept because it is upstream's own line and the subsumption is arithmetic rather than
	// structural: it holds only while the threshold is 2 and while every `this` type is also
	// syntactically visible, and neither is a property this rule guarantees.
	if thisParameter := signature.ThisParameter(); thisParameter != nil {
		walk.visitType(walk.ruleContext.TypeChecker.GetTypeOfSymbol(thisParameter), false, false)
	}

	for _, parameter := range signature.Parameters() {
		walk.visitType(walk.ruleContext.TypeChecker.GetTypeOfSymbol(parameter), false, false)
	}

	for _, typeParameter := range signature.TypeParameters() {
		walk.visitType(typeParameter, false, false)
	}

	// A type predicate stands in for the return type when the signature has one, so `input is T`
	// counts its T in return position exactly as a returned `T` would.
	returnType := walk.ruleContext.TypeChecker.GetReturnTypeOfSignature(signature)
	if predicate := walk.ruleContext.TypeChecker.GetTypePredicateOfSignature(
		signature,
	); predicate != nil && predicate.Type() != nil {
		returnType = predicate.Type()
	}
	walk.visitType(returnType, false, true)
}

// visitSymbolsList walks each symbol's type. Upstream's once-per-list guard is kept by its one
// caller, visitObjectType, keyed on the type that owns the list.
func (walk *typeUsageWalk) visitSymbolsList(symbols []*ast.Symbol, assumeMultipleUses bool) {
	for _, symbol := range symbols {
		walk.visitType(walk.ruleContext.TypeChecker.GetTypeOfSymbol(symbol), assumeMultipleUses, false)
	}
}

func (walk *typeUsageWalk) visitTypesList(types []*checker.Type, assumeMultipleUses bool) {
	for _, subject := range types {
		walk.visitType(subject, assumeMultipleUses, false)
	}
}

// incrementIdentifierCount is upstream's function of the same name: one arrival counts as one use,
// or as two when the walk could not see far enough to be sure.
func (walk *typeUsageWalk) incrementIdentifierCount(declaration *ast.Node, assumeMultipleUses bool) {
	value := 1
	if assumeMultipleUses {
		value = 2
	}
	walk.counts[declaration] += value
}

func (walk *typeUsageWalk) incrementTypeUsages(subject *checker.Type) int {
	walk.typeUsages[subject]++
	return walk.typeUsages[subject]
}
