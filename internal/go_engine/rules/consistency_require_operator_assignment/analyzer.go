// Package consistency_require_operator_assignment is the Go engine's rule that an assignment which reads its own
// target through a binary operator says so with the compound form: `count = count + 1` is `count += 1`.
//
// Writing the target twice makes a reader check that the two spellings name the same place, and a later edit that
// renames one and not the other is a bug the shorthand cannot express. Go's spec defines `x op= y` as `x = x op (y)`
// with x evaluated once, so where evaluating the target twice has no effect of its own the two forms are the same
// program, and the rule offers the rewrite as a fix.
//
// It is the Go form of ESLint core's operator-assignment, ported for TypeScript in internal/lint/rules/core, whose
// `always` arm it follows:
//
//	x = x op y    reported for every operator with a compound form, and fixed to `x op= y`
//	x = y op x    reported only when op commutes, `*`, `&`, `|` and `^`, and never fixed
//
// `+` is not commutative here although addition is, because `+` is also string concatenation, and the port keeps
// `x = y + x` clean for that reason. The reversed form is not fixed, as the port declines it: the rewrite moves the
// other operand to the far side of the target, and that move is left to the writer.
//
// `x = x + y + z` is `(x + y) + z`, whose left operand is not the target, so it is left alone as the port leaves it.
// Rewriting it as `x += y + z` would regroup the sum, which changes the result for floats, whose addition does not
// associate, and `x = x - y - z` would need a different operator inside.
package consistency_require_operator_assignment

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-operator-assignment"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_require_operator_assignment",
	Doc: "Flags x = x op y, where both sides name the same variable, field, index or pointer target, and offers " +
		"x op= y, because writing the target twice makes a reader check the two spellings match. x = y op x is " +
		"flagged without a fix when op is *, &, | or ^, and left alone otherwise.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var longFormAssignmentMessage = policy.MessageOf(Name, "longFormAssignment")

// commutes is, for every binary operator with a compound assignment form, whether `x = y op x` is the same
// computation as `x = x op y`. It is the whole set of operators the rule knows, and the port's split of it: `+` is
// absent from the commuting four because `+` concatenates strings.
var commutes = map[token.Token]bool{
	token.MUL:     true,
	token.AND:     true,
	token.OR:      true,
	token.XOR:     true,
	token.ADD:     false,
	token.SUB:     false,
	token.QUO:     false,
	token.REM:     false,
	token.SHL:     false,
	token.SHR:     false,
	token.AND_NOT: false,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if assignment, isAssignment := node.(*ast.AssignStmt); isAssignment {
				judge(pass, file, assignment)
			}
			return true
		})
	}
	return nil, nil
}

// judge reports assignment when it is a plain `=` of one target whose right side is a binary operation reading that
// target where the compound form would.
func judge(pass *analysis.Pass, file *ast.File, assignment *ast.AssignStmt) {
	// `:=` declares rather than assigns, and a tuple assignment has no compound form.
	if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return
	}
	operation, isBinary := ast.Unparen(assignment.Rhs[0]).(*ast.BinaryExpr)
	if !isBinary {
		return
	}
	commutative, hasCompoundForm := commutes[operation.Op]
	if !hasCompoundForm {
		return
	}
	target := ast.Unparen(assignment.Lhs[0])
	switch {
	case isSameReference(pass, target, operation.X):
		report(pass, assignment, target, operation, compoundFix(file, assignment, operation))
	case commutative && isSameReference(pass, target, operation.Y):
		report(pass, assignment, target, operation, nil)
	}
}

