// Source: tsgolint `internal/utilities/ts_eslint.go`.
//
// The checker-aware queries typescript-eslint's own rules lean on: constraint resolution, awaitability,
// contextual and enum types, the unsafe-assignment walk, and the precedence questions a fix needs
// before it can add or drop parentheses.
package type_checking

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/rule"
)

type ConstraintTypeInfo struct {
	ConstraintType  *checker.Type
	IsTypeParameter bool
}

/**
 * Returns whether the type is a generic and what its constraint is.
 *
 * If the type is not a generic, `isTypeParameter` will be `false`, and
 * `constraintType` will be the same as the input type.
 *
 * If the type is a generic, and it is constrained, `isTypeParameter` will be
 * `true`, and `constraintType` will be the constraint type.
 *
 * If the type is a generic, but it is not constrained, `constraintType` will be
 * `undefined` (rather than an `unknown` type), due to https://github.com/microsoft/TypeScript/issues/60475
 *
 * Successor to {@link getConstrainedTypeAtLocation} due to
 * https://github.com/typescript-eslint/typescript-eslint/issues/10438
 *
 * This is considered internal since it is unstable for now and may have breaking changes at any time.
 * Use at your own risk.
 *
 * @internal
 *
 */
func GetConstraintInfo(
	typeChecker *checker.Checker,
	subjectType *checker.Type,
) (constraintType *checker.Type, isTypeParameter bool) {
	if checker.Type_flags(subjectType)&checker.TypeFlagsTypeParameter != 0 {
		return checker.Checker_getBaseConstraintOfType(typeChecker, subjectType), true
	}
	return subjectType, false
}

type TypeAwaitable int32

const (
	TypeAwaitableAlways TypeAwaitable = iota
	TypeAwaitableNever
	TypeAwaitableMay
)

func NeedsToBeAwaited(
	typeChecker *checker.Checker,
	node *ast.Node,
	subjectType *checker.Type,
) TypeAwaitable {
	constraintType, isTypeParameter := GetConstraintInfo(typeChecker, subjectType)

	// unconstrained generic types should be treated as unknown
	if isTypeParameter && constraintType == nil {
		return TypeAwaitableMay
	}

	// `any` and `unknown` types may need to be awaited
	if IsTypeAnyType(constraintType) || IsTypeUnknownType(constraintType) {
		return TypeAwaitableMay
	}

	// 'thenable' values should always be be awaited
	if IsThenableType(typeChecker, node, constraintType) {
		return TypeAwaitableAlways
	}

	// anything else should not be awaited
	return TypeAwaitableNever
}

func GetConstrainedTypeAtLocation(typeChecker *checker.Checker, node *ast.Node) *checker.Type {
	nodeType := typeChecker.GetTypeAtLocation(node)

	constraint := checker.Checker_getBaseConstraintOfType(typeChecker, nodeType)
	if constraint != nil {
		return constraint
	}

	return nodeType
}

/**
 * Get the type name of a given type.
 * @param typeChecker The context sensitive TypeScript TypeChecker.
 * @param type The type to get the name of.
 */
