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
	Run: func(ruleContext rule.Context, options any) rule.Listeners {
		if ruleContext.TypeChecker == nil {
			return nil
		}
		compare := func(node, left, right *ast.Node) {
			if left == nil || right == nil {
				return
			}
			for _, sides := range [][2]*ast.Node{{left, right}, {right, left}} {
				comparedType := ruleContext.TypeChecker.GetTypeAtLocation(sides[0])
				compared := crossEnumMembers(ruleContext.TypeChecker, comparedType)
				if len(compared) == 0 {
					continue
				}
				singleEnum := true
				for _, part := range type_checking.UnionTypeParts(comparedType) {
					if crossEnumOwner(ruleContext.TypeChecker, part) != compared[0].enum {
						singleEnum = false
					}
				}
				if !singleEnum {
					continue
				}
				declaredTypes := crossEnumDeclaredTypes(ruleContext.TypeChecker, sides[1])
				var declared []crossEnumMember
				for _, typ := range declaredTypes {
					declared = append(declared, crossEnumMembers(ruleContext.TypeChecker, typ)...)
				}
				for _, other := range compared {
					if !crossEnumHasNarrowingConstituent(other, declaredTypes, declared) {
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
						ruleContext.ReportNode(node, rule.Message{Id: "crossEnumNarrowing", Description: crossEnumNarrowingText.Render(map[string]string{
							"other": type_checking.StableTypeText(ruleContext.TypeChecker, other.typ), "member": type_checking.StableTypeText(ruleContext.TypeChecker, member.typ), "value": value,
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

// A member's display text is built only when a finding reports it: a comparison against a large enum
// (TypeScript's SyntaxKind, typescript-eslint's AST_NODE_TYPES) decomposes hundreds of members, and
// printing each one cost ahra's cold run two minutes of CPU.
type crossEnumMember struct {
	enum  *ast.Symbol
	value any
	typ   *checker.Type
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
		result = append(result, crossEnumMember{enum: owner, value: part.AsLiteralType().Value(), typ: part})
	}
	return result
}

// The shim does not expose isTypeComparableTo. These are the grounded overlap
// constituents: a matching member of the compared enum, its broad primitive, or unknown.
// Plain literals are excluded: tsc keeps the other enum in that branch (cross-enum-j.ts).
func crossEnumHasNarrowingConstituent(other crossEnumMember, types []*checker.Type, members []crossEnumMember) bool {
	for _, member := range members {
		if member.enum == other.enum && member.value == other.value {
			return true
		}
	}
	for _, typ := range types {
		for _, part := range type_checking.UnionTypeParts(typ) {
			if part.Flags()&checker.TypeFlagsUnknown != 0 {
				return true
			}
			if part.Flags()&checker.TypeFlagsNumber != 0 && other.typ.Flags()&checker.TypeFlagsNumberLiteral != 0 ||
				part.Flags()&checker.TypeFlagsString != 0 && other.typ.Flags()&checker.TypeFlagsStringLiteral != 0 {
				return true
			}
		}
	}
	return false
}

// Recover slots through each constituent of the receiver's declared type before
// forming a property union: Kind.A | number would otherwise collapse to number
// and erase the enum member whose runtime tag collides (h.ts).
func crossEnumDeclaredTypes(tc *checker.Checker, node *ast.Node) []*checker.Type {
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
			// A string literal type's value is a string; on a miss the receiver stays nil and nothing is recovered
			if value, isString := typ.AsLiteralType().Value().(string); isString {
				receiver, key = access.Expression, value
			}
		}
	}
	if receiver != nil {
		// Each distinct slot type is kept once. A receiver typed as a large union (typescript-eslint's
		// Node has about 170 members) mostly shares one type per property, so without this a chain like
		// node.parent.type expands every member's parent into every member again.
		var slots []*checker.Type
		seen := map[*checker.Type]bool{}
		for _, typ := range crossEnumDeclaredTypes(tc, receiver) {
			for _, part := range type_checking.UnionTypeParts(typ) {
				if property := checker.Checker_getPropertyOfType(tc, part, key); property != nil {
					slot := checker.Checker_getTypeOfSymbol(tc, property)
					if !seen[slot] {
						seen[slot] = true
						slots = append(slots, slot)
					}
				}
			}
		}
		if len(slots) > 0 {
			return slots
		}
	}
	if symbol := tc.GetSymbolAtLocation(node); symbol != nil && symbol.Flags&ast.SymbolFlagsAlias == 0 {
		return []*checker.Type{checker.Checker_getTypeOfSymbol(tc, symbol)}
	}
	return []*checker.Type{tc.GetTypeAtLocation(node)}
}

// Unknown computed members still have an enum identity. They cannot supply a
// known runtime value, but must not hide the known members of their enum.
func crossEnumOwner(tc *checker.Checker, typ *checker.Type) *ast.Symbol {
	if typ == nil {
		return nil
	}
	symbol := checker.Type_symbol(typ)
	if symbol == nil || symbol.Flags&ast.SymbolFlagsEnumMember == 0 {
		return nil
	}
	// The binder sets a member's parent to its enum, which answers without asking the checker for the
	// enum declaration's type once per member per comparison.
	if symbol.Parent != nil && symbol.Parent.Flags&ast.SymbolFlagsEnum != 0 {
		return symbol.Parent
	}
	if symbol.ValueDeclaration == nil || symbol.ValueDeclaration.Parent == nil {
		return nil
	}
	return checker.Type_symbol(tc.GetTypeAtLocation(symbol.ValueDeclaration.Parent))
}
