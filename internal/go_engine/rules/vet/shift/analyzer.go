// Package shift is vet's shift pass, a shift of an integer by a constant that equals or exceeds its width in bits,
// which leaves nothing of the value: zero for a shift left or an unsigned shift right, and zero or minus one for a
// signed shift right. The width is the target's, read from the pass's sizes, so a shift of an int by 32 is judged
// for the architecture the package is checked for. A value of a type parameter is judged by the narrowest type its
// type set holds, said to be that narrow only maybe when the type set holds several widths. A shift of a constant is
// left alone, since `^uint(0) >> 63` and its kin are how code tells a 32 bit target from a 64 bit one, and so is a
// shift in a branch a constant condition rules out, the branch written for another architecture.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/shift/shift.go and dead.go, whose
// logic it keeps and whose messages it reports word for word, at the same range, and from the parts of
// golang.org/x/tools@v0.50.0 internal/typeparams (normalize.go, termlist.go, typeterm.go) that compute a type
// parameter's type set, which an analyzer outside x/tools cannot import. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
package shift

import (
	"go/ast"
	"go/constant"
	"go/printer"
	"go/token"
	"go/types"
	"math"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name:     "shift",
	Doc:      "check for shifts that equal or exceed the width of the integer",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/shift",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("shift: inspect's result is missing")
	}
	// A complete pass first, so every branch a constant rules out is known before any shift is judged.
	dead := map[ast.Node]bool{}
	// PreorderSeq rather than inspector.All, here and below, since the original visits two kinds of node in one
	// preorder walk, and All walks one kind. The type switches are checked, so a node of another kind is passed over.
	for statement := range inspected.PreorderSeq((*ast.IfStmt)(nil), (*ast.SwitchStmt)(nil)) {
		markDead(pass.TypesInfo, dead, statement)
	}
	for node := range inspected.PreorderSeq((*ast.AssignStmt)(nil), (*ast.BinaryExpr)(nil)) {
		if dead[node] {
			continue
		}
		switch shifting := node.(type) {
		case *ast.BinaryExpr:
			if shifting.Op == token.SHL || shifting.Op == token.SHR {
				checkLongShift(pass, shifting, shifting.X, shifting.Y)
			}
		case *ast.AssignStmt:
			// A shift assignment takes one operand on each side, so the parser never builds another shape of one.
			if len(shifting.Lhs) != 1 || len(shifting.Rhs) != 1 {
				continue
			}
			if shifting.Tok == token.SHL_ASSIGN || shifting.Tok == token.SHR_ASSIGN {
				checkLongShift(pass, shifting, shifting.Lhs[0], shifting.Rhs[0])
			}
		}
	}
	return nil, nil
}

// checkLongShift reports shift, whose shifted operand is shifted and whose count is count, when the count is a
// constant that equals or exceeds the width in bits of the shifted operand's type, or, for a type parameter, of the
// narrowest type in its type set.
func checkLongShift(pass *analysis.Pass, shift ast.Node, shifted ast.Expr, count ast.Expr) {
	if pass.TypesInfo.Types[shifted].Value != nil {
		// A shift of a constant is left alone: `^uint(0) >> 63` and its kin tell a 32 bit target from a 64 bit one.
		return
	}
	countValue := pass.TypesInfo.Types[count].Value
	if countValue == nil {
		return
	}
	// ToInt gives an Int, or Unknown for a count no integer holds, which Int64Val refuses, as it does an Int too large
	// for an int64.
	amount, isInt64 := constant.Int64Val(constant.ToInt(countValue))
	if !isInt64 {
		return
	}
	shiftedType := pass.TypesInfo.Types[shifted].Type
	// Types records every expression of a package that type-checks, so the original's guard never fires here.
	if shiftedType == nil {
		return
	}
	// The types whose widths are judged: the shifted operand's own, or every type a type parameter's type set holds.
	var structuralTypes []types.Type
	switch unaliased := types.Unalias(shiftedType).(type) {
	case *types.TypeParam:
		terms, termsError := structuralTerms(unaliased)
		// A type set that can't be computed or is empty fails go/types too, so the shift never type-checked.
		if termsError != nil {
			return
		}
		for _, term := range terms {
			structuralTypes = append(structuralTypes, term.Type())
		}
	default:
		structuralTypes = append(structuralTypes, unaliased)
	}
	widths := map[int64]struct{}{}
	for _, structuralType := range structuralTypes {
		widths[8*pass.TypesSizes.Sizeof(structuralType)] = struct{}{}
	}
	// With no width at all, a type parameter with no restriction on its type set, narrowest stays the largest int64,
	// and the shift of a value whose type set is not all integers never type-checked anyway.
	narrowest := int64(math.MaxInt64)
	for width := range widths {
		narrowest = min(narrowest, width)
	}
	if amount < narrowest {
		return
	}
	qualifier := ""
	if len(widths) > 1 {
		qualifier = "may be "
	}
	pass.ReportRangef(
		shift, "%s (%s%d bits) too small for shift of %d", formatted(pass.Fset, shifted), qualifier, narrowest, amount,
	)
}

// formatted is node printed as source. x/tools' astutil.Format.
func formatted(files *token.FileSet, node ast.Node) string {
	var builder strings.Builder
	// Ignored: as in the original, since writing to a strings.Builder never fails, and an expression the printer
	// can't print is still named by what it did print.
	_ = printer.Fprint(&builder, files, node)
	return builder.String()
}
