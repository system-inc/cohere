package adamic

import (
	"fmt"
	"strconv"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	type_checking "github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

var crossEnumNarrowingText = policy.MessageOf("adamic/no-cross-enum-narrowing", "crossEnumNarrowing")

// NoCrossEnumNarrowing refuses comparisons which erase an enum constituent
// despite its equal runtime tag. Each supported form has a strict-tsc-accepted,
// Node-failing program in testdata/cross-enum (#t98h9pd).
var NoCrossEnumNarrowing = rule.Rule{
	Name:             "adamic/no-cross-enum-narrowing",
	NeedsTypeChecker: true,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil {
			return nil
		}
		compare := func(node, left, right *ast.Node) {
			if left == nil || right == nil {
				return
			}
			for _, sides := range [][2]*ast.Node{{left, right}, {right, left}} {
				comparedType := ctx.TypeChecker.GetTypeAtLocation(sides[0])
				compared := crossEnumMembers(ctx.TypeChecker, comparedType)
				if len(compared) == 0 {
					continue
				}
				singleEnum := true
				for _, part := range type_checking.UnionTypeParts(comparedType) {
					if crossEnumOwner(ctx.TypeChecker, part) != compared[0].enum {
						singleEnum = false
					}
				}
				if !singleEnum {
					continue
				}
				declared := crossEnumMembers(ctx.TypeChecker, crossEnumDeclaredType(ctx.TypeChecker, sides[1]))
				for _, other := range compared {
					// A disjoint enum-only comparison is already rejected by tsc (TS2367).
					// The hole requires the compared enum to participate in the narrowed type.
					shared := false
					for _, member := range declared {
						if member.enum == other.enum && member.value == other.value {
							shared = true
							break
						}
					}
					if !shared {
						continue
					}
					for _, member := range declared {
						if member.enum == other.enum || member.value != other.value {
							continue
						}
						value := fmt.Sprint(other.value)
						if text, ok := other.value.(string); ok {
							value = strconv.Quote(text)
						}
						ctx.ReportNode(node, rule.Message{Id: "crossEnumNarrowing", Description: crossEnumNarrowingText.Render(map[string]string{
							"other": other.name, "member": member.name, "value": value,
						})})
						return
					}
				}
			}
		}
		return rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				expression := node.AsBinaryExpression()
				if expression == nil || expression.OperatorToken == nil {
					return
				}
				switch expression.OperatorToken.Kind {
				case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
					compare(node, expression.Left, expression.Right)
				}
			},
			ast.KindCaseClause: func(node *ast.Node) {
				clause := node.AsCaseOrDefaultClause()
				if clause == nil || node.Parent == nil || node.Parent.Parent == nil || node.Parent.Parent.Kind != ast.KindSwitchStatement {
					return
				}
				compare(clause.Expression, clause.Expression, node.Parent.Parent.AsSwitchStatement().Expression)
			},
		}
	},
}

type crossEnumMember struct {
	enum  *ast.Symbol
	name  string
	value any
}

func crossEnumMembers(tc *checker.Checker, typ *checker.Type) []crossEnumMember {
	var result []crossEnumMember
	if typ == nil {
		return result
	}
	for _, part := range type_checking.UnionTypeParts(typ) {
		if part.Flags()&checker.TypeFlagsEnumLiteral == 0 || part.Flags()&(checker.TypeFlagsNumberLiteral|checker.TypeFlagsStringLiteral) == 0 {
			continue
		}
		owner := crossEnumOwner(tc, part)
		if owner == nil {
			continue
		}
		result = append(result, crossEnumMember{enum: owner, name: type_checking.StableTypeText(tc, part), value: part.AsLiteralType().Value()})
	}
	return result
}

// Recover property types through the receiver's declared type, not its flow type.
// A union property's synthetic symbol alone may already have lost constituents.
func crossEnumDeclaredType(tc *checker.Checker, node *ast.Node) *checker.Type {
	node = ast.SkipParentheses(node)
	if node == nil {
		return nil
	}
	var receiver *ast.Node
	var key string
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		access := node.AsPropertyAccessExpression()
		receiver, key = access.Expression, access.Name().Text()
	case ast.KindElementAccessExpression:
		access := node.AsElementAccessExpression()
		typ := tc.GetTypeAtLocation(access.ArgumentExpression)
		if typ != nil && typ.Flags()&checker.TypeFlagsStringLiteral != 0 {
			receiver, key = access.Expression, typ.AsLiteralType().Value().(string)
		}
	}
	if receiver != nil {
		typ := crossEnumDeclaredType(tc, receiver)
		if typ != nil {
			if property := checker.Checker_getPropertyOfType(tc, typ, key); property != nil {
				return checker.Checker_getTypeOfSymbol(tc, property)
			}
		}
	}
	if symbol := tc.GetSymbolAtLocation(node); symbol != nil && symbol.Flags&ast.SymbolFlagsAlias == 0 {
		return checker.Checker_getTypeOfSymbol(tc, symbol)
	}
	return tc.GetTypeAtLocation(node)
}

// Unknown computed members still have an enum identity. They cannot supply a
// known runtime value, but must not hide the known members of their enum.
func crossEnumOwner(tc *checker.Checker, typ *checker.Type) *ast.Symbol {
	if typ == nil {
		return nil
	}
	symbol := checker.Type_symbol(typ)
	if symbol == nil || symbol.Flags&ast.SymbolFlagsEnumMember == 0 || symbol.ValueDeclaration == nil || symbol.ValueDeclaration.Parent == nil {
		return nil
	}
	return checker.Type_symbol(tc.GetTypeAtLocation(symbol.ValueDeclaration.Parent))
}
