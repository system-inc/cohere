package unreachable

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

// deadState is one function's walk, x/tools' deadState: what findLabels learns of its labels and breaks, and, as
// findDead goes, whether the statement it comes to next is reachable.
type deadState struct {
	pass *analysis.Pass
	// function is the cursor of the function walked, under which a dead statement's cursor is found for its fix.
	function inspector.Cursor
	// panicDeclared is whether the package declares its own `panic` at package level, in any of its files.
	panicDeclared bool
	// hasBreak is every loop, select, switch and type switch some break leaves.
	hasBreak map[ast.Stmt]bool
	// hasGoto is every label some goto names.
	hasGoto map[string]bool
	// labels is the statement each label labels, by the label's name.
	labels map[string]ast.Stmt
	// breakTarget is the innermost loop, select, switch or type switch around the statement findLabels is at, which
	// a break with no label leaves, and nil outside every one.
	breakTarget ast.Stmt
	// reachable is whether the statement findDead comes to next can be reached.
	reachable bool
}

// findLabels gathers information about the labels defined and used by statement and about which statements break,
// whether a label is involved or not.
func (state *deadState) findLabels(statement ast.Stmt) {
	switch node := statement.(type) {
	case *ast.AssignStmt,
		*ast.BadStmt,
		*ast.DeclStmt,
		*ast.DeferStmt,
		*ast.EmptyStmt,
		*ast.ExprStmt,
		*ast.GoStmt,
		*ast.IncDecStmt,
		*ast.ReturnStmt,
		*ast.SendStmt:
		// No statements inside.

	case *ast.BlockStmt:
		for _, inner := range node.List {
			state.findLabels(inner)
		}

	case *ast.BranchStmt:
		switch node.Tok {
		case token.GOTO:
			if node.Label != nil {
				state.hasGoto[node.Label.Name] = true
			}
		case token.BREAK:
			left := state.breakTarget
			if node.Label != nil {
				// The label's statement, or nil when no label of that name has been seen yet, since a break names
				// only a label around it, which findLabels has already passed.
				left = state.labels[node.Label.Name]
			}
			if left != nil {
				state.hasBreak[left] = true
			}
		}

	case *ast.IfStmt:
		state.findLabels(node.Body)
		if node.Else != nil {
			state.findLabels(node.Else)
		}

	case *ast.LabeledStmt:
		state.labels[node.Label.Name] = node.Stmt
		state.findLabels(node.Stmt)

	// The five statements a break with no label leaves are the same case, but each one's Body is read through its
	// own type, so the cases cannot be merged.
	case *ast.ForStmt:
		state.findLabelsWithin(node, node.Body)
	case *ast.RangeStmt:
		state.findLabelsWithin(node, node.Body)
	case *ast.SelectStmt:
		state.findLabelsWithin(node, node.Body)
	case *ast.SwitchStmt:
		state.findLabelsWithin(node, node.Body)
	case *ast.TypeSwitchStmt:
		state.findLabelsWithin(node, node.Body)

	case *ast.CommClause:
		for _, inner := range node.Body {
			state.findLabels(inner)
		}

	case *ast.CaseClause:
		for _, inner := range node.Body {
			state.findLabels(inner)
		}

	default:
		// Invariant: the cases above are every statement go/ast declares, so only a nil statement comes here, which
		// the parser never puts where a statement belongs. The original stops the process here.
		panic(fmt.Sprintf("unreachable: internal error in findLabels: unexpected statement %T", statement))
	}
}

// findLabelsWithin gathers what findLabels does from body, with target, the loop, select or switch body belongs to,
// as the statement a break with no label leaves.
func (state *deadState) findLabelsWithin(target ast.Stmt, body *ast.BlockStmt) {
	outer := state.breakTarget
	state.breakTarget = target
	state.findLabels(body)
	state.breakTarget = outer
}

