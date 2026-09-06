package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/rule"
)

// NoUnnecessaryTypeParameters flags a type parameter that appears only once in the signature that
// declares it, because a parameter used once relates nothing to anything and the constraint says
// the same thing more plainly.
//
//	valid:   function identity<T>(arg: T): T { return arg; }
//	valid:   class ClassyArray<T> { value1: T; value2: T; }
//	valid:   type Fn = <T>(input: T) => T;
//	invalid: const func = <T,>(param: T) => null;
//	invalid: function parseYAML<T>(input: string): T { return input as any as T; }
//	invalid: declare class C<V> {}
//
// `function f<T>(x: T): void` could take `unknown` and mean exactly the same thing. The parameter
// looks like it constrains the caller and does not: nothing downstream is tied to whatever `T`
// resolves to, so the generic is decoration. `parseYAML<T>(input: string): T` is the dangerous
// shape rather than merely the useless one, because the single use is in RETURN position, which
// makes it an unchecked assertion wearing the syntax of a type-safe call.
//
// # This counts USES, and three separate rules decide what a use is
//
// The counting is subtler than it looks, and each of the three rules below is pinned by cases in
// upstream's corpus that would otherwise pass for the wrong reason.
//
// A use inside the parameter's OWN constraint or default does not count. `<T, U extends T>` puts
// `T` in the source twice and only one of those is a relating use, so `U` is reported and `T` is
// not. Upstream implements this as a range test rather than a kind test: a reference whose position
// falls inside the declaring type parameter's own span is skipped.
//
// A use inside the function BODY does not count. This is the rule that surprises, and upstream's
// corpus pins it three times over:
//
//	function foo<T>(_: T) { const x: T = null!; const y: T = null!; }        REPORTS
//
// Two body uses, and it still reports, because the count stops at the start of the body. The
// reasoning holds up: a body use is an annotation the author wrote for themselves, and it relates
// the parameter to nothing the CALLER can see. The signature is still `(_: unknown) => void` from
// outside. Upstream's cutoff is the body's start position when there is a body, and the return
// type's end when there is not, which is why `declare` shapes and interface members count their
// return annotation while a function with a body does not count past its brace.
//
// A use as a TYPE ARGUMENT counts as multiple uses on its own. `Partial<T>` relates `T` to whatever
// `Partial` does with it, and this rule cannot see inside the alias, so it assumes the worst and
// stays silent. That is why `type Fn = <T>(input: Partial<T>) => typeof input;` is clean with only
// one visible mention of `T`. Array and ReadonlyArray are carved out of that exemption and deferred
// to the type phase, because `T[]` genuinely is a single use: `const func = <T,>(param: T[]) => null`
// reports.
//
// # Two mechanisms, and the second is the rule
//
// Upstream runs an AST pass first and reaches the type checker only when the AST pass is
// inconclusive. The AST pass can prove a parameter is used ENOUGH; it can never prove a parameter
// is used too little, because a use can be hiding in an INFERRED type that no syntax mentions:
//
//	function foo<T>(_: T) { function withX(): T { return null!; } }
//
// The parameter appears in the source inside the body, which the AST pass discards, and the
// function's own inferred return type mentions nothing. Only resolving types finds the truth. So a
// port that stopped at the AST pass would decline exactly the cases the rule exists for and would
// look like a working rule while reporting almost nothing.
//
// # The type walk, and the one shape this port cannot see
//
// The type phase resolves the declaration's type and walks the resulting type graph, counting each
// time it arrives at a type parameter's declaration. `assumeMultipleUses` is threaded through the
// walk: some positions are counted as two uses rather than one because the walk cannot see far
// enough to be sure, and over-counting is the safe direction (it produces silence rather than a
// false report).
//
// MAPPED TYPES ARE THE GAP, and it is a substrate limit rather than a judgment. Upstream reads four
// fields off a mapped type (its type parameter, constraint, name type and template type) to count
// uses inside `{ [K in keyof T]: ... }`. In this tree those four fields are unexported on
// `checker.MappedType` with no accessor method and no entry in `shim/checker/extra-shim.json`, so
// they cannot be reached without a shim change, and a shim change lands as its own commit because
// 49 rules import that package. Measured with a compiling probe rather than a grep: reaching
// `MappedType.TypeParameter()` fails to build while the same shape on `ConditionalType.CheckType()`
// and `IndexedAccessType.ObjectType()` compiles.
//
// The consequence is confined and it falls in the SAFE direction. Sixteen of upstream's 163 cases
// involve a mapped type, twelve of them clean and four reporting. Without the mapped-type arm the
// walk stops descending at a mapped type, so uses hiding inside one are not counted, which can only
// LOWER a count. A lower count reports where upstream reports and can also report where upstream is
// silent, so the exposure is false positives on the twelve clean shapes rather than missed findings.
// The four reporting mapped-type cases are covered by the test file and the twelve clean ones are
// recorded there too, marked with what makes each of them clean.
//
// # Why this is a suggestion and not a fix
//
// Upstream offers `replaceUsagesWithConstraint` as a SUGGESTION rather than a fix, and that is the
// right line: rewriting every use of the parameter to its constraint and deleting the parameter
// changes what the signature means to every caller. A human chooses it; the engine does not apply
// it unattended.
var NoUnnecessaryTypeParameters = rule.Rule{
	Name: "@typescript-eslint/no-unnecessary-type-parameters",

	// The type phase resolves declarations and walks the resulting type graph, so the checker is
	// required rather than merely convenient.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		// Declared above AND guarded here. NeedsTypeChecker governs the REGISTRATION path only, and
		// the test harness builds a rule.Context by hand, which is how a typed rule panics with a
		// nil receiver while its declaration reads as correct. Declining once in Run costs one
		// comparison per file instead of one per node.
		if ctx.TypeChecker == nil {
			return nil
		}

		checkFunctionLike := func(node *ast.Node) {
			checkTypeParameterOwner(ctx, node, "function")
		}
		checkClassLike := func(node *ast.Node) {
			checkTypeParameterOwner(ctx, node, "class")
		}

		// Upstream's two selector groups, expanded into the kinds this parser produces. The first
		// group is every signature-bearing declaration and carries the descriptor "function"; the
		// second is the two class forms and carries "class". Both selectors are qualified
		// `[typeParameters]` upstream, which is reproduced by the emptiness check inside
		// checkTypeParameterOwner rather than by the listener key.
		return rule.Listeners{
			ast.KindArrowFunction:       checkFunctionLike,
			ast.KindFunctionDeclaration: checkFunctionLike,
			ast.KindFunctionExpression:  checkFunctionLike,
			ast.KindCallSignature:       checkFunctionLike,
			ast.KindConstructSignature:  checkFunctionLike,
			ast.KindConstructorType:     checkFunctionLike,
			ast.KindFunctionType:        checkFunctionLike,
			ast.KindMethodSignature:     checkFunctionLike,
			ast.KindMethodDeclaration:   checkFunctionLike,
			ast.KindClassDeclaration:    checkClassLike,
			ast.KindClassExpression:     checkClassLike,
		}
	},
}

