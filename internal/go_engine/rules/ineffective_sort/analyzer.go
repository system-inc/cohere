// Package ineffective_sort is staticcheck's SA4029, x = sort.IntSlice(x) and its Float64Slice and StringSlice twins,
// a conversion to a type mistaken for a sorting function, which sorts nothing, ported as a check on types and syntax
// that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4029/sa4029.go, whose logic it keeps and
// whose message and fix it reports word for word, across the whole assignment as the original's does. The original
// matches the pattern `(AssignStmt target@(Ident _) "=" (CallExpr typ@(Symbol (Or "sort.Float64Slice" "sort.IntSlice"
// "sort.StringSlice")) [target]))` through code.Matches; the port walks the assignments that pattern's entry node
// names and tests each by syntax and types, as the pattern's matcher does: one identifier assigned with =, its
// parentheses looked through, from one call, its parentheses looked through, of one of sort's three slice types as
// the pattern's Symbol names a type, through a package-level alias too, whose one argument, its parentheses looked
// through, is an identifier spelled as the target is, since a repeated binding compares two identifiers by name.
//
// code.Matches walks a package only when code.CouldMatchAny finds one of the three in staticcheck's type index of it,
// which holds sort only when the package imports it or uses a method or field it declares, so a package reaching
// sort.IntSlice only through another package's alias is not walked, and staticcheck_code.IndexedObject asks the
// same once a package, before the port judges any assignment of it. The generated-file judgment filters nothing,
// since the original asks none. The message renders the conversion through staticcheck_report.Render, and the fix, a
// call of sort.Ints, sort.Float64s or sort.Strings spelled sort whatever the file names the package, is printed by
// staticcheck's analysis/edit, borrowed in this wave as the original uses it, through staticcheck_report.Fixes. The
// registry runs the port in the original's place, under its name, and the original stays only as the oracle: the parity
// tests beside this file run both over the same packages, and the two differ nowhere they hold.
package ineffective_sort

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every assignment, in preorder, judged only
// where the type index would hold one of sort's slice types, which it asks once for the package, on the first
// assignment, and keeps in the context's state.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.AssignStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		assignment, isAssignment := node.(*ast.AssignStmt)
		if !isAssignment {
			// Invariant: the dispatcher hands Node only the assignments it was asked for.
			panic("ineffective_sort: the dispatcher handed over a node that is not an assignment")
		}
		match := rule.State[packageMatch](context)
		if !match.decided {
			match.decided, match.couldMatch = true, couldMatch(context)
		}
		if !match.couldMatch {
			return
		}
		Check(context, assignment)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4029",
	"SA4029",
	"Flags x = sort.IntSlice(x) and its twins, a conversion that sorts nothing: staticcheck's SA4029, ported as "+
		"a check on types and syntax.",
	&Listener,
)

// packageMatch is what the port asks once of a package, in the context's state: whether the type index holds one of
// sort's slice types, as the original asks before it walks anything.
type packageMatch struct {
	// decided is whether couldMatch has been asked of the package yet.
	decided bool
	// couldMatch is its answer, once decided.
	couldMatch bool
}

// sliceTypes are sort's slice types the pattern names, each with the sorting function the fix calls in its place.
var sliceTypes = []struct {
	name        string
	alternative string
}{
	{"Float64Slice", "Float64s"},
	{"IntSlice", "Ints"},
	{"StringSlice", "Strings"},
}

// couldMatch is code.CouldMatchAny of the pattern: whether staticcheck's type index of the package holds any of the
// symbols it names.
func couldMatch(context *rule.Context) bool {
	for _, sliceType := range sliceTypes {
		if staticcheck_code.IndexedObject(context, "sort", sliceType.name) != nil {
			return true
		}
	}
	return false
}

// Check judges one assignment: x = sort.IntSlice(x), with Float64Slice or StringSlice in its place, converts x and
// assigns it back, sorting nothing, where x is a slice that is no type of its own. A target whose type is sort's
// slice type or another named slice type is left alone, since there the conversion may be meant.
func Check(context *rule.Context, assignment *ast.AssignStmt) {
	if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return
	}
	target, isIdentifier := ast.Unparen(assignment.Lhs[0]).(*ast.Ident)
	if !isIdentifier {
		return
	}
	call, isCall := ast.Unparen(assignment.Rhs[0]).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return
	}
	// The repeated binding compares the argument with the target as two identifiers, by spelling alone.
	argument, isArgument := ast.Unparen(call.Args[0]).(*ast.Ident)
	if !isArgument || argument.Name != target.Name {
		return
	}
	sliceType := symbolType(context, call.Fun)
	if sliceType == nil {
		return
	}
	if _, isSlice := types.Unalias(context.TypesInfo.TypeOf(target)).(*types.Slice); !isSlice {
		// The original avoids flagging x = sort.StringSlice(x) where x is a sort.StringSlice already.
		return
	}
	typeName := types.TypeString(types.Unalias(sliceType.Type()), nil)
	alternative := ""
	for _, each := range sliceTypes {
		if typeName == "sort."+each.name {
			alternative = each.alternative
		}
	}
	if alternative == "" {
		// Invariant: symbolType finds only a type name that is, or aliases, one of sort's three slice types.
		panic(fmt.Sprintf("unreachable: %q", typeName))
	}
	replacement := &ast.CallExpr{Fun: edit.Selector("sort", alternative), Args: []ast.Expr{target}}
	staticcheck_report.Report(context, assignment,
		fmt.Sprintf("%s is a type, not a function, and %s doesn't sort your values; consider using sort.%s instead",
			typeName, staticcheck_report.Render(context, assignment.Rhs[0]), alternative),
		staticcheck_report.Fixes(edit.Fix(fmt.Sprintf("Replace with call to sort.%s", alternative),
			edit.ReplaceWithNode(context.FileSet, assignment, replacement))))
}

// symbolType is the type name function names when the pattern's Symbol accepts it as one of sort's slice types, and
// nil otherwise. Symbol takes an identifier or a selector, parenthesized, instantiated or not, and judges the object
// it resolves to; a function, builtin, constant or variable is named by its package's path and its name, which none
// of sort's declarations spells as sort.IntSlice, so only a type name is one. A type name must sit at its package's
// scope, and is one when it prints as sort's type or is an alias whose chain ends at one that does; an alias a
// function declares is none, nor one whose chain ends at a basic type or anything that is no named type.
func symbolType(context *rule.Context, function ast.Expr) *types.TypeName {
	name := ast.Unparen(function)
	switch instantiated := name.(type) {
	case *ast.IndexExpr:
		name = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		name = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch name := name.(type) {
	case *ast.Ident:
		object = context.TypesInfo.ObjectOf(name)
	case *ast.SelectorExpr:
		object = context.TypesInfo.ObjectOf(name.Sel)
	default:
		return nil
	}
	original, isTypeName := object.(*types.TypeName)
	if !isTypeName {
		return nil
	}
	current := original
	for {
		if current.Parent() != current.Pkg().Scope() {
			return nil
		}
		printed := types.TypeString(current.Type(), nil)
		for _, sliceType := range sliceTypes {
			if printed == "sort."+sliceType.name {
				return original
			}
		}
		if !current.IsAlias() {
			return nil
		}
		// The pattern peels every layer of alias at once, where upstream notes it means one day to peel one at a time,
		// and follows a named type's name; a basic type it compares by name, which no basic type's matches.
		named, isNamed := types.Unalias(current.Type()).(interface{ Obj() *types.TypeName })
		if !isNamed {
			return nil
		}
		current = named.Obj()
	}
}
