// Package loop_copy is staticcheck's S1001, a loop that copies a slice or an array into another element by element,
// which a call to the builtin copy says in one line, or an assignment when both are arrays of one type, ported as a
// check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1001/s1001.go, whose logic it keeps and whose
// messages and fixes it reports word for word, at the same range, the loop's header. The original matches its one
// query, an Or of three loops, through staticcheck's pattern language:
//
//	(RangeStmt key@(Ident _) value@(Ident _) ":=" src [(AssignStmt (IndexExpr dst key) "=" value)])
//	(RangeStmt key@(Ident _) nil ":=" src [(AssignStmt (IndexExpr dst key) "=" (IndexExpr src key))])
//	(ForStmt (AssignStmt key@(Ident _) ":=" (IntegerLiteral "0")) (BinaryExpr key "<" (CallExpr (Symbol "len") [src]))
//		(IncDecStmt key "++") [(AssignStmt (IndexExpr dst key) "=" (IndexExpr src key))])
//
// The port asks the same of each range and for statement directly, reading pattern/match.go for what each construct
// matches. The query's one symbol, len, sits under a ForStmt the two RangeStmt alternatives stand beside, and an Or
// with a symbolless alternative is one the type index's prefilter always lets through, so the original walks every
// package and the port needs no guard for the package under analysis. The registry still runs the original, which
// stays the oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere they
// hold.
package loop_copy

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since this check filters them.
var Analyzer = &analysis.Analyzer{
	Name: "S1001",
	Doc: "Flags a loop that copies a slice or an array element by element, which copy or an assignment says: " +
		"staticcheck's S1001, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// copiedLoop is what the query binds of a loop it matches: the key, the value when a range names one, the slice or
// array read from and the one written to, each without the parentheses the pattern looks through before it binds.
type copiedLoop struct {
	key         *ast.Ident
	value       *ast.Ident
	source      ast.Expr
	destination ast.Expr
}

// run builds the pass's context and judges every range and for statement of the package, the two roots the query's
// alternatives name.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("loop_copy: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.RangeStmt)(nil), (*ast.ForStmt)(nil)}, func(node ast.Node) {
		Check(context, node)
	})
	return nil, nil
}

// Check judges one range or for statement: a loop the query matches, whose source and destination are the same
// wherever the loop stands and hold elements of one type, is reported as an assignment when both are arrays of one
// type, through a pointer or not, and otherwise as a call to copy, where copy is still the builtin. Any other node is
// none of the query's roots and is left alone.
func Check(context *rule.Context, node ast.Node) {
	var copied copiedLoop
	var isCopy bool
	switch loop := node.(type) {
	case *ast.RangeStmt:
		copied, isCopy = rangeCopy(loop)
	case *ast.ForStmt:
		copied, isCopy = countedCopy(context, loop)
	default:
		return
	}
	if !isCopy {
		return
	}

	keyObject := context.TypesInfo.ObjectOf(copied.key)
	// The second range alternative and the for statement bind no value, and the original compares each identifier's
	// object with a nil one then, so an identifier with no object counts as the loop's, as it does there.
	var valueObject types.Object
	if copied.value != nil {
		valueObject = context.TypesInfo.ObjectOf(copied.value)
	}
	if !isInvariant(context, keyObject, valueObject, copied.destination) {
		return
	}
	// For example: `for i := range foo()`.
	if !isInvariant(context, keyObject, valueObject, copied.source) {
		return
	}

	sourceType := context.TypesInfo.TypeOf(copied.source)
	destinationType := context.TypesInfo.TypeOf(copied.destination)
	// Every expression of a package that type-checks has a type, and the original would crash on one without.
	if sourceType == nil || destinationType == nil {
		return
	}
	sourceElement, isSourceArray, isSourcePointer, isSourceIndexable := elementType(sourceType)
	if !isSourceIndexable {
		return
	}
	if isSourcePointer {
		sourceType = pointedAt(sourceType)
	}
	destinationElement, isDestinationArray, isDestinationPointer, isDestinationIndexable := elementType(
		destinationType)
	if !isDestinationIndexable {
		return
	}
	if isDestinationPointer {
		destinationType = pointedAt(destinationType)
	}
	if !types.Identical(sourceElement, destinationElement) {
		return
	}

	if isSourceArray && isDestinationArray && types.Identical(sourceType, destinationType) {
		// Arrays of one type are assigned whole, each through the pointer that reaches it.
		source := copied.source
		if isSourcePointer {
			source = &ast.StarExpr{X: source}
		}
		destination := copied.destination
		if isDestinationPointer {
			destination = &ast.StarExpr{X: destination}
		}
		assignment := &ast.AssignStmt{Lhs: []ast.Expr{destination}, Rhs: []ast.Expr{source}, Tok: token.ASSIGN}
		staticcheck_report.Report(context, node, "should copy arrays using assignment instead of using a loop",
			staticcheck_report.FilterGenerated(),
			staticcheck_report.ShortRange(),
			staticcheck_report.Fixes(edit.Fix("Replace loop with assignment",
				edit.ReplaceWithNode(context.Fset, node, assignment))))
		return
	}

	// The loop's own scope decides what copy names where the loop starts: a copy the code declares there, in a
	// function or a package, is not the builtin, and the loop is left alone.
	copyAtLoop, evaluationError := types.Eval(context.Fset, context.Package, node.Pos(), "copy")
	if evaluationError != nil || !copyAtLoop.IsBuiltin() {
		return
	}
	// An array is copied through a slice of all of it, as the message's [:] says.
	to := "to"
	from := "from"
	source := copied.source
	if isSourceArray {
		from = "from[:]"
		source = &ast.SliceExpr{X: source}
	}
	destination := copied.destination
	if isDestinationArray {
		to = "to[:]"
		destination = &ast.SliceExpr{X: destination}
	}
	call := &ast.CallExpr{Fun: &ast.Ident{Name: "copy"}, Args: []ast.Expr{destination, source}}
	staticcheck_report.Report(context, node, fmt.Sprintf("should use copy(%s, %s) instead of a loop", to, from),
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(edit.Fix("Replace loop with call to copy()",
			edit.ReplaceWithNode(context.Fset, node, call))))
}

