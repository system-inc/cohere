// Package correctness_no_identical_branches is the Go engine's rule that a conditional's branches do not all do the
// same thing.
//
// An if whose two branches are the same code, or a switch whose every clause is, chooses nothing: the code runs the
// same way whatever the condition says. It is nearly always a copied branch that was never edited, and the reader
// trusts a decision that is not being made. It is the Go twin of nexus/correctness-no-identical-branches, whose first
// site was a quality flag in ahra that picked 'pro' either way, and of cohere-swift's port of it.
//
// # What it reports, and what it leaves alone, by decision
//
// The shapes follow the twins wherever Go has them:
//
//   - An if with an else whose two branches match. An if with no else has no second branch.
//   - An else-if chain is judged from its last if, the one with the final else: when its two branches match, that
//     condition is ignored, and the finding widens up the chain over every earlier if whose branch is the same too,
//     so `if a { x() } else if b { x() } else { x() }` is one finding on the whole chain. Two equal branches with a
//     different one after them are not reported: that is `if a || b` written long, and both conditions still decide.
//     A chain with no final else is never reported, since when no condition holds nothing runs.
//   - The twins' rare form, an if whose only statement is an if that matches its else-if whole, is kept.
//   - Empty branches are not reported. An empty branch is already its own smell, and a commented one carries its
//     intent in the comments, the one thing the comparison ignores.
//
// Switch is where Go parts from the twins. They decline it because a TypeScript or Swift case may fall through, so
// two cases with the same tokens can run differently. A Go case never falls through unless it says fallthrough, and
// the last clause cannot say it, so clauses that all match never fall through at all. An expression switch or type
// switch with a default, at least two clauses, and every clause the same non-empty code is reported. A switch with no
// default is not, since when no case matches nothing runs, so the cases still decide something. A select is not
// judged: two clauses that do the same thing with whichever channel is ready first is what a select is for.
//
// # Same tokens can still be different code
//
// Two branches are the same when they are the same syntax tree, with comments and layout ignored and every literal
// compared as written, so `"pro"` and a raw-string `pro` stay different. Go narrows no type through a condition, so
// most of what made the TypeScript twin ask its type checker does not arise. What remains is a name that means one
// thing in one branch and another in the other, and that is checked through the type information: every name must
// denote the same object on both sides, or two objects each side declares in the same place. Three shapes hinge on it:
//
//   - A type switch that binds a variable, `switch value := x.(type)`, gives it its own type in each clause, so
//     `fmt.Println(value)` under `case int` and under `default` passes two different types. The clauses' variables
//     are different objects, so such a switch is never reported. One that binds nothing is judged.
//   - The widening up an else-if chain compares branches under different ifs, whose init statements may each
//     declare the same name: in `if x := 1; a { use(x) } else if x := 2; b { use(x) } else { use(x) }`, a still
//     decides between 1 and 2, so the finding stays on `if x := 2; b`.
//   - A branch that declares its own variable and uses it matches one that does the same, since each use denotes
//     the declaration its own side made.
//
// Every way the comparison can be wrong is a missed finding, never a false one. A label used before its declaration
// in a branch, a field reached through an instantiated generic type, or a literal written in another form leaves
// the branches unmatched.
//
// # No fix
//
// Which branch was meant is the author's call, and deleting the conditional would keep the bug.
package correctness_no_identical_branches

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-no-identical-branches"

// listener hears every if, switch and type switch of the syntax dispatcher's walk. Nested conditionals are heard as
// the walk goes on, so each is judged on its own branches.
var listener = rule.Listener{
	Nodes: []ast.Node{(*ast.IfStmt)(nil), (*ast.SwitchStmt)(nil), (*ast.TypeSwitchStmt)(nil)},
	Node:  judgeConditional,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "correctness_no_identical_branches", "Flags an if with an else, and a "+
	"switch or type switch with a default, whose every branch is the same code, since its condition then chooses "+
	"nothing and one branch was usually meant to differ. Comments and layout do not count, and every name must "+
	"denote the same thing on both sides. Empty branches, a select, and a type switch that binds a variable are left "+
	"alone.", &listener)

// Rule is this rule's entry in the Go engine's registry. It reads the type information to tell a name that means the
// same thing in both branches from one that only looks the same.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var identicalBranchesMessage = policy.MessageOf(Name, "identicalBranches")

