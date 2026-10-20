package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
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
		checkSlot := func(target, receiver *ast.Node, value *checker.Type, name *string) {
			if value == nil {
				return
			}
			typeChecker := ctx.TypeChecker
			receiverType := unionWriteConstraint(typeChecker, typeChecker.GetTypeAtLocation(receiver))
			if receiverType == nil || receiverType.Flags()&checker.TypeFlagsUnion == 0 {
				return
			}
			for _, member := range receiverType.AsUnionType().Types() {
				var slots []*checker.Type
				if name != nil {
					if property := checker.Checker_getPropertyOfType(typeChecker, member, *name); property != nil {
						slots = []*checker.Type{checker.Checker_getWriteTypeOfSymbol(typeChecker, property)}
					}
				} else {
					slots = unionWriteSlots(typeChecker, member, target)
				}
				for _, slot := range slots {
					if !checker.Checker_isTypeAssignableTo(typeChecker, value, slot) {
						span := rule.TokenRange(ctx.SourceFile, target)
						slotName := ctx.SourceFile.Text()[span.Pos():span.End()]
						if name != nil {
							slotName += "." + *name
						}
						ctx.ReportNode(target, rule.Message{Id: "unionWrite", Description: unionWriteText.Render(map[string]string{
							"slot":   slotName,
							"source": type_checking.StableTypeText(typeChecker, value),
							"member": type_checking.StableTypeText(typeChecker, member),
							"target": type_checking.StableTypeText(typeChecker, slot),
						})})
						return
					}
				}
			}
		}
		check := func(target *ast.Node, value *checker.Type) {
			target = ast.SkipParentheses(target)
			switch target.Kind {
			case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
				checkSlot(target, target.Expression(), value, nil)
			}
		}
		// The compiler projects the actual source part through nested patterns,
		// tuples, iterables and defaults. Do not substitute the target's read type.
		patternWrite := func(node *ast.Node) {
			parent := node.Parent
			if parent == nil || parent.Kind == ast.KindBinaryExpression ||
				parent.Kind == ast.KindSpreadElement || parent.Kind == ast.KindSpreadAssignment {
				return
			}
			if flow.IsDestructuringTarget(node) {
				check(node, checker.Checker_getAssignedType(ctx.TypeChecker, node))
			}
		}
		restWrite := func(node *ast.Node) {
			if !flow.IsDestructuringTarget(node) {
				return
			}
			target := ast.SkipParentheses(node.Expression())
			if target.Kind != ast.KindPropertyAccessExpression && target.Kind != ast.KindElementAccessExpression {
				return
			}
			// Most rest targets are identifiers or non-union slots. Do not ask
			// the checker to project a remainder until there is a union to judge.
			receiver := unionWriteConstraint(ctx.TypeChecker, ctx.TypeChecker.GetTypeAtLocation(target.Expression()))
			if receiver == nil || receiver.Flags()&checker.TypeFlagsUnion == 0 {
				return
			}
			check(target, unionWriteRestType(ctx.TypeChecker, node))
		}
		update := func(node, operand *ast.Node, operator ast.Kind) {
			if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
				check(operand, ctx.TypeChecker.GetTypeAtLocation(node))
			}
		}
		return rule.Listeners{
			ast.KindSpreadElement:            restWrite,
			ast.KindSpreadAssignment:         restWrite,
			ast.KindPropertyAccessExpression: patternWrite,
			ast.KindElementAccessExpression:  patternWrite,
			ast.KindCallExpression: func(node *ast.Node) {
				call := node.AsCallExpression()
				callee := ast.SkipParentheses(call.Expression)
				if callee.Kind != ast.KindPropertyAccessExpression || len(node.Arguments()) < 2 {
					return
				}
				access := callee.AsPropertyAccessExpression()
				if access.Name().Text() != "assign" || access.Expression.Kind != ast.KindIdentifier || access.Expression.Text() != "Object" ||
					!type_checking.IsSymbolFromDefaultLibrary(ctx.Program, ctx.TypeChecker.GetSymbolAtLocation(access.Name())) {
					return
				}
				target := node.Arguments()[0]
				for _, source := range node.Arguments()[1:] {
					sourceType := unionWriteConstraint(ctx.TypeChecker, ctx.TypeChecker.GetTypeAtLocation(source))
					sources := []*checker.Type{sourceType}
					if sourceType.Flags()&checker.TypeFlagsUnion != 0 {
						sources = sourceType.AsUnionType().Types()
					}
					for _, member := range sources {
						for _, property := range checker.Checker_getPropertiesOfType(ctx.TypeChecker, member) {
							name := property.Name
							// An absent optional property is not copied. Preserve real undefined,
							// but remove the compiler's missing-property marker.
							checkSlot(target, target, checker.Checker_getNonMissingTypeOfSymbol(ctx.TypeChecker, property), &name)
						}
					}
				}
			},
			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				operator := binary.OperatorToken.Kind
				if !ast.IsAssignmentOperator(operator) {
					return
				}
				if flow.IsDestructuringTarget(node) {
					value := checker.Checker_getAssignedType(ctx.TypeChecker, node)
					// A missing optional source property is the compiler's error/any
					// type here; the default is still a concrete possible write.
					if value.Flags()&checker.TypeFlagsAny != 0 {
						value = ctx.TypeChecker.GetTypeAtLocation(binary.Right)
					} else {
						value = checker.Checker_getTypeWithDefault(ctx.TypeChecker, value, binary.Right)
					}
					check(binary.Left, value)
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

// Project the whole remainder once, before checkSlot visits target members.
// Each pattern has at most one rest. The prefix scan is linear in pattern size;
// the array union loop passes each source member, never the original union,
// to the compiler helpers. getRestType performs its own single member mapping.
func unionWriteRestType(typeChecker *checker.Checker, rest *ast.Node) *checker.Type {
	pattern := rest.Parent
	source := unionWriteConstraint(typeChecker, checker.Checker_getAssignedType(typeChecker, pattern))
	if rest.Kind == ast.KindSpreadAssignment {
		var omitted []*ast.Node
		for _, property := range pattern.AsObjectLiteralExpression().Properties.Nodes {
			if property == rest {
				break
			}
			if name := property.Name(); name != nil {
				omitted = append(omitted, name)
			}
		}
		return checker.Checker_getRestType(typeChecker, source, omitted, nil)
	}
	index := 0
	for _, element := range pattern.AsArrayLiteralExpression().Elements.Nodes {
		if element == rest {
			break
		}
		index++
	}
	members := []*checker.Type{source}
	if source.Flags()&checker.TypeFlagsUnion != 0 {
		members = source.AsUnionType().Types()
	}
	remainders := make([]*checker.Type, 0, len(members))
	for _, member := range members {
		if checker.IsTupleType(member) {
			// The rest is a fresh mutable tuple when its length is known.
			// Keep that precision so a remainder can also fit tuple-valued slots.
			remainders = append(remainders, checker.Checker_sliceTupleType(typeChecker, member, index, 0))
		} else {
			remainders = append(remainders, checker.Checker_getTypeOfDestructuredSpreadExpression(typeChecker, member))
		}
	}
	return typeChecker.GetUnionType(remainders)
}
