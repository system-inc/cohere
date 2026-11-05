package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
)

// A signature from a type can have hidden a value in a value-to-void relation.
// A body proves its own result, provided every return is provably empty. Merely
// having a body is insufficient: `return hidden()` may forward a hidden value
// from a void-typed function. Cache by declaration, including an in-progress false
// answer so recursive forwarding stays conservative.
type voidBodyProof struct {
	checker *checker.Checker
	empty   map[*ast.Node]bool
}

func (proof *voidBodyProof) signatureEmpty(signature *checker.Signature) bool {
	if signature == nil {
		return false
	}
	declaration := signature.Declaration()
	if declaration == nil || !ast.IsFunctionLike(declaration) || declaration.Body() == nil {
		return false
	}
	if empty, seen := proof.empty[declaration]; seen {
		return empty
	}
	proof.empty[declaration] = false
	body := declaration.Body()
	empty := true
	if !ast.IsBlock(body) {
		empty = proof.expressionEmpty(body, 0)
	} else {
		ast.ForEachReturnStatement(body, func(statement *ast.Node) bool {
			expression := statement.AsReturnStatement().Expression
			if expression != nil && !proof.expressionEmpty(expression, 0) {
				empty = false
			}
			return !empty
		})
	}
	proof.empty[declaration] = empty
	return empty
}

func (proof *voidBodyProof) expressionEmpty(expression *ast.Node, depth int) bool {
	if expression == nil || depth > 8 {
		return false
	}
	unwrapped := ast.SkipParentheses(expression)
	if unwrapped.Kind == ast.KindVoidExpression {
		return true
	}
	own := proof.checker.GetTypeAtLocation(unwrapped)
	if own != nil && own.Flags()&(checker.TypeFlagsUndefined|checker.TypeFlagsNever) != 0 {
		return true
	}
	switch unwrapped.Kind {
	case ast.KindCallExpression:
		return proof.signatureEmpty(proof.checker.GetResolvedSignature(unwrapped))
	case ast.KindAwaitExpression:
		return proof.expressionEmpty(unwrapped.AsAwaitExpression().Expression, depth+1)
	case ast.KindIdentifier:
		// Only an inferred const retains its initializer's provenance. An annotation
		// could already have erased a value, and a mutable binding could be replaced.
		symbol := proof.checker.GetSymbolAtLocation(unwrapped)
		if symbol == nil ||
			symbol.ValueDeclaration == nil ||
			symbol.ValueDeclaration.Kind != ast.KindVariableDeclaration {
			return false
		}
		declaration := symbol.ValueDeclaration
		variable := declaration.AsVariableDeclaration()
		return declaration.Parent != nil && declaration.Parent.Flags&ast.NodeFlagsConst != 0 && variable.Type == nil &&
			proof.expressionEmpty(variable.Initializer, depth+1)
	}
	return false
}

// A Promise value's type alone does not remember its producer. At the top use
// the expression's resolved signature; for a function's return use the source
// signature at that path. Container steps preserve that origin. Contravariant
// parameter paths and unresolved signatures remain conservative.
func (proof *voidBodyProof) promiseOriginEmpty(node *ast.Node, source *checker.Type, path []flow.Step) bool {
	if len(path) == 0 {
		return proof.expressionEmpty(node, 0)
	}
	walked := source
	for index, step := range path {
		if walked == nil {
			return false
		}
		switch step.Kind {
		case flow.StepReturn:
			signatures := checker.Checker_getSignaturesOfType(proof.checker, walked, checker.SignatureKindCall)
			if len(signatures) != 1 {
				return false
			}
			if index == len(path)-1 {
				return proof.signatureEmpty(signatures[0])
			}
			walked = checker.Checker_getReturnTypeOfSignature(proof.checker, signatures[0])
		case flow.StepProperty:
			property := checker.Checker_getPropertyOfType(proof.checker, walked, step.Name)
			if property == nil {
				return false
			}
			walked = checker.Checker_getTypeOfSymbol(proof.checker, property)
		case flow.StepElement, flow.StepTypeArgument:
			// The walker may have selected a union member or expanded a constraint.
			// Its original root then has no reference arguments we can safely inspect.
			if walked.Flags()&checker.TypeFlagsObject == 0 || walked.ObjectFlags()&checker.ObjectFlagsReference == 0 {
				return false
			}
			arguments := checker.Checker_getTypeArguments(proof.checker, walked)
			argument := step.Index
			if argument < 0 {
				argument = 0
			}
			if argument >= len(arguments) {
				return false
			}
			walked = arguments[argument]
		default:
			return false
		}
	}
	return false
}
