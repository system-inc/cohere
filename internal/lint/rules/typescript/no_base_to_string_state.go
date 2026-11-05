package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noBaseToStringState carries what every step of the recursion needs.
//
// A struct rather than a closure chain because the recursion is deep and threading four values
// through eight functions obscures which of them actually decide anything.
type noBaseToStringState struct {
	ctx      rule.Context
	settings NoBaseToStringOptions
}

// checkExpression is upstream's `checkExpression`.
//
// A literal is skipped outright: its string form is whatever the author wrote.
func (state *noBaseToStringState) checkExpression(node *ast.Node, knownType *checker.Type) {
	// The parenthesis unwrap, which upstream has no counterpart for because its parser folds the
	// node away before the rule runs. Two things here depend on it and they fail differently:
	//
	//	the literal skip below     `('x')` would not be recognized as a literal
	//	the message text           it interpolates getText(node), so `({}).toString()` renders the
	//	                           offending expression as `({})` where upstream renders `{}`
	//
	// The second is what upstream's own corpus catches: two of its cases write exactly that shape
	// and record the name without the parentheses. Written as a loop because `((x))` nests, and
	// deliberately not ast.SkipParentheses, which dereferences its argument.
	unwrapped := node
	for unwrapped != nil && unwrapped.Kind == ast.KindParenthesizedExpression {
		inner := unwrapped.AsParenthesizedExpression().Expression
		if inner == nil {
			break
		}
		unwrapped = inner
	}
	if unwrapped == nil {
		return
	}

	// Upstream's literal skip, subsumed here and kept anyway.
	//
	// A mutation removing it survives every fixture, so the rule was run with and without the branch
	// over a regular expression, a string, a number, a boolean and a template, and both versions
	// answered zero findings on all of them. Every literal type declares its own coercion method, so
	// the base question below already answers Always and this never decides anything.
	//
	// Kept because it is upstream's, because it declines in one kind test what otherwise costs a type
	// query and a walk, and because the premise is a library declaration rather than this rule's: the
	// same shape as the boolean carve-out above.
	if isNoBaseToStringLiteral(unwrapped) {
		return
	}

	subjectType := knownType
	if subjectType == nil {
		subjectType = state.ctx.TypeChecker.GetTypeAtLocation(unwrapped)
	}
	if subjectType == nil {
		return
	}

	certainty := state.collectToStringCertainty(subjectType, nil)
	if certainty == noBaseToStringAlways {
		return
	}

	nodeRange := rule.TokenRange(state.ctx.SourceFile, unwrapped)
	name := state.ctx.SourceFile.Text()[nodeRange.Pos():nodeRange.End()]
	state.ctx.ReportNode(unwrapped, rule.Message{
		Id: "baseToString",
		Description: "'" + name + "' " + string(certainty) +
			" use Object's default stringification format ('[object Object]') when stringified.",
	})
}

// checkExpressionForArrayJoin is upstream's `checkExpressionForArrayJoin`.
func (state *noBaseToStringState) checkExpressionForArrayJoin(node *ast.Node, subjectType *checker.Type) {
	certainty := state.collectJoinCertainty(subjectType, nil)
	if certainty == noBaseToStringAlways {
		return
	}

	nodeRange := rule.TokenRange(state.ctx.SourceFile, node)
	name := state.ctx.SourceFile.Text()[nodeRange.Pos():nodeRange.End()]
	state.ctx.ReportNode(node, rule.Message{
		Id: "baseArrayJoin",
		Description: "Using `join()` for " + name + " " + string(certainty) +
			" use Object's default stringification format ('[object Object]') when stringified.",
	})
}