// checkTypeParameterOwner is upstream's `checkNode`.
//
// For each type parameter the node declares, it asks the AST pass first and reaches the type
// checker only when the AST pass could not prove the parameter is used enough. The type counts are
// computed at most once per node, on first need, exactly as upstream's `counts ??=` does.
func checkTypeParameterOwner(ctx rule.Context, node *ast.Node, descriptor string) {
	typeParameters := node.TypeParameters()
	if len(typeParameters) == 0 {
		return
	}

	// The position past which a use no longer counts. Upstream reads the body's start when there is
	// a body and the return type's end otherwise, falling back to Infinity when there is neither.
	// A `declare` function, an interface member and a call signature all take the second branch,
	// which is what lets their return annotation count while a real function's body does not.
	countUsesBefore := usesCountedBefore(node)

	// Populated lazily, and only if some parameter survives the AST pass.
	var typeCounts map[*ast.Node]int
	typeCountsComputed := false

	for _, typeParameter := range typeParameters {
		name := typeParameter.Name()
		if name == nil || !ast.IsIdentifier(name) {
			continue
		}

		if isTypeParameterRepeatedInSyntax(ctx, node, typeParameter, name.Text(), countUsesBefore) {
			continue
		}

		if !typeCountsComputed {
			typeCounts = countTypeParameterUsage(ctx, node)
			typeCountsComputed = true
		}

		count, found := typeCounts[typeParameter]
		if !found || count > 2 {
			continue
		}

		uses := "used only once"
		if count == 1 {
			uses = "never used"
		}

		ctx.ReportNodeWithSuggestions(typeParameter,
			buildSoleTypeParameterMessage(name.Text(), uses, descriptor),
			rule.Suggestion{Message: buildReplaceUsagesWithConstraintMessage()})
	}
}

