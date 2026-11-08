package staticcheck_code

import (
	"go/ast"
	"go/token"
	"reflect"
)

// The syntax tree types sameFields skips, found once.
var (
	positionType     = reflect.TypeFor[token.Pos]()
	commentGroupType = reflect.TypeFor[*ast.CommentGroup]()
	// The comparison names the deprecated ast.Object only to skip its fields, as matchAST does, and matching it by
	// name as a string would hide that dependency from the compiler.
	//cohere:ignore SA1019 named only so the comparison can skip these deprecated fields
	objectType = reflect.TypeFor[*ast.Object]()
)

// SameSyntax is whether recalled is the syntax bound holds, as staticcheck's pattern language judges a node bound to
// a name and recalled by it, so a port of a query that names one binding twice, such as `(BinaryExpr x _ x)`, asks
// this where the query recalls x. It is honnef.co/go/tools@v0.8.1 pattern/match.go's match and matchAST, as they
// compare two syntax values; no code helper of staticcheck's spells it.
//
// It compares syntax, not meaning: parentheses are looked through at every depth, identifiers compare by name, so
// two spellings of one object differ and one name for two objects is the same, and positions are skipped, so a call
// spreading its last argument with ... is the same as one that does not. Each side sheds what the pattern language
// looks through, bound first, then recalled: parentheses, an expression statement, a declaration statement and a
// labeled statement for what they wrap, and a block and a field list for their lists. Two nodes are then the same
// field by field, a list and a lone statement or field are a list of one, and an absent value is the same as an
// absent value alone.
//
// Some answers follow from match.go's order rather than from Go's meaning, and are kept since they are the original's:
// a field with no tag is never the same as another, since an absent tag on the right is compared as an untyped nil
// with the typed one on the left, so a struct type, an interface with methods or a function type with parameters or
// results is never the same as itself; and a block of one statement is the same as that statement, unless the
// statement is one the pattern language looks through, which is shed to what it wraps first and is then no statement,
// so a block holding a() is not the same as a() alone, though a block holding for {} is the same as for {}.
func SameSyntax(bound ast.Node, recalled ast.Node) bool {
	return sameSyntax(bound, recalled)
}

// sameSyntax is SameSyntax of two syntax values, a node or a block's or a field list's list on either side, as
// match.go's match compares them.
func sameSyntax(bound any, recalled any) bool {
	switch node := bound.(type) {
	case *ast.ParenExpr:
		return sameSyntax(node.X, recalled)
	case *ast.ExprStmt:
		return sameSyntax(node.X, recalled)
	case *ast.DeclStmt:
		return sameSyntax(node.Decl, recalled)
	case *ast.LabeledStmt:
		return sameSyntax(node.Stmt, recalled)
	case *ast.BlockStmt:
		// Only a function declaration's body is ever an absent block, and match.go panics on one bound as this does.
		return sameSyntax(node.List, recalled)
	case *ast.FieldList:
		if node == nil {
			return sameSyntax(nil, recalled)
		}
		return sameSyntax(node.List, recalled)
	}

	switch node := recalled.(type) {
	case *ast.ParenExpr:
		return sameSyntax(bound, node.X)
	case *ast.ExprStmt:
		return sameSyntax(bound, node.X)
	case *ast.DeclStmt:
		return sameSyntax(bound, node.Decl)
	case *ast.LabeledStmt:
		return sameSyntax(bound, node.Stmt)
	case *ast.BlockStmt:
		if node == nil {
			return sameSyntax(bound, nil)
		}
		return sameSyntax(bound, node.List)
	case *ast.FieldList:
		if node == nil {
			return sameSyntax(bound, nil)
		}
		return sameSyntax(bound, node.List)
	case *ast.BasicLit:
		// An absent literal on the right becomes an untyped nil, which a typed absent one on the left never equals.
		if node == nil {
			return sameSyntax(bound, nil)
		}
	}

	if bound == nil || recalled == nil {
		return bound == recalled
	}
	boundNode, isBoundNode := bound.(ast.Node)
	recalledNode, isRecalledNode := recalled.(ast.Node)
	if isBoundNode && isRecalledNode {
		return sameFields(boundNode, recalledNode)
	}
	// Only a block's and a field list's lists come here as lists, since sameFields compares a node's own lists
	// itself, so match.go's third kind of list, expressions, never does. As in match.go, a side that is a list of
	// statements settles the answer as statements, and only then fields are tried.
	if same, isList := sameAsLists[ast.Stmt](bound, recalled); isList {
		return same
	}
	if same, isList := sameAsLists[*ast.Field](bound, recalled); isList {
		return same
	}
	return false
}

