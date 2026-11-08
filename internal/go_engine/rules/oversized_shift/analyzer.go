// Package oversized_shift is staticcheck's SA9006, a shift of a fixed-size integer by at least as many bits as it
// has, which always clears it, ported as a check on syntax that reports through the kit every port of staticcheck
// shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9006/sa9006.go, whose logic it keeps and
// whose message it reports word for word, over the whole shift, through staticcheck_report.Report with no option, as
// the original reports through report.Report: a generated file is reported too. The original matches the pattern
// `(Or (AssignStmt _ (Or ">>=" "<<=") _) (BinaryExpr _ (Or ">>" "<<") _))` through code.Matches, which names no symbol
// for code.CouldMatchAny to rule a package out by and so walks every assignment and binary expression the inspector
// holds; the port walks the same nodes and asks the same of each: an assignment that shifts in place, or a binary
// expression that shifts, whose shifted operand is of a fixed-size integer type, signed or not, by the sizes the
// pass's types were checked for, and whose shift is a constant that staticcheck_code.ExprToInt, the port of the helper
// the original asks, reads as at least that many bits. The original keeps nothing from one shift to the next, and
// stays the oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere they
// hold.
package oversized_shift

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every assignment and binary expression, in
// preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.AssignStmt)(nil), (*ast.BinaryExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		Check(context, node)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA9006",
	"SA9006",
	"Flags a shift of a fixed-size integer by at least its width, which always clears it: staticcheck's "+
		"SA9006, ported as a check on syntax.",
	&Listener,
)

// messageFormat is the original's message, word for word, around the operand's width and the shift.
const messageFormat = "shifting %d-bit value by %d bits will always clear it"

// bitsInByte is how many bits each byte of a type's size holds.
const bitsInByte = 8

// Check judges one node: an assignment that shifts in place, or a binary expression that shifts, by at least the
// shifted operand's width.
func Check(context *rule.Context, node ast.Node) {
	switch shift := node.(type) {
	case *ast.AssignStmt:
		if shift.Tok != token.SHL_ASSIGN && shift.Tok != token.SHR_ASSIGN {
			return
		}
		reportOversized(context, shift, shift.Lhs[0], shift.Rhs[0])
	case *ast.BinaryExpr:
		if shift.Op != token.SHL && shift.Op != token.SHR {
			return
		}
		reportOversized(context, shift, shift.X, shift.Y)
	}
}

// reportOversized reports node when shifted is of a fixed-size integer type and amount a constant of at least its
// width in bits.
func reportOversized(context *rule.Context, node ast.Node, shifted ast.Expr, amount ast.Expr) {
	basic, isBasic := context.TypesInfo.TypeOf(shifted).Underlying().(*types.Basic)
	if !isBasic {
		return
	}
	switch basic.Kind() {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
	default:
		// int, uint and uintptr vary by platform, and are left alone.
		return
	}
	width := context.TypesSizes.Sizeof(basic) * bitsInByte
	bits, isConstant := staticcheck_code.ExprToInt(context, amount)
	if !isConstant || bits < width {
		return
	}
	staticcheck_report.Report(context, node, fmt.Sprintf(messageFormat, width, bits))
}