// usesCountedBefore is upstream's `node.body?.range[0] ?? node.returnType?.range[1]`.
//
// Returning the source file's end when there is neither a body nor a return annotation reproduces
// upstream's `Infinity` default: every use in the declaration counts.
func usesCountedBefore(node *ast.Node) int {
	if body := node.Body(); body != nil {
		return body.Pos()
	}
	if returnType := node.Type(); returnType != nil {
		return returnType.End()
	}
	return maximumSourcePosition
}

// maximumSourcePosition stands in for upstream's `Infinity` cutoff. Any real position compares
// below it, which is the only property the comparison needs.
const maximumSourcePosition = int(^uint(0) >> 1)

// isTypeParameterRepeatedInSyntax is upstream's `isTypeParameterRepeatedInAST`.
//
// Upstream reads a scope-manager reference list; this walks the declaring node's subtree instead
// and resolves each identifier through the checker, which answers the same question with the
// machinery this tree has. The four filters below are upstream's, in upstream's order, and each one
// is load-bearing:
//
//	inside the parameter's own span    `<T, U extends T>` mentions T twice, one of them in U's own
//	                                   constraint. Upstream tests POSITION rather than kind, and so
//	                                   does this, because the same span test covers a default as
//	                                   well as a constraint with no second case.
//	past the counting cutoff           a use in the body relates the parameter to nothing a caller
//	                                   can see, so it does not count.
//	not this parameter                 a reference to a different type parameter, or a value-land
//	                                   identifier that merely shares the name.
//	used as a type argument            `Partial<T>` relates T to whatever Partial does, which this
//	                                   rule cannot see into, so it counts as repeated immediately.
//	                                   Array and ReadonlyArray are excluded and deferred to the
//	                                   type phase, where `T[]` is correctly counted as one use.
//
// Returning true means "used enough, do not report" and is the only way this pass can conclude
// anything. It can never prove a parameter is used too LITTLE, because a use can hide in an
// inferred type that no syntax mentions, which is what the type phase is for.
func isTypeParameterRepeatedInSyntax(
	ctx rule.Context,
	owner *ast.Node,
	typeParameter *ast.Node,
	typeParameterName string,
	countUsesBefore int,
) bool {
	declarationSymbol := ctx.TypeChecker.GetSymbolAtLocation(typeParameter.Name())

	total := 0
	repeated := false

	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if repeated {
			return true
		}

		if node.Kind == ast.KindIdentifier && node.Text() == typeParameterName {
			if countsAsRelatingUse(ctx, node, typeParameter, declarationSymbol, countUsesBefore) {
				if isUsedAsTypeArgument(node) {
					repeated = true
					return true
				}
				total++
				if total >= 2 {
					repeated = true
					return true
				}
			}
		}

		node.ForEachChild(visit)
		return repeated
	}

	owner.ForEachChild(visit)

	return repeated
}

