// Package guarded_delete is staticcheck's S1033, a call of the builtin delete guarded by a lookup of the very key it
// deletes, `if _, ok := m[key]; ok { delete(m, key) }`, which is `delete(m, key)` written longer, since deleting a key
// a map lacks does nothing, ported as a check on types and syntax that reports through the kit every port of
// staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1033/s1033.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the if statement's keyword. The original matches its
// one query, `(IfStmt (AssignStmt [(Ident "_") ok@(Ident _)] ":=" (IndexExpr m key)) ok [call@(CallExpr (Builtin
// "delete") [m key])] nil)`, through staticcheck's pattern language; the port asks the same of each if statement
// directly, reading pattern/match.go for what each construct of the query matches, and reports through
// staticcheck_report.Report where the original reports through report.Report. Unlike the ports before it, the query's
// bindings hold syntax, not objects, so its second m, its second key and its second ok are compared with the first as
// match.go compares two nodes, field by field through reflection, which sameSyntax and sameFields mirror line for
// line. The registry runs the port in the original's place, under its name, and the original stays only as the oracle:
// the parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package guarded_delete

import (
	"go/ast"
	"go/token"
	"reflect"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
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

// Listener is what the port hears of each package from the dispatcher: every if statement, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.IfStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		statement, isIf := node.(*ast.IfStmt)
		if !isIf {
			// Invariant: the dispatcher hands Node only the if statements it was asked for.
			panic("guarded_delete: the dispatcher handed over a node that is not an if statement")
		}
		Check(context, statement)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1033",
	"S1033",
	"Flags a delete guarded by a lookup of the key it deletes: staticcheck's S1033, ported.",
	&Listener,
)

// Check judges one if statement: one with no else, or an empty one, whose initializer declares the blank identifier
// and one variable from a map lookup, whose condition is that variable, and whose body is a call of the builtin delete
// on the same map and key, guards what delete already does, and the fix replaces the whole statement with the call.
func Check(context *rule.Context, statement *ast.IfStmt) {
	// The query's trailing `nil` is the else, which match.go meets through what it looks through: an else block is
	// its list of statements, and an empty block's list is a nil slice, which `nil` matches. So an empty else is no
	// else, and an else if or an else that holds anything, a lone semicolon included, is left alone.
	if statement.Else != nil {
		block, isBlock := statement.Else.(*ast.BlockStmt)
		if !isBlock || block.List != nil {
			return
		}
	}
	// `(AssignStmt ...)` is the initializer itself: a simple statement is never parenthesized, labeled or a block, so
	// nothing the pattern looks through can wrap it.
	assignment, isAssignment := statement.Init.(*ast.AssignStmt)
	// `":="` is the token exactly, so `_, ok = m[key]` into a variable declared before is left alone.
	if !isAssignment || assignment.Tok != token.DEFINE {
		return
	}
	// `[(Ident "_") ok@(Ident _)]` is two names exactly, the first the blank identifier. Neither can be parenthesized
	// on the left of `:=`, which the parser refuses, but the pattern would look through it, and so does the port.
	if len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return
	}
	blank, isBlankIdentifier := ast.Unparen(assignment.Lhs[0]).(*ast.Ident)
	if !isBlankIdentifier || blank.Name != "_" {
		return
	}
	found, isFoundIdentifier := ast.Unparen(assignment.Lhs[1]).(*ast.Ident)
	if !isFoundIdentifier {
		return
	}
	// A lone `(IndexExpr m key)` meets the right side's list as a list of one, through parentheses, and an IndexExpr
	// alone, never an IndexListExpr. The pattern asks nothing of the types: a lookup with two results is a map's, and a
	// generic function's instantiation never has two.
	lookup, isLookup := ast.Unparen(assignment.Rhs[0]).(*ast.IndexExpr)
	if !isLookup {
		return
	}
	// `ok` recalls the identifier the initializer bound, which match.go compares with the condition as two nodes: an
	// identifier, through parentheses, spelled the same, its position and its object never read.
	if !sameSyntax(found, statement.Cond) {
		return
	}
	call := loneCall(statement.Body.List)
	// `(Builtin "delete")` is the universe's delete, its name through parentheses, so a delete the code declares
	// itself, in a block or at package scope, is not it.
	if call == nil || len(call.Args) != 2 || !staticcheck_code.IsBuiltin(context, call.Fun, "delete") {
		return
	}
	// `[m key]` recalls the map and the key the lookup bound, each without its parentheses, as two nodes again: the
	// same syntax, not the same values, so a key that calls a function is the same key however often it is called,
	// and two variables spelled alike are one.
	if !sameSyntax(ast.Unparen(lookup.X), call.Args[0]) || !sameSyntax(ast.Unparen(lookup.Index), call.Args[1]) {
		return
	}
	// The fix prints the call the pattern bound, without the parentheses or the block around it, from its own
	// positions, so a call over several lines keeps its lines, and the comments in the if go with the if.
	staticcheck_report.Report(context, statement, "unnecessary guard around call to delete",
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(edit.Fix("Remove guard", edit.ReplaceWithNode(context.Fset, statement, call))))
}

// loneCall is the call statements hold, as the pattern's `[call@(CallExpr ...)]` meets an if's body: a list of one
// statement, which match.go looks through while it is an expression statement, a labeled statement, a parenthesized
// expression or a block of one statement, nested in any order, until a call is left. Anything else is no call, and
// nil says so.
func loneCall(statements []ast.Stmt) *ast.CallExpr {
	if len(statements) != 1 {
		return nil
	}
	var node ast.Node = statements[0]
	for {
		switch wrapper := node.(type) {
		case *ast.CallExpr:
			return wrapper
		case *ast.ExprStmt:
			node = wrapper.X
		case *ast.ParenExpr:
			node = wrapper.X
		case *ast.LabeledStmt:
			// A label nothing jumps to is an error the type checker reports, so a package that type-checks holds
			// none here, but the pattern looks through one, and so does the port.
			node = wrapper.Stmt
		case *ast.BlockStmt:
			return loneCall(wrapper.List)
		default:
			return nil
		}
	}
}

// sameSyntax is whether recalled is the syntax bound holds, as match.go's match compares two syntax values when a
// binding that holds a node is recalled. Each side sheds what the pattern looks through, the left first, then the
// right: parentheses, an expression statement, a declaration statement and a labeled statement for what they wrap,
// and a block and a field list for their lists. Two nodes are then the same field by field, a list and a lone
// statement or field are a list of one, and an absent value is the same as an absent value alone.
//
// Some answers follow from match.go's order rather than from Go's meaning, and are kept since they are the original's:
// a field with no tag is never the same as another, since an absent tag on the right is compared as an untyped nil
// with the typed one on the left, so a struct type, an interface with methods or a function type with parameters or
// results is never the same as itself; and a block of one statement is the same as that statement.
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
		// Only a function declaration's body is ever an absent block, and a binding never holds a declaration.
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
			panic("guarded_delete: a syntax node holds a field of kind " + boundField.Kind().String())
		}
	}
	return true
}