// collectJoinCertainty is upstream's `collectJoinCertainty`.
//
// Joining is only interesting for something array-shaped. Anything else is Always, because `join` on
// a non-array is either a user-defined method or a type error the compiler already reports.
func (state *noBaseToStringState) collectJoinCertainty(
	subjectType *checker.Type,
	visited []*checker.Type,
) noBaseToStringUsefulness {
	if type_checking.IsUnionType(subjectType) {
		return combineNoBaseToStringUnion(type_checking.UnionTypeParts(subjectType),
			func(part *checker.Type) noBaseToStringUsefulness {
				return state.collectJoinCertainty(part, visited)
			})
	}
	if type_checking.IsIntersectionType(subjectType) {
		return combineNoBaseToStringIntersection(type_checking.IntersectionTypeParts(subjectType),
			func(part *checker.Type) noBaseToStringUsefulness {
				return state.collectJoinCertainty(part, visited)
			})
	}
	if checker.IsTupleType(subjectType) {
		return state.collectTupleCertainty(subjectType, visited)
	}
	if checker.Checker_isArrayType(state.ctx.TypeChecker, subjectType) {
		return state.collectArrayCertainty(subjectType, visited)
	}
	return noBaseToStringAlways
}

// collectTupleCertainty is upstream's `collectTupleCertainty`.
//
// The combining rule differs from a union's on purpose: every element of a tuple is stringified, so
// any useless element makes the join useless, where a union only takes one branch.
func (state *noBaseToStringState) collectTupleCertainty(
	subjectType *checker.Type,
	visited []*checker.Type,
) noBaseToStringUsefulness {
	sawSometimes := false
	for _, argument := range checker.Checker_getTypeArguments(state.ctx.TypeChecker, subjectType) {
		switch state.collectToStringCertainty(argument, visited) {
		case noBaseToStringNever:
			return noBaseToStringNever
		case noBaseToStringSometimes:
			sawSometimes = true
		}
	}
	if sawSometimes {
		return noBaseToStringSometimes
	}
	return noBaseToStringAlways
}

// collectArrayCertainty is upstream's `collectArrayCertainty`.
//
// Upstream asserts the number index type is present and would throw if it were not. Ours declines
// instead: an assertion that cannot fail in upstream's tests can still fail on a shape our checker
// produces, and taking the whole run down over it would cost every rule this file.
func (state *noBaseToStringState) collectArrayCertainty(
	subjectType *checker.Type,
	visited []*checker.Type,
) noBaseToStringUsefulness {
	elementType := type_checking.GetNumberIndexType(state.ctx.TypeChecker, subjectType)
	if elementType == nil {
		return noBaseToStringAlways
	}
	return state.collectToStringCertainty(elementType, visited)
}

// collectToStringCertainty is upstream's `collectToStringCertainty`, the heart of the rule.
func (state *noBaseToStringState) collectToStringCertainty(
	subjectType *checker.Type,
	visited []*checker.Type,
) noBaseToStringUsefulness {
	// The visited set exists for a self-referential array or tuple, where the recursion would
	// otherwise not terminate. A slice rather than a map: these are single-digit lengths and a
	// linear scan avoids allocating a map per recursion.
	for _, seen := range visited {
		if seen == subjectType {
			return noBaseToStringAlways
		}
	}

	if type_checking.IsTypeParameter(subjectType) {
		constraint := checker.Checker_getBaseConstraintOfType(state.ctx.TypeChecker, subjectType)
		if constraint != nil {
			return state.collectToStringCertainty(constraint, visited)
		}
		// An unconstrained parameter is `unknown`, so it follows the same option.
		if state.settings.CheckUnknown {
			return noBaseToStringSometimes
		}
		return noBaseToStringAlways
	}

	// Upstream's comment says the Boolean type definition is missing toString(), so without this the
	// base question would answer Never for a boolean and report on `${true}`.
	//
	// It is subsumed here and kept anyway. A mutation removing it survives every fixture, which sent
	// this to a direct comparison rather than to an equivalence argument: the rule was run with and
	// without the branch over a boolean, a boolean literal, a boolean under String(), and an array of
	// booleans joined, and both versions answered zero findings on all four. Our lib declares
	// `toString` on the `Boolean` interface, so the Object-interface test below already answers
	// Always and this branch never decides anything.
	//
	// Kept because it is upstream's and because the premise belongs to a library declaration rather
	// than to this rule: a lib revision that dropped that declaration would turn this from redundant
	// into the only thing standing between the rule and a false positive on every interpolated
	// boolean.
	if type_checking.IsTypeFlagSet(subjectType,
		checker.TypeFlagsBoolean|checker.TypeFlagsBooleanLiteral) {
		return noBaseToStringAlways
	}

	if state.isIgnoredTypeName(subjectType) {
		return noBaseToStringAlways
	}

	if type_checking.IsIntersectionType(subjectType) {
		return combineNoBaseToStringIntersection(type_checking.IntersectionTypeParts(subjectType),
			func(part *checker.Type) noBaseToStringUsefulness {
				return state.collectToStringCertainty(part, visited)
			})
	}
	if type_checking.IsUnionType(subjectType) {
		return combineNoBaseToStringUnion(type_checking.UnionTypeParts(subjectType),
			func(part *checker.Type) noBaseToStringUsefulness {
				return state.collectToStringCertainty(part, visited)
			})
	}
	if checker.IsTupleType(subjectType) {
		return state.collectTupleCertainty(subjectType, append(visited, subjectType))
	}
	if checker.Checker_isArrayType(state.ctx.TypeChecker, subjectType) {
		return state.collectArrayCertainty(subjectType, append(visited, subjectType))
	}

	coercion, known := state.isToStringLikeFromObject(subjectType)
	if !known {
		// No coercion method at all, which covers `any` and `unknown`. Only `unknown` is reportable,
		// and only under the option.
		if state.settings.CheckUnknown &&
			checker.Type_flags(subjectType) == checker.TypeFlagsUnknown {
			return noBaseToStringSometimes
		}
		return noBaseToStringAlways
	}
	if coercion {
		return noBaseToStringNever
	}
	return noBaseToStringAlways
}