// countsAsRelatingUse applies upstream's three skip filters to one identifier.
func countsAsRelatingUse(
	ctx rule.Context,
	identifier *ast.Node,
	typeParameter *ast.Node,
	declarationSymbol *ast.Symbol,
	countUsesBefore int,
) bool {
	// References inside the type parameter's own definition do not count. Upstream's test is an
	// overlap of ranges rather than an ancestor walk, and reproducing the range test rather than
	// the intent keeps a default (`<T = T[]>`) covered by the same two comparisons a constraint is.
	//
	// SUBSUMED BY THE TYPE PHASE, measured rather than assumed, and kept anyway. A mutation
	// narrowing this overlap so an in-constraint mention COUNTS survives every fixture, and four
	// rounds of hunting a distinguishing input found none. The reason is not that the branch is
	// unreachable: a probe confirmed the two versions disagree about real identifiers, in
	// `<U extends U>`, `<U extends { self: U }>`, `<U extends [U]>` and `<U extends U | string>`.
	// It is that the only inputs where the disagreement could flip a VERDICT are ones where a type
	// parameter appears inside its own constraint, and for every such shape the type phase counts
	// it 3 times (the use, plus the constraint recursion in visitTypeParameter), which is already
	// past the report threshold of 2. So both versions reach silence by different routes.
	//
	// Kept because the subsumption is an accident of the two layers agreeing, not a property either
	// one guarantees: upstream writes this filter, the count that subsumes it is a threshold that
	// could move, and a guard whose redundancy depends on another layer's arithmetic is not one to
	// delete on the strength of a surviving mutant.
	if identifier.Pos() < typeParameter.End() && identifier.End() > typeParameter.Pos() {
		return false
	}

	// Nor references past the counting cutoff, which is the body's start when there is a body.
	if identifier.Pos() > countUsesBefore {
		return false
	}

	// The identifier is the declaration's own name rather than a use of it.
	if identifier == typeParameter.Name() {
		return false
	}

	// Upstream filters on `reference.isTypeReference`, which is the scope manager saying the
	// identifier stands in TYPE position. Without an equivalent filter a NAME that merely spells
	// the same thing is counted as a use, and the checker will happily resolve it to the type
	// parameter's own symbol, so symbol identity does not separate the two.
	//
	// Measured on upstream's `declare function setItem<T>(T): T;`, where the sole parameter is
	// written with no annotation so `T` is the PARAMETER's name. All three identifiers resolve to
	// the same symbol, and without this filter the parameter name counts as a second use, the AST
	// pass concludes "repeated", and the case goes silent while upstream reports it.
	if !isIdentifierInTypePosition(identifier) {
		return false
	}

	// Neither do references that are not to the same type parameter. Matching on TEXT alone would
	// count a value-land identifier that happens to share the name, and would count a shadowing
	// type parameter declared by a nested signature as if it were this one. Upstream separates
	// those with the scope manager; resolving the symbol answers the same question here.
	if declarationSymbol != nil {
		symbol := ctx.TypeChecker.GetSymbolAtLocation(identifier)
		if symbol != declarationSymbol {
			return false
		}
	}

	return true
}

// isUsedAsTypeArgument is upstream's type-argument branch.
//
// A parameter passed as a type argument, as in `Partial<T>`, is treated as repeated on sight,
// because this rule cannot descend into the alias to find out how many times it is really used and
// assuming the worst produces silence rather than a false report.
//
// Array and ReadonlyArray are carved out and deferred to the type phase. That exclusion is what
// makes `const func = <T,>(param: T[]) => null` report: `T[]` is genuinely one use, and the type
// phase counts it as one. Upstream's comment says the same thing in one line, and the corpus pins
// both directions, with `Partial<T>` clean and `T[]` reporting.
// SUBSUMED BY THE TYPE PHASE for the whole-branch case, measured. Forcing this function's caller
// to ignore it entirely survives every fixture, because the type phase independently declines the
// same shapes: `Partial<T>` resolves to a type whose alias arguments are visited with
// assumeMultipleUses, giving a count above the threshold either way. The CARVE-OUT inside it is a
// different matter and is genuinely load-bearing, which the array test file pins.
func isUsedAsTypeArgument(identifier *ast.Node) bool {
	// The identifier has to be the NAME of a type reference before it can be an argument to
	// another one. `T` in `Partial<T>` is the type name of the inner reference.
	reference := identifier.Parent
	if reference == nil || reference.Kind != ast.KindTypeReference {
		return false
	}
	if reference.AsTypeReferenceNode().TypeName != identifier {
		return false
	}

	// Upstream skips upward through union and intersection constituents before asking whether the
	// enclosing node is a type-argument list, so `Partial<T | null>` counts the same as `Partial<T>`.
	enclosing := skipTypeConstituentsUpward(reference.Parent)
	if enclosing == nil {
		return false
	}

	// A type argument's parent in this parser is the node that carries the argument list, so the
	// test is whether the enclosing node is a type reference (or a heritage clause element) whose
	// type arguments contain the reference we came from.
	container := enclosing
	if !containsTypeArgument(container, reference) {
		return false
	}

	// Array and ReadonlyArray are handled carefully; the check is deferred to the type-aware phase.
	if name := typeReferenceNameText(container); name == "Array" || name == "ReadonlyArray" {
		return false
	}

	return true
}