// isSameReference is whether left and right name the same place with nothing in either that could do anything when
// evaluated: the same variable, a field selected from the same reference, an element indexed from the same
// reference by the same key, or the same pointer dereferenced. A key is a variable or a constant, and constants
// compare by type and value, so `values[0x1]` and `values[1]` are one element.
//
// Anything else is not a reference, which is what keeps `values[next()] = values[next()] + 1` and
// `values[index+1] = values[index+1] + 1` clean: a call may answer differently the second time, and an arithmetic
// key is left alone rather than proved pure. Parentheses are transparent, as they are to the port.
func isSameReference(pass *analysis.Pass, left ast.Expr, right ast.Expr) bool {
	unwrappedLeft := ast.Unparen(left)
	unwrappedRight := ast.Unparen(right)
	leftValue := pass.TypesInfo.Types[unwrappedLeft]
	rightValue := pass.TypesInfo.Types[unwrappedRight]
	if leftValue.Value != nil || rightValue.Value != nil {
		// A key of interface type keeps its dynamic type, so `byKey[int8(1)]` and `byKey[1]` are two entries. The
		// types are compared before the values for a second reason: constant.Compare reads a string against a number
		// as two copies of the string, and one basic type on both sides is what it needs to compare like with like.
		return leftValue.Value != nil && rightValue.Value != nil &&
			types.Identical(leftValue.Type, rightValue.Type) &&
			constant.Compare(leftValue.Value, token.EQL, rightValue.Value)
	}
	switch leftNode := unwrappedLeft.(type) {
	case *ast.Ident:
		rightNode, isIdentifier := unwrappedRight.(*ast.Ident)
		if !isIdentifier {
			return false
		}
		// Only a variable is a place; a function or a type of the same name is not, and the blank identifier has no
		// object at all.
		variable, isVariable := pass.TypesInfo.ObjectOf(leftNode).(*types.Var)
		return isVariable && pass.TypesInfo.ObjectOf(rightNode) == variable
	case *ast.SelectorExpr:
		rightNode, isSelector := unwrappedRight.(*ast.SelectorExpr)
		if !isSelector {
			return false
		}
		leftSelection, leftIsSelection := pass.TypesInfo.Selections[leftNode]
		rightSelection, rightIsSelection := pass.TypesInfo.Selections[rightNode]
		if !leftIsSelection && !rightIsSelection {
			// A qualified identifier, another package's variable, has no selection; its name resolves to it.
			variable, isVariable := pass.TypesInfo.Uses[leftNode.Sel].(*types.Var)
			return isVariable && pass.TypesInfo.Uses[rightNode.Sel] == variable
		}
		return leftIsSelection && rightIsSelection &&
			leftSelection.Kind() == types.FieldVal && rightSelection.Kind() == types.FieldVal &&
			leftSelection.Obj() == rightSelection.Obj() &&
			isSameReference(pass, leftNode.X, rightNode.X)
	case *ast.IndexExpr:
		rightNode, isIndex := unwrappedRight.(*ast.IndexExpr)
		// An instantiation of a generic function is an IndexExpr too, and its function is no variable, so it fails
		// the comparison of what is indexed.
		return isIndex &&
			isSameReference(pass, leftNode.X, rightNode.X) &&
			isSameReference(pass, leftNode.Index, rightNode.Index)
	case *ast.StarExpr:
		rightNode, isStar := unwrappedRight.(*ast.StarExpr)
		return isStar && isSameReference(pass, leftNode.X, rightNode.X)
	}
	return false
}

// compoundFix rewrites `x = x op y` as `x op= y`, or is nil when a comment would be lost.
//
// It touches only the span from the `=` through the operator, which becomes `op=`, and any closing parentheses
// that wrapped the whole operation, so the target and the other operand are kept as written, comments and spacing
// inside them included. A comment in either replaced span would be discarded, so the finding stands without a fix.
func compoundFix(file *ast.File, assignment *ast.AssignStmt, operation *ast.BinaryExpr) []analysis.SuggestedFix {
	operatorEnd := operation.OpPos + token.Pos(len(operation.Op.String()))
	right := assignment.Rhs[0]
	for _, group := range file.Comments {
		for _, comment := range group.List {
			inOperatorSpan := comment.Pos() >= assignment.TokPos && comment.Pos() < operatorEnd
			inClosingSpan := comment.Pos() >= operation.End() && comment.Pos() < right.End()
			if inOperatorSpan || inClosingSpan {
				return nil
			}
		}
	}
	compound := operation.Op.String() + "="
	edits := []analysis.TextEdit{{Pos: assignment.TokPos, End: operatorEnd, NewText: []byte(compound)}}
	// The opening parentheses of `count = (count + 1)` sit inside the span above; the closing ones follow the operand.
	if operation.End() != right.End() {
		edits = append(edits, analysis.TextEdit{Pos: operation.End(), End: right.End()})
	}
	return []analysis.SuggestedFix{{Message: "Write it with " + compound, TextEdits: edits}}
}

// report flags assignment, naming its target as the source spells it, with fixes when there are any.
func report(
	pass *analysis.Pass,
	assignment *ast.AssignStmt,
	target ast.Expr,
	operation *ast.BinaryExpr,
	fixes []analysis.SuggestedFix,
) {
	pass.Report(analysis.Diagnostic{
		Pos:      assignment.Pos(),
		End:      assignment.End(),
		Category: longFormAssignmentMessage.Id,
		Message: longFormAssignmentMessage.Render(map[string]string{
			"target":   types.ExprString(target),
			"operator": operation.Op.String(),
		}),
		SuggestedFixes: fixes,
	})
}