func GetTypeName(
	typeChecker *checker.Checker,
	subjectType *checker.Type,
) string {
	// It handles `string` and string literal types as string.
	if checker.Type_flags(subjectType)&checker.TypeFlagsStringLike != 0 {
		return "string"
	}

	// If the type is a type parameter which extends primitive string types,
	// but it was not recognized as a string like. So check the constraint
	// type of the type parameter.
	if IsTypeParameter(subjectType) {
		// `type.getConstraint()` method doesn't return the constraint type of
		// the type parameter for some reason. So this gets the constraint type
		// via AST.
		symbol := checker.Type_symbol(subjectType)
		decls := symbol.Declarations
		if len(decls) > 0 {
			if ast.IsTypeParameterDeclaration(decls[0]) {
				typeParamDecl := decls[0].AsTypeParameterDeclaration()
				if typeParamDecl.Constraint != nil {
					return GetTypeName(
						typeChecker,
						checker.Checker_getTypeFromTypeNode(typeChecker, typeParamDecl.Constraint),
					)
				}
			}
		}
	}

	// If the type is a union and all types in the union are string like,
	// return `string`. For example:
	// - `"a" | "b"` is string.
	// - `string | string[]` is not string.
	if IsUnionType(subjectType) && Every(UnionTypeParts(subjectType), func(part *checker.Type) bool {
		return GetTypeName(typeChecker, part) == "string"
	}) {
		return "string"
	}

	// If the type is an intersection and a type in the intersection is string
	// like, return `string`. For example: `string & {__htmlEscaped: void}`
	if IsIntersectionType(subjectType) && Some(IntersectionTypeParts(subjectType), func(part *checker.Type) bool {
		return GetTypeName(typeChecker, part) == "string"
	}) {
		return "string"
	}

	return typeChecker.TypeToString(subjectType)
}

/**
 * Gets the location of the head of the given for statement variant for reporting.
 *
 * - `for (const foo in bar) expressionOrBlock`
 *    ^^^^^^^^^^^^^^^^^^^^^^
 *
 * - `for (const foo of bar) expressionOrBlock`
 *    ^^^^^^^^^^^^^^^^^^^^^^
 *
 * - `for await (const foo of bar) expressionOrBlock`
 *    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^
 *
 * - `for (let i = 0; i < 10; i++) expressionOrBlock`
 *    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^
 */
func GetForStatementHeadLoc(
	sourceFile *ast.SourceFile,
	node *ast.Node,
) core.TextRange {
	var statement *ast.Node
	if ast.IsForStatement(node) {
		statement = node.AsForStatement().Statement
	} else {
		statement = node.AsForInOrOfStatement().Statement
	}
	return rule.TokenRange(sourceFile, node).WithEnd(statement.Pos())
}

var arrayPredicateFunctions = []string{"every", "filter", "find", "findIndex", "findLast", "findLastIndex", "some"}

func IsArrayMethodCallWithPredicate(
	typeChecker *checker.Checker,
	node *ast.CallExpression,
) bool {
	if !ast.IsAccessExpression(node.Expression) {
		return false
	}

	propertyName, ok := checker.Checker_getAccessedPropertyName(typeChecker, node.Expression)
	if !ok || !slices.Contains(arrayPredicateFunctions, propertyName) {
		return false
	}

	subjectType := GetConstrainedTypeAtLocation(typeChecker, node.Expression.Expression())
	return TypeRecurser(subjectType, func(subjectType *checker.Type) bool {
		return checker.Checker_isArrayOrTupleType(typeChecker, subjectType)
	})
}

func IsRestParameterDeclaration(decl *ast.Declaration) bool {
	return ast.IsParameterDeclaration(decl) && decl.AsParameterDeclaration().DotDotDotToken != nil
}

/**
 * Gets the declaration for the given variable
 */
func GetDeclaration(
	typeChecker *checker.Checker,
	node *ast.Node,
) *ast.Declaration {
	symbol := typeChecker.GetSymbolAtLocation(node)
	if symbol == nil {
		return nil
	}
	if len(symbol.Declarations) > 0 {
		return symbol.Declarations[0]
	}
	return nil
}

/**
 * @returns true if the type is `any[]`
 */
func IsTypeAnyArrayType(
	subjectType *checker.Type,
	typeChecker *checker.Checker,
) bool {
	return checker.Checker_isArrayType(typeChecker, subjectType) &&
		IsTypeAnyType(checker.Checker_getTypeArguments(typeChecker, subjectType)[0])
}

/**
 * @returns true if the type is `unknown[]`
 */
func IsTypeUnknownArrayType(
	subjectType *checker.Type,
	typeChecker *checker.Checker,
) bool {
	return checker.Checker_isArrayType(typeChecker, subjectType) &&
		IsTypeUnknownType(checker.Checker_getTypeArguments(typeChecker, subjectType)[0])
}

