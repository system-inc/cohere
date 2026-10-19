package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

var unionWriteText = policy.MessageOf("adamic/union-write", "unionWrite")

// UnionWrite checks the write itself, rather than a value's flow into a mutable
// view (invariant-mutable). A value can match one union member exactly and still
// be corrupted through another member's writable property (#wcvkh0d).
//
// The receiver's flow type matters: narrowing to one member makes its writes
// ordinary tsc checks. Reading an intermediate property produces the union of
// its types, so the same check covers arbitrary target depth without recovering
// declared types and undoing narrowing. A generic receiver uses its constraint.
// Each refused syntax has a strict-tsc-accepted, Node-failing testdata probe.
var UnionWrite = rule.Rule{
	Name:             "adamic/union-write",
	NeedsTypeChecker: true,
	ProgramReads:     rule.ReadsCompilerOptions | rule.ReadsDefaultLibrary,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil || ctx.SourceFile == nil {
			return nil
		}
		check := func(target *ast.Node, value *checker.Type) {
			target = ast.SkipParentheses(target)
			var receiver *ast.Node
			switch target.Kind {
			case ast.KindPropertyAccessExpression:
				receiver = target.AsPropertyAccessExpression().Expression
			case ast.KindElementAccessExpression:
				receiver = target.AsElementAccessExpression().Expression
			default:
				return
			}
			typeChecker := ctx.TypeChecker
			receiverType := unionWriteConstraint(typeChecker, typeChecker.GetTypeAtLocation(receiver))
			if receiverType == nil || receiverType.Flags()&checker.TypeFlagsUnion == 0 {
				return
			}
			for _, member := range receiverType.AsUnionType().Types() {
				for _, slot := range unionWriteSlots(typeChecker, member, target) {
					if !checker.Checker_isTypeAssignableTo(typeChecker, value, slot) {
						span := rule.TokenRange(ctx.SourceFile, target)
						ctx.ReportNode(target, rule.Message{Id: "unionWrite", Description: unionWriteText.Render(map[string]string{
							"slot":   ctx.SourceFile.Text()[span.Pos():span.End()],
							"source": type_checking.StableTypeText(typeChecker, value),
							"member": type_checking.StableTypeText(typeChecker, member),
							"target": type_checking.StableTypeText(typeChecker, slot),
						})})
						return
					}
				}
			}
		}
		update := func(node, operand *ast.Node, operator ast.Kind) {
			if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
				check(operand, ctx.TypeChecker.GetTypeAtLocation(node))
			}
		}
		return rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				operator := binary.OperatorToken.Kind
				if !ast.IsAssignmentOperator(operator) {
					return
				}
				value := binary.Right
				// A logical assignment writes only its RHS. The expression's type also
				// contains the old value, which need not fit another member's slot.
				switch operator {
				case ast.KindEqualsToken, ast.KindQuestionQuestionEqualsToken, ast.KindBarBarEqualsToken, ast.KindAmpersandAmpersandEqualsToken:
				default:
					value = node // arithmetic compound assignment writes the operation's result
				}
				check(binary.Left, ctx.TypeChecker.GetTypeAtLocation(value))
			},
			ast.KindPrefixUnaryExpression: func(node *ast.Node) {
				unary := node.AsPrefixUnaryExpression()
				update(node, unary.Operand, unary.Operator)
			},
			ast.KindPostfixUnaryExpression: func(node *ast.Node) {
				unary := node.AsPostfixUnaryExpression()
				update(node, unary.Operand, unary.Operator)
			},
		}
	},
}

func unionWriteConstraint(typeChecker *checker.Checker, value *checker.Type) *checker.Type {
	if value != nil && value.Flags()&checker.TypeFlagsTypeVariable != 0 {
		if constraint := checker.Checker_getBaseConstraintOfType(typeChecker, value); constraint != nil {
			return constraint
		}
	}
	return value
}

// A computed key may itself be a union. Every possible key must fit every
// receiver member. Literal keys use the property's type (not a tuple's combined
// numeric index type); general keys use the corresponding index signature.
func unionWriteSlots(typeChecker *checker.Checker, member *checker.Type, target *ast.Node) []*checker.Type {
	if name, known := checker.Checker_getAccessedPropertyName(typeChecker, target); known {
		if property := checker.Checker_getPropertyOfType(typeChecker, member, name); property != nil {
			return []*checker.Type{checker.Checker_getWriteTypeOfSymbol(typeChecker, property)}
		}
	}
	if target.Kind != ast.KindElementAccessExpression {
		return nil
	}
	key := unionWriteConstraint(typeChecker, typeChecker.GetTypeAtLocation(target.AsElementAccessExpression().ArgumentExpression))
	keys := []*checker.Type{key}
	if key.Flags()&checker.TypeFlagsUnion != 0 {
		keys = key.AsUnionType().Types()
	}
	slots := make([]*checker.Type, 0, len(keys))
	for _, key := range keys {
		if key.Flags()&checker.TypeFlagsStringOrNumberLiteralOrUnique != 0 {
			if property := checker.Checker_getPropertyOfType(typeChecker, member, checker.GetPropertyNameFromType(key)); property != nil {
				slots = append(slots, checker.Checker_getWriteTypeOfSymbol(typeChecker, property))
				continue
			}
		}
		if index := checker.Checker_getApplicableIndexInfo(typeChecker, member, key); index != nil {
			slots = append(slots, index.ValueType())
		}
	}
	return slots
}