// isIgnoredTypeName is upstream's two ignoredTypeNames checks.
//
// The first covers a generic ignored type, where the type as written carries type arguments and the
// name has to be read off the alias or the symbol rather than off the rendered form. The second is
// the ordinary case and also walks base types, so a class extending Error is ignored too.
func (state *noBaseToStringState) isIgnoredTypeName(subjectType *checker.Type) bool {
	symbol := noBaseToStringAliasOrSymbol(subjectType)
	if symbol != nil && len(symbol.Declarations) > 0 {
		declaration := symbol.Declarations[0]
		if noBaseToStringDeclarationCanHaveTypeParameters(declaration) &&
			noBaseToStringDeclarationHasTypeParameters(declaration) &&
			noBaseToStringNameIsIgnored(state.settings.IgnoredTypeNames, symbol.Name) {
			return true
		}
	}

	return state.matchesTypeOrBaseType(subjectType, nil)
}

// matchesTypeOrBaseType is upstream's `matchesTypeOrBaseType` with the ignored-name predicate inlined.
//
// It walks base types, so a class extending an ignored one is ignored as well. The visited list
// guards against a cycle in a heritage chain, which error recovery can produce.
func (state *noBaseToStringState) matchesTypeOrBaseType(
	subjectType *checker.Type,
	visited []*checker.Type,
) bool {
	for _, seen := range visited {
		if seen == subjectType {
			return false
		}
	}
	if noBaseToStringNameIsIgnored(state.settings.IgnoredTypeNames,
		type_checking.GetTypeName(state.ctx.TypeChecker, subjectType)) {
		return true
	}
	visitedWithSubject := append(visited, subjectType)
	for _, baseType := range state.baseTypesOf(subjectType) {
		if state.matchesTypeOrBaseType(baseType, visitedWithSubject) {
			return true
		}
	}
	return false
}

// baseTypesOf reaches a class's bases, including through a generic instantiation.
//
// `Checker_getBaseTypes` answers nothing for an instantiated generic: measured, `Boom` reports one
// base and walks to `Error`, while `Boom<number>` and `Boom<T>` report zero and the walk stops
// immediately. Upstream never sees this because its `getBaseTypes` resolves through the reference
// itself, so the whole heritage chain is available to it for free.
//
// The recovery is the one a sibling rule already uses: an instantiated generic is a non-deferred type
// reference whose `Target()` is the declared type, and the bases live on the target. Without it a
// class extending `Error` through a generic subclass is not recognized as ignored, which is one of
// upstream's own passing cases and would be a false positive on every generic error class in a tree.
func (state *noBaseToStringState) baseTypesOf(subjectType *checker.Type) []*checker.Type {
	target := subjectType
	if checker.IsNonDeferredTypeReference(subjectType) {
		if referenced := subjectType.Target(); referenced != nil {
			target = referenced
		}
	}
	if checker.Type_objectFlags(target)&
		(checker.ObjectFlagsInterface|checker.ObjectFlagsClass) == 0 {
		return nil
	}
	return checker.Checker_getBaseTypes(state.ctx.TypeChecker, target)
}