// IsTypeReferenceType is upstream's isTypeReferenceType: an object type carrying the Reference flag,
// deferred or not.
//
// A deferred reference is one the checker builds lazily, and it builds one for every type written as a
// type alias's whole body: `type HistoriesType = Map<string, ReadingInterface[]>` gives a Map reference
// whose arguments resolve on first ask. Its target and its type arguments are those of the same reference
// written inline, so the comparison in IsUnsafeAssignment reads it the same way. Asking for a non-deferred
// reference here, as this file once did, stopped the comparison at any aliased type, and so the unsafe rules
// missed an `any` inside the type arguments of every parameter, variable or return typed through an alias
// (#chvhz8t).
func IsTypeReferenceType(subjectType *checker.Type) bool {
	return subjectType.Flags()&checker.TypeFlagsObject != 0 &&
		subjectType.ObjectFlags()&checker.ObjectFlagsReference != 0
}

// CanBeUnsafeAssignment answers whether IsUnsafeAssignment could find this sender unsafe against any
// receiver at all. It finds one unsafe only at an `any`, reached either directly or through the type
// arguments of type references, so a sender with no `any` on any such path is safe whatever the
// receiver is. A caller whose receiver type is costly to resolve (a JSX attribute's resolves the
// element's props) asks this first and skips the receiver (#hekjpw3).
func CanBeUnsafeAssignment(typeChecker *checker.Checker, sender *checker.Type) bool {
	return canBeUnsafeAssignmentWorker(typeChecker, sender, map[*checker.Type]bool{})
}

// canBeUnsafeAssignmentWorker is CanBeUnsafeAssignment's search from one type down.
//
// Mutates: visited gains each type reference searched, so the rest of the same search skips it.
func canBeUnsafeAssignmentWorker(
	typeChecker *checker.Checker,
	subjectType *checker.Type,
	visited map[*checker.Type]bool,
) bool {
	if IsTypeAnyType(subjectType) {
		return true
	}
	// A type already visited had its arguments searched in full, so reaching it again adds nothing.
	if !IsTypeReferenceType(subjectType) || visited[subjectType] {
		return false
	}
	visited[subjectType] = true
	for _, argument := range checker.Checker_getTypeArguments(typeChecker, subjectType) {
		if canBeUnsafeAssignmentWorker(typeChecker, argument, visited) {
			return true
		}
	}
	return false
}

/**
 * Does a simple check to see if there is an any being assigned to a non-any type.
 *
 * This also checks generic positions to ensure there's no unsafe sub-assignments.
 * Note: in the case of generic positions, it makes the assumption that the two types are the same.
 *
 * @example See tests for examples
 *
 * @returns false if it's safe, or an object with the two types if it's unsafe
 */
func IsUnsafeAssignment(
	subjectType *checker.Type,
	receiverT *checker.Type,
	typeChecker *checker.Checker,
	senderNode *ast.Node,
) (receiver *checker.Type, sender *checker.Type, unsafe bool) {
	return isUnsafeAssignmentWorker(
		subjectType,
		receiverT,
		typeChecker,
		senderNode,
		map[*checker.Type]*Set[*checker.Type]{},
	)
}

