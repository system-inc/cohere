// Package struct_conversion_by_field_copy is staticcheck's S1016, a struct literal that copies every field of one
// value of another struct type, field by field and in order, `T2{x.A, x.B}`, which the conversion `T2(x)` says
// shorter, ported as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1016/s1016.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the literal. The original queries no pattern: it walks
// every composite literal through code.Cursor, the inspector's root, and judges each by its types and syntax, as the
// port does, reporting through staticcheck_report.Report where the original reports through report.Report. The registry
// runs the port in the original's place, under its name, and the original stays only as the oracle: the parity tests
// beside this file run both over the same packages.
//
// The port parts from the original twice, where the original is wrong, each recorded in the differences fixture as a
// drop. The original declines a literal whose address is taken, since a conversion has none, but asks only the
// literal's parent, so `&(T2{x.A, x.B})`, which Go reads as `&T2{x.A, x.B}`, is reported, and the fix writes
// `&(T2(x))`, which does not compile; the port looks through the parentheses. And the original declines a literal
// of the very type its fields come from by comparing the two types as pointers, so an alias of that type, or the same
// generic instance made by another package's type checker, reads as another type, and the original suggests
// converting a value to the type it already has; the port compares the types by types.Identical.
package struct_conversion_by_field_copy

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"go/version"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every composite literal, in preorder, as the
// original walks them from the inspector's root, each with its ancestors, which tell whether it is the operand of an
// address operator.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CompositeLit)(nil)},
	Node: func(context *rule.Context, node ast.Node, stack []ast.Node) {
		literal, isLiteral := node.(*ast.CompositeLit)
		if !isLiteral {
			// Invariant: the dispatcher hands Node only the composite literals it was asked for.
			panic("struct_conversion_by_field_copy: the dispatcher handed over a node that is not a composite literal")
		}
		Check(context, literal, isAddressOperand(stack))
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1016",
	"S1016",
	"Flags a struct literal that copies every field of one value of another struct type: staticcheck's S1016, "+
		"ported.",
	&Listener,
)

// isAddressOperand is whether the literal whose ancestors are stack, outermost first, is what an address operator
// takes the address of, through any parentheses around it. The original asks the literal's parent alone, so
// `&(T2{x.A})` is not an operand there; Go reads it as `&T2{x.A}`, and the original's fix writes `&(T2(x))`, which
// does not compile.
func isAddressOperand(stack []ast.Node) bool {
	parent := len(stack) - 1
	for parent >= 0 {
		if _, isParenthesized := stack[parent].(*ast.ParenExpr); !isParenthesized {
			break
		}
		parent--
	}
	// Every literal's outermost ancestor is its file, so a literal has a parent past its parentheses; one handed no
	// ancestors has no address operator over it.
	if parent < 0 {
		return false
	}
	unary, isUnary := stack[parent].(*ast.UnaryExpr)
	return isUnary && unary.Op == token.AND
}