// skipTypeConstituentsUpward is upstream's `skipConstituentsUpward`.
func skipTypeConstituentsUpward(node *ast.Node) *ast.Node {
	for node != nil && (node.Kind == ast.KindUnionType || node.Kind == ast.KindIntersectionType) {
		node = node.Parent
	}
	return node
}

// containsTypeArgument reports whether `container` passes `argument` as one of its type arguments.
//
// CRASH PROTECTION as well as a filter, and the crash is the part no findings fixture can see.
// `Node.TypeArguments` panics with "Unhandled case" on any node kind outside its own switch
// (TypeScript/tsc/internal/ast/ast.go:515), and the kinds reaching here are whatever the parent
// chain of a type reference happens to be. Measured: without the kind guard the very first valid
// case, `class ClassyArray<T> { arr: T[]; }`, takes the process down, because `T` in `T[]` has an
// ArrayType parent that carries no type argument list. The walk recovers per FILE rather than per
// rule, so one such panic costs every rule its verdict on that file.
func containsTypeArgument(container *ast.Node, argument *ast.Node) bool {
	if !nodeCanCarryTypeArguments(container) {
		return false
	}
	for _, typeArgument := range container.TypeArguments() {
		if typeArgument == argument {
			return true
		}
	}
	return false
}

// nodeCanCarryTypeArguments names the kinds `Node.TypeArguments` is willing to answer for. The list
// mirrors that function's own switch rather than being derived from what this rule expects to see,
// because the safety property is about the callee's coverage and not about this rule's inputs.
func nodeCanCarryTypeArguments(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindCallExpression, ast.KindNewExpression, ast.KindTaggedTemplateExpression,
		ast.KindTypeReference, ast.KindExpressionWithTypeArguments, ast.KindImportType,
		ast.KindTypeQuery, ast.KindJsxOpeningElement, ast.KindJsxSelfClosingElement:
		return true
	default:
		return false
	}
}

// typeReferenceNameText answers the plain name a type reference is written with, or "" when the
// node is not a type reference or its name is qualified.
func typeReferenceNameText(node *ast.Node) string {
	if node == nil || node.Kind != ast.KindTypeReference {
		return ""
	}
	typeName := node.AsTypeReferenceNode().TypeName
	if typeName == nil || !ast.IsIdentifier(typeName) {
		return ""
	}
	return typeName.Text()
}

// typeUsageWalk carries the state upstream keeps in the closure variables of
// `collectTypeParameterUsageCounts`. One walk is one call to that function.
type typeUsageWalk struct {
	ctx rule.Context

	// counts is upstream's `foundIdentifierUsages`, keyed on the type parameter DECLARATION node
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

	// fromClass records that this walk is over a class or one of its members, where upstream
	// accepts every type argument as a multiple use.
	fromClass bool

	// functionLikeType records that a signature was visited, which suppresses the plain type visit.
	functionLikeType bool
}

// countTypeParameterUsage is upstream's function of the same name.
//
// A class counts uses across its own type parameters and every one of its members; anything else
// counts uses in the declaration itself.
func countTypeParameterUsage(ctx rule.Context, node *ast.Node) map[*ast.Node]int {
	counts := map[*ast.Node]int{}

	if node.Kind == ast.KindClassDeclaration || node.Kind == ast.KindClassExpression {
		for _, typeParameter := range node.TypeParameters() {
			collectTypeParameterUsageCounts(ctx, typeParameter, counts, true)
		}
		for _, member := range node.Members() {
			collectTypeParameterUsageCounts(ctx, member, counts, true)
		}
		return counts
	}

	collectTypeParameterUsageCounts(ctx, node, counts, false)
	return counts
}

// collectTypeParameterUsageCounts is upstream's function of the same name: a limited subset of a
// scope manager, but over types rather than over names.
func collectTypeParameterUsageCounts(
	ctx rule.Context,
	node *ast.Node,
	counts map[*ast.Node]int,
	fromClass bool,
) {
	walk := &typeUsageWalk{
		ctx:                ctx,
		counts:             counts,
		typeUsages:         map[*checker.Type]int{},
		visitedConstraints: map[*checker.Type]bool{},
		fromClass:          fromClass,
	}

	// A call signature and a constructor are resolved as SIGNATURES rather than as types, because
	// their type at a location is not the thing whose parameters and return type need counting.
	if node.Kind == ast.KindCallSignature || node.Kind == ast.KindConstructor {
		walk.functionLikeType = true
		walk.visitSignature(ctx.TypeChecker.GetSignatureFromDeclaration(node))
	}

	if !walk.functionLikeType {
		walk.visitType(ctx.TypeChecker.GetTypeAtLocation(node), false, false)
	}
}