// isUnsafeAssignmentWorker is IsUnsafeAssignment's comparison from one sender and receiver down.
//
// Mutates: visited gains each receiver a sender was compared against, so the rest of the same comparison skips
// the pair.
func isUnsafeAssignmentWorker(
	subjectType *checker.Type,
	receiver *checker.Type,
	typeChecker *checker.Checker,
	senderNode *ast.Node,
	visited map[*checker.Type]*Set[*checker.Type],
) (*checker.Type, *checker.Type, bool) {
	if IsTypeAnyType(subjectType) {
		// Allow assignment of any ==> unknown.
		if IsTypeUnknownType(receiver) {
			return nil, nil, false
		}

		if !IsTypeAnyType(receiver) {
			return receiver, subjectType, true
		}
	}

	typeAlreadyVisited, ok := visited[subjectType]

	if ok {
		if typeAlreadyVisited.Has(receiver) {
			return nil, nil, false
		}
		typeAlreadyVisited.Add(receiver)
	} else {
		visited[subjectType] = NewSetFromItems(receiver)
	}

	if IsTypeReferenceType(subjectType) && IsTypeReferenceType(receiver) {
		// Upstream leaves open how to handle cases like this,
		// where the types are assignable, but not the same type
		/*
		   function foo(): ReadonlySet<number> { return new Set<any>(); }

		   // and

		   type Test<T> = { prop: T }
		   type Test2 = { prop: string }
		   declare const a: Test<any>;
		   const b: Test2 = a;
		*/

		if subjectType.Target() != receiver.Target() {
			// if the type references are different, assume safe, as we won't know how to compare the two types
			// the generic positions might not be equivalent for both types
			return nil, nil, false
		}

		if senderNode != nil &&
			ast.IsNewExpression(senderNode) &&
			ast.IsIdentifier(senderNode.Expression()) &&
			senderNode.Expression().Text() == "Map" &&
			len(senderNode.Arguments()) == 0 &&
			senderNode.TypeArguments() == nil {
			// special case to handle `new Map()`
			// unfortunately Map's default empty constructor is typed to return `Map<any, any>` :(
			// https://github.com/typescript-eslint/typescript-eslint/issues/2109#issuecomment-634144396
			return nil, nil, false
		}

		typeArguments := checker.Checker_getTypeArguments(typeChecker, subjectType)
		if typeArguments == nil {
			return nil, nil, false
		}
		receiverTypeArguments := checker.Checker_getTypeArguments(typeChecker, receiver)
		if receiverTypeArguments == nil {
			return nil, nil, false
		}

		for index, argument := range typeArguments {
			receiverArgument := receiverTypeArguments[index]

			_, _, unsafe := isUnsafeAssignmentWorker(argument, receiverArgument, typeChecker, senderNode, visited)
			if unsafe {
				return receiver, subjectType, true
			}
		}

		return nil, nil, false
	}

	return nil, nil, false
}

/**
 * Returns the contextual type of a given node.
 * Contextual type is the type of the target the node is going into.
 * i.e. the type of a called function's parameter, or the defined type of a variable declaration
 */
func GetContextualType(
	typeChecker *checker.Checker,
	node *ast.Node,
) *checker.Type {
	parent := node.Parent

	if ast.IsCallExpression(parent) || ast.IsNewExpression(parent) {
		if node == parent.Expression() {
			// is the callee, so has no contextual type
			return nil
		}
	} else if ast.IsVariableDeclaration(parent) ||
		ast.IsPropertyDeclaration(parent) ||
		ast.IsParameterDeclaration(parent) {
		if node := parent.Type(); node != nil {
			return checker.Checker_getTypeFromTypeNode(typeChecker, node)
		}
		return nil
	} else if parent.Kind == ast.KindJsxExpression {
		return checker.Checker_getContextualType(typeChecker, parent, checker.ContextFlagsNone)
	} else if ast.IsIdentifier(node) &&
		(ast.IsPropertyAssignment(parent) || ast.IsShorthandPropertyAssignment(parent)) {
		return checker.Checker_getContextualType(typeChecker, node, checker.ContextFlagsNone)
	} else if ast.IsBinaryExpression(parent) &&
		parent.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken &&
		parent.AsBinaryExpression().Right == node {
		// is RHS of assignment
		return typeChecker.GetTypeAtLocation(parent.AsBinaryExpression().Left)
	} else if parent.Kind != ast.KindJsxExpression && !ast.IsTemplateSpan(parent) {
		// parent is not something we know we can get the contextual type of
		return nil
	}
	// Upstream reads no return statement's contextual type here either.

	return checker.Checker_getContextualType(typeChecker, node, checker.ContextFlagsNone)
}