// findDead walks statement looking for dead code. If state.reachable is false on entry, statement itself is dead.
// When findDead returns, state.reachable tells whether the statement following statement is reachable.
func (state *deadState) findDead(statement ast.Stmt) {
	// A labeled statement some goto names is taken as reachable, by that goto. This is slightly conservative, since
	// whether the goto itself is reachable is never asked, so `L: goto L` provokes no finding, as in the original.
	if labeled, isLabeled := statement.(*ast.LabeledStmt); isLabeled && state.hasGoto[labeled.Label.Name] {
		state.reachable = true
	}

	if !state.reachable {
		// An unreachable empty statement is never reported.
		if _, isEmpty := statement.(*ast.EmptyStmt); !isEmpty {
			state.report(statement)
			// Only the first of a run of dead statements is reported.
			state.reachable = true
		}
	}

	switch node := statement.(type) {
	case *ast.AssignStmt,
		*ast.BadStmt,
		*ast.DeclStmt,
		*ast.DeferStmt,
		*ast.EmptyStmt,
		*ast.GoStmt,
		*ast.IncDecStmt,
		*ast.SendStmt:
		// No control flow.

	case *ast.BlockStmt:
		for _, inner := range node.List {
			state.findDead(inner)
		}

	case *ast.BranchStmt:
		switch node.Tok {
		case token.BREAK, token.GOTO, token.FALLTHROUGH:
			state.reachable = false
		case token.CONTINUE:
			// A continue is taken as terminating too. The spec's terminating statements leave it out, since a
			// continue can never be the last statement before a return, but for finding dead code by syntax it
			// redirects control just as the others do.
			state.reachable = false
		}

	case *ast.ExprStmt:
		if state.callsBuiltinPanic(node) {
			state.reachable = false
		}

	case *ast.ForStmt:
		state.findDead(node.Body)
		state.reachable = node.Cond != nil || state.hasBreak[node]

	case *ast.IfStmt:
		state.findDead(node.Body)
		if node.Else != nil {
			bodyFallsThrough := state.reachable
			state.reachable = true
			state.findDead(node.Else)
			state.reachable = state.reachable || bodyFallsThrough
		} else {
			// The body may not have run at all.
			state.reachable = true
		}

	case *ast.LabeledStmt:
		state.findDead(node.Stmt)

	case *ast.RangeStmt:
		state.findDead(node.Body)
		state.reachable = true

	case *ast.ReturnStmt:
		state.reachable = false

	case *ast.SelectStmt:
		// Unlike a switch, a select without a default does not fall through: it blocks until one of its cases can
		// run, so whether it has a default does not matter.
		anyReachable := false
		for _, clause := range node.Body.List {
			communication, isCommunication := clause.(*ast.CommClause)
			if !isCommunication {
				// Invariant: the parser puts only communication clauses in a select's body.
				panic(fmt.Sprintf("unreachable: a select's body holds a %T", clause))
			}
			state.reachable = true
			for _, inner := range communication.Body {
				state.findDead(inner)
			}
			anyReachable = anyReachable || state.reachable
		}
		state.reachable = anyReachable || state.hasBreak[node]

	case *ast.SwitchStmt:
		state.findDeadInClauses(node, node.Body)

	case *ast.TypeSwitchStmt:
		state.findDeadInClauses(node, node.Body)

	default:
		// Invariant: the cases above are every statement go/ast declares but the case and communication clauses,
		// which the parser puts only in a switch's or a select's body, read above, and nil, which it never puts where
		// a statement belongs. The original stops the process here.
		panic(fmt.Sprintf("unreachable: internal error in findDead: unexpected statement %T", statement))
	}
}

// findDeadInClauses walks the case clauses of body, the body of switchStatement, a switch or type switch, each
// reachable on entry, and leaves state.reachable telling whether the statement after the switch is reachable: when
// some clause falls out of it, some break leaves it, or it has no default, which behaves as a default with an empty
// body.
func (state *deadState) findDeadInClauses(switchStatement ast.Stmt, body *ast.BlockStmt) {
	anyReachable := false
	hasDefault := false
	for _, clause := range body.List {
		caseClause, isCase := clause.(*ast.CaseClause)
		if !isCase {
			// Invariant: the parser puts only case clauses in a switch's or a type switch's body.
			panic(fmt.Sprintf("unreachable: a switch's body holds a %T", clause))
		}
		if caseClause.List == nil {
			hasDefault = true
		}
		state.reachable = true
		for _, inner := range caseClause.Body {
			state.findDead(inner)
		}
		anyReachable = anyReachable || state.reachable
	}
	state.reachable = anyReachable || state.hasBreak[switchStatement] || !hasDefault
}

// callsBuiltinPanic is whether statement is a call of the builtin panic: of the bare name `panic`, which the parser
// resolved to no declaration in its file, in a package that declares no `panic` of its own at package level either.
func (state *deadState) callsBuiltinPanic(statement *ast.ExprStmt) bool {
	call, isCall := statement.X.(*ast.CallExpr)
	if !isCall {
		return false
	}
	name, isName := call.Fun.(*ast.Ident)
	if !isName || name.Name != "panic" {
		return false
	}
	// The parser's resolution is what the original reads, and, the pass reading no type information, the only
	// record of a local or same-file declaration of the name.
	//cohere:ignore SA1019 the port reads no type information, and the parser's resolution is the original's judgment
	return name.Obj == nil && !state.panicDeclared
}

// report reports statement as unreachable, with the fix that deletes it.
func (state *deadState) report(statement ast.Stmt) {
	// statement is in the function's tree, so the cursor is found. A statement outside it would leave the zero
	// cursor, and deleteStatement would stop on it, as the original's DeleteStmt does.
	statementCursor, _ := state.function.FindNode(statement)
	// This report is a frequent source of findings beyond the end of a truncated file, as in the original; see
	// go.dev/issue/71659.
	state.pass.Report(analysis.Diagnostic{
		Pos:     statement.Pos(),
		End:     statement.End(),
		Message: "unreachable code",
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   "Remove",
			TextEdits: deleteStatement(state.pass.Fset.File(statement.Pos()), statementCursor),
		}},
	})
}