// sameAsLists compares bound and recalled as lists of Element, as match.go compares a list with a list or a lone
// element: a lone element on either side is a list of one, and anything else beside a list is never the same. isList
// is whether either side is a list of Element at all, and only then does same answer.
func sameAsLists[Element any](bound any, recalled any) (same bool, isList bool) {
	boundList, isBoundList := bound.([]Element)
	recalledList, isRecalledList := recalled.([]Element)
	if !isBoundList && !isRecalledList {
		return false, false
	}
	if !isRecalledList {
		lone, isElement := recalled.(Element)
		if !isElement {
			return false, true
		}
		recalledList = []Element{lone}
	}
	if !isBoundList {
		lone, isElement := bound.(Element)
		if !isElement {
			return false, true
		}
		boundList = []Element{lone}
	}
	return sameLists(boundList, recalledList), true
}

// sameLists is whether two lists are as long and the same element by element.
func sameLists[Element any](bound []Element, recalled []Element) bool {
	if len(bound) != len(recalled) {
		return false
	}
	for index := range bound {
		if !sameSyntax(bound[index], recalled[index]) {
			return false
		}
	}
	return true
}

// sameFields is whether two nodes are the same, as match.go's matchAST compares them: of one type, both absent or
// both present, and then every field the same, positions, comments and the deprecated objects skipped, names and
// literals by their text, operators, kinds and flags by value, and every node and list of nodes by sameSyntax, so
// parentheses are looked through at every depth.
func sameFields(bound ast.Node, recalled ast.Node) bool {
	boundValue := reflect.ValueOf(bound)
	recalledValue := reflect.ValueOf(recalled)
	if boundValue.Type() != recalledValue.Type() {
		return false
	}
	if boundValue.IsNil() || recalledValue.IsNil() {
		return boundValue.IsNil() == recalledValue.IsNil()
	}
	boundValue = boundValue.Elem()
	recalledValue = recalledValue.Elem()
	for index := range boundValue.NumField() {
		boundField := boundValue.Field(index)
		recalledField := recalledValue.Field(index)
		if boundField.Type() == positionType || boundField.Type() == objectType ||
			boundField.Type() == commentGroupType {
			continue
		}
		switch boundField.Kind() {
		case reflect.Slice:
			if boundField.Len() != recalledField.Len() {
				return false
			}
			for element := range boundField.Len() {
				if !sameSyntax(boundField.Index(element).Interface(), recalledField.Index(element).Interface()) {
					return false
				}
			}
		case reflect.String:
			if boundField.String() != recalledField.String() {
				return false
			}
		case reflect.Int:
			// A token, an operator or a literal's kind, and a channel's direction.
			if boundField.Int() != recalledField.Int() {
				return false
			}
		case reflect.Bool:
			if boundField.Bool() != recalledField.Bool() {
				return false
			}
		case reflect.Pointer, reflect.Interface:
			if !sameSyntax(boundField.Interface(), recalledField.Interface()) {
				return false
			}
		default:
			// Invariant: every field of go/ast's nodes is one of the kinds above, or a position, a comment group or
			// an object, which are skipped; matchAST panics on any other kind the same way.
			panic("staticcheck_code: a syntax node holds a field of kind " + boundField.Kind().String())
		}
	}
	return true
}