func GetThisExpression(
	node *ast.Node,
) *ast.Node {
	// The expression reached so far, walking in through calls and member accesses.
	current := node
	for {
		current = ast.SkipParentheses(current)

		if ast.IsCallExpression(current) {
			current = current.Expression()
		} else if current.Kind == ast.KindThisKeyword {
			return current
		} else if ast.IsAccessExpression(current) {
			current = current.Expression()
		} else {
			break
		}
	}

	return nil
}

/*
 * If passed an enum member, returns the type of the parent. Otherwise,
 * returns itself.
 *
 * For example:
 * - `Fruit` --> `Fruit`
 * - `Fruit.Apple` --> `Fruit`
 */
func getBaseEnumType(typeChecker *checker.Checker, subjectType *checker.Type) *checker.Type {
	symbol := checker.Type_symbol(subjectType)
	if !IsSymbolFlagSet(symbol, ast.SymbolFlagsEnumMember) {
		return subjectType
	}

	return typeChecker.GetTypeAtLocation(
		symbol.ValueDeclaration.Parent,
	)
}

/**
 * Retrieve only the Enum literals from a type. for example:
 * - 123 --> []
 * - {} --> []
 * - Fruit.Apple --> [Fruit.Apple]
 * - Fruit.Apple | Vegetable.Lettuce --> [Fruit.Apple, Vegetable.Lettuce]
 * - Fruit.Apple | Vegetable.Lettuce | 123 --> [Fruit.Apple, Vegetable.Lettuce]
 * - T extends Fruit --> [Fruit]
 */
func GetEnumLiterals(subjectType *checker.Type) []*checker.Type {
	return Filter(
		UnionTypeParts(subjectType),
		func(subType *checker.Type) bool {
			return IsTypeFlagSet(subType, checker.TypeFlagsEnumLiteral)
		},
	)
}

/**
 * A type can have 0 or more enum types. For example:
 * - 123 --> []
 * - {} --> []
 * - Fruit.Apple --> [Fruit]
 * - Fruit.Apple | Vegetable.Lettuce --> [Fruit, Vegetable]
 * - Fruit.Apple | Vegetable.Lettuce | 123 --> [Fruit, Vegetable]
 * - T extends Fruit --> [Fruit]
 */
func GetEnumTypes(
	typeChecker *checker.Checker,
	subjectType *checker.Type,
) []*checker.Type {
	return Map(
		GetEnumLiterals(subjectType),
		func(literal *checker.Type) *checker.Type { return getBaseEnumType(typeChecker, literal) },
	)
}

type DiscriminatedAnyType uint8

const (
	DiscriminatedAnyTypeAny DiscriminatedAnyType = iota
	DiscriminatedAnyTypePromiseAny
	DiscriminatedAnyTypeAnyArray
	DiscriminatedAnyTypeSafe
)

/**
 * @returns `DiscriminatedAnyTypeAny` if the type is `any`, `DiscriminatedAnyTypeAnyArray` if the type is `any[]` or
 *          `readonly any[]`, `DiscriminatedAnyTypePromiseAny` if the type is `Promise<any>`,
 *          otherwise it returns `DiscriminatedAnyTypeSafe`.
 */
func DiscriminateAnyType(
	subjectType *checker.Type,
	typeChecker *checker.Checker,
	program rule.Program,
	node *ast.Node,
) DiscriminatedAnyType {
	return discriminateAnyTypeWorker(subjectType, typeChecker, program, node, NewSetFromItems[*checker.Type]())
}

