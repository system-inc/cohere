// Package assign is vet's assign pass, a self-assignment, `x = x` or `values[index] = values[index]`, which is almost
// always useless, and even when it is not, usually a mistake. An assignment is judged pair by pair: a left side and
// its right side are the same when neither has an effect, the left is not an element of a map, whose index
// expression may add the key, and both print the same. The finding names every such pair, and its fix removes them,
// or the whole statement when every pair is one.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/assign/assign.go, with the
// helpers it calls from x/tools' internal packages (typesinternal.NoEffects, astutil.Format and refactor.DeleteStmt)
// translated beside it, and it reports the original's messages word for word, at the same position, with the same
// suggested fix, edit for edit. The original walks cursors, not nodes, because the fix that removes a whole statement
// reads its parent and siblings, which only a cursor reaches, so the port walks cursors too. The registry runs the
// port in the original's place, and the original stays the oracle: the parity tests beside this file run both over
// the same packages and compare findings and fixes. The two agree everywhere, so there is no differences fixture.
package assign

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "assign",
	Doc: "check for useless assignments\n\n" +
		"This checker reports assignments of the form x = x or a[i] = a[i].\n" +
		"These are almost always useless, and even when they aren't they are\n" +
		"usually a mistake.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/assign",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("assign: inspect's result is missing")
	}
	// Preorder visits each assignment in the order the original's does, as a cursor, which the fix that removes a
	// whole statement needs for the statement's parent and siblings.
	for cursor := range inspected.Root().Preorder((*ast.AssignStmt)(nil)) {
		assignment, isAssignment := cursor.Node().(*ast.AssignStmt)
		if !isAssignment {
			// Invariant: Preorder filtered by a node type hands over only nodes of that type.
			panic("assign: Preorder handed over a node that is not an assignment")
		}
		reportSelfAssignments(pass, cursor, assignment)
	}
	return nil, nil
}

// reportSelfAssignments reports assignment, the node at cursor, when any pair of its sides assigns a value to itself,
// with a fix that removes those pairs, keeping the commas between the pairs that stay, or the whole statement when
// every pair is one.
func reportSelfAssignments(pass *analysis.Pass, cursor inspector.Cursor, assignment *ast.AssignStmt) {
	// `:=` declares, and `+=` and its kin compute, so only a plain `=` can assign a value to itself.
	if assignment.Tok != token.ASSIGN {
		return
	}
	// A call or receive on the right that yields several values has no pairs.
	if len(assignment.Lhs) != len(assignment.Rhs) {
		return
	}
	// selfAssigned is each left side assigned to itself, as printed, in the statement's order.
	selfAssigned := []string{}
	edits := []analysis.TextEdit{}
	// runStartLeft and runStartRight are where the current run of adjacent self-assignments begins on each side, and
	// invalid outside a run. A run after the first pair begins at the end of the pair before it, so its edit takes
	// the comma before it; a run from the first pair begins at the pair itself and ends at the next pair's start, so
	// its edit takes the comma after it.
	runStartLeft, runStartRight := token.NoPos, token.NoPos
	for index, left := range assignment.Lhs {
		right := assignment.Rhs[index]
		if isSelfAssignment(pass, left, right) {
			selfAssigned = append(selfAssigned, formatted(pass.Fset, left))
			if !runStartLeft.IsValid() {
				if index > 0 {
					runStartLeft, runStartRight = assignment.Lhs[index-1].End(), assignment.Rhs[index-1].End()
				} else {
					runStartLeft, runStartRight = left.Pos(), right.Pos()
				}
			}
			continue
		}
		if runStartLeft.IsValid() {
			// The run ends at the pair before this one, or, when it began at the first pair, at this pair's start.
			endLeft, endRight := assignment.Lhs[index-1].End(), assignment.Rhs[index-1].End()
			if runStartLeft == assignment.Lhs[0].Pos() {
				endLeft, endRight = left.Pos(), right.Pos()
			}
			edits = append(edits,
				analysis.TextEdit{Pos: runStartLeft, End: endLeft},
				analysis.TextEdit{Pos: runStartRight, End: endRight},
			)
			runStartLeft, runStartRight = token.NoPos, token.NoPos
		}
	}
	// A run that reaches the statement's last pair ends there.
	if runStartLeft.IsValid() {
		last := len(assignment.Lhs) - 1
		edits = append(edits,
			analysis.TextEdit{Pos: runStartLeft, End: assignment.Lhs[last].End()},
			analysis.TextEdit{Pos: runStartRight, End: assignment.Rhs[last].End()},
		)
	}
	if len(selfAssigned) == 0 {
		return
	}
	// When every pair assigns a value to itself, the statement goes whole. Where it cannot, under a label for one,
	// deleteStatement returns nil, and the fix offers no edit, as the original's does, rather than the pairs' edits,
	// which would leave a bare `=`.
	if len(selfAssigned) == len(assignment.Lhs) {
		edits = deleteStatement(pass.Fset.File(assignment.Pos()), cursor, assignment)
	}
	pass.Report(analysis.Diagnostic{
		Pos:     assignment.Pos(),
		Message: "self-assignment of " + strings.Join(selfAssigned, ", "),
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   "Remove self-assignment",
			TextEdits: edits,
		}},
	})
}