// judgeConditional judges an if, with its ancestors in stack, or a switch or type switch.
func judgeConditional(context *rule.Context, node ast.Node, stack []ast.Node) {
	switch statement := node.(type) {
	case *ast.IfStmt:
		judgeIf(context, statement, stack)
	case *ast.SwitchStmt:
		judgeSwitch(context, statement.Switch, statement.Body)
	case *ast.TypeSwitchStmt:
		judgeSwitch(context, statement.Switch, statement.Body)
	}
}

// judgeIf reports statement when its two branches match, widened up its else-if chain over every earlier if whose
// branch matches too. stack is statement's ancestors, and an if that is another's else has that other if as its
// parent, so the chain's links above statement are the ifs that end stack, each the else of the one before it.
func judgeIf(context *rule.Context, statement *ast.IfStmt, stack []ast.Node) {
	switch elseBranch := statement.Else.(type) {
	case *ast.BlockStmt:
		if isEmpty(statement.Body.List) || isEmpty(elseBranch.List) ||
			!newComparison(context).sameStatements(statement.Body.List, elseBranch.List) {
			return
		}
	case *ast.IfStmt:
		// An if whose else is another if compares its branch against that whole if, which differs from it except in
		// the rare `if a { if c { p() } else { q() } } else if c { p() } else { q() }`, where a really is ignored. The
		// usual chain is judged at its last if and reaches back up through the widening below.
		if len(statement.Body.List) != 1 || !newComparison(context).same(statement.Body.List[0], elseBranch) {
			return
		}
	default:
		// No else, so there is no second branch to compare.
		return
	}
	anchor := statement
	for index := len(stack) - 1; index >= 0; index-- {
		parent, isIf := stack[index].(*ast.IfStmt)
		if !isIf || parent.Else != anchor ||
			!newComparison(context).sameStatements(parent.Body.List, statement.Body.List) {
			break
		}
		anchor = parent
	}
	report(context, anchor.If, anchor.Body.Lbrace)
}

// judgeSwitch reports the expression or type switch that opens at keyword with body when it has a default, at least
// two clauses, and every clause the same non-empty code.
func judgeSwitch(context *rule.Context, keyword token.Pos, body *ast.BlockStmt) {
	if len(body.List) < 2 {
		return
	}
	clauses := make([]*ast.CaseClause, 0, len(body.List))
	hasDefault := false
	for _, statement := range body.List {
		// The parser puts only case clauses in a switch's body, so anything else is skipped rather than guessed at.
		clause, isClause := statement.(*ast.CaseClause)
		if !isClause {
			return
		}
		// A clause with no expressions is the default.
		if clause.List == nil {
			hasDefault = true
		}
		clauses = append(clauses, clause)
	}
	if !hasDefault || isEmpty(clauses[0].Body) {
		return
	}
	// Each clause body is compared with no clause's variable mapped to another's, so a type switch's bound variable,
	// a different object in every clause, keeps its clauses apart.
	for _, clause := range clauses[1:] {
		if !newComparison(context).sameStatements(clauses[0].Body, clause.Body) {
			return
		}
	}
	report(context, keyword, body.Lbrace)
}

// isEmpty is whether a branch runs nothing: no statements, or only the empty statements a stray semicolon leaves.
// Comments are not statements, so a branch holding only comments is empty too.
func isEmpty(statements []ast.Stmt) bool {
	for _, statement := range statements {
		if _, isEmptyStatement := statement.(*ast.EmptyStmt); !isEmptyStatement {
			return false
		}
	}
	return true
}

func newComparison(context *rule.Context) *comparison {
	return &comparison{information: context.TypesInfo, counterparts: map[types.Object]types.Object{}}
}

// The syntax tree types the walk treats specially, found once.
var (
	positionType     = reflect.TypeFor[token.Pos]()
	commentGroupType = reflect.TypeFor[*ast.CommentGroup]()
	// The walk names the deprecated ast.Object and ast.Scope only to skip their fields, and matching them by name
	// as strings would hide that dependency from the compiler.
	//cohere:ignore SA1019 named only so the walk can skip these deprecated fields
	objectType = reflect.TypeFor[*ast.Object]()
	//cohere:ignore SA1019 named only so the walk can skip these deprecated fields
	scopeType = reflect.TypeFor[*ast.Scope]()
)

// report flags the conditional that opens at keyword with its first brace at brace, so the finding covers the
// header, `if ready {` or `switch mode {`, and not the branches.
func report(context *rule.Context, keyword token.Pos, brace token.Pos) {
	context.Report(analysis.Diagnostic{
		Pos:      keyword,
		End:      brace + 1,
		Category: identicalBranchesMessage.Id,
		Message:  identicalBranchesMessage.Render(map[string]string{}),
	})
}