func discriminateAnyTypeWorker(
	subjectType *checker.Type,
	typeChecker *checker.Checker,
	program rule.Program,
	node *ast.Node,
	// visited holds every type this walk has reached, so a type met again on the way down through awaited
	// types answers safe rather than being walked a second time.
	visited *Set[*checker.Type],
) DiscriminatedAnyType {
	if visited.Has(subjectType) {
		return DiscriminatedAnyTypeSafe
	}
	visited.Add(subjectType)
	if IsTypeAnyType(subjectType) {
		return DiscriminatedAnyTypeAny
	}
	if IsTypeAnyArrayType(subjectType, typeChecker) {
		return DiscriminatedAnyTypeAnyArray
	}

	foundPromiseAny := TypeRecurser(subjectType, func(subjectType *checker.Type) bool {
		if !IsThenableType(typeChecker, node, subjectType) {
			return false
		}
		awaitedType := checker.Checker_getAwaitedType(typeChecker, subjectType)
		if awaitedType == nil {
			return false
		}
		awaitedAnyType := discriminateAnyTypeWorker(awaitedType, typeChecker, program, node, visited)
		return awaitedAnyType == DiscriminatedAnyTypeAny
	})

	if foundPromiseAny {
		return DiscriminatedAnyTypePromiseAny
	}

	return DiscriminatedAnyTypeSafe
}

func GetParentFunctionNode(
	node *ast.Node,
) *ast.Node {
	current := node.Parent
	for current != nil {
		if ast.IsFunctionLikeDeclaration(current) {
			return current
		}

		current = current.Parent
	}

	return nil
}

func IsHigherPrecedenceThanAwait(node *ast.Node) bool {
	nodePrecedence := ast.GetExpressionPrecedence(node)
	awaitPrecedence := ast.GetOperatorPrecedence(
		ast.KindAwaitExpression,
		ast.KindUnknown,
		ast.OperatorPrecedenceFlagsNone,
	)
	return nodePrecedence > awaitPrecedence
}

func IsStrongPrecedenceNode(innerNode *ast.Node) bool {
	return ast.IsLiteralKind(innerNode.Kind) ||
		ast.IsBooleanLiteral(innerNode) ||
		ast.IsParenthesizedExpression(innerNode) ||
		innerNode.Kind == ast.KindIdentifier ||
		innerNode.Kind == ast.KindTypeReference ||
		innerNode.Kind == ast.KindTypeOperator ||
		innerNode.Kind == ast.KindArrayLiteralExpression ||
		innerNode.Kind == ast.KindObjectLiteralExpression ||
		innerNode.Kind == ast.KindPropertyAccessExpression ||
		innerNode.Kind == ast.KindElementAccessExpression ||
		innerNode.Kind == ast.KindCallExpression ||
		innerNode.Kind == ast.KindNewExpression ||
		innerNode.Kind == ast.KindTaggedTemplateExpression ||
		innerNode.Kind == ast.KindExpressionWithTypeArguments
}

func IsParenlessArrowFunction(node *ast.Node) bool {
	if !ast.IsArrowFunction(node) {
		return false
	}

	function := node.AsArrowFunction()

	return function.Parameters.End() == function.EqualsGreaterThanToken.Pos()
}

type MemberNameType uint8

const (
	MemberNameTypePrivate MemberNameType = iota
	MemberNameTypeQuoted
	MemberNameTypeNormal
	MemberNameTypeExpression
)

/**
 * Gets a string name representation of the name of the given MethodDefinition
 * or PropertyDefinition node, with handling for computed property names.
 */
func GetNameFromMember(sourceFile *ast.SourceFile, member *ast.Node) (string, MemberNameType) {
	switch member.Kind {
	case ast.KindIdentifier:
		return member.AsIdentifier().Text, MemberNameTypeNormal
	case ast.KindPrivateIdentifier:
		return member.AsPrivateIdentifier().Text, MemberNameTypePrivate
	case ast.KindComputedPropertyName:
		expression := member.AsComputedPropertyName().Expression
		// A literal key is named by its text. A keyword key, such as true or null, falls through to the
		// computed name's source text, as any other expression does.
		if ast.IsLiteralExpression(expression) {
			text := expression.Text()
			if !scanner.IsValidIdentifier(text) {
				return "\"" + text + "\"", MemberNameTypeQuoted
			}
			return text, MemberNameTypeNormal
		}
	}

	textRange := rule.TokenRange(sourceFile, member)
	return sourceFile.Text()[textRange.Pos():textRange.End()], MemberNameTypeExpression
}