// isSelfAssignment is whether assigning right to left assigns a value to itself: neither side has an effect, left is
// not an element of a map, and the two print the same.
func isSelfAssignment(pass *analysis.Pass, left ast.Expr, right ast.Expr) bool {
	// Two sides of different node types never print the same, and the check saves printing them.
	return hasNoEffects(pass.TypesInfo, left) &&
		hasNoEffects(pass.TypesInfo, right) &&
		!isMapIndex(pass.TypesInfo, left) &&
		reflect.TypeOf(left) == reflect.TypeOf(right) &&
		formatted(pass.Fset, left) == formatted(pass.Fset, right)
}

// isMapIndex is whether expression, through parentheses, indexes a map, whose element the assignment may create, so
// `values[key] = values[key]` is not useless. A type parameter's underlying type is its constraint, never a map, so an
// index into a map of a type parameter's type is judged as any other, as the original judges it.
func isMapIndex(information *types.Info, expression ast.Expr) bool {
	indexed, isIndex := ast.Unparen(expression).(*ast.IndexExpr)
	if !isIndex {
		return false
	}
	indexedType := information.Types[indexed.X].Type
	if indexedType == nil {
		return false
	}
	_, isMap := indexedType.Underlying().(*types.Map)
	return isMap
}

// hasNoEffects is whether expression cannot change the state of memory, conservatively: it may say an expression has
// an effect when it has none. x/tools' typesinternal.NoEffects.
func hasNoEffects(information *types.Info, expression ast.Expr) bool {
	noEffects := true
	ast.Inspect(expression, func(node ast.Node) bool {
		switch inspected := node.(type) {
		// Inspect calls with nil after each node's children, which is no expression and has no effect.
		case nil, *ast.Ident, *ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr,
			*ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr, *ast.TypeAssertExpr,
			*ast.StarExpr, *ast.CompositeLit,
			// What appears within an expression without being one.
			*ast.KeyValueExpr,
			*ast.FieldList,
			*ast.Field,
			*ast.Ellipsis,
			*ast.IndexListExpr:
		case *ast.ArrayType,
			*ast.StructType,
			*ast.ChanType,
			*ast.FuncType,
			*ast.MapType,
			*ast.InterfaceType:
			// A type has no effect, nor does anything within it, so the walk does not descend.
			return false
		case *ast.UnaryExpr:
			// A receive, `<-channel`, takes a value from the channel.
			if inspected.Op == token.ARROW {
				noEffects = false
			}
		case *ast.CallExpr:
			// A conversion has no effect, nor does a call of a pure builtin, though its operands might.
			if !information.Types[inspected.Fun].IsType() && !callsPureBuiltin(information, inspected) {
				noEffects = false
			}
		case *ast.FuncLit:
			// A function literal has no effect until called, so its body is not read.
			return false
		default:
			noEffects = false
		}
		return noEffects
	})
	return noEffects
}

// callsPureBuiltin is whether call calls a builtin that computes over its operands alone, as an operator does, and
// reads no state of the program. x/tools' typesinternal.CallsPureBuiltin.
func callsPureBuiltin(information *types.Info, call *ast.CallExpr) bool {
	called, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
	if !isIdentifier {
		return false
	}
	builtin, isBuiltin := information.ObjectOf(called).(*types.Builtin)
	if !isBuiltin {
		return false
	}
	switch builtin.Name() {
	// Not append, clear, close, copy, delete, panic, print, println or recover, which act or read state.
	case "len", "cap", "complex", "imag", "real", "make", "new", "max", "min":
		return true
	}
	return false
}

// formatted is node as gofmt prints it. x/tools' astutil.Format.
func formatted(fileSet *token.FileSet, node ast.Node) string {
	builder := strings.Builder{}
	// Ignored: a strings.Builder takes every write, and the printer fails only on a node it cannot print, which an
	// expression of a type-checked package never is, so the original ignores the error too.
	printer.Fprint(&builder, fileSet, node)
	return builder.String()
}