// visitType is upstream's `visitType`, branch for branch and in upstream's order.
//
// The ORDER is load-bearing rather than stylistic: the catch-all object arm has to come after the
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

	// Catch-all for a generic type ALIAS such as `Exclude<T, null>`. The walk does not descend into
	// the alias definition, so it cannot know how many times an argument is used inside and assumes
	// multiple uses.
	if alias := checker.Type_alias(subject); alias != nil && len(alias.TypeArguments()) > 0 {
		// SURVIVES MUTATION. Flipping this to false leaves every probed shape unchanged, including
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
	// This guard is CRASH PROTECTION as well as a branch selector. `GetTypeArguments` dereferences
	// the type's reference data (TypeScript/tsc/internal/checker/checker.go:22011) and takes the
	// process down on a type that is not a reference. Measured with a probe that called it
	// unguarded on the mapped type reached by upstream's `<T extends string>(t: T) => t as
	// { [K in 'a' as T]: 0 }`: immediate nil dereference. Since the walk recovers per FILE rather
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

	// Catch-all for inferred object types like `{ K: V }`. This has to stay AFTER the type
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
	// upstream records exactly one. Looping would be WIDER than upstream rather than safer.
	declaration := symbol.Declarations[0]
	if declaration.Kind != ast.KindTypeParameter {
		return
	}

	walk.incrementIdentifierCount(declaration, assumeMultipleUses)

	declarationData := declaration.AsTypeParameterDeclaration()

	// Visiting the type of a constrained type parameter recurses into the constraint, so each
	// constraint is visited at most once to avoid an infinite loop.
	if constraint := declarationData.Constraint; constraint != nil {
		constraintType := walk.ctx.TypeChecker.GetTypeAtLocation(constraint)
		if constraintType != nil && !walk.visitedConstraints[constraintType] {
			walk.visitedConstraints[constraintType] = true
			walk.visitType(constraintType, false, false)
		}
	}

	// SURVIVES MUTATION and is kept as upstream's own line. Deleting the default visit leaves every
	// verdict and every message unchanged, including on shapes built to reach it: `<T, U = T>`,
	// `<T, U = T[]>` and `<U = string>` all agree with the installed build either way, because a
	// default is written in the signature where the syntactic pass has already counted it. No
	// distinguishing input was found, so this is recorded as unmeasured rather than as equivalent.
	if defaultType := declarationData.DefaultType; defaultType != nil && !walk.visitedDefault {
		walk.visitedDefault = true
		walk.visitType(walk.ctx.TypeChecker.GetTypeAtLocation(defaultType), false, false)
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

	for _, typeArgument := range walk.ctx.TypeChecker.GetTypeArguments(subject) {
		// In a class context everything is accepted.
		thisAssumeMultipleUses := walk.fromClass || assumeMultipleUses

		if !thisAssumeMultipleUses {
			switch {
			case target != nil && target.IsTupleType():
				// A readonly tuple is considered to use the parameter once; a mutable one counts
				// multiply only when it is returned.
				thisAssumeMultipleUses = isReturnType && !target.AsTupleType().IsReadonly()
			case target != nil && walk.ctx.TypeChecker.IsArrayType(target):
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
func (walk *typeUsageWalk) visitObjectType(subject *checker.Type) {
	properties := walk.ctx.TypeChecker.GetPropertiesOfType(subject)
	walk.visitSymbolsList(properties, false)

	walk.visitType(walk.ctx.TypeChecker.GetNumberIndexType(subject), true, false)
	walk.visitType(walk.ctx.TypeChecker.GetStringIndexType(subject), true, false)

	for _, signature := range walk.ctx.TypeChecker.GetSignaturesOfType(subject, checker.SignatureKindCall) {
		walk.functionLikeType = true
		walk.visitSignature(signature)
	}

	for _, signature := range walk.ctx.TypeChecker.GetSignaturesOfType(subject, checker.SignatureKindConstruct) {
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

	// SUBSUMED IN PRACTICE, measured, and kept. Deleting this visit survives every fixture and
	// every shape probed against the installed build: `<T>(this: T): T`, `<T>(this: T, other: T)`,
	// `<T>(this: Record<'p', T>)` and upstream's own two `this` cases all keep their verdict. The
	// counts really do drop (4 to 2, and 2 to 1 on upstream's `foo<T>(this: T): void`), so the
	// branch is not inert; the drop simply never crosses the report threshold, because a `this`
	// parameter is written in the SIGNATURE, where the syntactic pass has already seen it.
	//
	// Kept because it is upstream's own line and the subsumption is arithmetic rather than
	// structural: it holds only while the threshold is 2 and while every `this` type is also
	// syntactically visible, and neither is a property this rule guarantees.
	if thisParameter := signature.ThisParameter(); thisParameter != nil {
		walk.visitType(walk.ctx.TypeChecker.GetTypeOfSymbol(thisParameter), false, false)
	}

	for _, parameter := range signature.Parameters() {
		walk.visitType(walk.ctx.TypeChecker.GetTypeOfSymbol(parameter), false, false)
	}

	for _, typeParameter := range signature.TypeParameters() {
		walk.visitType(typeParameter, false, false)
	}

	// A type predicate stands in for the return type when the signature has one, so `input is T`
	// counts its T in return position exactly as a returned `T` would.
	returnType := walk.ctx.TypeChecker.GetReturnTypeOfSignature(signature)
	if predicate := walk.ctx.TypeChecker.GetTypePredicateOfSignature(signature); predicate != nil && predicate.Type() != nil {
		returnType = predicate.Type()
	}
	walk.visitType(returnType, false, true)
}

// visitSymbolsList is upstream's `visitSymbolsListOnce` without the list-identity guard.
//
// Upstream keys that guard on the SYMBOL ARRAY's identity, which JavaScript gives it for free
// because `type.getProperties()` returns the same cached array each time. Go slices have no such
// identity, so the guard cannot be reproduced as written. The nine-visit type guard in visitType
// already bounds the recursion, which is what the list guard was protecting against.
func (walk *typeUsageWalk) visitSymbolsList(symbols []*ast.Symbol, assumeMultipleUses bool) {
	for _, symbol := range symbols {
		walk.visitType(walk.ctx.TypeChecker.GetTypeOfSymbol(symbol), assumeMultipleUses, false)
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

// isTypeReferenceType is `tsutils.isTypeReference`: an object type carrying the Reference flag.
func isTypeReferenceType(subject *checker.Type) bool {
	return checker.Type_flags(subject)&checker.TypeFlagsObject != 0 &&
		checker.Type_objectFlags(subject)&checker.ObjectFlagsReference != 0
}

func buildSoleTypeParameterMessage(name string, uses string, descriptor string) rule.Message {
	return rule.Message{
		Id:          "sole",
		Description: "Type parameter " + name + " is " + uses + " in the " + descriptor + " signature.",
	}
}

func buildReplaceUsagesWithConstraintMessage() rule.Message {
	return rule.Message{
		Id:          "replaceUsagesWithConstraint",
		Description: "Replace all usages of type parameter with its constraint.",
	}
}

// isIdentifierInTypePosition stands in for the scope manager's `reference.isTypeReference`.
//
// An identifier is a type reference when it names a type rather than declaring or denoting a value.
// The two shapes that reach this rule are the name of a type reference (`x: T`) and the left-hand
// side of a qualified type name (`T.Inner`); everything else, including a parameter's own name and
// a property access in value land, is not.
func isIdentifierInTypePosition(identifier *ast.Node) bool {
	parent := identifier.Parent
	if parent == nil {
		return false
	}

	switch parent.Kind {
	case ast.KindTypeReference:
		return parent.AsTypeReferenceNode().TypeName == identifier
	case ast.KindTypeQuery:
		// `typeof x` names a VALUE, so the identifier inside it is not a type reference. Upstream's
		// scope manager agrees, and upstream's corpus pins the consequence: `<T>(input: T) =>
		// typeof input` is clean because the parameter is used once in type position, not twice.
		return false
	case ast.KindQualifiedName:
		return parent.AsQualifiedName().Left == identifier
	case ast.KindExpressionWithTypeArguments:
		return parent.AsExpressionWithTypeArguments().Expression == identifier
	default:
		return false
	}
}
