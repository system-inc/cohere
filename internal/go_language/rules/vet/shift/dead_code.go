package shift

import (
	"go/ast"
	"go/constant"
	"go/types"
)

// markDead puts into dead every node of the branches statement, an if or a switch, can never take, by a constant
// condition or a constant case, so a shift written for another architecture's word size is not judged against this
// one's. It is a simplified detector of dead code, x/tools' updateDead in shift's dead.go: an if whose condition is a
// constant, a tagless switch's case whose every expression is the constant false, and an integer switch's case whose
// every value is a constant other than the tag's. Anything else, a for loop or a switch on a string, is taken as
// live.
//
// Mutates: dead the set of dead nodes the walk is building, filled one statement at a time
func markDead(information *types.Info, dead map[ast.Node]bool, statement ast.Node) {
	// A node already dead lies in a dead subtree, every node of which is already marked.
	if dead[statement] {
		return
	}
	switch branching := statement.(type) {
	case *ast.IfStmt:
		condition := information.Types[branching.Cond].Value
		if condition == nil {
			return
		}
		// The condition of an if that type-checks is a boolean, so BoolVal does not panic.
		if !constant.BoolVal(condition) {
			markSubtreeDead(dead, branching.Body)
			return
		}
		if branching.Else != nil {
			markSubtreeDead(dead, branching.Else)
		}
	case *ast.SwitchStmt:
		if branching.Tag == nil {
			for _, clause := range caseClauses(branching) {
				// The default case has no expressions, and is never dead.
				if clause.List != nil && everyExpressionIsFalse(information, clause) {
					markSubtreeDead(dead, clause)
				}
			}
			return
		}
		// A case is dead when every one of its values is a constant other than the tag's. As in the original, only
		// an integer tag that fits a uint64 is compared.
		tag := information.Types[branching.Tag].Value
		if tag == nil || tag.Kind() != constant.Int {
			return
		}
		tagNumber, tagFits := constant.Uint64Val(tag)
		if !tagFits {
			return
		}
		for _, clause := range caseClauses(branching) {
			if clause.List != nil && everyValueDiffers(information, clause, tagNumber) {
				markSubtreeDead(dead, clause)
			}
		}
	}
}

// everyExpressionIsFalse is whether every expression of clause, a case of a tagless switch, is the constant false, so
// the case is never taken.
func everyExpressionIsFalse(information *types.Info, clause *ast.CaseClause) bool {
	for _, expression := range clause.List {
		value := information.Types[expression].Value
		if value == nil || value.Kind() != constant.Bool || constant.BoolVal(value) {
			return false
		}
	}
	return true
}

// everyValueDiffers is whether every value of clause, a case of a switch on an integer constant, is a constant other
// than tagNumber, the tag's, so the case is never taken. A value that is not a constant, or does not fit a uint64,
// might match, as the original judges it.
func everyValueDiffers(information *types.Info, clause *ast.CaseClause, tagNumber uint64) bool {
	for _, expression := range clause.List {
		value := information.Types[expression].Value
		if value == nil {
			return false
		}
		// A case value of an integer switch that type-checks is converted to the tag's integer type, so its constant
		// is an Int and Uint64Val does not panic.
		number, numberFits := constant.Uint64Val(value)
		if !numberFits || number == tagNumber {
			return false
		}
	}
	return true
}

// caseClauses is the clauses of switchStatement's body, each a case or the default.
func caseClauses(switchStatement *ast.SwitchStmt) []*ast.CaseClause {
	clauses := make([]*ast.CaseClause, 0, len(switchStatement.Body.List))
	for _, statement := range switchStatement.Body.List {
		clause, isClause := statement.(*ast.CaseClause)
		if !isClause {
			// Invariant: the parser fills a switch statement's body with case clauses alone.
			panic("shift: a switch statement's body holds something other than a case clause")
		}
		clauses = append(clauses, clause)
	}
	return clauses
}

// markSubtreeDead puts root and every node beneath it into dead. x/tools' setDead, a closure in updateDead.
//
// Mutates: dead the set of dead nodes the walk is building, filled one subtree at a time
func markSubtreeDead(dead map[ast.Node]bool, root ast.Node) {
	ast.Inspect(root, func(node ast.Node) bool {
		if node != nil {
			dead[node] = true
		}
		return true
	})
}