// rangeCopy is what the two range alternatives bind of loop, and whether either matches. Both declare the key, an
// identifier, with := and range over a source; the first declares a value, an identifier too, and stores it, and the
// second declares none and stores the source's element at the key.
func rangeCopy(loop *ast.RangeStmt) (copiedLoop, bool) {
	// `key@(Ident _)` looks through parentheses to an identifier, and an absent key is none.
	key, isKey := ast.Unparen(loop.Key).(*ast.Ident)
	if !isKey || loop.Tok != token.DEFINE {
		return copiedLoop{}, false
	}
	// src binds the ranged expression with its parentheses removed, as match.go unwraps before it binds.
	source := ast.Unparen(loop.X)
	assignment, destination, isStore := storeAtKey(loop.Body, key)
	if !isStore {
		return copiedLoop{}, false
	}
	stored := assignment.Rhs[0]
	if loop.Value != nil {
		// Only the first alternative takes a value, `value@(Ident _)`, and recalls it as the right side: an
		// identifier spelled the same, since a recalled node compares by syntax. A blank value is an identifier too,
		// and no right side that type-checks is spelled _, so it matches neither alternative.
		value, isValue := ast.Unparen(loop.Value).(*ast.Ident)
		if !isValue || !staticcheck_code.SameSyntax(value, stored) {
			return copiedLoop{}, false
		}
		return copiedLoop{key: key, value: value, source: source, destination: destination}, true
	}
	// The second alternative's nil value is an absent one, and its right side is `(IndexExpr src key)`.
	if !readsAtKey(stored, source, key) {
		return copiedLoop{}, false
	}
	return copiedLoop{key: key, source: source, destination: destination}, true
}

// countedCopy is what the for statement alternative binds of loop, and whether it matches: a key declared with := as
// the integer literal zero, a condition that it is less than the builtin len of the source, a post statement that
// increments it, and a body that stores the source's element at the key.
func countedCopy(context *rule.Context, loop *ast.ForStmt) (copiedLoop, bool) {
	// The initializer is an assignment, never one wrapped in anything, since the pattern looks through no wrapping
	// a simple statement can take but an expression statement's, which holds no assignment.
	initializer, isAssignment := loop.Init.(*ast.AssignStmt)
	if !isAssignment || initializer.Tok != token.DEFINE || len(initializer.Lhs) != 1 || len(initializer.Rhs) != 1 {
		return copiedLoop{}, false
	}
	key, isKey := ast.Unparen(initializer.Lhs[0]).(*ast.Ident)
	if !isKey {
		return copiedLoop{}, false
	}
	// `(IntegerLiteral "0")` is an integer literal whose constant prints as 0, so 0x0 and -0 are it and 0.0 is not.
	zero, isLiteral := staticcheck_code.IntegerLiteral(context, initializer.Rhs[0])
	if !isLiteral || zero.Value.String() != "0" {
		return copiedLoop{}, false
	}

	condition, isBinary := ast.Unparen(loop.Cond).(*ast.BinaryExpr)
	if !isBinary || condition.Op != token.LSS || !staticcheck_code.SameSyntax(key, condition.X) {
		return copiedLoop{}, false
	}
	// `(CallExpr (Symbol "len") [src])` is a call of the builtin len, however parenthesized, with one argument, which
	// src binds without its parentheses; the call's ... is no field of the pattern's.
	length, isCall := ast.Unparen(condition.Y).(*ast.CallExpr)
	if !isCall || !staticcheck_code.IsSymbol(context, length.Fun, "len") || len(length.Args) != 1 {
		return copiedLoop{}, false
	}
	source := ast.Unparen(length.Args[0])

	increment, isIncrement := loop.Post.(*ast.IncDecStmt)
	if !isIncrement || increment.Tok != token.INC || !staticcheck_code.SameSyntax(key, increment.X) {
		return copiedLoop{}, false
	}

	assignment, destination, isStore := storeAtKey(loop.Body, key)
	if !isStore || !readsAtKey(assignment.Rhs[0], source, key) {
		return copiedLoop{}, false
	}
	return copiedLoop{key: key, source: source, destination: destination}, true
}