// Check judges one composite literal, and isAddressOperand is whether an address operator takes its address. A
// literal of a named struct type, or an alias of one, whose elements copy every field of one variable of another
// named struct type, or an alias of one, from the same package, field for field by name, is reported when the two
// structs are identical, their tags ignored from Go 1.8 on, and the fix writes the conversion in the literal's place.
func Check(context *rule.Context, literal *ast.CompositeLit, isAddressOperand bool) {
	// A conversion has no address, so a pointer to the literal stays as it is.
	if isAddressOperand {
		return
	}
	// A literal nested in another whose type it leaves out names no type the fix could convert to, and the
	// original's TypeOf of the missing type is nil.
	if literal.Type == nil {
		return
	}
	literalType, literalNamed, isLiteralNamed := namedType(context.TypesInfo.TypeOf(literal.Type))
	if !isLiteralNamed {
		return
	}
	literalStruct, isLiteralStruct := literalType.Underlying().(*types.Struct)
	if !isLiteralStruct {
		return
	}
	// Every field is set, one element each, and since a literal's elements are either all keyed or none, the keys
	// name every field once.
	if len(literal.Elts) == 0 || literalStruct.NumFields() != len(literal.Elts) {
		return
	}
	var source *ast.Ident
	var sourceType types.Type
	var sourceNamed *types.Named
	for index, element := range literal.Elts {
		selector, isFieldCopy := copiedField(literalStruct, index, element)
		if !isFieldCopy {
			return
		}
		// The value copied from is a variable named by an identifier as written, never parenthesized and never a
		// selector itself, as the original asserts on the selector's operand.
		identifier, isIdentifier := selector.X.(*ast.Ident)
		if !isIdentifier {
			return
		}
		identifierType := context.TypesInfo.TypeOf(identifier)
		if identifierType == nil {
			return
		}
		// All fields are copied from one object, compared by identity, so two variables spelled alike are two.
		if source != nil && context.TypesInfo.ObjectOf(source) != context.TypesInfo.ObjectOf(identifier) {
			return
		}
		// One object has one type, so each element's is the first's, and a pointer, a type parameter or an unnamed
		// struct is never one of a named type.
		elementType, elementNamed, isElementNamed := namedType(identifierType)
		if !isElementNamed {
			return
		}
		sourceType, sourceNamed = elementType, elementNamed
		// The message and the fix name the last element's identifier, as the original keeps the last it read.
		source = identifier
	}
	// Types of two packages might match by coincidence, and a dependency that adds a field to its type would break
	// code that relies on the conversion.
	if literalNamed.Obj().Pkg() != sourceNamed.Obj().Pkg() {
		return
	}
	sourceStruct, isSourceStruct := sourceType.Underlying().(*types.Struct)
	if !isSourceStruct {
		return
	}
	// A literal of the type its fields come from is a copy, not a conversion. The original compares the two as
	// pointers, so it takes an alias and the type it stands for, or one generic instance made by two type checkers,
	// as two types, and suggests converting a value to the type it has; identity as Go defines it is one type.
	if types.Identical(literalType, sourceType) {
		return
	}
	// Since Go 1.8 a conversion ignores struct tags, and before it the tags had to match too.
	if version.Compare(staticcheck_report.LanguageVersion(context, literal), "go1.8") >= 0 {
		if !types.IdenticalIgnoreTags(literalStruct, sourceStruct) {
			return
		}
	} else if !types.Identical(literalStruct, sourceStruct) {
		return
	}

	conversion := &ast.CallExpr{
		Fun:  literal.Type,
		Args: []ast.Expr{source},
	}
	staticcheck_report.Report(context, literal,
		fmt.Sprintf("should convert %s (type %s) to %s instead of using struct literal", source.Name,
			types.TypeString(sourceType, types.RelativeTo(context.Package)),
			types.TypeString(literalType, types.RelativeTo(context.Package))),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(
			edit.Fix("Use type conversion", edit.ReplaceWithNode(context.Fset, literal, conversion))))
}

// copiedField is the selector element copies the field at index of literalStruct from, and false when it copies
// nothing: a positional selector whose selected name is that field's, or a keyed one whose key is the selector's
// name. A keyed element's key in a struct literal is always a field's name, so a key that is no identifier, where
// the original's assertion would panic, never type-checks.
func copiedField(literalStruct *types.Struct, index int, element ast.Expr) (*ast.SelectorExpr, bool) {
	switch element := element.(type) {
	case *ast.SelectorExpr:
		if literalStruct.Field(index).Name() != element.Sel.Name {
			return nil, false
		}
		return element, true
	case *ast.KeyValueExpr:
		selector, isSelector := element.Value.(*ast.SelectorExpr)
		if !isSelector {
			return nil, false
		}
		key, isKey := element.Key.(*ast.Ident)
		if !isKey || key.Name != selector.Sel.Name {
			return nil, false
		}
		return selector, true
	default:
		return nil, false
	}
}

// namedType is candidate when it is a named type or an alias of one, beside the named type it is or stands for, and
// false for any other type, a type parameter, a pointer or an unnamed struct among them. An alias is kept as written,
// so the message names the alias.
func namedType(candidate types.Type) (types.Type, *types.Named, bool) {
	switch candidate := candidate.(type) {
	case *types.Named:
		return candidate, candidate, true
	case *types.Alias:
		named, isNamed := types.Unalias(candidate).(*types.Named)
		if !isNamed {
			return nil, nil, false
		}
		return candidate, named, true
	default:
		return nil, nil, false
	}
}