// isToStringLikeFromObject is upstream's `isToStringLikeFromObject`.
//
// The three returns are yes, no, and "no coercion method found at all", which the caller reads as
// `any` or `unknown`. The second return says whether the first is meaningful.
func (state *noBaseToStringState) isToStringLikeFromObject(subjectType *checker.Type) (bool, bool) {
	// An explicit [Symbol.toPrimitive] is by construction user-defined, so nothing else matters.
	for _, property := range checker.Checker_getPropertiesOfType(state.ctx.TypeChecker, subjectType) {
		if property.ValueDeclaration != nil &&
			state.isSymbolToPrimitiveMethod(property.ValueDeclaration) {
			return false, true
		}
	}

	foundFallbackOnObject := false
	for _, propertyName := range []string{"toLocaleString", "toString", "valueOf"} {
		candidate := checker.Checker_getPropertyOfType(state.ctx.TypeChecker, subjectType, propertyName)
		if candidate == nil || len(candidate.Declarations) == 0 {
			continue
		}

		// A declaration from anywhere other than the `Object` interface means somebody wrote their
		// own coercion, so the type is useful.
		for _, declaration := range candidate.Declarations {
			if !noBaseToStringDeclaredOnObjectInterface(declaration) {
				return false, true
			}
		}
		foundFallbackOnObject = true
	}

	if foundFallbackOnObject {
		return true, true
	}
	return false, false
}

// isSymbolToPrimitiveMethod is upstream's `isSymbolToPrimitiveMethod`.
//
// It matches a method signature whose name is the computed `[Symbol.toPrimitive]`, and requires that
// `Symbol` resolve to the builtin rather than to a local of that name.
func (state *noBaseToStringState) isSymbolToPrimitiveMethod(declaration *ast.Node) bool {
	if declaration.Kind != ast.KindMethodSignature {
		return false
	}
	name := declaration.AsMethodSignatureDeclaration().Name()
	if name == nil || name.Kind != ast.KindComputedPropertyName {
		return false
	}
	computed := name.AsComputedPropertyName().Expression
	if computed == nil || computed.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := computed.AsPropertyAccessExpression()
	if access.Expression == nil || access.Expression.Kind != ast.KindIdentifier ||
		access.Expression.AsIdentifier().Text != "Symbol" {
		return false
	}
	if access.Name() == nil || access.Name().Kind != ast.KindIdentifier ||
		access.Name().AsIdentifier().Text != "toPrimitive" {
		return false
	}
	symbol := state.ctx.TypeChecker.GetSymbolAtLocation(access.Expression)
	return symbol != nil && noBaseToStringIsDefaultLibrarySymbol(state.ctx, symbol)
}

// isBuiltinStringCall is upstream's `isBuiltInStringCall`.
//
// Upstream asks scope analysis whether any variable named `String` is declared, and treats "none" as
// the global. We have no scope index, so the symbol is resolved and its declarations examined: a
// `String` declared in this file is a shadow, and one declared only in the default library is the
// builtin. That is the complement of the naive predicate, which the brief warns answers false for
// exactly the globals a rule like this must recognize.
func (state *noBaseToStringState) isBuiltinStringCall(call *ast.CallExpression) bool {
	callee := call.Expression
	if callee == nil || callee.Kind != ast.KindIdentifier ||
		callee.AsIdentifier().Text != "String" {
		return false
	}
	if call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
		return false
	}

	symbol := state.ctx.TypeChecker.GetSymbolAtLocation(callee)
	if symbol == nil {
		// Unresolvable, so nothing says it is a shadow.
		return true
	}
	for _, declaration := range symbol.Declarations {
		if !noBaseToStringIsDefaultLibraryFile(state.ctx, ast.GetSourceFileOfNode(declaration)) {
			return false
		}
	}
	return true
}