// storeAtKey is the body's one statement when it is `(AssignStmt (IndexExpr dst key) "=" _)`, an assignment of one
// value to an index of one destination at the key, and that destination without its parentheses, which dst binds.
// The body is a list of one statement, which the pattern looks through labels and blocks of one statement to reach,
// as match.go does for any statement it compares.
func storeAtKey(body *ast.BlockStmt, key *ast.Ident) (*ast.AssignStmt, ast.Expr, bool) {
	assignment, isAssignment := soleAssignment(body.List)
	if !isAssignment || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil, nil, false
	}
	// `(IndexExpr ...)` is an index of one, never an instantiation's list of type arguments.
	stored, isIndex := ast.Unparen(assignment.Lhs[0]).(*ast.IndexExpr)
	if !isIndex || !staticcheck_code.SameSyntax(key, stored.Index) {
		return nil, nil, false
	}
	return assignment, ast.Unparen(stored.X), true
}

// readsAtKey is whether expression is `(IndexExpr src key)`, the source recalled at the key: an index whose operand is
// the bound source by syntax, so a call ranged over and called again is the same source, and whose index is the key.
func readsAtKey(expression ast.Expr, source ast.Expr, key *ast.Ident) bool {
	read, isIndex := ast.Unparen(expression).(*ast.IndexExpr)
	return isIndex && staticcheck_code.SameSyntax(source, read.X) && staticcheck_code.SameSyntax(key, read.Index)
}

// soleAssignment is the assignment a list of one statement holds, looking through labels and blocks of one
// statement on the way, as match.go looks through them before comparing a statement with the pattern's AssignStmt.
// A list of any other length, an empty block among them, holds none.
func soleAssignment(statements []ast.Stmt) (*ast.AssignStmt, bool) {
	if len(statements) != 1 {
		return nil, false
	}
	statement := statements[0]
	for {
		switch wrapped := statement.(type) {
		case *ast.AssignStmt:
			return wrapped, true
		case *ast.LabeledStmt:
			statement = wrapped.Stmt
		case *ast.BlockStmt:
			if len(wrapped.List) != 1 {
				return nil, false
			}
			statement = wrapped.List[0]
		default:
			return nil, false
		}
	}
}

// isInvariant is whether expression is the same value on every iteration: it has no side effects, judged with no
// function pure, and names neither the key's object nor the value's, so a body like `a[i][i] = v` is not a copy.
func isInvariant(context *rule.Context, keyObject types.Object, valueObject types.Object, expression ast.Expr) bool {
	if staticcheck_code.MayHaveSideEffects(context, expression, nil) {
		return false
	}
	invariant := true
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		object := context.TypesInfo.ObjectOf(identifier)
		if object == keyObject || object == valueObject {
			invariant = false
			return false
		}
		return true
	})
	return invariant
}

// elementType is the element type of a slice, an array or a pointer to either, read through named types, whether it
// is an array's, whether it was reached through a pointer, and whether the type has elements at all. A type
// parameter's underlying type is its constraint, so it has none here.
func elementType(indexed types.Type) (element types.Type, isArray bool, isPointer bool, isIndexable bool) {
	switch underlying := indexed.Underlying().(type) {
	case *types.Slice:
		return underlying.Elem(), false, false, true
	case *types.Array:
		return underlying.Elem(), true, false, true
	case *types.Pointer:
		element, isArray, _, isIndexable = elementType(underlying.Elem())
		return element, isArray, true, isIndexable
	default:
		return nil, false, false, false
	}
}

// pointedAt is what pointer points at, a type elementType found to be a pointer under its named types.
func pointedAt(pointer types.Type) types.Type {
	underlying, isPointer := pointer.Underlying().(*types.Pointer)
	if !isPointer {
		// Invariant: pointedAt is asked only of a type elementType read as a pointer, through the same Underlying.
		panic("loop_copy: a type elementType read as a pointer is not one")
	}
	return underlying.Elem()
}
